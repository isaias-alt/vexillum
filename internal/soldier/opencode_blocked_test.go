package soldier_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

// The two screens below are the exact text `herdr agent read` returned for a
// live OpenCode 2.0.32 TUI blocked on a permission prompt (a shell rule set to
// ask) and on the question tool. They are the spike's captures, kept verbatim.
const ocPermissionScreen = `  ┃  △ Permission required
  ┃
  ┃  $ echo permtest > perm.txt && cat perm.txt
  ┃
  ┃   Allow once   Always allow   Reject  ctrl+f fullscreen  ⇆ select  enter confirm
`

const ocQuestionScreen = `  → Asked 1 question

  ┃  Questions
  ┃
  ┃  Which color do you prefer?
  ┃
  ┃  1. Red
  ┃     Prefer the color red
  ┃  2. Blue
  ┃     Prefer the color blue
  ┃  3. Type your own answer
  ┃
  ┃  ↑↓ select  enter submit  esc dismiss
`

func TestParseOpenCodeBlocked_PermissionPrompt(t *testing.T) {
	kind, question, options, ok := soldier.ParseOpenCodeBlocked(ocPermissionScreen)
	if !ok || kind != "permission" {
		t.Fatalf("kind %q ok %v", kind, ok)
	}
	if !strings.Contains(question, "echo permtest > perm.txt && cat perm.txt") || !strings.Contains(question, "never chosen") {
		t.Errorf("question = %q", question)
	}
	if !slices.Equal(options, []string{"Allow once", "Reject"}) {
		t.Errorf("options = %v: Always allow must not be offered", options)
	}
}

func TestParseOpenCodeBlocked_QuestionForm(t *testing.T) {
	kind, question, options, ok := soldier.ParseOpenCodeBlocked(ocQuestionScreen)
	if !ok || kind != "question" {
		t.Fatalf("kind %q ok %v", kind, ok)
	}
	if question != "Which color do you prefer?" {
		t.Errorf("question = %q", question)
	}
	if !slices.Equal(options, []string{"Red", "Blue", "Type your own answer"}) {
		t.Errorf("options = %v", options)
	}
}

func TestParseOpenCodeBlocked_AnsiAndStaleScreens(t *testing.T) {
	// ANSI color codes around the text do not matter.
	colored := strings.ReplaceAll(ocQuestionScreen, "Which color", "\x1b[1mWhich color\x1b[0m")
	if _, q, _, ok := soldier.ParseOpenCodeBlocked(colored); !ok || q != "Which color do you prefer?" {
		t.Errorf("ansi: ok %v question %q", ok, q)
	}
	// The scrollback above the live form may hold an older one.
	two := ocQuestionScreen + "\n" + strings.ReplaceAll(strings.ReplaceAll(ocQuestionScreen, "Which color do you prefer?", "Which size?"), "Red", "Small")
	if _, q, opts, ok := soldier.ParseOpenCodeBlocked(two); !ok || q != "Which size?" || opts[0] != "Small" {
		t.Errorf("two forms: ok %v question %q options %v, want the last one", ok, q, opts)
	}
}

func TestParseOpenCodeBlocked_UnknownScreens(t *testing.T) {
	for name, screen := range map[string]string{
		"empty":           "",
		"idle composer":   "  ┃  Ask anything...\n  ┃  Build · DeepSeek V4.1 Flash Free\n",
		"a claude modal":  "Which database?\n1. Postgres\n2. SQLite\nEnter to select · ↑/↓ to navigate · Esc to cancel",
		"several forms":   strings.Replace(ocQuestionScreen, "↑↓ select  enter submit", "⇆ tab  ↑↓ select  enter continue", 1),
		"form no header":  strings.Replace(ocQuestionScreen, "Questions", "Something else", 1),
		"permission cut":  "  ┃  △ Permission required\n  ┃  $ ls\n",
		"form no options": "  ┃  Questions\n  ┃  Why?\n  ┃  ↑↓ select  enter submit  esc dismiss\n",
	} {
		t.Run(name, func(t *testing.T) {
			if kind, _, _, ok := soldier.ParseOpenCodeBlocked(screen); ok {
				t.Errorf("parsed as %q, want an unrecognized screen", kind)
			}
		})
	}
}

