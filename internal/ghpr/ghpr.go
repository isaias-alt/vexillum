// Package ghpr handles a shipped mission's real GitHub pull request end to
// end via the "gh" CLI: opening it (Create, once "vx ship" has pushed a
// mission's branch through internal/tribunal's own validation pipeline) and
// later merging it (MergeShipped, "vx land" on a shipped task) - the PR, not
// the project's own camp, is the source of truth once a mission has shipped,
// so land verifies live, right before merging, that the pull request is open,
// not a draft, mergeable, and every check is green - binding the merge to the
// exact head it just verified via --match-head-commit, so a push landing
// between that read and the merge fails the merge instead of landing
// something nothing checked.
//
// There is no away-authority or captain-hold locking, since vexillum has one
// commander, not concurrent agents trading merge authority over the same
// repo. No recorded pr= either - the PR is looked up from the task's own camp
// branch name, the same way GitHub already associates a branch with its open
// PR, so vexillum's state has nothing new to keep in sync with GitHub's.
package ghpr

import (
	"encoding/json"
	"errors"
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
	// MergeCommit is the commit a merged pull request created on its base
	// branch; nil while the pull request is not merged.
	MergeCommit *Commit `json:"mergeCommit"`
}

// Commit is a commit as "gh pr view --json mergeCommit" reports it.
type Commit struct {
	Oid string `json:"oid"`
}

// StateMerged is PullRequest.State of a merged pull request.
const StateMerged = "MERGED"

// ErrNoPullRequest is wrapped by View when the branch has no pull request,
// as opposed to gh failing for any other reason.
var ErrNoPullRequest = errors.New("no pull request for the branch")

// Installed reports whether the "gh" binary is on PATH.
func Installed() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// LoggedIn reports whether gh has a usable login. It is what makes a
// best-effort lookup safe to try: a gh that is installed but not logged in
// would only fail every call it is given.
func LoggedIn() bool {
	return exec.Command("gh", "auth", "status").Run() == nil
}

// View reads a pull request's live state by branch name, run from
// projectDir so gh infers the repository from its own "origin" remote,
// same as any other gh invocation there.
func View(projectDir, branch string) (PullRequest, error) {
	cmd := exec.Command("gh", "pr", "view", branch, "--json", "number,state,isDraft,mergeable,headRefOid,url,mergeCommit")
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if strings.Contains(text, "no pull requests found") {
			return PullRequest{}, fmt.Errorf("%w %s: %s", ErrNoPullRequest, branch, text)
		}
		return PullRequest{}, fmt.Errorf("%w: %s", err, text)
	}
	var pr PullRequest
	if err := json.Unmarshal(out, &pr); err != nil {
		return PullRequest{}, fmt.Errorf("parsing gh pr view output: %w", err)
	}
	return pr, nil
}

// Create opens a new pull request for branch via "gh pr create", once a
// mission has passed vexillum's own tribunal pipeline (internal/tribunal).
// The push target is the real remote directly, so vexillum opens the PR
// itself. base may be empty, letting gh fall back to the repository's default
// branch. Returns the new pull request's URL.
func Create(projectDir, branch, base, title, body string) (url string, err error) {
	args := []string{"pr", "create", "--head", branch, "--title", title, "--body", body}
	if base != "" {
		args = append(args, "--base", base)
	}
	cmd := exec.Command("gh", args...)
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("creating a pull request for %s: %w\n%s", branch, err, strings.TrimSpace(string(out)))
	}
	return lastLine(string(out)), nil
}

// Edit replaces the title and body of pull request number, so the text of an
// open pull request follows the commits pushed to it since.
func Edit(projectDir string, number int, title, body string) error {
	cmd := exec.Command("gh", "pr", "edit", strconv.Itoa(number), "--title", title, "--body", body)
	cmd.Dir = projectDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("editing pull request #%d: %w\n%s", number, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// lastLine returns the last non-empty line of s - "gh pr create" prints
// the new pull request's URL as its final line of stdout, sometimes
// preceded by informational lines (e.g. a note about an existing draft).
func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
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
