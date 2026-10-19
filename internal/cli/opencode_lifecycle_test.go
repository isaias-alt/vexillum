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
// binary is never on PATH. The fake answers --version, plays the background
// service for `opencode service ...` and `opencode api ...` (models it
// lists, sessions it creates, in files next to its records), and otherwise
// records what the soldier would have been started with: argv, working
// directory, the environment variables vexillum sets, and whether the config
// files vexillum used to write are in the camp at that moment.

// fakeOpenCode installs an executable named opencode reporting version on a
// fresh PATH (with herdr and the real git) and returns the directory it
// records into: args.N, cwd.N, autoupdate.N, envconfig.N and files.N for the
// Nth start of the soldier, apicalls (one line per `opencode api` call),
// create.N (the body of the Nth session created) and svcenv (the config-layer
// variables the service was started with).
func fakeOpenCode(t *testing.T, version string) string {
	t.Helper()
	bin := fakeBinDir(t, "herdr")
	rec := t.TempDir()
	installFakeOpenCode(t, bin, rec, version)
	t.Setenv("PATH", bin)
	return rec
}

// ocModelsJSON is the answer of GET /api/model the fake service gives.
const ocModelsJSON = `{"location":{"directory":"x"},"data":[` +
	`{"id":"deepseek-v4.1-flash-free","modelID":"deepseek-v4.1-flash-free","providerID":"opencode","variants":[{"id":"high"},{"id":"max"}]},` +
	`{"id":"glm-5.3-flash-free","modelID":"glm-5.3-flash-free","providerID":"opencode","variants":[]}]}`