func TestOpenCodePlanFor(t *testing.T) {
	form := []string{"Red", "Blue", "Type your own answer"}
	noCustom := []string{"Red", "Blue"}
	cases := []struct {
		name       string
		kind       string
		options    []string
		answer     string
		wantKeys   []string
		wantTyped  string
		wantChosen string
		wantWhy    string
	}{
		{"allow once", "permission", nil, "allow once", []string{"enter"}, "", "Allow once", ""},
		{"allow", "permission", nil, "Allow", []string{"enter"}, "", "Allow once", ""},
		{"reject", "permission", nil, " reject ", []string{"esc"}, "", "Reject", ""},
		{"always allow is never sent", "permission", nil, "always allow", nil, "", "", "never chosen"},
		{"nonsense on a permission prompt", "permission", nil, "maybe later", nil, "", "", "allow once"},
		{"first option", "question", form, "1", []string{"enter"}, "", "Red", ""},
		{"second by number", "question", form, "2", []string{"down", "enter"}, "", "Blue", ""},
		{"second by text", "question", form, "blue", []string{"down", "enter"}, "", "Blue", ""},
		{"the custom option alone has no text to type", "question", form, "3", nil, "", "", "needs the text to type"},
		{"the custom option by its label alone", "question", form, "Type your own answer", nil, "", "", "needs the text to type"},
		{"free text goes into the custom field", "question", form, "green, please", []string{"down", "down", "enter"}, "green, please", "green, please", ""},
		{"a number that is not an option is not typed", "question", form, "9", nil, "", "", "not one of the options"},
		{"non-ASCII is refused", "question", form, "vert é", nil, "", "", "printable ASCII"},
		{"a form with no text field takes only its options", "question", noCustom, "green", nil, "", "", "number or the exact text"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			keys, typed, chosen, why := soldier.OpenCodePlanFor(c.kind, c.options, c.answer)
			if !slices.Equal(keys, c.wantKeys) || typed != c.wantTyped || chosen != c.wantChosen {
				t.Errorf("keys %v typed %q chosen %q, want %v %q %q", keys, typed, chosen, c.wantKeys, c.wantTyped, c.wantChosen)
			}
			if c.wantWhy == "" && why != "" || c.wantWhy != "" && !strings.Contains(why, c.wantWhy) {
				t.Errorf("why = %q, want it to mention %q", why, c.wantWhy)
			}
		})
	}
}

func TestResolveBlockedDecisionFor_OpenCodeReadsItsOwnScreens(t *testing.T) {
	client := &fakeHerdr{readVisibleOutput: ocQuestionScreen}
	d := soldier.ResolveBlockedDecisionFor("opencode", client, "a", "some transcript")
	if d == nil || d.Question != "Which color do you prefer?" || len(d.Options) != 3 || d.Kind != state.DecisionKindProse {
		t.Errorf("question form: %+v", d)
	}

	// Anything else falls back to the prose heuristic, never to Claude's modal
	// parser, even when the pane happens to render like Claude's selector.
	client = &fakeHerdr{readVisibleOutput: askUserQuestionCapture("File name", "Which database?", []string{"Postgres", "SQLite"})}
	d = soldier.ResolveBlockedDecisionFor("opencode", client, "a", "Which database should this use?\n")
	if d == nil || d.Kind != state.DecisionKindProse || d.Question != "Which database should this use?" {
		t.Errorf("fallback: %+v", d)
	}
}

func newOpenCodeBlockedTask(t *testing.T, projectRoot string, major int) state.Task {
	t.Helper()
	task := newBlockedTask(t, projectRoot, "vx-refactor-the-auth-module")
	task.Harness = "opencode"
	task.HarnessMajor = major
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatal(err)
	}
	return task
}

