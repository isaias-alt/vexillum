package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/sentinel"
	"github.com/isaias-alt/vexillum/internal/state"
)

func runGitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

// newSettledMissionCamp builds a standalone git worktree (not through
// internal/camp - these tests only need internal/sentinel to see a real
// commit ahead of base, never a full camp.Acquire) whose branch already
// has a commit ahead of its base ("main") - internal/sentinel's own
// strong completion signal for a mission (internal/camp.HasNewCommits).
func newSettledMissionCamp(t *testing.T) (campPath, base string) {
	t.Helper()
	dir := t.TempDir()
	runGitT(t, dir, "init", "-q")
	runGitT(t, dir, "symbolic-ref", "HEAD", "refs/heads/main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("writing README: %v", err)
	}
	runGitT(t, dir, "add", "README.md")
	runGitT(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "initial commit")
	runGitT(t, dir, "checkout", "-q", "-b", "vexillum/task")
	if err := os.WriteFile(filepath.Join(dir, "output.txt"), []byte("done\n"), 0o644); err != nil {
		t.Fatalf("writing output: %v", err)
	}
	runGitT(t, dir, "add", "output.txt")
	runGitT(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "soldier's work")
	return dir, "main"
}

// newSettledMissionTask persists a Running mission task whose camp
// already carries a real commit ahead of base - internal/sentinel's
// strong completion signal for a mission - so a sentinel.Tick against it
// with a "done" live status actually settles it, the same way a
// genuinely finished soldier's would (internal/sentinel now refuses to
// trust a bare idle status alone - see internal/pause's package doc).
// Backdated past sentinel.Tick's settle-race grace period.
func newSettledMissionTask(t *testing.T, projectRoot string) state.Task {
	t.Helper()
	task, err := state.New(state.KindMission, "do the thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.Status = state.StatusRunning
	task.HerdrAgentName = "vx-do-the-thing"
	task.CampPath, task.CampBase = newSettledMissionCamp(t)
	task.UpdatedAt = task.UpdatedAt.Add(-1 * time.Minute)
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	return task
}

// sentinelMode classifies the three recognized forms and rejects anything
// else - an unrecognized subcommand must not silently fall through to
// starting the infinite polling loop (the bug found via a real commander
// session running "vexillum sentinel status").
func TestSentinelMode(t *testing.T) {
	cases := []struct {
		args    []string
		want    string
		wantErr bool
	}{
		{args: nil, want: "run"},
		{args: []string{}, want: "run"},
		{args: []string{"-h"}, want: "help"},
		{args: []string{"--help"}, want: "help"},
		{args: []string{"drain"}, want: "drain"},
		{args: []string{"await"}, want: "await"},
		{args: []string{"status"}, wantErr: true},
		{args: []string{"drain", "extra"}, want: "drain"},
	}

	for _, c := range cases {
		got, err := sentinelMode(c.args)
		if c.wantErr {
			if err == nil {
				t.Errorf("sentinelMode(%v): expected an error, got mode %q", c.args, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("sentinelMode(%v): unexpected error: %v", c.args, err)
			continue
		}
		if got != c.want {
			t.Errorf("sentinelMode(%v) = %q, want %q", c.args, got, c.want)
		}
	}
}

// projectRoot mirrors internal/sentinel's own test helper: a namespaced
// project root under vexillumHome, without needing a real project
// directory - runSentinelDrain/runSentinelAwait/sentinel.Tick only care
// that it's a plain root to read tasks/wakes from.
func projectRoot(vexillumHome, name string) string {
	return filepath.Join(vexillumHome, "projects", name)
}

// No pending wakes -> the empty hook JSON, so a Stop hook lets the turn
// end normally.
func TestRunSentinelDrain_NoWakes(t *testing.T) {
	proj := projectRoot(t.TempDir(), "proj1")

	var out bytes.Buffer
	if code := runSentinelDrain(proj, &out, &out); code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	if strings.TrimSpace(out.String()) != "{}" {
		t.Errorf("expected {}, got %q", out.String())
	}
}

// A pending wake produces a Stop-hook "block" decision naming the task
// and its transition, so the commander keeps working instead of quietly
// stopping.
func TestRunSentinelDrain_PendingWakeBlocksStop(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")

	// Backdated past sentinel.Tick's settle-race grace period (and camp
	// already settled), so this test exercises a real transition rather
	// than the grace period itself (see
	// internal/sentinel.TestTick_SkipsTasksWithinSettleGracePeriod).
	task := newSettledMissionTask(t, proj)

	// A real sentinel tick is what would have recorded this wake, so
	// exercise it the same way rather than writing the wake file by hand.
	client := &fakeHerdr{promptStatus: "done"}
	if _, err := sentinel.Tick(home, client); err != nil {
		t.Fatalf("sentinel.Tick: %v", err)
	}

	var out bytes.Buffer
	if code := runSentinelDrain(proj, &out, &out); code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, `"decision":"block"`) {
		t.Errorf("expected a blocking decision, got: %s", got)
	}
	if !strings.Contains(got, task.ID) {
		t.Errorf("expected the reason to name the task id, got: %s", got)
	}
}

// runSentinelAwait is what the async Stop hook actually calls. A wake
// already pending when it's invoked is found on its very first check -
// exit 2 with the reason on stderr, the block signal a real asyncRewake
// hook uses (verified live against a real Claude Code session).
func TestRunSentinelAwait_FindsAlreadyPendingWake(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")

	task := newSettledMissionTask(t, proj)
	client := &fakeHerdr{promptStatus: "done"}
	if _, err := sentinel.Tick(home, client); err != nil {
		t.Fatalf("sentinel.Tick: %v", err)
	}

	var stderr bytes.Buffer
	code := runSentinelAwait(proj, time.Hour, time.Millisecond, &stderr)
	if code != 2 {
		t.Fatalf("expected exit 2, got %d: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), task.ID) {
		t.Errorf("expected the reason to name the task id, got: %s", stderr.String())
	}
}

// With nothing ever pending, runSentinelAwait exits 0 once maxWait
// elapses - the async hook lets the turn end quietly, same as
// runSentinelDrain's {} for the instant-check case.
func TestRunSentinelAwait_TimesOutWithNothingPending(t *testing.T) {
	proj := projectRoot(t.TempDir(), "proj1")

	var stderr bytes.Buffer
	code := runSentinelAwait(proj, 20*time.Millisecond, 5*time.Millisecond, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0 on timeout, got %d: %s", code, stderr.String())
	}
	if stderr.String() != "" {
		t.Errorf("expected no output on a silent timeout, got: %s", stderr.String())
	}
}

// A wake that only shows up after a couple of poll cycles is still
// found before maxWait elapses - not just on the very first check.
func TestRunSentinelAwait_FindsWakeThatArrivesMidWait(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")

	task := newSettledMissionTask(t, proj)

	client := &fakeHerdr{promptStatus: "done"}
	go func() {
		time.Sleep(15 * time.Millisecond)
		_, _ = sentinel.Tick(home, client)
	}()

	var stderr bytes.Buffer
	code := runSentinelAwait(proj, time.Second, 5*time.Millisecond, &stderr)
	if code != 2 {
		t.Fatalf("expected exit 2 once the wake appeared, got %d: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), task.ID) {
		t.Errorf("expected the reason to name the task id, got: %s", stderr.String())
	}
}

// Outside a herdr-managed pane (no HERDR_WORKSPACE_ID), runSentinelAwaitGuarded
// must exit 0 immediately, without ever polling - the case that surfaced
// this: a third-party tool's own headless Claude Code turn (e.g.
// no-mistakes' review/test/lint steps) inherits the committed Stop hook
// too, but the sentinel has no task tracking it, so a real wake could
// never arrive and the turn would otherwise sit blocked for the full
// maxWait. A pending wake existing is irrelevant here - the whole point
// is that this invocation isn't the one meant to receive it.
func TestRunSentinelAwaitGuarded_NoWorkspaceExitsImmediately(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")

	newSettledMissionTask(t, proj)
	client := &fakeHerdr{promptStatus: "done"}
	if _, err := sentinel.Tick(home, client); err != nil {
		t.Fatalf("sentinel.Tick: %v", err)
	}

	start := time.Now()
	var stderr bytes.Buffer
	code := runSentinelAwaitGuarded(proj, time.Hour, 5*time.Millisecond, &stderr, "")
	elapsed := time.Since(start)

	if code != 0 {
		t.Fatalf("expected exit 0 with no workspace id, got %d: %s", code, stderr.String())
	}
	if stderr.String() != "" {
		t.Errorf("expected no output, got: %s", stderr.String())
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("expected an immediate return, took %s", elapsed)
	}
}

// Inside a herdr-managed pane (HERDR_WORKSPACE_ID set), runSentinelAwaitGuarded
// behaves exactly like the unguarded runSentinelAwait - a pending wake is
// still found and still blocks the turn.
func TestRunSentinelAwaitGuarded_WithWorkspaceFindsWake(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")

	task := newSettledMissionTask(t, proj)
	client := &fakeHerdr{promptStatus: "done"}
	if _, err := sentinel.Tick(home, client); err != nil {
		t.Fatalf("sentinel.Tick: %v", err)
	}

	var stderr bytes.Buffer
	code := runSentinelAwaitGuarded(proj, time.Hour, time.Millisecond, &stderr, "ws-123")
	if code != 2 {
		t.Fatalf("expected exit 2 with a workspace id set, got %d: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), task.ID) {
		t.Errorf("expected the reason to name the task id, got: %s", stderr.String())
	}
}

// C1-09: outside any git repository, resolveDrainTarget has nothing to
// resolve - a silent no-op ("", nil), not an error.
func TestResolveDrainTarget_NotARepo(t *testing.T) {
	vexillumHome := t.TempDir()
	notARepo := t.TempDir()

	got, err := resolveDrainTarget(notARepo, vexillumHome)
	if err != nil {
		t.Fatalf("resolveDrainTarget: %v", err)
	}
	if got != "" {
		t.Errorf("expected no project root outside a git repo, got %q", got)
	}
}

// C1-10: a real project's root resolves to the same project root
// internal/project.Root (and so camp.Acquire) would compute for it -
// this is what keeps "vexillum dispatch" and "vexillum sentinel drain"
// agreeing on the same project.
func TestResolveDrainTarget_RealProject(t *testing.T) {
	vexillumHome := t.TempDir()
	proj := initDispatchTestProject(t)

	got, err := resolveDrainTarget(proj, vexillumHome)
	if err != nil {
		t.Fatalf("resolveDrainTarget: %v", err)
	}
	if got == "" {
		t.Fatal("expected a resolved project root for a real project")
	}

	c, err := camp.Acquire(proj, vexillumHome, "task-1")
	if err != nil {
		t.Fatalf("camp.Acquire: %v", err)
	}
	// camp.Acquire's PoolRoot is <project root>/camps - its parent is
	// exactly the project root resolveDrainTarget should have resolved.
	wantRoot := filepath.Dir(c.PoolRoot)
	if got != wantRoot {
		t.Errorf("resolveDrainTarget(%q) = %q, want the same project root camp.Acquire uses (%q)", proj, got, wantRoot)
	}
}

// C1-11: resolveDrainTarget works from a subdirectory of the project too,
// not just its root - "git rev-parse --show-toplevel" walks up to find
// it, unlike the plain os.Getwd() dispatch/land/release/redispatch/ship
// use (which require running from the project root itself).
func TestResolveDrainTarget_FromSubdirectory(t *testing.T) {
	vexillumHome := t.TempDir()
	proj := initDispatchTestProject(t)
	subdir := filepath.Join(proj, "sub")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	fromRoot, err := resolveDrainTarget(proj, vexillumHome)
	if err != nil {
		t.Fatalf("resolveDrainTarget(root): %v", err)
	}
	fromSub, err := resolveDrainTarget(subdir, vexillumHome)
	if err != nil {
		t.Fatalf("resolveDrainTarget(subdir): %v", err)
	}
	if fromRoot == "" || fromRoot != fromSub {
		t.Errorf("expected the same project root from the project's root (%q) and a subdirectory (%q)", fromRoot, fromSub)
	}
}

// C1-12: a camp's own worktree - a soldier's cwd - resolves to no
// project at all, not the project it belongs to. A soldier's own Stop
// hook has nothing to drain for itself; only the commander, running from
// the real project root, should ever drain.
func TestResolveDrainTarget_InsideACampIsANoOp(t *testing.T) {
	vexillumHome := t.TempDir()
	proj := initDispatchTestProject(t)

	c, err := camp.Acquire(proj, vexillumHome, "task-1")
	if err != nil {
		t.Fatalf("camp.Acquire: %v", err)
	}

	got, err := resolveDrainTarget(c.Path, vexillumHome)
	if err != nil {
		t.Fatalf("resolveDrainTarget: %v", err)
	}
	if got != "" {
		t.Errorf("expected no project root from inside a camp, got %q", got)
	}
}

// C1-13: end-to-end - a wake genuinely pending for a project is left
// completely untouched by a drain invoked from inside one of that
// project's own camps (a soldier's Stop hook firing from its own
// worktree). Verifies both halves: resolveDrainTarget's no-op AND that
// runSentinelDrainOrAwait, wired the same way the real "vexillum
// sentinel drain" command is, never calls into sentinel.Drain for the
// project's real wakes when invoked this way.
func TestDrainFromCamp_DoesNotDrainProjectWakes(t *testing.T) {
	vexillumHome := t.TempDir()
	proj := initDispatchTestProject(t)

	c, err := camp.Acquire(proj, vexillumHome, "task-1")
	if err != nil {
		t.Fatalf("camp.Acquire: %v", err)
	}

	projRoot, err := resolveDrainTarget(proj, vexillumHome)
	if err != nil {
		t.Fatalf("resolveDrainTarget(project root): %v", err)
	}
	if projRoot == "" {
		t.Fatal("expected a resolved project root for the real project")
	}

	// A real pending wake for the project - what a commander's own drain
	// would need to surface.
	task := newSettledMissionTask(t, projRoot)
	client := &fakeHerdr{promptStatus: "done"}
	if _, err := sentinel.Tick(vexillumHome, client); err != nil {
		t.Fatalf("sentinel.Tick: %v", err)
	}

	wakeFile := filepath.Join(projRoot, "wakes", task.ID+".json")
	if _, err := os.Stat(wakeFile); err != nil {
		t.Fatalf("expected a real pending wake before the camp drain attempt: %v", err)
	}

	// Now resolve as if drain had been invoked with cwd inside the
	// soldier's own camp - the scenario the Stop hook actually hits.
	campProjectRoot, err := resolveDrainTarget(c.Path, vexillumHome)
	if err != nil {
		t.Fatalf("resolveDrainTarget(camp): %v", err)
	}
	if campProjectRoot != "" {
		t.Fatalf("expected no project resolved from inside the camp, got %q", campProjectRoot)
	}

	// The project's real wake must still be sitting there, completely
	// untouched.
	if _, err := os.Stat(wakeFile); err != nil {
		t.Errorf("expected the project's pending wake to survive a drain attempt from inside its own camp, but: %v", err)
	}
	pending, err := sentinel.Drain(projRoot)
	if err != nil {
		t.Fatalf("Drain(project root): %v", err)
	}
	if len(pending) != 1 || pending[0].TaskID != task.ID {
		t.Errorf("expected the project's wake to still be pending after the camp drain attempt, got %+v", pending)
	}
}
