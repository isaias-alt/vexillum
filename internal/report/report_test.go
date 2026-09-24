package report_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isaias-alt/vexillum/internal/report"
)

// Path resolves to <project root>/reports/<agent-name>.md.
func TestPath(t *testing.T) {
	got := report.Path("/proj-root", "vx-do-the-thing")
	want := filepath.Join("/proj-root", "reports", "vx-do-the-thing.md")
	if got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

// Two soldiers with different (already-disambiguated) agent names never
// collide on the same report path, even sharing the same project root -
// this is what actually keeps concurrent soldiers' reports apart, since
// they all write into one flat reports/ directory rather than one
// directory per task.
func TestPath_DistinctAgentNamesNeverCollide(t *testing.T) {
	a := report.Path("/proj-root", "vx-look-into-it")
	b := report.Path("/proj-root", "vx-look-into-it-a1b2c3")
	if a == b {
		t.Fatalf("expected distinct agent names to produce distinct report paths, both got %q", a)
	}
}

// Exists reflects the real filesystem: false before the file is written,
// true after.
func TestExists(t *testing.T) {
	root := t.TempDir()
	if report.Exists(root, "vx-do-the-thing") {
		t.Fatal("expected no report before one is written")
	}

	if err := os.MkdirAll(report.Dir(root), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(report.Path(root, "vx-do-the-thing"), []byte("# report\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if !report.Exists(root, "vx-do-the-thing") {
		t.Fatal("expected the report to exist once written")
	}
}

// Remove deletes an existing report.
func TestRemove_DeletesExistingReport(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(report.Dir(root), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := report.Path(root, "vx-do-the-thing")
	if err := os.WriteFile(path, []byte("# report\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := report.Remove(root, "vx-do-the-thing"); err != nil {
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
	if err := report.Remove(root, "vx-never-wrote-one"); err != nil {
		t.Errorf("expected no error removing a nonexistent report, got: %v", err)
	}
}
