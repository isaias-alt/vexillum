package tribunal

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/state"
)

func TestRunReview_NoChangesSkipsWithoutInvokingClaude(t *testing.T) {
	dir := newTribunalRepo(t)
	// git-only PATH, no claude stub: if runReview tried to invoke claude
	// here, it would fail with "executable file not found".
	t.Setenv("PATH", gitOnlyPath(t))

	sr, err := runReview(dir, "base", Options{}, "")
	if err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if !sr.Passed {
		t.Errorf("expected no-diff to pass trivially, got: %+v", sr)
	}
}

// The reviewer reads the diff itself: the prompt names the commits, the
// changed files and the mission statement, but contains no diff and nothing
// of the author's reasoning, and the process is a fresh one.
func TestRunReview_PromptHasNoDiffAndNoAuthorContext(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "src/change.txt", "DIFF_MARKER_LINE\n")
	stub := newClaudeStub(t)
	stub.out(0, reportJSON("", "src/change.txt"))

	sr, err := runReview(dir, "base", Options{Branch: "vexillum/abc", TaskPrompt: "MISSION_STATEMENT add a file"}, "")
	if err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if !sr.Passed {
		t.Fatalf("expected a pass, got %+v", sr)
	}

	prompt := stub.prompt(1)
	if strings.Contains(prompt, "DIFF_MARKER_LINE") || strings.Contains(prompt, "diff --git") || strings.Contains(prompt, "\n@@ ") {
		t.Errorf("the prompt must not carry the diff:\n%s", prompt)
	}
	baseSHA := strings.TrimSpace(run(t, dir, "rev-parse", "base"))
	head := strings.TrimSpace(run(t, dir, "rev-parse", "HEAD"))
	for _, want := range []string{"vexillum/abc", baseSHA, head, "- src/change.txt", "MISSION_STATEMENT add a file", "Read the relevant history and diff yourself", "assume the change is wrong"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected the prompt to contain %q:\n%s", want, prompt)
		}
	}
	args := stub.args(1)
	for _, banned := range []string{"--resume", "--continue", "-c\n", "-r\n"} {
		if strings.Contains(args, banned) {
			t.Errorf("a review must be a brand-new process, argv contains %q:\n%s", banned, args)
		}
	}
	for _, want := range []string{"--model\nsonnet", "--effort\nhigh"} {
		if !strings.Contains(args, want) {
			t.Errorf("expected argv to contain %q:\n%s", want, args)
		}
	}
}

// Everything the reviewer is told is one of these fields. Adding a field
// (a conversation, a transcript, the diff, the author's notes) breaks this
// test, so the addition has to be a deliberate, reviewed decision.
func TestReviewInput_OnlyCarriesWhitelistedContext(t *testing.T) {
	want := []string{"Branch", "BaseBranch", "BaseSHA", "TargetSHA", "Files", "Intent", "FixStartSHA"}
	typ := reflect.TypeOf(reviewInput{})
	var got []string
	for i := 0; i < typ.NumField(); i++ {
		got = append(got, typ.Field(i).Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reviewInput fields changed: got %v, want %v - never give the reviewer the author's conversation or the diff", got, want)
	}
}

func TestRunReview_NoPromptSkipsSimplificationAndSaysSo(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	stub := newClaudeStub(t)
	stub.out(0, reportJSON("", "change.txt"))

	sr, err := runReview(dir, "base", Options{Branch: "b", TaskPrompt: "  \n"}, "")
	if err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if !sr.Passed {
		t.Fatalf("expected a pass, got %+v", sr)
	}
	if strings.Contains(stub.prompt(1), "Simplification pass") || strings.Contains(stub.prompt(1), "<mission>") {
		t.Error("expected no simplification section without a task prompt")
	}
	if len(sr.Notes) != 1 || !strings.Contains(sr.Notes[0], "simplification pass skipped") {
		t.Errorf("expected a note saying the simplification pass was skipped, got %v", sr.Notes)
	}

	stub2 := newClaudeStub(t)
	stub2.out(0, reportJSON("", "change.txt"))
	sr, err = runReview(dir, "base", Options{Branch: "b", TaskPrompt: "add it"}, "")
	if err != nil || !sr.Passed {
		t.Fatalf("runReview: %v %+v", err, sr)
	}
	if !strings.Contains(stub2.prompt(1), "Simplification pass") || len(sr.Notes) != 0 {
		t.Error("expected the simplification pass and no note when the task has a prompt")
	}
}

func TestRunReview_SeverityDecidesBlocking(t *testing.T) {
	cases := []struct {
		name     string
		severity string
		action   string
		blocks   bool
	}{
		{"error blocks", "error", "auto-fix", true},
		{"warning blocks", "warning", "auto-fix", true},
		{"ask-user warning blocks", "warning", "ask-user", true},
		{"ask-user error blocks", "error", "ask-user", true},
		{"info does not block", "info", "no-op", false},
		{"info ask-user does not block", "info", "ask-user", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := newTribunalRepo(t)
			commitFile(t, dir, "change.txt", "hi\n")
			stub := newClaudeStub(t)
			stub.out(0, reportJSON(finding(tc.severity, tc.action, "change.txt"), "change.txt"))

			sr, err := runReview(dir, "base", Options{Branch: "b"}, "")
			if err != nil {
				t.Fatalf("runReview: %v", err)
			}
			if sr.Passed == tc.blocks {
				t.Fatalf("passed=%v, want %v: %+v", sr.Passed, !tc.blocks, sr)
			}
			if !strings.Contains(sr.Detail, "a defect") {
				t.Errorf("expected the finding in Detail, got %q", sr.Detail)
			}
			if sr.Report == nil || len(sr.Report.Findings) != 1 {
				t.Errorf("expected the report to be kept, got %+v", sr.Report)
			}
		})
	}
}

