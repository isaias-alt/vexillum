// Package ghpr merges a shipped mission's real GitHub pull request - the
// vexillum-side replacement for "vexillum land" once a mission has gone
// through the no-mistakes gate: the PR, not the project's own camp, is
// the source of truth (no-mistakes may have applied auto-fix commits, or
// rebased, in its own isolated worktree that the camp never sees). It
// verifies live, right before merging, that the pull request is open,
// not a draft, mergeable, and every check is green - binding the merge
// to the exact head it just verified via --match-head-commit, so a push
// landing between that read and the merge fails the merge instead of
// landing something nothing checked.
//
// A scoped-down version of what github.com/kunchenguid/firstmate's
// fm-pr-merge.sh does for the same problem: no away-authority or
// captain-hold locking, since vexillum has one commander, not concurrent
// agents trading merge authority over the same repo. No recorded pr=
// either - the PR is looked up from the task's own camp branch name, the
// same way GitHub already associates a branch with its open PR, so
// vexillum's state has nothing new to keep in sync with GitHub's.
package ghpr

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// PullRequest is the subset of "gh pr view --json ..." fields
// MergeShipped needs to verify a shipped mission's PR before merging it.
type PullRequest struct {
	Number     int    `json:"number"`
	State      string `json:"state"`
	IsDraft    bool   `json:"isDraft"`
	Mergeable  string `json:"mergeable"`
	HeadRefOid string `json:"headRefOid"`
	URL        string `json:"url"`
}

// Installed reports whether the "gh" binary is on PATH.
func Installed() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// View reads a pull request's live state by branch name, run from
// projectDir so gh infers the repository from its own "origin" remote,
// same as any other gh invocation there.
func View(projectDir, branch string) (PullRequest, error) {
	cmd := exec.Command("gh", "pr", "view", branch, "--json", "number,state,isDraft,mergeable,headRefOid,url")
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PullRequest{}, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	var pr PullRequest
	if err := json.Unmarshal(out, &pr); err != nil {
		return PullRequest{}, fmt.Errorf("parsing gh pr view output: %w", err)
	}
	return pr, nil
}

// MergeShipped looks up the pull request associated with branch, refuses
// to merge it unless it's open, not a draft, mergeable, and every check
// is green, then merges it (squash) bound to the exact head just
// verified. Returns the merged PR's URL on success.
func MergeShipped(projectDir, branch string) (url string, err error) {
	pr, err := View(projectDir, branch)
	if err != nil {
		return "", fmt.Errorf("reading the pull request for %s: %w", branch, err)
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
		return "", fmt.Errorf("refusing to merge %s:\n  - %s", pr.URL, strings.Join(refusals, "\n  - "))
	}

	checksCmd := exec.Command("gh", "pr", "checks", strconv.Itoa(pr.Number))
	checksCmd.Dir = projectDir
	if out, err := checksCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("refusing to merge %s: checks are not all green\n%s", pr.URL, strings.TrimSpace(string(out)))
	}

	mergeCmd := exec.Command("gh", "pr", "merge", strconv.Itoa(pr.Number), "--match-head-commit", pr.HeadRefOid, "--squash")
	mergeCmd.Dir = projectDir
	out, mergeErr := mergeCmd.CombinedOutput()
	if mergeErr != nil {
		return "", fmt.Errorf("merging %s: %w\n%s", pr.URL, mergeErr, strings.TrimSpace(string(out)))
	}

	return pr.URL, nil
}
