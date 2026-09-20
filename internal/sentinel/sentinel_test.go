package sentinel_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/isaias-alt/vexillum/internal/sentinel"
	"github.com/isaias-alt/vexillum/internal/state"
)

// fakeHerdr implements just enough of herdr.Client for these tests
// (AgentStatus is all Tick uses); the rest are no-ops.
type fakeHerdr struct {
	statuses   map[string]string
	err        error
	readOutput string
}

func (f *fakeHerdr) CreateTab(workspaceID, cwd, label string) (string, string, error) {
	return "", "", nil
}
func (f *fakeHerdr) AgentStart(name, kind, paneID string, agentArgs ...string) error { return nil }
func (f *fakeHerdr) AgentSendKeys(name string, keys ...string) error                 { return nil }
func (f *fakeHerdr) AgentReady(name string) (bool, error)                            { return true, nil }
func (f *fakeHerdr) AgentStatus(name string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.statuses[name], nil
}
func (f *fakeHerdr) AgentPrompt(name, text string, timeoutMS int) (string, error) { return "", nil }
func (f *fakeHerdr) AgentRead(name string, lines int) (string, error)             { return f.readOutput, nil }
func (f *fakeHerdr) TabClose(tabID string) error                                  { return nil }

func newRunningTask(t *testing.T, home, agentName string) state.Task {
	t.Helper()
	task, err := state.New(state.KindMission, "do the thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.Status = state.StatusRunning
	task.HerdrAgentName = agentName
	if err := state.Save(home, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	return task
}

// A running task whose live herdr status settled to done gets its
// persisted status updated and a wake recorded.
func TestTick_RecordsWakeOnTransition(t *testing.T) {
	home := t.TempDir()
	task := newRunningTask(t, home, "vx-do-the-thing")

	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "done"}}

	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if woke != 1 {
		t.Fatalf("expected 1 wake, got %d", woke)
	}

	persisted, err := state.Load(home, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Status != state.StatusDone {
		t.Errorf("expected persisted status done, got %s", persisted.Status)
	}

	wakes, err := sentinel.Drain(home)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(wakes) != 1 || wakes[0].TaskID != task.ID || wakes[0].NewStatus != state.StatusDone {
		t.Errorf("expected one drained wake for %s -> done, got %+v", task.ID, wakes)
	}
}

// A task that settles captures its final transcript, since dispatch's
// own quick-settle probe (internal/soldier.RunInHerdr) usually returns
// long before real work finishes - the sentinel is now the one that
// reads it.
func TestTick_CapturesOutputOnTransition(t *testing.T) {
	home := t.TempDir()
	task := newRunningTask(t, home, "vx-do-the-thing")

	client := &fakeHerdr{
		statuses:   map[string]string{"vx-do-the-thing": "done"},
		readOutput: "soldier: all done, opened PR #4",
	}

	if _, err := sentinel.Tick(home, client); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	persisted, err := state.Load(home, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Output != "soldier: all done, opened PR #4" {
		t.Errorf("expected captured output, got %q", persisted.Output)
	}
}

// A task whose live status hasn't changed produces no wake.
func TestTick_NoWakeWithoutTransition(t *testing.T) {
	home := t.TempDir()
	newRunningTask(t, home, "vx-do-the-thing")

	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "working"}}

	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if woke != 0 {
		t.Errorf("expected 0 wakes for an unchanged status, got %d", woke)
	}
}

