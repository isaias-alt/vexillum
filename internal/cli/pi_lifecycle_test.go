package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/herdr"
	vxproject "github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

// The tests below drive a task whose harness is pi through the CLI commands
// (dispatch, status, prompt, decide, redispatch, land, strike) with a fake
// `pi` executable on PATH, standing in for the real CLI nobody here can run
// against a model. The fake records the argv and working directory it was
// started with and prints a line; piHerdr starts it the way herdr would, in
// the soldier's camp, so what is asserted is the launch Pi would really get.

// fakePi installs an executable named pi on a fresh PATH (with herdr and the
// real git, and deliberately no claude) and returns the directory the fake
// records into: args.N holds the argv of its Nth start, one argument per
// line, and cwd.N its working directory.
func fakePi(t *testing.T) string {
	t.Helper()
	bin := fakeBinDir(t, "herdr")
	rec := t.TempDir()
	writeStub(t, bin, "pi", `d='`+rec+`'
n=$(cat "$d/count" 2>/dev/null || echo 0)
n=$((n+1))
echo $n > "$d/count"
printf '%s\n' "$@" > "$d/args.$n"
pwd -P > "$d/cwd.$n"
echo "pi started"`)
	t.Setenv("PATH", bin)
	return rec
}

// recorded returns what the fake pi saved for its nth start under prefix.
func recorded(t *testing.T, rec, prefix string, n int) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(rec, prefix+"."+string(rune('0'+n))))
	if err != nil {
		t.Fatalf("the fake pi did not record %s for start %d: %v", prefix, n, err)
	}
	return strings.TrimRight(string(b), "\n")
}

// piHerdr is a herdr.Client that starts the agent kind it is asked for as a
// real process from PATH, in the directory of the tab it created, the way
// herdr launches a pane's agent. It records the kinds and the keys sent.
type piHerdr struct {
	*fakeHerdr
	cwd   string
	kinds []string
	keys  [][]string
}

func newPiHerdr() *piHerdr {
	return &piHerdr{fakeHerdr: &fakeHerdr{tabID: "w1:t1", paneID: "w1:p1", promptStatus: "done", readOutput: "pi did the thing"}}
}

func (p *piHerdr) CreateTab(workspaceID, cwd, label string) (string, string, error) {
	p.cwd = cwd
	return p.fakeHerdr.CreateTab(workspaceID, cwd, label)
}

func (p *piHerdr) AgentStart(name, kind, paneID string, agentArgs ...string) error {
	p.kinds = append(p.kinds, kind)
	cmd := exec.Command(kind, agentArgs...)
	cmd.Dir = p.cwd
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.New("starting " + kind + ": " + err.Error() + ": " + string(out))
	}
	return p.fakeHerdr.AgentStart(name, kind, paneID, agentArgs...)
}

func (p *piHerdr) AgentSendKeys(name string, keys ...string) error {
	p.keys = append(p.keys, keys)
	return p.fakeHerdr.AgentSendKeys(name, keys...)
}

var _ herdr.Client = (*piHerdr)(nil)

func piDispatchProject(t *testing.T) (project, home string) {
	t.Helper()
	project = initDispatchTestProject(t)
	if err := writeLocalConfig(filepath.Join(project, ".vexillum")); err != nil {
		t.Fatalf("writing local config: %v", err)
	}
	// Committed, so the checkout is clean when a mission lands on it.
	for _, args := range [][]string{{"add", "-A"}, {"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "vexillum config"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = project
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
	}
	return project, t.TempDir()
}

// dispatch needs the CLI of the task's harness on PATH: pi for a pi profile,
// with no claude installed, and claude for everything else.
func TestHarnessPreconditions_NeedTheHarnessCLI(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "herdr"))
	getenv := func(string) string { return "w1" }

	_, err := harnessPreconditions("dispatch", "pi", getenv, exec.LookPath)
	if err == nil || !strings.Contains(err.Error(), "pi is not on PATH") || !strings.Contains(err.Error(), "pi.dev/install.sh") {
		t.Errorf("pi missing: got %v, want a pi-is-not-on-PATH error with the install hint", err)
	}
	if err != nil && strings.Contains(err.Error(), "claude") {
		t.Errorf("a pi dispatch must not ask for claude: %v", err)
	}

	if _, err := harnessPreconditions("dispatch", "", getenv, exec.LookPath); err == nil || !strings.Contains(err.Error(), "claude is not on PATH") {
		t.Errorf("claude missing: got %v", err)
	}

	fakePi(t)
	id, err := harnessPreconditions("dispatch", "pi", getenv, exec.LookPath)
	if err != nil || id != "w1" {
		t.Errorf("pi present, claude absent: got %q, %v; want w1 and no error", id, err)
	}

	if _, err := harnessPreconditions("dispatch", "opencode", getenv, exec.LookPath); err == nil || !strings.Contains(err.Error(), "not implemented yet") {
		t.Errorf("a reserved harness: got %v", err)
	}
}

