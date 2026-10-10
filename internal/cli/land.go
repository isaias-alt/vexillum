package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/ghpr"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

const landUsage = `Land a finished mission's work into this project's base branch.

Usage:
  ` + cmdname.Name + ` land <task-id>

Fast-forwards this project's own checkout to the mission's branch.
Refuses (leaving everything untouched) unless this checkout is clean and
the merge is a clean fast-forward - never forces or rebases anything.

Once that fast-forward merge succeeds, land automatically strikes the
mission's camp, returning it to the pool and closing its herdr pane too - the same
strike logic '` + cmdname.Name + ` strike' itself uses, which only clears a camp
that's already clean and landed, so this doesn't relax that safeguard. A
merge that's refused (dirty checkout, or diverged branch) never touches
the camp at all. In the rare case the merge succeeds but that automatic
strike then fails, land reports both outcomes plainly (the merge is
NOT undone) and leaves the camp for '` + cmdname.Name + ` strike <task-id>' to
retry by hand.

For a task already shipped through vexillum's own tribunal pipeline
('` + cmdname.Name + ` ship'), this instead merges the real pull request on GitHub -
the PR, not the camp's own branch, is the source of truth once the
general (or CI, or a reviewer) may have pushed further commits directly
to it on GitHub. Requires "gh". Refuses unless the pull request is open,
not a draft, mergeable, and every check is green; the merge is bound to
the exact head just verified. This path never auto-strikes: the branch
is still in flight until the general merges the real PR.
`

// Land runs the "vx land" command.
func Land(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(landUsage)
		return 0
	}
	if len(args) == 0 {
		fmt.Print(landUsage)
		return 1
	}

	projectDir, vexillumHome, err := resolveDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}

	return runLand(projectDir, vexillumHome, args[0], herdr.CLI{}, os.Stdout, os.Stderr)
}

func runLand(projectDir, vexillumHome, taskID string, client herdr.Client, stdout, stderr io.Writer) int {
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

	if task.Status == state.StatusShipped {
		return mergeShippedPR(projectDir, task, stdout, stderr)
	}

	c, err := camp.Resolve(projectDir, vexillumHome, task.CampSlot)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": resolving camp: %v\n", err)
		return 1
	}

	if err := camp.Land(c); err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": land refused: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "landed: fast-forwarded %s to %s\n", projectDir, c.Branch)

	// The fast-forward merge above is the safety gate; once it's
	// succeeded, striking is no longer a judgment call - reuse the exact
	// strike logic 'vx strike' uses (soldier.StrikeInHerdr, which
	// still refuses anything but a clean, landed camp) so the operator
	// doesn't have to chain a manual 'vx strike' every time. If it
	// fails anyway (rare - the merge just made the camp clean and landed),
	// the merge itself stands: report both outcomes plainly and leave the
	// camp for a manual strike, never swallow the error.
	strikeErr := soldier.StrikeInHerdr(task, c, client)

	// The merge stands whatever the strike did, so the task record must say
	// the work landed. It is written after the strike has released the lease
	// (the sentinel reopens a settled task whose agent is working again, but
	// only while its camp is leased) and closed the pane, and in every case:
	// a record left running would turn interrupted once the sentinel
	// notices the pane is gone.
	recordErr := recordLanded(projectRoot, taskID)

	if strikeErr != nil {
		fmt.Fprintf(stderr, cmdname.Name+": landed, but automatic strike failed: %v\n", strikeErr)
		fmt.Fprintf(stderr, cmdname.Name+": run '"+cmdname.Name+" strike %s' by hand to clean up the camp\n", taskID)
	}
	if recordErr != nil {
		fmt.Fprintf(stderr, cmdname.Name+": landed, but recording task %s as done failed: %v\n", taskID, recordErr)
	}
	if strikeErr != nil || recordErr != nil {
		return 1
	}
	fmt.Fprintln(stdout, "struck: camp returned to the pool, herdr pane closed.")
	return 0
}

// recordLanded settles the task record of a mission that has just landed.
// A task that was settled and then asked for more work (a rebase, say) is
// running again when the commander lands it, and a running task whose pane
// is gone is exactly what the sentinel marks interrupted. The task is
// re-read first so a sentinel write since land started is not undone. Only
// the open states move to done, the state a landed mission rests in; a
// terminal one (interrupted, failed, struck, shipped) is left alone.
func recordLanded(projectRoot, taskID string) error {
	task, err := state.Load(projectRoot, taskID)
	if err != nil {
		return fmt.Errorf("reloading task: %w", err)
	}
	switch task.Status {
	case state.StatusRunning, state.StatusBlocked, state.StatusUnconfirmed:
	default:
		return nil
	}
	task.Status = state.StatusDone
	task.UpdatedAt = time.Now().UTC()
	task.IdleUnconfirmedSince = time.Time{}
	task.AgentNotFoundSince = time.Time{}
	if err := state.Save(projectRoot, task); err != nil {
		return fmt.Errorf("saving task: %w", err)
	}
	return nil
}

// mergeShippedPR merges a shipped mission's real PR on GitHub - the
// vexillum-side replacement for a fast-forward land once a mission has
// gone through vexillum's own tribunal pipeline (see internal/ghpr for
// the verification and merge itself). Unlike a local fast-forward, this
// never auto-strikes the camp: the branch is still in flight until the
// general merges the real PR.
func mergeShippedPR(projectDir string, task state.Task, stdout, stderr io.Writer) int {
	if !ghpr.Installed() {
		fmt.Fprintln(stderr, cmdname.Name+": 'gh' is not installed - required to merge a shipped mission's PR (https://cli.github.com)")
		return 1
	}
	if task.CampBranch == "" {
		fmt.Fprintf(stderr, cmdname.Name+": task %s has no recorded camp branch to merge\n", task.ID)
		return 1
	}

	url, err := ghpr.MergeShipped(projectDir, task.CampBranch)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "merged %s\n", url)
	return 0
}
