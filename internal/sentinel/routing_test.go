package sentinel_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/sentinel"
	"github.com/isaias-alt/vexillum/internal/state"
)

// The reported bug: a commander in project A dispatched a soldier into project
// B, and the wake sat in B's queue, which only a Stop hook await running in B
// drains. The wake now goes to the queue of the commander that dispatched the
// task, and is judged against the task where it lives.
func TestTick_WakeGoesToTheCommanderThatDispatchedTheTask(t *testing.T) {
	home := t.TempDir()
	taskProj := projectRoot(home, "calculadolar-1")
	commanderProj := projectRoot(home, "ideaverse-2")
	task := newRunningTask(t, taskProj, "vx-do-the-thing")
	task.CommanderProject = commanderProj
	task.CommanderPane = "w1:p1"
	if err := state.Save(taskProj, task); err != nil {
		t.Fatal(err)
	}
	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "done"}}

	if woke, err := sentinel.Tick(home, client); err != nil || woke != 1 {
		t.Fatalf("Tick: woke=%d err=%v", woke, err)
	}

	if files := wakeFiles(t, taskProj); len(files) != 0 {
		t.Errorf("the task's own project got %v, want nothing", files)
	}
	claim, err := sentinel.Claim(commanderProj)
	if err != nil {
		t.Fatal(err)
	}
	if len(claim.Wakes) != 1 || claim.Wakes[0].TaskID != task.ID || claim.Wakes[0].Origin != taskProj {
		t.Fatalf("the commander's queue holds %+v, want the wake of %s with origin %s", claim.Wakes, task.ID, taskProj)
	}
	_ = claim.Commit()
}

func TestTick_WakeStaysInTheTaskProjectWithoutACommander(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	task := newRunningTask(t, proj, "vx-do-the-thing")
	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "done"}}
	if _, err := sentinel.Tick(home, client); err != nil {
		t.Fatal(err)
	}
	claim, _ := sentinel.Claim(proj)
	if len(claim.Wakes) != 1 || claim.Wakes[0].TaskID != task.ID || claim.Wakes[0].Origin != "" {
		t.Errorf("wakes = %+v, want one without an origin", claim.Wakes)
	}
}

// A commander project that is not a sibling under the same projects directory is
// not one vexillum made; the wake stays with the task rather than being written
// wherever a task file points.
func TestTick_ACommanderProjectOutsideTheProjectsDirectoryIsIgnored(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	task := newRunningTask(t, proj, "vx-do-the-thing")
	task.CommanderProject = elsewhere
	if err := state.Save(proj, task); err != nil {
		t.Fatal(err)
	}
	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "done"}}
	if _, err := sentinel.Tick(home, client); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "wakes")); !os.IsNotExist(err) {
		t.Errorf("a wake was written outside the projects directory (stat: %v)", err)
	}
	if claim, _ := sentinel.Claim(proj); len(claim.Wakes) != 1 {
		t.Errorf("the task's own project holds %+v, want its wake", claim.Wakes)
	}
}

func TestClaim_ARoutedWakeIsJudgedAgainstItsTaskInTheOriginProject(t *testing.T) {
	home := t.TempDir()
	taskProj := projectRoot(home, "calculadolar-1")
	commanderProj := projectRoot(home, "ideaverse-2")
	task, _ := state.New(state.KindScout, "look")
	task.Status = state.StatusDone
	if err := state.Save(taskProj, task); err != nil {
		t.Fatal(err)
	}
	w := sentinel.Wake{TaskID: task.ID, Kind: task.Kind, OldStatus: state.StatusRunning, NewStatus: state.StatusDone, DetectedAt: time.Now(), Origin: taskProj}

	writeWakeFile(t, commanderProj, w)
	claim, _ := sentinel.Claim(commanderProj)
	if claim.Len() != 1 {
		t.Fatalf("a current routed wake was dropped: %+v", claim.Wakes)
	}
	_ = claim.Release()

	// The commander answered since: the task moved on in its own project.
	task.Status = state.StatusRunning
	if err := state.Save(taskProj, task); err != nil {
		t.Fatal(err)
	}
	if claim, _ := sentinel.Claim(commanderProj); claim.Len() != 0 {
		t.Errorf("a routed wake whose task moved on was delivered: %+v", claim.Wakes)
	}
}

func TestAwaiterProject_FindsTheProjectOfTheNewestLiveAwaitOfAPane(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sentinel-awaiters")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(pid int, root, pane string, started time.Time) {
		t.Helper()
		body := `{"pid":` + strconv.Itoa(pid) + `,"owner_pid":1234,"project_root":"` + root + `","pane_id":"` + pane + `","started_at":"` + started.Format(time.RFC3339Nano) + `"}`
		if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+".json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	self := os.Getpid()
	write(self, "/vx/projects/ideaverse-2", "w1:p1", now)
	write(os.Getppid(), "/vx/projects/older-3", "w1:p1", now.Add(-time.Hour))
	write(2147483, "/vx/projects/dead-4", "w1:p1", now.Add(time.Hour)) // no such process, though the newest
	write(os.Getppid(), "/vx/projects/other-5", "w2:p2", now)

	root, ok := sentinel.AwaiterProject(home, "w1:p1")
	if !ok || root != "/vx/projects/ideaverse-2" {
		t.Errorf("AwaiterProject = %q, %v, want the newest live await of the pane", root, ok)
	}
	if _, ok := sentinel.AwaiterProject(home, "w9:p9"); ok {
		t.Error("a pane with no await has a project")
	}
	if _, ok := sentinel.AwaiterProject(home, ""); ok {
		t.Error("no pane has a project")
	}
}

func TestPendingQueues_ListsProjectsWithWakesWaiting(t *testing.T) {
	home := t.TempDir()
	quiet := projectRoot(home, "quiet-1")
	busy := projectRoot(home, "busy-2")
	if err := os.MkdirAll(filepath.Join(quiet, "wakes"), 0o755); err != nil {
		t.Fatal(err)
	}
	currentWake(t, busy)
	currentWake(t, busy)
	if err := sentinel.RecordForumWake(busy, sentinel.ForumWake{Session: "s1", File: "/a.html", Count: 1}); err != nil {
		t.Fatal(err)
	}
	held, _ := sentinel.Claim(projectRoot(home, "held-3")) // nothing there: no queue
	_ = held

	queues := sentinel.PendingQueues(home)
	if len(queues) != 1 || queues[0].ProjectRoot != busy || queues[0].Count != 3 || queues[0].Oldest.IsZero() {
		t.Errorf("queues = %+v, want only busy-2 with 3 wakes", queues)
	}
}
