// Package sentinel implements vexillum's supervision loop (Capa 4, paso
// 2): it polls herdr for status changes on tasks vexillum is tracking,
// persists any transition, and records a durable wake so the commander's
// Stop hook can surface it and keep working instead of quietly ending its
// turn.
//
// One sentinel process runs per machine (AcquireLock, IsRunning), not per
// project - it sweeps every project namespaced under vexillumHome (see
// internal/project) in a single Tick. A wake, like the task it's about,
// belongs to exactly one project's <project root>/wakes/ - Drain only
// ever surfaces (and removes) the wakes for the one project root it's
// given, so a commander in project A never drains a wake that belongs to
// a soldier in project B.
//
// Polling, not push (herdr's events.subscribe): this matches firstmate's
// own acknowledged fallback - "polling runs every cycle and remains the
// permanent fallback" (docs/herdr-backend.md, "Push events and polling
// fallback") - rather than depending on a raw socket event stream that
// isn't exposed through herdr's CLI (internal/herdr only shells out to
// the CLI, deliberately, per its own package doc).
package sentinel

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/pause"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/settle"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

// Wake is a durable record that a tracked task's status changed - the
// sentinel's equivalent of firstmate's wake queue. It's delivered, not
// acknowledged: Drain removes a wake's file the moment it hands it back,
// so a wake's mere presence on disk under <project root>/wakes/ already
// means "pending" - there's no separate acked flag to go stale.
type Wake struct {
	TaskID     string       `json:"task_id"`
	Kind       state.Kind   `json:"kind"`
	OldStatus  state.Status `json:"old_status"`
	NewStatus  state.Status `json:"new_status"`
	DetectedAt time.Time    `json:"detected_at"`

	// ReportPath is set when this wake settles a scout task to Done and
	// its report file (internal/report) already exists at that exact
	// moment - no new detection channel, this is just tickProject
	// checking for it within the same poll that already reads the
	// task's live herdr status (PRD asked for report detection to ride
	// the existing tick, not add one). A commander draining this wake
	// can go straight to the polished report instead of the raw
	// transcript in Output. Empty for a mission (which never has a
	// report - see internal/report's package doc) or for a scout whose
	// write hadn't landed by this exact tick: that's not an error, just
	// a race this field doesn't try to resolve - internal/cli.runRelease
	// makes its own live check before ever gating on one.
	ReportPath string `json:"report_path,omitempty"`
}

func wakesDir(projectRoot string) string {
	return filepath.Join(projectRoot, "wakes")
}

func wakePath(projectRoot, taskID string) string {
	return filepath.Join(wakesDir(projectRoot), taskID+".json")
}

// tickReadLines matches soldier.defaultReadLines: the sentinel is now
// the one that captures a task's final output for any soldier that
// outlives dispatch's own short quick-settle probe (internal/soldier's
// RunInHerdr doc comment), which is the normal case for real work.
const tickReadLines = 500

// settleGracePeriod is how long Tick refuses to act on a just-submitted
// task at all, regardless of what its live status reads. herdr's own
// "agent prompt --wait" documents that a submission starting from a
// non-working state needs up to 5000ms before a working/blocked
// transition is observed - internal/soldier.RunInHerdr's own probe call
// already accounts for that internally (herdr requires seeing
// working/blocked before it will accept a subsequent idle/done as a
// real settle). Tick's own AgentStatus read has no such protection - a
// live case caught this exactly: with the sentinel auto-started right
// alongside dispatch (see internal/cli.ensureSentinelRunning), Tick
// polled a task 5s after submission, read the still-stale pre-work
// "idle", mapped it straight to Done, and won the race against
// RunInHerdr's own (correctly guarded) probe - recording a false "done"
// for a soldier that had done nothing yet. A margin over herdr's
// documented 5s bound closes it without needing the same --until
// machinery on this side.
const settleGracePeriod = 8 * time.Second

