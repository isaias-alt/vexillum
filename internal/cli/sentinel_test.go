package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/sentinel"
	"github.com/isaias-alt/vexillum/internal/state"
)

// sentinelMode classifies the three recognized forms and rejects anything
// else - an unrecognized subcommand must not silently fall through to
// starting the infinite polling loop (the bug found via a real commander
// session running "vexillum sentinel status").
func TestSentinelMode(t *testing.T) {
	cases := []struct {
		args    []string
		want    string
		wantErr bool
	}{
		{args: nil, want: "run"},
		{args: []string{}, want: "run"},
		{args: []string{"-h"}, want: "help"},
		{args: []string{"--help"}, want: "help"},
		{args: []string{"drain"}, want: "drain"},
		{args: []string{"await"}, want: "await"},
		{args: []string{"status"}, wantErr: true},
		{args: []string{"drain", "extra"}, want: "drain"},
	}

	for _, c := range cases {
		got, err := sentinelMode(c.args)
		if c.wantErr {
			if err == nil {
				t.Errorf("sentinelMode(%v): expected an error, got mode %q", c.args, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("sentinelMode(%v): unexpected error: %v", c.args, err)
			continue
		}
		if got != c.want {
			t.Errorf("sentinelMode(%v) = %q, want %q", c.args, got, c.want)
		}
	}
}

// No pending wakes -> the empty hook JSON, so a Stop hook lets the turn
// end normally.
func TestRunSentinelDrain_NoWakes(t *testing.T) {
	home := t.TempDir()

	var out bytes.Buffer
	if code := runSentinelDrain(home, &out, &out); code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	if strings.TrimSpace(out.String()) != "{}" {
		t.Errorf("expected {}, got %q", out.String())
	}
}

// A pending wake produces a Stop-hook "block" decision naming the task
// and its transition, so the commander keeps working instead of quietly
// stopping.
func TestRunSentinelDrain_PendingWakeBlocksStop(t *testing.T) {
	home := t.TempDir()

	task, err := state.New(state.KindMission, "do the thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.Status = state.StatusRunning
	task.HerdrAgentName = "vx-do-the-thing"
	// Backdated past sentinel.Tick's settle-race grace period, so this
	// test exercises a real transition rather than the grace period
	// itself (see internal/sentinel.TestTick_SkipsTasksWithinSettleGracePeriod).
	task.UpdatedAt = task.UpdatedAt.Add(-1 * time.Minute)
	if err := state.Save(home, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	// A real sentinel tick is what would have recorded this wake, so
	// exercise it the same way rather than writing the wake file by hand.
	client := &fakeHerdr{promptStatus: "done"}
	if _, err := sentinel.Tick(home, client); err != nil {
		t.Fatalf("sentinel.Tick: %v", err)
	}

	var out bytes.Buffer
	if code := runSentinelDrain(home, &out, &out); code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, `"decision":"block"`) {
		t.Errorf("expected a blocking decision, got: %s", got)
	}
	if !strings.Contains(got, task.ID) {
		t.Errorf("expected the reason to name the task id, got: %s", got)
	}
}

// runSentinelAwait is what the async Stop hook actually calls. A wake
// already pending when it's invoked is found on its very first check -
// exit 2 with the reason on stderr, the block signal a real asyncRewake
// hook uses (verified live against a real Claude Code session).
func TestRunSentinelAwait_FindsAlreadyPendingWake(t *testing.T) {
	home := t.TempDir()

	task, err := state.New(state.KindMission, "do the thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.Status = state.StatusRunning
	task.HerdrAgentName = "vx-do-the-thing"
	task.UpdatedAt = task.UpdatedAt.Add(-1 * time.Minute)
	if err := state.Save(home, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	client := &fakeHerdr{promptStatus: "done"}
	if _, err := sentinel.Tick(home, client); err != nil {
		t.Fatalf("sentinel.Tick: %v", err)
	}

	var stderr bytes.Buffer
	code := runSentinelAwait(home, time.Hour, time.Millisecond, &stderr)
	if code != 2 {
		t.Fatalf("expected exit 2, got %d: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), task.ID) {
		t.Errorf("expected the reason to name the task id, got: %s", stderr.String())
	}
}

// With nothing ever pending, runSentinelAwait exits 0 once maxWait
// elapses - the async hook lets the turn end quietly, same as
// runSentinelDrain's {} for the instant-check case.
func TestRunSentinelAwait_TimesOutWithNothingPending(t *testing.T) {
	home := t.TempDir()

	var stderr bytes.Buffer
	code := runSentinelAwait(home, 20*time.Millisecond, 5*time.Millisecond, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0 on timeout, got %d: %s", code, stderr.String())
	}
	if stderr.String() != "" {
		t.Errorf("expected no output on a silent timeout, got: %s", stderr.String())
	}
}

// A wake that only shows up after a couple of poll cycles is still
// found before maxWait elapses - not just on the very first check.
func TestRunSentinelAwait_FindsWakeThatArrivesMidWait(t *testing.T) {
	home := t.TempDir()

	task, err := state.New(state.KindMission, "do the thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.Status = state.StatusRunning
	task.HerdrAgentName = "vx-do-the-thing"
	task.UpdatedAt = task.UpdatedAt.Add(-1 * time.Minute)
	if err := state.Save(home, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	client := &fakeHerdr{promptStatus: "done"}
	go func() {
		time.Sleep(15 * time.Millisecond)
		_, _ = sentinel.Tick(home, client)
	}()

	var stderr bytes.Buffer
	code := runSentinelAwait(home, time.Second, 5*time.Millisecond, &stderr)
	if code != 2 {
		t.Fatalf("expected exit 2 once the wake appeared, got %d: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), task.ID) {
		t.Errorf("expected the reason to name the task id, got: %s", stderr.String())
	}
}
