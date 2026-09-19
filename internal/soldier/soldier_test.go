package soldier_test

import (
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

func fakeCamp(t *testing.T) camp.Camp {
	t.Helper()
	return camp.Camp{Path: t.TempDir(), Slot: 1, Branch: "vexillum/test"}
}

func newTask(t *testing.T) state.Task {
	t.Helper()
	task, err := state.New(state.KindMission, "say hello")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	return task
}

// L3-02: a successful run captures output and exit code, records the camp
// assignment, and persists the task as done.
func TestRun_Success(t *testing.T) {
	home := t.TempDir()
	c := fakeCamp(t)
	task := newTask(t)

	cmd := soldier.CommandSpec{Command: "sh", Args: []string{"-c", "echo hello-from-soldier"}}
	got, err := soldier.Run(home, task, c, cmd)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got.Status != state.StatusDone {
		t.Errorf("expected status done, got %s", got.Status)
	}
	if got.ExitCode == nil || *got.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %v", got.ExitCode)
	}
	if !strings.Contains(got.Output, "hello-from-soldier") {
		t.Errorf("expected output to contain the command's output, got: %s", got.Output)
	}
	if got.CampPath != c.Path || got.CampSlot != c.Slot || got.CampBranch != c.Branch {
		t.Errorf("expected camp assignment to be recorded on the task, got %+v", got)
	}

	persisted, err := state.Load(home, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Status != state.StatusDone {
		t.Errorf("expected persisted status done, got %s", persisted.Status)
	}
}

// L3-03: a failing process (nonzero exit) is detected and reflected in
// the task's state. Run returns a nil error for it - that's an expected
// soldier outcome, not an infrastructure failure, so it doesn't leave the
// camp "hung": the task's status deterministically becomes failed and
// the camp stays clearly owned by that task in the pool state.
func TestRun_NonZeroExit(t *testing.T) {
	home := t.TempDir()
	c := fakeCamp(t)
	task := newTask(t)

	cmd := soldier.CommandSpec{Command: "sh", Args: []string{"-c", "echo boom >&2; exit 7"}}
	got, err := soldier.Run(home, task, c, cmd)
	if err != nil {
		t.Fatalf("Run returned an error for an expected nonzero exit: %v", err)
	}

	if got.Status != state.StatusFailed {
		t.Errorf("expected status failed, got %s", got.Status)
	}
	if got.ExitCode == nil || *got.ExitCode != 7 {
		t.Errorf("expected exit code 7, got %v", got.ExitCode)
	}
}

// A command that can't even be started (missing binary) is a real
// infrastructure error: reflected on the task as failed, and also
// returned as a Go error since it's not a soldier outcome.
func TestRun_CommandNotFound(t *testing.T) {
	home := t.TempDir()
	c := fakeCamp(t)
	task := newTask(t)

	cmd := soldier.CommandSpec{Command: "this-binary-does-not-exist-anywhere"}
	got, err := soldier.Run(home, task, c, cmd)
	if err == nil {
		t.Fatal("expected an error when the command can't be started")
	}
	if got.Status != state.StatusFailed {
		t.Errorf("expected status failed, got %s", got.Status)
	}
}

// L3-04: the task is persisted as running before the process starts (and
// while it's still in flight), so a vexillum crash mid-run leaves the
// state honestly reflecting that the task never got to report a final
// outcome, instead of it staying silently at "pending" or lying with a
// stale "done".
func TestRun_WriteAheadRunningState(t *testing.T) {
	home := t.TempDir()
	c := fakeCamp(t)
	task := newTask(t)

	done := make(chan struct{})
	go func() {
		defer close(done)
		cmd := soldier.CommandSpec{Command: "sh", Args: []string{"-c", "sleep 0.3"}}
		if _, err := soldier.Run(home, task, c, cmd); err != nil {
			t.Errorf("Run: %v", err)
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	sawRunning := false
	for time.Now().Before(deadline) {
		got, err := state.Load(home, task.ID)
		if err == nil && got.Status == state.StatusRunning {
			sawRunning = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	<-done

	if !sawRunning {
		t.Fatal("expected to observe the task persisted as running while the process was in flight")
	}

	final, err := state.Load(home, task.ID)
	if err != nil {
		t.Fatalf("Load after completion: %v", err)
	}
	if final.Status != state.StatusDone {
		t.Errorf("expected final status done, got %s", final.Status)
	}
}