func installFakeOpenCode(t *testing.T, bin, rec, version string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(rec, "models.json"), []byte(ocModelsJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	// Only shell builtins: the PATH of these tests holds nothing but herdr, git
	// and this stub, so there is no cat.
	writeStub(t, bin, "opencode", `if [ "$1" = "--version" ]; then echo '`+version+`'; exit 0; fi
d='`+rec+`'
if [ "$1" = "service" ]; then
  case "$2" in
    status) if [ -f "$d/svc" ]; then echo http://127.0.0.1:49999; else echo stopped; fi ;;
    start) : > "$d/svc"; printf '%s|%s|%s' "$OPENCODE_CONFIG" "$OPENCODE_CONFIG_DIR" "$OPENCODE_CONFIG_CONTENT" > "$d/svcenv"; echo http://127.0.0.1:49999 ;;
    *) exit 1 ;;
  esac
  exit 0
fi
if [ "$1" = "api" ]; then
  printf '%s\n' "$*" >> "$d/apicalls"
  case "$2 $3" in
    "GET /api/model")
      while IFS= read -r l || [ -n "$l" ]; do printf '%s\n' "$l"; done < "$d/models.json" ;;
    "POST /api/session")
      m=0
      [ -f "$d/sesscount" ] && read -r m < "$d/sesscount"
      m=$((m+1))
      echo $m > "$d/sesscount"
      printf '%s' "$5" > "$d/create.$m"
      q='"'
      dir=${5#*directory$q:$q}
      dir=${dir%%$q*}
      printf '{"data":{"id":"ses_fake%s","location":{"directory":"%s"}}}\n' "$m" "$dir" > "$d/sess.ses_fake$m"
      printf '{"data":{"id":"ses_fake%s"}}\n' "$m" ;;
    "GET /api/session/"*)
      id=${3#/api/session/}
      if [ -f "$d/sess.$id" ]; then
        while IFS= read -r l || [ -n "$l" ]; do printf '%s\n' "$l"; done < "$d/sess.$id"
      else
        echo '{"_tag":"SessionNotFoundError","message":"Session not found"}'
        echo 'HTTP 404 Not Found' >&2
        exit 1
      fi ;;
    *) echo "unexpected api call: $*" >&2; exit 1 ;;
  esac
  exit 0
fi
n=0
[ -f "$d/count" ] && read -r n < "$d/count"
n=$((n+1))
echo $n > "$d/count"
printf '%s\n' "$@" > "$d/args.$n"
pwd -P > "$d/cwd.$n"
printf '%s' "$OPENCODE_DISABLE_AUTOUPDATE" > "$d/autoupdate.$n"
printf '%s' "$OPENCODE_CONFIG_CONTENT" > "$d/envconfig.$n"
f=
[ -e opencode.json ] && f="$f opencode.json"
[ -e .opencode/agents/vx-soldier.md ] && f="$f .opencode/agents/vx-soldier.md"
printf '%s' "$f" > "$d/files.$n"
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

// A 2.x opencode: the version is detected and recorded, the service is started
// by vexillum from an environment without the config layer, the soldier gets a
// session on the task's model and variant located in the camp, it starts in
// the camp with --auto and --session and nothing else, the camp holds no file
// of vexillum's, the pane gets OPENCODE_DISABLE_AUTOUPDATE, and the mission
// lands and strikes as usual.
func TestOpenCodeLifecycle_V2DispatchStatusLandStrike(t *testing.T) {
	// A config layer in the environment of whoever runs vx dispatch must not
	// reach the service vexillum starts.
	t.Setenv("OPENCODE_CONFIG", "/poison/file.json")
	t.Setenv("OPENCODE_CONFIG_DIR", "/poison/dir")
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"model":"poison/x"}`)
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
	if task.HarnessSession != "ses_fake1" {
		t.Errorf("the task records session %q, want ses_fake1", task.HarnessSession)
	}
	if !slices.Equal(client.kinds, []string{"opencode"}) {
		t.Errorf("herdr agent kinds = %v", client.kinds)
	}
	if got := recorded(t, rec, "args", 1); got != "--auto\n--session\nses_fake1" {
		t.Errorf("opencode was started with %q, want --auto --session ses_fake1", got)
	}
	if got := recorded(t, rec, "autoupdate", 1); got != "1" {
		t.Errorf("OPENCODE_DISABLE_AUTOUPDATE = %q in the pane", got)
	}
	// (The poison set above is in the test process's own environment, which the
	// fake herdr hands to the pane; what vexillum itself puts in the pane is
	// client.env.)
	if !slices.Equal(client.env, []string{"OPENCODE_DISABLE_AUTOUPDATE=1"}) {
		t.Errorf("the pane's environment from vexillum = %v: no config may travel in it", client.env)
	}
	if got := recorded(t, rec, "files", 1); got != "" {
		t.Errorf("the camp held %q at launch time: nothing is written into it", got)
	}
	if svcenv, err := os.ReadFile(filepath.Join(rec, "svcenv")); err != nil || string(svcenv) != "||" {
		t.Errorf("the service was started with config-layer variables %q (%v)", svcenv, err)
	}
	wantCwd, _ := filepath.EvalSymlinks(task.CampPath)
	if got := recorded(t, rec, "cwd", 1); got != wantCwd {
		t.Errorf("started in %s, want the camp %s", got, wantCwd)
	}

	calls := strings.Split(strings.TrimSpace(readFile(t, filepath.Join(rec, "apicalls"))), "\n")
	if len(calls) < 2 || !strings.HasPrefix(calls[0], "api GET /api/model -H x-opencode-directory: ") || !strings.HasSuffix(calls[0], task.CampPath) {
		t.Errorf("api calls = %q: the models are listed for the camp first", calls)
	}
	body := readFile(t, filepath.Join(rec, "create.1"))
	for _, want := range []string{
		`"model":{"providerID":"opencode","id":"deepseek-v4.1-flash-free","variant":"max"}`,
		`"location":{"directory":"` + task.CampPath + `"}`,
		`"title":"vexillum ` + task.ID + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("session body lacks %s:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{`"agent"`, `"permission`} {
		if strings.Contains(body, forbidden) {
			t.Errorf("session body carries %s: the project's agent and denies must apply untouched:\n%s", forbidden, body)
		}
	}
	if len(client.promptCalls) != 1 || !strings.Contains(client.promptCalls[0], "needs-decision:") || strings.Contains(client.promptCalls[0], "AskUserQuestion tool") {
		t.Errorf("the soldier's prompt: %q", client.promptCalls)
	}

	var status bytes.Buffer
	if code := runStatus(project, home, true, &status, &status); code != 0 || !strings.Contains(status.String(), `"harness_major": 2`) || !strings.Contains(status.String(), `"harness_session": "ses_fake1"`) {
		t.Errorf("status --json (exit %d) does not show the recorded major and session:\n%s", code, status.String())
	}

	// The camp is clean, so the mission lands.
	if got := gitOut(t, task.CampPath, "status", "--porcelain", "--ignored"); got != "" {
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
	if got := recorded(t, rec, "files", 1); got != "" {
		t.Errorf("V1 found %q in the camp", got)
	}
	if _, err := os.Stat(filepath.Join(rec, "apicalls")); err == nil {
		t.Error("V1 has no service API: opencode api was run")
	}
	if task.HarnessSession != "" {
		t.Errorf("V1 records no session, got %q", task.HarnessSession)
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

// A project that tracks its own OpenCode configuration, or its own agent named
// vx-soldier, dispatches like any other: nothing is written, merged or
// refused, and the files are untouched.
func TestOpenCodeLifecycle_ProjectConfigIsNotACollision(t *testing.T) {
	for _, name := range []string{"opencode.json", "opencode.jsonc", ".opencode/agents/vx-soldier.md"} {
		t.Run(name, func(t *testing.T) {
			rec := fakeOpenCode(t, "opencode v2.0.32")
			project, home := piDispatchProject(t)
			if err := os.MkdirAll(filepath.Join(project, filepath.Dir(name)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(project, name), []byte(`{"theme":"mine"}`), 0o644); err != nil {
				t.Fatal(err)
			}
			gitOut(t, project, "add", name)
			gitOut(t, project, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "project opencode config")

			client := newPiHerdr()
			var out bytes.Buffer
			code := runDispatch(project, home, "w1", "do a thing", state.KindMission, "opencode/glm-5.3-flash-free", "", "opencode", client, &out, &out)
			if code != 0 || !strings.Contains(out.String(), "status=done") {
				t.Fatalf("the dispatch should succeed (exit %d): %s", code, out.String())
			}
			task := dispatchedTask(t, project, home)
			if got := readFile(t, filepath.Join(task.CampPath, name)); got != `{"theme":"mine"}` {
				t.Errorf("the project's %s was changed: %q", name, got)
			}
			if got := gitOut(t, task.CampPath, "status", "--porcelain", "--ignored"); got != "" {
				t.Errorf("the camp has changes:\n%s", got)
			}
			if got := recorded(t, rec, "args", 1); got != "--auto\n--session\nses_fake1" {
				t.Errorf("opencode was started with %q", got)
			}
		})
	}
}

// A provider, model or variant OpenCode does not list fails the dispatch with
// what does exist, before a soldier is started and before any session exists.
func TestOpenCodeLifecycle_UnknownModelFailsTheDispatchNamingWhatExists(t *testing.T) {
	for _, c := range []struct {
		model string
		want  []string
	}{
		{"nopeprov/x", []string{`no provider "nopeprov"`, "providers with models: opencode"}},
		{"opencode/zzz", []string{`no model "zzz"`, "deepseek-v4.1-flash-free, glm-5.3-flash-free"}},
		{"opencode/deepseek-v4.1-flash-free#nope", []string{`no variant "nope"`, "high, max"}},
	} {
		t.Run(c.model, func(t *testing.T) {
			rec := fakeOpenCode(t, "opencode v2.0.32")
			project, home := piDispatchProject(t)
			client := newPiHerdr()
			var out bytes.Buffer
			code := runDispatch(project, home, "w1", "do a thing", state.KindMission, c.model, "", "opencode", client, &out, &out)
			if code == 0 || !strings.Contains(out.String(), "status=failed") {
				t.Fatalf("the dispatch should fail with status=failed (exit %d): %s", code, out.String())
			}
			for _, want := range append(c.want, "cannot start this soldier on "+c.model) {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			if _, err := os.Stat(filepath.Join(rec, "count")); err == nil {
				t.Error("opencode was started although the dispatch failed")
			}
			if _, err := os.Stat(filepath.Join(rec, "sesscount")); err == nil {
				t.Error("a session was created for a model that does not exist")
			}
			if len(client.kinds) != 0 {
				t.Errorf("an agent was started: %v", client.kinds)
			}
			if task := dispatchedTask(t, project, home); task.Status != state.StatusFailed || task.HarnessSession != "" {
				t.Errorf("task = %s with session %q, want failed and none", task.Status, task.HarnessSession)
			}
		})
	}
}

// Redispatch relaunches on the generation recorded at dispatch.
func TestOpenCodeLifecycle_RedispatchKeepsTheRecordedMajor(t *testing.T) {
	rec := fakeOpenCode(t, "opencode v2.0.32")
	project, home := piDispatchProject(t)
	task := interruptedTestTask(t, project, home)
	root, _ := vxproject.Root(home, project)
	task.Harness = "opencode"
	task.Model = "opencode/deepseek-v4.1-flash-free#high"
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
	// A task that records no session (one dispatched before sessions, or by
	// hand) gets one now, on the recorded model and variant.
	if got := recorded(t, rec, "args", 1); got != "--auto\n--session\nses_fake1" {
		t.Errorf("opencode was restarted with %q", got)
	}
	if body := readFile(t, filepath.Join(rec, "create.1")); !strings.Contains(body, `"variant":"high"`) {
		t.Errorf("session body: %s", body)
	}
	reloaded, err := state.Load(root, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.HarnessSession != "ses_fake1" {
		t.Errorf("session = %q", reloaded.HarnessSession)
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

// interrupt marks the dispatched task interrupted, as the sentinel does when
// its pane is gone.
func interrupt(t *testing.T, project, home, id string) {
	t.Helper()
	root, _ := vxproject.Root(home, project)
	if _, err := state.Update(root, id, func(task *state.Task) (bool, error) {
		task.Status = state.StatusInterrupted
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A re-dispatch opens the session the task recorded, in the same camp, and
// creates none.
func TestOpenCodeLifecycle_RedispatchReusesTheSessionOfTheSameCamp(t *testing.T) {
	project, home, _, rec, _ := ocDispatch(t, "opencode v2.0.32", "opencode/deepseek-v4.1-flash-free#max", "")
	task := dispatchedTask(t, project, home)
	interrupt(t, project, home, task.ID)

	var out bytes.Buffer
	if code := runRedispatch(project, home, "w1", task.ID, newPiHerdr(), &out, &out); code != 0 {
		t.Fatalf("redispatch: exit %d: %s", code, out.String())
	}
	if got := recorded(t, rec, "args", 2); got != "--auto\n--session\nses_fake1" {
		t.Errorf("the relaunch started opencode with %q, want the same session", got)
	}
	if _, err := os.Stat(filepath.Join(rec, "create.2")); err == nil {
		t.Error("a second session was created")
	}
	if again := dispatchedTask(t, project, home); again.HarnessSession != "ses_fake1" || again.CampPath != task.CampPath {
		t.Errorf("after redispatch: session %q camp %q (was %q)", again.HarnessSession, again.CampPath, task.CampPath)
	}
}

// A session the service no longer has (deleted, or the service was reset) is
// replaced by a new one instead of failing the re-dispatch.
func TestOpenCodeLifecycle_RedispatchReplacesASessionTheServiceLost(t *testing.T) {
	project, home, _, rec, _ := ocDispatch(t, "opencode v2.0.32", "opencode/deepseek-v4.1-flash-free#max", "")
	task := dispatchedTask(t, project, home)
	interrupt(t, project, home, task.ID)
	if err := os.Remove(filepath.Join(rec, "sess.ses_fake1")); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if code := runRedispatch(project, home, "w1", task.ID, newPiHerdr(), &out, &out); code != 0 {
		t.Fatalf("redispatch: exit %d: %s", code, out.String())
	}
	if got := recorded(t, rec, "args", 2); got != "--auto\n--session\nses_fake2" {
		t.Errorf("the relaunch started opencode with %q, want the new session", got)
	}
	if again := dispatchedTask(t, project, home); again.HarnessSession != "ses_fake2" {
		t.Errorf("session = %q", again.HarnessSession)
	}
}