// notFoundConfirmWindow is how long Tick waits after first observing a
// task's agent as genuinely gone (herdr's "agent_not_found", not a
// transient read error - see herdr.IsNotFound) before marking that task
// interrupted (PRD v1, Capa 4 restart-proof: "los soldiers que se puedan
// resumir se resumen, los que no, quedan marcados como interrumpidos").
// A single observation isn't enough on its own - a herdr hiccup during
// something like its own restart could otherwise falsely condemn a
// soldier that's actually still there; requiring it to still be gone a
// tick or two later is what herdr.APIError alone can't tell us.
const notFoundConfirmWindow = 10 * time.Second

// Tick sweeps every project namespaced under vexillumHome
// (vexillumHome/projects/*, see internal/project) and, within each,
// checks every task currently marked running against its live herdr
// agent status - persisting any status change and recording a wake for
// it. Returns how many wakes it recorded across all projects. There's
// one sentinel process per machine (see AcquireLock), so this is the
// single place responsible for reconciling every project's tasks, not
// just whichever one last called dispatch. A fatal error in one
// project's sweep stops the whole Tick early, the same way a fatal error
// already stopped a single-project Tick before projects existed - the
// next Tick (5s later, see Run) picks up wherever this one left off.
func Tick(vexillumHome string, client herdr.Client) (int, error) {
	roots, err := project.AllRoots(vexillumHome)
	if err != nil {
		return 0, fmt.Errorf("listing projects: %w", err)
	}

	woke := 0
	for _, projectRoot := range roots {
		n, err := tickProject(projectRoot, client)
		woke += n
		if err != nil {
			return woke, err
		}
	}
	return woke, nil
}

// tickProject is Tick's per-project body: it never crosses project
// boundaries, so a wake it records can only ever belong to the project
// rooted at projectRoot.
func tickProject(projectRoot string, client herdr.Client) (int, error) {
	tasks, err := state.List(projectRoot)
	if err != nil {
		return 0, fmt.Errorf("listing tasks: %w", err)
	}

	woke := 0
	for _, task := range tasks {
		if task.Status != state.StatusRunning || task.HerdrAgentName == "" {
			continue
		}
		if time.Since(task.UpdatedAt) < settleGracePeriod {
			continue
		}

		live, err := client.AgentStatus(task.HerdrAgentName)
		if err != nil {
			if herdr.IsNotFound(err) {
				interrupted, ierr := handleAgentNotFound(projectRoot, task)
				if ierr != nil {
					return woke, ierr
				}
				if interrupted {
					woke++
				}
			}
			continue
		}
		if live == "" || live == "unknown" {
			continue
		}

		if !task.AgentNotFoundSince.IsZero() {
			// A prior tick saw this agent as gone, but it just resolved
			// fine - that was a blip, not a real teardown. Clear the mark
			// so a later genuine disappearance starts its own fresh
			// confirmation window instead of inheriting a stale one.
			task.AgentNotFoundSince = time.Time{}
			if err := state.Save(projectRoot, task); err != nil {
				return woke, fmt.Errorf("clearing not-found mark for task %s: %w", task.ID, err)
			}
		}

		newStatus := soldier.MapAgentStatus(live)
		if newStatus == task.Status {
			continue
		}

		// Every transition reaching here settles a Running task out of
		// Running (MapAgentStatus never re-maps live status back to
		// Running once it's left it, and task.Status is Running for
		// every task that reaches this point - see the loop's own guard
		// above). A Done transition needs corroboration first: idle only
		// ever means the turn stopped responding, never why - see
		// settleIdleTask and internal/pause's package doc.
		if newStatus != state.StatusDone {
			if err := settleTransition(projectRoot, task, newStatus, client); err != nil {
				return woke, err
			}
			woke++
			continue
		}

		settled, err := settleIdleTask(projectRoot, task, client)
		if err != nil {
			return woke, err
		}
		if settled {
			woke++
		}
	}
	return woke, nil
}

