package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/sentinel"
	"github.com/isaias-alt/vexillum/internal/state"
)

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
