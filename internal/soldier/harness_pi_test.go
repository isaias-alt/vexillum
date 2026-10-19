package soldier_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/piask"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

func TestHarnessFor_Pi(t *testing.T) {
	h, err := soldier.HarnessFor("pi")
	if err != nil {
		t.Fatalf("HarnessFor(pi): %v", err)
	}
	if _, ok := h.(soldier.PiHarness); !ok {
		t.Fatalf("HarnessFor(pi) = %T, want PiHarness", h)
	}
	if h.HerdrAgentKind() != "pi" || h.Binary() != "pi" {
		t.Errorf("kind %q, binary %q, want pi and pi", h.HerdrAgentKind(), h.Binary())
	}
	if h.QuestionModal() {
		t.Error("Pi has no question selector, QuestionModal must be false")
	}
	if d := h.TrustDialog(); d.Marker != "" || len(d.Keys) != 0 {
		t.Errorf("TrustDialog = %+v, want none: --approve means Pi never asks", d)
	}
	if !soldier.KnownHarness("pi") {
		t.Error("KnownHarness(pi) = false")
	}
}

// Claude keeps its own binary, its trust dialog and its question selector.
func TestClaudeHarness_Capabilities(t *testing.T) {
	h, _ := soldier.HarnessFor("")
	if h.Binary() != "claude" || !h.QuestionModal() {
		t.Errorf("binary %q, QuestionModal %v", h.Binary(), h.QuestionModal())
	}
	d := h.TrustDialog()
	if d.Marker != "trust this folder" || !slices.Equal(d.Keys, []string{"down", "enter"}) {
		t.Errorf("TrustDialog = %+v", d)
	}
}