// settleTransition persists task's straightforward transition out of
// Running - blocked or failed, the only two live statuses MapAgentStatus
// can produce here besides Done (see tickProject's own guard reasoning) -
// and records a wake for it. Captures the final transcript now, since
// dispatch's own quick-settle probe usually returned long before this
// point.
func settleTransition(projectRoot string, task state.Task, newStatus state.Status, client herdr.Client) error {
	old := task.Status
	task.Status = newStatus
	task.UpdatedAt = time.Now().UTC()
	task.IdleUnconfirmedSince = time.Time{}
	if output, err := client.AgentRead(task.HerdrAgentName, tickReadLines); err == nil {
		task.Output = output
	}
	if newStatus == state.StatusBlocked {
		task.Decision = soldier.ExtractDecision(task.Output)
	}
	if err := state.Save(projectRoot, task); err != nil {
		return fmt.Errorf("persisting task %s: %w", task.ID, err)
	}
	if err := recordWake(projectRoot, task, old, newStatus, ""); err != nil {
		return fmt.Errorf("recording wake for task %s: %w", task.ID, err)
	}
	return nil
}

// settleIdleTask handles a Running task whose live herdr status just
// mapped to Done (idle or done). It never trusts that alone - "idle"
// only ever means the turn stopped responding, never why (internal/pause's
// package doc):
//
//  1. A strong completion signal - a scout's internal/report file, or a
//     mission's own commit ahead of its camp's base (settle.HasCompletionSignal) -
//     settles the task Done, same as before this package read either
//     signal.
//  2. Otherwise, a currently valid declared pause (internal/pause.Active)
//     means the soldier is deliberately waiting on something of its own:
//     the task stays Running, rechecked next tick, no wake recorded.
//  3. Otherwise the idle turn is unexplained - handleIdleUnconfirmed takes
//     over, mirroring handleAgentNotFound's own confirm-window pattern
//     instead of trusting it as done.
//
// Returns whether it settled the task (and so recorded a wake).
func settleIdleTask(projectRoot string, task state.Task, client herdr.Client) (bool, error) {
	strong, reportPath, err := settle.HasCompletionSignal(projectRoot, task)
	if err != nil {
		return false, err
	}
	if strong {
		return true, settleDone(projectRoot, task, reportPath, client)
	}

	_, active, err := pause.Active(projectRoot, task.HerdrAgentName, time.Now())
	if err != nil {
		return false, fmt.Errorf("checking declared pause for task %s: %w", task.ID, err)
	}
	if active {
		if !task.IdleUnconfirmedSince.IsZero() {
			task.IdleUnconfirmedSince = time.Time{}
			if err := state.Save(projectRoot, task); err != nil {
				return false, fmt.Errorf("clearing idle-unconfirmed mark for task %s: %w", task.ID, err)
			}
		}
		return false, nil
	}

	return handleIdleUnconfirmed(projectRoot, task)
}

// settleDone persists task's corroborated Done transition and records a
// wake for it - exactly what tickProject did unconditionally before this
// package required corroboration first.
func settleDone(projectRoot string, task state.Task, reportPath string, client herdr.Client) error {
	old := task.Status
	task.Status = state.StatusDone
	task.UpdatedAt = time.Now().UTC()
	task.IdleUnconfirmedSince = time.Time{}
	if output, err := client.AgentRead(task.HerdrAgentName, tickReadLines); err == nil {
		task.Output = output
	}
	if err := state.Save(projectRoot, task); err != nil {
		return fmt.Errorf("persisting task %s: %w", task.ID, err)
	}
	if err := recordWake(projectRoot, task, old, state.StatusDone, reportPath); err != nil {
		return fmt.Errorf("recording wake for task %s: %w", task.ID, err)
	}
	return nil
}

// idleUnconfirmedConfirmWindow mirrors notFoundConfirmWindow's own
// reasoning, applied to a different ambiguity: an idle turn with no
// completion signal and no declared pause could just be a brand-new
// settle whose report/commit hasn't landed on disk yet (the same kind of
// race internal/report's own doc comment already calls out), not a
// genuinely stuck soldier. Waiting this long before treating it as
// suspicious avoids flagging every ordinary settle as unconfirmed.
const idleUnconfirmedConfirmWindow = 30 * time.Second

