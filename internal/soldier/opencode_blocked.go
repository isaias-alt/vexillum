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
//     selected when the form opens). Any other text is typed into the form's
//     "Type your own answer" field: the option is selected, the pane is
//     checked to show the text field, and the text goes in one key press per
//     character (herdr agent send-keys; a prompt is refused by herdr while
//     the pane is blocked). Only printable ASCII is typed (any other
//     character would be dropped silently), and a bare number that is not an
//     option is refused rather than typed.
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

	plan, why := openCodePlanFor(kind, decision.Options, answer)
	if why != "" {
		return refuse(why)
	}
	if err := client.AgentSendKeys(task.HerdrAgentName, plan.keys...); err != nil {
		return task, true, fmt.Errorf("answering the soldier's pane: %w", err)
	}
	if plan.typed != "" {
		// Selecting "Type your own answer" opens a text field; typing before
		// it is there would land in the wrong place.
		if !waitForTextField(client, task.HerdrAgentName) {
			return task, true, fmt.Errorf("the form's text field did not open after selecting %q, so the text was not typed; the pane may be waiting in the form: answer it there (herdr agent read/attach %s)", openCodeCustomAnswer, task.HerdrAgentName)
		}
		if err := client.AgentSendKeys(task.HerdrAgentName, typedKeys(plan.typed)...); err != nil {
			return task, true, fmt.Errorf("typing the answer into the soldier's pane: %w", err)
		}
	}
	chosen := plan.chosen
	// herdr reports an OpenCode pane's state from the plugin's events, so the
	// pane still reads blocked for a moment after the key press, and a wait
	// for "settled" would return at once with the old state (seen live: the
	// task was recorded blocked with a stale question while the soldier was
	// already working). Wait for it to leave blocked first.
	waitLeavesBlocked(client, task.HerdrAgentName)
	status, werr := waitProbe(client, task.HerdrAgentName)
	result, err = finishAnswerBlocked(projectRoot, task, answer, chosen, status, werr, client)
	return result, true, err
}

// openCodePlan is how an answer is given on an OpenCode screen.
type openCodePlan struct {
	// keys are sent first (logical herdr keys).
	keys []string
	// typed, when set, is text typed into the form's text field once the keys
	// have opened it, followed by Enter.
	typed string
	// chosen is what the answer amounts to, for the amendment.
	chosen string
}

// openCodeTextFieldFooter is the footer a question form shows while its text
// field is open (the list has "esc dismiss").
const openCodeTextFieldFooter = "esc close"

// openCodeTextFieldWindow bounds the wait for the text field to open. A
// variable so tests can shorten it.
var openCodeTextFieldWindow = 5 * time.Second

// waitForTextField polls the visible pane until it shows an open text field.
func waitForTextField(client herdr.Client, name string) bool {
	deadline := time.Now().Add(openCodeTextFieldWindow)
	for {
		if visible, err := client.AgentReadVisible(name); err == nil {
			for _, l := range openCodeScreenLines(visible) {
				if strings.Contains(l, openCodeTextFieldFooter) && strings.Contains(l, "enter submit") {
					return true
				}
			}
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// typedKeys turns text into the herdr keys that type it: one per character,
// a space being "space", then Enter to submit.
func typedKeys(text string) []string {
	keys := make([]string, 0, len(text)+1)
	for _, r := range text {
		if r == ' ' {
			keys = append(keys, "space")
		} else {
			keys = append(keys, string(r))
		}
	}
	return append(keys, "enter")
}

const maxTypedAnswerRunes = 300

// printableASCII reports whether s is non-empty and only printable ASCII,
// the characters herdr's send-keys was seen to type (others are dropped).
func printableASCII(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}

// openCodeLeaveBlockedWindow bounds the wait for a pane to leave blocked after
// a key press. A variable so tests can shorten it.
var openCodeLeaveBlockedWindow = 8 * time.Second

// waitLeavesBlocked polls name's live status until it is anything but blocked
// or openCodeLeaveBlockedWindow passes. A status it cannot read ends the wait:
// the caller's own wait reports what is wrong with the pane.
func waitLeavesBlocked(client herdr.Client, name string) {
	deadline := time.Now().Add(openCodeLeaveBlockedWindow)
	for time.Now().Before(deadline) {
		live, err := client.AgentStatus(name)
		if err != nil || MapAgentStatus(live) != state.StatusBlocked {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// openCodePlanFor maps the general's answer to how it is given on a screen of
// the given kind. why is non-empty when the answer cannot be given safely.
func openCodePlanFor(kind openCodeBlockKind, options []string, answer string) (plan openCodePlan, why string) {
	a := strings.ToLower(strings.TrimSpace(answer))
	switch kind {
	case openCodeBlockPermission:
		switch a {
		case "allow once", "allow", "yes", "y", "1":
			return openCodePlan{keys: []string{"enter"}, chosen: openCodeAllowOnce}, ""
		case "reject", "deny", "no", "n", "2":
			return openCodePlan{keys: []string{"esc"}, chosen: openCodeReject}, ""
		case "always allow", "always":
			return openCodePlan{}, "\"always allow\" is never chosen on your behalf (it may save a permanent rule): answer \"allow once\" or \"reject\", or choose it yourself in the pane"
		}
		return openCodePlan{}, fmt.Sprintf("a permission prompt takes \"allow once\" or \"reject\", not %q", answer)
	case openCodeBlockQuestion:
		downs := func(n int) []string {
			keys := make([]string, 0, n)
			for i := 1; i < n; i++ {
				keys = append(keys, "down")
			}
			return append(keys, "enter")
		}
		custom := len(options) > 0 && strings.EqualFold(options[len(options)-1], openCodeCustomAnswer)
		n, label, ok := resolveModalAnswer(options, answer)
		if ok && !(custom && n == len(options)) {
			return openCodePlan{keys: downs(n), chosen: label}, ""
		}
		text := strings.TrimSpace(answer)
		if ok {
			// The custom option itself, with no text of the answer's own.
			return openCodePlan{}, fmt.Sprintf("%q opens a text field and needs the text to type: answer with the text itself", openCodeCustomAnswer)
		}
		if !custom {
			return openCodePlan{}, fmt.Sprintf("a question form takes the number or the exact text of one of its options (%s), not %q", strings.Join(options, " / "), answer)
		}
		if _, err := strconv.Atoi(text); err == nil {
			return openCodePlan{}, fmt.Sprintf("%q is a number that is not one of the options (%s); it is not typed as text", text, strings.Join(options, " / "))
		}
		if !printableASCII(text) || len([]rune(text)) > maxTypedAnswerRunes {
			return openCodePlan{}, fmt.Sprintf("a typed answer must be printable ASCII of at most %d characters (herdr's key presses drop anything else)", maxTypedAnswerRunes)
		}
		return openCodePlan{keys: downs(len(options)), typed: text, chosen: text}, ""
	}
	return openCodePlan{}, "unknown OpenCode screen"
}
