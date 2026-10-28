package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/herdr"
	vxproject "github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/sentinel"
	"github.com/isaias-alt/vexillum/internal/state"
)

// A dispatched project that has been "vx init"-ed, with a commit.
func initializedDispatchProject(t *testing.T) string {
	t.Helper()
	dir := initDispatchTestProject(t)
	if err := writeLocalConfig(filepath.Join(dir, ".vexillum")); err != nil {
		t.Fatalf("writing local config: %v", err)
	}
	return dir
}

// The reported bug, end to end with fakes: a commander works in repository A
// and dispatches into repository B. Its Stop hook await runs in A, so a wake
// recorded in B's queue was never heard. The wake must reach the await of the
// commander that dispatched the task.
func TestNotificationChain_ASoldierDispatchedIntoAnotherProjectWakesItsCommander(t *testing.T) {
	home := t.TempDir()
	repoA := initializedDispatchProject(t)
	repoB := initializedDispatchProject(t)
	rootA, _ := vxproject.Root(home, repoA)
	rootB, _ := vxproject.Root(home, repoB)

	// The commander's pane works in A.
	useLocator(t, "w1:p1", &stubLocator{agents: map[string]herdr.AgentInfo{"w1:p1": {Kind: "claude", PaneID: "w1:p1", Cwd: repoA}}})

	client := &fakeHerdr{tabID: "w1:t1", paneID: "w1:p2", promptStatus: "working"}
	var out, errb bytes.Buffer
	if code := runDispatch(repoB, home, "w1", "do a thing", state.KindMission, "", "", "", client, &out, &errb); code != 0 {
		t.Fatalf("dispatch exit %d: %s%s", code, out.String(), errb.String())
	}
	if !strings.Contains(errb.String(), "warning: dispatching into") || !strings.Contains(errb.String(), repoA) {
		t.Errorf("a dispatch from another project's commander should warn and name that project, stderr: %q", errb.String())
	}

	tasks, _, err := state.List(rootB)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks in B: %v, %v", tasks, err)
	}
	task := tasks[0]
	if task.CommanderProject != rootA || task.CommanderPane != "w1:p1" {
		t.Fatalf("task commander = pane %q in %q, want w1:p1 in %q", task.CommanderPane, task.CommanderProject, rootA)
	}

	// The soldier delivers and settles.
	runGitT(t, task.CampPath, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-q", "-m", "work")
	task.UpdatedAt = time.Now().Add(-time.Minute)
	task.PromptSubmittedAt = task.UpdatedAt
	if err := state.Save(rootB, task); err != nil {
		t.Fatal(err)
	}
	client.promptStatus = "done"
	if woke, err := sentinel.Tick(home, client); err != nil || woke != 1 {
		t.Fatalf("Tick: woke=%d err=%v", woke, err)
	}

	if files := pendingWakeFiles(t, rootB); len(files) != 0 {
		t.Errorf("the dispatch project's queue holds %v, want nothing: nobody awaits there", files)
	}
	code, text := await(rootA, time.Hour)
	if code != 2 || !strings.Contains(text, task.ID) {
		t.Fatalf("the commander's await in A got exit %d: %q, want the wake of %s", code, text, task.ID)
	}
	if code, text := await(rootA, 30*time.Millisecond); code != 0 {
		t.Fatalf("the wake was delivered twice: %d %q", code, text)
	}
}

// Without a commander pane (a plain shell) nothing changes: the wake goes to
// the task's own project.
func TestNotificationChain_ADispatchOutsideAPaneKeepsTheWakeInItsOwnProject(t *testing.T) {
	home := t.TempDir()
	repoB := initializedDispatchProject(t)
	rootB, _ := vxproject.Root(home, repoB)
	useLocator(t, "", &stubLocator{})

	client := &fakeHerdr{tabID: "w1:t1", paneID: "w1:p2", promptStatus: "working"}
	var out bytes.Buffer
	if code := runDispatch(repoB, home, "w1", "do a thing", state.KindMission, "", "", "", client, &out, &out); code != 0 {
		t.Fatalf("dispatch exit %d: %s", code, out.String())
	}
	tasks, _, _ := state.List(rootB)
	if len(tasks) != 1 || tasks[0].CommanderProject != "" || tasks[0].CommanderPane != "" {
		t.Fatalf("task = %+v, want no commander recorded", tasks)
	}
	if strings.Contains(out.String(), "warning") {
		t.Errorf("unexpected warning: %s", out.String())
	}
}

// A commander that works in the project it dispatches into is the ordinary
// case: it is recorded, and nothing is said.
func TestRunDispatch_ACommanderInTheSameProjectIsRecordedWithoutAWarning(t *testing.T) {
	home := t.TempDir()
	repo := initializedDispatchProject(t)
	root, _ := vxproject.Root(home, repo)
	useLocator(t, "w1:p1", &stubLocator{agents: map[string]herdr.AgentInfo{"w1:p1": {Kind: "claude", PaneID: "w1:p1", Cwd: repo}}})

	client := &fakeHerdr{tabID: "w1:t1", paneID: "w1:p2", promptStatus: "working"}
	var out, errb bytes.Buffer
	if code := runDispatch(repo, home, "w1", "do a thing", state.KindMission, "", "", "", client, &out, &errb); code != 0 {
		t.Fatalf("dispatch exit %d: %s%s", code, out.String(), errb.String())
	}
	tasks, _, _ := state.List(root)
	if len(tasks) != 1 || tasks[0].CommanderProject != root || tasks[0].CommanderPane != "w1:p1" {
		t.Fatalf("task = %+v, want the commander recorded in %s", tasks, root)
	}
	if strings.Contains(errb.String(), "warning") {
		t.Errorf("unexpected warning: %s", errb.String())
	}
}