// Declared coverage is held against vexillum's own file list: a changed file
// the reviewer did not report reading fails the step, even with no findings.
func TestRunReview_IncompleteCoverageFails(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "a.txt", "a\n")
	commitFile(t, dir, "b.txt", "b\n")
	stub := newClaudeStub(t)
	stub.out(0, reportJSON("", "a.txt"))

	sr, err := runReview(dir, "base", Options{Branch: "b"}, "")
	if err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if sr.Passed {
		t.Fatal("expected incomplete coverage to fail the step")
	}
	if !strings.Contains(sr.Detail, "incomplete coverage") || !strings.Contains(sr.Detail, "b.txt") || strings.Contains(sr.Detail, "did not report reading: a.txt") {
		t.Errorf("expected Detail to name exactly the unreviewed file, got %q", sr.Detail)
	}
}

func TestRunReview_InvalidJSONIsRetriedThenAccepted(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	stub := newClaudeStub(t)
	stub.out(1, "no json at all")
	stub.out(2, `{"findings": [{"file": "change.txt", "line": 1, "severity": "fatal", "action": "auto-fix", "description": "d", "failure_scenario": "s"}], "reviewed_paths": [], "risk_level": "low", "risk_rationale": "r"}`)
	stub.out(3, reportJSON("", "change.txt"))

	sr, err := runReview(dir, "base", Options{Branch: "b"}, "")
	if err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if !sr.Passed {
		t.Fatalf("expected the third, valid answer to pass, got %+v", sr)
	}
	if stub.calls() != 3 {
		t.Fatalf("expected 3 attempts, got %d", stub.calls())
	}
	if !strings.Contains(stub.prompt(2), "previous answer was rejected") || !strings.Contains(stub.prompt(2), "findings") {
		t.Errorf("expected the retry to carry the validation error:\n%s", stub.prompt(2))
	}
	if !strings.Contains(stub.prompt(3), `severity must be one of`) {
		t.Errorf("expected the second retry to carry the schema error:\n%s", stub.prompt(3))
	}
}

func TestRunReview_InvalidJSONAfterAllAttemptsFails(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	stub := newClaudeStub(t)
	stub.out(0, "PASS, trust me")

	sr, err := runReview(dir, "base", Options{Branch: "b"}, "")
	if err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if sr.Passed {
		t.Fatal("expected no valid report to fail the step")
	}
	if stub.calls() != reviewMaxAttempts {
		t.Errorf("expected %d bounded attempts, got %d", reviewMaxAttempts, stub.calls())
	}
}

