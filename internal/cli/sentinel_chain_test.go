package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/herdr"
	vxproject "github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/sentinel"
	"github.com/isaias-alt/vexillum/internal/state"
)

func pendingWakeFiles(t *testing.T, projectRoot string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(projectRoot, "wakes"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// await runs the Stop hook's await with a bounded wait and returns its exit
// code and what it wrote.
func await(projectRoot string, maxWait time.Duration) (int, string) {
	var stderr bytes.Buffer
	code := runSentinelAwait(projectRoot, maxWait, time.Millisecond, &stderr, stillWanted, nil)
	return code, stderr.String()
}

// The whole notification chain, against a fake herdr and an isolated home:
// a running task whose pane settles yields exactly one wake, await returns
// it once and never again, and a done soldier re-prompted with vx prompt
// yields a new wake when it settles again.
func TestNotificationChain_SettleAwaitRepromptSettle(t *testing.T) {
	home := t.TempDir()
	projectDir := t.TempDir()
	projectRoot, err := vxproject.Root(home, projectDir)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}

	// A running mission whose camp already carries its commit, so a settle
	// is corroborated, past the settle grace period.
	task := newSettledMissionTask(t, projectRoot)
	client := &fakeHerdr{promptStatus: "working"}

	// Still working: nothing to report, and await finds nothing.
	if woke, err := sentinel.Tick(home, client); err != nil || woke != 0 {
		t.Fatalf("Tick while working: woke=%d err=%v", woke, err)
	}
	if code, out := await(projectRoot, 30*time.Millisecond); code != 0 {
		t.Fatalf("expected await to find nothing while the soldier works, got %d: %s", code, out)
	}

	// The pane settles: exactly one wake, however often the sentinel looks.
	client.promptStatus = "done"
	if woke, err := sentinel.Tick(home, client); err != nil || woke != 1 {
		t.Fatalf("Tick on settle: woke=%d err=%v", woke, err)
	}
	for i := 0; i < 3; i++ {
		if woke, err := sentinel.Tick(home, client); err != nil || woke != 0 {
			t.Fatalf("expected no further wakes for an unchanged task, woke=%d err=%v", woke, err)
		}
	}
	if files := pendingWakeFiles(t, projectRoot); len(files) != 1 {
		t.Fatalf("expected exactly one pending wake, got %v", files)
	}

	// await returns it once...
	code, out := await(projectRoot, time.Hour)
	if code != 2 || !strings.Contains(out, task.ID) || !strings.Contains(out, "running -> done") {
		t.Fatalf("expected await to report the task running -> done (exit 2), got %d: %s", code, out)
	}
	// ...and a second await does not return it again.
	if code, out := await(projectRoot, 30*time.Millisecond); code != 0 {
		t.Fatalf("expected the second await to find nothing, got %d: %s", code, out)
	}
	if files := pendingWakeFiles(t, projectRoot); len(files) != 0 {
		t.Fatalf("expected the delivered wake consumed, got %v", files)
	}

	// The commander re-prompts the done soldier with vx prompt; it is still
	// working past the quick probe, so the task is left running.
	client.promptErr = &herdr.APIError{Code: "timeout", Message: "still working"}
	var stdout, stderr bytes.Buffer
	if code := runReprompt(projectDir, home, task.ID, "also add a test", client, &stdout, &stderr); code != 0 {
		t.Fatalf("vx prompt: %d: %s%s", code, stdout.String(), stderr.String())
	}
	got, err := state.Load(projectRoot, task.ID)
	if err != nil || got.Status != state.StatusRunning {
		t.Fatalf("expected the task running again, got %+v err=%v", got.Status, err)
	}
	if code, out := await(projectRoot, 30*time.Millisecond); code != 0 {
		t.Fatalf("expected no wake for the re-prompt itself, got %d: %s", code, out)
	}

	// It settles again: a new wake, delivered once.
	got.UpdatedAt = time.Now().Add(-time.Minute)
	if err := state.Save(projectRoot, got); err != nil {
		t.Fatal(err)
	}
	if woke, err := sentinel.Tick(home, client); err != nil || woke != 1 {
		t.Fatalf("Tick on the second settle: woke=%d err=%v", woke, err)
	}
	code, out = await(projectRoot, time.Hour)
	if code != 2 || !strings.Contains(out, task.ID) {
		t.Fatalf("expected a new wake after the re-prompted soldier settled, got %d: %s", code, out)
	}
	if code, out := await(projectRoot, 30*time.Millisecond); code != 0 {
		t.Fatalf("expected the second wake delivered only once, got %d: %s", code, out)
	}
}

// A wake sitting undelivered while the commander strikes the task is not
// replayed by the next await.
func TestNotificationChain_StruckTaskIsNotReplayed(t *testing.T) {
	home := t.TempDir()
	projectRoot, err := vxproject.Root(home, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	newSettledMissionTask(t, projectRoot)

	client := &fakeHerdr{promptStatus: "done"}
	if woke, err := sentinel.Tick(home, client); err != nil || woke != 1 {
		t.Fatalf("Tick: woke=%d err=%v", woke, err)
	}

	// The camp is struck before anyone listens.
	if err := os.Remove(filepath.Join(projectRoot, "camps", "pool.json")); err != nil {
		t.Fatal(err)
	}
	if code, out := await(projectRoot, 30*time.Millisecond); code != 0 {
		t.Fatalf("expected no replay for a struck task, got %d: %s", code, out)
	}
}
