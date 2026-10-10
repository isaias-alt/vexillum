// Package sentinel implements vexillum's supervision loop: it polls herdr
// for status changes on tasks vexillum is tracking, persists any transition, and records a durable wake so the commander's
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
// Polling, not push (herdr's events.subscribe): polling runs every cycle
// and remains the permanent fallback, rather than depending on a raw socket
// event stream that isn't exposed through herdr's CLI (internal/herdr only
// shells out to the CLI, deliberately, per its own package doc).
package sentinel

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/pause"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/settle"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

// Wake is a durable record that a tracked task's status changed - the
// sentinel's wake queue. It's delivered, not acknowledged: Drain removes a
// wake's file the moment it hands it back, so a wake's mere presence on disk
// under <project root>/wakes/ already means "pending" - there's no separate
// acked flag to go stale.
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
	// a race this field doesn't try to resolve - internal/cli.runStrike
	// makes its own live check before ever gating on one.
	ReportPath string `json:"report_path,omitempty"`
}

func wakesDir(projectRoot string) string {
	return filepath.Join(projectRoot, "wakes")
}

// claimSeq makes each claimed file's name unique within the process, so a
// wake rewritten for the same task while an earlier drain is still reading
// its claimed copy can never be renamed over it.
var claimSeq atomic.Uint64

