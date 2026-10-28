package sentinel_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/sentinel"
	"github.com/isaias-alt/vexillum/internal/state"
)

// currentWake records a task that is Done, without a camp, and the pending wake
// announcing it: a wake that is still news.
func currentWake(t *testing.T, root string) sentinel.Wake {
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
	writeWakeFile(t, root, w)
	return w
}

func writeWakeFile(t *testing.T, root string, w sentinel.Wake) {
	t.Helper()
	if err := sentinel.RecordWakeForTest(root, w); err != nil {
		t.Fatal(err)
	}
}

func wakeFiles(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "wakes"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

func TestClaim_KeepsTheWakeUntilItIsCommitted(t *testing.T) {
	root := projectRoot(t.TempDir(), "proj")
	w := currentWake(t, root)

	claim, err := sentinel.Claim(root)
	if err != nil {
		t.Fatal(err)
	}
	if claim.Len() != 1 || claim.Wakes[0].TaskID != w.TaskID {
		t.Fatalf("claimed %+v, want the wake of %s", claim.Wakes, w.TaskID)
	}
	if !strings.Contains(claim.Reason(), w.TaskID) {
		t.Errorf("reason %q does not name the task", claim.Reason())
	}
	// Claimed, so nobody else sees it, but still on disk: a crash now loses nothing.
	if again, _ := sentinel.Claim(root); again.Len() != 0 {
		t.Errorf("a second claim got %+v while the first is held", again.Wakes)
	}
	if files := wakeFiles(t, root); len(files) != 1 || !strings.Contains(files[0], ".claimed-") {
		t.Fatalf("files while claimed = %v, want just the claimed copy", files)
	}

	if err := claim.Commit(); err != nil {
		t.Fatal(err)
	}
	if files := wakeFiles(t, root); len(files) != 0 {
		t.Errorf("files after Commit = %v, want none", files)
	}
}

func TestClaim_ReleaseGivesTheWakeBackToTheNextClaim(t *testing.T) {
	root := projectRoot(t.TempDir(), "proj")
	w := currentWake(t, root)

	claim, _ := sentinel.Claim(root)
	if err := claim.Release(); err != nil {
		t.Fatal(err)
	}
	if files := wakeFiles(t, root); len(files) != 1 || files[0] != w.TaskID+".json" {
		t.Fatalf("files after Release = %v, want the pending wake back", files)
	}
	next, _ := sentinel.Claim(root)
	if next.Len() != 1 || next.Wakes[0].TaskID != w.TaskID {
		t.Fatalf("the next claim got %+v, want the released wake", next.Wakes)
	}
	// Released twice is harmless: the claim is spent.
	if err := claim.Release(); err != nil {
		t.Errorf("second Release: %v", err)
	}
	_ = next.Commit()
}

func TestClaim_ReleaseNeverOverwritesANewerWakeOfTheSameTask(t *testing.T) {
	root := projectRoot(t.TempDir(), "proj")
	old := currentWake(t, root)
	claim, _ := sentinel.Claim(root)

	newer := old
	newer.ReportPath = "/newer.md"
	writeWakeFile(t, root, newer)

	if err := claim.Release(); err != nil {
		t.Fatal(err)
	}
	next, _ := sentinel.Claim(root)
	if next.Len() != 1 || next.Wakes[0].ReportPath != "/newer.md" {
		t.Errorf("the next claim got %+v, want the newer wake", next.Wakes)
	}
	if files := wakeFiles(t, root); len(files) != 1 {
		t.Errorf("files = %v, want only the held claim of the newer wake", files)
	}
}

func TestClaim_ConsumesAWakeThatIsNoLongerNewsAtOnce(t *testing.T) {
	root := projectRoot(t.TempDir(), "proj")
	w := currentWake(t, root)
	// The commander moved on: the task is not Done any more.
	task, _ := state.Load(root, w.TaskID)
	task.Status = state.StatusRunning
	if err := state.Save(root, task); err != nil {
		t.Fatal(err)
	}

	claim, _ := sentinel.Claim(root)
	if claim.Len() != 0 {
		t.Fatalf("claimed %+v, want a stale wake dropped", claim.Wakes)
	}
	if files := wakeFiles(t, root); len(files) != 0 {
		t.Errorf("a stale wake left %v behind", files)
	}
}

func TestClaim_APendingWakeOfAnotherKindOfFileIsLeftAlone(t *testing.T) {
	root := projectRoot(t.TempDir(), "proj")
	if err := os.MkdirAll(filepath.Join(root, "wakes"), 0o755); err != nil {
		t.Fatal(err)
	}
	junk := filepath.Join(root, "wakes", "notes.txt")
	if err := os.WriteFile(junk, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if claim, err := sentinel.Claim(root); err != nil || claim.Len() != 0 {
		t.Fatalf("claim = %+v, err %v", claim, err)
	}
	if _, err := os.Stat(junk); err != nil {
		t.Errorf("an unrelated file was touched: %v", err)
	}
}

// A delivery that died after claiming (SIGKILL, power loss) left its claim
// behind; a later claim finds it again once it is clearly abandoned, so the
// wake is not lost.
func TestClaim_AnAbandonedClaimComesBackAfterAWhile(t *testing.T) {
	root := projectRoot(t.TempDir(), "proj")
	w := currentWake(t, root)
	pending := filepath.Join(root, "wakes", w.TaskID+".json")

	claimedAt := func(age time.Duration) string {
		return fmt.Sprintf("%s.claimed-%d-4242-1", pending, time.Now().Add(-age).UnixMilli())
	}
	fresh := claimedAt(5 * time.Second)
	if err := os.Rename(pending, fresh); err != nil {
		t.Fatal(err)
	}
	if got, _ := sentinel.Claim(root); got.Len() != 0 {
		t.Fatalf("a claim held for seconds was taken from its holder: %+v", got.Wakes)
	}

	stale := claimedAt(10 * time.Minute)
	if err := os.Rename(fresh, stale); err != nil {
		t.Fatal(err)
	}
	got, _ := sentinel.Claim(root)
	if got.Len() != 1 || got.Wakes[0].TaskID != w.TaskID {
		t.Fatalf("the abandoned claim was not recovered: %+v", got.Wakes)
	}
	_ = got.Commit()
}

func TestClaim_BothKindsTravelTogether(t *testing.T) {
	root := projectRoot(t.TempDir(), "proj")
	w := currentWake(t, root)
	if err := sentinel.RecordForumWake(root, sentinel.ForumWake{Session: "s1", File: "/a.html", Count: 1}); err != nil {
		t.Fatal(err)
	}

	claim, _ := sentinel.Claim(root)
	// The forum wake has no unread inbox entry: it is judged stale on its own,
	// the task wake is still news.
	if len(claim.Wakes) != 1 || claim.Wakes[0].TaskID != w.TaskID {
		t.Fatalf("claim = %+v", claim)
	}
	if err := claim.Release(); err != nil {
		t.Fatal(err)
	}
	if files := wakeFiles(t, root); len(files) != 1 {
		t.Errorf("files = %v", files)
	}
}