func TestHarnessBinary(t *testing.T) {
	for name, want := range map[string]string{"": "claude", "claude": "claude", "pi": "pi", "opencode": "opencode"} {
		got, err := soldier.HarnessBinary(name)
		if err != nil || got != want {
			t.Errorf("HarnessBinary(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
	if _, err := soldier.HarnessBinary("gemini"); err == nil {
		t.Error("HarnessBinary(gemini): expected an error, it is not a harness")
	}
}

func TestPiValidateModelEffort(t *testing.T) {
	cases := []struct {
		name    string
		model   string
		effort  string
		wantErr string
	}{
		{"both empty", "", "", ""},
		{"provider/id", "anthropic/claude-sonnet-5", "", ""},
		{"every thinking level", "", "minimal", ""},
		{"off", "", "off", ""},
		{"max", "", "max", ""},
		{"xhigh", "x/y", "xhigh", ""},
		{"id with a colon segment", "openrouter/meta-llama/llama-4-scout:free", "low", ""},
		{"nested gateway id", "cloudflare-workers-ai/@cf/moonshotai/kimi-k2.6", "", ""},
		{"dotted provider", "my.provider_1/some-model", "", ""},
		{"claude alias is not provider/id", "sonnet", "", "provider/id"},
		{"no id", "anthropic/", "", "provider/id"},
		{"no provider", "/claude", "", "provider/id"},
		{"whitespace", "anthropic/claude sonnet", "", "provider/id"},
		{"leading dash in provider", "-x/y", "", "provider/id"},
		{"unknown effort", "", "extreme", "xhigh"},
		{"effort is case sensitive", "", "High", "unknown effort"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := soldier.ValidateModelEffort("pi", c.model, c.effort)
			switch {
			case c.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case c.wantErr != "" && err == nil:
				t.Fatal("expected an error")
			case c.wantErr != "" && !strings.Contains(err.Error(), c.wantErr):
				t.Errorf("error %q does not mention %q", err, c.wantErr)
			}
		})
	}
}

func piTask(t *testing.T, model, effort string) state.Task {
	t.Helper()
	task := newTask(t)
	task.Harness = "pi"
	task.Model = model
	task.Effort = effort
	return task
}

func TestPiModelEffortArgs(t *testing.T) {
	h := soldier.PiHarness{}
	cases := []struct {
		model, effort string
		want          []string
	}{
		{"", "", nil},
		{"anthropic/claude-sonnet-5", "", []string{"--model", "anthropic/claude-sonnet-5"}},
		{"", "high", []string{"--thinking", "high"}},
		{"openrouter/foo/bar:free", "low", []string{"--model", "openrouter/foo/bar:free", "--thinking", "low"}},
	}
	for _, c := range cases {
		if got := h.ModelEffortArgs(piTask(t, c.model, c.effort)); !slices.Equal(got, c.want) {
			t.Errorf("ModelEffortArgs(%q, %q) = %v, want %v", c.model, c.effort, got, c.want)
		}
	}
}

// The model id keeps its ":free" tail and the thinking level goes only in
// --thinking: Pi would read a ":low" appended to the model as a suffix, and a
// trailing ":free" is part of an OpenRouter id, not a thinking level.
func TestPiModelEffortArgs_NeverFoldsThinkingIntoTheModel(t *testing.T) {
	got := soldier.PiHarness{}.ModelEffortArgs(piTask(t, "openrouter/foo/bar:free", "high"))
	for _, a := range got {
		if a == "openrouter/foo/bar:free:high" {
			t.Fatalf("thinking level folded into the model: %v", got)
		}
	}
}

func TestPiCommand(t *testing.T) {
	task := piTask(t, "anthropic/claude-sonnet-5", "medium")
	task.Prompt = "say hello"

	cmd, err := soldier.HarnessCommand("pi", task)
	if err != nil {
		t.Fatalf("HarnessCommand: %v", err)
	}
	if cmd.Command != "pi" {
		t.Errorf("command = %q, want pi", cmd.Command)
	}
	want := []string{"-p", "--no-session", "--model", "anthropic/claude-sonnet-5", "--thinking", "medium", "--", "say hello"}
	if !slices.Equal(cmd.Args, want) {
		t.Errorf("args = %v, want %v", cmd.Args, want)
	}
}

func TestPiCommand_NoModelEffort(t *testing.T) {
	task := piTask(t, "", "")
	task.Prompt = "- a prompt that starts with a dash"
	cmd, err := soldier.HarnessCommand("pi", task)
	if err != nil {
		t.Fatalf("HarnessCommand: %v", err)
	}
	want := []string{"-p", "--no-session", "--", "- a prompt that starts with a dash"}
	if !slices.Equal(cmd.Args, want) {
		t.Errorf("args = %v, want %v", cmd.Args, want)
	}
}

// ReadOnly narrows the run to Pi's read tools and switches MCP off, and the
// prompt stays last, after "--".
func TestPiCommand_ReadOnly(t *testing.T) {
	task := piTask(t, "anthropic/claude-sonnet-5", "high")
	task.Prompt = "review this"

	cmd, err := soldier.HarnessCommandWith("pi", task, soldier.CommandOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("HarnessCommandWith: %v", err)
	}
	want := []string{"-p", "--no-session", "--model", "anthropic/claude-sonnet-5", "--thinking", "high", "--tools", "read,grep,find,ls", "--no-mcp", "--", "review this"}
	if !slices.Equal(cmd.Args, want) {
		t.Errorf("args = %v, want %v", cmd.Args, want)
	}

	// Without the option nothing narrows the run.
	cmd, _ = soldier.HarnessCommandWith("pi", task, soldier.CommandOptions{})
	if slices.Contains(cmd.Args, "--tools") || slices.Contains(cmd.Args, "--no-mcp") {
		t.Errorf("a run that was not asked to be read-only has tool restrictions: %v", cmd.Args)
	}
}

// Claude has no flag to narrow it to reading, so ReadOnly leaves its command
// exactly as it was.
func TestClaudeCommand_ReadOnlyIsIgnored(t *testing.T) {
	task := newTask(t)
	plain, _ := soldier.HarnessCommand("claude", task)
	ro, _ := soldier.HarnessCommandWith("claude", task, soldier.CommandOptions{ReadOnly: true})
	if !slices.Equal(plain.Args, ro.Args) {
		t.Errorf("claude args changed with ReadOnly: %v vs %v", plain.Args, ro.Args)
	}
}

// A Pi soldier starts with --approve (so a camp with .pi resources never opens
// Pi's trust dialog), --tui-mode regular (normal scrollback, not the alternate
// screen) and the model/thinking args, and none of Claude's flags.
func TestRunInHerdr_PiStartsWithItsOwnArgs(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	task.Harness = "pi"
	task.Model = "anthropic/claude-sonnet-5"
	task.Effort = "high"
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptStatus: "done"}

	if _, err := runInHerdr(t, home, "w1", task, c, client); err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}

	root := testProjectRoot(t, home)
	want := []string{"--approve", "--tui-mode", "regular", "--model", "anthropic/claude-sonnet-5", "--thinking", "high",
		"-e", piask.ExtensionPath(root), "--vx-ask-dir", piask.Dir(root, task.ID)}
	if len(client.startArgs) != 1 || !slices.Equal(client.startArgs[0], want) {
		t.Errorf("expected the soldier to start with %v, got %v", want, client.startArgs)
	}
}

