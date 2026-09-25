package atomicfile_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
)

// assertNoLeftoverTempFiles fails the test if dir contains any file left
// over from WriteJSON's temp-file pattern (filepath.Base(path)+".*.tmp") -
// every failure branch must remove it, never leave it behind.
func assertNoLeftoverTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading dir %s: %v", dir, err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leftover temp file not cleaned up: %s", e.Name())
		}
	}
}

func TestWriteJSON_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	type payload struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}
	if err := atomicfile.WriteJSON(path, payload{Name: "task", N: 3}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	want := "{\n  \"name\": \"task\",\n  \"n\": 3\n}\n"
	if string(data) != want {
		t.Errorf("content = %q, want %q", data, want)
	}

	assertNoLeftoverTempFiles(t, dir)
}

// json.MarshalIndent failing (an unsupported type, before any file is ever
// touched) must not create the target or leave a temp file behind.
func TestWriteJSON_MarshalFailure_LeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	type unmarshalable struct {
		Ch chan int
	}
	err := atomicfile.WriteJSON(path, unmarshalable{Ch: make(chan int)})
	if err == nil {
		t.Fatal("expected an error for an unmarshalable value")
	}
	if !strings.Contains(err.Error(), "encoding") {
		t.Errorf("expected error to mention encoding, got: %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("target file should not exist after a marshal failure, stat err: %v", statErr)
	}
	assertNoLeftoverTempFiles(t, dir)
}

// A pre-existing target file must survive a failed write completely
// unchanged - not truncated, not partially overwritten. WriteJSON never
// opens the target itself (only a temp file it renames into place), so a
// failure before the rename step must leave the target byte-for-byte as it
// was.
func TestWriteJSON_FailureNeverTouchesExistingTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	original := []byte(`{"n": 1}` + "\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("seeding existing target: %v", err)
	}

	type unmarshalable struct {
		Ch chan int
	}
	if err := atomicfile.WriteJSON(path, unmarshalable{Ch: make(chan int)}); err == nil {
		t.Fatal("expected an error for an unmarshalable value")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading target after failed write: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("target content changed after a failed write: got %q, want %q", got, original)
	}
	assertNoLeftoverTempFiles(t, dir)
}

// os.CreateTemp failing (an unwritable parent directory) must surface a
// clear error and never produce the target file.
func TestWriteJSON_CreateTempFailure_UnwritableDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: directory permissions don't block writes")
	}

	parent := t.TempDir()
	dir := filepath.Join(parent, "readonly")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	// Restore write permission before TempDir cleanup removes the tree.
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	path := filepath.Join(dir, "state.json")
	err := atomicfile.WriteJSON(path, map[string]int{"n": 1})
	if err == nil {
		t.Fatal("expected an error when the parent directory isn't writable")
	}
	if !strings.Contains(err.Error(), "creating temp file") {
		t.Errorf("expected error to mention temp file creation, got: %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("target file should not exist, stat err: %v", statErr)
	}
}

// The final os.Rename failing (here: the target path is occupied by a
// non-empty directory, so rename(2) fails deterministically on every
// platform this project ships for) must still clean up the temp file and
// must never disturb whatever already occupies the target path - the write
// and close steps that precede rename share this exact same cleanup
// contract (remove the temp file, wrap and return the error) but aren't
// independently reachable here: forcing write(2) or close(2) itself to
// fail would need unsafe, non-portable fault injection (e.g. shrinking
// RLIMIT_FSIZE, which this sandbox's own process supervision kills the
// test binary for attempting) rather than a real, hermetic failure.
func TestWriteJSON_RenameFailure_CleansUpAndLeavesTargetUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	sentinel := filepath.Join(path, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("original"), 0o644); err != nil {
		t.Fatalf("seeding target dir: %v", err)
	}

	err := atomicfile.WriteJSON(path, map[string]int{"n": 1})
	if err == nil {
		t.Fatal("expected an error when the target path is occupied by a directory")
	}
	if !strings.Contains(err.Error(), "committing") {
		t.Errorf("expected error to mention committing the write, got: %v", err)
	}

	data, readErr := os.ReadFile(sentinel)
	if readErr != nil {
		t.Fatalf("target directory should be untouched, but reading it failed: %v", readErr)
	}
	if string(data) != "original" {
		t.Errorf("target content changed to %q, want unchanged", data)
	}

	assertNoLeftoverTempFiles(t, dir)
}
