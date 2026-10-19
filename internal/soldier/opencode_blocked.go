package soldier

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/state"
)

// OpenCode blocks a soldier on two screens of its own: a permission prompt and
// a question form (the question tool). Neither is Claude Code's AskUserQuestion
// selector, so modal.go's parser does not apply, and answering by typing into
// the pane would be wrong for both: what the pane takes there is a key press.
// The exact screens below were captured from a live OpenCode 2.0.32 TUI under
// herdr (see docs/verification/opencode-soldier-checklist.md). An OpenCode
// soldier started by vexillum runs with --auto, so a permission prompt is rare
// (it needs an `ask` rule that --auto does not auto-approve); a question form
// is the usual one.
//
// What vexillum does with them, on 2.x only (1.x screens were never observed):
//
//   - Permission prompt: "Allow once" is answered with Enter, the default
//     option; Reject with Esc. "Always allow" is never chosen for the general:
//     it may persist a rule, and the decision it stands for is not one a
//     one-line answer can carry.
//   - Question form: the answer is the option's number, or its exact text,
//     and is delivered as arrow-down presses and Enter (the first option is
//     selected when the form opens). "Type your own answer" and any text that
//     is not an option are refused: they open a text field vexillum has no
//     verified way to fill.
//   - Any other screen the pane is blocked on is refused with a message, and
//     nothing is typed into it.
//
// A pane that is not blocked at all (the soldier ended its turn with a
// needs-decision: line) is answered as free text through the normal herdr
// prompt path, like every other harness.

// openCodeBlockKind is what an OpenCode pane that herdr reports as blocked
// is showing.
type openCodeBlockKind int

const (
	openCodeBlockUnknown openCodeBlockKind = iota
	openCodeBlockPermission
	openCodeBlockQuestion
)

// Option labels the permission prompt shows.
const (
	openCodeAllowOnce   = "Allow once"
	openCodeAlwaysAllow = "Always allow"
	openCodeReject      = "Reject"
	// openCodeCustomAnswer is the last option of a question form: it opens a
	// text field instead of submitting.
	openCodeCustomAnswer = "Type your own answer"
)

var (
	// openCodeBarGlyphs are the box-drawing characters OpenCode draws in front
	// of a block's lines.
	openCodeBarGlyphs = " \t┃│┆╎"

	openCodeOptionLine = regexp.MustCompile(`^(\d+)\.\s+(.+?)\s*$`)
	openCodeFormFooter = regexp.MustCompile(`esc dismiss`)
	openCodeFormEnter  = regexp.MustCompile(`enter (?:submit|continue|done|select|confirm)`)
)

// openCodeScreenLines is the plain text of a --source visible --ansi capture,
// one entry per line, without colors or the bar OpenCode draws in front of a
// block.
func openCodeScreenLines(visible string) []string {
	plain := ansiSGR.ReplaceAllString(visible, "")
	lines := strings.Split(strings.ReplaceAll(plain, "\r\n", "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(strings.TrimLeft(l, openCodeBarGlyphs), " \t")
	}
	return lines
}

// parseOpenCodeBlocked classifies the visible pane of an OpenCode soldier
// that herdr reports as blocked, and extracts the question for the general. ok
// is false for a screen that is neither a permission prompt nor a
// single-question form; the Decision is then nil.
func parseOpenCodeBlocked(visible string) (kind openCodeBlockKind, d *state.Decision, ok bool) {
	lines := openCodeScreenLines(visible)

	// The last "Permission required" on the screen is the live one.
	header := -1
	for i, l := range lines {
		if strings.Contains(l, "Permission required") {
			header = i
		}
	}
	if header >= 0 {
		optionsAt := -1
		for i := header + 1; i < len(lines); i++ {
			if strings.Contains(lines[i], openCodeAllowOnce) && strings.Contains(lines[i], openCodeReject) {
				optionsAt = i
				break
			}
		}
		if optionsAt > header {
			var body []string
			for _, l := range lines[header+1 : optionsAt] {
				if l != "" {
					body = append(body, l)
				}
			}
			question := "OpenCode asks for permission"
			if len(body) > 0 {
				question += ": " + truncateRunes(strings.Join(body, " "), 300)
			}
			question += " (answer \"allow once\" or \"reject\"; \"always allow\" is never chosen for you)"
			return openCodeBlockPermission, &state.Decision{
				Question: question,
				Options:  []string{openCodeAllowOnce, openCodeReject},
				AskedAt:  time.Now().UTC(),
				Kind:     state.DecisionKindProse,
			}, true
		}
	}

	// A question form: a "Questions" header, a list of numbered options, and a
	// footer with "esc dismiss". A form of several questions has a tab
	// footer ("⇆ tab") and is not parsed.
	footer := -1
	for i, l := range lines {
		if openCodeFormFooter.MatchString(l) && openCodeFormEnter.MatchString(l) {
			footer = i
		}
	}
	if footer < 0 || strings.Contains(lines[footer], "⇆ tab") {
		return openCodeBlockUnknown, nil, false
	}
	formStart := -1
	for i := footer; i >= 0; i-- {
		if lines[i] == "Questions" {
			formStart = i
			break
		}
	}
	if formStart < 0 {
		return openCodeBlockUnknown, nil, false
	}
	var question []string
	var options []string
	for _, l := range lines[formStart+1 : footer] {
		if m := openCodeOptionLine.FindStringSubmatch(l); m != nil {
			if n, _ := strconv.Atoi(m[1]); n == len(options)+1 {
				options = append(options, m[2])
				continue
			}
		}
		if len(options) == 0 && l != "" {
			question = append(question, l)
		}
	}
	if len(question) == 0 || len(options) == 0 {
		return openCodeBlockUnknown, nil, false
	}
	return openCodeBlockQuestion, &state.Decision{
		Question: strings.Join(question, " "),
		Options:  options,
		AskedAt:  time.Now().UTC(),
		Kind:     state.DecisionKindProse,
	}, true
}

func truncateRunes(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "..."
	}
	return s
}

