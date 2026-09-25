package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/pause"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/report"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

const redispatchUsage = `Re-dispatch an interrupted task from its original prompt.

Usage:
  vexillum redispatch <task-id>

Only a task in status "interrupted" can be re-dispatched. This is
re-dispatch, not resumption: the task's dirty camp (working tree and any
commits it never landed) is discarded, a fresh camp is created, and the
mission is relaunched from its original prompt as if freshly dispatched -
none of the dead soldier's partial work or agent session is recovered.
Requires HERDR_WORKSPACE_ID - run this from inside a herdr-managed pane.
`

// Redispatch runs the "vexillum redispatch" command.
func Redispatch(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(redispatchUsage)
		return 0
	}
	if len(args) == 0 {
		fmt.Print(redispatchUsage)
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

	workspaceID := os.Getenv("HERDR_WORKSPACE_ID")
	if workspaceID == "" {
		fmt.Fprintln(os.Stderr, "vexillum: HERDR_WORKSPACE_ID is not set - redispatch must run from inside a herdr-managed pane")
		return 1
	}

	ensureSentinelRunning(vexillumHome, os.Stderr)

	return runRedispatch(projectDir, vexillumHome, homeDir, workspaceID, args[0], herdr.CLI{}, os.Stdout, os.Stderr)
}

func runRedispatch(projectDir, vexillumHome, homeDir, workspaceID, taskID string, client herdr.Client, stdout, stderr io.Writer) int {
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

	if task.Status != state.StatusInterrupted {
		fmt.Fprintf(stderr, "vexillum: task %s is %s, not interrupted - only an interrupted task can be re-dispatched (a done mission is landed with 'vexillum land' and released; a running one is left alone)\n", taskID, task.Status)
		return 1
	}

	if task.CampSlot != 0 {
		c, err := camp.Resolve(projectDir, vexillumHome, task.CampSlot)
		if err != nil {
			fmt.Fprintf(stderr, "vexillum: resolving old camp: %v\n", err)
			return 1
		}
		if err := soldier.DiscardInHerdr(task, c, client, homeDir); err != nil {
			fmt.Fprintf(stderr, "vexillum: discarding old camp: %v\n", err)
			return 1
		}
	}

	// The fresh run below reuses this task's original, unchanged prompt
	// (task.Prompt never gets its report instructions written back onto
	// it - see internal/soldier.RunInHerdr), so its candidate agent name
	// (internal/soldier.herdrAgentName, a slug of that same prompt) will
	// very likely come out identical to the dead soldier's. Any report
	// the dead soldier left behind at that exact path must be cleared
	// before relaunching - otherwise it could be mistaken for the new
	// attempt's own report (e.g. by 'vexillum release' gating on mere
	// existence) even if the new soldier never gets around to writing
	// one itself.
	if task.HerdrAgentName != "" {
		if err := report.Remove(projectRoot, task.HerdrAgentName, task.ID); err != nil {
			fmt.Fprintf(stderr, "vexillum: %v\n", err)
			return 1
		}
		// Same reasoning as report.Remove above: a leftover pause file
		// from the dead soldier's previous life would sit at the exact
		// path the freshly re-dispatched soldier is about to be told to
		// write to, and could be mistaken for its own declaration.
		if err := pause.Remove(projectRoot, task.HerdrAgentName); err != nil {
			fmt.Fprintf(stderr, "vexillum: %v\n", err)
			return 1
		}
	}

	task.Redispatches++
	task.Status = state.StatusPending
	task.CampSlot = 0
	task.CampPath = ""
	task.CampBranch = ""
	task.CampBase = ""
	task.HerdrWorkspaceID = ""
	task.HerdrTabID = ""
	task.HerdrPaneID = ""
	task.HerdrAgentName = ""
	task.Output = ""
	task.ExitCode = nil
	// Clearing this is not cosmetic: a stale mark left over from the
	// previous life of this task would let sentinel.Tick's
	// notFoundConfirmWindow (already elapsed, since it's what got this
	// task marked interrupted in the first place) condemn the freshly
	// re-dispatched task back to interrupted on its very first
	// AgentStatus hiccup.
	task.AgentNotFoundSince = time.Time{}
	task.UpdatedAt = time.Now().UTC()
	if err := state.Save(projectRoot, task); err != nil {
		fmt.Fprintf(stderr, "vexillum: persisting reset task: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "redispatching task %s (redispatch #%d)\n", task.ID, task.Redispatches)
	return acquireAndRunInHerdr(projectDir, vexillumHome, workspaceID, task, client, stdout, stderr)
}
