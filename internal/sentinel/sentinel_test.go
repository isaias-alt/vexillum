package sentinel_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/pause"
	"github.com/isaias-alt/vexillum/internal/report"
	"github.com/isaias-alt/vexillum/internal/sentinel"
	"github.com/isaias-alt/vexillum/internal/state"
)

// fakeHerdr implements just enough of herdr.Client for these tests
// (AgentStatus is all Tick uses); the rest are no-ops.
type fakeHerdr struct {
	statuses   map[string]string
	err        error
	errFor     map[string]error // per-name error, checked before the blanket err
	readOutput string
}

func (f *fakeHerdr) CreateTab(workspaceID, cwd, label string, env ...string) (string, string, error) {
	return "", "", nil
}
func (f *fakeHerdr) AgentStart(name, kind, paneID string, agentArgs ...string) error { return nil }
func (f *fakeHerdr) AgentSendKeys(name string, keys ...string) error                 { return nil }
func (f *fakeHerdr) AgentReady(name string) (bool, error)                            { return true, nil }
func (f *fakeHerdr) AgentStatus(name string) (string, error) {
	if err, ok := f.errFor[name]; ok {
		return "", err
	}
	if f.err != nil {
		return "", f.err
	}
	return f.statuses[name], nil
}
func (f *fakeHerdr) AgentPrompt(name, text string, timeoutMS int) (string, error) { return "", nil }
func (f *fakeHerdr) AgentRead(name string, lines int) (string, error)             { return f.readOutput, nil }
func (f *fakeHerdr) TabClose(tabID string) error                                  { return nil }

// projectRoot returns a namespaced project root under vexillumHome, the
// same shape internal/project.Root would produce (vexillumHome/projects/<key>),
// without needing a real project directory or git repo - Tick only cares
// that it's a subdirectory of vexillumHome/projects, and Drain/state don't
// care about its name at all.
func projectRoot(vexillumHome, name string) string {
	return filepath.Join(vexillumHome, "projects", name)
}

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

