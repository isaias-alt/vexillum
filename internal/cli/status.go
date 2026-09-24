package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/state"
)

const statusUsage = `Report the current project's fleet of tasks - every mission and scout
vexillum knows about, in whatever state it's in. Read-only.

Usage:
  vexillum status [--json]

Without --json, prints one line per task (most recently updated first).

--json prints a single JSON object instead:

  {
    "schema_version": 3,
    "generated_at": "2026-09-24T12:00:00Z",
    "project_root": "/abs/path/to/this project's ~/.vexillum namespace",
    "tasks": [ ... ]
  }

"tasks" is the exact internal/state.Task schema (v3) vexillum itself
persists to <project root>/tasks/*.json - this command adds nothing to
it, so a consumer (e.g. the /muster plugin) reads the same shape vexillum
itself reasons about, never a separate reinterpretation of it. Grouping,
filtering, and judgment calls (what counts as "needs attention", what's
in the current session, GitHub PR enrichment) are left to that consumer,
not decided here.
`

// statusSnapshot is the --json envelope: state.Task already carries its
// own schema_version per task, but a snapshot as a whole also needs one
// so a consumer can tell at a glance whether the shape it just parsed is
// the one it knows how to read, without having to first find a task to
// check (an empty fleet still reports a schema_version).
type statusSnapshot struct {
	SchemaVersion int          `json:"schema_version"`
	GeneratedAt   time.Time    `json:"generated_at"`
	ProjectRoot   string       `json:"project_root"`
	Tasks         []state.Task `json:"tasks"`
}

// Status runs the "vexillum status" command.
func Status(args []string) int {
	jsonOutput, help, err := parseStatusArgs(args)
	if help {
		fmt.Print(statusUsage)
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		fmt.Fprint(os.Stderr, statusUsage)
		return 1
	}

	projectDir, vexillumHome, err := resolveDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		return 1
	}

	return runStatus(projectDir, vexillumHome, jsonOutput, os.Stdout, os.Stderr)
}

func parseStatusArgs(args []string) (jsonOutput, help bool, err error) {
	for _, a := range args {
		switch a {
		case "-h", "--help":
			return false, true, nil
		case "--json":
			jsonOutput = true
		default:
			return false, false, fmt.Errorf("unknown status flag %q", a)
		}
	}
	return jsonOutput, false, nil
}

func runStatus(projectDir, vexillumHome string, jsonOutput bool, stdout, stderr io.Writer) int {
	projectRoot, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		fmt.Fprintln(stderr, "vexillum:", err)
		return 1
	}

	tasks, err := state.List(projectRoot)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: listing tasks: %v\n", err)
		return 1
	}
	if tasks == nil {
		// A consumer parsing --json shouldn't have to special-case "no
		// tasks yet" as a JSON null instead of an empty array.
		tasks = []state.Task{}
	}
	sortTasksByRecency(tasks)

	if jsonOutput {
		snapshot := statusSnapshot{
			SchemaVersion: state.SchemaVersion,
			GeneratedAt:   time.Now().UTC(),
			ProjectRoot:   projectRoot,
			Tasks:         tasks,
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(snapshot); err != nil {
			fmt.Fprintf(stderr, "vexillum: encoding status: %v\n", err)
			return 1
		}
		return 0
	}

	if len(tasks) == 0 {
		fmt.Fprintln(stdout, "no tasks yet in this project.")
		return 0
	}
	for _, t := range tasks {
		fmt.Fprintf(stdout, "%s  %-7s %-11s %-24s %s\n", t.ID, t.Kind, t.Status, t.CampBranch, truncatePrompt(t.Prompt, 60))
	}
	return 0
}

// sortTasksByRecency orders tasks most-recently-updated first, breaking
// ties by ID so the ordering is deterministic (two tasks saved within the
// same clock tick shouldn't otherwise flap between runs).
func sortTasksByRecency(tasks []state.Task) {
	sort.Slice(tasks, func(i, j int) bool {
		if !tasks[i].UpdatedAt.Equal(tasks[j].UpdatedAt) {
			return tasks[i].UpdatedAt.After(tasks[j].UpdatedAt)
		}
		return tasks[i].ID < tasks[j].ID
	})
}

func truncatePrompt(prompt string, max int) string {
	r := []rune(prompt)
	if len(r) <= max {
		return prompt
	}
	return string(r[:max]) + "..."
}