// A V2 soldier blocked on a question form is answered with arrow keys and
// Enter, after looking at the pane, and nothing is typed.
func TestAnswerBlocked_OpenCodeQuestionFormIsAnsweredWithKeys(t *testing.T) {
	defer soldier.SetOpenCodeLeaveBlockedWindow(50 * time.Millisecond)()
	home := t.TempDir()
	root := testProjectRoot(t, home)
	task := newOpenCodeBlockedTask(t, root, 2)
	client := &fakeHerdr{promptStatus: "blocked", readVisibleOutput: ocQuestionScreen, waitStatus: "done", readOutput: "Noted. Blue."}

	got, err := soldier.AnswerBlocked(root, task, "2", client)
	if err != nil {
		t.Fatalf("AnswerBlocked: %v", err)
	}
	if len(client.sendKeysCalls) != 1 || strings.Join(client.sendKeysCalls[0], ",") != "down,enter" {
		t.Errorf("keys = %v, want down,enter", client.sendKeysCalls)
	}
	if len(client.promptCalls) != 0 {
		t.Errorf("text was typed into the pane: %v", client.promptCalls)
	}
	if got.Status != state.StatusDone {
		t.Errorf("status = %s, want done", got.Status)
	}
	if len(got.Amendments) != 1 || !strings.Contains(got.Amendments[0].Text, "Blue") {
		t.Errorf("the amendment should name the option, got %+v", got.Amendments)
	}
}

func TestAnswerBlocked_OpenCodePermissionPrompt(t *testing.T) {
	defer soldier.SetOpenCodeLeaveBlockedWindow(50 * time.Millisecond)()
	home := t.TempDir()
	root := testProjectRoot(t, home)
	task := newOpenCodeBlockedTask(t, root, 2)

	client := &fakeHerdr{promptStatus: "blocked", readVisibleOutput: ocPermissionScreen, waitStatus: "done", readOutput: "ok"}
	if _, err := soldier.AnswerBlocked(root, task, "allow once", client); err != nil {
		t.Fatalf("allow once: %v", err)
	}
	if len(client.sendKeysCalls) != 1 || strings.Join(client.sendKeysCalls[0], ",") != "enter" {
		t.Errorf("keys = %v, want enter", client.sendKeysCalls)
	}

	// "Always allow" is refused, with nothing sent.
	client = &fakeHerdr{promptStatus: "blocked", readVisibleOutput: ocPermissionScreen}
	_, err := soldier.AnswerBlocked(root, task, "always allow", client)
	if err == nil || !strings.Contains(err.Error(), "never chosen") {
		t.Fatalf("always allow: error = %v", err)
	}
	if len(client.sendKeysCalls) != 0 || len(client.promptCalls) != 0 {
		t.Errorf("something was sent: keys %v prompts %v", client.sendKeysCalls, client.promptCalls)
	}
}

// A pane blocked on a screen vexillum does not know gets nothing typed into it,
// and neither does a 1.x pane (its screens were never observed).
func TestAnswerBlocked_OpenCodeRefusesWhatItCannotAnswerSafely(t *testing.T) {
	home := t.TempDir()
	root := testProjectRoot(t, home)

	for name, c := range map[string]struct {
		major   int
		screen  string
		answer  string
		wantErr string
	}{
		"unknown screen on 2.x":          {2, "something new\nPress any key", "yes", "does not recognize"},
		"a number that is not an option": {2, ocQuestionScreen, "9", "not one of the options"},
		"the custom option with no text": {2, ocQuestionScreen, "3", "needs the text to type"},
		"any blocked screen on 1.x":      {1, ocQuestionScreen, "1", "only supported on OpenCode 2.x"},
	} {
		t.Run(name, func(t *testing.T) {
			task := newOpenCodeBlockedTask(t, root, c.major)
			client := &fakeHerdr{promptStatus: "blocked", readVisibleOutput: c.screen}
			got, err := soldier.AnswerBlocked(root, task, c.answer, client)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) || !strings.Contains(err.Error(), "nothing was typed") {
				t.Fatalf("error = %v, want one mentioning %q and that nothing was typed", err, c.wantErr)
			}
			if len(client.sendKeysCalls) != 0 || len(client.promptCalls) != 0 {
				t.Errorf("something was sent: keys %v prompts %v", client.sendKeysCalls, client.promptCalls)
			}
			if got.Status != state.StatusBlocked {
				t.Errorf("status = %s: a refused answer leaves the task blocked", got.Status)
			}
		})
	}
}

