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
		AskedAt:  time.Now().UTC(),
	}
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
