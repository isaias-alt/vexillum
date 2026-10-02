package ghpr

import (
	"errors"
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

func TestCreate_ReturnsTheNewPullRequestURL(t *testing.T) {
	t.Setenv("PATH", ghStub(t, `case "$1 $2" in
  "pr create") echo 'https://github.com/x/y/pull/7' ;;
esac`))

	url, err := Create(t.TempDir(), "vexillum/abc123", "main", "a title", "a body")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if url != "https://github.com/x/y/pull/7" {
		t.Errorf("expected the new PR URL back, got %q", url)
	}
}

// gh pr create sometimes prints informational lines before the URL (e.g.
// a note about an existing draft it reused) - only the last line is the
// URL.
func TestCreate_TakesTheLastLineWhenGhPrintsExtraOutput(t *testing.T) {
	t.Setenv("PATH", ghStub(t, `case "$1 $2" in
  "pr create") printf 'Some note from gh\nhttps://github.com/x/y/pull/7\n' ;;
esac`))

	url, err := Create(t.TempDir(), "vexillum/abc123", "", "a title", "a body")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if url != "https://github.com/x/y/pull/7" {
		t.Errorf("expected only the last line as the URL, got %q", url)
	}
}

func TestCreate_ReportsFailure(t *testing.T) {
	t.Setenv("PATH", ghStub(t, `case "$1 $2" in
  "pr create") echo "a pull request for branch vexillum/abc123 already exists" >&2; exit 1 ;;
esac`))

	_, err := Create(t.TempDir(), "vexillum/abc123", "", "a title", "a body")
	if err == nil {
		t.Fatal("expected an error when gh pr create fails")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected gh's own error output surfaced, got: %v", err)
	}
}

func TestView_ParsesTheMergeCommitOfAMergedPullRequest(t *testing.T) {
	t.Setenv("PATH", ghStub(t, `case "$1 $2" in
  "pr view") echo '{"number":42,"state":"MERGED","isDraft":false,"mergeable":"UNKNOWN","headRefOid":"abc123","url":"https://github.com/x/y/pull/42","mergeCommit":{"oid":"def456"}}' ;;
esac`))

	pr, err := View(t.TempDir(), "vexillum/abc123")
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	if pr.State != StateMerged || pr.MergeCommit == nil || pr.MergeCommit.Oid != "def456" {
		t.Errorf("expected the merge commit parsed, got %+v", pr)
	}
}

func TestView_OpenPullRequestHasNoMergeCommit(t *testing.T) {
	t.Setenv("PATH", ghStub(t, `echo '{"number":42,"state":"OPEN","isDraft":false,"mergeable":"MERGEABLE","headRefOid":"abc123","url":"u","mergeCommit":null}'`))

	pr, err := View(t.TempDir(), "vexillum/abc123")
	if err != nil || pr.MergeCommit != nil {
		t.Errorf("expected no merge commit for an open PR, got %+v err=%v", pr, err)
	}
}

func TestView_NoPullRequestIsATypedError(t *testing.T) {
	t.Setenv("PATH", ghStub(t, `echo 'no pull requests found for branch "vexillum/abc123"' >&2; exit 1`))
	if _, err := View(t.TempDir(), "vexillum/abc123"); !errors.Is(err, ErrNoPullRequest) {
		t.Errorf("expected ErrNoPullRequest, got: %v", err)
	}

	t.Setenv("PATH", ghStub(t, `echo 'HTTP 502' >&2; exit 1`))
	if _, err := View(t.TempDir(), "vexillum/abc123"); err == nil || errors.Is(err, ErrNoPullRequest) {
		t.Errorf("expected a plain error for any other failure, got: %v", err)
	}
}

func TestLoggedIn(t *testing.T) {
	t.Setenv("PATH", ghStub(t, `[ "$1 $2" = "auth status" ] && exit 0; exit 2`))
	if !LoggedIn() {
		t.Error("expected LoggedIn when gh auth status succeeds")
	}
	t.Setenv("PATH", ghStub(t, `exit 1`))
	if LoggedIn() {
		t.Error("expected not LoggedIn when gh auth status fails")
	}
	t.Setenv("PATH", t.TempDir())
	if LoggedIn() {
		t.Error("expected not LoggedIn without a gh binary")
	}
}

func TestEdit_PassesNumberTitleAndBody(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("PATH", ghStub(t, `printf '%s\n' "$@" > '`+argsFile+`'`))

	if err := Edit(t.TempDir(), 7, "feat: x", "the body"); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	got, _ := os.ReadFile(argsFile)
	if string(got) != "pr\nedit\n7\n--title\nfeat: x\n--body\nthe body\n" {
		t.Errorf("unexpected gh arguments: %q", got)
	}

	t.Setenv("PATH", ghStub(t, `echo nope >&2; exit 1`))
	if err := Edit(t.TempDir(), 7, "t", "b"); err == nil || !strings.Contains(err.Error(), "#7") {
		t.Errorf("expected an error naming the pull request, got: %v", err)
	}
}
