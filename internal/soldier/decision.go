package soldier

import (
	"regexp"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/state"
)

// optionLinePattern matches a single bulleted or numbered line ("- foo",
// "* foo", "1. foo", "2) foo") and captures its text.
var optionLinePattern = regexp.MustCompile(`^\s*(?:[-*\x{2022}]|\d+[.):])\s+(.+?)\s*$`)

// needsDecisionPattern matches the soldier-authored end-of-turn line
// documented in needsDecisionInstructions: a strict, syntactic marker for
// a plain-prose question that needs the general's answer, adapted from
// upstream-tool's own "needs-decision:" status-line convention. Unlike
// optionLinePattern (a fuzzy heuristic over arbitrary prose), this is the
// only thing that can turn a plain-prose question into a genuine
// StatusBlocked transition at all - herdr's own classifier never does,
// only Claude Code's AskUserQuestion modal reliably reaches it (see the
// durable decision record's design report).
var needsDecisionPattern = regexp.MustCompile(`(?im)^\s*needs-decision:\s*(.+?)\s*$`)

// ExtractDecision builds a state.Decision - the actual commander-facing
// question, and any options offered alongside it - out of transcript, the
// raw text captured at the moment a task's status settles to
// state.StatusBlocked (see RunInHerdr and internal/sentinel's
// settleTransition, the only two places that transition happens). It's a
// heuristic, not a parser: herdr's "recent-unwrapped" transcript ends with
// whatever the agent most recently emitted, so the last paragraph is taken
// as the question, and any numbered/bulleted lines within it as its
// options. The result is always real, structured text on its own fields -
// never a pointer back into transcript prose for a caller to re-parse
// later.
//
// Returns nil only if transcript has no content at all to extract a
// question from (e.g. AgentRead itself failed and left Output empty) - a
// genuinely blocked task otherwise always gets a Decision.
func ExtractDecision(transcript string) *state.Decision {
	lines := strings.Split(strings.ReplaceAll(transcript, "\r\n", "\n"), "\n")

	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	if end == 0 {
		return nil
	}

	start := paragraphStart(lines, end)
	question, options := splitQuestionAndOptions(lines[start:end])

	if len(question) == 0 {
		// The last paragraph was entirely a list - the question text (if
		// any) sits in the paragraph before it, not captured yet.
		prevEnd := start
		for prevEnd > 0 && strings.TrimSpace(lines[prevEnd-1]) == "" {
			prevEnd--
		}
		prevStart := paragraphStart(lines, prevEnd)
		prevQuestion, _ := splitQuestionAndOptions(lines[prevStart:prevEnd])
		question = append(question, prevQuestion...)
	}

	if len(question) == 0 {
		if len(options) == 0 {
			return nil
		}
		// Nothing recognizable as prose anywhere nearby - fall back to
		// the option text itself rather than reporting no question at all
		// for a task that's genuinely Blocked.
		question = options
		options = nil
	}

	return &state.Decision{
		Question: strings.Join(question, " "),
		Options:  options,
		Kind:     state.DecisionKindProse,
		AskedAt:  time.Now().UTC(),
	}
}

// ExtractNeedsDecisionSignal reports whether transcript's soldier-authored
// output contains a needs-decision: line (see needsDecisionInstructions)
// and, if so, the Decision built from it. Unlike ExtractDecision (a fuzzy
// heuristic over arbitrary prose), this is a strict syntactic match on an
// exact, documented format - the only thing that can turn a plain-prose
// question into a real StatusBlocked transition, since herdr's own
// classifier never does for plain prose (see the durable decision
// record's design report). Only the last matching line counts, in case an
// earlier one sits in unrelated context further up the transcript (a past
// turn, quoted text). Returns false, nil if no such line is present
// anywhere - callers must never invent a Decision when there isn't one.
func ExtractNeedsDecisionSignal(transcript string) (*state.Decision, bool) {
	matches := needsDecisionPattern.FindAllStringSubmatch(transcript, -1)
	if len(matches) == 0 {
		return nil, false
	}
	question := strings.TrimSpace(matches[len(matches)-1][1])
	if question == "" {
		return nil, false
	}
	return &state.Decision{
		Question: question,
		Kind:     state.DecisionKindProse,
		AskedAt:  time.Now().UTC(),
	}, true
}

// paragraphStart walks lines backward from end (exclusive) to the start of
// its enclosing paragraph - the first blank line, or the start of lines.
func paragraphStart(lines []string, end int) int {
	start := end
	for start > 0 && strings.TrimSpace(lines[start-1]) != "" {
		start--
	}
	return start
}

// splitQuestionAndOptions separates paragraph into prose lines (the
// question) and recognized bulleted/numbered lines (options), each
// preserving its original order.
func splitQuestionAndOptions(paragraph []string) (question, options []string) {
	for _, line := range paragraph {
		if m := optionLinePattern.FindStringSubmatch(line); m != nil {
			options = append(options, strings.TrimSpace(m[1]))
			continue
		}
		if t := strings.TrimSpace(line); t != "" {
			question = append(question, t)
		}
	}
	return question, options
}
