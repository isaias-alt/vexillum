package herdr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeHC returns an HC that drives bench/fakes/hc (a stateful stand-in for
// herdr-core) in a temp state directory, with the waits shortened.
func fakeHC(t *testing.T) *HC {
	t.Helper()
	fake, err := filepath.Abs(filepath.Join("..", "..", "bench", "fakes", "hc"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("VXFAKE_HC_DIR", t.TempDir())
	t.Setenv("VXFAKE_HC_WORK_SECS", "1")

	return &HC{
		Bin: fake, Session: "fakesession",
		PollInterval: 50 * time.Millisecond, StallWindow: 400 * time.Millisecond, StartWindow: 800 * time.Millisecond,
		ShellWindow: time.Second, BlockedGrace: -1,
	}
}

func callsLog(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(os.Getenv("VXFAKE_HC_DIR"), "calls.log"))
	if err != nil {
		t.Fatalf("reading the fake's call log: %v", err)
	}
	return string(raw)
}

func termFile(t *testing.T, addr, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(os.Getenv("VXFAKE_HC_DIR"), addr, name))
	if err != nil {
		t.Fatalf("reading %s/%s of the fake: %v", addr, name, err)
	}
	return string(raw)
}

// started brings up a soldier-shaped terminal: created, bound to a task and with
// the agent launched and reporting.
func started(t *testing.T, h *HC, name string) string {
	t.Helper()
	tab, pane, err := h.CreateTab("vx-proj", t.TempDir(), name)
	if err != nil {
		t.Fatalf("CreateTab: %v", err)
	}
	if tab != pane {
		t.Fatalf("an hc tab and pane are the same terminal, got %q and %q", tab, pane)
	}
	if err := h.BindTask(pane, "task1"); err != nil {
		t.Fatalf("BindTask: %v", err)
	}
	if err := h.AgentStart(name, "claude", pane); err != nil {
		t.Fatalf("AgentStart: %v", err)
	}
	return pane
}

func TestParseHCVersion(t *testing.T) {
	for _, c := range []struct {
		out  string
		want string
		ok   bool
	}{
		{"hc 0.1.16\n", "0.1.16", true},
		{"hc 0.2.0-rc1", "0.2.0-rc1", true},
		{"herdr 0.9.5\n", "", false},
		{"herdr-core 0.1.16", "", false},
		{"", "", false},
	} {
		got, err := ParseHCVersion(c.out)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("ParseHCVersion(%q) = %q, %v; want %q, ok=%v", c.out, got, err, c.want, c.ok)
		}
	}
}

// herdr-core is tried first, then hc, and a binary that does not print "hc x.y.z"
// is passed over: the herdr TUI must never be taken for the core.
func TestResolveHC(t *testing.T) {
	paths := map[string]string{"herdr-core": "/bin/herdr-core", "hc": "/bin/hc"}
	lookPath := func(name string) (string, error) {
		if p, ok := paths[name]; ok {
			return p, nil
		}
		return "", os.ErrNotExist
	}
	version := func(out map[string]string) func(string) (string, error) {
		return func(bin string) (string, error) { return ParseHCVersion(out[bin]) }
	}

	bin, ver, err := ResolveHC(lookPath, version(map[string]string{"/bin/herdr-core": "hc 0.1.16", "/bin/hc": "hc 0.1.15"}))
	if err != nil || bin != "/bin/herdr-core" || ver != "0.1.16" {
		t.Errorf("both usable: got %q %q %v, want herdr-core 0.1.16", bin, ver, err)
	}
	bin, _, err = ResolveHC(lookPath, version(map[string]string{"/bin/herdr-core": "herdr 0.9.5", "/bin/hc": "hc 0.1.15"}))
	if err != nil || bin != "/bin/hc" {
		t.Errorf("herdr-core is not the core: got %q %v, want hc", bin, err)
	}
	_, _, err = ResolveHC(lookPath, version(map[string]string{"/bin/herdr-core": "herdr 0.9.5", "/bin/hc": "herdr 0.9.5"}))
	if err == nil || !strings.Contains(err.Error(), "herdr TUI") {
		t.Errorf("only TUIs: got %v, want an error naming the TUI", err)
	}
	_, _, err = ResolveHC(func(string) (string, error) { return "", os.ErrNotExist }, version(nil))
	if err == nil || !strings.Contains(err.Error(), "not on PATH") {
		t.Errorf("nothing installed: got %v", err)
	}
}

