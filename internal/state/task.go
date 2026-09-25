// Package state models a task (mission or scout) as a struct serializable
// to JSON, and reads/writes it to <project root>/tasks/ - the caller
// resolves that project root (see internal/project) before calling in;
// this package only ever sees the root it's handed, not vexillumHome
// directly. This is the base for restart-proofing in later layers, where
// each task's state on disk is what a restarted vexillum reconciles
// against.
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

	"github.com/isaias-alt/vexillum/internal/atomicfile"
)

// SchemaVersion is the current version of the Task JSON schema. Bump it
// when Task's shape changes in a way that breaks reading older state
// files.
//
// v2 (Capa 3) added the camp assignment, exit code and output fields, and
// the running/done/failed statuses a soldier run transitions through.
// v3 (Capa 4) added the herdr workspace/tab/pane/agent identifiers and
// the blocked status, for soldiers that run in a real herdr pane instead
// of headless.
const SchemaVersion = 3

// Kind distinguishes a mission (delivers code changes, landed locally via
// vexillum land - v1 never opens a real PR) from a scout (delivers a
// report).
type Kind string

const (
	KindMission Kind = "mission"
	KindScout   Kind = "scout"
)

// Status is a task's lifecycle state.
type Status string

const (
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	StatusBlocked Status = "blocked"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
	// StatusInterrupted marks a task whose herdr agent genuinely
	// disappeared (pane closed, herdr restarted and lost session state)
	// while it was Running - internal/sentinel.Tick sets this once it's
	// confirmed the agent is gone, not just unreachable for a moment.
	// Distinct from StatusFailed: the soldier didn't necessarily do
	// anything wrong, vexillum just lost the ability to observe it -
	// any work it had already committed is still sitting in its camp.
	StatusInterrupted Status = "interrupted"
	// StatusShipped marks a mission pushed through the no-mistakes gate
	// (vexillum ship). From here the PR is the source of truth, not this
	// camp: land merges the real PR instead of fast-forwarding a local
	// branch that no-mistakes may have moved (auto-fix commits, or a
	// rebase before merging), and release can no longer rely on a plain
	// ancestor check once GitHub squashes or rebases the merge.
	StatusShipped Status = "shipped"
	// StatusUnconfirmed marks a Running task whose herdr agent went idle
	// (live status "idle"/"done") without a strong completion signal -
	// internal/report's file for a scout, internal/camp.HasNewCommits for
	// a mission - and without a currently valid declared pause
	// (internal/pause). internal/sentinel.Tick sets this once that's held
	// true for its own confirm window, mirroring exactly how
	// StatusInterrupted only fires once a genuine agent disappearance is
	// confirmed rather than trusting a single observation.
	//
	// Distinct from StatusDone: idle alone was never proof of anything,
	// only proof that the turn stopped responding - see internal/pause's
	// package doc. Distinct from StatusFailed: nothing here says the
	// soldier did anything wrong, just that vexillum can't yet tell
	// success from a soldier that's quietly stuck. Distinct from
	// StatusInterrupted: the herdr agent is still there and reachable,
	// just unexplained - a human needs to look (at the camp, or the pane
	// itself), the same as an Interrupted task, but there's nothing left
	// for the sentinel to keep polling for on its own once this fires
	// (matches Interrupted's own resting-state precedent).
	StatusUnconfirmed Status = "unconfirmed"
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

	// Camp assignment, set once a soldier run starts (Capa 3). Empty
	// until then.
	CampSlot   int    `json:"camp_slot,omitempty"`
	CampPath   string `json:"camp_path,omitempty"`
	CampBranch string `json:"camp_branch,omitempty"`

	// CampBase is the branch CampBranch was forked from (camp.Camp.Base
	// at the moment this task's camp was acquired) - internal/sentinel
	// uses it with camp.HasNewCommits to ask "did this mission actually
	// produce a commit" without needing the project's own checkout
	// directory, which the sentinel never has (see camp.Camp.Base's own
	// doc comment). Purely additive, same reasoning as AgentNotFoundSince
	// below: an older task file without it just decodes to "", which
	// already means exactly "no base recorded, can't verify" to the
	// completion check - no SchemaVersion bump needed.
	CampBase string `json:"camp_base,omitempty"`

	// Soldier run result. ExitCode applies only to a headless run
	// (internal/soldier.Run); a run in a real herdr pane
	// (internal/soldier.RunInHerdr) has no process exit code, since
	// it's an interactive agent session, not a one-shot command.
	// Output holds the captured transcript either way.
	ExitCode *int   `json:"exit_code,omitempty"`
	Output   string `json:"output,omitempty"`

	// herdr pane assignment, set once a soldier runs in a real herdr
	// pane (Capa 4, internal/soldier.RunInHerdr). Empty for a headless
	// run.
	HerdrWorkspaceID string `json:"herdr_workspace_id,omitempty"`
	HerdrTabID       string `json:"herdr_tab_id,omitempty"`
	HerdrPaneID      string `json:"herdr_pane_id,omitempty"`
	HerdrAgentName   string `json:"herdr_agent_name,omitempty"`

	// AgentNotFoundSince marks when internal/sentinel.Tick first observed
	// this task's herdr agent as gone (herdr's specific "agent_not_found",
	// not a transient read error) - zero means never observed missing.
	// Purely additive: an older task file without it just decodes to the
	// zero value, which already means exactly "never observed missing",
	// so this does not need a SchemaVersion bump.
	AgentNotFoundSince time.Time `json:"agent_not_found_since,omitzero"`

	// IdleUnconfirmedSince marks when internal/sentinel.Tick first
	// observed this task's live herdr status as idle/done with neither a
	// strong completion signal nor a currently valid declared pause
	// (internal/pause) - zero means never. Same purely-additive reasoning
	// as AgentNotFoundSince: an older task file without it decodes to the
	// zero value, which already means "never observed ambiguous".
	IdleUnconfirmedSince time.Time `json:"idle_unconfirmed_since,omitzero"`

	// Redispatches counts how many times this task has been re-dispatched
	// after going Interrupted (PRD v2, A.2) - zero means never. Re-dispatch
	// reuses this same Task (same ID), so a re-dispatched task's identity
	// stays stable rather than forking into a new one - same reasoning as
	// AgentNotFoundSince: purely additive, no SchemaVersion bump needed.
	Redispatches int `json:"redispatches,omitempty"`

	// Model and Effort are the soldier's --model/--effort choice (ADR-05:
	// the commander picks these by judgment from its AGENTS.md rules, this
	// is only where the choice is recorded). Empty means the flag was
	// never passed to claude, which then falls back to its own default.
	// A re-dispatch reuses whatever this task already carries. Purely
	// additive, no SchemaVersion bump needed.
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
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

func tasksDir(projectRoot string) string {
	return filepath.Join(projectRoot, "tasks")
}

func taskPath(projectRoot, id string) string {
	return filepath.Join(tasksDir(projectRoot), id+".json")
}

// Save persists t to <project root>/tasks/<id>.json atomically: it writes
// to a temp file in the same directory and renames it into place, so a
// reader never observes a partially written file.
func Save(projectRoot string, t Task) error {
	dir := tasksDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating tasks directory: %w", err)
	}
	if err := atomicfile.WriteJSON(taskPath(projectRoot, t.ID), t); err != nil {
		return fmt.Errorf("saving task %s: %w", t.ID, err)
	}
	return nil
}

// Load reads and decodes the task with the given id. A corrupt or
// incomplete file, or one with an unsupported schema version, produces a
// clear error instead of a panic or garbage data.
func Load(projectRoot, id string) (Task, error) {
	path := taskPath(projectRoot, id)
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

// List returns every task found in <project root>/tasks/, sorted by ID.
// An empty or missing tasks directory yields an empty list, not an error.
// If any task file is corrupt, List fails with an error naming that file
// rather than silently skipping it or returning a partial list.
func List(projectRoot string) ([]Task, error) {
	dir := tasksDir(projectRoot)
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
