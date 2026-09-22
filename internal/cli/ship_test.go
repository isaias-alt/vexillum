package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/state"
)

// gatedTestProject is initDispatchTestProject plus a "no-mistakes" remote
// pointing at a real bare repo, so a real "git push no-mistakes <branch>"
// can succeed without the real no-mistakes binary or a real GitHub remote.
func gatedTestProject(t *testing.T) string {
	t.Helper()
	project := initDispatchTestProject(t)

	bareRepo := filepath.Join(t.TempDir(), "gate.git")
	cmd := exec.Command("git", "init", "--bare", "-q", bareRepo)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	cmd = exec.Command("git", "remote", "add", "no-mistakes", bareRepo)
	cmd.Dir = project
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add no-mistakes: %v\n%s", err, out)
	}
	return project
}

func doneMissionTask(t *testing.T, project, home string) state.Task {
	t.Helper()
	task, err := state.New(state.KindMission, "do a thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	c, err := camp.Acquire(project, home, task.ID)
	if err != nil {
		t.Fatalf("camp.Acquire: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Path, "change.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("writing change: %v", err)
	}
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = c.Path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "change")
	cmd.Dir = c.Path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	task.CampSlot = c.Slot
	task.CampPath = c.Path
	task.CampBranch = c.Branch
	task.Status = state.StatusDone
	if err := state.Save(home, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	return task
}

// B4-04: shipping a done mission in a gated project pushes its camp branch
// to the "no-mistakes" remote.
func TestRunShip_Success(t *testing.T) {
	project := gatedTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)

	var out bytes.Buffer
	code := runShip(project, home, task.ID, &out, &out)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "pushed "+task.CampBranch) {
		t.Errorf("expected output to confirm the push, got: %s", out.String())
	}
}

// B4-05: ship refuses a task that isn't done yet.
func TestRunShip_RefusesNotDone(t *testing.T) {
	project := gatedTestProject(t)
	home := t.TempDir()

	task, err := state.New(state.KindMission, "do a thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.Status = state.StatusRunning
	if err := state.Save(home, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	var out bytes.Buffer
	code := runShip(project, home, task.ID, &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit for a task that isn't done")
	}
	if !strings.Contains(out.String(), "not done") {
		t.Errorf("expected the error to name why it refused, got: %s", out.String())
	}
}

// B4-06: ship refuses a scout - it should never have anything to ship.
func TestRunShip_RefusesScout(t *testing.T) {
	project := gatedTestProject(t)
	home := t.TempDir()

	task, err := state.New(state.KindScout, "look into it")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.Status = state.StatusDone
	if err := state.Save(home, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	var out bytes.Buffer
	code := runShip(project, home, task.ID, &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit for a scout")
	}
	if !strings.Contains(out.String(), "scout") {
		t.Errorf("expected the error to name why it refused, got: %s", out.String())
	}
}

// gitOnlyPath returns a PATH containing nothing but a real git - used to
// deterministically guarantee "no-mistakes" is NOT found on PATH,
// regardless of what's installed on the machine running the test.
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

// noMistakesStub puts a fake "no-mistakes" binary on PATH (alongside a
// real git) whose "init" subcommand runs script - so tests can simulate
// a successful or failing gate setup without the real no-mistakes CLI.
func noMistakesStub(t *testing.T, initScript string) string {
	t.Helper()
	dir := gitOnlyPath(t)
	body := "#!/bin/sh\nif [ \"$1\" = \"init\" ]; then\n" + initScript + "\nfi\n"
	if err := os.WriteFile(filepath.Join(dir, "no-mistakes"), []byte(body), 0o755); err != nil {
		t.Fatalf("writing no-mistakes stub: %v", err)
	}
	return dir
}

// B4-07: ship refuses when "no-mistakes" isn't installed at all - no
// silent auto-gate attempt without the real binary to do it with.
func TestRunShip_RefusesWhenNoMistakesNotInstalled(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	t.Setenv("PATH", gitOnlyPath(t))

	var out bytes.Buffer
	code := runShip(project, home, task.ID, &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit when no-mistakes isn't installed")
	}
	if !strings.Contains(out.String(), "is not installed") {
		t.Errorf("expected the error to say no-mistakes isn't installed, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "vexillum land") {
		t.Errorf("expected the error to point at 'vexillum land' as the alternative, got: %s", out.String())
	}
}

// B4-08: ship auto-gates an ungated project on first use (runs
// "no-mistakes init" itself) instead of refusing, then ships normally -
// the general never has to remember a separate setup step.
func TestRunShip_AutoGatesOnFirstShip(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)

	bareRepo := filepath.Join(t.TempDir(), "gate.git")
	cmd := exec.Command("git", "init", "--bare", "-q", bareRepo)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	t.Setenv("PATH", noMistakesStub(t, "  git remote add no-mistakes "+bareRepo))

	var out bytes.Buffer
	code := runShip(project, home, task.ID, &out, &out)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "running 'no-mistakes init'") {
		t.Errorf("expected output to mention auto-gating, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "pushed "+task.CampBranch) {
		t.Errorf("expected output to confirm the push after gating, got: %s", out.String())
	}
}

// B4-09: a failed auto-gate attempt is reported clearly and never
// attempts the push.
func TestRunShip_AutoGateFailureIsReported(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	t.Setenv("PATH", noMistakesStub(t, "  echo 'no origin remote configured' >&2\n  exit 1"))

	var out bytes.Buffer
	code := runShip(project, home, task.ID, &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit when 'no-mistakes init' fails")
	}
	if !strings.Contains(out.String(), "'no-mistakes init' failed") {
		t.Errorf("expected the failure to be reported, got: %s", out.String())
	}
	if strings.Contains(out.String(), "pushed ") {
		t.Errorf("expected no push attempt after a failed auto-gate, got: %s", out.String())
	}
}