// A timeout is a failure, never a pass, and is not retried.
func TestRunReview_TimeoutFails(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	stub := newClaudeStub(t)
	stub.sleepOn(1)
	stub.out(0, reportJSON("", "change.txt"))

	start := time.Now()
	sr, err := runReview(dir, "base", Options{Branch: "b", Timeout: 8 * time.Second}, "")
	if err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if sr.Passed {
		t.Fatal("a timeout must never be a pass")
	}
	if !strings.Contains(sr.Detail, "timed out") {
		t.Errorf("expected Detail to say it timed out, got %q", sr.Detail)
	}
	if stub.calls() != 1 {
		t.Errorf("a timeout must not be retried, got %d calls", stub.calls())
	}
	if elapsed := time.Since(start); elapsed > 25*time.Second {
		t.Errorf("the timeout took %s to take effect", elapsed)
	}
}

// The general's later instructions reach the reviewer as part of the intent:
// after the original prompt, numbered in the order given, labeled as coming
// afterward, and the simplification rule counts them as required scope.
func TestRunReview_IntentIncludesTheGeneralsLaterInstructions(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	stub := newClaudeStub(t)
	stub.out(0, reportJSON("", "change.txt"))

	at := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	opts := Options{
		Branch:     "b",
		TaskPrompt: "ORIGINAL_PROMPT add a file",
		TaskAmendments: []state.Amendment{
			{Text: "FIRST_LATER restyle it", At: at, Source: state.AmendmentSourcePrompt},
			{Text: "SECOND_LATER add a retry", At: at.Add(time.Hour), Source: state.AmendmentSourceDecide},
		},
	}
	sr, err := runReview(dir, "base", opts, "")
	if err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if !sr.Passed {
		t.Fatalf("expected a pass, got %+v", sr)
	}

	prompt := stub.prompt(1)
	mission := prompt[strings.Index(prompt, "<mission>"):strings.Index(prompt, "</mission>")]
	order := []string{"ORIGINAL_PROMPT add a file", "Instructions the general gave afterward", "1. [2026-10-02T09:00:00Z, via vx prompt] FIRST_LATER restyle it", "2. [2026-10-02T10:00:00Z, via vx decide] SECOND_LATER add a retry"}
	last := -1
	for _, want := range order {
		i := strings.Index(mission, want)
		if i < 0 {
			t.Fatalf("expected the mission to contain %q:\n%s", want, mission)
		}
		if i < last {
			t.Errorf("%q is out of order in the mission:\n%s", want, mission)
		}
		last = i
	}

	// Required by an instruction is not unrequested, but required by neither
	// is still reported.
	for _, want := range []string{"a component any of them asks for is required", "neither the original request nor a later instruction strictly requires", `severity "warning" and action "ask-user"`} {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected the simplification rules to contain %q:\n%s", want, prompt)
		}
	}
}

// With no amendments the intent is exactly the prompt, as before.
func TestRunReview_NoAmendmentsLeavesTheIntentAsThePrompt(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	stub := newClaudeStub(t)
	stub.out(0, reportJSON("", "change.txt"))

	if _, err := runReview(dir, "base", Options{Branch: "b", TaskPrompt: "just this"}, ""); err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if strings.Contains(stub.prompt(1), "Instructions the general gave afterward") {
		t.Error("expected no later-instructions section without amendments")
	}
	if !strings.Contains(stub.prompt(1), "<mission>\njust this\n</mission>") {
		t.Errorf("expected the bare prompt as the mission:\n%s", stub.prompt(1))
	}
}

// The fixer judges "required" against the same intent as the reviewer.
func TestOptionsIntent_FeedsTheFixerToo(t *testing.T) {
	opts := Options{
		TaskPrompt:     "ORIGINAL",
		TaskAmendments: []state.Amendment{{Text: "LATER_ASK", At: time.Unix(0, 0), Source: state.AmendmentSourcePrompt}},
	}
	prompt := buildFixPrompt([]Finding{{File: "a.go", Line: 1, Severity: SeverityWarning, Description: "d"}}, opts.intent(), "b")
	for _, want := range []string{"ORIGINAL", "LATER_ASK", "Instructions the general gave afterward"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected the fix prompt to contain %q:\n%s", want, prompt)
		}
	}
}
