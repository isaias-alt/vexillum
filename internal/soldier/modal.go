package soldier

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/state"
)

// askUserQuestionFooter is the exact footer Claude Code's AskUserQuestion
// selector renders - confirmed live against herdr 0.9.3 / Claude Code CLI
// v2.1.280 (the durable decision record's capture report). It's the
// strongest, cheapest anchor for "this specific Blocked shape is the
// AskUserQuestion modal", as opposed to some other Blocked shape: the
// workspace-trust dialog's footer reads "Enter to confirm ... Esc to
// cancel" - no "select", no "navigate", no arrow glyph - and a Bash
// approval prompt's differs too.
const askUserQuestionFooter = "Enter to select"

// Claude Code always appends these two options to an AskUserQuestion
// modal itself, never chosen by the model - a freeform text entry and a
// chat escape hatch, always last. AnswerBlocked treats a resolved answer
// that targets optionTypeSomething specially (falls back to plain text
// submission instead of a digit key, since selecting it opens a real text
// field rather than submitting directly); optionChatAboutThis is answered
// the same as any other rendered option, its own digit.
const (
	optionTypeSomething = "Type something."
	optionChatAboutThis = "Chat about this"
)

// ansiSGR strips ANSI SGR (color/style) escape sequences from a
// --source visible --ansi capture, leaving the plain rendered text. The
// parser only needs the text layout (header, question, options, footer) -
// never the colors themselves, since Decision.SelectedIndex was dropped
// from the design: digit-select answers by the option's rendered number,
// not by what's currently highlighted (durable decision record capture
// report, point 6).
var ansiSGR = regexp.MustCompile("\x1b\\[[0-9;]*m")

// modalOptionLine matches a single rendered AskUserQuestion option line,
// ANSI already stripped: an optional "❯ " selection marker, then
// "<N>. <label>".
var modalOptionLine = regexp.MustCompile(`^\s*(?:❯\s*)?(\d+)\.\s+(.+?)\s*$`)

// ErrNotAskUserQuestionModal means a --source visible --ansi capture of a
// Blocked task's pane does not confidently look like Claude Code's
// AskUserQuestion selector - some other Blocked shape (the workspace-trust
// dialog, a Bash approval prompt, a future dialog vexillum doesn't know
// about yet). Callers (ResolveBlockedDecision) must fall back to the
// general prose heuristic (ExtractDecision) rather than accept an
// ambiguous or partially-parsed modal Decision - fail loud, the same
// principle ExtractDecision itself follows for a transcript it can't make
// sense of.
var ErrNotAskUserQuestionModal = errors.New("visible pane capture is not an AskUserQuestion modal")

// ParseAskUserQuestionModal parses visible - a --source visible --ansi
// capture of a Blocked task's pane - as Claude Code's AskUserQuestion
// selector, per the exact rendering captured live (durable decision
// record capture report): a black-on-lavender "☐ <label>" header line, a
// bold-white question line, a numbered option list, and a footer reading
// "Enter to select · ↑/↓ to navigate · Esc to cancel". Options is every
// rendered option in order, numbered exactly as displayed - including the
// two Claude-Code-injected ones (optionTypeSomething, optionChatAboutThis,
// always last) - so Options[i] is always answerable by sending key
// "i+1" (see AnswerBlocked), with no separate index bookkeeping.
//
// Returns ErrNotAskUserQuestionModal if the footer isn't found at all
// (this isn't the AskUserQuestion modal), or if the footer is found but
// no question/options can be read from around it (an unexpected
// rendering change) - never a partially-parsed or ambiguous Decision.
func ParseAskUserQuestionModal(visible string) (*state.Decision, error) {
	plain := ansiSGR.ReplaceAllString(visible, "")
	lines := strings.Split(strings.ReplaceAll(plain, "\r\n", "\n"), "\n")

	footerIdx := -1
	for i, line := range lines {
		if strings.Contains(line, askUserQuestionFooter) {
			footerIdx = i
			break
		}
	}
	if footerIdx == -1 {
		return nil, ErrNotAskUserQuestionModal
	}

	var question string
	var options []string
	for i := 0; i < footerIdx; i++ {
		if m := modalOptionLine.FindStringSubmatch(lines[i]); m != nil {
			options = append(options, strings.TrimSpace(m[2]))
			continue
		}
		trimmed := strings.TrimSpace(lines[i])
		if question == "" && trimmed != "" && !strings.HasPrefix(trimmed, "☐") && !isRuleLine(trimmed) {
			question = trimmed
		}
	}

	if question == "" || len(options) == 0 {
		return nil, fmt.Errorf("%w: found the footer but couldn't read a question and options above it", ErrNotAskUserQuestionModal)
	}

	return &state.Decision{
		Question: question,
		Options:  options,
		Kind:     state.DecisionKindModal,
		AskedAt:  time.Now().UTC(),
	}, nil
}

// isRuleLine reports whether s is one of the modal's full-width "─" rule
// lines (the one before the header box, or the one before the two
// auto-injected options) - structural chrome, never the question.
func isRuleLine(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r != '─' {
			return false
		}
	}
	return true
}

// ResolveBlockedDecision builds the Decision for a task that just settled
// Blocked because herdr's own classifier caught it (RunInHerdr and
// internal/sentinel's settleTransition are the only two places that
// happens) - as opposed to the needs-decision: prose signal
// (ExtractNeedsDecisionSignal), which forces a Blocked transition herdr's
// classifier never makes on its own for plain prose. Tries the
// AskUserQuestion-modal parser first, against a fresh
// --source visible --ansi capture (the modal's actual rendering - the one
// Blocked shape herdr's classifier reliably catches, per the durable
// decision record's capture report); falls back to the general prose
// heuristic (ExtractDecision, over the already-captured scrollback
// transcript) for any other Blocked shape the modal parser doesn't
// recognize (a Bash approval prompt, some future dialog) - never nil for
// a genuinely blocked task, the same principle ExtractDecision itself
// follows.
func ResolveBlockedDecision(client herdr.Client, agentName, output string) *state.Decision {
	if visible, err := client.AgentReadVisible(agentName); err == nil {
		if d, err := ParseAskUserQuestionModal(visible); err == nil {
			return d
		}
	}
	return ExtractDecision(output)
}

// resolveModalAnswer maps answer against a modal Decision's rendered
// Options - a 1-based index, or the option's exact text (trimmed,
// case-insensitive) - to that option's rendered number and label. Reports
// ok=false if answer doesn't confidently match anything, so the caller
// falls back to plain text submission rather than guess at a key press.
func resolveModalAnswer(options []string, answer string) (optionNumber int, label string, ok bool) {
	trimmed := strings.TrimSpace(answer)
	if n, err := strconv.Atoi(trimmed); err == nil && n >= 1 && n <= len(options) {
		return n, options[n-1], true
	}
	for i, opt := range options {
		if strings.EqualFold(strings.TrimSpace(opt), trimmed) {
			return i + 1, opt, true
		}
	}
	return 0, "", false
}