func writeAndCommit(t *testing.T, dir, name, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	runGitT(t, dir, "add", name)
	runGitT(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", message)
}

// newCommittedMissionCamp builds a standalone git "camp" (not through
// internal/camp - the sentinel only ever reads a git worktree by path, it
// never acquires one) whose branch already has a real commit ahead of its
// base ("main") - the strong completion signal internal/camp.HasNewCommits
// looks for. Used by tests where the mission genuinely finished.
func newCommittedMissionCamp(t *testing.T) (campPath, base string) {
	t.Helper()
	dir := t.TempDir()
	runGitT(t, dir, "init", "-q")
	runGitT(t, dir, "symbolic-ref", "HEAD", "refs/heads/main")
	writeAndCommit(t, dir, "README.md", "hi\n", "initial commit")
	runGitT(t, dir, "checkout", "-q", "-b", "vexillum/task")
	writeAndCommit(t, dir, "output.txt", "soldier's work\n", "soldier's work")
	return dir, "main"
}

// newEmptyMissionCamp is newCommittedMissionCamp's counterpart: a real git
// worktree whose branch has never diverged from its base - no strong
// completion signal at all. Used by tests where the mission hasn't
// actually delivered anything yet.
func newEmptyMissionCamp(t *testing.T) (campPath, base string) {
	t.Helper()
	dir := t.TempDir()
	runGitT(t, dir, "init", "-q")
	runGitT(t, dir, "symbolic-ref", "HEAD", "refs/heads/main")
	writeAndCommit(t, dir, "README.md", "hi\n", "initial commit")
	runGitT(t, dir, "checkout", "-q", "-b", "vexillum/task")
	return dir, "main"
}

// newRunningTask backdates UpdatedAt well past Tick's settle-race grace
// period, so tests exercising a real transition aren't accidentally
// testing the grace period instead - see
// TestTick_SkipsTasksWithinSettleGracePeriod for that. Its camp already
// carries a real commit ahead of base (newCommittedMissionCamp), since
// most tests using this helper exist to exercise wake/transition
// mechanics, not internal/sentinel's own strong-completion-signal gate
// (which has its own dedicated tests) - without a real signal, every one
// of those tests would get stuck in the ambiguous-idle path instead of
// ever settling.
func newRunningTask(t *testing.T, projectRoot, agentName string) state.Task {
	t.Helper()
	task, err := state.New(state.KindMission, "do the thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.Status = state.StatusRunning
	task.HerdrAgentName = agentName
	task.CampPath, task.CampBase = newCommittedMissionCamp(t)
	task.UpdatedAt = time.Now().Add(-1 * time.Minute)
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	return task
}

// newRunningScoutTask mirrors newRunningTask but for a scout - the only
// kind that ever gets a report detected (internal/report's package doc).
func newRunningScoutTask(t *testing.T, projectRoot, agentName string) state.Task {
	t.Helper()
	task, err := state.New(state.KindScout, "look into it")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.Status = state.StatusRunning
	task.HerdrAgentName = agentName
	task.UpdatedAt = time.Now().Add(-1 * time.Minute)
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	return task
}

// A scout task that settles to done, with its report file already
// written by the time Tick checks, gets that report path recorded on the
// wake - the sentinel's "detection", riding the existing tick rather than
// a new channel.
func TestTick_RecordsReportPathWhenScoutSettlesWithReport(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	task := newRunningScoutTask(t, proj, "vx-look-into-it")

	if err := os.MkdirAll(report.Dir(proj), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(report.Path(proj, "vx-look-into-it", task.ID), []byte("# findings\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	client := &fakeHerdr{statuses: map[string]string{"vx-look-into-it": "done"}}
	if _, err := sentinel.Tick(home, client); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	wakes, err := sentinel.Drain(proj)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(wakes) != 1 {
		t.Fatalf("expected 1 wake, got %d", len(wakes))
	}
	want := report.Path(proj, "vx-look-into-it", task.ID)
	if wakes[0].ReportPath != want {
		t.Errorf("expected wake.ReportPath = %q, got %q", want, wakes[0].ReportPath)
	}
}

// A scout task that goes idle without ever writing a report is not
// trusted as done - no report means no strong completion signal, so the
// sentinel treats the idle turn as ambiguous (internal/pause's package
// doc) rather than inventing a settle for it. The first observation just
// records the ambiguity, with no wake yet - see
// TestTick_IdleWithoutSignalOrPausePastConfirmWindowBecomesUnconfirmed
// for what happens if this persists.
func TestTick_ScoutWithoutReportIsNotMarkedDoneImmediately(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	task := newRunningScoutTask(t, proj, "vx-look-into-it")

	client := &fakeHerdr{statuses: map[string]string{"vx-look-into-it": "done"}}
	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if woke != 0 {
		t.Errorf("expected no wake on the first idle-without-report observation, got %d", woke)
	}

	persisted, err := state.Load(proj, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Status != state.StatusRunning {
		t.Errorf("expected status to remain running, got %s", persisted.Status)
	}
	if persisted.IdleUnconfirmedSince.IsZero() {
		t.Error("expected IdleUnconfirmedSince to be recorded")
	}
}

// A mission that settles to done never gets a ReportPath, even if a file
// happens to sit at the path a scout of the same agent name would have
// used - missions never have a report (internal/report's package doc).
func TestTick_NoReportPathForMission(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	task := newRunningTask(t, proj, "vx-do-the-thing")

	if err := os.MkdirAll(report.Dir(proj), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(report.Path(proj, "vx-do-the-thing", task.ID), []byte("# not a scout\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "done"}}
	if _, err := sentinel.Tick(home, client); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	wakes, err := sentinel.Drain(proj)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(wakes) != 1 {
		t.Fatalf("expected 1 wake, got %d", len(wakes))
	}
	if wakes[0].ReportPath != "" {
		t.Errorf("expected an empty ReportPath for a mission, got %q", wakes[0].ReportPath)
	}
}

// A scout that settles to blocked (not done) never gets a ReportPath even
// with a report file already present - only a real Done settlement counts.
func TestTick_NoReportPathWhenScoutSettlesBlocked(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	task := newRunningScoutTask(t, proj, "vx-look-into-it")

	if err := os.MkdirAll(report.Dir(proj), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(report.Path(proj, "vx-look-into-it", task.ID), []byte("# partial\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	client := &fakeHerdr{statuses: map[string]string{"vx-look-into-it": "blocked"}}
	if _, err := sentinel.Tick(home, client); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	wakes, err := sentinel.Drain(proj)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(wakes) != 1 {
		t.Fatalf("expected 1 wake, got %d", len(wakes))
	}
	if wakes[0].ReportPath != "" {
		t.Errorf("expected an empty ReportPath for a blocked scout, got %q", wakes[0].ReportPath)
	}
}

// A running task whose live herdr status settled to done gets its
// persisted status updated and a wake recorded.
func TestTick_RecordsWakeOnTransition(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	task := newRunningTask(t, proj, "vx-do-the-thing")

	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "done"}}

	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if woke != 1 {
		t.Fatalf("expected 1 wake, got %d", woke)
	}

	persisted, err := state.Load(proj, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Status != state.StatusDone {
		t.Errorf("expected persisted status done, got %s", persisted.Status)
	}

	wakes, err := sentinel.Drain(proj)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(wakes) != 1 || wakes[0].TaskID != task.ID || wakes[0].NewStatus != state.StatusDone {
		t.Errorf("expected one drained wake for %s -> done, got %+v", task.ID, wakes)
	}
}

// A task that settles captures its final transcript, since dispatch's
// own quick-settle probe (internal/soldier.RunInHerdr) usually returns
// long before real work finishes - the sentinel is now the one that
// reads it.
func TestTick_CapturesOutputOnTransition(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	task := newRunningTask(t, proj, "vx-do-the-thing")

	client := &fakeHerdr{
		statuses:   map[string]string{"vx-do-the-thing": "done"},
		readOutput: "soldier: all done, opened PR #4",
	}

	if _, err := sentinel.Tick(home, client); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	persisted, err := state.Load(proj, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Output != "soldier: all done, opened PR #4" {
		t.Errorf("expected captured output, got %q", persisted.Output)
	}
}

// A just-submitted task (UpdatedAt still fresh) is never acted on, even
// if its live status already reads as settled - the exact race caught
// live: the sentinel auto-starts right alongside dispatch, and herdr can
// briefly still report "idle" (not yet picked up the prompt) before an
// agent transitions to "working". Without this grace period, that
// stale idle gets misread as "already done".
func TestTick_SkipsTasksWithinSettleGracePeriod(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	task, err := state.New(state.KindMission, "do the thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.Status = state.StatusRunning
	task.HerdrAgentName = "vx-do-the-thing"
	// Deliberately NOT backdated - this is what a task looks like the
	// instant after RunInHerdr submits its prompt.
	if err := state.Save(proj, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "idle"}}

	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if woke != 0 {
		t.Errorf("expected 0 wakes within the settle grace period, got %d", woke)
	}

	persisted, err := state.Load(proj, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Status != state.StatusRunning {
		t.Errorf("expected status to remain running within the grace period, got %s", persisted.Status)
	}
}

// A task whose live status hasn't changed produces no wake.
func TestTick_NoWakeWithoutTransition(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	newRunningTask(t, proj, "vx-do-the-thing")

	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "working"}}

	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if woke != 0 {
		t.Errorf("expected 0 wakes for an unchanged status, got %d", woke)
	}
}

// Drain only returns a wake once - a second drain with nothing new
// yields nothing.
func TestDrain_OnlySurfacesEachWakeOnce(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	newRunningTask(t, proj, "vx-do-the-thing")
	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "done"}}

	if _, err := sentinel.Tick(home, client); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	first, err := sentinel.Drain(proj)
	if err != nil {
		t.Fatalf("Drain 1: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("expected 1 wake on first drain, got %d", len(first))
	}

	second, err := sentinel.Drain(proj)
	if err != nil {
		t.Fatalf("Drain 2: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("expected 0 wakes on second drain, got %d", len(second))
	}
}

// C1-07: Drain deletes a delivered wake's file outright, rather than
// rewriting it with some acked marker - a directory listing of pending
// wakes (wakes/) is never left holding files for wakes that already went
// out.
func TestDrain_DeletesTheWakeFileOnDelivery(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	task := newRunningTask(t, proj, "vx-do-the-thing")
	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "done"}}

	if _, err := sentinel.Tick(home, client); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	wakeFile := filepath.Join(proj, "wakes", task.ID+".json")
	if _, err := os.Stat(wakeFile); err != nil {
		t.Fatalf("expected a wake file to exist before draining: %v", err)
	}

	if _, err := sentinel.Drain(proj); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	if _, err := os.Stat(wakeFile); !os.IsNotExist(err) {
		t.Errorf("expected the wake file to be removed after Drain, stat error: %v", err)
	}
}

// Tasks that aren't running (pending/done/failed/blocked already) are
// never polled - only running tasks are actionable for the sentinel.
func TestTick_IgnoresNonRunningTasks(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	task, err := state.New(state.KindScout, "look into it")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.Status = state.StatusDone
	task.HerdrAgentName = "vx-look-into-it"
	if err := state.Save(proj, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	client := &fakeHerdr{statuses: map[string]string{"vx-look-into-it": "blocked"}}
	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if woke != 0 {
		t.Errorf("expected 0 wakes for a non-running task, got %d", woke)
	}
}

// A transient AgentStatus read failure on one task doesn't fail the
// whole tick.
func TestTick_ToleratesTransientReadFailure(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	newRunningTask(t, proj, "vx-do-the-thing")

	client := &fakeHerdr{err: errFake{}}
	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick should not fail on a transient read error: %v", err)
	}
	if woke != 0 {
		t.Errorf("expected 0 wakes, got %d", woke)
	}
}

type errFake struct{}

func (errFake) Error() string { return "transient herdr read failure" }

// The first time Tick sees a task's agent as genuinely gone
// (agent_not_found, not a generic read error), it just records the
// moment - it doesn't interrupt the task yet, in case this is only a
// momentary blip.
func TestTick_FirstNotFoundJustMarksIt(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	task := newRunningTask(t, proj, "vx-do-the-thing")

	client := &fakeHerdr{err: &herdr.APIError{Code: "agent_not_found", Message: "agent target vx-do-the-thing not found"}}
	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if woke != 0 {
		t.Errorf("expected 0 wakes on the first not-found observation, got %d", woke)
	}

	persisted, err := state.Load(proj, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Status != state.StatusRunning {
		t.Errorf("expected status to remain running on the first observation, got %s", persisted.Status)
	}
	if persisted.AgentNotFoundSince.IsZero() {
		t.Error("expected AgentNotFoundSince to be recorded")
	}
}

// A second not-found observation still within the confirm window
// doesn't interrupt the task yet either.
func TestTick_NotFoundWithinConfirmWindowDoesNotInterrupt(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	task := newRunningTask(t, proj, "vx-do-the-thing")
	task.AgentNotFoundSince = time.Now().Add(-3 * time.Second)
	if err := state.Save(proj, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	client := &fakeHerdr{err: &herdr.APIError{Code: "agent_not_found", Message: "agent target vx-do-the-thing not found"}}
	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if woke != 0 {
		t.Errorf("expected 0 wakes within the confirm window, got %d", woke)
	}

	persisted, err := state.Load(proj, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Status != state.StatusRunning {
		t.Errorf("expected status to remain running within the confirm window, got %s", persisted.Status)
	}
}

// A task whose agent has been confirmed gone (not-found held true past
// the confirm window) is marked interrupted, and a wake is recorded for
// it - the fix for the restart-proof gap: an agent that's genuinely
// disappeared no longer leaves its task Running forever.
func TestTick_NotFoundPastConfirmWindowInterruptsTask(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	task := newRunningTask(t, proj, "vx-do-the-thing")
	task.AgentNotFoundSince = time.Now().Add(-11 * time.Second)
	if err := state.Save(proj, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	client := &fakeHerdr{err: &herdr.APIError{Code: "agent_not_found", Message: "agent target vx-do-the-thing not found"}}
	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if woke != 1 {
		t.Fatalf("expected 1 wake once the confirm window elapses, got %d", woke)
	}

	persisted, err := state.Load(proj, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Status != state.StatusInterrupted {
		t.Errorf("expected status interrupted, got %s", persisted.Status)
	}

	wakes, err := sentinel.Drain(proj)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(wakes) != 1 || wakes[0].TaskID != task.ID || wakes[0].NewStatus != state.StatusInterrupted {
		t.Errorf("expected one drained wake for %s -> interrupted, got %+v", task.ID, wakes)
	}
}

// Capa 4 paso 5 (fault isolation), formalized: three tasks in one Tick
// call - one settles normally, one has its agent confirmed gone
// (interrupted), one is still genuinely running - none of that interferes
// with any of the others. Matches a live test: three real parallel
// `vexillum dispatch` missions, one pane killed on purpose, the other two
// landed clean while the killed one interrupted independently.
func TestTick_OneFailingTaskDoesNotAffectItsSiblings(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	settled := newRunningTask(t, proj, "vx-settles-fine")
	failing := newRunningTask(t, proj, "vx-agent-is-gone")
	failing.AgentNotFoundSince = time.Now().Add(-11 * time.Second)
	if err := state.Save(proj, failing); err != nil {
		t.Fatalf("state.Save (failing): %v", err)
	}
	stillRunning := newRunningTask(t, proj, "vx-still-working")

	client := &fakeHerdr{
		statuses: map[string]string{
			"vx-settles-fine":  "done",
			"vx-still-working": "working",
		},
		errFor: map[string]error{
			"vx-agent-is-gone": &herdr.APIError{Code: "agent_not_found", Message: "agent target vx-agent-is-gone not found"},
		},
	}

	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if woke != 2 {
		t.Fatalf("expected 2 wakes (settled + interrupted; still-working produces none), got %d", woke)
	}

	gotSettled, err := state.Load(proj, settled.ID)
	if err != nil {
		t.Fatalf("Load settled: %v", err)
	}
	if gotSettled.Status != state.StatusDone {
		t.Errorf("expected the settled task to be done, got %s", gotSettled.Status)
	}

	gotFailing, err := state.Load(proj, failing.ID)
	if err != nil {
		t.Fatalf("Load failing: %v", err)
	}
	if gotFailing.Status != state.StatusInterrupted {
		t.Errorf("expected the failing task to be interrupted, got %s", gotFailing.Status)
	}

	gotStillRunning, err := state.Load(proj, stillRunning.ID)
	if err != nil {
		t.Fatalf("Load stillRunning: %v", err)
	}
	if gotStillRunning.Status != state.StatusRunning {
		t.Errorf("expected the still-working task to remain running, got %s", gotStillRunning.Status)
	}
}

// A prior not-found mark is cleared once the agent resolves fine again -
// a blip, not a real teardown - so a later genuine disappearance starts
// its own fresh confirmation window rather than interrupting instantly.
func TestTick_RecoveringFromNotFoundClearsTheMark(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	task := newRunningTask(t, proj, "vx-do-the-thing")
	task.AgentNotFoundSince = time.Now().Add(-3 * time.Second)
	if err := state.Save(proj, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "working"}}
	if _, err := sentinel.Tick(home, client); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	persisted, err := state.Load(proj, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !persisted.AgentNotFoundSince.IsZero() {
		t.Error("expected AgentNotFoundSince to be cleared after the agent resolved fine again")
	}
	if persisted.Status != state.StatusRunning {
		t.Errorf("expected status to remain running, got %s", persisted.Status)
	}
}

// newRunningMissionTaskWithCamp mirrors newRunningTask but lets the
// caller control the camp's completion signal directly (campPath/base),
// for tests exercising internal/sentinel's own strong-signal and
// declared-pause logic rather than plain wake/transition mechanics.
func newRunningMissionTaskWithCamp(t *testing.T, projectRoot, agentName, campPath, base string) state.Task {
	t.Helper()
	task, err := state.New(state.KindMission, "do the thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.Status = state.StatusRunning
	task.HerdrAgentName = agentName
	task.CampPath = campPath
	task.CampBase = base
	task.UpdatedAt = time.Now().Add(-1 * time.Minute)
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	return task
}

func writePauseFile(t *testing.T, proj, agentName, content string) {
	t.Helper()
	if err := os.MkdirAll(pause.Dir(proj), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(pause.Path(proj, agentName), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// The motivating bug, scenario (a): a mission that declared a pause
// before going idle (a background job it started, still in flight) is
// never marked done just because its turn went idle - internal/pause's
// whole reason for existing.
func TestTick_DeclaredPauseKeepsMissionRunning(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	campPath, base := newEmptyMissionCamp(t)
	task := newRunningMissionTaskWithCamp(t, proj, "vx-do-the-thing", campPath, base)
	writePauseFile(t, proj, "vx-do-the-thing", "paused: waiting on my own e2e validation run\nuntil: the run finishes\n")

	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "idle"}}
	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if woke != 0 {
		t.Errorf("expected no wake while a declared pause is active, got %d", woke)
	}

	persisted, err := state.Load(proj, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Status != state.StatusRunning {
		t.Errorf("expected status to remain running while paused, got %s", persisted.Status)
	}
}

// Scenario (b): a mission that genuinely finished - a real commit ahead
// of its camp's base - is marked done as soon as its turn goes idle, same
// as before this package required corroboration.
func TestTick_MissionWithNewCommitSettlesDone(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	campPath, base := newCommittedMissionCamp(t)
	task := newRunningMissionTaskWithCamp(t, proj, "vx-do-the-thing", campPath, base)

	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "done"}}
	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if woke != 1 {
		t.Fatalf("expected 1 wake, got %d", woke)
	}

	persisted, err := state.Load(proj, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Status != state.StatusDone {
		t.Errorf("expected status done, got %s", persisted.Status)
	}
}

// Scenario (c), first observation: a mission that goes idle with no
// declared pause and no commit ahead of base is not marked done - the
// sentinel records the ambiguity instead, mirroring
// TestTick_FirstNotFoundJustMarksIt.
func TestTick_IdleWithoutSignalOrPauseFirstObservationStaysRunning(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	campPath, base := newEmptyMissionCamp(t)
	task := newRunningMissionTaskWithCamp(t, proj, "vx-do-the-thing", campPath, base)

	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "idle"}}
	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if woke != 0 {
		t.Errorf("expected no wake on the first ambiguous-idle observation, got %d", woke)
	}

	persisted, err := state.Load(proj, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Status != state.StatusRunning {
		t.Errorf("expected status to remain running, got %s", persisted.Status)
	}
	if persisted.IdleUnconfirmedSince.IsZero() {
		t.Error("expected IdleUnconfirmedSince to be recorded")
	}
}

// A second ambiguous-idle observation still within the confirm window
// doesn't mark the task unconfirmed yet either - mirrors
// TestTick_NotFoundWithinConfirmWindowDoesNotInterrupt.
func TestTick_IdleWithoutSignalOrPauseWithinConfirmWindowStaysRunning(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	campPath, base := newEmptyMissionCamp(t)
	task := newRunningMissionTaskWithCamp(t, proj, "vx-do-the-thing", campPath, base)
	task.IdleUnconfirmedSince = time.Now().Add(-5 * time.Second)
	if err := state.Save(proj, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "idle"}}
	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if woke != 0 {
		t.Errorf("expected no wake within the confirm window, got %d", woke)
	}

	persisted, err := state.Load(proj, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Status != state.StatusRunning {
		t.Errorf("expected status to remain running within the confirm window, got %s", persisted.Status)
	}
}

// Scenario (c), confirmed: once the ambiguity has held true past the
// confirm window, the task is marked unconfirmed (never done) and a wake
// is recorded - the actual fix for the motivating bug: an idle turn with
// no proof either way no longer gets a free pass to "done". Mirrors
// TestTick_NotFoundPastConfirmWindowInterruptsTask.
func TestTick_IdleWithoutSignalOrPausePastConfirmWindowBecomesUnconfirmed(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	campPath, base := newEmptyMissionCamp(t)
	task := newRunningMissionTaskWithCamp(t, proj, "vx-do-the-thing", campPath, base)
	task.IdleUnconfirmedSince = time.Now().Add(-31 * time.Second)
	if err := state.Save(proj, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "idle"}}
	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if woke != 1 {
		t.Fatalf("expected 1 wake once the confirm window elapses, got %d", woke)
	}

	persisted, err := state.Load(proj, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Status != state.StatusUnconfirmed {
		t.Errorf("expected status unconfirmed, got %s", persisted.Status)
	}
	if persisted.Status == state.StatusDone {
		t.Error("an unproven idle turn must never be marked done")
	}

	wakes, err := sentinel.Drain(proj)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(wakes) != 1 || wakes[0].TaskID != task.ID || wakes[0].NewStatus != state.StatusUnconfirmed {
		t.Errorf("expected one drained wake for %s -> unconfirmed, got %+v", task.ID, wakes)
	}
}

// A declared pause written after the ambiguity was first observed still
// rescues the task: the mark is cleared and the turn is treated as a
// known wait on the very next tick, instead of racing toward unconfirmed -
// a soldier can legitimately declare its pause a little after its turn
// actually goes idle (the same kind of landing race internal/report's own
// doc comment calls out for a report file).
func TestTick_LatePauseClearsIdleUnconfirmedMark(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	campPath, base := newEmptyMissionCamp(t)
	task := newRunningMissionTaskWithCamp(t, proj, "vx-do-the-thing", campPath, base)
	task.IdleUnconfirmedSince = time.Now().Add(-5 * time.Second)
	if err := state.Save(proj, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	writePauseFile(t, proj, "vx-do-the-thing", "paused: waiting on my own e2e validation run\n")

	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "idle"}}
	if _, err := sentinel.Tick(home, client); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	persisted, err := state.Load(proj, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !persisted.IdleUnconfirmedSince.IsZero() {
		t.Error("expected IdleUnconfirmedSince to be cleared once a declared pause is found")
	}
	if persisted.Status != state.StatusRunning {
		t.Errorf("expected status to remain running, got %s", persisted.Status)
	}
}

// A pause file older than pause.ValidityWindow is no longer trusted - the
// ambiguous-idle handling takes over as if no pause had ever been
// declared, so a soldier that got genuinely stuck after a stale pause
// still eventually surfaces to the commander.
func TestTick_StalePauseDoesNotBlockUnconfirmedEscalation(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	campPath, base := newEmptyMissionCamp(t)
	task := newRunningMissionTaskWithCamp(t, proj, "vx-do-the-thing", campPath, base)
	task.IdleUnconfirmedSince = time.Now().Add(-31 * time.Second)
	if err := state.Save(proj, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	writePauseFile(t, proj, "vx-do-the-thing", "paused: waiting on my own e2e validation run\n")
	stale := time.Now().Add(-pause.ValidityWindow - time.Minute)
	if err := os.Chtimes(pause.Path(proj, "vx-do-the-thing"), stale, stale); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "idle"}}
	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if woke != 1 {
		t.Fatalf("expected the stale pause to be ignored and the task escalated, got %d wakes", woke)
	}

	persisted, err := state.Load(proj, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Status != state.StatusUnconfirmed {
		t.Errorf("expected status unconfirmed, got %s", persisted.Status)
	}
}

// C1-08: Tick sweeps every project namespaced under vexillumHome, not
// just one - a wake recorded for a task in project A must never leak
// into project B's own wakes/, and vice versa. This is the core guarantee
// the whole namespacing change exists for: two projects open at once no
// longer share tasks/ or wakes/.
func TestTick_SweepsEveryProjectAndKeepsWakesIsolated(t *testing.T) {
	home := t.TempDir()
	projA := projectRoot(home, "proj-a")
	projB := projectRoot(home, "proj-b")

	taskA := newRunningTask(t, projA, "vx-task-a")
	taskB := newRunningTask(t, projB, "vx-task-b")

	client := &fakeHerdr{statuses: map[string]string{
		"vx-task-a": "done",
		"vx-task-b": "done",
	}}

	woke, err := sentinel.Tick(home, client)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if woke != 2 {
		t.Fatalf("expected 2 wakes across both projects, got %d", woke)
	}

	wakesA, err := sentinel.Drain(projA)
	if err != nil {
		t.Fatalf("Drain(A): %v", err)
	}
	if len(wakesA) != 1 || wakesA[0].TaskID != taskA.ID {
		t.Errorf("expected project A's drain to contain only task A, got %+v", wakesA)
	}

	wakesB, err := sentinel.Drain(projB)
	if err != nil {
		t.Fatalf("Drain(B): %v", err)
	}
	if len(wakesB) != 1 || wakesB[0].TaskID != taskB.ID {
		t.Errorf("expected project B's drain to contain only task B, got %+v", wakesB)
	}
}

// Sanity: Drain on a project with no wakes yet returns an empty slice,
// not an error.
func TestDrain_EmptyWhenNoWakesDir(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	wakes, err := sentinel.Drain(proj)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(wakes) != 0 {
		t.Errorf("expected no wakes, got %d", len(wakes))
	}
}

// AcquireLock refuses a second lock while this process (a real, live
// pid - our own) holds it, and the released lock can be re-acquired.
// Global - keyed directly off vexillumHome, not a project root: one
// sentinel per machine, not one per project.
func TestAcquireLock_RefusesSecondWhileHeld(t *testing.T) {
	home := t.TempDir()

	release, err := sentinel.AcquireLock(home)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}

	if _, err := sentinel.AcquireLock(home); err == nil {
		t.Fatal("expected a second AcquireLock to refuse while the first is held")
	}

	release()

	release2, err := sentinel.AcquireLock(home)
	if err != nil {
		t.Fatalf("AcquireLock after release: %v", err)
	}
	release2()
}

// A lock file left behind by a process that's no longer running (a
// stale pid) is reclaimed instead of refusing forever.
func TestAcquireLock_ReclaimsStaleLock(t *testing.T) {
	home := t.TempDir()

	// A pid essentially guaranteed to be dead/unowned.
	const deadPID = 999999
	if err := os.WriteFile(filepath.Join(home, "sentinel.pid"), []byte(strconv.Itoa(deadPID)), 0o644); err != nil {
		t.Fatalf("writing stale lock: %v", err)
	}

	release, err := sentinel.AcquireLock(home)
	if err != nil {
		t.Fatalf("expected AcquireLock to reclaim a stale lock, got: %v", err)
	}
	release()
}

// IsRunning reflects a live lock without claiming it itself - dispatch
// uses this to decide whether to auto-start a sentinel.
func TestIsRunning(t *testing.T) {
	home := t.TempDir()

	if sentinel.IsRunning(home) {
		t.Error("expected IsRunning to be false with no lock file at all")
	}

	release, err := sentinel.AcquireLock(home)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if !sentinel.IsRunning(home) {
		t.Error("expected IsRunning to be true while this process holds the lock")
	}

	release()
	if sentinel.IsRunning(home) {
		t.Error("expected IsRunning to be false after the lock was released")
	}
}

// A stale lock file (dead pid) reads as not running, same as
// AcquireLock's own reclaim logic.
func TestIsRunning_FalseForStaleLock(t *testing.T) {
	home := t.TempDir()
	const deadPID = 999999
	if err := os.WriteFile(filepath.Join(home, "sentinel.pid"), []byte(strconv.Itoa(deadPID)), 0o644); err != nil {
		t.Fatalf("writing stale lock: %v", err)
	}

	if sentinel.IsRunning(home) {
		t.Error("expected IsRunning to be false for a stale pid")
	}
}
