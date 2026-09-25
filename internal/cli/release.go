package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/report"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

const releaseUsage = `Release a soldier's camp back to the pool once its work has landed.

Usage:
  vexillum release <task-id> [--force]

Refuses unless the camp is clean and (for a mission) landed. For a scout,
also refuses unless its final report exists at
~/.vexillum/projects/<project>/reports/<agent-name>.md - the report is
the scout's work product, the same way a mission's is its landed commit.
--force skips the report check (never the clean/landed camp check) - an
explicit, logged escape hatch, never silent.

On success, returns the worktree to the pool for reuse and closes the
herdr pane.
`

// Release runs the "vexillum release" command.
func Release(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(releaseUsage)
		return 0
	}
	if len(args) == 0 {
		fmt.Print(releaseUsage)
		return 1
	}

	taskID, force, err := parseReleaseArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		return 1
	}

	projectDir, vexillumHome, err := resolveDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		return 1
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum: cannot determine home directory:", err)
		return 1
	}

	return runRelease(projectDir, vexillumHome, homeDir, taskID, force, herdr.CLI{}, os.Stdout, os.Stderr)
}

func parseReleaseArgs(args []string) (taskID string, force bool, err error) {
	for _, a := range args {
		if a == "--force" {
			force = true
			continue
		}
		if taskID != "" {
			return "", false, fmt.Errorf("unexpected extra argument %q", a)
		}
		taskID = a
	}
	if taskID == "" {
		return "", false, fmt.Errorf("missing task id")
	}
	return taskID, force, nil
}

func runRelease(projectDir, vexillumHome, homeDir, taskID string, force bool, client herdr.Client, stdout, stderr io.Writer) int {
	if err := state.ValidateID(taskID); err != nil {
		fmt.Fprintln(stderr, "vexillum:", err)
		return 1
	}

	projectRoot, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		fmt.Fprintln(stderr, "vexillum:", err)
		return 1
	}

	task, err := state.Load(projectRoot, taskID)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: loading task %s: %v\n", taskID, err)
		return 1
	}

	// A scout's report is its work product, the same way a mission's
	// landed commit is (checked by camp.Release itself, via
	// soldier.ReleaseInHerdr below) - a mission never has one to check
	// (internal/report's package doc), so this only ever applies to a
	// scout. --force is the explicit, logged escape hatch, mirroring
	// firstmate's own teardown gate ("REFUSED: scout task $ID has no
	// report ... use --force after explicit discard approval").
	if task.Kind == state.KindScout && !force && !report.Exists(projectRoot, task.HerdrAgentName, task.ID) {
		fmt.Fprintf(stderr, "vexillum: release refused: scout task %s has no report at %s\n", taskID, report.Path(projectRoot, task.HerdrAgentName, task.ID))
		fmt.Fprintln(stderr, "vexillum: the report is the work product - have the soldier write it, or pass --force to release anyway")
		return 1
	}

	c, err := camp.Resolve(projectDir, vexillumHome, task.CampSlot)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: resolving camp: %v\n", err)
		return 1
	}

	if err := soldier.ReleaseInHerdr(task, c, client, homeDir); err != nil {
		fmt.Fprintf(stderr, "vexillum: release refused: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "released: camp returned to the pool, herdr pane closed.")
	return 0
}