// The kind handed to herdr is pi.
type kindRecordingHerdr struct {
	*fakeHerdr
	kinds []string
}

func (k *kindRecordingHerdr) AgentStart(name, kind, paneID string, agentArgs ...string) error {
	k.kinds = append(k.kinds, kind)
	return k.fakeHerdr.AgentStart(name, kind, paneID, agentArgs...)
}

func TestRunInHerdr_PiStartsAHerdrAgentOfKindPi(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	task.Harness = "pi"
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}
	client := &kindRecordingHerdr{fakeHerdr: &fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptStatus: "done"}}

	if _, err := runInHerdr(t, home, "w1", task, c, client); err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if !slices.Equal(client.kinds, []string{"pi"}) {
		t.Errorf("herdr agent kinds = %v, want [pi]", client.kinds)
	}
}

// A Pi soldier is told to ask through its ask_user tool, with the
// needs-decision: line as the fallback for a session where the tool is not
// there, and is not told about Claude's AskUserQuestion.
func TestRunInHerdr_PiPromptDoesNotMentionAskUserQuestion(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	task.Harness = "pi"
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}
	client := &fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptStatus: "done"}

	if _, err := runInHerdr(t, home, "w1", task, c, client); err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	prompt := client.promptCalls[0]
	// The temp dir in the pause path carries this test's name, so look for the
	// tool as the prompt words it.
	if strings.Contains(prompt, "AskUserQuestion tool") {
		t.Errorf("a Pi soldier's prompt mentions AskUserQuestion:\n%s", prompt)
	}
	for _, want := range []string{"call your ask_user tool", "Only if that tool is not available", "needs-decision:", "real decision or answer from the general", "do not use it for an ordinary status update."} {
		if !strings.Contains(prompt, want) {
			t.Errorf("a Pi soldier's prompt is missing %q", want)
		}
	}

	// Claude's prompt is unchanged.
	home = t.TempDir()
	ctask := newMissionTask(t)
	cc := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + ctask.ID}
	cclient := &fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptStatus: "done"}
	if _, err := runInHerdr(t, home, "w1", ctask, cc, cclient); err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if !strings.Contains(cclient.promptCalls[0], "you are not using the AskUserQuestion tool for it") {
		t.Errorf("a Claude soldier's prompt lost its AskUserQuestion clause")
	}
}

// Claude's "trust this folder" dialog is something to dismiss; a Pi soldier
// stuck on any startup block fails clearly instead, with no blind key press,
// even when the pane happens to say the same words.
func TestRunInHerdr_PiDoesNotDismissAStartupDialog(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	task.Harness = "pi"
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}
	client := &fakeHerdr{
		tabID:      "w1:t2",
		paneID:     "w1:p2",
		startErr:   &herdr.APIError{Code: "agent_not_ready", Message: "blocked during startup"},
		readOutput: "Trust project folder?\n→ Trust\n  Do not trust\nYes, I trust this folder",
		readyAfter: 1 << 30,
	}
	defer soldier.SetNotReadyPollWindow(50 * time.Millisecond)()

	got, err := runInHerdr(t, home, "w1", task, c, client)
	if err == nil {
		t.Fatal("expected an error for a Pi soldier stuck on a startup dialog")
	}
	if got.Status != state.StatusFailed {
		t.Errorf("status = %s, want failed", got.Status)
	}
	if len(client.sendKeysCalls) != 0 {
		t.Errorf("keys were sent to a Pi startup dialog: %v", client.sendKeysCalls)
	}
}

