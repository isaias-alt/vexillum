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
		// newSettledMissionTask leases a camp; a struck one has no slot.
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

// A shipped soldier waits open until its PR merges, so follow-up work is a
// prompt away: the task goes running with the text recorded as an amendment,
// and when the soldier settles it is done again, not shipped, with exactly one
// wake for that transition.
func TestReprompt_ShippedTaskGoesRunningThenDoneWithOneWake(t *testing.T) {
	project, home, projectRoot, task := repromptFixture(t, state.StatusShipped, true)

	client := &fakeHerdr{promptStatus: "done", promptErr: &herdr.APIError{Code: "timeout", Message: "still working"}}
	var out, errOut bytes.Buffer
	if code := runReprompt(project, home, task.ID, "address the review comments", client, &out, &errOut); code != 0 {
		t.Fatalf("expected success, got %d: %s%s", code, out.String(), errOut.String())
	}
	if len(client.promptCalls) != 1 || client.promptCalls[0] != "address the review comments" {
		t.Errorf("expected the text delivered verbatim, got %v", client.promptCalls)
	}
	got, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.StatusRunning {
		t.Fatalf("expected the shipped task running, got %s", got.Status)
	}
	if len(got.Amendments) != 1 || got.Amendments[0].Text != "address the review comments" || got.Amendments[0].Source != state.AmendmentSourcePrompt {
		t.Fatalf("expected the prompt recorded as an amendment, got %+v", got.Amendments)
	}

	// Past the grace period the soldier is seen settled: running -> done,
	// one wake, and no wake for anything else on later ticks.
	got.UpdatedAt = time.Now().Add(-time.Minute)
	if err := state.Save(projectRoot, got); err != nil {
		t.Fatal(err)
	}
	for i, wantWoke := range []int{1, 0} {
		woke, err := sentinel.Tick(home, client)
		if err != nil || woke != wantWoke {
			t.Fatalf("tick %d: expected woke=%d, got woke=%d err=%v", i+1, wantWoke, woke, err)
		}
	}
	wakes, err := sentinel.Drain(projectRoot)
	if err != nil || len(wakes) != 1 || wakes[0].OldStatus != state.StatusRunning || wakes[0].NewStatus != state.StatusDone {
		t.Fatalf("expected one running -> done wake, got %+v err=%v", wakes, err)
	}
	settled, _ := state.Load(projectRoot, task.ID)
	if settled.Status != state.StatusDone {
		t.Errorf("expected done after the follow-up (not shipped: the PR lacks the new commits), got %s", settled.Status)
	}
}

func TestReprompt_ShippedTaskFastSettleIsDone(t *testing.T) {
	project, home, projectRoot, task := repromptFixture(t, state.StatusShipped, true)

	client := &fakeHerdr{promptStatus: "done", readOutput: "all done"}
	var out, errOut bytes.Buffer
	if code := runReprompt(project, home, task.ID, "tweak", client, &out, &errOut); code != 0 {
		t.Fatalf("expected success, got %d: %s%s", code, out.String(), errOut.String())
	}
	got, _ := state.Load(projectRoot, task.ID)
	if got.Status != state.StatusDone {
		t.Errorf("expected done, got %s", got.Status)
	}
}

