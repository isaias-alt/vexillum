package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/herdr"
	vxproject "github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
	"github.com/isaias-alt/vexillum/internal/tribunal"
)

// blockedTestTask persists a task already in StatusBlocked, with a real
// decision extracted from a transcript - the exact shape RunInHerdr or
// sentinel.tickProject leaves on disk for a genuinely blocked soldier.
func blockedTestTask(t *testing.T, projectRoot string) state.Task {
	t.Helper()
	task, err := state.New(state.KindMission, "pick a database for the reporting service")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.HerdrAgentName = "vx-pick-a-database"
	task.Status = state.StatusBlocked
	task.Output = "I've weighed the options for the reporting service.\n\n" +
		"Which database should this use?\n\n" +
		"1. Postgres - already in use elsewhere\n" +
		"2. ClickHouse - better for this workload but a new dependency\n"
	task.Decision = soldier.ExtractDecision(task.Output)
	if task.Decision == nil {
		t.Fatal("expected ExtractDecision to produce a decision from a real blocked transcript")
	}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	return task
}

// Regression test for the durable decision feature end to end: a blocked
// task's question survives a simulated restart (a fresh state.Load off
// disk, not the in-memory task the test itself built), and 'vexillum
// decide' both delivers and durably records the answer, resuming the task.
func TestDecide_BlockedDecisionSurvivesRestartAndIsAnswered(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()

	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	task := blockedTestTask(t, projectRoot)

	// Simulate a restart: don't trust the in-memory task built above -
	// reload strictly from disk, the way a freshly started vexillum
	// process (or a fresh 'vx status') would.
	reloaded, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatalf("state.Load after simulated restart: %v", err)
	}
	if reloaded.Status != state.StatusBlocked {
		t.Fatalf("expected the reloaded task to still be blocked, got %s", reloaded.Status)
	}
	if reloaded.Decision == nil {
		t.Fatal("expected the decision to survive a restart, got nil")
	}
	if reloaded.Decision.Question != "Which database should this use?" {
		t.Errorf("expected the actual question text to survive, got %q", reloaded.Decision.Question)
	}
	if len(reloaded.Decision.Options) != 2 {
		t.Errorf("expected the options to survive, got %v", reloaded.Decision.Options)
	}
	if reloaded.Decision.Answer != "" || !reloaded.Decision.AnsweredAt.IsZero() {
		t.Errorf("expected no answer recorded yet, got %+v", reloaded.Decision)
	}

	// Answer it via the new path.
	client := &fakeHerdr{promptStatus: "done", readOutput: "went with Postgres, done"}
	var out bytes.Buffer
	code := runDecide(project, home, task.ID, "Use Postgres", client, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	if len(client.promptCalls) != 1 || client.promptCalls[0] != "Use Postgres" {
		t.Errorf("expected the answer delivered to the soldier's pane, got %v", client.promptCalls)
	}
	if client.promptNames[0] != task.HerdrAgentName {
		t.Errorf("expected the answer delivered to the task's own agent, got %q", client.promptNames[0])
	}

	// The task resumes and the answer is durably recorded - assert again
	// via a fresh state.Load, not the value runDecide happened to compute
	// in memory.
	final, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatalf("state.Load after decide: %v", err)
	}
	if final.Status != state.StatusDone {
		t.Errorf("expected the task to resume and settle to done, got %s", final.Status)
	}
	if final.Decision == nil || final.Decision.Answer != "Use Postgres" {
		t.Errorf("expected the answer recorded against the decision, got %+v", final.Decision)
	}
	if final.Decision.AnsweredAt.IsZero() {
		t.Error("expected AnsweredAt to be set")
	}
	// The original question is preserved alongside the answer - decide
	// doesn't discard what was actually asked.
	if final.Decision.Question != "Which database should this use?" {
		t.Errorf("expected the original question preserved, got %q", final.Decision.Question)
	}
}

// decide refuses a task that isn't blocked, without touching it.
func TestDecide_RefusesNonBlocked(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}

	task, err := state.New(state.KindMission, "do a thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.Status = state.StatusRunning
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	var out bytes.Buffer
	code := runDecide(project, home, task.ID, "some answer", &fakeHerdr{}, &out, &out)
	if code == 0 {
		t.Fatal("expected non-zero exit for a non-blocked task")
	}
	if !strings.Contains(out.String(), "not blocked") {
		t.Errorf("expected the error to name why it refused, got: %s", out.String())
	}

	reloaded, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if reloaded.Status != state.StatusRunning {
		t.Errorf("expected the task untouched, got status=%s", reloaded.Status)
	}
}

