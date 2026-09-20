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

// Tick checks every task currently marked running against its live herdr
// agent status, persists any status change, and records a wake for it.
// Returns how many wakes it recorded. A transient read failure on one
// task is skipped, not fatal - there's always a next tick.
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

		live, err := client.AgentStatus(task.HerdrAgentName)
		if err != nil || live == "" || live == "unknown" {
			continue
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
