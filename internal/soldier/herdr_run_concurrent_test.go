package soldier_test

import (
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

// racingHerdr runs a callback inside a herdr call, as another vx command or
// the sentinel does while a slow call is in flight: the record on disk
// changes after the caller loaded its copy and before it writes.
type racingHerdr struct {
	fakeHerdr
	onPrompt func()
	onStatus func()
}

func (r *racingHerdr) AgentPrompt(name, text string, until []string, timeoutMS int) (string, error) {
	if r.onPrompt != nil {
		during := r.onPrompt
		r.onPrompt = nil
		during()
	}
	return r.fakeHerdr.AgentPrompt(name, text, until, timeoutMS)
}

func (r *racingHerdr) AgentStatus(name string) (string, error) {
	if r.onStatus != nil {
		during := r.onStatus
		r.onStatus = nil
		during()
	}
	return r.fakeHerdr.AgentStatus(name)
}

const (
	racedSHA         = "feedface"
	racedInstruction = "also update the docs"
)

// raceWrite is the write a concurrent command makes: a field the writer under
// test does not own (a pushed commit) and a later instruction.
func raceWrite(t *testing.T, projectRoot, id string) {
	t.Helper()
	if _, err := state.Update(projectRoot, id, func(task *state.Task) (bool, error) {
		task.LastPushedSHA = racedSHA
		task.AddAmendment(state.AmendmentSourcePrompt, racedInstruction, time.Now())
		return true, nil
	}); err != nil {
		t.Errorf("concurrent state.Update: %v", err)
	}
}

// assertRaceKept fails unless the record on disk still has raceWrite's fields.
func assertRaceKept(t *testing.T, projectRoot, id string) state.Task {
	t.Helper()
	persisted, err := state.Load(projectRoot, id)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.LastPushedSHA != racedSHA {
		t.Errorf("expected the concurrent write to LastPushedSHA kept, got %q", persisted.LastPushedSHA)
	}
	found := false
	for _, a := range persisted.Amendments {
		if a.Text == racedInstruction {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the concurrent amendment kept, got %+v", persisted.Amendments)
	}
	return persisted
}

func savedDoneTask(t *testing.T, projectRoot string) state.Task {
	t.Helper()
	task := newMissionTask(t)
	task.Status = state.StatusDone
	task.HerdrAgentName = "vx-do-the-thing"
	task.UpdatedAt = time.Now().Add(-time.Hour).UTC()
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	return task
}

func TestRunInHerdr_KeepsAWriteMadeWhileTheSoldierSettles(t *testing.T) {
	home := t.TempDir()
	projectRoot := testProjectRoot(t, home)
	task := newMissionTask(t)
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}
	client := &racingHerdr{fakeHerdr: fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptStatus: "done", readOutput: "transcript"}}
	client.onPrompt = func() { raceWrite(t, projectRoot, task.ID) }

	got, err := runInHerdr(t, home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if got.Status != state.StatusDone {
		t.Errorf("expected done, got %s", got.Status)
	}
	persisted := assertRaceKept(t, projectRoot, task.ID)
	if persisted.Status != state.StatusDone || persisted.Output != "transcript" || persisted.HerdrTabID != "w1:t2" {
		t.Errorf("expected the settle recorded next to the concurrent write, got %+v", persisted)
	}
}

func TestRunInHerdr_FailureKeepsAWriteMadeWhileTheSoldierWasPrompted(t *testing.T) {
	home := t.TempDir()
	projectRoot := testProjectRoot(t, home)
	task := newMissionTask(t)
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}
	client := &racingHerdr{fakeHerdr: fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptErr: &herdr.APIError{Code: "boom", Message: "no"}}}
	client.onPrompt = func() { raceWrite(t, projectRoot, task.ID) }

	if _, err := runInHerdr(t, home, "w1", task, c, client); err == nil {
		t.Fatal("expected the prompt error")
	}
	persisted := assertRaceKept(t, projectRoot, task.ID)
	if persisted.Status != state.StatusFailed {
		t.Errorf("expected failed, got %s", persisted.Status)
	}
}

func TestAnswerBlocked_KeepsAWriteMadeWhileTheAnswerIsDelivered(t *testing.T) {
	home := t.TempDir()
	projectRoot := testProjectRoot(t, home)
	task := newBlockedTask(t, projectRoot, "vx-refactor-the-auth-module")
	client := &racingHerdr{fakeHerdr: fakeHerdr{promptStatus: "done", readOutput: "used Auth0, done"}}
	client.onPrompt = func() { raceWrite(t, projectRoot, task.ID) }

	if _, err := soldier.AnswerBlocked(projectRoot, task, "Use Auth0", client); err != nil {
		t.Fatalf("AnswerBlocked: %v", err)
	}
	persisted := assertRaceKept(t, projectRoot, task.ID)
	if persisted.Status != state.StatusDone || persisted.Decision == nil || persisted.Decision.Answer != "Use Auth0" {
		t.Errorf("expected the answer recorded next to the concurrent write, got %+v", persisted)
	}
	var decide int
	for _, a := range persisted.Amendments {
		if a.Source == state.AmendmentSourceDecide {
			decide++
		}
	}
	if decide != 1 || len(persisted.Amendments) != 2 {
		t.Errorf("expected the decide amendment next to the concurrent one, got %+v", persisted.Amendments)
	}
}