// decide refuses an interrupted task with a message pointing at
// redispatch instead - that's a different, already-existing recovery path.
func TestDecide_RefusesInterrupted(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}

	task, err := state.New(state.KindMission, "do a thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.Status = state.StatusInterrupted
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	var out bytes.Buffer
	code := runDecide(project, home, task.ID, "some answer", &fakeHerdr{}, &out, &out)
	if code == 0 {
		t.Fatal("expected non-zero exit for an interrupted task")
	}
	if !strings.Contains(out.String(), "redispatch") {
		t.Errorf("expected the error to point at redispatch, got: %s", out.String())
	}
}

// If the soldier's pane is actually gone by the time decide tries to
// deliver the answer, the task is marked interrupted rather than left
// dangling as blocked forever (internal/sentinel never polls a blocked
// task).
func TestDecide_PaneGoneMarksInterrupted(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	task := blockedTestTask(t, projectRoot)

	client := &fakeHerdr{promptErr: &herdr.APIError{Code: "agent_not_running", Message: "pane closed"}}
	var out bytes.Buffer
	code := runDecide(project, home, task.ID, "Use Postgres", client, &out, &out)
	if code == 0 {
		t.Fatal("expected non-zero exit when the pane is gone")
	}
	if !strings.Contains(out.String(), "redispatch") {
		t.Errorf("expected the error to point at redispatch, got: %s", out.String())
	}

	reloaded, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if reloaded.Status != state.StatusInterrupted {
		t.Errorf("expected the task marked interrupted, got %s", reloaded.Status)
	}
}

// falseBlockedTask persists a task wrongly marked blocked on the dispatch
// prompt's own template line, the way the bug left it.
func falseBlockedTask(t *testing.T, projectRoot string) state.Task {
	t.Helper()
	task, err := state.New(state.KindMission, "do a thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.HerdrAgentName = "vx-do-a-thing"
	task.Status = state.StatusBlocked
	task.Output = "needs-decision: <a one-line summary of the question and any options>\n"
	task.Decision = &state.Decision{Question: "<a one-line summary of the question and any options>", Kind: state.DecisionKindProse}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	return task
}

// vx decide --dismiss clears a wrongly blocked task according to its live
// pane and sends the soldier nothing.
func TestDecideDismiss_ClearsAWronglyBlockedTask(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatal(err)
	}
	task := falseBlockedTask(t, projectRoot)

	client := &fakeHerdr{promptStatus: "idle", readOutput: "all done"}
	var out, errOut bytes.Buffer
	if code := runDecideDismiss(project, home, task.ID, client, &out, &errOut); code != 0 {
		t.Fatalf("expected success, got %d: %s%s", code, out.String(), errOut.String())
	}
	if len(client.promptCalls) != 0 {
		t.Errorf("nothing may be sent to the soldier, got %v", client.promptCalls)
	}
	got, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.StatusDone {
		t.Errorf("expected done, got %s", got.Status)
	}
	if got.Decision == nil || !got.Decision.Dismissed {
		t.Errorf("expected the decision kept and marked dismissed, got %+v", got.Decision)
	}
}

func TestDecideDismiss_RefusesAReallyBlockedPane(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatal(err)
	}
	task := falseBlockedTask(t, projectRoot)

	client := &fakeHerdr{promptStatus: "blocked"}
	var out, errOut bytes.Buffer
	if code := runDecideDismiss(project, home, task.ID, client, &out, &errOut); code == 0 {
		t.Fatal("expected a refusal for a pane that is really blocked")
	}
	if !strings.Contains(errOut.String(), "decide") {
		t.Errorf("expected the refusal to point at answering, got: %s", errOut.String())
	}
	got, _ := state.Load(projectRoot, task.ID)
	if got.Status != state.StatusBlocked {
		t.Errorf("expected the task untouched, got %s", got.Status)
	}
}

func TestDecideDismiss_RefusesANonBlockedTask(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatal(err)
	}
	task := falseBlockedTask(t, projectRoot)
	task.Status = state.StatusDone
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := runDecideDismiss(project, home, task.ID, &fakeHerdr{}, &out, &errOut); code == 0 {
		t.Fatal("expected a refusal for a task that is not blocked")
	}
	if !strings.Contains(errOut.String(), "not blocked") {
		t.Errorf("expected the refusal to say why, got: %s", errOut.String())
	}
}

