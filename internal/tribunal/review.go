// The adversarial review step: a brand-new claude session is pointed at the
// branch, reads the diff itself with read-only git, hunts for defects it can
// substantiate with a concrete scenario, declares which changed files it
// actually read, and (when the task has a prompt) also audits the change for
// components the mission never asked for. There is no separate verifier: the
// evidence bar written into the prompt is the false-positive control.
package tribunal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

// The reviewer and the fixer are fixed, not a per-call judgment call - the
// product AGENTS.md's "Choosing a model and effort" gives sonnet/high to
// work that needs judgment but not Opus, and a review is exactly that.
const (
	reviewModel  = "sonnet"
	reviewEffort = "high"
	fixModel     = "sonnet"
	fixEffort    = "high"
)

// reviewMaxAttempts bounds the reviewer invocations one review step spends
// on output that fails schema validation: the first run plus two retries.
// A formatting slip is not a verdict; a timeout or a non-zero exit is not
// retried at all.
const reviewMaxAttempts = 3

// reviewInput is everything the reviewer is told. Deliberately no diff and
// nothing of the author's conversation or reasoning: the reviewer reads the
// diff itself with git, and intent is the commander's request, never the
// author's account of what it did.
type reviewInput struct {
	Branch     string
	BaseBranch string
	BaseSHA    string
	TargetSHA  string
	// Files is the changed-file list vexillum computed from git; the
	// reviewer is held to covering every one.
	Files []string
	// Intent is state.Task.Prompt, the original mission statement. Empty
	// means no usable prompt: the simplification pass is omitted.
	Intent string
	// FixStartSHA is the head before the first automated fix round, set on
	// re-reviews only; it switches on the fix-round provenance clause.
	FixStartSHA string
}

// runReview runs one adversarial review pass over campPath's branch since
// base, in a brand-new claude process - never a resumed session, so
// whoever prescribed a fix never certifies it. fixStartSHA is non-empty on
// a re-review after automated fix rounds. A reviewer that times out, emits
// no valid report after reviewMaxAttempts, or leaves a changed file
// unreviewed fails the step; none of those ever reads as a pass.
func runReview(campPath, base string, opts Options, fixStartSHA string) (StepResult, error) {
	baseSHA, err := gitOutput(campPath, "merge-base", base, "HEAD")
	if err != nil {
		return StepResult{}, fmt.Errorf("resolving the merge base of %s and HEAD: %w", base, err)
	}
	targetSHA, err := gitOutput(campPath, "rev-parse", "HEAD")
	if err != nil {
		return StepResult{}, fmt.Errorf("resolving HEAD: %w", err)
	}
	files, err := changedFiles(campPath, baseSHA, targetSHA)
	if err != nil {
		return StepResult{}, err
	}
	if len(files) == 0 {
		return StepResult{Step: StepReview, Passed: true, Detail: "no changes to review"}, nil
	}

	in := reviewInput{
		Branch:      opts.Branch,
		BaseBranch:  base,
		BaseSHA:     baseSHA,
		TargetSHA:   targetSHA,
		Files:       files,
		Intent:      opts.intent(),
		FixStartSHA: fixStartSHA,
	}
	var notes []string
	if in.Intent == "" {
		notes = append(notes, "simplification pass skipped: the task has no prompt to judge the change against")
	}

	prompt := buildReviewPrompt(in)
	var report Report
	var validationErr error
	for attempt := 1; ; attempt++ {
		turn := prompt
		if validationErr != nil {
			turn += "\n\nThe report you just gave was rejected: " + validationErr.Error() +
				"\nReview once more and finish with a single corrected JSON object that follows the schema exactly."
		}
		out, err := runClaude(campPath, state.Task{Prompt: turn, Model: reviewModel, Effort: reviewEffort}, opts.timeout())
		if errors.Is(err, errTimedOut) {
			return StepResult{Step: StepReview, Passed: false, Notes: notes,
				Detail: fmt.Sprintf("the reviewer %v - a timeout is never treated as a pass", err)}, nil
		}
		if err != nil {
			return StepResult{}, fmt.Errorf("running the review soldier: %w", err)
		}
		report, validationErr = parseReport(out)
		if validationErr == nil {
			break
		}
		if attempt == reviewMaxAttempts {
			return StepResult{Step: StepReview, Passed: false, Notes: notes,
				Detail: fmt.Sprintf("the reviewer returned no valid report after %d attempts: %v", attempt, validationErr)}, nil
		}
	}

	return evaluateReport(report, files, notes), nil
}

// evaluateReport turns a validated report into the step's verdict,
// deterministically: any error or warning blocks, and so does a changed
// file missing from reviewed_paths. Info findings never block.
func evaluateReport(report Report, files, notes []string) StepResult {
	sr := StepResult{Step: StepReview, Report: &report, Notes: notes}

	var problems []string
	if blocking := report.Blocking(); len(blocking) > 0 {
		problems = append(problems, fmt.Sprintf("%d blocking finding(s)", len(blocking)))
	}
	if missing := uncovered(files, report.ReviewedPaths); len(missing) > 0 {
		problems = append(problems, "incomplete coverage - the reviewer did not report reading: "+strings.Join(missing, ", "))
	}

	sr.Passed = len(problems) == 0
	var detail []string
	if !sr.Passed {
		detail = append(detail, "review blocked: "+strings.Join(problems, "; "))
	}
	if len(report.Findings) > 0 {
		detail = append(detail, FormatFindings(report.Findings))
	}
	if !sr.Passed {
		detail = append(detail, fmt.Sprintf("risk: %s - %s", report.RiskLevel, report.RiskRationale))
	}
	sr.Detail = strings.Join(detail, "\n")
	return sr
}

// changedFiles lists the files changed between baseSHA and targetSHA.
func changedFiles(campPath, baseSHA, targetSHA string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", "-z", "--no-renames", baseSHA+".."+targetSHA)
	cmd.Dir = campPath
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("listing changed files between %s and %s: %w", baseSHA, targetSHA, err)
	}
	var files []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			files = append(files, p)
		}
	}
	return files, nil
}

// runClaude runs one headless claude invocation in campPath through the
// soldier harness's command spec, bounded by timeout.
func runClaude(campPath string, task state.Task, timeout time.Duration) (string, error) {
	spec := soldier.ClaudeCommand(task)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, spec.Command, spec.Args...)
	cmd.Dir = campPath
	// A killed claude may leave children holding the output pipes open;
	// WaitDelay force-closes them so a timeout can never hang here.
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("%w after %s", errTimedOut, timeout)
	}
	if err != nil {
		return "", fmt.Errorf("%w\n%s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// errTimedOut marks a claude invocation cut short by its absolute timeout.
var errTimedOut = errors.New("timed out")

func buildReviewPrompt(in reviewInput) string { return "" }
