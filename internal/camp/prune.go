package camp

import (
	"fmt"
	"path/filepath"
	"strings"
)

// PruneReport says what Prune removed or why it kept the task's branch.
type PruneReport struct {
	// DeletedBranch is the branch that was deleted, empty when none was.
	DeletedBranch string
	// Kept is a one-line reason the branch was kept, with what the general
	// can do about it. Empty when the branch was deleted or already gone.
	Kept string
	// PrunedWorktrees counts the stale worktree registrations removed.
	PrunedWorktrees int
}

// Prune runs after a successful release. It deletes the task's local branch
// when that is safe, and runs git worktree prune for the project so stale
// camp registrations do not accumulate.
//
// The branch goes only when it is an ancestor of the base branch (git branch
// -d semantics), or when the task is shipped, GitHub reported its pull
// request as merged and that merge is on the local base. The second case is
// not an ancestor, so git branch -d would refuse it; the ref is deleted with
// update-ref against the exact tip instead, never with branch -D. A branch
// checked out in any other worktree is never deleted. Whatever else fails to
// hold, the branch is kept and PruneReport.Kept says why.
func Prune(c Camp, opts ReleaseOptions) (PruneReport, error) {
	var report PruneReport

	unlock, err := lockPool(c.PoolRoot)
	if err != nil {
		return report, err
	}
	defer unlock()

	// The slot may already belong to a new task: then its worktree is on a
	// new branch and this task's branch is not ours to touch any more.
	pool, err := loadPool(c.PoolRoot)
	if err != nil {
		return report, err
	}
	reusable := false
	for _, s := range pool.Slots {
		if s.Number == c.Slot && s.LeasedBy == "" && s.Branch == c.Branch {
			reusable = true
		}
	}

	if reusable {
		if err := pruneBranch(c, opts, &report); err != nil {
			return report, err
		}
	}

	out, err := runGit(c.ProjectDir, "worktree", "prune", "-v")
	if err != nil {
		return report, fmt.Errorf("pruning stale worktrees: %w", err)
	}
	report.PrunedWorktrees = len(nonEmptyLines(out))
	return report, nil
}

func pruneBranch(c Camp, opts ReleaseOptions, report *PruneReport) error {
	ref := "refs/heads/" + c.Branch
	tip, err := runGit(c.ProjectDir, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		return nil // already gone, nothing to prune
	}
	tip = strings.TrimSpace(tip)

	base, err := currentBranch(c.ProjectDir)
	if err != nil {
		return fmt.Errorf("resolving base branch: %w", err)
	}
	if c.Branch == base {
		return nil
	}

	other, err := checkedOutElsewhere(c, c.Branch)
	if err != nil {
		return err
	}
	if other != "" {
		report.Kept = fmt.Sprintf("kept branch %s: it is checked out in %s; free that worktree, then run git branch -d %s", c.Branch, other, c.Branch)
		return nil
	}

	ancestor, err := isAncestor(c.ProjectDir, tip, base)
	if err != nil {
		return fmt.Errorf("checking whether %s is merged: %w", c.Branch, err)
	}
	merged := false
	if !ancestor && opts.Shipped && opts.PRMerged != nil {
		merged, _ = verifyMerged(c, base, *opts.PRMerged)
	}
	if !ancestor && !merged {
		report.Kept = fmt.Sprintf("kept branch %s: it is not merged into %s (no merged pull request confirmed); once sure, run git branch -D %s", c.Branch, base, c.Branch)
		return nil
	}

	// The released camp still sits on its branch; a detached HEAD at the same
	// commit leaves its files untouched and lets the branch go.
	if onBranch, err := currentBranch(c.Path); err == nil && onBranch == c.Branch {
		if _, err := runGit(c.Path, "checkout", "--detach"); err != nil {
			return fmt.Errorf("detaching camp before deleting its branch: %w", err)
		}
	}

	if ancestor {
		if _, err := runGit(c.ProjectDir, "branch", "-d", c.Branch); err != nil {
			return fmt.Errorf("deleting branch: %w", err)
		}
	} else {
		if _, err := runGit(c.ProjectDir, "update-ref", "-d", ref, tip); err != nil {
			return fmt.Errorf("deleting branch: %w", err)
		}
		// update-ref leaves branch.<name> config behind; it holds nothing
		// worth failing over.
		_, _ = runGit(c.ProjectDir, "config", "--remove-section", "branch."+c.Branch)
	}
	report.DeletedBranch = c.Branch
	return nil
}

// checkedOutElsewhere returns the path of a worktree other than the camp's
// own that has branch checked out, or "".
func checkedOutElsewhere(c Camp, branch string) (string, error) {
	out, err := runGit(c.ProjectDir, "worktree", "list", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("listing worktrees: %w", err)
	}
	var path string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case line == "branch refs/heads/"+branch:
			if !samePath(path, c.Path) {
				return path, nil
			}
		}
	}
	return "", nil
}

// samePath compares two paths after resolving symlinks, since git reports
// the real path of a worktree (/private/var on macOS).
func samePath(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return a == b
}
