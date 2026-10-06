package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/ghpr"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/report"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

const strikeUsage = `Strike a soldier's camp (dismantle it and return it to the pool) once its work has landed.

Usage:
  ` + cmdname.Name + ` strike <task-id> [--force] [--discard]

Refuses unless the camp is clean and (for a mission) landed. For a scout,
also refuses unless its final report exists at
~/.vexillum/projects/<project>/reports/<agent-name>.md - the report is
the scout's work product, the same way a mission's is its landed commit.
--force skips the report check (never the clean/landed camp check) - an
explicit, logged escape hatch, never silent.

A shipped mission lands when its pull request is merged, so strike checks
that. When the camp's commits are not on the base branch yet, the refusal
says to merge the pull request and run git pull on the base branch in the
project checkout, then retry. If gh is installed and logged in, strike also
asks GitHub whether the pull request merged (only for a shipped task, and
never required): a merged pull request counts as landed once its merge
commit is on the local base branch, even when later commits on the base
touch the same lines and the content check would refuse forever. Until the
base has been pulled, the refusal says so.

--discard strikes the camp even when that check fails, or when the camp has
uncommitted changes. It prints exactly what it threw away: the unlanded
commits (hash and subject) and the uncommitted changes, which are reset. Use
it only when the general has confirmed that the camp's work is already on the
base or is abandoned - never on your own judgment. The worktree is detached
from the task branch, which is kept with the discarded commits: the output
says how many are unlanded and that it can be deleted by hand.

A herdr tab that is already gone (a soldier stopped by hand) never fails a
strike. Any other herdr error is reported after the camp was returned and the
task closed (an interrupted or failed task becomes struck); striking again
retries closing the pane, and on a task whose camp is already back in the pool
it only cleans up the record. A camp leased to another task is never touched.

On success, returns the worktree to the pool for reuse and closes the
herdr pane, then prunes: it deletes the task's local branch
vexillum/<task-id> with git branch -d semantics (never -D) when the branch
is an ancestor of the base branch, or when the task is shipped, gh reports
its pull request as merged and the base was pulled. Otherwise, or when the
branch is checked out in another worktree, the branch is kept and one line
says why and what to do. It also runs git worktree prune on the project
repo, and prints what was pruned in one short line. --discard deletes a
branch only under those same rules, so an unlanded branch survives it.
`

// Strike runs the "vx strike" command.
func Strike(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(strikeUsage)
		return 0
	}
	if len(args) == 0 {
		fmt.Print(strikeUsage)
		return 1
	}

	taskID, opts, err := parseStrikeArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}

	projectDir, vexillumHome, err := resolveDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}

	return runStrike(projectDir, vexillumHome, taskID, opts, herdr.CLI{}, os.Stdout, os.Stderr)
}

// strikeOptions are strike's flags.
type strikeOptions struct {
	// Force skips a scout's report check.
	Force bool
	// Discard strikes a camp that is dirty or not landed, reporting what
	// it threw away.
	Discard bool
}

func parseStrikeArgs(args []string) (taskID string, opts strikeOptions, err error) {
	for _, a := range args {
		switch a {
		case "--force":
			opts.Force = true
			continue
		case "--discard":
			opts.Discard = true
			continue
		}
		if strings.HasPrefix(a, "-") {
			return "", strikeOptions{}, fmt.Errorf("unknown flag %q for strike", a)
		}
		if taskID != "" {
			return "", strikeOptions{}, fmt.Errorf("unexpected extra argument %q", a)
		}
		taskID = a
	}
	if taskID == "" {
		return "", strikeOptions{}, fmt.Errorf("missing task id")
	}
	return taskID, opts, nil
}

