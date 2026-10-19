package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	vxproject "github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/state"
)

// The tests below drive a task whose harness is opencode through the CLI
// commands with a fake `opencode` executable on PATH, once reporting a 2.x
// version and once a 1.x one: nobody running them has a model to talk to, and
// the machine they run on may have either generation installed, so the real
// binary is never on PATH. The fake answers --version, and otherwise records
// what the soldier would have been started with: argv, working directory, the
// two environment variables vexillum sets, and the files it finds in the camp
// at that moment.

// fakeOpenCode installs an executable named opencode reporting version on a
// fresh PATH (with herdr and the real git) and returns the directory it
// records into, in the same layout as fakePi, plus env.N, cfg.N and agent.N.
func fakeOpenCode(t *testing.T, version string) string {
	t.Helper()
	bin := fakeBinDir(t, "herdr")
	rec := t.TempDir()
	installFakeOpenCode(t, bin, rec, version)
	t.Setenv("PATH", bin)
	return rec
}

func installFakeOpenCode(t *testing.T, bin, rec, version string) {
	t.Helper()
	// Only shell builtins: the PATH of these tests holds nothing but herdr, git
	// and this stub, so there is no cat.
	writeStub(t, bin, "opencode", `if [ "$1" = "--version" ]; then echo '`+version+`'; exit 0; fi
d='`+rec+`'
n=0
[ -f "$d/count" ] && read -r n < "$d/count"
n=$((n+1))
echo $n > "$d/count"
copy() { [ -f "$1" ] || return 0; while IFS= read -r l || [ -n "$l" ]; do printf '%s\n' "$l"; done < "$1" > "$2"; }
printf '%s\n' "$@" > "$d/args.$n"
pwd -P > "$d/cwd.$n"
printf '%s' "$OPENCODE_DISABLE_AUTOUPDATE" > "$d/autoupdate.$n"
printf '%s' "$OPENCODE_CONFIG_CONTENT" > "$d/envconfig.$n"
copy opencode.json "$d/cfg.$n"
copy .opencode/agents/vx-soldier.md "$d/agent.$n"
echo "opencode started"`)
}

func ocDispatch(t *testing.T, version, model, effort string) (project, home string, client *piHerdr, rec string, out *bytes.Buffer) {
	t.Helper()
	rec = fakeOpenCode(t, version)
	project, home = piDispatchProject(t)
	client = newPiHerdr()
	out = &bytes.Buffer{}
	if code := runDispatch(project, home, "w1", "do a thing", state.KindMission, model, effort, "opencode", client, out, out); code != 0 {
		t.Fatalf("dispatch: exit %d: %s", code, out.String())
	}
	return
}

