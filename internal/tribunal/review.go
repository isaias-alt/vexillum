package tribunal

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

// reviewModel and reviewEffort are fixed, not a per-call judgment call -
// same reasoning the product AGENTS.md's "Choosing a model and effort"
// gives for any scout whose question requires judgment rather than
// verifiable facts: a code review is exactly that kind of question, and
// the commander already never picks a soldier's model per-invocation for
// this step (ADR-05 reserves that judgment for dispatch, not tribunal).
const (
	reviewModel  = "sonnet"
	reviewEffort = "medium"
)

// VerdictPrefix is the line the review soldier's prompt requires its
// response to end with - the only thing runReview actually parses out of
// an otherwise free-form review.
const VerdictPrefix = "TRIBUNAL_VERDICT:"

// runReview dispatches a headless review soldier (internal/soldier's
// Layer 3 path - ClaudeCommand's CommandSpec, run directly with
// os/exec rather than through a herdr pane) against campPath's diff since
// base, and blocks on anything short of an explicit pass. Unlike
// dispatch's interactive soldiers, this runs synchronously inside
// tribunal.Run: there is no pane to poll and no sentinel watching it,
// the same "ship doesn't return until the whole pipeline has" property
// the rest of tribunal has.
func runReview(campPath, base string) (StepResult, error) {
	diff, err := gitDiff(campPath, base, "")
	if err != nil {
		return StepResult{}, err
	}
	if strings.TrimSpace(diff) == "" {
		return StepResult{Step: StepReview, Passed: true, Detail: "no changes to review"}, nil
	}

	task := state.Task{Prompt: buildReviewPrompt(diff), Model: reviewModel, Effort: reviewEffort}
	spec := soldier.ClaudeCommand(task)
	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Dir = campPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return StepResult{}, fmt.Errorf("running the review soldier: %w\n%s", err, strings.TrimSpace(string(out)))
	}

	passed, detail := parseVerdict(string(out))
	return StepResult{Step: StepReview, Passed: passed, Detail: detail}, nil
}

// buildReviewPrompt is the review soldier's entire prompt: the diff to
// review plus the fixed verdict contract runReview's parseVerdict expects
// back. Scoped to correctness bugs only, not style or simplification -
// this step exists to keep an actual defect out of a real PR, not to
// duplicate what lint and human PR review already do.
func buildReviewPrompt(diff string) string {
	return fmt.Sprintf(`Review the following diff for correctness bugs only - not style, not
missing tests, not simplification opportunities. This is the final gate
before a real pull request opens: a false PASS lets a bug ship, a false
FAIL blocks working code, so only flag something you are confident is an
actual defect, and explain briefly why.

%s

End your response with exactly one line, in this exact form and nothing
after it:

%s PASS

or, if you found a blocking defect:

%s FAIL: <one-line reason>`, diff, VerdictPrefix, VerdictPrefix)
}

// parseVerdict extracts runReview's pass/fail outcome from the review
// soldier's raw output - the last line starting with VerdictPrefix, since
// a soldier's own reasoning may otherwise mention the word "PASS" or
// "FAIL" in passing. No recognizable verdict line is treated as a
// failure, not a pass: an ambiguous review result should never let a
// mission through by default.
func parseVerdict(output string) (passed bool, detail string) {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, VerdictPrefix) {
			continue
		}
		verdict := strings.TrimSpace(strings.TrimPrefix(line, VerdictPrefix))
		switch {
		case verdict == "PASS":
			return true, ""
		case strings.HasPrefix(verdict, "FAIL"):
			reason := strings.TrimSpace(strings.TrimPrefix(verdict, "FAIL"))
			reason = strings.TrimSpace(strings.TrimPrefix(reason, ":"))
			if reason == "" {
				reason = "review found a blocking issue"
			}
			return false, reason
		default:
			return false, "review soldier returned an unrecognized verdict: " + verdict
		}
	}
	return false, "review soldier's output ended without a " + VerdictPrefix + " line"
}