func TestPinnedSession(t *testing.T) {
	for _, c := range []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"HC_SESSION": "work"}, "work"},
		{map[string]string{"HC_SESSION": "work", "HC_TERMINAL_SOCKET": "/x/default/default.sock"}, "work"},
		{map[string]string{"HC_TERMINAL_SOCKET": "/home/u/.local/state/herdr-core/scratch/scratch.sock"}, "scratch"},
		{map[string]string{"HC_TERMINAL_SOCKET": "/run/odd.sock"}, "default"},
		{nil, "default"},
	} {
		if got := PinnedSession(func(k string) string { return c.env[k] }); got != c.want {
			t.Errorf("PinnedSession(%v) = %q, want %q", c.env, got, c.want)
		}
	}
}

func TestHCEnvPinsSessionAndStripsTerminalIdentity(t *testing.T) {
	got := hcEnv([]string{"PATH=/bin", "HC_SOCKET=/s", "HC_SESSION=other", "HC_TERMINAL_ID=t9", "HC_TERMINAL_SOCKET=/t", "HC_SCOPE=x", "HOME=/h"}, "pinned")
	want := "PATH=/bin HOME=/h HC_SESSION=pinned"
	if strings.Join(got, " ") != want {
		t.Errorf("hcEnv = %q, want %q", got, want)
	}
}

func TestBackendName(t *testing.T) {
	for _, c := range []struct {
		val, want string
		ok        bool
	}{{"", "herdr", true}, {"herdr", "herdr", true}, {"HC", "hc", true}, {" hc ", "hc", true}, {"tmux", "", false}} {
		got, err := BackendName(func(string) string { return c.val })
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("BackendName(%q) = %q, %v; want %q, ok=%v", c.val, got, err, c.want, c.ok)
		}
	}
}

// The calls reach the core pinned to the configured session, with the terminal
// identity of the caller removed, and the terminal is started detached from it.
func TestHC_CreateTab_Run(t *testing.T) {
	h := fakeHC(t)
	t.Setenv("HC_SOCKET", "/somewhere/else.sock")
	t.Setenv("HC_TERMINAL_ID", "t9")
	t.Setenv("HC_TERMINAL_SOCKET", "/commander.sock")
	t.Setenv("HC_SESSION", "commanders")

	cwd := t.TempDir()
	tab, pane, err := h.CreateTab("vx-proj-1", cwd, "vx-fix-bug", "A=1")
	if err != nil || tab != "t1" || pane != "t1" {
		t.Fatalf("CreateTab = %q, %q, %v; want t1, t1", tab, pane, err)
	}
	log := callsLog(t)
	for _, want := range []string{
		"session=fakesession socket=unset terminal=unset scope=unset",
		"run --bg --json --no-parent --outlive-parent --no-notify --cwd " + cwd,
		"--label vexillum.managed=1 --label vexillum.project=proj-1 --label herdr.space=vx-proj-1 --label herdr.tab=vx-fix-bug",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("hc was called without %q; calls:\n%s", want, log)
		}
	}
}

func TestHC_CreateTab_RejectsABadEnvEntry(t *testing.T) {
	h := fakeHC(t)
	if _, _, err := h.CreateTab("vx-p", t.TempDir(), "n", "not-an-assignment"); err == nil {
		t.Fatal("an env entry that is not KEY=VALUE must be refused before anything starts")
	}
}

