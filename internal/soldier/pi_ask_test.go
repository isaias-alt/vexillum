package soldier_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/piask"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

const askID = "0123456789abcdef0123456789abcdef"

func writeAskQuestion(t *testing.T, root string, task state.Task, id string) string {
	t.Helper()
	dir := piask.Dir(root, task.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"version":1,"id":"` + id + `","question":"Which database?","options":["Postgres","SQLite"],"asked_at":"2026-01-02T03:04:05Z"}`
	if err := os.WriteFile(filepath.Join(dir, "question.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Only Pi has an extension of vexillum's: Claude's launch args are untouched.
func TestExtensionHarness_OnlyPi(t *testing.T) {
	pi, _ := soldier.HarnessFor("pi")
	if _, ok := pi.(soldier.ExtensionHarness); !ok {
		t.Error("PiHarness is not an ExtensionHarness")
	}
	claude, _ := soldier.HarnessFor("claude")
	if _, ok := claude.(soldier.ExtensionHarness); ok {
		t.Error("ClaudeHarness must not have a launch extension")
	}

	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}
	client := &fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptStatus: "done"}
	if _, err := runInHerdr(t, home, "w1", task, c, client); err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if slices.Contains(client.startArgs[0], "-e") {
		t.Errorf("a claude soldier was given an extension: %v", client.startArgs[0])
	}
	if _, err := os.Stat(piask.ExtensionPath(testProjectRoot(t, home))); err == nil {
		t.Error("a claude dispatch installed the pi extension")
	}
}

// The extension is installed under the project's state directory, never in
// the camp, and the task's question directory is not created by dispatch.
func TestPiLaunchArgs_InstallsOutsideTheCamp(t *testing.T) {
	root := t.TempDir()
	task := newMissionTask(t)
	args, err := soldier.PiHarness{}.LaunchArgs(root, task)
	if err != nil {
		t.Fatalf("LaunchArgs: %v", err)
	}
	want := []string{"-e", piask.ExtensionPath(root), "--vx-ask-dir", piask.Dir(root, task.ID)}
	if !slices.Equal(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
	if _, err := os.Stat(piask.ExtensionPath(root)); err != nil {
		t.Errorf("extension not written: %v", err)
	}

	task.ID = "../escape"
	if _, err := (soldier.PiHarness{}).LaunchArgs(root, task); err == nil {
		t.Error("a task id that is not an id was accepted")
	}
}

// A blocked Pi task with a recorded question gets a Decision made of it; with
// none (the extension not loaded) it is read from the transcript as before; a
// question already answered is not taken again; Claude never reads the file.
func TestResolveBlockedDecisionForTask(t *testing.T) {
	root := t.TempDir()
	client := &fakeHerdr{}
	task := newMissionTask(t)
	task.Harness = "pi"
	output := "Which cloud?\n\n1. AWS\n2. GCP\n"

	d := soldier.ResolveBlockedDecisionForTask(root, task, client, "vx-x", output)
	if d == nil || d.Kind != state.DecisionKindProse || d.Question != "Which cloud?" {
		t.Fatalf("with no question file: %+v", d)
	}

	writeAskQuestion(t, root, task, askID)
	d = soldier.ResolveBlockedDecisionForTask(root, task, client, "vx-x", output)
	if d == nil || d.Kind != state.DecisionKindAsk || d.QuestionID != askID || d.Question != "Which database?" || !slices.Equal(d.Options, []string{"Postgres", "SQLite"}) {
		t.Fatalf("with a question file: %+v", d)
	}
	if !d.AskedAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("AskedAt = %v", d.AskedAt)
	}

	answered := task
	answered.Decision = &state.Decision{Kind: state.DecisionKindAsk, QuestionID: askID, AnsweredAt: time.Now()}
	if d := soldier.ResolveBlockedDecisionForTask(root, answered, client, "vx-x", output); d == nil || d.Kind != state.DecisionKindProse {
		t.Errorf("an answered question was taken again: %+v", d)
	}
	if soldier.HasOpenAskQuestion(root, answered) || !soldier.HasOpenAskQuestion(root, task) {
		t.Error("HasOpenAskQuestion does not follow whether the question is answered")
	}

	task.Harness = ""
	if d := soldier.ResolveBlockedDecisionForTask(root, task, client, "vx-x", output); d == nil || d.Kind == state.DecisionKindAsk {
		t.Errorf("a claude task read the pi question file: %+v", d)
	}
}

// AnswerBlocked on an ask decision resolves an option number to its text,
// delivers it through the file the extension waits on (here a goroutine that
// takes it the way the extension does), and records the answer.
func TestAnswerBlocked_AskDeliversThroughTheFile(t *testing.T) {
	home := t.TempDir()
	root := testProjectRoot(t, home)
	task := newMissionTask(t)
	task.Harness = "pi"
	task.HerdrAgentName = "vx-pick"
	task.Status = state.StatusBlocked
	task.Decision = &state.Decision{Question: "Which database?", Options: []string{"Postgres", "SQLite"}, Kind: state.DecisionKindAsk, QuestionID: askID, AskedAt: time.Now()}
	if err := state.Save(root, task); err != nil {
		t.Fatal(err)
	}
	dir := writeAskQuestion(t, root, task, askID)

	received := make(chan string, 1)
	go func() {
		for {
			if b, err := os.ReadFile(filepath.Join(dir, "answer.json")); err == nil && strings.Contains(string(b), askID) {
				received <- string(b)
				os.Remove(filepath.Join(dir, "question.json"))
				os.Remove(filepath.Join(dir, "answer.json"))
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()

	client := &fakeHerdr{promptStatus: "working", waitStatus: "done"}
	got, err := soldier.AnswerBlocked(root, task, "2", client)
	if err != nil {
		t.Fatalf("AnswerBlocked: %v", err)
	}
	select {
	case body := <-received:
		if !strings.Contains(body, `"answer": "SQLite"`) || !strings.Contains(body, `"option": 2`) {
			t.Errorf("delivered %s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing was delivered")
	}
	if len(client.promptCalls) != 0 || len(client.sendKeysCalls) != 0 {
		t.Errorf("something was typed into the pane: prompts %q keys %v", client.promptCalls, client.sendKeysCalls)
	}
	if got.Status != state.StatusDone || got.Decision.Answer != "2" || got.Decision.AnsweredAt.IsZero() {
		t.Errorf("task = status %s, decision %+v", got.Status, got.Decision)
	}
}

// A dismissed ask question stays dismissed: the file the extension has not
// cleared yet does not block the task again.
func TestDismissBlocked_AskQuestionStaysDismissed(t *testing.T) {
	home := t.TempDir()
	root := testProjectRoot(t, home)
	task := newMissionTask(t)
	task.Harness = "pi"
	task.HerdrAgentName = "vx-pick"
	task.Status = state.StatusBlocked
	task.Decision = &state.Decision{Question: "Which database?", Kind: state.DecisionKindAsk, QuestionID: askID, AskedAt: time.Now()}
	if err := state.Save(root, task); err != nil {
		t.Fatal(err)
	}
	writeAskQuestion(t, root, task, askID)

	client := &fakeHerdr{promptStatus: "working"}
	got, err := soldier.DismissBlocked(root, task, client)
	if err != nil {
		t.Fatalf("DismissBlocked: %v", err)
	}
	if got.Status != state.StatusRunning || !got.Decision.Dismissed {
		t.Fatalf("status %s, decision %+v", got.Status, got.Decision)
	}
	if soldier.HasOpenAskQuestion(root, got) {
		t.Error("a dismissed question is still open")
	}
}
