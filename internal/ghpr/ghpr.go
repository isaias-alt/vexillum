// Package ghpr handles a shipped mission's real GitHub pull request end
// to end via the "gh" CLI: opening it (Create, once "vx ship" has
// pushed a mission's branch through internal/tribunal's own validation
// pipeline) and later merging it (MergeShipped, "vx land" on a
// shipped task) - the PR, not the project's own camp, is the source of
// truth once a mission has shipped, so land verifies live, right before
// merging, that the pull request is open, not a draft, mergeable, and
// every check is green - binding the merge to the exact head it just
// verified via --match-head-commit, so a push landing between that read
// and the merge fails the merge instead of landing something nothing
// checked.
//
// A scoped-down version of what github.com/upstream's
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

// Create opens a new pull request for branch via "gh pr create", once a
// mission has passed vexillum's own tribunal pipeline
// (internal/tribunal) - the vexillum-side replacement for review-tool'
// own auto-open-PR step, now that the push target is the real remote
// directly rather than a gate remote that opened the PR on vexillum's
// behalf. base may be empty, letting gh fall back to the repository's
// default branch. Returns the new pull request's URL.
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

// ReviewNotesSection renders the tribunal review's non-blocking (info)
// findings as a markdown section to append to a pull request body: a blank
// line, a heading and one bullet per note. Empty when there are none, so a
// clean review leaves the body untouched.
func ReviewNotesSection(notes []string) string {
	if len(notes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n## Review notes\n\nNon-blocking findings from the adversarial review:\n\n")
	for _, n := range notes {
		b.WriteString("- " + n + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
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
