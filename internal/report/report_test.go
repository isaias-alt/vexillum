package report_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isaias-alt/vexillum/internal/report"
)

// Path resolves to <project root>/reports/<agent-name>-<task-id>.md.
func TestPath(t *testing.T) {
	got := report.Path("/proj-root", "vx-do-the-thing", "abc123")
	want := filepath.Join("/proj-root", "reports", "vx-do-the-thing-abc123.md")
	if got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

// Two soldiers with different (already-disambiguated) agent names never
// collide on the same report path, even sharing the same project root -
// this is what keeps concurrent soldiers' reports apart when their agent
// names happen to differ.
func TestPath_DistinctAgentNamesNeverCollide(t *testing.T) {
	a := report.Path("/proj-root", "vx-look-into-it", "task-a")
	b := report.Path("/proj-root", "vx-look-into-it-a1b2c3", "task-a")
	if a == b {
		t.Fatalf("expected distinct agent names to produce distinct report paths, both got %q", a)
	}
}

// The real-world scenario this package guards against: a camp's live-agent
// anti-collision check (internal/soldier.herdrAgentName/startAgent) only
// ever looks at agents that are alive in herdr *right now*. Once an old
// camp is released, its agent name is freed for reuse - so two entirely
// unrelated scouts, dispatched far apart in time, can legitimately end up
// with the identical HerdrAgentName (same prompt prefix producing the same
// slug, with no live collision to disambiguate against at start time). The
// task ID is what must keep their report paths apart even then, since it
// is never reused (internal/state.New, crypto/rand) the way an agent name
// can be.
func TestPath_SameAgentNameDifferentTaskIDsNeverCollide(t *testing.T) {
	const sharedAgentName = "vx-contexto-vexillum-es-un-orquestador"

	first := report.Path("/proj-root", sharedAgentName, "task-one")
	second := report.Path("/proj-root", sharedAgentName, "task-two")

	if first == second {
		t.Fatalf("two scouts sharing an agent name produced the same report path %q - the second would silently overwrite the first's report", first)
	}
}

// Exists reflects the real filesystem: false before the file is written,
// true after.
func TestExists(t *testing.T) {
	root := t.TempDir()
	if report.Exists(root, "vx-do-the-thing", "task-1") {
		t.Fatal("expected no report before one is written")
	}

	if err := os.MkdirAll(report.Dir(root), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(report.Path(root, "vx-do-the-thing", "task-1"), []byte("# report\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if !report.Exists(root, "vx-do-the-thing", "task-1") {
		t.Fatal("expected the report to exist once written")
	}
}

// Exists is keyed on the task ID too: a different task sharing the same
// agent name must not be reported as already having a report.
func TestExists_DoesNotLeakAcrossTaskIDsSharingAnAgentName(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(report.Dir(root), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(report.Path(root, "vx-shared-name", "task-1"), []byte("# report\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if report.Exists(root, "vx-shared-name", "task-2") {
		t.Fatal("expected task-2's report to not exist just because task-1's, sharing the same agent name, does")
	}
}

// Remove deletes an existing report.
func TestRemove_DeletesExistingReport(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(report.Dir(root), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := report.Path(root, "vx-do-the-thing", "task-1")
	if err := os.WriteFile(path, []byte("# report\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := report.Remove(root, "vx-do-the-thing", "task-1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected the report file to be gone, stat error: %v", err)
	}
}

// Remove on a report that was never written is not an error - most agent
// names (every mission, any scout still in flight) never have one.
func TestRemove_MissingReportIsFine(t *testing.T) {
	root := t.TempDir()
	if err := report.Remove(root, "vx-never-wrote-one", "task-1"); err != nil {
		t.Errorf("expected no error removing a nonexistent report, got: %v", err)
	}
}