// A task marked blocked by a needs-decision: line has an idle pane: that is
// answered as free text, like every other harness, never as keys.
func TestAnswerBlocked_OpenCodeIdlePaneTakesFreeText(t *testing.T) {
	home := t.TempDir()
	root := testProjectRoot(t, home)
	task := newOpenCodeBlockedTask(t, root, 2)
	client := &fakeHerdr{promptStatus: "done", readVisibleOutput: ocQuestionScreen, readOutput: "done"}

	got, err := soldier.AnswerBlocked(root, task, "Use Auth0", client)
	if err != nil {
		t.Fatalf("AnswerBlocked: %v", err)
	}
	if len(client.sendKeysCalls) != 0 {
		t.Errorf("keys were sent to an idle pane: %v", client.sendKeysCalls)
	}
	if len(client.promptCalls) != 1 || client.promptCalls[0] != "Use Auth0" {
		t.Errorf("prompts = %v", client.promptCalls)
	}
	if got.Status != state.StatusDone {
		t.Errorf("status = %s", got.Status)
	}
}

type goneHerdr struct{ *fakeHerdr }

func (g goneHerdr) AgentStatus(string) (string, error) {
	return "", &herdr.APIError{Code: "agent_not_found", Message: "no such agent"}
}

func TestAnswerBlocked_OpenCodePaneGoneMarksInterrupted(t *testing.T) {
	home := t.TempDir()
	root := testProjectRoot(t, home)
	task := newOpenCodeBlockedTask(t, root, 2)
	client := goneHerdr{&fakeHerdr{}}

	got, err := soldier.AnswerBlocked(root, task, "1", client)
	if err == nil || !strings.Contains(err.Error(), "redispatch") {
		t.Fatalf("error = %v", err)
	}
	if got.Status != state.StatusInterrupted {
		t.Errorf("status = %s, want interrupted", got.Status)
	}
	if len(client.sendKeysCalls) != 0 || len(client.promptCalls) != 0 {
		t.Error("something was sent to a pane that is gone")
	}
}

// scriptedStatus reports a pane's live status from a script, one entry per
// call, repeating the last: the pane of an answered OpenCode soldier still
// reads blocked for a moment after the key press.
type scriptedStatus struct {
	*fakeHerdr
	script []string
	calls  int
	waited int // AgentStatus calls made before AgentWait was first called
}

func (s *scriptedStatus) AgentStatus(string) (string, error) {
	i := min(s.calls, len(s.script)-1)
	s.calls++
	return s.script[i], nil
}

func (s *scriptedStatus) AgentWait(name string, until []string, timeoutMS int) (string, error) {
	if s.waited == 0 {
		s.waited = s.calls
	}
	return s.fakeHerdr.AgentWait(name, until, timeoutMS)
}

// The settle after an answer is waited for only once the pane has left
// blocked: a wait that started while it still read blocked would return at
// once with the old state and record the task blocked on a question already
// answered (seen live on 2.0.32).
func TestAnswerBlocked_OpenCodeWaitsForThePaneToLeaveBlocked(t *testing.T) {
	home := t.TempDir()
	root := testProjectRoot(t, home)
	task := newOpenCodeBlockedTask(t, root, 2)
	client := &scriptedStatus{
		fakeHerdr: &fakeHerdr{readVisibleOutput: ocQuestionScreen, waitStatus: "done", readOutput: "Noted. Blue."},
		// 1: the pane check, 2-3: still blocked after the key press, 4: working.
		script: []string{"blocked", "blocked", "blocked", "working"},
	}

	got, err := soldier.AnswerBlocked(root, task, "2", client)
	if err != nil {
		t.Fatalf("AnswerBlocked: %v", err)
	}
	if client.waited < 4 {
		t.Errorf("the settle was waited for after %d status call(s), want it to follow the pane leaving blocked (4)", client.waited)
	}
	if got.Status != state.StatusDone {
		t.Errorf("status = %s, want done", got.Status)
	}
}

