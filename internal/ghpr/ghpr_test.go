package ghpr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ghStub puts a fake "gh" binary on PATH that responds to the exact
// commands MergeShipped issues ("pr view", "pr checks", "pr merge"), so
// tests can simulate GitHub's pull request state without a real gh
// install or a live API call.
func ghStub(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	body := "#!/bin/sh\n" + script + "\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(body), 0o755); err != nil {
		t.Fatalf("writing gh stub: %v", err)
	}
	return dir
}

const healthyPRView = `case "$1 $2" in
  "pr view") echo '{"number":42,"state":"OPEN","isDraft":false,"mergeable":"MERGEABLE","headRefOid":"abc123","url":"https://github.com/x/y/pull/42"}' ;;
  "pr checks") exit 0 ;;
  "pr merge") echo "merged" ;;
esac`

func TestInstalled(t *testing.T) {
	t.Setenv("PATH", ghStub(t, healthyPRView))
	if !Installed() {
		t.Error("expected Installed=true when a gh binary is on PATH")
	}

	t.Setenv("PATH", t.TempDir())
	if Installed() {
		t.Error("expected Installed=false when nothing is on PATH")
	}
}

func TestView_ParsesPullRequest(t *testing.T) {
	t.Setenv("PATH", ghStub(t, healthyPRView))

	pr, err := View(t.TempDir(), "vexillum/abc123")
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	if pr.Number != 42 || pr.State != "OPEN" || pr.Mergeable != "MERGEABLE" || pr.URL != "https://github.com/x/y/pull/42" {
		t.Errorf("unexpected pull request: %+v", pr)
	}
}

func TestMergeShipped_Success(t *testing.T) {
	t.Setenv("PATH", ghStub(t, healthyPRView))

	url, err := MergeShipped(t.TempDir(), "vexillum/abc123")
	if err != nil {
		t.Fatalf("MergeShipped: %v", err)
	}
	if url != "https://github.com/x/y/pull/42" {
		t.Errorf("expected the PR URL back, got %q", url)
	}
}

func TestMergeShipped_RefusesClosedPR(t *testing.T) {
	t.Setenv("PATH", ghStub(t, `case "$1 $2" in
  "pr view") echo '{"number":42,"state":"CLOSED","isDraft":false,"mergeable":"MERGEABLE","headRefOid":"abc123","url":"https://github.com/x/y/pull/42"}' ;;
esac`))

	_, err := MergeShipped(t.TempDir(), "vexillum/abc123")
	if err == nil {
		t.Fatal("expected an error for a closed pull request")
	}
	if !strings.Contains(err.Error(), `state is "CLOSED"`) {
		t.Errorf("expected the error to name the state, got: %v", err)
	}
}

func TestMergeShipped_RefusesDraft(t *testing.T) {
	t.Setenv("PATH", ghStub(t, `case "$1 $2" in
  "pr view") echo '{"number":42,"state":"OPEN","isDraft":true,"mergeable":"MERGEABLE","headRefOid":"abc123","url":"https://github.com/x/y/pull/42"}' ;;
esac`))

	_, err := MergeShipped(t.TempDir(), "vexillum/abc123")
	if err == nil {
		t.Fatal("expected an error for a draft pull request")
	}
	if !strings.Contains(err.Error(), "draft") {
		t.Errorf("expected the error to mention the draft state, got: %v", err)
	}
}

func TestMergeShipped_RefusesNotMergeable(t *testing.T) {
	t.Setenv("PATH", ghStub(t, `case "$1 $2" in
  "pr view") echo '{"number":42,"state":"OPEN","isDraft":false,"mergeable":"CONFLICTING","headRefOid":"abc123","url":"https://github.com/x/y/pull/42"}' ;;
esac`))

	_, err := MergeShipped(t.TempDir(), "vexillum/abc123")
	if err == nil {
		t.Fatal("expected an error for a conflicting pull request")
	}
	if !strings.Contains(err.Error(), "not mergeable") {
		t.Errorf("expected the error to mention mergeability, got: %v", err)
	}
}

func TestMergeShipped_RefusesRedChecksBeforeMerging(t *testing.T) {
	t.Setenv("PATH", ghStub(t, `case "$1 $2" in
  "pr view") echo '{"number":42,"state":"OPEN","isDraft":false,"mergeable":"MERGEABLE","headRefOid":"abc123","url":"https://github.com/x/y/pull/42"}' ;;
  "pr checks") echo "lint  fail  https://example.com/run/1" >&2; exit 1 ;;
  "pr merge") echo "MERGE SHOULD NOT HAVE BEEN CALLED" >&2; exit 1 ;;
esac`))

	_, err := MergeShipped(t.TempDir(), "vexillum/abc123")
	if err == nil {
		t.Fatal("expected an error when checks are not green")
	}
	if !strings.Contains(err.Error(), "checks are not all green") {
		t.Errorf("expected the error to mention checks, got: %v", err)
	}
	if strings.Contains(err.Error(), "SHOULD NOT HAVE BEEN CALLED") {
		t.Error("merge must never be attempted when checks are red")
	}
}
