package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/herdr"
	vxproject "github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/sentinel"
	"github.com/isaias-alt/vexillum/internal/state"
)

// repromptFixture persists a task in status with a camp lease (or without,
// when leased is false) under an isolated vexillum home.
func repromptFixture(t *testing.T, status state.Status, leased bool) (project, home, projectRoot string, task state.Task) {
	t.Helper()
	project = t.TempDir()
	home = t.TempDir()
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	task = newSettledMissionTask(t, projectRoot)
	task.Status = status
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	if !leased {
		// newSettledMissionTask leases a camp; a released one has no slot.
		if err := os.Remove(filepath.Join(projectRoot, "camps", "pool.json")); err != nil {
			t.Fatal(err)
		}
	}
	return project, home, projectRoot, task
}

// The reported bug end to end: a done soldier is re-prompted with vx
// prompt, its task goes back to running with the text delivered, and when
// it finishes again the sentinel records a wake - a transition it would
// never have seen had the task stayed done.
func TestReprompt_ReopensDoneTaskSoNextFinishWakesTheCommander(t *testing.T) {
	project, home, projectRoot, task := repromptFixture(t, state.StatusDone, true)

	// The soldier is still going past the quick-settle probe.
	client := &fakeHerdr{promptStatus: "done", promptErr: &herdr.APIError{Code: "timeout", Message: "still working"}}
	var out, errOut bytes.Buffer
	if code := runReprompt(project, home, task.ID, "also add a changelog entry", client, &out, &errOut); code != 0 {
		t.Fatalf("expected success, got %d: %s%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "status=running") {
		t.Errorf("expected status=running in the output, got: %s", out.String())
	}
	if len(client.promptCalls) != 1 || client.promptCalls[0] != "also add a changelog entry" || client.promptNames[0] != task.HerdrAgentName {
		t.Errorf("expected the text delivered verbatim to the soldier, got calls=%v names=%v", client.promptCalls, client.promptNames)
	}

	got, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.StatusRunning {
		t.Fatalf("expected the task reopened to running, got %s", got.Status)
	}

	// The soldier finishes. Past the settle grace period, the sentinel
	// sees running -> done and records the wake.
	got.UpdatedAt = time.Now().Add(-time.Minute)
	if err := state.Save(projectRoot, got); err != nil {
		t.Fatal(err)
	}
	woke, err := sentinel.Tick(home, client)
	if err != nil || woke != 1 {
		t.Fatalf("expected the second finish to wake the commander, woke=%d err=%v", woke, err)
	}
	wakes, err := sentinel.Drain(projectRoot)
	if err != nil || len(wakes) != 1 || wakes[0].TaskID != task.ID {
		t.Fatalf("expected one wake for the task, got %+v err=%v", wakes, err)
	}
}

// A re-prompt that settles inside the quick probe reports the settled
// status right away (the commander is the one waiting on this command).
func TestReprompt_FastSettleReportsDone(t *testing.T) {
	project, home, projectRoot, task := repromptFixture(t, state.StatusDone, true)

	client := &fakeHerdr{promptStatus: "done", readOutput: "all done"}
	var out, errOut bytes.Buffer
	if code := runReprompt(project, home, task.ID, "say hi", client, &out, &errOut); code != 0 {
		t.Fatalf("expected success, got %d: %s%s", code, out.String(), errOut.String())
	}
	got, _ := state.Load(projectRoot, task.ID)
	if got.Status != state.StatusDone || got.Output != "all done" {
		t.Errorf("expected done with fresh output, got status=%s output=%q", got.Status, got.Output)
	}
}

func TestReprompt_AcceptsUnconfirmed(t *testing.T) {
	project, home, projectRoot, task := repromptFixture(t, state.StatusUnconfirmed, true)

	client := &fakeHerdr{promptErr: &herdr.APIError{Code: "timeout", Message: "still working"}}
	var out, errOut bytes.Buffer
	if code := runReprompt(project, home, task.ID, "carry on", client, &out, &errOut); code != 0 {
		t.Fatalf("expected success, got %d: %s%s", code, out.String(), errOut.String())
	}
	got, _ := state.Load(projectRoot, task.ID)
	if got.Status != state.StatusRunning {
		t.Errorf("expected running, got %s", got.Status)
	}
}

func TestReprompt_RefusesStatusesThatCannotBePrompted(t *testing.T) {
	cases := []struct {
		status state.Status
		want   string
	}{
		{state.StatusRunning, "already running"},
		{state.StatusBlocked, "--dismiss"},
		{state.StatusInterrupted, "redispatch"},
		{state.StatusFailed, "only a done or unconfirmed"},
		{state.StatusShipped, "only a done or unconfirmed"},
		{state.StatusPending, "only a done or unconfirmed"},
	}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			project, home, projectRoot, task := repromptFixture(t, tc.status, true)

			client := &fakeHerdr{}
			var out, errOut bytes.Buffer
			if code := runReprompt(project, home, task.ID, "x", client, &out, &errOut); code == 0 {
				t.Fatal("expected a non-zero exit")
			}
			if !strings.Contains(errOut.String(), tc.want) {
				t.Errorf("expected the refusal to mention %q, got: %s", tc.want, errOut.String())
			}
			if len(client.promptCalls) != 0 {
				t.Errorf("nothing must be delivered to the soldier, got %v", client.promptCalls)
			}
			got, _ := state.Load(projectRoot, task.ID)
			if got.Status != tc.status {
				t.Errorf("expected status unchanged (%s), got %s", tc.status, got.Status)
			}
		})
	}
}