// A mission dispatched on a pi profile starts pi in the camp with the model,
// the thinking level and Pi's two fixed flags, records the harness on the
// task, shows it in status, and lands and strikes like any other mission.
func TestPiLifecycle_DispatchStatusLandStrike(t *testing.T) {
	rec := fakePi(t)
	project, home := piDispatchProject(t)
	client := newPiHerdr()

	var out bytes.Buffer
	code := runDispatch(project, home, "w1", "do a thing", state.KindMission, "anthropic/claude-haiku-4-5", "low", "pi", client, &out, &out)
	if code != 0 {
		t.Fatalf("dispatch: exit %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "status=done") {
		t.Errorf("expected status=done, got: %s", out.String())
	}

	task := dispatchedTask(t, project, home)
	if task.Harness != "pi" || task.Model != "anthropic/claude-haiku-4-5" || task.Effort != "low" {
		t.Errorf("task = harness %q model %q effort %q", task.Harness, task.Model, task.Effort)
	}
	if !slices.Equal(client.kinds, []string{"pi"}) {
		t.Errorf("herdr agent kinds = %v, want [pi]", client.kinds)
	}
	wantArgs := "--approve\n--tui-mode\nregular\n--model\nanthropic/claude-haiku-4-5\n--thinking\nlow"
	if got := recorded(t, rec, "args", 1); got != wantArgs {
		t.Errorf("pi was started with:\n%s\nwant:\n%s", got, wantArgs)
	}
	wantCwd, _ := filepath.EvalSymlinks(task.CampPath)
	if got := recorded(t, rec, "cwd", 1); got != wantCwd {
		t.Errorf("pi was started in %s, want the camp %s", got, wantCwd)
	}
	if len(client.keys) != 0 {
		t.Errorf("keys were sent to pi's pane at startup: %v", client.keys)
	}
	if len(client.promptCalls) != 1 || !strings.Contains(client.promptCalls[0], "needs-decision:") || strings.Contains(client.promptCalls[0], "AskUserQuestion tool") {
		t.Errorf("the pi soldier's prompt: %q", client.promptCalls)
	}

	// status shows the harness the task runs on.
	var status bytes.Buffer
	if code := runStatus(project, home, true, &status, &status); code != 0 || !strings.Contains(status.String(), `"harness": "pi"`) {
		t.Errorf("status --json (exit %d) does not show the harness:\n%s", code, status.String())
	}

	// The soldier commits; land fast-forwards and strikes the camp.
	if err := os.WriteFile(filepath.Join(task.CampPath, "change.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "change"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = task.CampPath
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
	}
	out.Reset()
	if code := runLand(project, home, task.ID, client, &out, &out); code != 0 {
		t.Fatalf("land: exit %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "landed:") || !strings.Contains(out.String(), "struck:") {
		t.Errorf("expected landed and struck, got: %s", out.String())
	}
	if !client.tabClosed {
		t.Error("expected the strike to close the soldier's pane")
	}
	if _, err := os.Stat(filepath.Join(project, "change.txt")); err != nil {
		t.Errorf("the change did not land on the project: %v", err)
	}
}

// A scout on a pi profile starts the same way and is told where to write its
// report; a strike is refused until it exists, as for any scout.
func TestPiLifecycle_ScoutStrikeNeedsItsReport(t *testing.T) {
	fakePi(t)
	project, home := piDispatchProject(t)
	client := newPiHerdr()

	var out bytes.Buffer
	if code := runDispatch(project, home, "w1", "find out why", state.KindScout, "openrouter/some/model:free", "minimal", "pi", client, &out, &out); code != 0 {
		t.Fatalf("dispatch: exit %d: %s", code, out.String())
	}
	if len(client.promptCalls) != 1 || !strings.Contains(client.promptCalls[0], "write your final report as a single Markdown file") {
		t.Fatalf("the scout was not told to write a report: %q", client.promptCalls)
	}
	task := dispatchedTask(t, project, home)
	out.Reset()
	if code := runStrike(project, home, task.ID, strikeOptions{}, client, &out, &out); code == 0 {
		t.Errorf("a scout without its report was struck: %s", out.String())
	}
}

// A Pi soldier that asks a question is blocked with a prose decision, and the
// answer goes to its pane as text (a herdr prompt), never as a key press.
func TestPiLifecycle_BlockedQuestionIsAnsweredAsFreeText(t *testing.T) {
	fakePi(t)
	project, home := piDispatchProject(t)
	client := newPiHerdr()
	client.promptStatus = "blocked"
	client.readOutput = "Which database should this use?\n\n1. Postgres\n2. SQLite\n"
	// A pane that happens to render like Claude's selector is still not one.
	client.readVisibleOutput = "☐ File name\nWhich database should this use?\n1. Postgres\n2. SQLite\nEnter to select · ↑/↓ to navigate · Esc to cancel"

	var out bytes.Buffer
	if code := runDispatch(project, home, "w1", "pick a database", state.KindMission, "", "", "pi", client, &out, &out); code != 0 {
		t.Fatalf("dispatch: exit %d: %s", code, out.String())
	}
	task := dispatchedTask(t, project, home)
	if task.Status != state.StatusBlocked || task.Decision == nil || task.Decision.Kind != state.DecisionKindProse {
		t.Fatalf("status %s, decision %+v; want blocked with a prose decision", task.Status, task.Decision)
	}

	client.promptStatus = "done"
	out.Reset()
	if code := runDecide(project, home, task.ID, "2", client, &out, &out); code != 0 {
		t.Fatalf("decide: exit %d: %s", code, out.String())
	}
	if len(client.keys) != 0 {
		t.Errorf("a key press was sent to a Pi pane: %v", client.keys)
	}
	if last := client.promptCalls[len(client.promptCalls)-1]; last != "2" {
		t.Errorf("the answer was delivered as %q, want the text 2", last)
	}
	after := dispatchedTask(t, project, home)
	if after.Status != state.StatusDone || after.Decision == nil || after.Decision.Answer != "2" {
		t.Errorf("after the answer: status %s, decision %+v", after.Status, after.Decision)
	}
}

// A finished Pi soldier takes a follow-up prompt through the normal herdr
// prompt path, and the prompt is recorded as an amendment.
func TestPiLifecycle_PromptFollowUp(t *testing.T) {
	fakePi(t)
	project, home := piDispatchProject(t)
	client := newPiHerdr()

	var out bytes.Buffer
	if code := runDispatch(project, home, "w1", "do a thing", state.KindMission, "", "", "pi", client, &out, &out); code != 0 {
		t.Fatalf("dispatch: exit %d: %s", code, out.String())
	}
	task := dispatchedTask(t, project, home)

	out.Reset()
	if code := runReprompt(project, home, task.ID, "also add a test", client, &out, &out); code != 0 {
		t.Fatalf("prompt: exit %d: %s", code, out.String())
	}
	if last := client.promptCalls[len(client.promptCalls)-1]; last != "also add a test" {
		t.Errorf("the follow-up was delivered as %q", last)
	}
	after := dispatchedTask(t, project, home)
	if len(after.Amendments) != 1 || after.Amendments[0].Text != "also add a test" {
		t.Errorf("amendments = %+v", after.Amendments)
	}
}

// Redispatching an interrupted pi task relaunches pi, with the same model and
// thinking level, from a fresh camp; the harness survives.
func TestPiLifecycle_Redispatch(t *testing.T) {
	rec := fakePi(t)
	project, home := piDispatchProject(t)
	task := interruptedTestTask(t, project, home)
	projectRoot, err := vxproject.Root(home, project)
	if err != nil {
		t.Fatal(err)
	}
	task.Harness = "pi"
	task.Model = "openai/gpt-5"
	task.Effort = "high"
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatal(err)
	}

	if got := taskHarness(project, home, task.ID); got != "pi" {
		t.Errorf("taskHarness = %q, want pi (so redispatch checks for pi on PATH, not claude)", got)
	}
	if got := taskHarness(project, home, "no-such-task"); got != "" {
		t.Errorf("taskHarness of an unknown task = %q, want empty", got)
	}

	client := newPiHerdr()
	var out bytes.Buffer
	if code := runRedispatch(project, home, "w1", task.ID, client, &out, &out); code != 0 {
		t.Fatalf("redispatch: exit %d: %s", code, out.String())
	}
	wantArgs := "--approve\n--tui-mode\nregular\n--model\nopenai/gpt-5\n--thinking\nhigh"
	if got := recorded(t, rec, "args", 1); got != wantArgs {
		t.Errorf("pi was restarted with:\n%s\nwant:\n%s", got, wantArgs)
	}
	reloaded, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Harness != "pi" || reloaded.Redispatches != 1 {
		t.Errorf("after redispatch: harness %q, redispatches %d", reloaded.Harness, reloaded.Redispatches)
	}
}

// A model that is not provider/id is refused for a pi profile, and so is an
// effort that is not a thinking level: the check Dispatch runs before it
// creates anything.
func TestPiLifecycle_ValidationRefusesBeforeCreatingAnything(t *testing.T) {
	for _, c := range []struct{ model, effort, want string }{
		{"sonnet", "", "provider/id"},
		{"", "extreme", "xhigh"},
	} {
		err := soldier.ValidateModelEffort("pi", c.model, c.effort)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("model %q effort %q: got %v, want an error mentioning %q", c.model, c.effort, err, c.want)
		}
	}
}
