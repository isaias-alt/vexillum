package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/state"
)

// ghStub puts a fake "gh" binary on PATH (alongside a real git) that
// responds to the exact commands mergeShippedPR issues ("pr view", "pr
// checks", "pr merge"), so tests can simulate GitHub's pull request state
// without a real gh install or a live API call.
func ghStub(t *testing.T, script string) string {
	t.Helper()
	dir := gitOnlyPath(t)
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

func shippedTask(t *testing.T, home string) state.Task {
	t.Helper()
	task, err := state.New(state.KindMission, "do a thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.Status = state.StatusShipped
	task.CampBranch = "vexillum/abc123"
	if err := state.Save(home, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	return task
}

// A healthy, mergeable PR with green checks merges successfully.
func TestMergeShippedPR_Success(t *testing.T) {
	t.Setenv("PATH", ghStub(t, healthyPRView))

	var out bytes.Buffer
	code := mergeShippedPR(t.TempDir(), state.Task{ID: "t1", CampBranch: "vexillum/abc123"}, &out, &out)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "merged https://github.com/x/y/pull/42") {
		t.Errorf("expected output to confirm the merge, got: %s", out.String())
	}
}

// A closed (or already-merged) PR is refused, naming its state.
func TestMergeShippedPR_RefusesClosedPR(t *testing.T) {
	t.Setenv("PATH", ghStub(t, `case "$1 $2" in
  "pr view") echo '{"number":42,"state":"CLOSED","isDraft":false,"mergeable":"MERGEABLE","headRefOid":"abc123","url":"https://github.com/x/y/pull/42"}' ;;
esac`))

	var out bytes.Buffer
	code := mergeShippedPR(t.TempDir(), state.Task{ID: "t1", CampBranch: "vexillum/abc123"}, &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit for a closed pull request")
	}
	if !strings.Contains(out.String(), `state is "CLOSED"`) {
		t.Errorf("expected the refusal to name the state, got: %s", out.String())
	}
}

// A draft PR is refused.
func TestMergeShippedPR_RefusesDraft(t *testing.T) {
	t.Setenv("PATH", ghStub(t, `case "$1 $2" in
  "pr view") echo '{"number":42,"state":"OPEN","isDraft":true,"mergeable":"MERGEABLE","headRefOid":"abc123","url":"https://github.com/x/y/pull/42"}' ;;
esac`))

	var out bytes.Buffer
	code := mergeShippedPR(t.TempDir(), state.Task{ID: "t1", CampBranch: "vexillum/abc123"}, &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit for a draft pull request")
	}
	if !strings.Contains(out.String(), "draft") {
		t.Errorf("expected the refusal to mention the draft state, got: %s", out.String())
	}
}

// A PR with conflicts (mergeable != MERGEABLE) is refused.
func TestMergeShippedPR_RefusesNotMergeable(t *testing.T) {
	t.Setenv("PATH", ghStub(t, `case "$1 $2" in
  "pr view") echo '{"number":42,"state":"OPEN","isDraft":false,"mergeable":"CONFLICTING","headRefOid":"abc123","url":"https://github.com/x/y/pull/42"}' ;;
esac`))

	var out bytes.Buffer
	code := mergeShippedPR(t.TempDir(), state.Task{ID: "t1", CampBranch: "vexillum/abc123"}, &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit for a conflicting pull request")
	}
	if !strings.Contains(out.String(), "not mergeable") {
		t.Errorf("expected the refusal to mention mergeability, got: %s", out.String())
	}
}

// Red or pending checks refuse the merge before it's ever attempted.
func TestMergeShippedPR_RefusesRedChecks(t *testing.T) {
	t.Setenv("PATH", ghStub(t, `case "$1 $2" in
  "pr view") echo '{"number":42,"state":"OPEN","isDraft":false,"mergeable":"MERGEABLE","headRefOid":"abc123","url":"https://github.com/x/y/pull/42"}' ;;
  "pr checks") echo "lint  fail  https://example.com/run/1" >&2; exit 1 ;;
  "pr merge") echo "MERGE SHOULD NOT HAVE BEEN CALLED" >&2; exit 1 ;;
esac`))

	var out bytes.Buffer
	code := mergeShippedPR(t.TempDir(), state.Task{ID: "t1", CampBranch: "vexillum/abc123"}, &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit when checks are not green")
	}
	if !strings.Contains(out.String(), "checks are not all green") {
		t.Errorf("expected the refusal to mention checks, got: %s", out.String())
	}
	if strings.Contains(out.String(), "SHOULD NOT HAVE BEEN CALLED") {
		t.Error("merge must never be attempted when checks are red")
	}
}

// Without "gh" on PATH, mergeShippedPR fails clearly instead of a raw
// exec error.
func TestMergeShippedPR_RefusesWithoutGh(t *testing.T) {
	t.Setenv("PATH", gitOnlyPath(t))

	var out bytes.Buffer
	code := mergeShippedPR(t.TempDir(), state.Task{ID: "t1", CampBranch: "vexillum/abc123"}, &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit when gh isn't installed")
	}
	if !strings.Contains(out.String(), "'gh' is not installed") {
		t.Errorf("expected the error to say gh isn't installed, got: %s", out.String())
	}
}

// runLand on a shipped task merges the real PR instead of trying to
// fast-forward the camp - it never even resolves a camp (this task's
// CampSlot is left at its zero value, which would fail camp.Resolve).
func TestRunLand_ShippedTaskMergesPR(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	task := shippedTask(t, home)
	t.Setenv("PATH", ghStub(t, healthyPRView))

	var out bytes.Buffer
	code := runLand(project, home, task.ID, &out, &out)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "merged https://github.com/x/y/pull/42") {
		t.Errorf("expected output to confirm the PR merge, got: %s", out.String())
	}
}
