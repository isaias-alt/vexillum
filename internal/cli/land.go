package cli

import (
	"fmt"
	"io"
	"os"

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

Once that fast-forward merge succeeds, land automatically releases the
mission's camp back to the pool and closes its herdr pane too - the same
release logic '` + cmdname.Name + ` release' itself uses, which only clears a camp
that's already clean and landed, so this doesn't relax that safeguard. A
merge that's refused (dirty checkout, or diverged branch) never touches
the camp at all. In the rare case the merge succeeds but that automatic
release then fails, land reports both outcomes plainly - the merge is
NOT undone - and leaves the camp for '` + cmdname.Name + ` release <task-id>' to
retry by hand.

For a task already shipped through vexillum's own tribunal pipeline
('` + cmdname.Name + ` ship'), this instead merges the real pull request on GitHub -
the PR, not the camp's own branch, is the source of truth once the
general (or CI, or a reviewer) may have pushed further commits directly
to it on GitHub. Requires "gh". Refuses unless the pull request is open,
not a draft, mergeable, and every check is green; the merge is bound to
the exact head just verified. This path never auto-releases: the branch
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

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+": cannot determine home directory:", err)
		return 1
	}

	return runLand(projectDir, vexillumHome, homeDir, args[0], herdr.CLI{}, os.Stdout, os.Stderr)
}

func runLand(projectDir, vexillumHome, homeDir, taskID string, client herdr.Client, stdout, stderr io.Writer) int {
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
	// succeeded, releasing is no longer a judgment call - reuse the exact
	// release logic 'vx release' uses (soldier.ReleaseInHerdr, which
	// still refuses anything but a clean, landed camp) so the operator
	// doesn't have to chain a manual 'vx release' every time. If it
	// fails anyway (rare - the merge just made the camp clean and landed),
	// the merge itself stands: report both outcomes plainly and leave the
	// camp for a manual release, never swallow the error.
	if err := soldier.ReleaseInHerdr(task, c, client, homeDir); err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": landed, but automatic release failed: %v\n", err)
		fmt.Fprintf(stderr, cmdname.Name+": run '"+cmdname.Name+" release %s' by hand to clean up the camp\n", taskID)
		return 1
	}
	fmt.Fprintln(stdout, "released: camp returned to the pool, herdr pane closed.")
	return 0
}

// mergeShippedPR merges a shipped mission's real PR on GitHub - the
// vexillum-side replacement for a fast-forward land once a mission has
// gone through vexillum's own tribunal pipeline (see internal/ghpr for
// the verification and merge itself). Unlike a local fast-forward, this
// never auto-releases the camp: the branch is still in flight until the
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