// A live await in the commander's pane is the better witness of where wakes are
// heard than the agent's working directory.
func TestResolveCommanderHome_PrefersTheProjectOfTheLiveAwaitOfThePane(t *testing.T) {
	home := t.TempDir()
	repoB := initializedDispatchProject(t)
	rootB, _ := vxproject.Root(home, repoB)
	awaitedRoot := filepath.Join(home, "projects", "ideaverse-12345678")

	dir := filepath.Join(home, "sentinel-awaiters")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	rec := `{"pid":` + strconv.Itoa(os.Getpid()) + `,"owner_pid":1,"project_root":"` + awaitedRoot + `","pane_id":"w1:p1","started_at":"2026-10-28T10:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(os.Getpid())+".json"), []byte(rec), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_PANE_ID", "w1:p1")
	loc := &stubLocator{agents: map[string]herdr.AgentInfo{"w1:p1": {Kind: "claude", PaneID: "w1:p1", Cwd: repoB}}}

	got := resolveCommanderHome(home, rootB, repoB, loc)
	if got.Root != awaitedRoot || got.Pane != "w1:p1" {
		t.Errorf("home = %+v, want the awaited project %s", got, awaitedRoot)
	}
}

func claimableWake(t *testing.T, root string) sentinel.Wake {
	t.Helper()
	task, err := state.New(state.KindScout, "look into it")
	if err != nil {
		t.Fatal(err)
	}
	task.Status = state.StatusDone
	if err := state.Save(root, task); err != nil {
		t.Fatal(err)
	}
	w := sentinel.Wake{TaskID: task.ID, Kind: task.Kind, OldStatus: state.StatusRunning, NewStatus: state.StatusDone, DetectedAt: time.Now().UTC()}
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "wakes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "wakes", w.TaskID+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return w
}

// The await used to consume the wake before printing it, so a hook that was
// already gone lost it. Now the wake is put back when the await's owner is gone.
func TestAwait_APrintIntoAGoneHookPutsTheWakeBack(t *testing.T) {
	root := filepath.Join(t.TempDir(), "projects", "proj")
	w := claimableWake(t, root)

	var stderr bytes.Buffer
	code := runSentinelAwait(root, time.Hour, time.Millisecond, &stderr, func() bool { return false }, nil)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit %d, stderr %q: a hook that is gone must be told nothing", code, stderr.String())
	}
	if files := pendingWakeFiles(t, root); len(files) != 1 || files[0] != w.TaskID+".json" {
		t.Fatalf("pending after the failed delivery = %v, want the wake back", files)
	}
	// The next await, with a hook that is there, gets it.
	if code, out := await(root, time.Hour); code != 2 || !strings.Contains(out, w.TaskID) {
		t.Fatalf("the next await got %d: %q", code, out)
	}
}

func TestAwait_AnInterruptBetweenClaimAndPrintPutsTheWakeBack(t *testing.T) {
	root := filepath.Join(t.TempDir(), "projects", "proj")
	w := claimableWake(t, root)

	interrupt := make(chan os.Signal, 1)
	interrupt <- syscall.SIGTERM
	var stderr bytes.Buffer
	if code := runSentinelAwait(root, time.Hour, time.Millisecond, &stderr, stillWanted, interrupt); code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if files := pendingWakeFiles(t, root); len(files) != 1 || files[0] != w.TaskID+".json" {
		t.Fatalf("pending = %v, want the wake back", files)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, syscall.EPIPE }

func TestAwait_AFailedWritePutsTheWakeBack(t *testing.T) {
	root := filepath.Join(t.TempDir(), "projects", "proj")
	w := claimableWake(t, root)
	if code := runSentinelAwait(root, time.Hour, time.Millisecond, failingWriter{}, stillWanted, nil); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if files := pendingWakeFiles(t, root); len(files) != 1 || files[0] != w.TaskID+".json" {
		t.Fatalf("pending = %v, want the wake back", files)
	}
}

func TestAwait_ADeliveredWakeIsConsumedAndLeavesNoClaimBehind(t *testing.T) {
	root := filepath.Join(t.TempDir(), "projects", "proj")
	w := claimableWake(t, root)
	code, out := await(root, time.Hour)
	if code != 2 || !strings.Contains(out, w.TaskID) {
		t.Fatalf("exit %d: %q", code, out)
	}
	if files := pendingWakeFiles(t, root); len(files) != 0 {
		t.Errorf("files after delivery = %v, want none (not even a claim)", files)
	}
}

func TestDrain_APrintToAClosedReaderPutsTheWakeBack(t *testing.T) {
	root := filepath.Join(t.TempDir(), "projects", "proj")
	w := claimableWake(t, root)

	var stderr bytes.Buffer
	if code := runSentinelDrain(root, failingWriter{}, &stderr); code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, stderr.String())
	}
	if files := pendingWakeFiles(t, root); len(files) != 1 || files[0] != w.TaskID+".json" {
		t.Fatalf("pending = %v, want the wake back", files)
	}
	var stdout bytes.Buffer
	if code := runSentinelDrain(root, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), w.TaskID) {
		t.Fatalf("the retry got %d: %q", code, stdout.String())
	}
}
