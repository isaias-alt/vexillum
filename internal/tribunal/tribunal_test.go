package tribunal

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// newTribunalRepo creates a git repo with one commit on "main", tags
// that commit as "base" (the fork point a camp's own base branch would
// resolve to), and returns the repo's directory - callers add further
// commits on top of "main" to simulate a mission's own camp changes, then
// call tribunal functions with base="base".
func newTribunalRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "init", "-q")
	run(t, dir, "symbolic-ref", "HEAD", "refs/heads/main")
	run(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "--allow-empty", "-m", "base")
	run(t, dir, "branch", "base")
	return dir
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// commitFile writes name/content into dir and commits it on the current
// branch - the mission-side change a tribunal step is meant to see.
func commitFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	run(t, dir, "add", "-A")
	run(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "change "+name)
}

// gitOnlyPath returns a PATH containing nothing but a real git - used to
// prove a step never shells out to "claude" (or anything else) when it
// isn't supposed to: if it tried, the exec would fail with "executable
// file not found" instead of silently succeeding against whatever
// happens to be installed on the machine running the test.
func gitOnlyPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("git not found on PATH: %v", err)
	}
	if err := os.Symlink(realGit, filepath.Join(dir, "git")); err != nil {
		t.Fatalf("linking real git: %v", err)
	}
	return dir
}

// fakeClaude puts a fake "claude" binary on PATH (alongside a real git)
// whose stdout is fixed to output, regardless of the args it's invoked
// with - so runReview can be tested without a real Claude Code CLI or API
// quota, the same stubbing technique internal/cli's ship_test.go already
// uses for external tools.
func fakeClaude(t *testing.T, output string) string {
	t.Helper()
	dir := gitOnlyPath(t)

	// The stub script below needs "cat" to print its fixed output -
	// symlinked in for the same reason git is: a PATH scoped to exactly
	// what the stub needs, nothing implicitly inherited from the machine
	// running the test.
	realCat, err := exec.LookPath("cat")
	if err != nil {
		t.Fatalf("cat not found on PATH: %v", err)
	}
	if err := os.Symlink(realCat, filepath.Join(dir, "cat")); err != nil {
		t.Fatalf("linking real cat: %v", err)
	}

	script := "#!/bin/sh\ncat <<'TRIBUNAL_EOF'\n" + output + "\nTRIBUNAL_EOF\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing claude stub: %v", err)
	}
	return dir
}

func TestRun_StopsAtFirstFailingStep(t *testing.T) {
	dir := newTribunalRepo(t)
	// No go.mod/package.json: lint and tests both skip (pass), so review
	// is the first real step - and a broken claude stub fails it,
	// meaning docs must never run.
	commitFile(t, dir, "change.txt", "hi\n")
	t.Setenv("PATH", fakeClaude(t, "nothing useful here"))

	result, err := Run(dir, "base")
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
	t.Setenv("PATH", fakeClaude(t, "looks fine.\n"+VerdictPrefix+" PASS"))

	result, err := Run(dir, "base")
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

func TestRunCommand_MissingBinaryReportsExecErrorAsDetail(t *testing.T) {
	sr, err := runCommand(StepLint, t.TempDir(), "vexillum-tribunal-does-not-exist")
	if err != nil {
		t.Fatalf("runCommand: %v", err)
	}
	if sr.Passed {
		t.Fatal("expected a missing binary to fail the step")
	}
	if sr.Detail == "" {
		t.Error("expected the exec error itself as Detail when the command never produced output")
	}
}

func TestHasFile(t *testing.T) {
	dir := t.TempDir()
	if hasFile(dir, "go.mod") {
		t.Error("expected hasFile=false for a missing file")
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	if !hasFile(dir, "go.mod") {
		t.Error("expected hasFile=true once the file exists")
	}
}
