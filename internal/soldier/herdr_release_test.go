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

	if err := soldier.ReleaseInHerdr(task, c, client, t.TempDir()); err != nil {
		t.Fatalf("ReleaseInHerdr: %v", err)
	}
	if len(client.tabCloseCalls) != 1 || client.tabCloseCalls[0] != "w1:t5" {
		t.Errorf("expected TabClose(w1:t5) exactly once, got %v", client.tabCloseCalls)
	}
}

// L4-07 / B3-08: camp.Release's own safety refusals (dirty, unlanded,
// wrong owner) win - neither the pane nor an orphaned browser bridge is
// touched when the camp can't actually be returned.
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

	homeDir := t.TempDir()
	sessionDir := filepath.Join(homeDir, ".chrome-devtools-axi", "sessions", "vx-"+task.ID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("creating session dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "bridge.pid"), []byte("1 2\n"), 0o644); err != nil {
		t.Fatalf("writing bridge.pid: %v", err)
	}
	logFile := filepath.Join(t.TempDir(), "npx.log")
	npxDir := t.TempDir()
	script := "#!/bin/sh\necho \"$@ $CHROME_DEVTOOLS_AXI_SESSION\" >> " + logFile + "\n"
	if err := os.WriteFile(filepath.Join(npxDir, "npx"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing npx stub: %v", err)
	}
	t.Setenv("PATH", npxDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := soldier.ReleaseInHerdr(task, c, client, homeDir); err == nil {
		t.Fatal("expected ReleaseInHerdr to fail for a dirty camp")
	}
	if len(client.tabCloseCalls) != 0 {
		t.Errorf("expected the pane to stay open when release is refused, but TabClose was called: %v", client.tabCloseCalls)
	}
	if _, err := os.ReadFile(logFile); err == nil {
		t.Error("expected the orphaned browser bridge to stay untouched when release is refused, but npx was invoked")
	}
}

// B3-06 (mirrors B3-03 for the non-destructive path): ReleaseInHerdr stops
// a soldier's orphaned browser bridge, if one was ever started, once
// camp.Release actually succeeds.
func TestReleaseInHerdr_StopsOrphanBrowser(t *testing.T) {
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

	homeDir := t.TempDir()
	sessionDir := filepath.Join(homeDir, ".chrome-devtools-axi", "sessions", "vx-"+task.ID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("creating session dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "bridge.pid"), []byte("1 2\n"), 0o644); err != nil {
		t.Fatalf("writing bridge.pid: %v", err)
	}
	logFile := filepath.Join(t.TempDir(), "npx.log")
	npxDir := t.TempDir()
	script := "#!/bin/sh\necho \"$@ $CHROME_DEVTOOLS_AXI_SESSION\" >> " + logFile + "\n"
	if err := os.WriteFile(filepath.Join(npxDir, "npx"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing npx stub: %v", err)
	}
	t.Setenv("PATH", npxDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	client := &fakeHerdr{}
	if err := soldier.ReleaseInHerdr(task, c, client, homeDir); err != nil {
		t.Fatalf("ReleaseInHerdr: %v", err)
	}

	out, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("expected the orphaned browser bridge to be stopped, but npx was never invoked: %v", err)
	}
	if !strings.Contains(string(out), "vx-"+task.ID) {
		t.Errorf("expected the stop invocation scoped to this task's session, got: %q", out)
	}
}

// B3-07 (mirrors B3-04 for the non-destructive path): ReleaseInHerdr never
// fails just because there's nothing to stop - the common case, where a
// soldier never touched a browser.
func TestReleaseInHerdr_NoOrphanBrowserIsFine(t *testing.T) {
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

	client := &fakeHerdr{}
	if err := soldier.ReleaseInHerdr(task, c, client, t.TempDir()); err != nil {
		t.Fatalf("ReleaseInHerdr: %v", err)
	}
}

// B3-03: DiscardInHerdr stops a dead soldier's orphaned browser bridge, if
// one was ever started - PRD v2, B.3's requisito derivado on A.2.
func TestDiscardInHerdr_StopsOrphanBrowser(t *testing.T) {
	project := initReleaseTestProject(t)
	home := t.TempDir()
	task := newMissionTask(t)

	c, err := camp.Acquire(project, home, task.ID)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	homeDir := t.TempDir()
	sessionDir := filepath.Join(homeDir, ".chrome-devtools-axi", "sessions", "vx-"+task.ID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("creating session dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "bridge.pid"), []byte("1 2\n"), 0o644); err != nil {
		t.Fatalf("writing bridge.pid: %v", err)
	}
	logFile := filepath.Join(t.TempDir(), "npx.log")
	npxDir := t.TempDir()
	script := "#!/bin/sh\necho \"$@ $CHROME_DEVTOOLS_AXI_SESSION\" >> " + logFile + "\n"
	if err := os.WriteFile(filepath.Join(npxDir, "npx"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing npx stub: %v", err)
	}
	t.Setenv("PATH", npxDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	client := &fakeHerdr{}
	if err := soldier.DiscardInHerdr(task, c, client, homeDir); err != nil {
		t.Fatalf("DiscardInHerdr: %v", err)
	}

	out, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("expected the orphaned browser bridge to be stopped, but npx was never invoked: %v", err)
	}
	if !strings.Contains(string(out), "vx-"+task.ID) {
		t.Errorf("expected the stop invocation scoped to this task's session, got: %q", out)
	}
}

// B3-04: DiscardInHerdr never fails just because there's nothing to stop -
// the common case, where a soldier never touched a browser.
func TestDiscardInHerdr_NoOrphanBrowserIsFine(t *testing.T) {
	project := initReleaseTestProject(t)
	home := t.TempDir()
	task := newMissionTask(t)

	c, err := camp.Acquire(project, home, task.ID)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	client := &fakeHerdr{}
	if err := soldier.DiscardInHerdr(task, c, client, t.TempDir()); err != nil {
		t.Fatalf("DiscardInHerdr: %v", err)
	}
}
