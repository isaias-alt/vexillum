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
	if !strings.Contains(stub.prompt(2), "report you just gave was rejected") || !strings.Contains(stub.prompt(2), "findings") {
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

// The reviewer runs through the mission's harness: an empty or explicit
// claude harness invokes claude as before.
func TestRunReview_UsesTheMissionsHarness(t *testing.T) {
	for _, harness := range []string{"", "claude"} {
		t.Run("harness="+harness, func(t *testing.T) {
			dir := newTribunalRepo(t)
			commitFile(t, dir, "change.txt", "hi\n")
			stub := newClaudeStub(t)
			stub.out(0, reportJSON("", "change.txt"))

			sr, err := runReview(dir, "base", Options{Branch: "b", Harness: harness}, "")
			if err != nil {
				t.Fatalf("runReview: %v", err)
			}
			if !sr.Passed || stub.calls() != 1 {
				t.Fatalf("expected one passing claude invocation, got passed=%v calls=%d", sr.Passed, stub.calls())
			}
		})
	}
}

// A harness with no implementation never falls back to claude: the review
// step errors out without invoking anything, so it can never read as a pass.
func TestRunReview_RefusesAHarnessWithNoImplementation(t *testing.T) {
	for _, harness := range []string{"gemini"} {
		t.Run(harness, func(t *testing.T) {
			dir := newTribunalRepo(t)
			commitFile(t, dir, "change.txt", "hi\n")
			stub := newClaudeStub(t)
			stub.out(0, reportJSON("", "change.txt"))

			_, err := runReview(dir, "base", Options{Branch: "b", Harness: harness}, "")
			if err == nil {
				t.Fatal("expected an error for a harness with no implementation")
			}
			if !strings.Contains(err.Error(), harness) {
				t.Errorf("expected the error to name the harness %q, got: %v", harness, err)
			}
			if stub.calls() != 0 {
				t.Errorf("claude must not be invoked, got %d calls", stub.calls())
			}
		})
	}
}

// A Pi mission is reviewed by pi, not claude: one `pi -p` run with no session,
// on the model and thinking level the mission itself ran with (the fixed
// claude sonnet/high mean nothing to Pi), the prompt last after "--", and no
// claude on PATH at all.
func TestRunReview_PiRunsPiOnTheMissionsModel(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	stub := newPiStub(t)
	stub.out(0, reportJSON("", "change.txt"))

	opts := Options{Branch: "b", Harness: "pi", TaskModel: "openrouter/foo/bar:free", TaskEffort: "low"}
	sr, err := runReview(dir, "base", opts, "")
	if err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if !sr.Passed || stub.calls() != 1 {
		t.Fatalf("expected one passing pi invocation, got passed=%v calls=%d", sr.Passed, stub.calls())
	}
	// The recorded argv has one argument per line, and the prompt is many
	// lines, so only the leading flags are compared.
	args := strings.Split(stub.args(1), "\n")
	want := []string{"-p", "--no-session", "--model", "openrouter/foo/bar:free", "--thinking", "low", "--"}
	if len(args) <= len(want) || strings.Join(args[:len(want)], "|") != strings.Join(want, "|") {
		t.Errorf("pi argv starts %q, want %q then the prompt", args[:min(len(args), len(want))], want)
	}
	if strings.Contains(stub.args(1), "sonnet") {
		t.Errorf("the claude reviewer model leaked into pi's argv: %s", stub.args(1))
	}
	if !strings.Contains(stub.prompt(1), "Independent check of branch b against base") {
		t.Errorf("the reviewer's brief was not the last argument: %q", stub.prompt(1))
	}
}

// A Pi mission whose profile set no model leaves the choice to Pi: no
// --model or --thinking at all.
func TestRunReview_PiWithNoModelUsesPisDefault(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	stub := newPiStub(t)
	stub.out(0, reportJSON("", "change.txt"))

	if _, err := runReview(dir, "base", Options{Branch: "b", Harness: "pi"}, ""); err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if a := stub.args(1); strings.Contains(a, "--model") || strings.Contains(a, "--thinking") {
		t.Errorf("expected no model or thinking flags, got %s", a)
	}
}

// Claude's reviewer keeps its fixed model and effort, whatever model the
// mission ran with.
func TestRunReview_ClaudeKeepsItsFixedModel(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	stub := newClaudeStub(t)
	stub.out(0, reportJSON("", "change.txt"))

	opts := Options{Branch: "b", TaskModel: "haiku", TaskEffort: "low"}
	if _, err := runReview(dir, "base", opts, ""); err != nil {
		t.Fatalf("runReview: %v", err)
	}
	a := strings.Fields(stub.args(1))
	if !strings.Contains(strings.Join(a, " "), "--model sonnet") || !strings.Contains(strings.Join(a, " "), "--effort high") {
		t.Errorf("claude argv lost the fixed sonnet/high: %v", a)
	}
}
