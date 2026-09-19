package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// L2-01: a new task is saved as a valid JSON file under
// ~/.vexillum/tasks/<id>.json.
func TestSave_CreatesJSONFile(t *testing.T) {
	home := t.TempDir()
	task, err := New(KindMission, "fix the flaky test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := Save(home, task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(home, "tasks", task.ID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected task file at %s: %v", path, err)
	}
	if !json.Valid(data) {
		t.Errorf("task file is not valid JSON: %s", data)
	}
}

// L2-02: round-trip fidelity for both mission and scout tasks.
func TestSaveLoad_RoundTrip(t *testing.T) {
	for _, kind := range []Kind{KindMission, KindScout} {
		t.Run(string(kind), func(t *testing.T) {
			home := t.TempDir()
			original, err := New(kind, "investigate the flaky test")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if err := Save(home, original); err != nil {
				t.Fatalf("Save: %v", err)
			}

			got, err := Load(home, original.ID)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got != original {
				t.Errorf("round-trip mismatch:\n got:  %+v\n want: %+v", got, original)
			}
		})
	}
}

// L2-03: a corrupt (non-JSON) task file produces a clear error, not a
// panic.
func TestLoad_CorruptFile(t *testing.T) {
	home := t.TempDir()
	writeRawTask(t, home, "bad", []byte("{not json"))

	if _, err := Load(home, "bad"); err == nil {
		t.Fatal("expected an error loading a corrupt task file")
	}
}

// L2-04: a JSON file missing required fields is treated as incomplete,
// not garbage data.
func TestLoad_IncompleteFile(t *testing.T) {
	home := t.TempDir()
	writeRawTask(t, home, "incomplete", []byte(`{"schema_version":1,"status":"pending"}`))

	if _, err := Load(home, "incomplete"); err == nil {
		t.Fatal("expected an error loading an incomplete task file")
	}
}

// L2-05: an unsupported schema version is rejected with a clear error
// instead of being silently accepted.
func TestLoad_UnsupportedSchemaVersion(t *testing.T) {
	home := t.TempDir()
	future := `{"schema_version":999,"id":"deadbeef","kind":"mission","status":"pending","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`
	writeRawTask(t, home, "deadbeef", []byte(future))

	_, err := Load(home, "deadbeef")
	if err == nil {
		t.Fatal("expected an error loading a task with an unsupported schema version")
	}
	if !strings.Contains(err.Error(), "999") {
		t.Errorf("expected error to name the offending version, got: %v", err)
	}
}

// L2-06: Save writes atomically - no leftover temp files after a
// successful write.
func TestSave_NoLeftoverTempFiles(t *testing.T) {
	home := t.TempDir()
	task, err := New(KindMission, "atomic write check")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := Save(home, task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(home, "tasks"))
	if err != nil {
		t.Fatalf("reading tasks dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one file in tasks dir, got %d: %v", len(entries), entries)
	}
	if entries[0].Name() != task.ID+".json" {
		t.Errorf("expected only %s.json, found %s", task.ID, entries[0].Name())
	}
}

// L2-07: List returns every saved task, mission and scout mixed, and an
// empty (or missing) tasks directory yields an empty list, not an error.
func TestList_ReturnsAllTasks(t *testing.T) {
	home := t.TempDir()
	mission, err := New(KindMission, "ship the feature")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	scout, err := New(KindScout, "investigate the bug")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := Save(home, mission); err != nil {
		t.Fatalf("Save mission: %v", err)
	}
	if err := Save(home, scout); err != nil {
		t.Fatalf("Save scout: %v", err)
	}

	tasks, err := List(home)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	found := map[string]bool{}
	for _, task := range tasks {
		found[task.ID] = true
	}
	if !found[mission.ID] || !found[scout.ID] {
		t.Errorf("expected both tasks in list, got %+v", tasks)
	}
}

func TestList_EmptyWhenNoTasksDir(t *testing.T) {
	home := t.TempDir()

	tasks, err := List(home)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected no tasks, got %d", len(tasks))
	}
}

// L2-08: List fails clearly, naming the offending file, when one of the
// task files is corrupt - it doesn't skip it silently or return a partial
// list.
func TestList_FailsOnCorruptFile(t *testing.T) {
	home := t.TempDir()
	good, err := New(KindMission, "a healthy task")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := Save(home, good); err != nil {
		t.Fatalf("Save: %v", err)
	}
	writeRawTask(t, home, "corrupt", []byte("not json at all"))

	_, err = List(home)
	if err == nil {
		t.Fatal("expected List to fail when a task file is corrupt")
	}
	if !strings.Contains(err.Error(), "corrupt.json") {
		t.Errorf("expected error to name the corrupt file, got: %v", err)
	}
}

// Two tasks created in quick succession get distinct IDs.
func TestNew_UniqueIDs(t *testing.T) {
	a, err := New(KindMission, "task a")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b, err := New(KindMission, "task b")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.ID == b.ID {
		t.Errorf("expected distinct IDs, got %s twice", a.ID)
	}
}

func writeRawTask(t *testing.T, vexillumHome, id string, data []byte) {
	t.Helper()
	dir := filepath.Join(vexillumHome, "tasks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir tasks dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), data, 0o644); err != nil {
		t.Fatalf("writing raw task file: %v", err)
	}
}
