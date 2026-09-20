package soldier_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/soldier"
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

func initReleaseTestProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitT(t, dir, "init", "-q")
	runGitT(t, dir, "symbolic-ref", "HEAD", "refs/heads/main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatalf("writing README: %v", err)
	}
	runGitT(t, dir, "add", "README.md")
	runGitT(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "initial commit")
	return dir
}

// L4-06: releasing a landed, clean camp closes the soldier's herdr tab -
// at the same moment as the worktree returns to the pool, matching
// firstmate's own teardown timing.
func TestReleaseInHerdr_ClosesTabOnceCampReleases(t *testing.T) {
	project := initReleaseTestProject(t)
	home := t.TempDir()
	task := newMissionTask(t)

	c, err := camp.Acquire(project, home, task.ID)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Path, "change.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("writing change: %v", err)
	}
	runGitT(t, c.Path, "add", "-A")
	runGitT(t, c.Path, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "change")
	runGitT(t, project, "merge", "--ff-only", c.Branch)

	task.HerdrTabID = "w1:t5"
	client := &fakeHerdr{}

	if err := soldier.ReleaseInHerdr(task, c, client); err != nil {
		t.Fatalf("ReleaseInHerdr: %v", err)
	}
	if len(client.tabCloseCalls) != 1 || client.tabCloseCalls[0] != "w1:t5" {
		t.Errorf("expected TabClose(w1:t5) exactly once, got %v", client.tabCloseCalls)
	}
}

// L4-07: camp.Release's own safety refusals (dirty, unlanded, wrong
// owner) win - the pane stays open when the camp can't actually be
// returned.
func TestReleaseInHerdr_KeepsTabOpenWhenCampRefuses(t *testing.T) {
	project := initReleaseTestProject(t)
	home := t.TempDir()
	task := newMissionTask(t)

	c, err := camp.Acquire(project, home, task.ID)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Path, "dirty.txt"), []byte("oops\n"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	task.HerdrTabID = "w1:t5"
	client := &fakeHerdr{}

	if err := soldier.ReleaseInHerdr(task, c, client); err == nil {
		t.Fatal("expected ReleaseInHerdr to fail for a dirty camp")
	}
	if len(client.tabCloseCalls) != 0 {
		t.Errorf("expected the pane to stay open when release is refused, but TabClose was called: %v", client.tabCloseCalls)
	}
}
