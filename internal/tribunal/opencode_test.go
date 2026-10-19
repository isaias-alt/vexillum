package tribunal

import (
	"encoding/json"
	"strings"
	"testing"
)

// newOpenCodeStub is the stub under the name "opencode": its prompt is the
// last argument (after "--"), and it records the environment it saw and
// whatever was on its stdin, which is empty only when stdin is the null
// device (an open pipe would hang its cat until the timeout).
func newOpenCodeStub(t *testing.T) *claudeStub {
	t.Helper()
	return newStub(t, "opencode", `for last; do :; done; printf '%s' "$last" > "$d/prompt.$n"
printf '%s' "$OPENCODE_DISABLE_AUTOUPDATE" > "$d/env.$n"
cat > "$d/stdin.$n"`)
}

// events renders text as the JSON event lines `opencode run --format json`
// prints: a step_start and then the answer as a text event. No step_finish.
func events(t *testing.T, text string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"type": "text", "sessionID": "ses_1", "part": map[string]any{"type": "text", "text": text}})
	if err != nil {
		t.Fatal(err)
	}
	return `{"type":"step_start","sessionID":"ses_1","part":{"type":"step-start"}}` + "\n" + string(b) + "\n"
}

// An OpenCode mission is reviewed by `opencode run --auto --format json` on
// the mission's own model and variant, with a null stdin and auto-update off,
// and the report is read out of the JSON events.
func TestRunReview_OpenCodeV2RunsOpenCodeOnTheMissionsModel(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	stub := newOpenCodeStub(t)
	stub.out(0, events(t, reportJSON("", "change.txt")))

	opts := Options{Branch: "b", Harness: "opencode", HarnessMajor: 2, TaskModel: "opencode/deepseek-v4.1-flash-free#high", TaskEffort: "low", Timeout: 20e9}
	sr, err := runReview(dir, "base", opts, "")
	if err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if !sr.Passed || stub.calls() != 1 {
		t.Fatalf("expected one passing opencode invocation, got passed=%v calls=%d", sr.Passed, stub.calls())
	}
	args := strings.Split(stub.args(1), "\n")
	want := []string{"run", "--auto", "--format", "json", "--model", "opencode/deepseek-v4.1-flash-free#high", "--"}
	if len(args) <= len(want) || strings.Join(args[:len(want)], "|") != strings.Join(want, "|") {
		t.Errorf("opencode argv starts %q, want %q then the prompt", args[:min(len(args), len(want))], want)
	}
	if strings.Contains(stub.args(1), "sonnet") || strings.Contains(stub.args(1), "low") && strings.Contains(stub.args(1), "--variant") {
		t.Errorf("the claude reviewer model or the effort label leaked into opencode's argv: %s", stub.args(1))
	}
	if !strings.Contains(stub.prompt(1), "Independent check of branch b against base") {
		t.Errorf("the reviewer's brief was not the last argument: %q", stub.prompt(1))
	}
	if got := stub.read("env", 1); got != "1" {
		t.Errorf("OPENCODE_DISABLE_AUTOUPDATE = %q in the child, want 1", got)
	}
	if got := stub.read("stdin", 1); got != "" {
		t.Errorf("stdin was not the null device, the child read %q", got)
	}
}

func TestRunReview_OpenCodeV1SplitsTheVariantIntoItsFlag(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	stub := newOpenCodeStub(t)
	stub.out(0, events(t, reportJSON("", "change.txt")))

	opts := Options{Branch: "b", Harness: "opencode", HarnessMajor: 1, TaskModel: "anthropic/claude-sonnet-5#high", Timeout: 20e9}
	if _, err := runReview(dir, "base", opts, ""); err != nil {
		t.Fatalf("runReview: %v", err)
	}
	args := strings.Split(stub.args(1), "\n")
	want := []string{"run", "--auto", "--format", "json", "--model", "anthropic/claude-sonnet-5", "--variant", "high", "--"}
	if len(args) <= len(want) || strings.Join(args[:len(want)], "|") != strings.Join(want, "|") {
		t.Errorf("opencode argv starts %q, want %q", args[:min(len(args), len(want))], want)
	}
}

// Exit 1 with a JSON error line on stdout is a failure that names the cause,
// never a pass and never an unreadable report.
func TestRunReview_OpenCodeErrorLineIsAFailure(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	stub := newOpenCodeStub(t)
	stub.out(1, `{"type":"error","sessionID":"ses_1","error":{"type":"provider.no-route","message":"Variant unavailable for opencode/x: high"}}`+"\n")
	stub.hook(1, "cat \"$d/out.1\"\nexit 1\n")

	opts := Options{Branch: "b", Harness: "opencode", HarnessMajor: 2, TaskModel: "opencode/x#high", Timeout: 20e9}
	_, err := runReview(dir, "base", opts, "")
	if err == nil || !strings.Contains(err.Error(), "Variant unavailable") {
		t.Fatalf("error = %v, want one naming the provider error", err)
	}
}

// An error event with exit code 0 is still a failure.
func TestRunReview_OpenCodeErrorEventWithExitZeroIsAFailure(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	stub := newOpenCodeStub(t)
	stub.out(0, `{"type":"error","error":{"message":"boom"}}`+"\n")

	_, err := runReview(dir, "base", Options{Branch: "b", Harness: "opencode", HarnessMajor: 2, Timeout: 20e9}, "")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v", err)
	}
}
