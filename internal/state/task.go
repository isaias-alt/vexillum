// Package state models a task (mission or scout) as a struct serializable
// to JSON, and reads/writes it to ~/.vexillum/tasks/. No workers yet: tasks
// are created and inspected by hand. This is the base for restart-proofing
// in later layers, where each task's state on disk is what a restarted
// vexillum reconciles against.
package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// SchemaVersion is the current version of the Task JSON schema. Bump it
// when Task's shape changes in a way that breaks reading older state
// files.
const SchemaVersion = 1

// Kind distinguishes a mission (delivers a PR) from a scout (delivers a
// report).
type Kind string

const (
	KindMission Kind = "mission"
	KindScout   Kind = "scout"
)

// Status is a task's lifecycle state. Capa 2 only ever creates tasks as
// StatusPending; later layers (workers) transition them.
type Status string

const (
	StatusPending Status = "pending"
)

// Task is a mission or scout, serialized to JSON in ~/.vexillum/tasks/.
type Task struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"id"`
	Kind          Kind      `json:"kind"`
	Prompt        string    `json:"prompt"`
	Status        Status    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// New creates a Task of the given kind with a fresh unique ID, in
// StatusPending.
func New(kind Kind, prompt string) (Task, error) {
	id, err := newID()
	if err != nil {
		return Task{}, fmt.Errorf("generating task id: %w", err)
	}
	now := time.Now().UTC()
	return Task{
		SchemaVersion: SchemaVersion,
		ID:            id,
		Kind:          kind,
		Prompt:        prompt,
		Status:        StatusPending,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func tasksDir(vexillumHome string) string {
	return filepath.Join(vexillumHome, "tasks")
}

func taskPath(vexillumHome, id string) string {
	return filepath.Join(tasksDir(vexillumHome), id+".json")
}

// Save persists t to ~/.vexillum/tasks/<id>.json atomically: it writes to a
// temp file in the same directory and renames it into place, so a reader
// never observes a partially written file.
func Save(vexillumHome string, t Task) error {
	dir := tasksDir(vexillumHome)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating tasks directory: %w", err)
	}

	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding task %s: %w", t.ID, err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, t.ID+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file for task %s: %w", t.ID, err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing task %s: %w", t.ID, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing temp file for task %s: %w", t.ID, err)
	}
	if err := os.Rename(tmpPath, taskPath(vexillumHome, t.ID)); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("committing task %s: %w", t.ID, err)
	}
	return nil
}

// Load reads and decodes the task with the given id. A corrupt or
// incomplete file, or one with an unsupported schema version, produces a
// clear error instead of a panic or garbage data.
func Load(vexillumHome, id string) (Task, error) {
	path := taskPath(vexillumHome, id)
	data, err := os.ReadFile(path)
	if err != nil {
		return Task{}, fmt.Errorf("reading task %s: %w", id, err)
	}
	return decodeTask(path, data)
}

func decodeTask(path string, data []byte) (Task, error) {
	var t Task
	if err := json.Unmarshal(data, &t); err != nil {
		return Task{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	if t.ID == "" || t.Kind == "" {
		return Task{}, fmt.Errorf("%s: incomplete task (missing id or kind)", path)
	}
	if t.SchemaVersion != SchemaVersion {
		return Task{}, fmt.Errorf("%s: unsupported schema version %d (expected %d)", path, t.SchemaVersion, SchemaVersion)
	}
	return t, nil
}

// List returns every task found in ~/.vexillum/tasks/, sorted by ID. An
// empty or missing tasks directory yields an empty list, not an error. If
// any task file is corrupt, List fails with an error naming that file
// rather than silently skipping it or returning a partial list.
func List(vexillumHome string) ([]Task, error) {
	dir := tasksDir(vexillumHome)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing tasks directory: %w", err)
	}

	var tasks []Task
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		t, err := decodeTask(path, data)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}

	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return tasks, nil
}