// A 2.x opencode: the version is detected and recorded, the soldier starts in
// the camp with --auto and nothing else, the model and variant reach it
// through the files in the camp (which git does not see), the pane gets
// OPENCODE_DISABLE_AUTOUPDATE, and the mission lands and strikes as usual.
func TestOpenCodeLifecycle_V2DispatchStatusLandStrike(t *testing.T) {
	project, home, client, rec, out := ocDispatch(t, "opencode v2.0.32", "opencode/deepseek-v4.1-flash-free#max", "medium")
	if !strings.Contains(out.String(), "status=done") {
		t.Errorf("expected status=done, got: %s", out.String())
	}

	task := dispatchedTask(t, project, home)
	if task.Harness != "opencode" || task.HarnessMajor != 2 || task.HarnessVersion != "2.0.32" {
		t.Errorf("task = harness %q major %d version %q", task.Harness, task.HarnessMajor, task.HarnessVersion)
	}
	if task.Model != "opencode/deepseek-v4.1-flash-free#max" || task.Effort != "medium" {
		t.Errorf("model %q effort %q: the effort label is recorded, the model kept whole", task.Model, task.Effort)
	}
	if !slices.Equal(client.kinds, []string{"opencode"}) {
		t.Errorf("herdr agent kinds = %v", client.kinds)
	}
	if got := recorded(t, rec, "args", 1); got != "--auto" {
		t.Errorf("opencode was started with %q, want only --auto", got)
	}
	if got := recorded(t, rec, "autoupdate", 1); got != "1" {
		t.Errorf("OPENCODE_DISABLE_AUTOUPDATE = %q in the pane", got)
	}
	if got := recorded(t, rec, "envconfig", 1); got != "" {
		t.Errorf("OPENCODE_CONFIG_CONTENT = %q: the V2 TUI ignores it and vexillum must not set it", got)
	}
	if got := recorded(t, rec, "agent", 1); !strings.Contains(got, `model: "opencode/deepseek-v4.1-flash-free#max"`) {
		t.Errorf("the agent file at launch time:\n%s", got)
	}
	if got := recorded(t, rec, "cfg", 1); !strings.Contains(got, `"default_agent": "vx-soldier"`) {
		t.Errorf("opencode.json at launch time:\n%s", got)
	}
	wantCwd, _ := filepath.EvalSymlinks(task.CampPath)
	if got := recorded(t, rec, "cwd", 1); got != wantCwd {
		t.Errorf("started in %s, want the camp %s", got, wantCwd)
	}
	if len(client.promptCalls) != 1 || !strings.Contains(client.promptCalls[0], "needs-decision:") || strings.Contains(client.promptCalls[0], "AskUserQuestion tool") {
		t.Errorf("the soldier's prompt: %q", client.promptCalls)
	}

	var status bytes.Buffer
	if code := runStatus(project, home, true, &status, &status); code != 0 || !strings.Contains(status.String(), `"harness_major": 2`) {
		t.Errorf("status --json (exit %d) does not show the recorded major:\n%s", code, status.String())
	}

	// The camp is clean with the files in it, so the mission lands.
	if got := gitOut(t, task.CampPath, "status", "--porcelain"); got != "" {
		t.Fatalf("the camp is dirty:\n%s", got)
	}
	if err := os.WriteFile(filepath.Join(task.CampPath, "change.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, task.CampPath, "add", "-A")
	gitOut(t, task.CampPath, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "change")
	out.Reset()
	if code := runLand(project, home, task.ID, client, out, out); code != 0 {
		t.Fatalf("land: exit %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "landed:") || !strings.Contains(out.String(), "struck:") {
		t.Errorf("expected landed and struck, got: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(project, "change.txt")); err != nil {
		t.Errorf("the change did not land: %v", err)
	}
	for _, leaked := range []string{"opencode.json", ".opencode"} {
		if _, err := os.Stat(filepath.Join(project, leaked)); err == nil {
			t.Errorf("%s leaked into the project's own checkout", leaked)
		}
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, b)
	}
	return string(b)
}

// A 1.x opencode: --model on the command line, the variant in the config
// the environment carries, no files in the camp, and the 1.x recorded.
func TestOpenCodeLifecycle_V1Dispatch(t *testing.T) {
	project, home, _, rec, _ := ocDispatch(t, "1.19.3", "anthropic/claude-sonnet-5#high", "medium")

	task := dispatchedTask(t, project, home)
	if task.HarnessMajor != 1 || task.HarnessVersion != "1.19.3" {
		t.Errorf("task = major %d version %q", task.HarnessMajor, task.HarnessVersion)
	}
	if got := recorded(t, rec, "args", 1); got != "--auto\n--model\nanthropic/claude-sonnet-5" {
		t.Errorf("opencode was started with:\n%s", got)
	}
	want := `{"agent":{"build":{"model":"anthropic/claude-sonnet-5","variant":"high"}}}`
	if got := recorded(t, rec, "envconfig", 1); got != want {
		t.Errorf("OPENCODE_CONFIG_CONTENT = %q, want %q", got, want)
	}
	if got := recorded(t, rec, "autoupdate", 1); got != "1" {
		t.Errorf("OPENCODE_DISABLE_AUTOUPDATE = %q", got)
	}
	if _, err := os.Stat(filepath.Join(rec, "cfg.1")); err == nil {
		t.Error("V1 found an opencode.json in the camp")
	}
}

// Dispatch refuses a version vexillum does not drive, or no opencode at all,
// before it creates a task or a camp.
func TestOpenCodeLifecycle_DispatchRefusesWhatItCannotDrive(t *testing.T) {
	for _, c := range []struct{ version, want string }{
		{"1.17.6", "older than 1.18.29"},
		{"3.0.0", "not a version vexillum knows"},
	} {
		t.Run(c.version, func(t *testing.T) {
			fakeOpenCode(t, c.version)
			getenv := func(string) string { return "w1" }
			_, err := harnessPreconditions("dispatch", "opencode", getenv, exec.LookPath)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("preconditions = %v, want one mentioning %q", err, c.want)
			}

			project, home := piDispatchProject(t)
			var out bytes.Buffer
			code := runDispatch(project, home, "w1", "x", state.KindMission, "", "", "opencode", newPiHerdr(), &out, &out)
			if code == 0 || !strings.Contains(out.String(), c.want) {
				t.Errorf("runDispatch exit %d: %s", code, out.String())
			}
			root, _ := vxproject.Root(home, project)
			if tasks, _, _ := state.List(root); len(tasks) != 0 {
				t.Errorf("%d task(s) were created", len(tasks))
			}
		})
	}

	t.Setenv("PATH", fakeBinDir(t, "herdr"))
	_, err := harnessPreconditions("dispatch", "opencode", func(string) string { return "w1" }, exec.LookPath)
	if err == nil || !strings.Contains(err.Error(), "opencode is not on PATH") {
		t.Errorf("no opencode: got %v", err)
	}
}

// A project that tracks its own opencode.json is not touched: the dispatch
// fails with a message naming the file, and the task is left struck-able.
func TestOpenCodeLifecycle_ProjectConfigCollisionFailsTheDispatch(t *testing.T) {
	rec := fakeOpenCode(t, "opencode v2.0.32")
	project, home := piDispatchProject(t)
	if err := os.WriteFile(filepath.Join(project, "opencode.json"), []byte(`{"theme":"mine"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, project, "add", "opencode.json")
	gitOut(t, project, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "project opencode config")

	client := newPiHerdr()
	var out bytes.Buffer
	code := runDispatch(project, home, "w1", "do a thing", state.KindMission, "opencode/glm-5.3-flash-free", "", "opencode", client, &out, &out)
	if code == 0 {
		t.Fatalf("the dispatch should fail: %s", out.String())
	}
	if !strings.Contains(out.String(), "status=failed") {
		t.Errorf("a dispatch that failed before creating a pane should say status=failed:\n%s", out.String())
	}
	for _, want := range []string{"opencode.json", "is tracked by the project", "never overwrites or merges"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	task := dispatchedTask(t, project, home)
	if task.Status != state.StatusFailed {
		t.Errorf("status = %s, want failed", task.Status)
	}
	if _, err := os.Stat(filepath.Join(rec, "count")); err == nil {
		t.Error("opencode was started although the dispatch failed")
	}
	if len(client.kinds) != 0 {
		t.Errorf("an agent was started: %v", client.kinds)
	}
	got, _ := os.ReadFile(filepath.Join(task.CampPath, "opencode.json"))
	if string(got) != `{"theme":"mine"}` {
		t.Errorf("the project's opencode.json was changed: %q", got)
	}
}

// Redispatch relaunches on the generation recorded at dispatch.
func TestOpenCodeLifecycle_RedispatchKeepsTheRecordedMajor(t *testing.T) {
	rec := fakeOpenCode(t, "opencode v2.0.32")
	project, home := piDispatchProject(t)
	task := interruptedTestTask(t, project, home)
	root, _ := vxproject.Root(home, project)
	task.Harness = "opencode"
	task.Model = "opencode/glm-5.3-flash-free#high"
	task.Effort = "low"
	task.HarnessMajor = 2
	task.HarnessVersion = "2.0.30"
	if err := state.Save(root, task); err != nil {
		t.Fatal(err)
	}
	if got := taskHarness(project, home, task.ID); got != "opencode" {
		t.Errorf("taskHarness = %q", got)
	}

	var out bytes.Buffer
	if code := runRedispatch(project, home, "w1", task.ID, newPiHerdr(), &out, &out); code != 0 {
		t.Fatalf("redispatch: exit %d: %s", code, out.String())
	}
	if got := recorded(t, rec, "args", 1); got != "--auto" {
		t.Errorf("opencode was restarted with %q", got)
	}
	if got := recorded(t, rec, "agent", 1); !strings.Contains(got, `model: "opencode/glm-5.3-flash-free#high"`) {
		t.Errorf("the agent file at relaunch:\n%s", got)
	}
	reloaded, err := state.Load(root, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.HarnessMajor != 2 || reloaded.HarnessVersion != "2.0.30" || reloaded.Redispatches != 1 {
		t.Errorf("after redispatch: major %d version %q redispatches %d: the recorded generation must be kept", reloaded.HarnessMajor, reloaded.HarnessVersion, reloaded.Redispatches)
	}
}

// If the installed generation is no longer the recorded one, redispatch says
// so before discarding anything.
func TestOpenCodeLifecycle_RedispatchRefusesAChangedMajor(t *testing.T) {
	fakeOpenCode(t, "opencode v2.0.32")
	project, home := piDispatchProject(t)
	task := interruptedTestTask(t, project, home)
	root, _ := vxproject.Root(home, project)
	task.Harness = "opencode"
	task.HarnessMajor = 1
	task.HarnessVersion = "1.19.3"
	if err := state.Save(root, task); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if code := runRedispatch(project, home, "w1", task.ID, newPiHerdr(), &out, &out); code == 0 {
		t.Fatalf("redispatch should refuse: %s", out.String())
	}
	for _, want := range []string{"dispatched on opencode 1.x", "2.0.32 is installed"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	reloaded, _ := state.Load(root, task.ID)
	if reloaded.Status != state.StatusInterrupted || reloaded.Redispatches != 0 {
		t.Errorf("the task was changed by a refused redispatch: %+v", reloaded)
	}
	if _, err := os.Stat(filepath.Join(task.CampPath, "unlanded.txt")); err != nil {
		t.Errorf("the old camp was touched: %v", err)
	}
}