func TestLaunchLine(t *testing.T) {
	got := launchLine([]string{"A=1", "B=two words", "C=it's"}, "claude", []string{"--model", "opus", "--append-system-prompt", "say 'hi'", "plain"})
	want := `env A=1 B='two words' C='it'\''s' claude --model opus --append-system-prompt 'say '\''hi'\''' plain`
	if got != want {
		t.Errorf("launchLine =\n%s\nwant\n%s", got, want)
	}
	if got := launchLine(nil, "pi", nil); got != "pi" {
		t.Errorf("launchLine without env or args = %q, want pi", got)
	}
}

// CreateTab + BindTask + AgentStart: the launch line carries the env CreateTab was
// given, the terminal is marked, and the agent reports ready.
func TestHC_StartAgent(t *testing.T) {
	h := fakeHC(t)
	_, pane, err := h.CreateTab("vx-p", t.TempDir(), "vx-do-it", "TASK_KEY=abc", "Q=a b")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.BindTask(pane, "task9"); err != nil {
		t.Fatal(err)
	}
	if err := h.AgentStart("vx-do-it", "claude", pane, "--dangerously-skip-permissions"); err != nil {
		t.Fatalf("AgentStart: %v", err)
	}

	if sent := termFile(t, pane, "sent"); !strings.Contains(sent, `env TASK_KEY=abc Q='a b' claude --dangerously-skip-permissions`) {
		t.Errorf("the shell was typed %q", sent)
	}
	labels := termFile(t, pane, "labels")
	for _, want := range []string{"vexillum.managed=1", "vexillum.project=p", "vexillum.task=task9", "vexillum.agent=vx-do-it", "vexillum.launched=1", "herdr.space=vx-p"} {
		if !strings.Contains(labels, want+"\n") {
			t.Errorf("labels %q lack %s", labels, want)
		}
	}
	ready, err := h.AgentReady("vx-do-it")
	if err != nil || !ready {
		t.Errorf("AgentReady = %v, %v; want true", ready, err)
	}
	if st, err := h.AgentStatus("vx-do-it"); err != nil || st != "idle" {
		t.Errorf("AgentStatus = %q, %v; want idle", st, err)
	}
}

// A second terminal asking for a live agent's name gets the name_taken error the
// soldier package falls back from.
func TestHC_StartAgent_NameTaken(t *testing.T) {
	h := fakeHC(t)
	started(t, h, "vx-same")
	_, pane2, err := h.CreateTab("vx-p", t.TempDir(), "vx-same")
	if err != nil {
		t.Fatal(err)
	}
	err = h.AgentStart("vx-same", "claude", pane2)
	if !IsNameTaken(err) {
		t.Fatalf("AgentStart with a live name = %v, want name taken", err)
	}
	if err := h.AgentStart("vx-same-abc123", "claude", pane2); err != nil {
		t.Fatalf("AgentStart with the fallback name: %v", err)
	}
}

// Claude's trust dialog reports no status: the start ends agent_not_ready, the
// screen shows the dialog, "down enter" accepts it and the agent then reports.
func TestHC_StartAgent_TrustDialog(t *testing.T) {
	h := fakeHC(t)
	t.Setenv("VXFAKE_HC_START", "silent")
	_, pane, _ := h.CreateTab("vx-p", t.TempDir(), "vx-trust")
	_ = h.BindTask(pane, "t")
	err := h.AgentStart("vx-trust", "claude", pane)
	if !IsNotReady(err) {
		t.Fatalf("AgentStart at the trust dialog = %v, want not ready", err)
	}
	if ready, _ := h.AgentReady("vx-trust"); ready {
		t.Error("an agent that has reported nothing is not ready")
	}
	screen, err := h.AgentRead("vx-trust", 20)
	if err != nil || !strings.Contains(strings.ToLower(screen), "trust this folder") {
		t.Fatalf("AgentRead = %q, %v; want the trust dialog", screen, err)
	}
	if err := h.AgentSendKeys("vx-trust", "down", "enter"); err != nil {
		t.Fatal(err)
	}
	if ready, err := h.AgentReady("vx-trust"); err != nil || !ready {
		t.Errorf("after the dialog AgentReady = %v, %v; want true", ready, err)
	}
}

