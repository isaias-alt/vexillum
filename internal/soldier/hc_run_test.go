package soldier_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/opencodeosc"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

// hcClient is the hc backend driving bench/fakes/hc in a temp state directory.
func hcClient(t *testing.T) *herdr.HC {
	t.Helper()
	fake, err := filepath.Abs(filepath.Join("..", "..", "bench", "fakes", "hc"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("VXFAKE_HC_DIR", t.TempDir())
	t.Setenv("VXFAKE_HC_WORK_SECS", "1")
	return &herdr.HC{
		Bin: fake, Session: "fakesession",
		PollInterval: 50 * time.Millisecond, StallWindow: 400 * time.Millisecond, StartWindow: 800 * time.Millisecond,
		ShellWindow: time.Second, BlockedGrace: -1,
	}
}

func hcTermFile(t *testing.T, addr, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(os.Getenv("VXFAKE_HC_DIR"), addr, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// A whole dispatch on the hc backend, against the fake core: one shell terminal
// for the soldier, the harness typed in with the task's args, the terminal marked
// with the task, the prompt delivered, and the soldier left Running for the
// sentinel once it is seen working.
func TestRunInHerdr_HCBackend(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	got, err := runInHerdr(t, home, "vx-proj-1", task, c, hcClient(t))
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if got.Status != state.StatusRunning {
		t.Errorf("status = %s, want running (the soldier is working when the probe ends)", got.Status)
	}
	if got.HerdrWorkspaceID != "vx-proj-1" || got.HerdrTabID != "t1" || got.HerdrPaneID != "t1" || got.HerdrAgentName != "vx-do-the-thing" {
		t.Errorf("recorded ids = %+v", got)
	}
	if got.Backend != "hc" {
		t.Errorf("Backend = %q, want hc recorded on the task", got.Backend)
	}
	if got.PromptSubmittedAt.IsZero() {
		t.Error("the prompt was delivered and must be recorded as submitted")
	}

	sent := hcTermFile(t, "t1", "sent")
	if !strings.Contains(sent, "claude --dangerously-skip-permissions --prompt-suggestions false") {
		t.Errorf("the launch line typed into the shell lacks the harness args:\n%s", sent)
	}
	if !strings.Contains(sent, "do the thing") {
		t.Errorf("the task's prompt was never typed:\n%s", sent)
	}
	labels := hcTermFile(t, "t1", "labels")
	for _, want := range []string{"vexillum.task=" + task.ID, "vexillum.agent=vx-do-the-thing", "vexillum.managed=1"} {
		if !strings.Contains(labels, want+"\n") {
			t.Errorf("labels lack %s:\n%s", want, labels)
		}
	}
}

// Claude's trust dialog on the hc backend: the start ends not-ready, the screen
// shows the dialog and the existing "down enter" answer gets past it.
func TestRunInHerdr_HCBackendDismissesTheTrustDialog(t *testing.T) {
	t.Setenv("VXFAKE_HC_START", "silent")
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	got, err := runInHerdr(t, home, "vx-proj-1", task, c, hcClient(t))
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if got.Status != state.StatusRunning {
		t.Errorf("status = %s, want running", got.Status)
	}
	if keys := hcTermFile(t, "t1", "keys"); !strings.Contains(keys, "down enter") {
		t.Errorf("the trust dialog was not answered with down enter; keys: %q", keys)
	}
}

// A soldier that ends up blocked on AskUserQuestion: its visible screen, read
// through hc (plain text, no ANSI), still parses as the modal, and a digit
// answers it.
func TestAnswerBlocked_HCBackendModal(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}
	client := hcClient(t)

	// Start the soldier, then make its next turn ask a question.
	if _, err := runInHerdr(t, home, "vx-proj-1", task, c, client); err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if st, err := client.AgentWait("vx-do-the-thing", nil, 5000); err != nil || st != "done" {
		t.Fatalf("waiting for the first turn: %q, %v", st, err)
	}
	t.Setenv("VXFAKE_HC_BLOCK", "1")
	if st, err := client.AgentPrompt("vx-do-the-thing", "ask me something", []string{"idle", "done", "blocked", "working"}, 10000); err != nil || st != "blocked" {
		t.Fatalf("prompt = %q, %v; want blocked", st, err)
	}

	screen, err := client.AgentReadVisible("vx-do-the-thing")
	if err != nil {
		t.Fatal(err)
	}
	d, err := soldier.ParseAskUserQuestionModal(screen)
	if err != nil {
		t.Fatalf("ParseAskUserQuestionModal(hc read) = %v\n%s", err, screen)
	}
	if d.Question != "Which color do you prefer?" || len(d.Options) != 4 || d.Options[0] != "Red" {
		t.Errorf("parsed modal = %+v", d)
	}
}

// The same modal with Claude Code's side panel on screen (a "│" column shared
// with the conversation) still parses once hc read strips the panel.
func TestAnswerBlocked_HCBackendModalWithSidePanel(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}
	client := hcClient(t)

	if _, err := runInHerdr(t, home, "vx-proj-1", task, c, client); err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if st, err := client.AgentWait("vx-do-the-thing", nil, 5000); err != nil || st != "done" {
		t.Fatalf("waiting for the first turn: %q, %v", st, err)
	}
	t.Setenv("VXFAKE_HC_BLOCK", "1")
	if st, err := client.AgentPrompt("vx-do-the-thing", "ask me something", []string{"idle", "done", "blocked", "working"}, 10000); err != nil || st != "blocked" {
		t.Fatalf("prompt = %q, %v; want blocked", st, err)
	}

	rows := []string{
		" ☐ Color",
		"",
		"Which color do you prefer?",
		"",
		"❯ 1. Red",
		"  2. Blue",
		"  3. Type something.",
		"  4. Chat about this",
		"",
		"Enter to select · ↑/↓ to navigate · Esc to cancel",
	}
	var grid strings.Builder
	for i, r := range rows {
		panel := "    "
		if i == 0 {
			panel = " source: Current ▾"
		} else if i == 2 {
			panel = " No changes this session"
		}
		grid.WriteString(fmt.Sprintf("%-60s │%s\n", r, panel))
	}
	screenPath := filepath.Join(os.Getenv("VXFAKE_HC_DIR"), "t1", "screen")
	if err := os.WriteFile(screenPath, []byte(grid.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	screen, err := client.AgentReadVisible("vx-do-the-thing")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(screen, "│") {
		t.Errorf("the panel column was not stripped:\n%s", screen)
	}
	d, err := soldier.ParseAskUserQuestionModal(screen)
	if err != nil {
		t.Fatalf("ParseAskUserQuestionModal(hc read with panel) = %v\n%s", err, screen)
	}
	if d.Question != "Which color do you prefer?" || len(d.Options) != 4 || d.Options[0] != "Red" {
		t.Errorf("parsed modal = %+v", d)
	}
}

// An OpenCode 2.x soldier on the hc backend: the status plugin is installed under
// the project's state, registered through OPENCODE_CLI_CONFIG_CONTENT on the
// launch line (and nothing else of OpenCode's config), and the soldier is then
// watched like any other: it reports through the plugin, is prompted once, is
// seen working and ends done with a turn counted.
func TestRunInHerdr_HCBackendOpenCodeV2(t *testing.T) {
	home := t.TempDir()
	task := ocTask(t, 2, "", "")
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}
	client := hcClient(t)

	got, err := runInHerdr(t, home, "vx-proj-1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if got.Status != state.StatusRunning || got.Backend != "hc" || got.PromptSubmittedAt.IsZero() {
		t.Errorf("task = status %s, backend %q, submitted %v; want a running hc soldier whose prompt was delivered", got.Status, got.Backend, got.PromptSubmittedAt)
	}

	// The launch line, byte for byte: the plugin directory under the project
	// root, the environment sorted, the arguments the V2 TUI is started with.
	root := testProjectRoot(t, home)
	wantLine := `env OPENCODE_CLI_CONFIG_CONTENT='{"plugins":["` + filepath.Join(root, "opencode", "osc7501") + `"]}' OPENCODE_DISABLE_AUTOUPDATE=1 opencode --auto`
	sent := hcTermFile(t, "t1", "sent")
	if lines := strings.Split(sent, "\n"); lines[0] != wantLine {
		t.Errorf("launch line =\n%s\nwant\n%s", lines[0], wantLine)
	}
	if strings.Contains(sent, "OPENCODE_CONFIG_CONTENT") {
		t.Errorf("OPENCODE_CONFIG_CONTENT is read by the shared service and must not be set:\n%s", sent)
	}
	if n := strings.Count(sent, "say hello"); n != 1 {
		t.Errorf("the prompt was typed %d times, want exactly once:\n%s", n, sent)
	}

	// The plugin is on disk, current, and not in the camp.
	if st, err := opencodeosc.Inspect(root); err != nil || st != opencodeosc.Current {
		t.Errorf("plugin state = %v, %v; want current", st, err)
	}

	// Watched like any soldier: the terminal says it runs opencode, was seen
	// working at dispatch, and ends done with the turn counted.
	if info, err := client.AgentGet(got.HerdrAgentName); err != nil || info.Kind != "opencode" {
		t.Errorf("AgentGet = %+v, %v; want kind opencode", info, err)
	}
	if st, err := client.AgentWait(got.HerdrAgentName, nil, 5000); err != nil || st != "done" {
		t.Fatalf("waiting for the turn: %q, %v; want done", st, err)
	}
	if turn := hcTermFile(t, "t1", "turn"); strings.TrimSpace(turn) != "1" {
		t.Errorf("turn = %q, want 1", turn)
	}
}

// A redispatch launches through the same RunInHerdr, so the plugin is registered
// again for the new terminal even if the file was deleted in between.
func TestRunInHerdr_HCBackendOpenCodeV2Again(t *testing.T) {
	home := t.TempDir()
	task := ocTask(t, 2, "", "")
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}
	client := hcClient(t)
	root := testProjectRoot(t, home)

	if _, err := runInHerdr(t, home, "vx-proj-1", task, c, client); err != nil {
		t.Fatalf("first RunInHerdr: %v", err)
	}
	if err := os.Remove(opencodeosc.Path(root)); err != nil {
		t.Fatal(err)
	}
	task2 := ocTask(t, 2, "", "")
	task2.Prompt = "do it again"
	c.Slot, c.Branch = 2, "vexillum/"+task2.ID
	if _, err := runInHerdr(t, home, "vx-proj-1", task2, c, client); err != nil {
		t.Fatalf("second RunInHerdr: %v", err)
	}
	if st, err := opencodeosc.Inspect(root); err != nil || st != opencodeosc.Current {
		t.Errorf("plugin state after the second launch = %v, %v; want reinstalled", st, err)
	}
	if first := strings.Split(hcTermFile(t, "t2", "sent"), "\n")[0]; !strings.Contains(first, "OPENCODE_CLI_CONFIG_CONTENT=") {
		t.Errorf("the second soldier's launch line lacks the plugin: %s", first)
	}
}

// A soldier whose plugin does not load never reports, and the dispatch says so
// once instead of typing the prompt again and again into a terminal nobody
// watches.
func TestRunInHerdr_HCBackendOpenCodeV2PluginNotLoaded(t *testing.T) {
	t.Setenv("VXFAKE_HC_START", "mute")
	defer soldier.SetNotReadyPollWindow(50 * time.Millisecond)()
	home := t.TempDir()
	task := ocTask(t, 2, "", "")
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	got, err := runInHerdr(t, home, "vx-proj-1", task, c, hcClient(t))
	if err == nil {
		t.Fatalf("RunInHerdr = %+v, want an error: opencode reported no status", got)
	}
	if !strings.Contains(err.Error(), "agent_not_ready") && !strings.Contains(err.Error(), "reported no status") {
		t.Errorf("error = %v, want the not-ready message", err)
	}
	if n := strings.Count(hcTermFile(t, "t1", "sent"), "say hello"); n != 0 {
		t.Errorf("the prompt was typed %d times into a terminal that never reported", n)
	}
}
