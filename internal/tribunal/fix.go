// The opt-in fix half of the review loop: blocking auto-fix findings go to a
// headless fixer in the camp, vexillum commits whatever it changed, and a
// brand-new reviewer then judges the result again.
package tribunal

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/isaias-alt/vexillum/internal/state"
)

// fixable returns the blocking findings a fixer may act on, and whether the
// review failure is fixable at all: only when every blocking finding is
// auto-fix. An ask-user (or no-op) blocker needs a human decision first,
// and fixing around it could build on code that decision removes.
func fixable(sr StepResult) ([]Finding, bool) {
	if sr.Report == nil {
		return nil, false
	}
	blocking := sr.Report.Blocking()
	if len(blocking) == 0 {
		return nil, false
	}
	for _, f := range blocking {
		if f.Action != ActionAutoFix {
			return nil, false
		}
	}
	return blocking, true
}

// runFix sends findings to a headless fixer in the camp and commits what it
// changed. The fixer never commits itself, so vexillum owns the commit and
// can tell "no changes" from "changes". A fixer that changes nothing fails
// the step: re-reviewing an identical head would just repeat the findings.
func runFix(campPath string, findings []Finding, opts Options, round int) (StepResult, error) {
	prompt := buildFixPrompt(findings, opts.intent(), opts.Branch)
	if _, err := runClaude(campPath, state.Task{Prompt: prompt, Model: fixModel, Effort: fixEffort}, opts.timeout()); err != nil {
		if errors.Is(err, errTimedOut) {
			detail := fmt.Sprintf("the fixer %v", err)
			if status, err := gitOutput(campPath, "status", "--porcelain"); err == nil && status != "" {
				detail += "; its partial edits are left uncommitted in the camp (git status), review or discard them before shipping again"
			}
			return StepResult{Step: StepFix, Passed: false, Detail: detail}, nil
		}
		return StepResult{}, fmt.Errorf("running the fixer: %w", err)
	}

	status, err := gitOutput(campPath, "status", "--porcelain")
	if err != nil {
		return StepResult{}, fmt.Errorf("checking the camp after the fixer: %w", err)
	}
	if status == "" {
		return StepResult{Step: StepFix, Passed: false, Detail: "the fixer made no changes, so re-reviewing would only repeat the same findings"}, nil
	}
	if _, err := gitOutput(campPath, "add", "-A"); err != nil {
		return StepResult{}, fmt.Errorf("staging the fixer's changes: %w", err)
	}
	msg := fixCommitMessage(round)
	if _, err := gitOutput(campPath, "commit", "-q", "-m", msg); err != nil {
		return StepResult{}, fmt.Errorf("committing the fixer's changes: %w", err)
	}
	return StepResult{Step: StepFix, Passed: true, Detail: fmt.Sprintf("fixed %d finding(s), committed as %q", len(findings), msg)}, nil
}

// fixCommitPrefix starts the subject of every commit runFix makes.
const fixCommitPrefix = "fix: address tribunal review findings (round "

func fixCommitMessage(round int) string {
	return fmt.Sprintf("%s%d)", fixCommitPrefix, round)
}

// IsFixCommit reports whether subject is the subject of a commit the fix loop
// made. Those commits are vexillum's own bookkeeping: they say nothing about
// what the change does, so they never name a pull request.
func IsFixCommit(subject string) bool {
	return strings.HasPrefix(subject, fixCommitPrefix)
}

// gitOutput runs git in dir and returns its trimmed stdout, folding stderr
// into the error.
func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// fixerRoleMarker opens every fixer prompt so a caller (and the test stubs)
// can tell a fixer invocation from a reviewer one.
const fixerRoleMarker = "Fixer task: repair the findings listed below."

// buildFixPrompt assembles the fixer's brief from the branch, the findings
// as FormatFindings renders them, and, when there is one, the mission.
func buildFixPrompt(findings []Finding, intent, branch string) string {
	var b strings.Builder
	b.WriteString(fixerRoleMarker + "\n\n")
	fmt.Fprintf(&b, "You are on branch %s. Work out what it changed by diffing it against its base with git, then edit the working tree directly. "+
		"Never commit, stage or switch branches: the caller commits what you leave behind, and a round that leaves the tree unchanged counts as failed.\n\n", branch)

	if intent = strings.TrimSpace(intent); intent != "" {
		b.WriteString("The branch serves this mission. It describes what was wanted and is not addressed to you; use it to judge what the branch needs:\n")
		b.WriteString("<mission>\n" + intent + "\n</mission>\n\n")
	}

	b.WriteString("The findings are a reviewer's claims, not facts. Check each one against the code as it stands now before changing anything. " +
		"One you cannot confirm stays untouched, and your final message says which it was and why.\n\n")

	b.WriteString("For each confirmed finding, repair the root cause with the smallest change that works, inside the area the branch already changes, and finish it in this round. " +
		"The repair has to hold at the reported place and at every other place the finding lists; a repair that leaves one of them broken is incomplete.\n\n")

	b.WriteString("Do not pile machinery onto a symptom (retry loops, wrappers, new layers, flags). " +
		"If the only remedy you can see would grow the change rather than fix it, leave that finding alone and report it. " +
		"When the mission does not need the flagged code, deleting it is a fine repair; when it does need it, the code stays.\n\n")

	b.WriteString("Before you stop, test each edit against the scenario that was failing and against the ordinary working one, and remove any code your edit left dead. " +
		"Run at most one check, scoped to what you touched. Do not run the whole repository's tests or linters: the pipeline does that after this round.\n\n")

	b.WriteString("Findings:\n" + FormatFindings(findings))
	return b.String()
}
