package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/herdr"
	vxproject "github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/sentinel"
	"github.com/isaias-alt/vexillum/internal/state"
)

// goneHerdr answers every agent lookup the way herdr does once a pane was
// closed by a strike.
type goneHerdr struct{ fakeHerdr }

func (g *goneHerdr) AgentStatus(name string) (string, error) {
	return "", &herdr.APIError{Code: "agent_not_found", Message: "agent target " + name + " not found"}
}

// A mission that was settled, asked for a rebase (so the sentinel or vx
// prompt put it back to running) and landed before it settled again must end
// as done, and the sentinel must not turn it interrupted once its pane is
// gone. Before the fix the record stayed running.
func TestRunLand_RecordsARunningMissionAsDoneAndTheSentinelKeepsIt(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatal(err)
	}

	task.Status = state.StatusRunning
	task.HerdrAgentName = "vx-do-a-thing"
	task.HerdrTabID = "T1"
	task.UpdatedAt = time.Now().Add(-time.Minute)
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatal(err)
	}

	client := &goneHerdr{}
	var stdout, stderr bytes.Buffer
	if code := runLand(project, home, task.ID, client, &stdout, &stderr); code != 0 {
		t.Fatalf("land failed (%d): %s%s", code, stdout.String(), stderr.String())
	}
	if !client.tabClosed {
		t.Fatalf("expected the soldier's tab to be closed by the strike")
	}

	got, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.StatusDone {
		t.Fatalf("expected the landed mission recorded as done, got %s", got.Status)
	}

	// Two sentinel passes past the not-found confirm window: nothing to see.
	for i := 0; i < 2; i++ {
		if woke, err := sentinel.Tick(home, client); err != nil || woke != 0 {
			t.Fatalf("Tick %d: woke=%d err=%v", i, woke, err)
		}
	}
	got, err = state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.StatusDone {
		t.Fatalf("expected the sentinel to leave the landed mission done, got %s", got.Status)
	}
	if !got.AgentNotFoundSince.IsZero() {
		t.Errorf("expected no not-found mark on a landed mission, got %s", got.AgentNotFoundSince)
	}
}

// The record is also settled when the automatic strike fails: the merge
// stands, so the mission landed. land still exits non-zero for the strike.
func TestRunLand_RecordsTheMissionDoneEvenWhenTheStrikeFails(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatal(err)
	}
	task.Status = state.StatusBlocked
	task.HerdrTabID = "T1"
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatal(err)
	}

	client := &fakeHerdr{tabCloseErr: &herdr.APIError{Code: "boom", Message: "cannot close"}}
	var stdout, stderr bytes.Buffer
	code := runLand(project, home, task.ID, client, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "automatic strike failed") {
		t.Fatalf("expected a failed strike to be reported, got %d: %s", code, stderr.String())
	}

	got, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.StatusDone {
		t.Fatalf("expected the landed mission recorded as done, got %s", got.Status)
	}
}

// A task that already rests in a terminal state keeps it: land only closes
// the open ones.
func TestRunLand_LeavesATerminalRecordAlone(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatal(err)
	}
	updatedAt := task.UpdatedAt

	var stdout, stderr bytes.Buffer
	if code := runLand(project, home, task.ID, &fakeHerdr{}, &stdout, &stderr); code != 0 {
		t.Fatalf("land failed (%d): %s%s", code, stdout.String(), stderr.String())
	}
	got, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.StatusDone || !got.UpdatedAt.Equal(updatedAt) {
		t.Errorf("expected the done record untouched, got %s updated %s (was %s)", got.Status, got.UpdatedAt, updatedAt)
	}
}