func runStrike(projectDir, vexillumHome, taskID string, opts strikeOptions, client herdr.Client, stdout, stderr io.Writer) int {
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

	// A scout's report is its work product, the same way a mission's
	// landed commit is (checked by camp.Strike itself, via
	// soldier.StrikeInHerdr below) - a mission never has one to check
	// (internal/report's package doc), so this only ever applies to a
	// scout. --force is the explicit, logged escape hatch, for use after
	// explicit discard approval.
	if task.Kind == state.KindScout && !opts.Force && !report.Exists(projectRoot, task.HerdrAgentName, task.ID) {
		fmt.Fprintf(stderr, cmdname.Name+": strike refused: scout task %s has no report at %s\n", taskID, report.Path(projectRoot, task.HerdrAgentName, task.ID))
		fmt.Fprintln(stderr, cmdname.Name+": the report is the work product - have the soldier write it, or pass --force to strike anyway")
		return 1
	}

	c, err := camp.Resolve(projectDir, vexillumHome, task.CampSlot)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": resolving camp: %v\n", err)
		return 1
	}

	campOpts := camp.StrikeOptions{
		Shipped:  task.Status == state.StatusShipped,
		PRMerged: mergedPullRequest(projectDir, task, c.Branch),
		Discard:  opts.Discard,
	}
	report, err := soldier.StrikeInHerdrWith(task, c, client, campOpts)
	printDiscarded(stdout, c.Branch, report)
	var paneErr *soldier.PaneCloseError
	if err != nil && !errors.As(err, &paneErr) {
		fmt.Fprintf(stderr, cmdname.Name+": strike refused: %v\n", err)
		return 1
	}

	// The camp is back in the pool at this point, so the task record is
	// closed even when the herdr pane could not be: a half state would leave
	// the task interrupted with nothing left to strike.
	if task.Status == state.StatusInterrupted || task.Status == state.StatusFailed {
		task.Status = state.StatusStruck
		task.UpdatedAt = time.Now().UTC()
		if err := state.Save(projectRoot, task); err != nil {
			fmt.Fprintf(stderr, cmdname.Name+": camp struck, but recording task %s as struck failed: %v\n", taskID, err)
			fmt.Fprintf(stderr, cmdname.Name+": run '"+cmdname.Name+" strike %s' again to finish the cleanup\n", taskID)
			return 1
		}
	}
	code := 0
	if paneErr != nil {
		fmt.Fprintf(stderr, cmdname.Name+": struck, but %v\n", paneErr)
		fmt.Fprintf(stderr, cmdname.Name+": run '"+cmdname.Name+" strike %s' again to retry closing the pane\n", taskID)
		code = 1
	} else {
		fmt.Fprintln(stdout, "struck: camp returned to the pool, herdr pane closed.")
	}
	// Pruning is housekeeping after a strike that already happened, so a
	// failure here is reported but never turns the strike into a failure.
	pruned, err := camp.Prune(c, campOpts)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": struck, but pruning failed: %v\n", err)
		return code
	}
	printPruned(stdout, pruned)
	return code
}

// printPruned says in one short line what was pruned, and why a branch was
// kept when it was.
func printPruned(w io.Writer, p camp.PruneReport) {
	var done []string
	if p.DeletedBranch != "" {
		done = append(done, "deleted branch "+p.DeletedBranch)
	}
	if p.PrunedWorktrees > 0 {
		done = append(done, fmt.Sprintf("pruned %d stale worktree registration(s)", p.PrunedWorktrees))
	}
	if len(done) > 0 {
		fmt.Fprintln(w, "pruned: "+strings.Join(done, ", ")+".")
	}
	if p.Kept != "" {
		fmt.Fprintln(w, p.Kept+".")
	}
}

// mergedPullRequest asks GitHub whether the pull request of a shipped task's
// branch merged, returning what camp.StrikeWith needs to trust that. It is
// best effort and never required: nil when the task is not shipped (no
// network call at all), gh is missing or logged out, the lookup fails, or
// the pull request is not merged.
func mergedPullRequest(projectDir string, task state.Task, branch string) *camp.PRMerge {
	if task.Status != state.StatusShipped || !ghpr.Installed() || !ghpr.LoggedIn() {
		return nil
	}
	pr, err := ghpr.View(projectDir, branch)
	if err != nil || pr.State != ghpr.StateMerged {
		return nil
	}
	merged := &camp.PRMerge{HeadCommit: pr.HeadRefOid}
	if pr.MergeCommit != nil {
		merged.MergeCommit = pr.MergeCommit.Oid
	}
	return merged
}

// printDiscarded says what a --discard strike threw away, so the general
// can see exactly what is gone.
func printDiscarded(w io.Writer, branch string, report camp.StrikeReport) {
	if len(report.DiscardedCommits) > 0 {
		fmt.Fprintf(w, "discarded %d unlanded commit(s) of %s:\n", len(report.DiscardedCommits), branch)
		for _, line := range report.DiscardedCommits {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
	if len(report.DiscardedChanges) > 0 {
		fmt.Fprintf(w, "discarded %d uncommitted change(s):\n", len(report.DiscardedChanges))
		for _, line := range report.DiscardedChanges {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
}
