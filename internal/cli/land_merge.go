package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"github.com/isaias-alt/vexillum/internal/state"
)

// ghPullRequest is the subset of "gh pr view --json ..." fields
// mergeShippedPR needs to verify a shipped mission's PR before merging it.
type ghPullRequest struct {
	Number     int    `json:"number"`
	State      string `json:"state"`
	IsDraft    bool   `json:"isDraft"`
	Mergeable  string `json:"mergeable"`
	HeadRefOid string `json:"headRefOid"`
	URL        string `json:"url"`
}

// mergeShippedPR merges a shipped mission's real PR on GitHub - the
// vexillum-side replacement for "vexillum land" once a mission has gone
// through the no-mistakes gate: the PR, not this project's camp, is the
// source of truth (no-mistakes may have applied auto-fix commits, or
// rebased, in its own isolated worktree that this camp never sees). It
// verifies live, right before merging, that the pull request is open, not
// a draft, mergeable, and every check is green - binding the merge to the
// exact head it just verified via --match-head-commit, so a push landing
// between that read and the merge fails the merge instead of landing
// something nothing checked.
//
// A scoped-down version of what github.com/kunchenguid/firstmate's
// fm-pr-merge.sh does for the same problem: no away-authority or
// captain-hold locking, since vexillum has one commander, not concurrent
// agents trading merge authority over the same repo. No recorded pr=
// either - the PR is looked up from the task's own camp branch name, the
// same way GitHub already associates a branch with its open PR, so
// vexillum's state has nothing new to keep in sync with GitHub's.
func mergeShippedPR(projectDir string, task state.Task, stdout, stderr io.Writer) int {
	if _, err := exec.LookPath("gh"); err != nil {
		fmt.Fprintln(stderr, "vexillum: 'gh' is not installed - required to merge a shipped mission's PR (https://cli.github.com)")
		return 1
	}
	if task.CampBranch == "" {
		fmt.Fprintf(stderr, "vexillum: task %s has no recorded camp branch to merge\n", task.ID)
		return 1
	}

	pr, err := ghPRView(projectDir, task.CampBranch)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: reading the pull request for %s: %v\n", task.CampBranch, err)
		return 1
	}

	var refusals []string
	if pr.State != "OPEN" {
		refusals = append(refusals, fmt.Sprintf("state is %q, not open", pr.State))
	}
	if pr.IsDraft {
		refusals = append(refusals, "still a draft")
	}
	if pr.Mergeable != "MERGEABLE" {
		refusals = append(refusals, fmt.Sprintf("not mergeable (mergeable=%q)", pr.Mergeable))
	}
	if len(refusals) > 0 {
		fmt.Fprintf(stderr, "vexillum: refusing to merge %s:\n  - %s\n", pr.URL, strings.Join(refusals, "\n  - "))
		return 1
	}

	checksCmd := exec.Command("gh", "pr", "checks", strconv.Itoa(pr.Number))
	checksCmd.Dir = projectDir
	if out, err := checksCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(stderr, "vexillum: refusing to merge %s: checks are not all green\n%s\n", pr.URL, strings.TrimSpace(string(out)))
		return 1
	}

	mergeCmd := exec.Command("gh", "pr", "merge", strconv.Itoa(pr.Number), "--match-head-commit", pr.HeadRefOid, "--squash")
	mergeCmd.Dir = projectDir
	out, mergeErr := mergeCmd.CombinedOutput()
	if mergeErr != nil {
		fmt.Fprintf(stderr, "vexillum: merging %s: %v\n%s\n", pr.URL, mergeErr, strings.TrimSpace(string(out)))
		return 1
	}

	fmt.Fprintf(stdout, "merged %s\n", pr.URL)
	return 0
}

// ghPRView reads the subset of a pull request's live state mergeShippedPR
// needs, by branch name - run from projectDir so gh infers the repository
// from its own "origin" remote, same as any other gh invocation there.
func ghPRView(projectDir, branch string) (ghPullRequest, error) {
	cmd := exec.Command("gh", "pr", "view", branch, "--json", "number,state,isDraft,mergeable,headRefOid,url")
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ghPullRequest{}, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	var pr ghPullRequest
	if err := json.Unmarshal(out, &pr); err != nil {
		return ghPullRequest{}, fmt.Errorf("parsing gh pr view output: %w", err)
	}
	return pr, nil
}