// A follow-up that never reached the soldier leaves the shipped task shipped.
func TestReprompt_ShippedTaskFailedDeliveryStaysShipped(t *testing.T) {
	project, home, projectRoot, task := repromptFixture(t, state.StatusShipped, true)

	client := &fakeHerdr{promptErr: &herdr.APIError{Code: "agent_not_running", Message: "pane closed"}}
	var out, errOut bytes.Buffer
	if code := runReprompt(project, home, task.ID, "tweak", client, &out, &errOut); code == 0 {
		t.Fatal("expected a failure")
	}
	got, _ := state.Load(projectRoot, task.ID)
	if got.Status != state.StatusShipped || len(got.Amendments) != 0 {
		t.Errorf("expected shipped with no amendment, got %s %+v", got.Status, got.Amendments)
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
		{state.StatusFailed, "only a done, shipped or unconfirmed"},
		{state.StatusPending, "only a done, shipped or unconfirmed"},
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

func TestReprompt_RefusesAStruckCamp(t *testing.T) {
	project, home, projectRoot, task := repromptFixture(t, state.StatusDone, false)

	client := &fakeHerdr{}
	var out, errOut bytes.Buffer
	if code := runReprompt(project, home, task.ID, "x", client, &out, &errOut); code == 0 {
		t.Fatal("expected a non-zero exit")
	}
	if !strings.Contains(errOut.String(), "struck") {
		t.Errorf("expected the refusal to say the camp was struck, got: %s", errOut.String())
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

// vx prompt records the text as an amendment on the task, with its source
// and a timestamp, and leaves the dispatch prompt alone. The amendment lives
// in the task's state file only, never in a file inside the camp the soldier
// can write to.
func TestReprompt_RecordsAnAmendmentOutsideTheCamp(t *testing.T) {
	project, home, projectRoot, task := repromptFixture(t, state.StatusDone, true)

	client := &fakeHerdr{promptStatus: "done", readOutput: "all done"}
	var out, errOut bytes.Buffer
	text := "also restyle the banner AMENDMENT_MARKER"
	if code := runReprompt(project, home, task.ID, text, client, &out, &errOut); code != 0 {
		t.Fatalf("expected success, got %d: %s%s", code, out.String(), errOut.String())
	}

	got, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Prompt != task.Prompt {
		t.Errorf("the dispatch prompt must stay untouched, got %q", got.Prompt)
	}
	if len(got.Amendments) != 1 || got.Amendments[0].Text != text || got.Amendments[0].Source != state.AmendmentSourcePrompt || got.Amendments[0].At.IsZero() {
		t.Fatalf("expected one vx prompt amendment, got %+v", got.Amendments)
	}
	if !strings.Contains(got.Intent(), text) {
		t.Errorf("expected the intent to include the amendment, got %q", got.Intent())
	}

	// Nothing under the camp carries the amendment.
	err = filepath.WalkDir(task.CampPath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if data, readErr := os.ReadFile(path); readErr == nil && strings.Contains(string(data), "AMENDMENT_MARKER") {
			t.Errorf("amendment text leaked into a camp file: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A prompt that never reached the soldier is not an instruction it received,
// so it leaves no amendment.
func TestReprompt_FailedDeliveryRecordsNoAmendment(t *testing.T) {
	project, home, projectRoot, task := repromptFixture(t, state.StatusDone, true)

	client := &fakeHerdr{promptErr: &herdr.APIError{Code: "agent_not_running", Message: "pane closed"}}
	var out, errOut bytes.Buffer
	if code := runReprompt(project, home, task.ID, "do more", client, &out, &errOut); code == 0 {
		t.Fatal("expected a failure")
	}
	got, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Amendments) != 0 {
		t.Errorf("expected no amendment for an undelivered prompt, got %+v", got.Amendments)
	}
}

// Amendments accumulate across prompts, in the order given.
func TestReprompt_AmendmentsAccumulateInOrder(t *testing.T) {
	project, home, projectRoot, task := repromptFixture(t, state.StatusDone, true)

	for _, text := range []string{"first extra", "second extra"} {
		client := &fakeHerdr{promptStatus: "done", readOutput: "all done"}
		var out, errOut bytes.Buffer
		if code := runReprompt(project, home, task.ID, text, client, &out, &errOut); code != 0 {
			t.Fatalf("expected success, got %d: %s%s", code, out.String(), errOut.String())
		}
	}
	got, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Amendments) != 2 || got.Amendments[0].Text != "first extra" || got.Amendments[1].Text != "second extra" {
		t.Errorf("expected both amendments in order, got %+v", got.Amendments)
	}
}
