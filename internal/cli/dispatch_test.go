package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/camp"
	vxproject "github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/report"
	"github.com/isaias-alt/vexillum/internal/state"
)

// fakeHerdr is a minimal herdr.Client for testing the CLI wiring, not the
// underlying orchestration logic (that's covered in internal/soldier).
type fakeHerdr struct {
	tabID, paneID string
	promptStatus  string
	readOutput    string
	tabCloseErr   error

	lastAgentArgs []string
	tabClosed     bool
}

func (f *fakeHerdr) CreateTab(workspaceID, cwd, label string, env ...string) (string, string, error) {
	return f.tabID, f.paneID, nil
}
func (f *fakeHerdr) AgentStart(name, kind, paneID string, agentArgs ...string) error {
	f.lastAgentArgs = agentArgs
	return nil
}
func (f *fakeHerdr) AgentSendKeys(name string, keys ...string) error { return nil }
func (f *fakeHerdr) AgentReady(name string) (bool, error)            { return true, nil }
func (f *fakeHerdr) AgentStatus(name string) (string, error)         { return f.promptStatus, nil }
func (f *fakeHerdr) AgentPrompt(name, text string, timeoutMS int) (string, error) {
	return f.promptStatus, nil
}
func (f *fakeHerdr) AgentRead(name string, lines int) (string, error) { return f.readOutput, nil }
func (f *fakeHerdr) TabClose(tabID string) error {
	f.tabClosed = true
	return f.tabCloseErr
}

// refuseInsideVexillumHome catches running a project command from
// inside a camp's own worktree - a real bug caught live: a commander
// cd'd into a task's camp to inspect it, then dispatched a second
// mission without cd-ing back, creating a whole separate camp pool
// keyed off the camp's own path (invisible to every future command run
// correctly from the real project root).
func TestRefuseInsideVexillumHome(t *testing.T) {
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")
	campPath := filepath.Join(vexillumHome, "myproject-abc12345", "1", "myproject")
	if err := os.MkdirAll(campPath, 0o755); err != nil {
		t.Fatalf("mkdir camp path: %v", err)
	}

	if err := refuseInsideVexillumHome(campPath, vexillumHome); err == nil {
		t.Error("expected an error when projectDir is inside vexillumHome")
	}

	realProject := t.TempDir()
	if err := refuseInsideVexillumHome(realProject, vexillumHome); err != nil {
		t.Errorf("expected no error for a real project dir outside vexillumHome, got: %v", err)
	}
}

func TestParseDispatchArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantPrompt string
		wantKind   state.Kind
		wantModel  string
		wantEffort string
		wantErr    bool
	}{
		{"defaults to mission", []string{"do", "the", "thing"}, "do the thing", state.KindMission, "", "", false},
		{"explicit mission", []string{"--kind", "mission", "do it"}, "do it", state.KindMission, "", "", false},
		{"explicit scout", []string{"--kind", "scout", "look into it"}, "look into it", state.KindScout, "", "", false},
		{"unknown kind", []string{"--kind", "bogus", "x"}, "", "", "", "", true},
		{"kind without value", []string{"--kind"}, "", "", "", "", true},
		{"missing prompt", []string{"--kind", "scout"}, "", "", "", "", true},
		{"model and effort", []string{"--model", "haiku", "--effort", "low", "do it"}, "do it", state.KindMission, "haiku", "low", false},
		{"model without value", []string{"--model"}, "", "", "", "", true},
		{"effort without value", []string{"--effort"}, "", "", "", "", true},
		{"flags mixed with prompt words", []string{"do", "--model", "sonnet", "the", "thing"}, "do the thing", state.KindMission, "sonnet", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prompt, kind, model, effort, err := parseDispatchArgs(c.args)
			if c.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if prompt != c.wantPrompt || kind != c.wantKind || model != c.wantModel || effort != c.wantEffort {
				t.Errorf("got prompt=%q kind=%q model=%q effort=%q, want prompt=%q kind=%q model=%q effort=%q",
					prompt, kind, model, effort, c.wantPrompt, c.wantKind, c.wantModel, c.wantEffort)
			}
		})
	}
}

func initDispatchTestProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "symbolic-ref", "HEAD", "refs/heads/main")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("symbolic-ref: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("writing README: %v", err)
	}
	cmd = exec.Command("git", "add", "README.md")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "initial")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	return dir
}

// vexillum dispatch refuses on a project that was never `vexillum init`-ed.
func TestRunDispatch_RefusesUninitializedProject(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()

	var out bytes.Buffer
	code := runDispatch(project, home, "w1", "do a thing", state.KindMission, "", "", &fakeHerdr{}, &out, &out)

	if code == 0 {
		t.Fatal("expected non-zero exit for an uninitialized project")
	}
	if !strings.Contains(out.String(), "vexillum init") {
		t.Errorf("expected the error to suggest running vexillum init, got: %s", out.String())
	}
}

// A successful dispatch creates a camp, runs the soldier, and reports the
// task id and status.
func TestRunDispatch_Success(t *testing.T) {
	project := initDispatchTestProject(t)
	if err := writeLocalConfig(filepath.Join(project, ".vexillum")); err != nil {
		t.Fatalf("writing local config: %v", err)
	}
	home := t.TempDir()

	client := &fakeHerdr{tabID: "w1:t1", paneID: "w1:p1", promptStatus: "done", readOutput: "did the thing"}

	var out bytes.Buffer
	code := runDispatch(project, home, "w1", "do a thing", state.KindMission, "", "", client, &out, &out)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "status=done") {
		t.Errorf("expected output to report status=done, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "did the thing") {
		t.Errorf("expected output to include the transcript, got: %s", out.String())
	}
}

// A dispatch with --model/--effort threads both through to the real
// claude launch (client.AgentStart's agentArgs) unexamined - vexillum
// validates them but never decides which ones to use (ADR-05).
func TestRunDispatch_PassesModelEffortToClaude(t *testing.T) {
	project := initDispatchTestProject(t)
	if err := writeLocalConfig(filepath.Join(project, ".vexillum")); err != nil {
		t.Fatalf("writing local config: %v", err)
	}
	home := t.TempDir()

	client := &fakeHerdr{tabID: "w1:t1", paneID: "w1:p1", promptStatus: "done", readOutput: "did the thing"}

	var out bytes.Buffer
	code := runDispatch(project, home, "w1", "do a thing", state.KindMission, "haiku", "low", client, &out, &out)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	want := []string{"--dangerously-skip-permissions", "--prompt-suggestions", "false", "--model", "haiku", "--effort", "low"}
	if !slices.Equal(client.lastAgentArgs, want) {
		t.Errorf("got agent args %v, want %v", client.lastAgentArgs, want)
	}
}

// landTestMission acquires a camp for a fresh mission task, commits one
// change to it, and persists the task - the shape a mission ready to land
// has. Returns the task and its camp.
func landTestMission(t *testing.T, project, home string) (state.Task, camp.Camp) {
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
	task.HerdrTabID = "w1:t1"
	task.Status = state.StatusDone
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	return task, c
}

