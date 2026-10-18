package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/pause"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/report"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

const redispatchUsage = `Re-dispatch an interrupted task from its original prompt.

Usage:
  ` + cmdname.Name + ` redispatch <task-id>

Only a task in status "interrupted" can be re-dispatched. This is
re-dispatch, not resumption: the task's dirty camp (working tree and any
commits it never landed) is discarded, a fresh camp is created, and the
mission is relaunched from its original prompt as if freshly dispatched -
none of the dead soldier's partial work or agent session is recovered.
The task is relaunched on the harness it was dispatched with. Requires
HERDR_WORKSPACE_ID (run this from inside a herdr-managed pane) and both herdr
and that harness's CLI (claude, or pi) on PATH; if any is missing it says so
and exits before creating anything.
`

// Redispatch runs the "vx redispatch" command.
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
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}

	workspaceID, err := harnessPreconditions("redispatch", taskHarness(projectDir, vexillumHome, args[0]), os.Getenv, exec.LookPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}

	ensureSentinelRunning(vexillumHome, os.Stderr)

	return runRedispatch(projectDir, vexillumHome, workspaceID, args[0], herdr.CLI{}, os.Stdout, os.Stderr)
}

// taskHarness is the harness name recorded on the task with the given id, or
// "" (claude) when it cannot be read: the caller only needs it to know which
// CLI must be on PATH, and runRedispatch reports a missing or unreadable task
// with its own, more specific message.
func taskHarness(projectDir, vexillumHome, id string) string {
	if state.ValidateID(id) != nil {
		return ""
	}
	projectRoot, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		return ""
	}
	task, err := state.Load(projectRoot, id)
	if err != nil {
		return ""
	}
	return task.Harness
}

func runRedispatch(projectDir, vexillumHome, workspaceID, taskID string, client herdr.Client, stdout, stderr io.Writer) int {
	if err := state.ValidateID(taskID); err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}

	projectRoot, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}

	task, err := state.Load(projectRoot, taskID)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": loading task %s: %v\n", taskID, err)
		return 1
	}

	// The task keeps the harness it was dispatched with. One this build
	// cannot drive is refused before the old camp is discarded, so the task
	// is left as it was.
	if _, err := soldier.HarnessFor(task.Harness); err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": task %s: %v\n", taskID, err)
		return 1
	}

	if task.Status != state.StatusInterrupted {
		fmt.Fprintf(stderr, cmdname.Name+": task %s is %s, not interrupted - only an interrupted task can be re-dispatched (a done mission is landed with '"+cmdname.Name+" land' and struck; a running one is left alone)\n", taskID, task.Status)
		return 1
	}

	if task.CampSlot != 0 {
		c, err := camp.Resolve(projectDir, vexillumHome, task.CampSlot)
		if err != nil {
			fmt.Fprintf(stderr, cmdname.Name+": resolving old camp: %v\n", err)
			return 1
		}
		if err := soldier.DiscardInHerdr(task, c, client); err != nil {
			fmt.Fprintf(stderr, cmdname.Name+": discarding old camp: %v\n", err)
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
	// attempt's own report (e.g. by 'vx strike' gating on mere
	// existence) even if the new soldier never gets around to writing
	// one itself.
	if task.HerdrAgentName != "" {
		if err := report.Remove(projectRoot, task.HerdrAgentName, task.ID); err != nil {
			fmt.Fprintf(stderr, cmdname.Name+": %v\n", err)
			return 1
		}
		// Same reasoning as report.Remove above: a leftover pause file
		// from the dead soldier's previous life would sit at the exact
		// path the freshly re-dispatched soldier is about to be told to
		// write to, and could be mistaken for its own declaration.
		if err := pause.Remove(projectRoot, task.HerdrAgentName); err != nil {
			fmt.Fprintf(stderr, cmdname.Name+": %v\n", err)
			return 1
		}
	}

	// The reset is applied to the record on disk under the tasks lock, so a
	// write made since this command loaded it (the sentinel's transcript
	// refresh, say) is built on, not overwritten with the loaded copy.
	task, err = updateRedispatchReset(projectRoot, taskID)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": persisting reset task: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "redispatching task %s (redispatch #%d)\n", task.ID, task.Redispatches)
	return acquireAndRunInHerdr(projectDir, vexillumHome, workspaceID, task, client, stdout, stderr)
}

// updateRedispatchReset puts a task back to the state of a fresh dispatch that
// has not acquired a camp yet, and returns it as saved. It sets only the
// fields a re-dispatch owns: the counter, the status, the old camp and herdr
// assignment, the dead soldier's transcript, exit code and question.
func updateRedispatchReset(projectRoot, taskID string) (state.Task, error) {
	var reset state.Task
	_, err := state.Update(projectRoot, taskID, func(task *state.Task) (bool, error) {
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
		task.Decision = nil
		// Clearing this is not cosmetic: a stale mark left over from the
		// previous life of this task would let sentinel.Tick's
		// notFoundConfirmWindow (already elapsed, since it's what got this
		// task marked interrupted in the first place) condemn the freshly
		// re-dispatched task back to interrupted on its very first
		// AgentStatus hiccup.
		task.AgentNotFoundSince = time.Time{}
		task.UpdatedAt = time.Now().UTC()
		reset = *task
		return true, nil
	})
	return reset, err
}