func TestHC_StartAgent_ProgramExitsAtOnce(t *testing.T) {
	h := fakeHC(t)
	t.Setenv("VXFAKE_HC_START", "exit")
	h.StartWindow = 3 * time.Second
	_, pane, _ := h.CreateTab("vx-p", t.TempDir(), "vx-gone")
	err := h.AgentStart("vx-gone", "claude", pane)
	if !IsNotRunning(err) || !strings.Contains(err.Error(), "command not found") {
		t.Fatalf("AgentStart of a program that exits = %v, want not running, with the screen", err)
	}
}

// done is not idle, and a prompt that asks for "working" is answered as soon as
// the agent is at it instead of when its turn ends.
func TestHC_PromptWorkingThenDone(t *testing.T) {
	h := fakeHC(t)
	t.Setenv("VXFAKE_HC_WORK_SECS", "2")
	started(t, h, "vx-turn")

	begin := time.Now()
	st, err := h.AgentPrompt("vx-turn", "do the thing", []string{"idle", "done", "blocked", "working"}, 15000)
	if err != nil || st != "working" {
		t.Fatalf("AgentPrompt = %q, %v; want working", st, err)
	}
	if took := time.Since(begin); took > 1500*time.Millisecond {
		t.Errorf("the prompt returned after %s; it must not wait for the turn to end", took)
	}
	st, err = h.AgentWait("vx-turn", nil, 15000)
	if err != nil || st != "done" {
		t.Fatalf("AgentWait = %q, %v; want done", st, err)
	}
	if st, _ := h.AgentStatus("vx-turn"); st != "done" {
		t.Errorf("AgentStatus after a turn = %q, want done (not idle)", st)
	}
}

// Without "working" in until the prompt waits for the turn to end, and the stale
// done of a previous turn is not mistaken for this one's.
func TestHC_PromptWaitsForTheTurn(t *testing.T) {
	h := fakeHC(t)
	started(t, h, "vx-wait")
	for i, text := range []string{"first", "second"} {
		st, err := h.AgentPrompt("vx-wait", text, []string{"idle", "done", "blocked"}, 15000)
		if err != nil || st != "done" {
			t.Fatalf("prompt %d = %q, %v; want done", i+1, st, err)
		}
	}
}

func TestHC_PromptTimeout(t *testing.T) {
	h := fakeHC(t)
	t.Setenv("VXFAKE_HC_WORK_SECS", "30")
	started(t, h, "vx-slow")
	_, err := h.AgentPrompt("vx-slow", "long job", []string{"idle", "done"}, 300)
	if !IsTimeout(err) {
		t.Fatalf("AgentPrompt that outlasts its timeout = %v, want the timeout error", err)
	}
	if _, err := h.AgentWait("vx-slow", []string{"idle", "done"}, 300); !IsTimeout(err) {
		t.Fatalf("AgentWait that outlasts its timeout = %v, want the timeout error", err)
	}
	if _, err := h.AgentWait("vx-slow", nil, 1000); !IsTimeout(err) {
		t.Fatalf("native wait that outlasts its timeout = %v, want the timeout error", err)
	}
}

// An agent that never reports (opencode, or a booting one) cannot be watched:
// the prompt ends as the stalled error the soldier retries on.
func TestHC_PromptStalled(t *testing.T) {
	h := fakeHC(t)
	t.Setenv("VXFAKE_HC_START", "silent")
	_, pane, _ := h.CreateTab("vx-p", t.TempDir(), "vx-mute")
	_ = h.BindTask(pane, "t")
	_ = h.AgentStart("vx-mute", "claude", pane)
	_, err := h.AgentPrompt("vx-mute", "hello", []string{"working"}, 15000)
	if !IsStalled(err) {
		t.Fatalf("AgentPrompt to an agent that reports nothing = %v, want stalled", err)
	}
}