// --dismiss through the real entry point: it takes only the task id.
func TestDecide_DismissTakesNoAnswer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if code := Decide([]string{"sometask", "--dismiss", "extra"}); code == 0 {
		t.Error("expected --dismiss with an answer to be refused")
	}
}

// Both commands that refuse a blocked task say how to clear a wrong block.
func TestRefusalsOfABlockedTaskMentionDismiss(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatal(err)
	}
	task := falseBlockedTask(t, projectRoot)

	var out bytes.Buffer
	if code := runShip(project, home, task.ID, tribunal.Options{}, &out, &out); code == 0 {
		t.Fatal("expected ship to refuse a blocked task")
	}
	if !strings.Contains(out.String(), "decide "+task.ID+" --dismiss") {
		t.Errorf("expected ship's refusal to name the way out, got: %s", out.String())
	}

	out.Reset()
	if msg := repromptRefusal(task); !strings.Contains(msg, "decide "+task.ID+" --dismiss") {
		t.Errorf("expected prompt's refusal to name the way out, got: %s", msg)
	}
}

// vx decide records the answer as an amendment, with the question it
// answered; --dismiss answers nothing and records nothing.
func TestDecide_RecordsAnAmendmentWithTheQuestion(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatal(err)
	}
	task := blockedTestTask(t, projectRoot)

	client := &fakeHerdr{promptStatus: "done", readOutput: "went with Postgres, done"}
	var out bytes.Buffer
	if code := runDecide(project, home, task.ID, "Use Postgres, and add a retry", client, &out, &out); code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}

	got, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Prompt != task.Prompt {
		t.Errorf("the dispatch prompt must stay untouched, got %q", got.Prompt)
	}
	if len(got.Amendments) != 1 || got.Amendments[0].Source != state.AmendmentSourceDecide {
		t.Fatalf("expected one vx decide amendment, got %+v", got.Amendments)
	}
	for _, want := range []string{"Which database should this use?", "Use Postgres, and add a retry"} {
		if !strings.Contains(got.Amendments[0].Text, want) {
			t.Errorf("expected the amendment to contain %q, got %q", want, got.Amendments[0].Text)
		}
	}
}

// A modal pick typed as a digit is recorded as the option it selected: "2"
// alone would tell the tribunal nothing.
func TestDecide_ModalPickIsRecordedAsTheOptionLabel(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatal(err)
	}
	task := blockedTestTask(t, projectRoot)
	task.Decision.Kind = state.DecisionKindModal
	task.Decision.Options = []string{"Postgres", "ClickHouse"}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatal(err)
	}

	client := &fakeHerdr{promptStatus: "done", readOutput: "ok"}
	var out bytes.Buffer
	if code := runDecide(project, home, task.ID, "2", client, &out, &out); code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	got, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Amendments) != 1 || !strings.HasSuffix(got.Amendments[0].Text, "ClickHouse") {
		t.Errorf("expected the amendment to name the chosen option, got %+v", got.Amendments)
	}
}

// An answer that could not be delivered (pane gone) is no instruction.
func TestDecide_PaneGoneRecordsNoAmendment(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatal(err)
	}
	task := blockedTestTask(t, projectRoot)

	client := &fakeHerdr{promptErr: &herdr.APIError{Code: "agent_not_running", Message: "pane closed"}}
	var out bytes.Buffer
	if code := runDecide(project, home, task.ID, "Use Postgres", client, &out, &out); code == 0 {
		t.Fatal("expected a failure")
	}
	got, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Amendments) != 0 {
		t.Errorf("expected no amendment, got %+v", got.Amendments)
	}
}

func TestDecideDismiss_RecordsNoAmendment(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatal(err)
	}
	task := falseBlockedTask(t, projectRoot)

	client := &fakeHerdr{promptStatus: "idle", readOutput: "all done"}
	var out, errOut bytes.Buffer
	if code := runDecideDismiss(project, home, task.ID, client, &out, &errOut); code != 0 {
		t.Fatalf("expected success, got %d: %s%s", code, out.String(), errOut.String())
	}
	got, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Amendments) != 0 {
		t.Errorf("a dismissal sends nothing and must record nothing, got %+v", got.Amendments)
	}
}