// The transcript a concurrent writer refreshed is the one the interrupted
// note is appended to, not the stale copy the caller loaded.
func TestAnswerBlocked_PaneGoneAppendsToTheTranscriptOnDisk(t *testing.T) {
	home := t.TempDir()
	projectRoot := testProjectRoot(t, home)
	task := newBlockedTask(t, projectRoot, "vx-refactor-the-auth-module")
	client := &racingHerdr{fakeHerdr: fakeHerdr{promptErr: &herdr.APIError{Code: "agent_not_running", Message: "pane closed"}}}
	client.onPrompt = func() {
		raceWrite(t, projectRoot, task.ID)
		if _, err := state.Update(projectRoot, task.ID, func(current *state.Task) (bool, error) {
			current.Output = "fresher transcript"
			return true, nil
		}); err != nil {
			t.Errorf("state.Update: %v", err)
		}
	}

	if _, err := soldier.AnswerBlocked(projectRoot, task, "Use Auth0", client); err == nil {
		t.Fatal("expected an error when the pane is gone")
	}
	persisted := assertRaceKept(t, projectRoot, task.ID)
	if persisted.Status != state.StatusInterrupted || !strings.HasPrefix(persisted.Output, "fresher transcript\n\n[vexillum]") {
		t.Errorf("expected interrupted with the note after the fresher transcript, got %s %q", persisted.Status, persisted.Output)
	}
}

func TestDismissBlocked_KeepsAWriteMadeWhileThePaneIsChecked(t *testing.T) {
	home := t.TempDir()
	projectRoot := testProjectRoot(t, home)
	task := dismissibleTask(t, projectRoot)
	client := &racingHerdr{fakeHerdr: fakeHerdr{promptStatus: "working"}}
	client.onStatus = func() { raceWrite(t, projectRoot, task.ID) }

	if _, err := soldier.DismissBlocked(projectRoot, task, client); err != nil {
		t.Fatalf("DismissBlocked: %v", err)
	}
	persisted := assertRaceKept(t, projectRoot, task.ID)
	if persisted.Status != state.StatusRunning || persisted.Decision == nil || !persisted.Decision.Dismissed {
		t.Errorf("expected the dismissal recorded next to the concurrent write, got %+v", persisted)
	}
}

func TestReprompt_KeepsAWriteMadeWhileThePromptIsDelivered(t *testing.T) {
	home := t.TempDir()
	projectRoot := testProjectRoot(t, home)
	task := savedDoneTask(t, projectRoot)
	client := &racingHerdr{fakeHerdr: fakeHerdr{promptStatus: "done", readOutput: "rebased"}}
	client.onPrompt = func() { raceWrite(t, projectRoot, task.ID) }

	if _, err := soldier.Reprompt(projectRoot, task, "rebase it", client); err != nil {
		t.Fatalf("Reprompt: %v", err)
	}
	persisted := assertRaceKept(t, projectRoot, task.ID)
	if persisted.Status != state.StatusDone || persisted.Output != "rebased" || len(persisted.Amendments) != 2 {
		t.Errorf("expected the settle and both amendments, got %+v", persisted)
	}
}

// A prompt that cannot be delivered is taken back: no amendment, the earlier
// status and timestamp, and whatever else was written meanwhile stays.
func TestReprompt_FailedPromptRestoresOnlyWhatItWrote(t *testing.T) {
	home := t.TempDir()
	projectRoot := testProjectRoot(t, home)
	task := savedDoneTask(t, projectRoot)
	client := &racingHerdr{fakeHerdr: fakeHerdr{promptErr: &herdr.APIError{Code: "boom", Message: "no"}}}
	client.onPrompt = func() { raceWrite(t, projectRoot, task.ID) }

	if _, err := soldier.Reprompt(projectRoot, task, "rebase it", client); err == nil {
		t.Fatal("expected the prompt error")
	}
	persisted := assertRaceKept(t, projectRoot, task.ID)
	if persisted.Status != state.StatusDone || !persisted.UpdatedAt.Equal(task.UpdatedAt) {
		t.Errorf("expected the status and time restored, got %s %s", persisted.Status, persisted.UpdatedAt)
	}
	if len(persisted.Amendments) != 1 {
		t.Errorf("expected only the concurrent amendment left, got %+v", persisted.Amendments)
	}
}

// When the sentinel has meanwhile found the pane gone and marked the task
// interrupted, restoring the earlier done status would hide that.
func TestReprompt_FailedPromptLeavesWhatTheSentinelRecordedMeanwhile(t *testing.T) {
	home := t.TempDir()
	projectRoot := testProjectRoot(t, home)
	task := savedDoneTask(t, projectRoot)
	client := &racingHerdr{fakeHerdr: fakeHerdr{promptErr: &herdr.APIError{Code: "agent_not_running", Message: "pane closed"}}}
	client.onPrompt = func() {
		if _, err := state.Update(projectRoot, task.ID, func(current *state.Task) (bool, error) {
			current.Status = state.StatusInterrupted
			current.UpdatedAt = time.Now().UTC()
			return true, nil
		}); err != nil {
			t.Errorf("state.Update: %v", err)
		}
	}

	if _, err := soldier.Reprompt(projectRoot, task, "rebase it", client); err == nil {
		t.Fatal("expected an error when the pane is gone")
	}
	persisted, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != state.StatusInterrupted || len(persisted.Amendments) != 0 {
		t.Errorf("expected interrupted and no amendment, got %s %+v", persisted.Status, persisted.Amendments)
	}
}
