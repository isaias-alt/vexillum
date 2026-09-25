package pause_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/pause"
)

// Path resolves to <project root>/pauses/<agent-name>.md.
func TestPath(t *testing.T) {
	got := pause.Path("/proj-root", "vx-do-the-thing")
	want := filepath.Join("/proj-root", "pauses", "vx-do-the-thing.md")
	if got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func writePause(t *testing.T, root, agentName, content string) {
	t.Helper()
	if err := os.MkdirAll(pause.Dir(root), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(pause.Path(root, agentName), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// Exists reflects the real filesystem, regardless of age.
func TestExists(t *testing.T) {
	root := t.TempDir()
	if pause.Exists(root, "vx-do-the-thing") {
		t.Fatal("expected no pause before one is written")
	}
	writePause(t, root, "vx-do-the-thing", "paused: waiting on my e2e run\n")
	if !pause.Exists(root, "vx-do-the-thing") {
		t.Fatal("expected the pause to exist once written")
	}
}

// Active is false when no pause file was ever written.
func TestActive_FalseWhenNeverWritten(t *testing.T) {
	root := t.TempDir()
	_, active, err := pause.Active(root, "vx-do-the-thing", time.Now())
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if active {
		t.Fatal("expected no active pause before one is written")
	}
}

// A freshly written pause file is active and its reason/until lines are
// parsed.
func TestActive_ParsesFreshPause(t *testing.T) {
	root := t.TempDir()
	writePause(t, root, "vx-do-the-thing", "paused: waiting on my own e2e validation run\nuntil: the run finishes\n")

	p, active, err := pause.Active(root, "vx-do-the-thing", time.Now())
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if !active {
		t.Fatal("expected a freshly written pause to be active")
	}
	if p.Reason != "waiting on my own e2e validation run" {
		t.Errorf("expected the declared reason to be parsed, got %q", p.Reason)
	}
	if p.Until != "the run finishes" {
		t.Errorf("expected the declared until to be parsed, got %q", p.Until)
	}
}

// A pause file's mere presence still counts, even without the "paused:"
// verb - the file itself is the durable signal, not its exact prose (see
// package doc).
func TestActive_LenientAboutFormat(t *testing.T) {
	root := t.TempDir()
	writePause(t, root, "vx-do-the-thing", "just waiting on something, no exact format here\n")

	_, active, err := pause.Active(root, "vx-do-the-thing", time.Now())
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if !active {
		t.Fatal("expected the pause file's mere presence to count as active")
	}
}

// A pause file older than ValidityWindow is no longer trusted as active -
// internal/sentinel's ambiguous-idle handling takes over instead of
// trusting a stale declaration forever.
func TestActive_FalseWhenStale(t *testing.T) {
	root := t.TempDir()
	writePause(t, root, "vx-do-the-thing", "paused: waiting on my e2e run\n")

	path := pause.Path(root, "vx-do-the-thing")
	old := time.Now().Add(-pause.ValidityWindow - time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	_, active, err := pause.Active(root, "vx-do-the-thing", time.Now())
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if active {
		t.Fatal("expected a stale pause to no longer be active")
	}
}

// A pause just inside ValidityWindow is still active.
func TestActive_TrueJustInsideValidityWindow(t *testing.T) {
	root := t.TempDir()
	writePause(t, root, "vx-do-the-thing", "paused: waiting on my e2e run\n")

	path := pause.Path(root, "vx-do-the-thing")
	recent := time.Now().Add(-pause.ValidityWindow + time.Minute)
	if err := os.Chtimes(path, recent, recent); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	_, active, err := pause.Active(root, "vx-do-the-thing", time.Now())
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if !active {
		t.Fatal("expected a pause just inside the validity window to still be active")
	}
}

// Remove deletes an existing pause file.
func TestRemove_DeletesExistingPause(t *testing.T) {
	root := t.TempDir()
	writePause(t, root, "vx-do-the-thing", "paused: waiting\n")

	if err := pause.Remove(root, "vx-do-the-thing"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if pause.Exists(root, "vx-do-the-thing") {
		t.Error("expected the pause file to be gone")
	}
}

// Remove on a pause that was never written is not an error - most agent
// names never have one.
func TestRemove_MissingPauseIsFine(t *testing.T) {
	root := t.TempDir()
	if err := pause.Remove(root, "vx-never-paused"); err != nil {
		t.Errorf("expected no error removing a nonexistent pause, got: %v", err)
	}
}