// claimedPath is where a drain moves wake file path while it reads it. The
// result no longer ends in ".json", so no drain lists it as pending.
func claimedPath(path string) string {
	return fmt.Sprintf("%s.claimed-%d-%d", path, os.Getpid(), claimSeq.Add(1))
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
// interrupted (restart-proof: soldiers that can be resumed are resumed, the
// rest are marked interrupted).
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

	if err := reopenActiveTasks(projectRoot, tasks, client); err != nil {
		return 0, err
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
			saved, err := saveIfRunning(projectRoot, task)
			if err != nil {
				return woke, fmt.Errorf("clearing not-found mark for task %s: %w", task.ID, err)
			}
			if !saved {
				continue
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
			settled, err := settleTransition(projectRoot, task, newStatus, client)
			if err != nil {
				return woke, err
			}
			if settled {
				woke++
			}
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

// reopenable reports whether a task in status s can be put back to Running
// by reopenActiveTasks: a soldier that settled (done, unconfirmed), asked a
// question (blocked) or was shipped (its pane stays open until the pull
// request merges) and whose pane is still open can be prompted again.
// interrupted and failed are deliberately absent: their pane is gone. A
// reopened shipped task settles back to done, not shipped: the new work is
// not on the pull request until it is shipped again.
func reopenable(s state.Status) bool {
	return s == state.StatusDone || s == state.StatusBlocked || s == state.StatusUnconfirmed || s == state.StatusShipped
}

// reopenActiveTasks puts a settled task back to Running when its herdr
// agent is working again. The sentinel only ever notifies on a transition
// out of Running, so a soldier re-prompted after it settled (the commander
// ran "herdr agent prompt" by hand, or the general typed into its pane)
// would otherwise finish a second time with no transition and no wake, and
// the commander would wait forever. Reopening it is what makes the next
// settle a transition again. No wake is recorded for the reopening itself:
// whoever re-prompted the soldier already knows.
//
// Only a task that still holds its camp lease is probed (camp.LeasedTasks):
// a struck task's pane is closed, and probing every historical task on
// every tick would cost one herdr call each, forever. A failed probe
// (pane gone, herdr hiccup) just leaves the task as it was - never marks
// it interrupted, which is only for tasks the sentinel was watching
// run. The reopened task gets a fresh UpdatedAt, so tickProject's settle
// grace period covers the stale-idle window right after a prompt, exactly
// as it does for a fresh dispatch.
func reopenActiveTasks(projectRoot string, tasks []state.Task, client herdr.Client) error {
	var leased map[string]bool
	leasesLoaded := false
	for _, task := range tasks {
		if !reopenable(task.Status) || task.HerdrAgentName == "" {
			continue
		}
		if !leasesLoaded {
			leasesLoaded = true
			var err error
			leased, err = camp.LeasedTasks(projectRoot)
			if err != nil {
				// An unreadable pool means "can't tell", not "nothing is
				// leased": skip the reopen pass this tick, the next one
				// retries.
				return nil
			}
		}
		if !leased[task.ID] {
			continue
		}

		live, err := client.AgentStatus(task.HerdrAgentName)
		if err != nil || live != "working" {
			continue
		}

		task.Status = state.StatusRunning
		task.UpdatedAt = time.Now().UTC()
		task.IdleUnconfirmedSince = time.Time{}
		task.AgentNotFoundSince = time.Time{}
		if err := state.Save(projectRoot, task); err != nil {
			return fmt.Errorf("reopening task %s: %w", task.ID, err)
		}
	}
	return nil
}

// saveIfRunning persists task only while its record on disk is still
// Running, and reports whether it did. tickProject works from a snapshot of
// the task list, and a herdr call or a captured transcript can take seconds,
// so a command that moved the task in the meantime (vx land recording it
// done, vx strike, vx prompt) must win: writing the stale snapshot back would
// undo it, for instance by marking a landed mission interrupted because its
// pane was closed. Every transition tickProject makes out of Running goes
// through here, so a task that is no longer Running is left exactly as it
// is and no wake is recorded for it.
func saveIfRunning(projectRoot string, task state.Task) (bool, error) {
	current, err := state.Load(projectRoot, task.ID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if current.Status != state.StatusRunning {
		return false, nil
	}
	if err := state.Save(projectRoot, task); err != nil {
		return false, err
	}
	return true, nil
}

// settleTransition persists task's straightforward transition out of
// Running - blocked or failed, the only two live statuses MapAgentStatus
// can produce here besides Done (see tickProject's own guard reasoning) -
// and records a wake for it. Captures the final transcript now, since
// dispatch's own quick-settle probe usually returned long before this
// point. Returns whether it settled the task: false when the task is no
// longer Running on disk (see saveIfRunning).
func settleTransition(projectRoot string, task state.Task, newStatus state.Status, client herdr.Client) (bool, error) {
	old := task.Status
	task.Status = newStatus
	task.UpdatedAt = time.Now().UTC()
	task.IdleUnconfirmedSince = time.Time{}
	if output, err := client.AgentRead(task.HerdrAgentName, tickReadLines); err == nil {
		task.Output = output
	}
	if newStatus == state.StatusBlocked {
		// Reaching here means live == "blocked" (the only raw status
		// MapAgentStatus maps to StatusBlocked) - herdr's own classifier
		// caught it, almost always Claude Code's AskUserQuestion modal.
		// See soldier.ResolveBlockedDecision.
		task.Decision = soldier.ResolveBlockedDecision(client, task.HerdrAgentName, task.Output)
	}
	saved, err := saveIfRunning(projectRoot, task)
	if err != nil {
		return false, fmt.Errorf("persisting task %s: %w", task.ID, err)
	}
	if !saved {
		return false, nil
	}
	if err := recordWake(projectRoot, task, old, newStatus, ""); err != nil {
		return false, fmt.Errorf("recording wake for task %s: %w", task.ID, err)
	}
	return true, nil
}

// settleIdleTask handles a Running task whose live herdr status just
// mapped to Done (idle or done). It never trusts that alone - "idle"
// only ever means the turn stopped responding, never why (internal/pause's
// package doc):
//
//  0. A needs-decision: line in the soldier's fresh output
//     (soldier.FinalTurnNeedsDecision) takes priority over everything
//     below: the soldier explicitly said it's waiting on the general - a
//     plain-prose question, the one shape herdr's own classifier never
//     catches on its own (see the durable decision record's design
//     report) - so the task is forced Blocked instead of settling Done or
//     falling through to the ambiguous-idle handling.
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
// Steps 0 and 1 end the turn, so before either one is taken the pane is
// read until it stops changing (awaitSettledTranscript): the output saved with
// the wake is the soldier's whole last message, not whatever was painted at the
// instant herdr said idle. When it cannot be shown to hold still, the task
// still settles but Output says so.
//
// Returns whether it settled the task (and so recorded a wake).
func settleIdleTask(projectRoot string, task state.Task, client herdr.Client) (bool, error) {
	output, readErr := client.AgentRead(task.HerdrAgentName, tickReadLines)

	// Whichever way this idle turn ends below (a question for the general or
	// a finished task), what the sentinel saves is the soldier's last word:
	// wait until the pane stops changing before reading it, so the final
	// report is in Output when the commander is woken for it.
	strong, reportPath, signalErr := settle.HasCompletionSignal(projectRoot, task)
	note := ""
	switch {
	case readErr != nil:
		note = transcriptUnreadNote
	case signalErr == nil && strong || needsDecisionIn(task, output):
		var stable bool
		if output, stable = awaitSettledTranscript(client, task.HerdrAgentName, output); !stable {
			note = transcriptChangingNote
		}
	}

	if readErr == nil {
		if d, found := soldier.FinalTurnNeedsDecision(task, output); found {
			return settleNeedsDecision(projectRoot, task, output, d)
		}
	}

	if signalErr != nil {
		return false, signalErr
	}
	if strong {
		return settleDone(projectRoot, task, reportPath, output, readErr, note)
	}

	_, active, err := pause.Active(projectRoot, task.HerdrAgentName, time.Now())
	if err != nil {
		return false, fmt.Errorf("checking declared pause for task %s: %w", task.ID, err)
	}
	if active {
		if !task.IdleUnconfirmedSince.IsZero() {
			task.IdleUnconfirmedSince = time.Time{}
			if _, err := saveIfRunning(projectRoot, task); err != nil {
				return false, fmt.Errorf("clearing idle-unconfirmed mark for task %s: %w", task.ID, err)
			}
		}
		return false, nil
	}

	return handleIdleUnconfirmed(projectRoot, task)
}

func needsDecisionIn(task state.Task, output string) bool {
	_, found := soldier.FinalTurnNeedsDecision(task, output)
	return found
}

// settleReadGap is how long awaitSettledTranscript waits between two reads
// of a soldier's pane, and settleReadAttempts how many reads it makes at
// most. herdr reports a turn idle as soon as the agent stops, and a pane can
// still be painting the end of its last message at that moment, so one read
// at the instant of the transition can miss the final report. Two reads that
// agree a second apart mean the pane has settled. Bounded, so a pane that
// never stops changing (a clock in its status line) delays the wake by a few
// seconds, not forever.
var settleReadGap = time.Second

const settleReadAttempts = 5

// SetSettleReadGap swaps the pause between those reads and returns a function
// that restores it. For tests, which have no pane to wait for.
func SetSettleReadGap(d time.Duration) (restore func()) {
	old := settleReadGap
	settleReadGap = d
	return func() { settleReadGap = old }
}

// Appended to a task's Output when the sentinel could not vouch that it holds
// the soldier's whole final message, so the gap shows up where the commander
// reads the result instead of looking like a report that was never written.
const (
	transcriptChangingNote = "[vexillum] the soldier's pane was still changing when it was marked done, so its final report may be missing above. " +
		"Read the pane, or ask the soldier for the report with 'vx prompt'."
	transcriptUnreadNote = "[vexillum] the soldier's pane could not be read when it was marked done, so the output above may not include its final report. " +
		"Read the pane, or ask the soldier for the report with 'vx prompt'."
)

// awaitSettledTranscript re-reads the pane of agent until two consecutive
// reads agree, starting from first, and returns the last text read and
// whether it held still. A read that fails along the way ends the wait with
// the last good text and stable false.
func awaitSettledTranscript(client herdr.Client, agent, first string) (output string, stable bool) {
	output = first
	for attempt := 1; attempt < settleReadAttempts; attempt++ {
		time.Sleep(settleReadGap)
		next, err := client.AgentRead(agent, tickReadLines)
		if err != nil {
			return output, false
		}
		if next == output {
			return output, true
		}
		output = next
	}
	return output, false
}

// settleNeedsDecision persists task's transition from an apparently-idle
// turn straight to StatusBlocked, because its fresh output actually
// carries a needs-decision: line (soldier.FinalTurnNeedsDecision) -
// the soldier is waiting on the general, not finished. output is
// whatever settleIdleTask already read to find that line, so this never
// re-reads it.
func settleNeedsDecision(projectRoot string, task state.Task, output string, decision *state.Decision) (bool, error) {
	old := task.Status
	task.Status = state.StatusBlocked
	task.Output = output
	task.Decision = decision
	task.UpdatedAt = time.Now().UTC()
	task.IdleUnconfirmedSince = time.Time{}
	saved, err := saveIfRunning(projectRoot, task)
	if err != nil {
		return false, fmt.Errorf("persisting task %s: %w", task.ID, err)
	}
	if !saved {
		return false, nil
	}
	if err := recordWake(projectRoot, task, old, state.StatusBlocked, ""); err != nil {
		return false, fmt.Errorf("recording wake for task %s: %w", task.ID, err)
	}
	return true, nil
}

// settleDone persists task's corroborated Done transition and records a
// wake for it - exactly what tickProject did unconditionally before this
// package required corroboration first. output/readErr are whatever
// settleIdleTask already read while checking for a needs-decision line,
// so this never issues a second AgentRead for the same tick.
func settleDone(projectRoot string, task state.Task, reportPath, output string, readErr error, note string) (bool, error) {
	old := task.Status
	task.Status = state.StatusDone
	task.UpdatedAt = time.Now().UTC()
	task.IdleUnconfirmedSince = time.Time{}
	if readErr == nil {
		task.Output = output
	}
	if note != "" {
		if task.Output != "" {
			task.Output += "\n\n"
		}
		task.Output += note
	}
	saved, err := saveIfRunning(projectRoot, task)
	if err != nil {
		return false, fmt.Errorf("persisting task %s: %w", task.ID, err)
	}
	if !saved {
		return false, nil
	}
	if err := recordWake(projectRoot, task, old, state.StatusDone, reportPath); err != nil {
		return false, fmt.Errorf("recording wake for task %s: %w", task.ID, err)
	}
	return true, nil
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
		if _, err := saveIfRunning(projectRoot, task); err != nil {
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
	saved, err := saveIfRunning(projectRoot, task)
	if err != nil {
		return false, fmt.Errorf("persisting unconfirmed task %s: %w", task.ID, err)
	}
	if !saved {
		return false, nil
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
		if _, err := saveIfRunning(projectRoot, task); err != nil {
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
	saved, err := saveIfRunning(projectRoot, task)
	if err != nil {
		return false, fmt.Errorf("persisting interrupted task %s: %w", task.ID, err)
	}
	if !saved {
		return false, nil
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

// Drain returns the pending wakes for the project rooted at projectRoot
// that are still news, oldest first. Every wake file it finds is consumed:
// a wake is surfaced at most once, never repeated on every drain, and its
// file's mere existence on disk is what "pending" means (no separate acked
// flag to keep in sync).
//
// A wake is a note that a task changed status, written when the sentinel
// saw it. By the time anyone drains, the commander may have moved on: a
// backlog that built up while nobody was listening would otherwise replay
// as a burst of "soldier changed" notices for tasks long since landed and
// struck. Such a wake is consumed without being returned - see
// wakeIsCurrent for what counts as still current.
//
// Each wake is claimed by renaming its file before it is read: rename is
// atomic with exactly one winner, so two drains racing over the same wake
// (the Stop hook's await and a manual drain) can never both deliver it. A
// plain remove is not enough: concurrent unlinks of one file can all
// report success on some filesystems, so no remove result says who won.
func Drain(projectRoot string) ([]Wake, error) {
	dir := wakesDir(projectRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing wakes: %w", err)
	}

	leases := lazyLeases(projectRoot)
	var drained []Wake
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		claimed := claimedPath(path)
		if err := os.Rename(path, claimed); err != nil {
			// Already claimed by a concurrent drain (or gone): not ours.
			continue
		}
		data, err := os.ReadFile(claimed)
		if rmErr := os.Remove(claimed); rmErr != nil && !os.IsNotExist(rmErr) {
			return nil, fmt.Errorf("removing delivered wake %s: %w", entry.Name(), rmErr)
		}
		if err != nil {
			continue
		}
		var w Wake
		if err := json.Unmarshal(data, &w); err != nil {
			// A corrupt wake is garbage, consumed like any other.
			continue
		}
		if wakeIsCurrent(projectRoot, w, leases) {
			drained = append(drained, w)
		}
	}

	sort.Slice(drained, func(i, j int) bool { return drained[i].DetectedAt.Before(drained[j].DetectedAt) })
	return drained, nil
}

// lazyLeases returns a function that reads the project's camp leases on
// first use and remembers the answer, so a drain with nothing to evaluate
// never touches the pool. A pool that cannot be read yields nil: "can't
// tell", which wakeIsCurrent treats as not struck.
func lazyLeases(projectRoot string) func() map[string]bool {
	var leased map[string]bool
	loaded := false
	return func() map[string]bool {
		if !loaded {
			loaded = true
			if l, err := camp.LeasedTasks(projectRoot); err == nil {
				leased = l
			}
		}
		return leased
	}
}

// wakeIsCurrent reports whether w still describes its task: the commander
// would learn something true by hearing it. It is not when
//
//   - the task is gone, or its status is no longer the one the wake
//     announced (the commander answered, re-prompted, shipped or redispatched
//     it since - whoever did that already knows);
//   - the task's camp was struck, so its soldier and pane are gone and
//     there is nothing left to act on (a task that never had a camp is not
//     subject to this check);
//   - it announced a mission done and that mission's commits are no longer
//     ahead of its base: they were landed.
//
// Anything it cannot establish (an unreadable pool, a camp whose git state
// cannot be read) counts as current: a notice is never dropped on a guess.
func wakeIsCurrent(projectRoot string, w Wake, leases func() map[string]bool) bool {
	task, err := state.Load(projectRoot, w.TaskID)
	if err != nil {
		return !errors.Is(err, os.ErrNotExist)
	}
	if task.Status != w.NewStatus {
		return false
	}
	if task.CampPath != "" {
		if leased := leases(); leased != nil && !leased[task.ID] {
			return false
		}
	}
	if task.Kind == state.KindMission && w.NewStatus == state.StatusDone && task.CampPath != "" && task.CampBase != "" {
		if ahead, err := camp.HasNewCommits(task.CampPath, task.CampBase); err == nil && !ahead {
			return false
		}
	}
	return true
}

// Run polls at the given interval, logging each tick's outcome to stdout,
// until retire reports a reason to stop (or forever when retire is nil). It
// is checked after every pass, never in the middle of one, so a pass that
// started is always finished before Run returns the reason. Meant to run in
// the foreground of a long-lived background process the commander starts once
// per machine.
func Run(vexillumHome string, client herdr.Client, interval time.Duration, stdout io.Writer, retire func() string) (reason string) {
	for {
		woke, err := Tick(vexillumHome, client)
		switch {
		case err != nil:
			fmt.Fprintf(stdout, "sentinel: tick error: %v\n", err)
		case woke > 0:
			fmt.Fprintf(stdout, "sentinel: recorded %d wake(s)\n", woke)
		}
		if retire != nil {
			if reason := retire(); reason != "" {
				return reason
			}
		}
		time.Sleep(interval)
	}
}