func TestHC_Blocked(t *testing.T) {
	h := fakeHC(t)
	started(t, h, "vx-ask")
	t.Setenv("VXFAKE_HC_BLOCK", "1")
	st, err := h.AgentPrompt("vx-ask", "ask me", []string{"idle", "done", "blocked", "working"}, 15000)
	if err != nil || st != "blocked" {
		t.Fatalf("AgentPrompt = %q, %v; want blocked", st, err)
	}
	screen, err := h.AgentReadVisible("vx-ask")
	if err != nil || !strings.Contains(screen, "1. Red") {
		t.Fatalf("AgentReadVisible = %q, %v; want the modal", screen, err)
	}
	// Typing into a blocked agent would answer its dialog: refused, as herdr does.
	if _, err := h.AgentPrompt("vx-ask", "more text", nil, 1000); !IsBlocked(err) {
		t.Errorf("a prompt to a blocked agent = %v, want the blocked error", err)
	}
	// A digit answers it, and the agent is working again.
	if err := h.AgentSendKeys("vx-ask", "1"); err != nil {
		t.Fatal(err)
	}
	st, err = h.AgentWait("vx-ask", []string{"idle", "done", "blocked", "working"}, 15000)
	if err != nil || st != "working" {
		t.Errorf("AgentWait after the answer = %q, %v; want working", st, err)
	}
}

func TestHC_KeysOutsideHCsVocabularyFail(t *testing.T) {
	h := fakeHC(t)
	started(t, h, "vx-keys")
	if err := h.AgentSendKeys("vx-keys", "+"); err == nil {
		t.Error("hc keys cannot press '+': the error must come back")
	}
}

func TestHC_ErrorsMapToTheHerdrPredicates(t *testing.T) {
	h := fakeHC(t)
	started(t, h, "vx-here")
	if _, err := h.AgentStatus("vx-nothing"); !IsNotFound(err) {
		t.Errorf("an unknown name = %v, want not found", err)
	}
	if _, err := h.AgentGet("t77"); !IsNotFound(err) || !IsTabGone(err) {
		t.Errorf("an unknown address = %v, want not found and tab gone", err)
	}
}

// hc keeps the last status a program reported after it exits, so an agent that
// is gone back to its shell must read as gone, not as done forever.
func TestHC_AgentThatExitedIsGone(t *testing.T) {
	h := fakeHC(t)
	pane := started(t, h, "vx-quit")
	if err := h.AgentSendKeys("vx-quit", "ctrl+c"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.AgentStatus("vx-quit"); !IsNotFound(err) {
		t.Errorf("AgentStatus of an exited agent = %v, want not found", err)
	}
	if ok, err := h.AgentReady("vx-quit"); ok || !IsNotFound(err) {
		t.Errorf("AgentReady of an exited agent = %v, %v; want false, not found", ok, err)
	}
	if infos, _ := h.AgentList(); len(infos) != 0 {
		t.Errorf("AgentList = %+v, want the exited agent left out", infos)
	}
	if err := h.TabClose(pane); err != nil {
		t.Errorf("a terminal whose agent exited can still be closed: %v", err)
	}
}

func TestHC_TabClose(t *testing.T) {
	h := fakeHC(t)
	pane := started(t, h, "vx-bye")
	if err := h.TabClose(pane); err != nil {
		t.Fatalf("TabClose: %v", err)
	}
	log := callsLog(t)
	for _, want := range []string{"kill " + pane, "rm " + pane, "get " + pane} {
		if !strings.Contains(log, want) {
			t.Errorf("TabClose did not call %q:\n%s", want, log)
		}
	}
	if _, err := h.AgentStatus("vx-bye"); !IsNotFound(err) {
		t.Errorf("a closed soldier = %v, want not found", err)
	}
	if err := h.TabClose(pane); !IsTabGone(err) {
		t.Errorf("closing it again = %v, want tab gone", err)
	}
}

// Anything vexillum did not start is left alone: no kill, and no typing into a
// shell or a program that is not an agent.
func TestHC_RefusesTerminalsItDidNotStart(t *testing.T) {
	h := fakeHC(t)
	if _, err := h.exec("run", "--bg", "--json", "--label", "mine=1"); err != nil {
		t.Fatal(err)
	}
	if err := h.TabClose("t1"); err == nil || !strings.Contains(err.Error(), "did not start it") {
		t.Errorf("TabClose of a foreign terminal = %v, want a refusal", err)
	}
	if strings.Contains(callsLog(t), "kill t1") {
		t.Error("a foreign terminal was killed")
	}
	if err := h.AgentSendKeys("t1", "enter"); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("AgentSendKeys to a foreign shell = %v, want a refusal", err)
	}
	if _, err := h.AgentPrompt("t1", "rm -rf", nil, 1000); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("AgentPrompt to a foreign shell = %v, want a refusal", err)
	}
	if err := h.BindTask("t1", "x"); err == nil {
		t.Error("BindTask must not label a terminal vexillum did not start")
	}
}

