package sentinel_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/piask"
	"github.com/isaias-alt/vexillum/internal/sentinel"
	"github.com/isaias-alt/vexillum/internal/state"
)

const askQuestionID = "0123456789abcdef0123456789abcdef"

// leaveQuestion records an open ask_user question for task, the way the
// extension in a Pi soldier's session does.
func leaveQuestion(t *testing.T, proj string, task state.Task) {
	t.Helper()
	dir := piask.Dir(proj, task.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"version":1,"id":"` + askQuestionID + `","question":"Which database?","options":["Postgres","SQLite"],"asked_at":"2026-01-02T03:04:05Z"}`
	if err := os.WriteFile(filepath.Join(dir, "question.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func piRunningTask(t *testing.T, harness string) (home, proj string, task state.Task) {
	t.Helper()
	home = t.TempDir()
	proj = projectRoot(home, "proj1")
	task = newRunningTask(t, proj, "vx-do-the-thing")
	task.Harness = harness
	if err := state.Save(proj, task); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return home, proj, task
}

func tickAndLoad(t *testing.T, home, proj string, task state.Task, live string) state.Task {
	t.Helper()
	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": live}, readOutput: "pi is waiting\n"}
	if _, err := sentinel.Tick(home, client); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	got, err := state.Load(proj, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return got
}

// herdr reports a Pi soldier blocked (its Pi integration saw the extension's
// herdr:blocked event): the decision is the question the extension recorded,
// with its options and id, not a guess at the transcript.
func TestTick_PiAskQuestionBecomesTheDecision(t *testing.T) {
	home, proj, task := piRunningTask(t, "pi")
	leaveQuestion(t, proj, task)

	got := tickAndLoad(t, home, proj, task, "blocked")
	if got.Status != state.StatusBlocked {
		t.Fatalf("status = %s, want blocked", got.Status)
	}
	d := got.Decision
	if d == nil || d.Kind != state.DecisionKindAsk || d.QuestionID != askQuestionID || d.Question != "Which database?" || len(d.Options) != 2 || d.Options[1] != "SQLite" {
		t.Fatalf("decision = %+v", d)
	}
	if want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC); !d.AskedAt.Equal(want) {
		t.Errorf("AskedAt = %v, want the time the extension recorded", d.AskedAt)
	}
}

// herdr's own Pi integration is not installed, so it reports the soldier as
// working while it waits in ask_user: the open question alone blocks it.
func TestTick_PiAskQuestionBlocksEvenWhenHerdrSaysWorking(t *testing.T) {
	home, proj, task := piRunningTask(t, "pi")
	leaveQuestion(t, proj, task)

	got := tickAndLoad(t, home, proj, task, "working")
	if got.Status != state.StatusBlocked || got.Decision == nil || got.Decision.Kind != state.DecisionKindAsk {
		t.Fatalf("status %s, decision %+v; want blocked on the ask_user question", got.Status, got.Decision)
	}
}

// No question open: a working Pi soldier stays running.
func TestTick_PiWithoutAQuestionStaysRunning(t *testing.T) {
	home, proj, task := piRunningTask(t, "pi")
	if got := tickAndLoad(t, home, proj, task, "working"); got.Status != state.StatusRunning {
		t.Errorf("status = %s, want running", got.Status)
	}
}

// A question already answered or dismissed does not block the task again
// while its file is still there (the extension has not cleared it yet).
func TestTick_PiAnsweredQuestionDoesNotBlockAgain(t *testing.T) {
	home, proj, task := piRunningTask(t, "pi")
	task.Decision = &state.Decision{Question: "Which database?", Kind: state.DecisionKindAsk, QuestionID: askQuestionID, AnsweredAt: time.Now().UTC()}
	if err := state.Save(proj, task); err != nil {
		t.Fatal(err)
	}
	leaveQuestion(t, proj, task)

	if got := tickAndLoad(t, home, proj, task, "working"); got.Status != state.StatusRunning {
		t.Errorf("status = %s, want running", got.Status)
	}
}

// Only a harness with the extension reads the file: a Claude task with one
// lying around is not blocked by it.
func TestTick_ClaudeIgnoresAskQuestionFiles(t *testing.T) {
	home, proj, task := piRunningTask(t, "")
	leaveQuestion(t, proj, task)

	if got := tickAndLoad(t, home, proj, task, "working"); got.Status != state.StatusRunning {
		t.Errorf("status = %s, want running", got.Status)
	}
}