// handleIdleUnconfirmed records the first time task's turn was observed
// idle with neither a completion signal nor a valid declared pause, and
// marks it StatusUnconfirmed once that's held true for
// idleUnconfirmedConfirmWindow - mirrors handleAgentNotFound exactly,
// applied to a different kind of ambiguity (an agent that's still there,
// just unexplained, instead of one that's genuinely gone). Returns
// whether it settled the task (and so recorded a wake) on this call.
func handleIdleUnconfirmed(projectRoot string, task state.Task) (bool, error) {
	if task.IdleUnconfirmedSince.IsZero() {
		task.IdleUnconfirmedSince = time.Now().UTC()
		if err := state.Save(projectRoot, task); err != nil {
			return false, fmt.Errorf("recording idle-unconfirmed mark for task %s: %w", task.ID, err)
		}
		return false, nil
	}

	if time.Since(task.IdleUnconfirmedSince) < idleUnconfirmedConfirmWindow {
		return false, nil
	}

	old := task.Status
	task.Status = state.StatusUnconfirmed
	task.UpdatedAt = time.Now().UTC()
	if task.Output != "" {
		task.Output += "\n\n"
	}
	task.Output += "[vexillum] this soldier's turn went idle with no completion signal (no report for a scout, " +
		"no new commit for a mission) and no declared pause (internal/pause) - marked unconfirmed, not done. " +
		"Check its camp/pane before assuming either way."
	if err := state.Save(projectRoot, task); err != nil {
		return false, fmt.Errorf("persisting unconfirmed task %s: %w", task.ID, err)
	}
	if err := recordWake(projectRoot, task, old, state.StatusUnconfirmed, ""); err != nil {
		return false, fmt.Errorf("recording wake for unconfirmed task %s: %w", task.ID, err)
	}
	return true, nil
}

// handleAgentNotFound records the first time task's agent was observed
// genuinely gone, and marks the task Interrupted once that's held true
// for notFoundConfirmWindow. Returns whether it interrupted the task
// (and so recorded a wake) on this call.
func handleAgentNotFound(projectRoot string, task state.Task) (interrupted bool, err error) {
	if task.AgentNotFoundSince.IsZero() {
		task.AgentNotFoundSince = time.Now().UTC()
		if err := state.Save(projectRoot, task); err != nil {
			return false, fmt.Errorf("recording not-found mark for task %s: %w", task.ID, err)
		}
		return false, nil
	}

	if time.Since(task.AgentNotFoundSince) < notFoundConfirmWindow {
		return false, nil
	}

	old := task.Status
	task.Status = state.StatusInterrupted
	task.UpdatedAt = time.Now().UTC()
	if task.Output != "" {
		task.Output += "\n\n"
	}
	task.Output += "[vexillum] this soldier's herdr agent disappeared (pane closed, or herdr restarted) - marked interrupted. Any work it already committed is still in its camp."
	if err := state.Save(projectRoot, task); err != nil {
		return false, fmt.Errorf("persisting interrupted task %s: %w", task.ID, err)
	}
	if err := recordWake(projectRoot, task, old, state.StatusInterrupted, ""); err != nil {
		return false, fmt.Errorf("recording wake for interrupted task %s: %w", task.ID, err)
	}
	return true, nil
}

func recordWake(projectRoot string, task state.Task, old, newStatus state.Status, reportPath string) error {
	dir := wakesDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	w := Wake{
		TaskID:     task.ID,
		Kind:       task.Kind,
		OldStatus:  old,
		NewStatus:  newStatus,
		DetectedAt: time.Now().UTC(),
		ReportPath: reportPath,
	}
	return atomicfile.WriteJSON(wakePath(projectRoot, task.ID), w)
}

// Drain returns every pending wake for the project rooted at projectRoot,
// oldest first, deleting each one's file as it's delivered - a wake is
// surfaced once, not repeated on every drain, and its file's mere
// existence on disk is what "pending" means (no separate acked flag to
// keep in sync).
func Drain(projectRoot string) ([]Wake, error) {
	dir := wakesDir(projectRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing wakes: %w", err)
	}

	var drained []Wake
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var w Wake
		if err := json.Unmarshal(data, &w); err != nil {
			continue
		}
		drained = append(drained, w)
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("removing delivered wake for task %s: %w", w.TaskID, err)
		}
	}

	sort.Slice(drained, func(i, j int) bool { return drained[i].DetectedAt.Before(drained[j].DetectedAt) })
	return drained, nil
}

