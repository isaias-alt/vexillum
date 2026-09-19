package camp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// initProjectRepo creates a git repo with a single commit on a branch
// named "main", regardless of this machine's init.defaultBranch config.
func initProjectRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitT(t, dir, "init", "-q")
	runGitT(t, dir, "symbolic-ref", "HEAD", "refs/heads/main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("test project\n"), 0o644); err != nil {
		t.Fatalf("writing README: %v", err)
	}
	runGitT(t, dir, "add", "README.md")
	runGitT(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "initial commit")
	return dir
}

func commitAll(t *testing.T, dir, message string) {
	t.Helper()
	runGitT(t, dir, "add", "-A")
	runGitT(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", message)
}

// L3-01: Acquire creates an isolated worktree on its own branch, without
// touching the project's own working tree.
func TestAcquire_CreatesIsolatedWorktree(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	c, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if info, err := os.Stat(c.Path); err != nil || !info.IsDir() {
		t.Fatalf("expected camp worktree at %s: %v", c.Path, err)
	}
	if c.Branch != "vexillum/task-1" {
		t.Errorf("expected branch vexillum/task-1, got %s", c.Branch)
	}

	branchInCamp := strings.TrimSpace(runGitT(t, c.Path, "rev-parse", "--abbrev-ref", "HEAD"))
	if branchInCamp != c.Branch {
		t.Errorf("expected camp to be on %s, got %s", c.Branch, branchInCamp)
	}

	branchInProject := strings.TrimSpace(runGitT(t, project, "rev-parse", "--abbrev-ref", "HEAD"))
	if branchInProject != "main" {
		t.Errorf("expected project working tree to stay on main, got %s", branchInProject)
	}
}

// L3-07: releasing a clean, landed camp returns its slot to the pool, and
// a later Acquire reuses that same slot/worktree instead of creating a
// new one.
func TestAcquireRelease_ReusesSlot(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	c1, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire 1: %v", err)
	}

	if err := os.WriteFile(filepath.Join(c1.Path, "change.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("writing change: %v", err)
	}
	commitAll(t, c1.Path, "task 1 change")
	runGitT(t, project, "merge", "--ff-only", c1.Branch)

	if err := Release(c1, "task-1"); err != nil {
		t.Fatalf("Release: %v", err)
	}

	c2, err := Acquire(project, home, "task-2")
	if err != nil {
		t.Fatalf("Acquire 2: %v", err)
	}
	if c2.Slot != c1.Slot {
		t.Errorf("expected slot reuse: slot 1 = %d, slot 2 = %d", c1.Slot, c2.Slot)
	}
	if c2.Path != c1.Path {
		t.Errorf("expected the same worktree path reused, got %s vs %s", c1.Path, c2.Path)
	}
}

// L3-05: Release refuses to return a camp with uncommitted changes.
func TestRelease_RefusesDirtyCamp(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	c, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Path, "untracked.txt"), []byte("oops\n"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	if err := Release(c, "task-1"); err == nil {
		t.Fatal("expected Release to refuse a dirty camp")
	}
}

// L3-06: Release refuses a camp whose branch has commits not yet landed
// on the base branch.
func TestRelease_RefusesUnlandedCamp(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	c, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Path, "change.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("writing change: %v", err)
	}
	commitAll(t, c.Path, "unlanded change")

	if err := Release(c, "task-1"); err == nil {
		t.Fatal("expected Release to refuse an unlanded camp")
	}
}

// L3-09: Release refuses when the caller isn't the slot's recorded owner.
func TestRelease_RefusesWrongOwner(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	c, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if err := Release(c, "someone-else"); err == nil {
		t.Fatal("expected Release to refuse a non-owning caller")
	}
}

// L3-08: a leased slot is never handed out to another task - Acquire
// creates a new one instead.
func TestAcquire_DoesNotReuseLeasedSlot(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	c1, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire 1: %v", err)
	}

	c2, err := Acquire(project, home, "task-2")
	if err != nil {
		t.Fatalf("Acquire 2: %v", err)
	}

	if c2.Slot == c1.Slot {
		t.Fatalf("expected a distinct slot while task-1's camp is still leased, got %d for both", c1.Slot)
	}
}
