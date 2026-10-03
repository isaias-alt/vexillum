package tribunal

import (
	"strings"
	"testing"
)

func TestRun_StopsAtFirstFailingStep(t *testing.T) {
	dir := newTribunalRepo(t)
	// No go.mod/package.json: lint and tests both skip (pass), so review
	// is the first real step - and an unusable reviewer answer fails it,
	// meaning docs must never run.
	commitFile(t, dir, "change.txt", "hi\n")
	stub := newClaudeStub(t)
	stub.out(0, "nothing useful here")

	result, err := Run(dir, "base", Options{Branch: "b"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Passed() {
		t.Fatal("expected Passed()=false")
	}
	failed := result.FailedStep()
	if failed == nil || failed.Step != StepReview {
		t.Fatalf("expected review to be the failing step, got %+v", failed)
	}
	if len(result.Steps) != 3 {
		t.Fatalf("expected exactly 3 steps to have run (lint, tests, review), got %d: %+v", len(result.Steps), result.Steps)
	}
}

func TestRun_AllStepsPass(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	stub := newClaudeStub(t)
	stub.out(0, reportJSON("", "change.txt"))

	result, err := Run(dir, "base", Options{Branch: "b", TaskPrompt: "add change.txt"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Passed() {
		t.Fatalf("expected Passed()=true, got steps: %+v", result.Steps)
	}
	if len(result.Steps) != 4 {
		t.Fatalf("expected all 4 steps to run, got %d: %+v", len(result.Steps), result.Steps)
	}
	if result.Steps[0].Step != StepLint || result.Steps[1].Step != StepTests || result.Steps[2].Step != StepReview || result.Steps[3].Step != StepDocs {
		t.Fatalf("expected steps in lint, tests, review, docs order, got %+v", result.Steps)
	}
}

// Without the opt-in flag a blocking review only rejects: one reviewer run,
// no fixer, no new commit.
func TestRun_BlockingReviewDoesNotFixWithoutOptIn(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	head := run(t, dir, "rev-parse", "HEAD")
	stub := newClaudeStub(t)
	stub.out(0, reportJSON(finding("error", "auto-fix", "change.txt"), "change.txt"))

	result, err := Run(dir, "base", Options{Branch: "b"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Passed() {
		t.Fatal("expected the run to fail")
	}
	if stub.calls() != 1 {
		t.Errorf("expected exactly 1 claude call (the review), got %d", stub.calls())
	}
	if run(t, dir, "rev-parse", "HEAD") != head {
		t.Error("expected no commit without the fix opt-in")
	}
}

// The opt-in loop: review blocks on an auto-fix finding, a fixer edits the
// camp, vexillum commits it, and a brand-new reviewer (told the fixer's
// commits are unreviewed code) passes.
func TestRun_FixLoopFixesThenPasses(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	startHead := strings.TrimSpace(run(t, dir, "rev-parse", "HEAD"))
	stub := newClaudeStub(t)
	stub.out(1, reportJSON(finding("error", "auto-fix", "change.txt"), "change.txt"))
	stub.hook(2, "echo fixed > fixed.txt")
	stub.out(3, reportJSON("", "change.txt", "fixed.txt"))

	var logs []string
	result, err := Run(dir, "base", Options{Branch: "b", Fix: true, Log: func(m string) { logs = append(logs, m) }})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Passed() {
		t.Fatalf("expected the loop to end passing, got %+v", result.Steps)
	}
	if stub.calls() != 3 {
		t.Fatalf("expected review, fix, review = 3 claude calls, got %d", stub.calls())
	}
	if len(result.Earlier) != 1 {
		t.Fatalf("expected 1 superseded round, got %d", len(result.Earlier))
	}
	if !strings.Contains(stub.prompt(2), "a defect") || strings.Contains(stub.prompt(2), "git diff") {
		t.Errorf("expected the fixer prompt to carry the findings and not a diff, got:\n%s", stub.prompt(2))
	}
	rereview := stub.prompt(3)
	if !strings.Contains(rereview, "Fix-round provenance") || !strings.Contains(rereview, startHead) {
		t.Errorf("expected the re-review to carry the provenance clause naming the pre-fix head, got:\n%s", rereview)
	}
	if strings.Contains(stub.prompt(1), "Fix-round provenance") {
		t.Error("the first review must not carry the provenance clause")
	}
	if strings.Contains(stub.args(3), "--resume") {
		t.Error("the re-review must be a fresh process, never a resumed session")
	}
	if !strings.Contains(run(t, dir, "log", "-1", "--format=%s"), "tribunal review findings (round 1)") {
		t.Error("expected vexillum to commit the fixer's work")
	}
	if len(logs) == 0 {
		t.Error("expected progress to be logged")
	}
}

// The loop is bounded: an endlessly failing review ends in a rejection
// after MaxFixRounds fix rounds.
func TestRun_FixLoopStopsAtMaxRounds(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	stub := newClaudeStub(t)
	stub.out(0, reportJSON(finding("error", "auto-fix", "change.txt"), "change.txt", "fix1.txt", "fix2.txt"))
	stub.hook(2, "echo 1 > fix1.txt")
	stub.hook(4, "echo 2 > fix2.txt")

	result, err := Run(dir, "base", Options{Branch: "b", Fix: true, MaxFixRounds: 2})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Passed() {
		t.Fatal("expected the run to fail after exhausting the rounds")
	}
	// review, fix, review, fix, review.
	if stub.calls() != 5 {
		t.Errorf("expected 5 claude calls for 2 fix rounds, got %d", stub.calls())
	}
	if len(result.Earlier) != 2 {
		t.Errorf("expected 2 superseded rounds, got %d", len(result.Earlier))
	}
	if failed := result.FailedStep(); failed == nil || failed.Step != StepReview || len(failed.Notes) == 0 {
		t.Errorf("expected a failed review noting the exhausted loop, got %+v", failed)
	}
}

// An ask-user finding needs a human decision: the fix loop never runs, even
// when every other blocker is auto-fix.
func TestRun_FixLoopNeverFixesAskUser(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	stub := newClaudeStub(t)
	stub.out(0, reportJSON(finding("warning", "ask-user", "change.txt")+","+finding("error", "auto-fix", "change.txt"), "change.txt"))

	result, err := Run(dir, "base", Options{Branch: "b", Fix: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Passed() {
		t.Fatal("expected the run to fail")
	}
	if stub.calls() != 1 {
		t.Errorf("expected only the review to run, got %d claude calls", stub.calls())
	}
}

func TestRun_FixerThatChangesNothingFails(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	stub := newClaudeStub(t)
	stub.out(0, reportJSON(finding("error", "auto-fix", "change.txt"), "change.txt"))

	result, err := Run(dir, "base", Options{Branch: "b", Fix: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Passed() {
		t.Fatal("expected the run to fail")
	}
	last := result.Steps[len(result.Steps)-1]
	if last.Step != StepFix || last.Passed {
		t.Errorf("expected the last step to be a failed fix, got %+v", last)
	}
	if failed := result.FailedStep(); failed == nil || failed.Step != StepFix {
		t.Errorf("expected the fix to be the step that stopped the run, got %+v", failed)
	}
	if stub.calls() != 2 {
		t.Errorf("expected no re-review after a no-op fixer, got %d calls", stub.calls())
	}
}