// visibleSequence returns the visible pane from a script, one per call,
// repeating the last: the form first, then its text field once opened.
type visibleSequence struct {
	*fakeHerdr
	script []string
	calls  int
}

func (v *visibleSequence) AgentReadVisible(string) (string, error) {
	i := min(v.calls, len(v.script)-1)
	v.calls++
	return v.script[i], nil
}

const ocTextFieldScreen = `  ┃  Questions
  ┃
  ┃  Which color do you prefer?
  ┃
  ┃  1. Red
  ┃     Prefer the color red
  ┃  2. Blue
  ┃     Prefer the color blue
  ┃  3. 
  ┃
  ┃  enter submit  esc close
`

// Text that is not an option goes into the form's own text field: the custom
// option is selected, the pane is seen to show the text field, and only then
// is the text typed, one key per character, and submitted.
func TestAnswerBlocked_OpenCodeTypesFreeTextIntoTheTextField(t *testing.T) {
	defer soldier.SetOpenCodeLeaveBlockedWindow(50 * time.Millisecond)()
	home := t.TempDir()
	root := testProjectRoot(t, home)
	task := newOpenCodeBlockedTask(t, root, 2)
	client := &visibleSequence{
		fakeHerdr: &fakeHerdr{promptStatus: "blocked", waitStatus: "done", readOutput: "ok"},
		script:    []string{ocQuestionScreen, ocQuestionScreen, ocTextFieldScreen},
	}

	got, err := soldier.AnswerBlocked(root, task, "Dark Green", client)
	if err != nil {
		t.Fatalf("AnswerBlocked: %v", err)
	}
	if len(client.sendKeysCalls) != 2 {
		t.Fatalf("key calls = %v, want the selection and then the typing", client.sendKeysCalls)
	}
	if strings.Join(client.sendKeysCalls[0], ",") != "down,down,enter" {
		t.Errorf("selection keys = %v", client.sendKeysCalls[0])
	}
	if strings.Join(client.sendKeysCalls[1], ",") != "D,a,r,k,space,G,r,e,e,n,enter" {
		t.Errorf("typing keys = %v", client.sendKeysCalls[1])
	}
	if len(client.promptCalls) != 0 {
		t.Errorf("a prompt was sent: %v", client.promptCalls)
	}
	if got.Status != state.StatusDone || len(got.Amendments) != 1 || !strings.Contains(got.Amendments[0].Text, "Dark Green") {
		t.Errorf("status %s amendments %+v", got.Status, got.Amendments)
	}
}

// If the text field never shows, nothing is typed.
func TestAnswerBlocked_OpenCodeDoesNotTypeUntilTheTextFieldIsOpen(t *testing.T) {
	defer soldier.SetOpenCodeTextFieldWindow(100 * time.Millisecond)()
	home := t.TempDir()
	root := testProjectRoot(t, home)
	task := newOpenCodeBlockedTask(t, root, 2)
	client := &visibleSequence{
		fakeHerdr: &fakeHerdr{promptStatus: "blocked"},
		script:    []string{ocQuestionScreen},
	}

	_, err := soldier.AnswerBlocked(root, task, "Dark Green", client)
	if err == nil || !strings.Contains(err.Error(), "text field did not open") {
		t.Fatalf("error = %v", err)
	}
	if len(client.sendKeysCalls) != 1 {
		t.Errorf("key calls = %v, want only the selection", client.sendKeysCalls)
	}
}