// Drain only returns a wake once - a second drain with nothing new
// yields nothing.
func TestDrain_OnlySurfacesEachWakeOnce(t *testing.T) {
	home := t.TempDir()
	newRunningTask(t, home, "vx-do-the-thing")
	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "done"}}

	if _, err := sentinel.Tick(home, client); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	first, err := sentinel.Drain(home)
	if err != nil {
		t.Fatalf("Drain 1: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("expected 1 wake on first drain, got %d", len(first))
	}

	second, err := sentinel.Drain(home)
	if err != nil {
		t.Fatalf("Drain 2: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("expected 0 wakes on second drain, got %d", len(second))
	}
}

// Tasks that aren't running (pending/done/failed/blocked already) are
// never polled - only running tasks are actionable for the sentinel.
func TestTick_IgnoresNonRunningTasks(t *testing.T) {
	home := t.TempDir()
	task, err := state.New(state.KindScout, "look into it")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.Status = state.StatusDone
	task.HerdrAgentName = "vx-look-into-it"
	if err := state.Save(home, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	client := &fakeHerdr{statuses: map[string]string{"vx-look-into-it": "blocked"}}
	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if woke != 0 {
		t.Errorf("expected 0 wakes for a non-running task, got %d", woke)
	}
}

// A transient AgentStatus read failure on one task doesn't fail the
// whole tick.
func TestTick_ToleratesTransientReadFailure(t *testing.T) {
	home := t.TempDir()
	newRunningTask(t, home, "vx-do-the-thing")

	client := &fakeHerdr{err: errFake{}}
	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick should not fail on a transient read error: %v", err)
	}
	if woke != 0 {
		t.Errorf("expected 0 wakes, got %d", woke)
	}
}

type errFake struct{}

func (errFake) Error() string { return "transient herdr read failure" }

// Sanity: Drain on a project with no wakes yet returns an empty slice,
// not an error.
func TestDrain_EmptyWhenNoWakesDir(t *testing.T) {
	home := t.TempDir()
	wakes, err := sentinel.Drain(home)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(wakes) != 0 {
		t.Errorf("expected no wakes, got %d", len(wakes))
	}
}

// AcquireLock refuses a second lock while this process (a real, live
// pid - our own) holds it, and the released lock can be re-acquired.
func TestAcquireLock_RefusesSecondWhileHeld(t *testing.T) {
	home := t.TempDir()

	release, err := sentinel.AcquireLock(home)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}

	if _, err := sentinel.AcquireLock(home); err == nil {
		t.Fatal("expected a second AcquireLock to refuse while the first is held")
	}

	release()

	release2, err := sentinel.AcquireLock(home)
	if err != nil {
		t.Fatalf("AcquireLock after release: %v", err)
	}
	release2()
}

// A lock file left behind by a process that's no longer running (a
// stale pid) is reclaimed instead of refusing forever.
func TestAcquireLock_ReclaimsStaleLock(t *testing.T) {
	home := t.TempDir()

	// A pid essentially guaranteed to be dead/unowned.
	const deadPID = 999999
	if err := os.WriteFile(filepath.Join(home, "sentinel.pid"), []byte(strconv.Itoa(deadPID)), 0o644); err != nil {
		t.Fatalf("writing stale lock: %v", err)
	}

	release, err := sentinel.AcquireLock(home)
	if err != nil {
		t.Fatalf("expected AcquireLock to reclaim a stale lock, got: %v", err)
	}
	release()
}

// IsRunning reflects a live lock without claiming it itself - dispatch
// uses this to decide whether to auto-start a sentinel.
func TestIsRunning(t *testing.T) {
	home := t.TempDir()

	if sentinel.IsRunning(home) {
		t.Error("expected IsRunning to be false with no lock file at all")
	}

	release, err := sentinel.AcquireLock(home)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if !sentinel.IsRunning(home) {
		t.Error("expected IsRunning to be true while this process holds the lock")
	}

	release()
	if sentinel.IsRunning(home) {
		t.Error("expected IsRunning to be false after the lock was released")
	}
}

// A stale lock file (dead pid) reads as not running, same as
// AcquireLock's own reclaim logic.
func TestIsRunning_FalseForStaleLock(t *testing.T) {
	home := t.TempDir()
	const deadPID = 999999
	if err := os.WriteFile(filepath.Join(home, "sentinel.pid"), []byte(strconv.Itoa(deadPID)), 0o644); err != nil {
		t.Fatalf("writing stale lock: %v", err)
	}

	if sentinel.IsRunning(home) {
		t.Error("expected IsRunning to be false for a stale pid")
	}
}
