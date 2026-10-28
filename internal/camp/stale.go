package camp

import (
	"errors"
	"fmt"
	"path/filepath"
)

// SlotReused reports whether pool slot no longer belongs to the task that
// recorded it: the slot is leased to another task, or it sits idle on a
// branch other than the task's own (it was reused and released since). The
// worktree there is not the task's any more, so only RetireStale may deal
// with the task, never StrikeWith. branch is the task's recorded camp
// branch; when it is empty only a lease held by another task counts. A slot
// the pool does not know is not reported as reused: that stays an error for
// Resolve to name.
func SlotReused(projectRoot string, slot int, taskID, branch string) (bool, error) {
	pool, err := loadPool(filepath.Join(projectRoot, "camps"))
	if err != nil {
		return false, err
	}
	return slotReused(pool, slot, taskID, branch), nil
}

func slotReused(pool poolState, slot int, taskID, branch string) bool {
	for _, s := range pool.Slots {
		if s.Number != slot {
			continue
		}
		if s.LeasedBy != "" && s.LeasedBy != taskID {
			return true
		}
		return s.LeasedBy == "" && branch != "" && s.Branch != branch
	}
	return false
}

// RetireStale lets a strike close a task whose camp slot was reused by
// another task. Only c's ProjectDir, PoolRoot, Slot and Branch are used, and
// c.Branch must be the stale task's own branch, never the pool's: the
// worktree in the slot is not touched at all (no reset, no checkout, no
// release), and neither is the branch the slot is on now.
//
// Unless opts.Discard is set, the task's branch must prove landed on the
// project's base branch (the same proof StrikeWith accepts: an ancestor of
// the base, a merged pull request, or content already in the base). A branch
// that no longer exists holds nothing to lose and passes. With Discard the
// branch is not checked and the commits it holds beyond the base are listed
// in StrikeReport. It is deleted afterwards only when it is merged into the
// base and checked out nowhere, with git branch -d semantics.
func RetireStale(c Camp, taskID string, opts StrikeOptions) (StrikeReport, PruneReport, error) {
	var report StrikeReport
	var pruned PruneReport

	unlock, err := lockPool(c.PoolRoot)
	if err != nil {
		return report, pruned, err
	}
	defer unlock()

	pool, err := loadPool(c.PoolRoot)
	if err != nil {
		return report, pruned, err
	}
	if !slotReused(pool, c.Slot, taskID, c.Branch) {
		return report, pruned, fmt.Errorf("camp slot %d still belongs to task %s, nothing stale to retire", c.Slot, taskID)
	}

	ref := "refs/heads/" + c.Branch
	if _, err := runGit(c.ProjectDir, "rev-parse", "--verify", "--quiet", ref); err != nil {
		// Nothing left to lose or to prune.
		return report, pruned, nil
	}

	base, err := currentBranch(c.ProjectDir)
	if err != nil {
		return report, pruned, fmt.Errorf("resolving base branch: %w", err)
	}
	landed, why, err := revLanded(c.ProjectDir, ref, c.ProjectDir, base, opts)
	if err != nil && !opts.Discard {
		return report, pruned, err
	}
	if !landed && !opts.Discard {
		msg := fmt.Sprintf("branch %s has commits not yet landed on %s, refusing to retire task %s (its camp slot %d now belongs to another task)", c.Branch, base, taskID, c.Slot)
		if why != "" {
			msg += ": " + why
		} else if opts.Shipped {
			msg += fmt.Sprintf(": this mission was shipped as a pull request - merge the pull request, run git pull on %s in %s, then retry", base, c.ProjectDir)
		}
		return report, pruned, errors.New(msg)
	}
	if !landed {
		out, err := runGit(c.ProjectDir, "log", "--format=%h %s", base+".."+ref)
		if err != nil {
			return report, pruned, fmt.Errorf("listing the commits to discard: %w", err)
		}
		report.DiscardedCommits = nonEmptyLines(out)
	}

	pruneStaleBranch(c, base, ref, landed, &pruned)
	return report, pruned, nil
}

// pruneStaleBranch deletes the stale task's branch when it is an ancestor of
// the base, with git branch -d semantics: git refuses a branch checked out in
// any worktree, the slot now in use included. A branch that landed some other
// way (a merged pull request, content already in the base) is kept, and so is
// an unlanded one under Discard.
func pruneStaleBranch(c Camp, base, ref string, landed bool, report *PruneReport) {
	if c.Branch == base {
		return
	}
	ancestor, err := isAncestor(c.ProjectDir, ref, base)
	if err != nil || !ancestor {
		why := "it is not merged into " + base
		if landed {
			why = "it is not an ancestor of " + base + " (it landed as a squash or rebase merge)"
		}
		report.Kept = fmt.Sprintf("kept branch %s: %s; once sure, run git branch -D %s", c.Branch, why, c.Branch)
		return
	}
	if _, err := runGit(c.ProjectDir, "branch", "-d", c.Branch); err != nil {
		report.Kept = fmt.Sprintf("kept branch %s: git refused to delete it (checked out in a worktree?); free it, then run git branch -d %s", c.Branch, c.Branch)
		return
	}
	report.DeletedBranch = c.Branch
}
