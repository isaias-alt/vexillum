package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/state"
)

// statusProject returns a fresh, isolated (projectDir, vexillumHome,
// projectRoot) triple - projectRoot is where state.Save/state.List
// actually read and write for this projectDir, exactly as runStatus
// itself resolves it.
func statusProject(t *testing.T) (projectDir, vexillumHome, projectRoot string) {
	t.Helper()
	projectDir = t.TempDir()
	vexillumHome = t.TempDir()
	root, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		t.Fatalf("resolving project root: %v", err)
	}
	return projectDir, vexillumHome, root
}

func saveTask(t *testing.T, projectRoot string, mutate func(*state.Task)) state.Task {
	t.Helper()
	task, err := state.New(state.KindMission, "do a thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	if mutate != nil {
		mutate(&task)
	}
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	return task
}

// An empty project reports a valid, empty snapshot - never an error, and
// "tasks" is an empty array, not JSON null, so a consumer never has to
// special-case "no tasks yet".
func TestStatus_JSON_EmptyProject(t *testing.T) {
	projectDir, vexillumHome, projectRoot := statusProject(t)

	var out, errOut bytes.Buffer
	code := runStatus(projectDir, vexillumHome, true, &out, &errOut)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstderr: %s", code, errOut.String())
	}

	var snapshot statusSnapshot
	if err := json.Unmarshal(out.Bytes(), &snapshot); err != nil {
		t.Fatalf("parsing JSON output: %v\noutput: %s", err, out.String())
	}
	if snapshot.SchemaVersion != state.SchemaVersion {
		t.Errorf("schema_version = %d, want %d", snapshot.SchemaVersion, state.SchemaVersion)
	}
	if snapshot.ProjectRoot != projectRoot {
		t.Errorf("project_root = %q, want %q", snapshot.ProjectRoot, projectRoot)
	}
	if snapshot.Tasks == nil {
		t.Errorf("tasks = nil, want an empty (non-nil) slice")
	}
	if len(snapshot.Tasks) != 0 {
		t.Errorf("tasks = %v, want empty", snapshot.Tasks)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"tasks": []`)) {
		t.Errorf("expected tasks to serialize as an empty JSON array, got:\n%s", out.String())
	}
}

// Every task in the project shows up in the snapshot, carrying the exact
// internal/state.Task schema untouched - status is single-sourced in Go,
// this command doesn't reinterpret it.
func TestStatus_JSON_ReportsEveryTaskUntouched(t *testing.T) {
	projectDir, vexillumHome, projectRoot := statusProject(t)

	mission := saveTask(t, projectRoot, func(task *state.Task) {
		task.Status = state.StatusRunning
		task.CampBranch = "vexillum/" + task.ID
		task.Model = "sonnet"
		task.Effort = "high"
	})
	scout := saveTask(t, projectRoot, func(task *state.Task) {
		task.Kind = state.KindScout
		task.Status = state.StatusBlocked
	})

	var out, errOut bytes.Buffer
	code := runStatus(projectDir, vexillumHome, true, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstderr: %s", code, errOut.String())
	}

	var snapshot statusSnapshot
	if err := json.Unmarshal(out.Bytes(), &snapshot); err != nil {
		t.Fatalf("parsing JSON output: %v\noutput: %s", err, out.String())
	}
	if len(snapshot.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d: %+v", len(snapshot.Tasks), snapshot.Tasks)
	}

	byID := map[string]state.Task{}
	for _, task := range snapshot.Tasks {
		byID[task.ID] = task
	}
	got, ok := byID[mission.ID]
	if !ok {
		t.Fatalf("mission %s missing from snapshot", mission.ID)
	}
	if got.Status != state.StatusRunning || got.CampBranch != mission.CampBranch || got.Model != "sonnet" || got.Effort != "high" {
		t.Errorf("mission task reported with altered fields: %+v", got)
	}
	if got, ok := byID[scout.ID]; !ok || got.Kind != state.KindScout || got.Status != state.StatusBlocked {
		t.Errorf("scout task not reported faithfully: %+v (ok=%v)", got, ok)
	}
}

// Tasks are ordered most-recently-updated first, so a consumer building a
// digest doesn't have to re-sort - the freshest activity leads.
func TestStatus_JSON_OrderedByRecencyDescending(t *testing.T) {
	projectDir, vexillumHome, projectRoot := statusProject(t)

	base := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	oldest := saveTask(t, projectRoot, func(task *state.Task) { task.UpdatedAt = base })
	middle := saveTask(t, projectRoot, func(task *state.Task) { task.UpdatedAt = base.Add(time.Hour) })
	newest := saveTask(t, projectRoot, func(task *state.Task) { task.UpdatedAt = base.Add(2 * time.Hour) })

	var out, errOut bytes.Buffer
	code := runStatus(projectDir, vexillumHome, true, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstderr: %s", code, errOut.String())
	}

	var snapshot statusSnapshot
	if err := json.Unmarshal(out.Bytes(), &snapshot); err != nil {
		t.Fatalf("parsing JSON output: %v\noutput: %s", err, out.String())
	}
	if len(snapshot.Tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(snapshot.Tasks))
	}
	gotOrder := []string{snapshot.Tasks[0].ID, snapshot.Tasks[1].ID, snapshot.Tasks[2].ID}
	wantOrder := []string{newest.ID, middle.ID, oldest.ID}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Errorf("task order = %v, want %v", gotOrder, wantOrder)
			break
		}
	}
}

// Without --json, an empty project reports plainly rather than printing
// an empty table.
func TestStatus_PlainText_EmptyProject(t *testing.T) {
	projectDir, vexillumHome, _ := statusProject(t)

	var out, errOut bytes.Buffer
	code := runStatus(projectDir, vexillumHome, false, &out, &errOut)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstderr: %s", code, errOut.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("no tasks yet")) {
		t.Errorf("expected an empty-fleet message, got:\n%s", out.String())
	}
}

// Without --json, each task appears as a line naming its id, kind, and
// status - the plain-text form is a human-readable listing, not a digest.
func TestStatus_PlainText_ListsTasks(t *testing.T) {
	projectDir, vexillumHome, projectRoot := statusProject(t)

	task := saveTask(t, projectRoot, func(task *state.Task) {
		task.Status = state.StatusDone
		task.Prompt = "fix the thing"
	})

	var out, errOut bytes.Buffer
	code := runStatus(projectDir, vexillumHome, false, &out, &errOut)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstderr: %s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{task.ID, "mission", "done", "fix the thing"} {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Errorf("expected output to contain %q, got:\n%s", want, got)
		}
	}
}

// Without --json, a blocked task's open question is surfaced on its own
// line - not just the word "blocked" - so it mirrors the durable decision
// vexillum status --json already carries in the task's "decision" field.
func TestStatus_PlainText_ShowsBlockedQuestion(t *testing.T) {
	projectDir, vexillumHome, projectRoot := statusProject(t)

	saveTask(t, projectRoot, func(task *state.Task) {
		task.Status = state.StatusBlocked
		task.Decision = &state.Decision{Question: "Which auth library should I use?"}
	})

	var out, errOut bytes.Buffer
	code := runStatus(projectDir, vexillumHome, false, &out, &errOut)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstderr: %s", code, errOut.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("Which auth library should I use?")) {
		t.Errorf("expected the open question surfaced in plain-text output, got:\n%s", out.String())
	}
}

// A corrupt task file is surfaced as an error, not silently dropped or
// papered over - the same posture state.List itself takes.
func TestStatus_CorruptTaskFileIsAnError(t *testing.T) {
	projectDir, vexillumHome, projectRoot := statusProject(t)
	saveTask(t, projectRoot, nil)

	tasksDir := filepath.Join(projectRoot, "tasks")
	if err := os.WriteFile(filepath.Join(tasksDir, "corrupt.json"), []byte("not json"), 0o644); err != nil {
		t.Fatalf("writing corrupt task file: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runStatus(projectDir, vexillumHome, true, &out, &errOut)

	if code == 0 {
		t.Fatalf("expected non-zero exit for a corrupt task file, got 0")
	}
	if errOut.Len() == 0 {
		t.Errorf("expected an error message on stderr")
	}
}

// parseStatusArgs is where Status's own flag handling lives, kept as a
// pure function (mirroring parseDispatchArgs) so -h and an unknown flag
// can be tested without going through the real os.Stdout/os.Stderr that
// the exported Status wrapper writes to.
func TestParseStatusArgs(t *testing.T) {
	cases := []struct {
		args     []string
		wantJSON bool
		wantHelp bool
		wantErr  bool
	}{
		{args: nil, wantJSON: false, wantHelp: false},
		{args: []string{"--json"}, wantJSON: true},
		{args: []string{"-h"}, wantHelp: true},
		{args: []string{"--help"}, wantHelp: true},
		{args: []string{"--bogus"}, wantErr: true},
	}
	for _, c := range cases {
		gotJSON, gotHelp, err := parseStatusArgs(c.args)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseStatusArgs(%v): expected an error, got none", c.args)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseStatusArgs(%v): unexpected error: %v", c.args, err)
			continue
		}
		if gotJSON != c.wantJSON || gotHelp != c.wantHelp {
			t.Errorf("parseStatusArgs(%v) = (json=%v, help=%v), want (json=%v, help=%v)", c.args, gotJSON, gotHelp, c.wantJSON, c.wantHelp)
		}
	}
}