// resolveOpenCodeBlocked builds the Decision for an OpenCode task that herdr
// reports blocked: the question of the form or permission prompt on screen
// when it is one vexillum knows, else the prose heuristic over the transcript,
// like every other harness. Never nil for a genuinely blocked task.
func resolveOpenCodeBlocked(client herdr.Client, agentName, output string) *state.Decision {
	if visible, err := client.AgentReadVisible(agentName); err == nil {
		if _, d, ok := parseOpenCodeBlocked(visible); ok {
			return d
		}
	}
	return ExtractDecision(output)
}

// answerOpenCodeBlocked is AnswerBlocked for an OpenCode task. handled is false
// when the soldier's pane is not blocked on a screen at all (its live status
// is anything but blocked): the question then sits in the transcript as a
// needs-decision: line, and the caller answers it as free text. When handled
// is true the answer was delivered, or refused, here.
//
// The pane is checked first, every time: typing into an OpenCode pane that is
// not at the prompt it is believed to be at lands in the wrong place (a line
// meant as an answer was once typed into a live TUI as a new prompt).
func answerOpenCodeBlocked(projectRoot string, task state.Task, answer string, client herdr.Client) (result state.Task, handled bool, err error) {
	live, err := client.AgentStatus(task.HerdrAgentName)
	if err != nil {
		if herdr.IsNotFound(err) || herdr.IsNotRunning(err) {
			// A pane that is gone is an interruption, as DismissBlocked
			// records it.
			saved, saveErr := markInterrupted(projectRoot, task.ID, "[vexillum] this soldier's herdr pane is gone - marked interrupted. Use '"+cmdname.Name+" redispatch' instead.")
			if saveErr != nil {
				return task, true, fmt.Errorf("persisting interrupted task (after: %v): %w", err, saveErr)
			}
			return saved, true, fmt.Errorf("the soldier's herdr pane is gone - marked interrupted, use '"+cmdname.Name+" redispatch' instead: %w", err)
		}
		return task, true, fmt.Errorf("reading the soldier's live status before answering: %w", err)
	}
	if MapAgentStatus(live) != state.StatusBlocked {
		return task, false, nil
	}

	refuse := func(why string) (state.Task, bool, error) {
		return task, true, fmt.Errorf("%s; nothing was typed into the pane. Answer it in the pane itself (herdr agent read/attach %s), or use '%s decide %s --dismiss' only if there is nothing to answer", why, task.HerdrAgentName, cmdname.Name, task.ID)
	}
	if task.HarnessMajor != 2 {
		return refuse(fmt.Sprintf("the soldier's pane is blocked on an OpenCode screen, and key answers are only supported on OpenCode 2.x (this task runs %d.x, whose screens were never observed)", task.HarnessMajor))
	}
	visible, err := client.AgentReadVisible(task.HerdrAgentName)
	if err != nil {
		return task, true, fmt.Errorf("reading the soldier's pane before answering: %w", err)
	}
	kind, decision, ok := parseOpenCodeBlocked(visible)
	if !ok {
		return refuse("the soldier's pane is blocked on a screen vexillum does not recognize (not an OpenCode permission prompt or a single-question form)")
	}

	keys, chosen, why := openCodeKeysFor(kind, decision.Options, answer)
	if why != "" {
		return refuse(why)
	}
	if err := client.AgentSendKeys(task.HerdrAgentName, keys...); err != nil {
		return task, true, fmt.Errorf("answering the soldier's pane: %w", err)
	}
	status, werr := client.AgentWait(task.HerdrAgentName, nil, quickSettleTimeoutMS)
	result, err = finishAnswerBlocked(projectRoot, task, answer, chosen, status, werr, client)
	return result, true, err
}

// openCodeKeysFor maps the general's answer to the keys that give it on a
// screen of the given kind, and the option it amounts to (for the amendment).
// why is non-empty when the answer cannot be given safely.
func openCodeKeysFor(kind openCodeBlockKind, options []string, answer string) (keys []string, chosen, why string) {
	a := strings.ToLower(strings.TrimSpace(answer))
	switch kind {
	case openCodeBlockPermission:
		switch a {
		case "allow once", "allow", "yes", "y", "1":
			return []string{"enter"}, openCodeAllowOnce, ""
		case "reject", "deny", "no", "n", "2":
			return []string{"esc"}, openCodeReject, ""
		case "always allow", "always":
			return nil, "", "\"always allow\" is never chosen on your behalf (it may save a permanent rule): answer \"allow once\" or \"reject\", or choose it yourself in the pane"
		}
		return nil, "", fmt.Sprintf("a permission prompt takes \"allow once\" or \"reject\", not %q", answer)
	case openCodeBlockQuestion:
		n, label, ok := resolveModalAnswer(options, answer)
		if !ok {
			return nil, "", fmt.Sprintf("a question form takes the number or the exact text of one of its options (%s), not %q", strings.Join(options, " / "), answer)
		}
		if strings.EqualFold(label, openCodeCustomAnswer) {
			return nil, "", fmt.Sprintf("%q opens a text field, which vexillum has no verified way to fill; pick a listed option, or answer in the pane", openCodeCustomAnswer)
		}
		keys = make([]string, 0, n)
		for i := 1; i < n; i++ {
			keys = append(keys, "down")
		}
		return append(keys, "enter"), label, ""
	}
	return nil, "", "unknown OpenCode screen"
}