// A name resolves only to a terminal that carries the vexillum labels: a foreign
// terminal that happens to hold the label cannot be reached by it.
func TestHC_NameNeedsTheManagedLabel(t *testing.T) {
	h := fakeHC(t)
	if _, err := h.exec("run", "--bg", "--json", "--label", "vexillum.agent=vx-spoof"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.AgentStatus("vx-spoof"); !IsNotFound(err) {
		t.Errorf("a name held by an unmanaged terminal = %v, want not found", err)
	}
}

// The commander's own pane is not one vexillum started, but it runs an agent, so
// it can be looked up and prompted (the sentinel's wake injection); the user's
// plain shells are not listed.
func TestHC_CommanderAndList(t *testing.T) {
	h := fakeHC(t)
	started(t, h, "vx-troop")
	// A commander: an app terminal (no vexillum labels) running claude that has
	// reported.
	if _, err := h.exec("run", "--bg", "--json", "--cwd", "/work/proj"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.exec("send", "t2", "--", "claude"); err != nil {
		t.Fatal(err)
	}
	// And a shell of the user's.
	if _, err := h.exec("run", "--bg", "--json"); err != nil {
		t.Fatal(err)
	}

	info, err := h.AgentGet("t2")
	if err != nil || info.Kind != "claude" || info.PaneID != "t2" || info.Cwd != "/work/proj" {
		t.Fatalf("AgentGet(commander) = %+v, %v; want a claude agent in /work/proj", info, err)
	}
	if info, err := h.AgentGet("t3"); err != nil || info.Kind != "" {
		t.Errorf("AgentGet(shell) = %+v, %v; want no kind", info, err)
	}
	soldier, err := h.AgentGet("vx-troop")
	if err != nil || soldier.Name != "vx-troop" || soldier.Kind != "claude" {
		t.Fatalf("AgentGet(soldier by name) = %+v, %v", soldier, err)
	}

	list, err := h.AgentList()
	if err != nil {
		t.Fatal(err)
	}
	var panes []string
	for _, a := range list {
		panes = append(panes, a.PaneID)
	}
	if strings.Join(panes, ",") != "t1,t2" {
		t.Errorf("AgentList panes = %v, want the soldier and the commander but not the shell", panes)
	}
	if _, err := h.AgentPrompt("t2", "wake up", []string{"working"}, 5000); err != nil {
		t.Errorf("prompting the commander's pane: %v", err)
	}
}

func TestHC_Hello(t *testing.T) {
	h := fakeHC(t)
	if err := h.HelloOK(); err != nil {
		t.Fatalf("HelloOK: %v", err)
	}
	t.Setenv("VXFAKE_HC_GENERATION", "2")
	if err := h.HelloOK(); err == nil || !strings.Contains(err.Error(), "generation 2") {
		t.Errorf("HelloOK with generation 2 = %v, want a refusal naming it", err)
	}
}

func TestHCTerminalKind(t *testing.T) {
	shell := []string{"/bin/zsh", "-l"}
	for _, c := range []struct {
		name string
		t    hcTerminal
		want string
	}{
		{"shell at the prompt", hcTerminal{Argv: shell, PID: 1}, ""},
		{"claude after its first report", hcTerminal{Argv: shell, PID: 1, Foreground: &hcForeground{PID: 2, Name: "2.1.309", Argv: []string{"claude"}}, Status: &hcStatus{App: "claude-code", State: "idle"}}, "claude"},
		{"claude before it reports", hcTerminal{Argv: shell, PID: 1, Foreground: &hcForeground{PID: 2, Name: "2.1.316", Argv: []string{"/home/u/.local/bin/claude"}}}, "claude"},
		{"pi runs under node", hcTerminal{Argv: shell, PID: 1, Foreground: &hcForeground{PID: 2, Name: "node", Argv: []string{"node", "/opt/pi"}}}, "pi"},
		{"a stale status after the agent exited", hcTerminal{Argv: shell, PID: 1, Foreground: &hcForeground{PID: 1, Name: "zsh"}, Status: &hcStatus{App: "claude-code", State: "done"}}, ""},
		{"vim", hcTerminal{Argv: shell, PID: 1, Foreground: &hcForeground{PID: 2, Name: "vim", Argv: []string{"vim"}}}, ""},
		{"opencode started as the terminal's program", hcTerminal{Argv: []string{"opencode"}, PID: 1}, "opencode"},
	} {
		if got := c.t.kind(); got != c.want {
			t.Errorf("%s: kind = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestHCStatusWords(t *testing.T) {
	for _, c := range []struct {
		status *hcStatus
		want   string
	}{{nil, "unknown"}, {&hcStatus{State: "idle"}, "idle"}, {&hcStatus{State: "DONE"}, "done"}, {&hcStatus{State: "error"}, "error"}, {&hcStatus{}, "unknown"}} {
		if got := (hcTerminal{Status: c.status}).statusWord(); got != c.want {
			t.Errorf("statusWord(%+v) = %q, want %q", c.status, got, c.want)
		}
	}
}

func TestHCErrorMapping(t *testing.T) {
	for _, c := range []struct {
		exit   int
		stderr string
		code   string
	}{
		{3, `{"error":{"kind":"not_found","message":"no terminal"}}`, "agent_not_found"},
		{3, ``, "agent_not_found"},
		{9, `{"error":{"kind":"blocked","message":"t9 is blocked (question)","hint":"read its screen first"}}`, "agent_blocked"},
		{10, `{"error":{"kind":"status_mismatch","message":"t7 is done, not idle"}}`, "status_mismatch"},
		{11, `{"error":{"kind":"finished","message":"t7 finished"}}`, "agent_not_running"},
		{1, `{"error":{"kind":"name_taken","message":"taken"}}`, "agent_name_taken"},
		{1, `{"error":{"kind":"spawn_failed","message":"cannot start"}}`, "spawn_failed"},
	} {
		err := hcError([]string{"x"}, c.exit, []byte(c.stderr))
		apiErr, ok := err.(*APIError)
		if !ok || apiErr.Code != c.code {
			t.Errorf("hcError(%d, %s) = %v, want code %s", c.exit, c.stderr, err, c.code)
		}
	}
	if err := hcError([]string{"x"}, 1, []byte("boom")); err == nil || IsNotFound(err) {
		t.Errorf("an unparsable failure must stay a plain error, got %v", err)
	}
}
