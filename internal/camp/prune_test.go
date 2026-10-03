package camp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func branchExists(t *testing.T, dir, branch string) bool {
	t.Helper()
	return strings.TrimSpace(runGitT(t, dir, "branch", "--list", branch)) != ""
}

func TestPrune(t *testing.T) {
	tests := []struct {
		name string
		// setup prepares the camp (already acquired, with one commit) and
		// returns the options Prune runs with.
		setup       func(t *testing.T, project string, c Camp) ReleaseOptions
		wantDeleted bool
		wantKept    string // substring of PruneReport.Kept, empty for none
	}{
		{
			name: "landed branch is deleted",
			setup: func(t *testing.T, project string, c Camp) ReleaseOptions {
				runGitT(t, project, "merge", "--ff-only", c.Branch)
				return ReleaseOptions{}
			},
			wantDeleted: true,
		},
		{
			name: "unmerged branch is kept",
			setup: func(t *testing.T, project string, c Camp) ReleaseOptions {
				return ReleaseOptions{}
			},
			wantKept: "not merged into main",
		},
		{
			name: "branch checked out in another worktree is kept",
			setup: func(t *testing.T, project string, c Camp) ReleaseOptions {
				runGitT(t, project, "merge", "--ff-only", c.Branch)
				other := filepath.Join(t.TempDir(), "other")
				runGitT(t, project, "worktree", "add", "--force", other, c.Branch)
				return ReleaseOptions{}
			},
			wantKept: "checked out in",
		},
		{
			name: "merged pull request on a pulled base deletes a non-ancestor branch",
			setup: func(t *testing.T, project string, c Camp) ReleaseOptions {
				head := strings.TrimSpace(runGitT(t, c.Path, "rev-parse", "HEAD"))
				// Squash-merge: same content, different commit.
				runGitT(t, project, "merge", "--squash", c.Branch)
				runGitT(t, project, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "squash")
				merge := strings.TrimSpace(runGitT(t, project, "rev-parse", "HEAD"))
				return ReleaseOptions{Shipped: true, PRMerged: &PRMerge{MergeCommit: merge, HeadCommit: head}}
			},
			wantDeleted: true,
		},
		{
			name: "merged pull request is ignored when the task is not shipped",
			setup: func(t *testing.T, project string, c Camp) ReleaseOptions {
				head := strings.TrimSpace(runGitT(t, c.Path, "rev-parse", "HEAD"))
				return ReleaseOptions{PRMerged: &PRMerge{MergeCommit: head, HeadCommit: head}}
			},
			wantKept: "not merged into main",
		},
		{
			name: "merged pull request whose merge commit is not on the base is kept",
			setup: func(t *testing.T, project string, c Camp) ReleaseOptions {
				head := strings.TrimSpace(runGitT(t, c.Path, "rev-parse", "HEAD"))
				return ReleaseOptions{Shipped: true, PRMerged: &PRMerge{MergeCommit: head, HeadCommit: head}}
			},
			wantKept: "not merged into main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project := initProjectRepo(t)
			home := t.TempDir()
			c, err := Acquire(project, home, "task-1")
			if err != nil {
				t.Fatalf("Acquire: %v", err)
			}
			if err := os.WriteFile(filepath.Join(c.Path, "change.txt"), []byte("hi\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			commitAll(t, c.Path, "work")
			opts := tt.setup(t, project, c)

			// Mark the slot idle the way a release does, without its landed
			// check: Prune carries its own safety.
			unlock, err := lockPool(c.PoolRoot)
			if err != nil {
				t.Fatal(err)
			}
			pool, err := loadPool(c.PoolRoot)
			if err != nil {
				t.Fatal(err)
			}
			pool.Slots[0].LeasedBy = ""
			if err := savePool(c.PoolRoot, pool); err != nil {
				t.Fatal(err)
			}
			unlock()

			report, err := Prune(c, opts)
			if err != nil {
				t.Fatalf("Prune: %v", err)
			}

			if got := report.DeletedBranch == c.Branch; got != tt.wantDeleted {
				t.Errorf("deleted = %v, want %v (report %+v)", got, tt.wantDeleted, report)
			}
			if exists := branchExists(t, project, c.Branch); exists == tt.wantDeleted {
				t.Errorf("branch exists = %v, want %v", exists, !tt.wantDeleted)
			}
			if tt.wantKept == "" && report.Kept != "" {
				t.Errorf("unexpected keep reason %q", report.Kept)
			}
			if tt.wantKept != "" && !strings.Contains(report.Kept, tt.wantKept) {
				t.Errorf("keep reason %q does not contain %q", report.Kept, tt.wantKept)
			}
			if strings.Contains(report.Kept, "-D") && !strings.Contains(report.Kept, "git branch -D "+c.Branch) {
				t.Errorf("keep reason should tell the exact command: %q", report.Kept)
			}
			// The camp must stay usable: a deleted branch leaves it detached.
			if tt.wantDeleted {
				if got := strings.TrimSpace(runGitT(t, c.Path, "rev-parse", "--abbrev-ref", "HEAD")); got != "HEAD" {
					t.Errorf("camp should be detached after the branch is deleted, on %q", got)
				}
			}
		})
	}
}

func TestPrune_LeavesABranchAloneOnceTheSlotIsReused(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()
	c, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	// The slot is still leased: another task owns it now as far as Prune can tell.
	report, err := Prune(c, ReleaseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.DeletedBranch != "" || report.Kept != "" || !branchExists(t, project, c.Branch) {
		t.Errorf("a leased slot's branch must be untouched, got %+v", report)
	}
}

func TestPrune_RemovesStaleWorktreeRegistrations(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()
	c, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(t.TempDir(), "gone")
	runGitT(t, project, "worktree", "add", "-b", "scratch", stale)
	if err := os.RemoveAll(stale); err != nil {
		t.Fatal(err)
	}
	report, err := Prune(c, ReleaseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.PrunedWorktrees != 1 {
		t.Errorf("PrunedWorktrees = %d, want 1", report.PrunedWorktrees)
	}
}
