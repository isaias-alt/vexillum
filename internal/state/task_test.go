package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
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
			if !reflect.DeepEqual(got, original) {
				t.Errorf("round-trip mismatch:\n got:  %+v\n want: %+v", got, original)
			}
		})
	}
}

// A blocked task's Decision - the structured question, options, and (once
// answered) the answer - round-trips through Save/Load exactly, the same
// restart-proof guarantee every other Task field already has. This is the
// durable-decision feature's core guarantee: the question must survive
// being read back from disk, not just an in-memory copy.
func TestSaveLoad_RoundTripPreservesDecision(t *testing.T) {
	home := t.TempDir()
	original, err := New(KindMission, "pick a database")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	original.Status = StatusBlocked
	original.Decision = &Decision{
		Question:   "Which database should this use?",
		Options:    []string{"Postgres", "ClickHouse"},
		AskedAt:    time.Now().UTC().Truncate(time.Second),
		Answer:     "Postgres",
		AnsweredAt: time.Now().UTC().Truncate(time.Second),
	}

	if err := Save(home, original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(home, original.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Decision == nil {
		t.Fatal("expected the decision to survive the round trip, got nil")
	}
	if !reflect.DeepEqual(got.Decision, original.Decision) {
		t.Errorf("decision round-trip mismatch:\n got:  %+v\n want: %+v", got.Decision, original.Decision)
	}
}

// A task that never blocked round-trips with a nil Decision - no
// structure is invented for a task that never had a question to record.
func TestSaveLoad_RoundTripNilDecisionWhenNeverBlocked(t *testing.T) {
	home := t.TempDir()
	original, err := New(KindMission, "do a thing")
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
	if got.Decision != nil {
		t.Errorf("expected a nil decision, got %+v", got.Decision)
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

	tasks, _, err := List(home)
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

	tasks, _, err := List(home)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected no tasks, got %d", len(tasks))
	}
}

// L2-08: List skips an unreadable task file instead of failing the whole
// listing, hands back the healthy tasks, and names every skipped file with
// the reason, so no task is hidden by a neighbour it shares a directory with.
func TestList_SkipsUnreadableFilesAndReportsThem(t *testing.T) {
	home := t.TempDir()
	good, err := New(KindMission, "a healthy task")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := Save(home, good); err != nil {
		t.Fatalf("Save: %v", err)
	}
	writeRawTask(t, home, "corrupt", []byte("not json at all"))
	future := `{"schema_version":3,"id":"future","kind":"mission","status":"pending","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`
	writeRawTask(t, home, "future", []byte(future))

	tasks, skipped, err := List(home)
	if err != nil {
		t.Fatalf("List must not fail on an unreadable task file: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != good.ID {
		t.Fatalf("expected only the healthy task, got %+v", tasks)
	}
	if len(skipped) != 2 {
		t.Fatalf("expected 2 skipped files, got %+v", skipped)
	}
	// os.ReadDir order is by name: corrupt.json, then future.json.
	if !strings.HasSuffix(skipped[0].Path, "corrupt.json") || !strings.Contains(skipped[0].Err.Error(), "corrupt.json") {
		t.Errorf("expected the corrupt file named in the first skip, got %+v", skipped[0])
	}
	if !strings.HasSuffix(skipped[1].Path, "future.json") || !strings.Contains(skipped[1].Err.Error(), "unsupported schema version 3") {
		t.Errorf("expected the schema version named in the second skip, got %+v", skipped[1])
	}
}

// A tasks path that is not a directory is not "no tasks": it is the one
// listing failure List still reports as an error.
func TestList_ErrorsWhenTasksDirIsUnreadable(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "tasks"), []byte("junk"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, _, err := List(home); err == nil {
		t.Fatal("expected List to fail when the tasks path is not a directory")
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

// ValidateID guards every command that accepts a task id as external
// input (a CLI argument) before that id is used to build a filesystem
// path - a real task id, generated by New/newID, must always pass; a
// malformed one, especially anything that could escape the intended
// directory in a filepath.Join, must always be rejected with a clear
// error.
func TestValidateID(t *testing.T) {
	valid, err := New(KindMission, "a real task")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cases := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"a real generated id", valid.ID, false},
		{"path traversal", "../../etc/passwd", true},
		{"path separator", "abc/def0123456789", true},
		{"absolute path", "/etc/passwd", true},
		{"empty", "", true},
		{"too short", "deadbeef", true},
		{"too long", "0123456789abcdef00", true},
		{"uppercase hex", "0123456789ABCDEF", true},
		{"non-hex characters", "ghijklmnopqrstuv", true},
		{"whitespace", "0123456789abcde ", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateID(tc.id)
			if tc.wantErr && err == nil {
				t.Errorf("ValidateID(%q): expected an error, got nil", tc.id)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("ValidateID(%q): expected no error, got: %v", tc.id, err)
			}
		})
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

// rawTaskFile reads a task's file as a generic JSON object.
func rawTaskFile(t *testing.T, home, id string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "tasks", id+".json"))
	if err != nil {
		t.Fatalf("reading task file: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("task file is not a JSON object: %v", err)
	}
	return doc
}

// addRawField injects a top-level key into a task's file, the way a newer
// build whose Task has more fields would have written it.
func addRawField(t *testing.T, home, id, key, rawValue string) {
	t.Helper()
	doc := rawTaskFile(t, home, id)
	doc[key] = json.RawMessage(rawValue)
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encoding task file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "tasks", id+".json"), data, 0o644); err != nil {
		t.Fatalf("writing task file: %v", err)
	}
}

// A task file carrying a key this build's Task does not declare keeps it
// through a normal load, change, save cycle - the incident where an old
// sentinel rewrote every task and erased camp_base and amendments.
func TestSave_PreservesUnknownFields(t *testing.T) {
	home := t.TempDir()
	task, err := New(KindMission, "ship it")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := Save(home, task); err != nil {
		t.Fatalf("Save: %v", err)
	}
	addRawField(t, home, task.ID, "from_the_future", `{"nested":[1,2,{"a":"b"}],"flag":true}`)
	addRawField(t, home, task.ID, "another_one", `"plain string"`)

	loaded, err := Load(home, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	loaded.Status = StatusRunning
	loaded.CampBase = "main"
	loaded.AddAmendment(AmendmentSourcePrompt, "also do X", time.Now())
	if err := Save(home, loaded); err != nil {
		t.Fatalf("Save after update: %v", err)
	}

	doc := rawTaskFile(t, home, task.ID)
	var future struct {
		Nested []json.RawMessage `json:"nested"`
		Flag   bool              `json:"flag"`
	}
	if err := json.Unmarshal(doc["from_the_future"], &future); err != nil {
		t.Fatalf("unknown field did not survive intact: %v (doc: %v)", err, doc)
	}
	if len(future.Nested) != 3 || !future.Flag {
		t.Errorf("unknown field changed: %s", doc["from_the_future"])
	}
	if string(doc["another_one"]) != `"plain string"` {
		t.Errorf("another_one = %s, want the original string", doc["another_one"])
	}

	// The known-field updates still landed.
	got, err := Load(home, task.ID)
	if err != nil {
		t.Fatalf("Load after update: %v", err)
	}
	if got.Status != StatusRunning || got.CampBase != "main" || len(got.Amendments) != 1 {
		t.Errorf("known-field update lost: status=%s camp_base=%q amendments=%d", got.Status, got.CampBase, len(got.Amendments))
	}
}

// Preserving unknown keys never keeps a key the task owns: a field the new
// state leaves empty is dropped, not resurrected from the old file.
func TestSave_StillClearsOwnedFields(t *testing.T) {
	home := t.TempDir()
	task, err := New(KindMission, "ship it")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	task.CampBase = "main"
	task.Decision = &Decision{Question: "which?", AskedAt: time.Now().UTC()}
	if err := Save(home, task); err != nil {
		t.Fatalf("Save: %v", err)
	}
	addRawField(t, home, task.ID, "extra", `1`)

	task.CampBase = ""
	task.Decision = nil
	if err := Save(home, task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	doc := rawTaskFile(t, home, task.ID)
	for _, key := range []string{"camp_base", "decision"} {
		if _, ok := doc[key]; ok {
			t.Errorf("%s was kept after being cleared: %s", key, doc[key])
		}
	}
	if string(doc["extra"]) != "1" {
		t.Errorf("extra = %s, want 1", doc["extra"])
	}
}

// Every top-level key Task declares is recognized as owned, so none of them is
// ever carried over from an old file as if it were unknown.
func TestOwnedKeys_CoversEveryTaskField(t *testing.T) {
	task, err := New(KindMission, "p")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Populate every field so omitempty drops none of them.
	one := 1
	now := time.Now().UTC()
	task.CampSlot, task.CampPath, task.CampBranch, task.CampBase = 1, "p", "b", "main"
	task.ExitCode, task.Output = &one, "o"
	task.HerdrWorkspaceID, task.HerdrTabID, task.HerdrPaneID, task.HerdrAgentName = "w", "t", "p", "a"
	task.AgentNotFoundSince, task.IdleUnconfirmedSince, task.PromptSubmittedAt = now, now, now
	task.Redispatches, task.Model, task.Effort = 1, "m", "e"
	task.Decision = &Decision{Question: "q"}
	task.Amendments = []Amendment{{Text: "t"}}
	task.LastPushedSHA = "abc"
	task.Harness = "claude"
	task.HarnessMajor, task.HarnessVersion = 2, "2.0.32"
	task.HarnessSession = "ses_abc"

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for key := range doc {
		if !ownedKeys[key] {
			t.Errorf("key %q is marshaled by Task but not in ownedKeys", key)
		}
	}
	if len(doc) != len(ownedKeys) {
		t.Errorf("populated task marshals %d keys, ownedKeys has %d", len(doc), len(ownedKeys))
	}
}

// An existing file that is not a JSON object has nothing to preserve and is
// replaced, as before.
func TestSave_ReplacesUnparsableExistingFile(t *testing.T) {
	home := t.TempDir()
	task, err := New(KindScout, "look around")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	dir := filepath.Join(home, "tasks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, task.ID+".json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(home, task); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := Load(home, task.ID); err != nil {
		t.Errorf("Load after replacing a corrupt file: %v", err)
	}
}

// Version 4 is just past the legacy range: a newer build wrote it, so it is
// refused even though the legacy numbers load.
func TestLoad_RefusesVersionJustPastLegacy(t *testing.T) {
	home := t.TempDir()
	writeRawTask(t, home, "deadbeef", []byte(`{"schema_version":4,"id":"deadbeef","kind":"mission","status":"pending","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`))
	if _, err := Load(home, "deadbeef"); err == nil {
		t.Fatal("expected an error loading schema_version 4")
	}
}

// Update saves what fn changed, from the record on disk at that moment.
func TestUpdate_SavesWhatTheCallbackChanged(t *testing.T) {
	root := t.TempDir()
	task, err := New(KindMission, "p")
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(root, task); err != nil {
		t.Fatal(err)
	}

	saved, err := Update(root, task.ID, func(cur *Task) (bool, error) {
		cur.Status = StatusDone
		return true, nil
	})
	if err != nil || !saved {
		t.Fatalf("Update: saved=%v err=%v", saved, err)
	}
	got, err := Load(root, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusDone {
		t.Errorf("expected done, got %s", got.Status)
	}
}

// A callback that reports false changes nothing, even if it edited its copy.
func TestUpdate_LeavesTheRecordAloneWhenTheCallbackDeclines(t *testing.T) {
	root := t.TempDir()
	task, err := New(KindMission, "p")
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(root, task); err != nil {
		t.Fatal(err)
	}

	saved, err := Update(root, task.ID, func(cur *Task) (bool, error) {
		cur.Status = StatusFailed
		return false, nil
	})
	if err != nil || saved {
		t.Fatalf("Update: saved=%v err=%v", saved, err)
	}
	got, err := Load(root, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPending {
		t.Errorf("expected the record untouched, got %s", got.Status)
	}
}

// A missing task is reported as not existing and never reaches the callback;
// the lock file Update takes is not mistaken for a task.
func TestUpdate_MissingTask(t *testing.T) {
	root := t.TempDir()
	called := false
	_, err := Update(root, "0123456789abcdef", func(*Task) (bool, error) {
		called = true
		return true, nil
	})
	if !errors.Is(err, os.ErrNotExist) || called {
		t.Fatalf("expected a not-exist error without calling fn, got err=%v called=%v", err, called)
	}
	tasks, skipped, err := List(root)
	if err != nil || len(tasks) != 0 || len(skipped) != 0 {
		t.Errorf("expected the lock file to be invisible to List, got %v %v %v", tasks, skipped, err)
	}
}

// Concurrent read-modify-writes must not lose updates: each Update sees the
// result of the one before it.
func TestUpdate_SerializesConcurrentCallers(t *testing.T) {
	root := t.TempDir()
	task, err := New(KindMission, "p")
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(root, task); err != nil {
		t.Fatal(err)
	}

	const workers = 16
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Update(root, task.ID, func(cur *Task) (bool, error) {
				cur.Redispatches++
				return true, nil
			})
			if err != nil {
				t.Errorf("Update: %v", err)
			}
		}()
	}
	wg.Wait()

	got, err := Load(root, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Redispatches != workers {
		t.Errorf("expected %d increments, got %d", workers, got.Redispatches)
	}
}

// A task file written before Harness existed (schema version 0) still loads,
// and its empty Harness means claude.
func TestLoad_TaskFileWithoutHarness(t *testing.T) {
	home := t.TempDir()
	old := `{"schema_version":0,"id":"deadbeef","kind":"mission","prompt":"p","status":"running","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`
	writeRawTask(t, home, "deadbeef", []byte(old))

	got, err := Load(home, "deadbeef")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Harness != "" {
		t.Errorf("Harness = %q, want empty for a task file without the field", got.Harness)
	}
	if got.SchemaVersion != 0 {
		t.Errorf("SchemaVersion = %d, want the file's own 0 preserved", got.SchemaVersion)
	}
}

// New writes the current schema version, and a task's Harness survives a
// save/load round trip.
func TestSaveLoad_RoundTripsHarness(t *testing.T) {
	home := t.TempDir()
	task, err := New(KindMission, "harness round trip")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if task.SchemaVersion != SchemaVersion || SchemaVersion != 1 {
		t.Fatalf("SchemaVersion = %d (const %d), want 1", task.SchemaVersion, SchemaVersion)
	}
	task.Harness = "claude"
	if err := Save(home, task); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(home, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Harness != "claude" {
		t.Errorf("Harness = %q, want claude", got.Harness)
	}
}

// A task with no harness omits the key, so claude tasks stay byte-compatible
// with what an older build wrote apart from the schema version.
func TestSave_OmitsEmptyHarness(t *testing.T) {
	home := t.TempDir()
	task, err := New(KindScout, "no harness")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := Save(home, task); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, "tasks", task.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"harness"`) {
		t.Errorf("task file carries a harness key for an empty Harness:\n%s", data)
	}
}

// The first version past the current one is refused, so an older build never
// drives a task shape it does not know.
func TestLoad_RejectsSchemaVersionAfterCurrent(t *testing.T) {
	home := t.TempDir()
	future := fmt.Sprintf(`{"schema_version":%d,"id":"deadbeef","kind":"mission","status":"pending","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`, SchemaVersion+1)
	writeRawTask(t, home, "deadbeef", []byte(future))
	if _, err := Load(home, "deadbeef"); err == nil {
		t.Fatal("expected an error loading a task one schema version ahead of this build")
	}
}

// The session of an OpenCode 2.x soldier survives a save and a load, and a task
// without one writes no key, so every older task file reads back unchanged.
func TestSave_HarnessSessionRoundTripsAndIsOmittedWhenEmpty(t *testing.T) {
	home := t.TempDir()
	task, err := New(KindMission, "p")
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(home, task); err != nil {
		t.Fatal(err)
	}
	if _, ok := rawTaskFile(t, home, task.ID)["harness_session"]; ok {
		t.Error("an empty session was written")
	}
	task.HarnessSession = "ses_eaa2d9d6cffe3U0WWSA8fu7NsB"
	if err := Save(home, task); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(home, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HarnessSession != task.HarnessSession {
		t.Errorf("session = %q, want %q", loaded.HarnessSession, task.HarnessSession)
	}
}
