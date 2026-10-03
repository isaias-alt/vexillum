package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/ghpr"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/report"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

const releaseUsage = `Release a soldier's camp back to the pool once its work has landed.

Usage:
  ` + cmdname.Name + ` release <task-id> [--force] [--discard]

Refuses unless the camp is clean and (for a mission) landed. For a scout,
also refuses unless its final report exists at
~/.vexillum/projects/<project>/reports/<agent-name>.md - the report is
the scout's work product, the same way a mission's is its landed commit.
--force skips the report check (never the clean/landed camp check) - an
explicit, logged escape hatch, never silent.

A shipped mission lands when its pull request is merged, so release checks
that. When the camp's commits are not on the base branch yet, the refusal
says to merge the pull request and run git pull on the base branch in the
project checkout, then retry. If gh is installed and logged in, release also
asks GitHub whether the pull request merged (only for a shipped task, and
never required): a merged pull request counts as landed once its merge
commit is on the local base branch, even when later commits on the base
touch the same lines and the content check would refuse forever. Until the
base has been pulled, the refusal says so.

--discard releases the camp even when that check fails, or when the camp has
uncommitted changes. It prints exactly what it threw away: the unlanded
commits (hash and subject) and the uncommitted changes, which are reset. Use
it only when the general has confirmed that the camp's work is already on the
base or is abandoned - never on your own judgment. The discarded commits
stay reachable from the camp's branch until the slot is reused.

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

// Release runs the "vx release" command.
func Release(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(releaseUsage)
		return 0
	}
	if len(args) == 0 {
		fmt.Print(releaseUsage)
		return 1
	}

	taskID, opts, err := parseReleaseArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
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

	return runRelease(projectDir, vexillumHome, homeDir, taskID, opts, herdr.CLI{}, os.Stdout, os.Stderr)
}

// releaseOptions are release's flags.
type releaseOptions struct {
	// Force skips a scout's report check.
	Force bool
	// Discard releases a camp that is dirty or not landed, reporting what
	// it threw away.
	Discard bool
}

func parseReleaseArgs(args []string) (taskID string, opts releaseOptions, err error) {
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
			return "", releaseOptions{}, fmt.Errorf("unknown flag %q for release", a)
		}
		if taskID != "" {
			return "", releaseOptions{}, fmt.Errorf("unexpected extra argument %q", a)
		}
		taskID = a
	}
	if taskID == "" {
		return "", releaseOptions{}, fmt.Errorf("missing task id")
	}
	return taskID, opts, nil
}

func runRelease(projectDir, vexillumHome, homeDir, taskID string, opts releaseOptions, client herdr.Client, stdout, stderr io.Writer) int {
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
	// landed commit is (checked by camp.Release itself, via
	// soldier.ReleaseInHerdr below) - a mission never has one to check
	// (internal/report's package doc), so this only ever applies to a
	// scout. --force is the explicit, logged escape hatch, mirroring
	// upstream-tool's own teardown gate ("REFUSED: scout task $ID has no
	// report ... use --force after explicit discard approval").
	if task.Kind == state.KindScout && !opts.Force && !report.Exists(projectRoot, task.HerdrAgentName, task.ID) {
		fmt.Fprintf(stderr, cmdname.Name+": release refused: scout task %s has no report at %s\n", taskID, report.Path(projectRoot, task.HerdrAgentName, task.ID))
		fmt.Fprintln(stderr, cmdname.Name+": the report is the work product - have the soldier write it, or pass --force to release anyway")
		return 1
	}

	c, err := camp.Resolve(projectDir, vexillumHome, task.CampSlot)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": resolving camp: %v\n", err)
		return 1
	}

	campOpts := camp.ReleaseOptions{
		Shipped:  task.Status == state.StatusShipped,
		PRMerged: mergedPullRequest(projectDir, task, c.Branch),
		Discard:  opts.Discard,
	}
	report, err := soldier.ReleaseInHerdrWith(task, c, client, homeDir, campOpts)
	printDiscarded(stdout, c.Branch, report)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": release refused: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "released: camp returned to the pool, herdr pane closed.")
	// Pruning is housekeeping after a release that already happened, so a
	// failure here is reported but never turns the release into a failure.
	pruned, err := camp.Prune(c, campOpts)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": released, but pruning failed: %v\n", err)
		return 0
	}
	printPruned(stdout, pruned)
	return 0
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
// branch merged, returning what camp.ReleaseWith needs to trust that. It is
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

// printDiscarded says what a --discard release threw away, so the general
// can see exactly what is gone.
func printDiscarded(w io.Writer, branch string, report camp.ReleaseReport) {
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
