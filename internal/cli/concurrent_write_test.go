package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	vxproject "github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/state"
)

// tabCloseHook is a herdr whose TabClose runs a callback first, as another
// command writes the task record while a vx command is partway through.
type tabCloseHook struct {
	fakeHerdr
	during func()
}

func (h *tabCloseHook) TabClose(tabID string) error {
	if h.during != nil {
		during := h.during
		h.during = nil
		during()
	}
	return h.fakeHerdr.TabClose(tabID)
}

const concurrentSHA = "feedface"

// writeConcurrently is the write another command makes to a field none of the
// commands under test owns.
func writeConcurrently(t *testing.T, projectRoot, taskID string) {
	t.Helper()
	if _, err := state.Update(projectRoot, taskID, func(task *state.Task) (bool, error) {
		task.LastPushedSHA = concurrentSHA
		return true, nil
	}); err != nil {
		t.Errorf("concurrent state.Update: %v", err)
	}
}

func TestRunStrike_KeepsAWriteMadeWhileTheCampIsStruck(t *testing.T) {
	m, client := interruptedMissionWithGonePane(t)
	projectRoot, err := vxproject.Root(m.home, m.project)
	if err != nil {
		t.Fatal(err)
	}
	hooked := &tabCloseHook{fakeHerdr: *client, during: func() { writeConcurrently(t, projectRoot, m.task.ID) }}

	var buf bytes.Buffer
	code := runStrike(m.project, m.home, m.task.ID, strikeOptions{Discard: true}, hooked, &buf, &buf)
	if code != 0 {
		t.Fatalf("expected the strike to complete, got %d: %s", code, buf.String())
	}
	got := loadTask(t, m)
	if got.Status != state.StatusStruck {
		t.Errorf("expected the task marked %s, got %s", state.StatusStruck, got.Status)
	}
	if got.LastPushedSHA != concurrentSHA {
		t.Errorf("expected the concurrent write kept, got %q", got.LastPushedSHA)
	}
}

func TestRunRedispatch_KeepsAWriteMadeWhileTheOldCampIsDiscarded(t *testing.T) {
	project := initDispatchTestProject(t)
	home := t.TempDir()
	task := interruptedTestTask(t, project, home)
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatal(err)
	}
	ran := false
	client := &tabCloseHook{
		fakeHerdr: fakeHerdr{tabID: "w2:t2", paneID: "w2:p2", promptStatus: "done", readOutput: "did the thing"},
		during: func() {
			ran = true
			writeConcurrently(t, projectRoot, task.ID)
		},
	}

	var out bytes.Buffer
	if code := runRedispatch(project, home, "w1", task.ID, client, &out, &out); code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	if !ran {
		t.Fatal("expected the old camp's tab to be closed while discarding it")
	}
	got, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Redispatches != 1 || got.Status != state.StatusDone {
		t.Errorf("expected the redispatch recorded and settled, got redispatches=%d status=%s", got.Redispatches, got.Status)
	}
	if got.LastPushedSHA != concurrentSHA {
		t.Errorf("expected the concurrent write kept, got %q", got.LastPushedSHA)
	}
}

// ship runs for minutes (the tribunal), so the task it loaded at the start is
// old by the time it records the shipped status. A write that lands while the
// pull request is being opened must survive it.
func TestRunShip_KeepsAWriteMadeWhileThePullRequestIsOpened(t *testing.T) {
	project := shipTestProject(t)
	home := t.TempDir()
	task := doneMissionTask(t, project, home)
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatal(err)
	}
	taskFile := filepath.Join(projectRoot, "tasks", task.ID+".json")

	// The gh stub edits the task file the way a concurrent writer would: it
	// adds a field to the record. It runs before ship's final write.
	sed, err := exec.LookPath("sed")
	if err != nil {
		t.Skip("sed not found")
	}
	mv, err := exec.LookPath("mv")
	if err != nil {
		t.Skip("mv not found")
	}
	gh := `case "$1 $2" in
  "pr create")
    ` + sed + ` 's/"kind": "mission",/"kind": "mission", "effort": "high",/' '` + taskFile + `' > '` + taskFile + `.edit'
    ` + mv + ` '` + taskFile + `.edit' '` + taskFile + `'
    echo "https://github.com/x/y/pull/1" ;;
esac`
	t.Setenv("PATH", shipToolsPath(t, passingReview, gh))

	var out bytes.Buffer
	if code := runShip(project, home, task.ID, shipOptions{}, &out, &out); code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out.String())
	}
	got, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.StatusShipped {
		t.Errorf("expected shipped, got %s", got.Status)
	}
	if got.Effort != "high" {
		t.Errorf("expected the concurrent write kept, got effort %q (file: %s)", got.Effort, readFileForTest(t, taskFile))
	}
	if got.LastPushedSHA == "" {
		t.Error("expected the pushed commit recorded")
	}
	if got.UpdatedAt.Before(task.UpdatedAt.Add(-time.Second)) {
		t.Errorf("expected a fresh UpdatedAt, got %s", got.UpdatedAt)
	}
}

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	return string(b)
}