func TestReprompt_RefusesAReleasedCamp(t *testing.T) {
	project, home, projectRoot, task := repromptFixture(t, state.StatusDone, false)

	client := &fakeHerdr{}
	var out, errOut bytes.Buffer
	if code := runReprompt(project, home, task.ID, "x", client, &out, &errOut); code == 0 {
		t.Fatal("expected a non-zero exit")
	}
	if !strings.Contains(errOut.String(), "released") {
		t.Errorf("expected the refusal to say the camp was released, got: %s", errOut.String())
	}
	if len(client.promptCalls) != 0 {
		t.Errorf("nothing must be delivered, got %v", client.promptCalls)
	}
	got, _ := state.Load(projectRoot, task.ID)
	if got.Status != state.StatusDone {
		t.Errorf("expected done untouched, got %s", got.Status)
	}
}

// A prompt that cannot be delivered must leave the task exactly as it was,
// not stranded as running with nothing working on it.
func TestReprompt_FailedDeliveryRestoresTheTask(t *testing.T) {
	cases := map[string]error{
		"pane gone": &herdr.APIError{Code: "agent_not_running", Message: "pane closed"},
		"other":     &herdr.APIError{Code: "boom", Message: "something else"},
	}
	for name, promptErr := range cases {
		t.Run(name, func(t *testing.T) {
			project, home, projectRoot, task := repromptFixture(t, state.StatusDone, true)
			before, _ := state.Load(projectRoot, task.ID)

			client := &fakeHerdr{promptErr: promptErr}
			var out, errOut bytes.Buffer
			if code := runReprompt(project, home, task.ID, "x", client, &out, &errOut); code == 0 {
				t.Fatal("expected a non-zero exit")
			}
			got, _ := state.Load(projectRoot, task.ID)
			if got.Status != state.StatusDone || !got.UpdatedAt.Equal(before.UpdatedAt) {
				t.Errorf("expected the task restored (done, same UpdatedAt), got status=%s updated=%v want %v", got.Status, got.UpdatedAt, before.UpdatedAt)
			}
		})
	}
}

func TestReprompt_ValidatesInput(t *testing.T) {
	project, home, _, task := repromptFixture(t, state.StatusDone, true)
	var out, errOut bytes.Buffer
	if code := runReprompt(project, home, "../etc", "x", &fakeHerdr{}, &out, &errOut); code == 0 {
		t.Error("expected an invalid task id to be rejected")
	}
	if code := runReprompt(project, home, task.ID, "   ", &fakeHerdr{}, &out, &errOut); code == 0 {
		t.Error("expected empty text to be rejected")
	}
	if code := runReprompt(project, home, "0123456789abcdef", "x", &fakeHerdr{}, &out, &errOut); code == 0 {
		t.Error("expected an unknown task to be rejected")
	}
}