func lockPath(vexillumHome string) string {
	return filepath.Join(vexillumHome, "sentinel.pid")
}

// AcquireLock claims the single-sentinel-per-home lock, refusing if
// another sentinel process is already alive - matches firstmate's own
// "never broadly kill watchers... race-proof singleton lock" principle:
// vexillum should never end up with two sentinels racing to reconcile
// the same tasks. Call the returned release func (e.g. via defer) to
// release the lock on clean shutdown.
//
// The claim itself is atomic, not a read-then-write: two processes racing
// AcquireLock at the same instant can't both observe "no live lock" and
// both write the pid file (see claimLock). The loser inspects whatever
// pid won and either reports it as already running (if alive) or, if the
// pid file is stale (unparseable, or its pid is dead - e.g. a sentinel
// that crashed instead of releasing cleanly), removes it and retries.
func AcquireLock(vexillumHome string) (release func(), err error) {
	if err := os.MkdirAll(vexillumHome, 0o755); err != nil {
		return nil, err
	}
	path := lockPath(vexillumHome)

	for {
		claimed, err := claimLock(vexillumHome, path)
		if err != nil {
			return nil, err
		}
		if claimed {
			return func() { _ = os.Remove(path) }, nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				// Removed between our failed claim and this read - someone
				// else reclaimed a stale lock. Retry our own claim.
				continue
			}
			return nil, readErr
		}
		if pid, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr == nil && processAlive(pid) {
			return nil, fmt.Errorf("a sentinel is already running (pid %d) for %s - not starting a second one", pid, vexillumHome)
		}
		// Stale lock (unparseable contents, or a pid that's no longer
		// alive because its sentinel crashed without releasing) - reclaim
		// it and retry the atomic claim.
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("removing stale sentinel lock: %w", err)
		}
	}
}

// claimLock attempts to atomically publish path as this process's lock
// file, containing its pid. It reports claimed=false (no error) if path
// already exists, so the caller can decide whether that's a live sentinel
// or a stale lock to reclaim.
//
// A plain O_CREATE|O_EXCL open followed by a separate write would leave a
// window where path exists but is still empty - a concurrent reader in
// that window would see unparseable content and could mistake a lock
// that's mid-claim for a stale one. To avoid that, the pid is written in
// full to a temp file first, then published via a hard link: link(2)
// atomically fails with EEXIST if path already exists, and otherwise path
// never appears with anything but its full, already-written content.
func claimLock(vexillumHome, path string) (claimed bool, err error) {
	tmp, err := os.CreateTemp(vexillumHome, "sentinel.pid.tmp-*")
	if err != nil {
		return false, err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.WriteString(strconv.Itoa(os.Getpid())); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}

	if err := os.Link(tmpPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// IsRunning reports whether a sentinel process is currently alive for
// vexillumHome, without claiming the lock itself - internal/cli uses
// this to decide whether dispatch needs to auto-start one.
func IsRunning(vexillumHome string) bool {
	data, err := os.ReadFile(lockPath(vexillumHome))
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false
	}
	return processAlive(pid)
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds regardless of whether the pid
	// is live; signal 0 is the standard existence probe (sends nothing,
	// just checks permission/existence).
	return proc.Signal(syscall.Signal(0)) == nil
}

// Run polls forever at the given interval, logging each tick's outcome
// to stdout. Meant to run in the foreground of a long-lived background
// process the commander starts once per project.
func Run(vexillumHome string, client herdr.Client, interval time.Duration, stdout io.Writer) {
	for {
		woke, err := Tick(vexillumHome, client)
		switch {
		case err != nil:
			fmt.Fprintf(stdout, "sentinel: tick error: %v\n", err)
		case woke > 0:
			fmt.Fprintf(stdout, "sentinel: recorded %d wake(s)\n", woke)
		}
		time.Sleep(interval)
	}
}
