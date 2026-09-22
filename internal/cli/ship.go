package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/state"
)

const shipUsage = `Ship a finished mission through the no-mistakes validation gate, opening
a real PR.

Usage:
  vexillum ship <task-id>

Requires the "no-mistakes" binary installed ('vexillum doctor' reports
whether it is). If this project hasn't been gated yet, ship runs
'no-mistakes init' itself the first time - no separate setup step for
the general to remember; see docs/no-mistakes.md. Then it pushes the
mission's camp branch to the "no-mistakes" remote, deterministically -
no soldier or agent judgment decides whether or when this push happens,
since it is the one vexillum action with a real, irreversible effect
outside the machine.

no-mistakes then runs its own review/test/lint/docs pipeline in an
isolated worktree and opens the PR itself once every check is green.
Track that pipeline with 'no-mistakes axi status' or the 'no-mistakes'
TUI - vexillum does not supervise it.
`

// Ship runs the "vexillum ship" command.
func Ship(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(shipUsage)
		return 0
	}
	if len(args) == 0 {
		fmt.Print(shipUsage)
		return 1
	}

	projectDir, vexillumHome, err := resolveDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		return 1
	}

	return runShip(projectDir, vexillumHome, args[0], os.Stdout, os.Stderr)
}

func runShip(projectDir, vexillumHome, taskID string, stdout, stderr io.Writer) int {
	task, err := state.Load(vexillumHome, taskID)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: loading task %s: %v\n", taskID, err)
		return 1
	}

	if task.Kind != state.KindMission {
		fmt.Fprintf(stderr, "vexillum: task %s is a scout, not a mission - a scout should never have committed anything to ship\n", taskID)
		return 1
	}
	if task.Status != state.StatusDone {
		fmt.Fprintf(stderr, "vexillum: task %s is %s, not done - only a finished mission can be shipped\n", taskID, task.Status)
		return 1
	}

	if !noMistakesGateConfigured(projectDir) {
		if err := gateProject(projectDir, stdout, stderr); err != nil {
			return 1
		}
	}

	c, err := camp.Resolve(projectDir, vexillumHome, task.CampSlot)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: resolving camp: %v\n", err)
		return 1
	}

	cmd := exec.Command("git", "push", "no-mistakes", c.Branch)
	cmd.Dir = c.Path
	out, pushErr := cmd.CombinedOutput()
	if pushErr != nil {
		fmt.Fprintf(stderr, "vexillum: pushing %s through the no-mistakes gate: %v\n%s\n", c.Branch, pushErr, strings.TrimSpace(string(out)))
		return 1
	}

	fmt.Fprintf(stdout, "pushed %s through the no-mistakes gate.\n", c.Branch)
	fmt.Fprintln(stdout, "track its pipeline with 'no-mistakes axi status' or the 'no-mistakes' TUI - it opens the PR itself once every check passes.")
	return 0
}

// gateProject runs "no-mistakes init" for projectDir the first time
// ship needs the gate and finds it isn't configured yet - lazily, on
// first use, rather than as a "vexillum init" side effect: init stays
// local-only and network-free (PRD v2, "Decisiones de integración de
// AXIs" applies the same reasoning here even though no-mistakes isn't
// technically an AXI), and nothing about the gate exists on disk until a
// mission actually needs to ship. Fails clearly if "no-mistakes" isn't
// installed at all, rather than a confusing exec error.
func gateProject(projectDir string, stdout, stderr io.Writer) error {
	if _, err := exec.LookPath("no-mistakes"); err != nil {
		fmt.Fprintln(stderr, "vexillum: 'no-mistakes' is not installed - see docs/no-mistakes.md, or use 'vexillum land' for a local fast-forward instead")
		return err
	}

	fmt.Fprintln(stdout, "this project isn't gated yet - running 'no-mistakes init'...")
	cmd := exec.Command("no-mistakes", "init")
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: 'no-mistakes init' failed: %v\n%s\n", err, strings.TrimSpace(string(out)))
		return err
	}
	fmt.Fprintln(stdout, "gate initialized.")
	return nil
}
