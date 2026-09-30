package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/checkpoint"
	vxproject "github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/state"
)

// shipTestProject is initDispatchTestProject plus a real "origin" remote
// pointing at a real bare repo, so a real "git push origin <branch>" can
// succeed without a real GitHub remote.
func shipTestProject(t *testing.T) string {
	t.Helper()
	project := initDispatchTestProject(t)

	bareRepo := filepath.Join(t.TempDir(), "origin.git")
	cmd := exec.Command("git", "init", "--bare", "-q", bareRepo)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	cmd = exec.Command("git", "remote", "add", "origin", bareRepo)
	cmd.Dir = project
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add origin: %v\n%s", err, out)
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
	task.CampBase = c.Base
	task.Status = state.StatusDone
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	return task
}

// A malformed task id must never reach state.Load/camp.Resolve's
// filepath.Join calls - runShip rejects it up front.
func TestRunShip_RejectsInvalidTaskID(t *testing.T) {
	var out bytes.Buffer
	code := runShip("/does/not/matter", "/does/not/matter", "../../etc/passwd", &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit for an invalid task id")
	}
	if !strings.Contains(out.String(), "invalid task id") {
		t.Errorf("expected the error to name the invalid task id, got: %s", out.String())
	}
}

// gitOnlyPath returns a PATH containing nothing but a real git - used to
// deterministically guarantee neither "claude" nor "gh" is found on
// PATH, regardless of what's installed on the machine running the test.
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

// shipToolsPath returns a PATH with a real git, a fake "claude" whose
// review verdict is fixed to claudeVerdict regardless of the prompt it's
// given, and a fake "gh" driven by ghScript - so ship's checkpoint
// pipeline (internal/checkpoint's review step) and its own "gh pr
// create"/"gh pr view" calls can be exercised without a real Claude Code
// API call or a real GitHub remote.
func shipToolsPath(t *testing.T, claudeVerdict, ghScript string) string {
	t.Helper()
	dir := gitOnlyPath(t)

	// The claude stub below needs "cat" to print its fixed output -
	// symlinked in for the same reason git is: a PATH scoped to exactly
	// what the stubs need, nothing implicitly inherited from the machine
	// running the test.
	realCat, err := exec.LookPath("cat")
	if err != nil {
		t.Fatalf("cat not found on PATH: %v", err)
	}
	if err := os.Symlink(realCat, filepath.Join(dir, "cat")); err != nil {
		t.Fatalf("linking real cat: %v", err)
	}

	claudeScript := "#!/bin/sh\ncat <<'CHECKPOINT_EOF'\n" + claudeVerdict + "\nCHECKPOINT_EOF\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(claudeScript), 0o755); err != nil {
		t.Fatalf("writing claude stub: %v", err)
	}

	ghBody := "#!/bin/sh\n" + ghScript + "\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(ghBody), 0o755); err != nil {
		t.Fatalf("writing gh stub: %v", err)
	}

	return dir
}

const passingReview = "reviewed, no issues.\n" + checkpoint.VerdictPrefix + " PASS"

const ghCreatesNewPR = `case "$1 $2" in
  "pr create") echo "https://github.com/x/y/pull/1" ;;
esac`

const ghHasOpenPR = `case "$1 $2" in
  "pr view") echo '{"number":1,"state":"OPEN","isDraft":false,"mergeable":"MERGEABLE","headRefOid":"abc123","url":"https://github.com/x/y/pull/1"}' ;;
esac`

// B4-04: shipping a done mission whose checkpoint pipeline passes pushes
// its camp branch to "origin" and opens a real pull request.
func TestRunShip_Success(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	t.Setenv("PATH", shipToolsPath(t, passingReview, ghCreatesNewPR))

	var out bytes.Buffer
	code := runShip(project, home, task.ID, &out, &out)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "pushed "+task.CampBranch) {
		t.Errorf("expected output to confirm the push, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "https://github.com/x/y/pull/1") {
		t.Errorf("expected output to include the new pull request's URL, got: %s", out.String())
	}
}

// A successful ship records the task as shipped, so a later "vexillum
// land" knows to merge the real PR instead of fast-forwarding the camp.
func TestRunShip_RecordsShippedStatus(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	t.Setenv("PATH", shipToolsPath(t, passingReview, ghCreatesNewPR))

	var out bytes.Buffer
	if code := runShip(project, home, task.ID, &out, &out); code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}

	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	reloaded, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if reloaded.Status != state.StatusShipped {
		t.Errorf("expected status %q after a successful ship, got %q", state.StatusShipped, reloaded.Status)
	}
}

// A mission already shipped can be shipped again - pushing follow-up
// commits onto the same open PR is a normal continuation, not an error,
// and it must not attempt to open a second pull request for the branch.
func TestRunShip_AllowsReshippingAShippedTask(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	task.Status = state.StatusShipped
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	t.Setenv("PATH", shipToolsPath(t, passingReview, `case "$1 $2" in
  "pr view") echo '{"number":1,"state":"OPEN","isDraft":false,"mergeable":"MERGEABLE","headRefOid":"abc123","url":"https://github.com/x/y/pull/1"}' ;;
  "pr create") echo "PR CREATE SHOULD NOT HAVE BEEN CALLED" >&2; exit 1 ;;
esac`))

	var out bytes.Buffer
	code := runShip(project, home, task.ID, &out, &out)

	if code != 0 {
		t.Fatalf("expected exit 0 reshipping an already-shipped task, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "pushed "+task.CampBranch) {
		t.Errorf("expected output to confirm the push, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "https://github.com/x/y/pull/1") {
		t.Errorf("expected output to include the existing pull request's URL, got: %s", out.String())
	}
}

// B4-05: ship refuses a task that isn't done yet.
func TestRunShip_RefusesNotDone(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()

	task, err := state.New(state.KindMission, "do a thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.Status = state.StatusRunning
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	if err := state.Save(projectRoot, task); err != nil {
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
	project := shipTestProject(t)
	home := t.TempDir()

	task, err := state.New(state.KindScout, "look into it")
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
	code := runShip(project, home, task.ID, &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit for a scout")
	}
	if !strings.Contains(out.String(), "scout") {
		t.Errorf("expected the error to name why it refused, got: %s", out.String())
	}
}

// ship refuses up front when "gh" isn't installed - no point running the
// whole checkpoint pipeline just to fail at the push step.
func TestRunShip_RefusesWhenGhNotInstalled(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	t.Setenv("PATH", gitOnlyPath(t))

	var out bytes.Buffer
	code := runShip(project, home, task.ID, &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit when gh isn't installed")
	}
	if !strings.Contains(out.String(), "'gh' is not installed") {
		t.Errorf("expected the error to say gh isn't installed, got: %s", out.String())
	}
}

// A failing checkpoint step blocks the ship entirely - no push, no PR.
func TestRunShip_RefusesWhenCheckpointFails(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	t.Setenv("PATH", shipToolsPath(t, "no verdict line here at all", `case "$1 $2" in
  "pr create") echo "PR CREATE SHOULD NOT HAVE BEEN CALLED" >&2; exit 1 ;;
esac`))

	var out bytes.Buffer
	code := runShip(project, home, task.ID, &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit when a checkpoint step fails")
	}
	if !strings.Contains(out.String(), "review") {
		t.Errorf("expected the error to name the failing step, got: %s", out.String())
	}
	if strings.Contains(out.String(), "pushed ") {
		t.Errorf("expected no push attempt after a failed checkpoint step, got: %s", out.String())
	}

	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	reloaded, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if reloaded.Status == state.StatusShipped {
		t.Error("expected the task to stay unshipped after a failed checkpoint")
	}
}
