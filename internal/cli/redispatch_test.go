package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/pause"
	vxproject "github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/report"
	"github.com/isaias-alt/vexillum/internal/state"
)

// interruptedTestTask acquires a real camp for a fresh task, commits an
// unlanded change into it (the dead soldier's leftover work), and
// persists the task as StatusInterrupted with that camp/herdr state
// attached - the shape a real Interrupted task left by sentinel.Tick
// actually has.
func interruptedTestTask(t *testing.T, project, home string) state.Task {
	t.Helper()
	task, err := state.New(state.KindMission, "do a thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	c, err := camp.Acquire(project, home, task.ID)
	if err != nil {
		t.Fatalf("camp.Acquire: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Path, "unlanded.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatalf("writing unlanded change: %v", err)
	}
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = c.Path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "unlanded work")
	cmd.Dir = c.Path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	task.CampSlot = c.Slot
	task.CampPath = c.Path
	task.CampBranch = c.Branch
	task.HerdrWorkspaceID = "w1"
	task.HerdrTabID = "w1:t1"
	task.HerdrPaneID = "w1:p1"
	task.HerdrAgentName = "vx-do-a-thing"
	task.Status = state.StatusInterrupted
	task.AgentNotFoundSince = time.Now().UTC().Add(-time.Hour)
	task.Output = "[vexillum] this soldier's herdr agent disappeared"
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	return task
}

// interruptedTestScoutTask mirrors interruptedTestTask but for a scout -
// the only kind that ever has a report to clean up on redispatch (see
// internal/report's package doc).
func interruptedTestScoutTask(t *testing.T, project, home string) state.Task {
	t.Helper()
	task, err := state.New(state.KindScout, "look into it")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	c, err := camp.Acquire(project, home, task.ID)
	if err != nil {
		t.Fatalf("camp.Acquire: %v", err)
	}

	task.CampSlot = c.Slot
	task.CampPath = c.Path
	task.CampBranch = c.Branch
	task.HerdrWorkspaceID = "w1"
	task.HerdrTabID = "w1:t1"
	task.HerdrPaneID = "w1:p1"
	task.HerdrAgentName = "vx-look-into-it"
	task.Status = state.StatusInterrupted
	task.AgentNotFoundSince = time.Now().UTC().Add(-time.Hour)
	task.Output = "[vexillum] this soldier's herdr agent disappeared"
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	return task
}

// A2-06 (this mission): redispatching an interrupted scout deletes its
// dead attempt's leftover report before relaunching - the fresh run
// reuses the same, unchanged prompt, so its candidate agent name will
// very likely be identical, and a stale report sitting at that exact
// path could otherwise be mistaken for the new attempt's own.
func TestRunRedispatch_CleansUpStaleReport(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()
	homeDir := t.TempDir()

	task := interruptedTestScoutTask(t, project, home)

	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	if err := os.MkdirAll(report.Dir(projectRoot), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	staleReport := report.Path(projectRoot, task.HerdrAgentName, task.ID)
	if err := os.WriteFile(staleReport, []byte("# stale findings from the dead soldier\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	client := &fakeHerdr{tabID: "w2:t2", paneID: "w2:p2", promptStatus: "done", readOutput: "did the thing"}

	var out bytes.Buffer
	code := runRedispatch(project, home, homeDir, "w1", task.ID, client, &out, &out)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	if _, err := os.Stat(staleReport); !os.IsNotExist(err) {
		t.Errorf("expected the stale report to be removed before relaunching, stat error: %v", err)
	}
}

// Same reasoning as TestRunRedispatch_CleansUpStaleReport, applied to a
// dead soldier's leftover declared pause: a stale pause file at the fresh
// run's likely-identical candidate agent name must not be mistaken for
// the new attempt's own declaration.
func TestRunRedispatch_CleansUpStalePause(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()
	homeDir := t.TempDir()

	task := interruptedTestTask(t, project, home)

	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	if err := os.MkdirAll(pause.Dir(projectRoot), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	stalePause := pause.Path(projectRoot, task.HerdrAgentName)
	if err := os.WriteFile(stalePause, []byte("paused: waiting on a background job that's now dead\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	client := &fakeHerdr{tabID: "w2:t2", paneID: "w2:p2", promptStatus: "done", readOutput: "did the thing"}

	var out bytes.Buffer
	code := runRedispatch(project, home, homeDir, "w1", task.ID, client, &out, &out)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	if _, err := os.Stat(stalePause); !os.IsNotExist(err) {
		t.Errorf("expected the stale pause to be removed before relaunching, stat error: %v", err)
	}
}

// A malformed task id must never reach state.Load/camp.Resolve's
// filepath.Join calls - runRedispatch rejects it up front.
func TestRunRedispatch_RejectsInvalidTaskID(t *testing.T) {
	var out bytes.Buffer
	code := runRedispatch("/does/not/matter", "/does/not/matter", "/does/not/matter", "w1", "../../etc/passwd", &fakeHerdr{}, &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit for an invalid task id")
	}
	if !strings.Contains(out.String(), "invalid task id") {
		t.Errorf("expected the error to name the invalid task id, got: %s", out.String())
	}
}

// A2-03: redispatch refuses a task that isn't interrupted, without
// touching its camp.
func TestRunRedispatch_RefusesNonInterrupted(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()
	homeDir := t.TempDir()

	task, err := state.New(state.KindMission, "do a thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.Status = state.StatusDone
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	var out bytes.Buffer
	code := runRedispatch(project, home, homeDir, "w1", task.ID, &fakeHerdr{}, &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit for a non-interrupted task")
	}
	if !strings.Contains(out.String(), "not interrupted") {
		t.Errorf("expected the error to name why it refused, got: %s", out.String())
	}

	reloaded, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if reloaded.Status != state.StatusDone || reloaded.Redispatches != 0 {
		t.Errorf("expected the task to be untouched, got status=%s redispatches=%d", reloaded.Status, reloaded.Redispatches)
	}
}

// A2-04: redispatching an interrupted task discards its old camp, resets
// it in place (same task id), bumps Redispatches, clears
// AgentNotFoundSince, and runs it fresh through a new camp/pane.
func TestRunRedispatch_Success(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()
	homeDir := t.TempDir()

	task := interruptedTestTask(t, project, home)

	client := &fakeHerdr{tabID: "w2:t2", paneID: "w2:p2", promptStatus: "done", readOutput: "did the thing"}

	var out bytes.Buffer
	code := runRedispatch(project, home, homeDir, "w1", task.ID, client, &out, &out)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "redispatch #1") {
		t.Errorf("expected output to name the redispatch count, got: %s", out.String())
	}

	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	reloaded, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if reloaded.ID != task.ID {
		t.Fatalf("expected the same task id, got %s", reloaded.ID)
	}
	if reloaded.Redispatches != 1 {
		t.Errorf("expected Redispatches=1, got %d", reloaded.Redispatches)
	}
	if !reloaded.AgentNotFoundSince.IsZero() {
		t.Errorf("expected AgentNotFoundSince cleared, got %v", reloaded.AgentNotFoundSince)
	}
	if reloaded.Status != state.StatusDone {
		t.Errorf("expected the fresh run to settle to done, got %s", reloaded.Status)
	}

	// Same task id means Acquire rebuilds the same branch name
	// ("vexillum/<task-id>") - that's expected, it's a brand new branch
	// off the current base, not the one Discard deleted. What matters is
	// that the dead soldier's unlanded commit is gone from the new camp.
	if _, err := os.Stat(filepath.Join(reloaded.CampPath, "unlanded.txt")); err == nil {
		t.Errorf("expected the discarded commit's file to be gone from the new camp")
	}
}

// A2-05: a TabClose failure on the old, already-dead pane never blocks the
// redispatch - there's nothing left on herdr's side to clean up either
// way.
func TestRunRedispatch_SucceedsDespiteTabCloseFailure(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()
	homeDir := t.TempDir()

	task := interruptedTestTask(t, project, home)

	client := &fakeHerdr{
		tabID: "w2:t2", paneID: "w2:p2", promptStatus: "done", readOutput: "did the thing",
		tabCloseErr: errors.New("tab already gone"),
	}

	var out bytes.Buffer
	code := runRedispatch(project, home, homeDir, "w1", task.ID, client, &out, &out)

	if code != 0 {
		t.Fatalf("expected exit 0 despite the old tab's close failing, got %d: %s", code, out.String())
	}
}

// B3-05: an orphaned chrome-devtools-axi bridge left by the dead soldier
// (PRD v2, B.3's requisito derivado on A.2) doesn't block the redispatch
// either - it's stopped as part of discarding the old camp, best-effort,
// same as the old herdr tab.
func TestRunRedispatch_StopsOrphanBrowser(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()
	homeDir := t.TempDir()

	task := interruptedTestTask(t, project, home)

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

	client := &fakeHerdr{tabID: "w2:t2", paneID: "w2:p2", promptStatus: "done", readOutput: "did the thing"}

	var out bytes.Buffer
	code := runRedispatch(project, home, homeDir, "w1", task.ID, client, &out, &out)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}

	log, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("expected the orphaned browser bridge to be stopped, but npx was never invoked: %v", err)
	}
	if !strings.Contains(string(log), "vx-"+task.ID) {
		t.Errorf("expected the stop invocation scoped to this task's session, got: %q", log)
	}
}
