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
// a plain-prose question that needs the general's answer, written as a
// structured status line, never inferred from free-form language. Unlike
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
// record's design report).
//
// Only text after the end of the dispatched prompt counts (see
// afterDispatchPrompt): the pane echoes that prompt, and with it the
// template line "needs-decision: <a one-line summary ...>" that
// needsDecisionInstructions teaches, which is documentation, not a
// question. A placeholder (angle-bracket text such as the template's own,
// see isNeedsDecisionPlaceholder) never counts either, even when it shows
// up after the prompt. Of what remains only the last matching line counts,
// in case an earlier one sits in unrelated context further up the
// transcript (a past turn, quoted text). Returns false, nil if no such
// line is present anywhere - callers must never invent a Decision when
// there isn't one.
func ExtractNeedsDecisionSignal(transcript string) (*state.Decision, bool) {
	matches := needsDecisionPattern.FindAllStringSubmatch(afterDispatchPrompt(transcript), -1)
	for i := len(matches) - 1; i >= 0; i-- {
		question := strings.TrimSpace(matches[i][1])
		if question == "" || isNeedsDecisionPlaceholder(question) {
			continue
		}
		return &state.Decision{
			Question: question,
			Kind:     state.DecisionKindProse,
			AskedAt:  time.Now().UTC(),
		}, true
	}
	return nil, false
}

// FinalTurnNeedsDecision is ExtractNeedsDecisionSignal for a task that may
// have been asked questions before: it additionally ignores a line whose
// question is the one task's own Decision already records as answered or
// dismissed. The pane's scrollback keeps an answered question's line, so
// without this a soldier that carried on after "vx decide" (or after
// "vx decide --dismiss") would be blocked again by its own old line the
// next time it went idle.
func FinalTurnNeedsDecision(task state.Task, transcript string) (*state.Decision, bool) {
	d, found := ExtractNeedsDecisionSignal(transcript)
	if !found {
		return nil, false
	}
	if prev := task.Decision; prev != nil && !prev.AnsweredAt.IsZero() && prev.Question == d.Question {
		return nil, false
	}
	return d, true
}

// afterDispatchPrompt returns the part of transcript after the end of the
// last copy of the dispatched prompt it contains - the one that ends with
// needsDecisionTrailer, the final sentence every soldier prompt gets. A
// transcript without it (the prompt scrolled out of the captured window)
// is returned whole: then none of the prompt's own lines can be in it
// either, apart from the template line isNeedsDecisionPlaceholder covers.
func afterDispatchPrompt(transcript string) string {
	i := strings.LastIndex(transcript, needsDecisionTrailer)
	if i < 0 {
		return transcript
	}
	return transcript[i+len(needsDecisionTrailer):]
}

// placeholderPattern matches text that is entirely one angle-bracket
// group ("<a one-line summary ...>", "<question>"): a fill-in-the-blank
// marker from documentation, never something a soldier would actually ask.
var placeholderPattern = regexp.MustCompile(`^<[^<>]*>$`)

// isNeedsDecisionPlaceholder reports whether question, the text after
// "needs-decision:", is the template's own placeholder or another bare
// angle-bracket placeholder rather than a real question.
func isNeedsDecisionPlaceholder(question string) bool {
	question = strings.TrimSpace(question)
	return question == needsDecisionPlaceholder || placeholderPattern.MatchString(question)
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
