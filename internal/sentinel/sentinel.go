// Package sentinel implements vexillum's supervision loop (Capa 4, paso
// 2): it polls herdr for status changes on tasks vexillum is tracking,
// persists any transition, and records a durable, ack-based wake so the
// commander's Stop hook can surface it and keep working instead of
// quietly ending its turn.
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
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

// Wake is a durable, ack-based record that a tracked task's status
// changed - the sentinel's equivalent of firstmate's wake queue.
type Wake struct {
	TaskID     string       `json:"task_id"`
	Kind       state.Kind   `json:"kind"`
	OldStatus  state.Status `json:"old_status"`
	NewStatus  state.Status `json:"new_status"`
	DetectedAt time.Time    `json:"detected_at"`
	Acked      bool         `json:"acked"`
}

func wakesDir(vexillumHome string) string {
	return filepath.Join(vexillumHome, "wakes")
}

func wakePath(vexillumHome, taskID string) string {
	return filepath.Join(wakesDir(vexillumHome), taskID+".json")
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

// Tick checks every task currently marked running against its live herdr
// agent status, persists any status change, and records a wake for it.
// Returns how many wakes it recorded. A transient read failure on one
// task is skipped, not fatal - there's always a next tick. An agent
// confirmed genuinely gone (not just transiently unreachable) instead
// gets marked interrupted - see notFoundConfirmWindow.
func Tick(vexillumHome string, client herdr.Client) (int, error) {
	tasks, err := state.List(vexillumHome)
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
				interrupted, ierr := handleAgentNotFound(vexillumHome, task)
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
			if err := state.Save(vexillumHome, task); err != nil {
				return woke, fmt.Errorf("clearing not-found mark for task %s: %w", task.ID, err)
			}
		}

		newStatus := soldier.MapAgentStatus(live)
		if newStatus == task.Status {
			continue
		}

		old := task.Status
		task.Status = newStatus
		task.UpdatedAt = time.Now().UTC()
		// Every transition reaching here settles a Running task into a
		// terminal one (MapAgentStatus never re-maps live status back to
		// Running once it's left it) - capture its final transcript now,
		// since dispatch's own quick-settle probe usually returned long
		// before this point.
		if output, err := client.AgentRead(task.HerdrAgentName, tickReadLines); err == nil {
			task.Output = output
		}
		if err := state.Save(vexillumHome, task); err != nil {
			return woke, fmt.Errorf("persisting task %s: %w", task.ID, err)
		}
		if err := recordWake(vexillumHome, task, old, newStatus); err != nil {
			return woke, fmt.Errorf("recording wake for task %s: %w", task.ID, err)
		}
		woke++
	}
	return woke, nil
}

// handleAgentNotFound records the first time task's agent was observed
// genuinely gone, and marks the task Interrupted once that's held true
// for notFoundConfirmWindow. Returns whether it interrupted the task
// (and so recorded a wake) on this call.
func handleAgentNotFound(vexillumHome string, task state.Task) (interrupted bool, err error) {
	if task.AgentNotFoundSince.IsZero() {
		task.AgentNotFoundSince = time.Now().UTC()
		if err := state.Save(vexillumHome, task); err != nil {
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
	if err := state.Save(vexillumHome, task); err != nil {
		return false, fmt.Errorf("persisting interrupted task %s: %w", task.ID, err)
	}
	if err := recordWake(vexillumHome, task, old, state.StatusInterrupted); err != nil {
		return false, fmt.Errorf("recording wake for interrupted task %s: %w", task.ID, err)
	}
	return true, nil
}

func recordWake(vexillumHome string, task state.Task, old, newStatus state.Status) error {
	dir := wakesDir(vexillumHome)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	w := Wake{
		TaskID:     task.ID,
		Kind:       task.Kind,
		OldStatus:  old,
		NewStatus:  newStatus,
		DetectedAt: time.Now().UTC(),
		Acked:      false,
	}
	return atomicfile.WriteJSON(wakePath(vexillumHome, task.ID), w)
}

// Drain returns every unacknowledged wake, oldest first, and marks them
// acknowledged - a wake is surfaced once, not repeated on every drain.
func Drain(vexillumHome string) ([]Wake, error) {
	dir := wakesDir(vexillumHome)
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
		if err := json.Unmarshal(data, &w); err != nil || w.Acked {
			continue
		}
		drained = append(drained, w)
		w.Acked = true
		if err := atomicfile.WriteJSON(path, w); err != nil {
			return nil, fmt.Errorf("acknowledging wake for task %s: %w", w.TaskID, err)
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
func AcquireLock(vexillumHome string) (release func(), err error) {
	path := lockPath(vexillumHome)

	if data, readErr := os.ReadFile(path); readErr == nil {
		if pid, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr == nil && processAlive(pid) {
			return nil, fmt.Errorf("a sentinel is already running (pid %d) for %s - not starting a second one", pid, vexillumHome)
		}
	}

	if err := os.MkdirAll(vexillumHome, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return nil, err
	}
	return func() { _ = os.Remove(path) }, nil
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