// A successful land fast-forwards the base branch AND automatically
// releases the mission's camp - the operator no longer chains a manual
// 'vexillum release' afterward. Verified two ways: the herdr pane got
// closed (soldier.ReleaseInHerdr's own side effect), and the pool slot
// is no longer leased so a second 'vexillum release' on the same task has
// nothing left to do.
func TestRunLand_AutoReleasesCamp(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()
	task, _ := landTestMission(t, project, home)

	client := &fakeHerdr{}
	var out bytes.Buffer
	if code := runLand(project, home, t.TempDir(), task.ID, client, &out, &out); code != 0 {
		t.Fatalf("runLand: expected exit 0, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "landed:") || !strings.Contains(out.String(), "released:") {
		t.Errorf("expected output to report both landing and releasing, got: %s", out.String())
	}
	if !client.tabClosed {
		t.Error("expected land to close the soldier's herdr pane via the release path")
	}

	// The camp is already released - a second, independent release call
	// for the same task must find nothing left to do.
	out.Reset()
	code := runRelease(project, home, t.TempDir(), task.ID, false, &fakeHerdr{}, &out, &out)
	if code == 0 {
		t.Fatalf("expected a follow-up 'vexillum release' to fail, camp was already released; got exit 0: %s", out.String())
	}
	if !strings.Contains(out.String(), "not leased") {
		t.Errorf("expected the refusal to say the slot is no longer leased, got: %s", out.String())
	}
}

// A land that's refused (dirty checkout, or a diverged branch) never
// touches the camp: no automatic release runs, the pane is never closed,
// and the camp remains leased and landable/releasable once the underlying
// problem is fixed.
func TestRunLand_RefusalLeavesCampUntouched(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()
	task, _ := landTestMission(t, project, home)

	// Make the project's own checkout dirty so camp.Land refuses before
	// ever attempting the merge.
	if err := os.WriteFile(filepath.Join(project, "dirty.txt"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatalf("writing dirty file: %v", err)
	}

	client := &fakeHerdr{}
	var out bytes.Buffer
	code := runLand(project, home, t.TempDir(), task.ID, client, &out, &out)
	if code == 0 {
		t.Fatalf("expected a non-zero exit for a refused land, got 0: %s", out.String())
	}
	if strings.Contains(out.String(), "released:") {
		t.Errorf("a refused land must never report a release, got: %s", out.String())
	}
	if client.tabClosed {
		t.Error("a refused land must never close the soldier's herdr pane")
	}

	// The camp is still leased and untouched: releasing it directly still
	// refuses too, since the mission's commit never actually landed.
	out.Reset()
	code = runRelease(project, home, t.TempDir(), task.ID, false, &fakeHerdr{}, &out, &out)
	if code == 0 {
		t.Fatalf("expected release to still refuse an un-landed camp, got exit 0: %s", out.String())
	}
}

// The rare case: the fast-forward merge itself succeeds, but the
// automatic release that follows fails (here, because the soldier left
// uncommitted junk in its own camp worktree - camp.Land only checks the
// PROJECT checkout is clean, not the camp's). land must report both
// outcomes plainly - the merge is not undone, and the failure is not
// swallowed - and leave the camp for a manual 'vexillum release' to
// retry.
func TestRunLand_MergeSucceedsButAutoReleaseFails(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()
	task, c := landTestMission(t, project, home)

	if err := os.WriteFile(filepath.Join(c.Path, "leftover.txt"), []byte("oops\n"), 0o644); err != nil {
		t.Fatalf("writing uncommitted leftover file in the camp: %v", err)
	}

	client := &fakeHerdr{}
	var out bytes.Buffer
	code := runLand(project, home, t.TempDir(), task.ID, client, &out, &out)
	if code == 0 {
		t.Fatalf("expected a non-zero exit when the automatic release fails, got 0: %s", out.String())
	}
	if !strings.Contains(out.String(), "landed:") {
		t.Errorf("expected the merge success to still be reported, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "automatic release failed") {
		t.Errorf("expected the release failure to be reported plainly, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "vexillum release "+task.ID) {
		t.Errorf("expected land to point the operator at a manual 'vexillum release %s', got: %s", task.ID, out.String())
	}
	if client.tabClosed {
		t.Error("a failed release must never close the herdr pane")
	}

	// Verify the merge genuinely landed despite the release failure.
	head := runGitOutput(t, project, "rev-parse", "HEAD")
	campHead := runGitOutput(t, c.Path, "rev-parse", "HEAD")
	if head != campHead {
		t.Errorf("expected the project checkout to have fast-forwarded to the camp's HEAD despite the release failure, got project=%s camp=%s", head, campHead)
	}

	// Clean up the leftover file and confirm a manual release now
	// succeeds, proving the camp was left in a recoverable state.
	if err := os.Remove(filepath.Join(c.Path, "leftover.txt")); err != nil {
		t.Fatalf("removing leftover file: %v", err)
	}
	out.Reset()
	if code := runRelease(project, home, t.TempDir(), task.ID, false, &fakeHerdr{}, &out, &out); code != 0 {
		t.Fatalf("expected the manual follow-up release to succeed once the camp is clean, got %d: %s", code, out.String())
	}
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v (in %s): %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newReleaseTestScoutTask creates a fresh camp (clean, trivially "landed"
// since it carries no commits of its own yet) for a scout task and
// persists it - the shape a scout ready to release has, minus its report.
func newReleaseTestScoutTask(t *testing.T, project, home string) (state.Task, string) {
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
	task.HerdrAgentName = "vx-look-into-it"
	task.Status = state.StatusDone
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	return task, projectRoot
}

// The report gate (docs/ decisions, point 5): a scout with no report file
// is refused, analogous to firstmate's own teardown refusal for a task
// missing report.md.
func TestRunRelease_RefusesScoutWithoutReport(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()
	task, projectRoot := newReleaseTestScoutTask(t, project, home)

	var out bytes.Buffer
	code := runRelease(project, home, t.TempDir(), task.ID, false, &fakeHerdr{}, &out, &out)

	if code == 0 {
		t.Fatal("expected a non-zero exit for a scout with no report")
	}
	wantPath := report.Path(projectRoot, task.HerdrAgentName, task.ID)
	if !strings.Contains(out.String(), wantPath) {
		t.Errorf("expected the refusal to name the missing report path %q, got: %s", wantPath, out.String())
	}
}

// A scout whose report exists releases normally, no --force needed.
func TestRunRelease_ScoutWithReportSucceeds(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()
	task, projectRoot := newReleaseTestScoutTask(t, project, home)

	if err := os.MkdirAll(report.Dir(projectRoot), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(report.Path(projectRoot, task.HerdrAgentName, task.ID), []byte("# findings\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var out bytes.Buffer
	code := runRelease(project, home, t.TempDir(), task.ID, false, &fakeHerdr{}, &out, &out)

	if code != 0 {
		t.Fatalf("expected exit 0 for a scout with a report, got %d: %s", code, out.String())
	}
}

// --force skips the report check for a scout with no report - the
// explicit, logged escape hatch, never a silent default.
func TestRunRelease_ForceSkipsReportGate(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()
	task, _ := newReleaseTestScoutTask(t, project, home)

	var out bytes.Buffer
	code := runRelease(project, home, t.TempDir(), task.ID, true, &fakeHerdr{}, &out, &out)

	if code != 0 {
		t.Fatalf("expected exit 0 with --force despite the missing report, got %d: %s", code, out.String())
	}
}

// A mission is never gated on a report, even with --force absent -
// missions don't have one, optional or otherwise (confirmed against
// firstmate: report.md is exclusive to worker/scout tasks).
func TestRunRelease_MissionNeverRequiresReport(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()

	task, err := state.New(state.KindMission, "do a thing")
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
	task.HerdrAgentName = "vx-do-a-thing"
	task.Status = state.StatusDone
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	var out bytes.Buffer
	code := runRelease(project, home, t.TempDir(), task.ID, false, &fakeHerdr{}, &out, &out)

	if code != 0 {
		t.Fatalf("expected exit 0 for a mission with no report, got %d: %s", code, out.String())
	}
}

// vexillum release <task-id> --force parses regardless of flag/arg order.
func TestParseReleaseArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantTaskID string
		wantForce  bool
		wantErr    bool
	}{
		{"task id only", []string{"abc123"}, "abc123", false, false},
		{"force after id", []string{"abc123", "--force"}, "abc123", true, false},
		{"force before id", []string{"--force", "abc123"}, "abc123", true, false},
		{"missing task id", []string{"--force"}, "", false, true},
		{"no args", []string{}, "", false, true},
		{"two positional args", []string{"abc123", "def456"}, "", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			taskID, force, err := parseReleaseArgs(c.args)
			if c.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if taskID != c.wantTaskID || force != c.wantForce {
				t.Errorf("got taskID=%q force=%v, want taskID=%q force=%v", taskID, force, c.wantTaskID, c.wantForce)
			}
		})
	}
}