// A Pi soldier that blocks has its question read from the transcript as prose,
// even when the visible pane happens to render like Claude's selector: there
// is no modal to press a key in.
func TestRunInHerdr_PiBlockedIsAlwaysProse(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	task.Harness = "pi"
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{
		tabID: "w1:t2", paneID: "w1:p2", promptStatus: "blocked",
		readOutput:        "Which database should this use?\n\n1. Postgres\n2. SQLite\n",
		readVisibleOutput: askUserQuestionCapture("File name", "Which database should this use?", []string{"Postgres", "SQLite"}),
	}

	got, err := runInHerdr(t, home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if got.Status != state.StatusBlocked {
		t.Fatalf("status = %s, want blocked", got.Status)
	}
	if got.Decision == nil || got.Decision.Kind != state.DecisionKindProse {
		t.Fatalf("expected a prose decision for a Pi soldier, got %+v", got.Decision)
	}
}

// The needs-decision: line works for a Pi soldier exactly as for Claude's.
func TestRunInHerdr_PiNeedsDecisionLineForcesBlocked(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	task.Harness = "pi"
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{
		tabID: "w1:t2", paneID: "w1:p2", promptStatus: "idle",
		readOutput: "I looked at both.\n\nneeds-decision: use Postgres or SQLite?\n",
	}
	got, err := runInHerdr(t, home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if got.Status != state.StatusBlocked || got.Decision == nil || got.Decision.Question != "use Postgres or SQLite?" {
		t.Fatalf("status %s, decision %+v", got.Status, got.Decision)
	}
}

// Answering a blocked Pi soldier is a plain herdr prompt: the answer is typed
// into the pane as text, never a digit key press.
func TestAnswerBlocked_PiAnswersAsFreeText(t *testing.T) {
	home := t.TempDir()
	projectRoot := testProjectRoot(t, home)
	task := newBlockedTask(t, projectRoot, "vx-refactor-the-auth-module")
	task.Harness = "pi"
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatal(err)
	}

	client := &fakeHerdr{promptStatus: "done", readOutput: "used Auth0, done"}
	got, err := soldier.AnswerBlocked(projectRoot, task, "1", client)
	if err != nil {
		t.Fatalf("AnswerBlocked: %v", err)
	}
	if len(client.sendKeysCalls) != 0 {
		t.Errorf("a key press was sent to a Pi pane: %v", client.sendKeysCalls)
	}
	if len(client.promptCalls) != 1 || client.promptCalls[0] != "1" {
		t.Errorf("expected the answer typed as text, got %v", client.promptCalls)
	}
	if got.Status != state.StatusDone {
		t.Errorf("status = %s, want done", got.Status)
	}
}

func TestResolveBlockedDecisionFor(t *testing.T) {
	capture := askUserQuestionCapture("File name", "Which database?", []string{"Postgres", "SQLite"})
	client := &fakeHerdr{readVisibleOutput: capture}
	output := "Which database?\n\n1. Postgres\n2. SQLite\n"

	for _, harness := range []string{"", "claude"} {
		if d := soldier.ResolveBlockedDecisionFor(harness, client, "a", output); d == nil || d.Kind != state.DecisionKindModal {
			t.Errorf("harness %q: expected the modal parsed, got %+v", harness, d)
		}
	}
	if d := soldier.ResolveBlockedDecisionFor("pi", client, "a", output); d == nil || d.Kind != state.DecisionKindProse {
		t.Errorf("pi: expected prose, got %+v", d)
	}
}

// A task that names a harness the build cannot drive is not blocked on a
// modal either: the lookup failing keeps the Claude behavior, and
// RunInHerdr refuses such a task before it gets here.
func TestResolveBlockedDecisionFor_UnknownHarnessKeepsTheModalParser(t *testing.T) {
	capture := askUserQuestionCapture("File name", "Which database?", []string{"Postgres", "SQLite"})
	client := &fakeHerdr{readVisibleOutput: capture}
	if d := soldier.ResolveBlockedDecisionFor("gemini", client, "a", "Which database?\n"); d == nil || d.Kind != state.DecisionKindModal {
		t.Errorf("got %+v", d)
	}
}
