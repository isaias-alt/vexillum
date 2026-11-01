package fakes

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runFakeHerdr(t *testing.T, env []string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command("sh", append([]string{"herdr"}, args...)...)
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// The fake must answer the JSON envelope internal/herdr parses.
func TestFakeHerdrTabCreateEnvelope(t *testing.T) {
	out, _, err := runFakeHerdr(t, nil, "tab", "create", "--workspace", "w1")
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Result struct {
			Tab struct {
				TabID string `json:"tab_id"`
			} `json:"tab"`
			RootPane struct {
				PaneID string `json:"pane_id"`
			} `json:"root_pane"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if resp.Result.Tab.TabID == "" || resp.Result.RootPane.PaneID == "" {
		t.Errorf("tab and pane ids are required: %s", out)
	}
}

func TestFakeHerdrPromptSettlesAtOnceByDefault(t *testing.T) {
	out, _, err := runFakeHerdr(t, nil, "agent", "prompt", "a", "text", "--wait", "--timeout", "15000")
	if err != nil || !strings.Contains(out, `"agent_status":"working"`) {
		t.Errorf("want a working agent at once, got %q, %v", out, err)
	}
}

func TestFakeHerdrBusyPromptTimesOut(t *testing.T) {
	_, stderr, err := runFakeHerdr(t, []string{"VXBENCH_PROMPT_MODE=busy"}, "agent", "prompt", "a", "text", "--wait", "--timeout", "1000")
	if err == nil {
		t.Fatal("a busy soldier makes the prompt fail with the timeout error")
	}
	if !strings.Contains(stderr, `"code":"timeout"`) {
		t.Errorf("want the timeout error code on stderr, got %q", stderr)
	}
}

// A call that asks to stop at "working" is answered at once by a busy soldier,
// as herdr does; the timeout test above is the same call without --until.
func TestFakeHerdrBusyPromptStopsAtWorkingWhenAsked(t *testing.T) {
	out, _, err := runFakeHerdr(t, []string{"VXBENCH_PROMPT_MODE=busy"}, "agent", "prompt", "a", "text", "--wait", "--timeout", "60000",
		"--until", "idle", "--until", "working")
	if err != nil || !strings.Contains(out, `"agent_status":"working"`) {
		t.Errorf("want a working agent at once, got %q, %v", out, err)
	}
}

func TestFakeHerdrSilentSoldierTimesOutEvenWhenAskedForWorking(t *testing.T) {
	_, stderr, err := runFakeHerdr(t, []string{"VXBENCH_PROMPT_MODE=silent"}, "agent", "prompt", "a", "text", "--wait", "--timeout", "1000",
		"--until", "working")
	if err == nil || !strings.Contains(stderr, `"code":"timeout"`) {
		t.Errorf("want the timeout error, got %q, %v", stderr, err)
	}
}

func TestFakeHerdrAgentGetReportsTheConfiguredStatus(t *testing.T) {
	out, _, err := runFakeHerdr(t, []string{"VXBENCH_AGENT_STATUS=idle"}, "agent", "get", "a")
	if err != nil || !strings.Contains(out, `"agent_status":"idle"`) {
		t.Errorf("want an idle agent, got %q, %v", out, err)
	}
	out, _, err = runFakeHerdr(t, nil, "agent", "get", "a")
	if err != nil || !strings.Contains(out, `"agent_status":"working"`) {
		t.Errorf("want a working agent by default, got %q, %v", out, err)
	}
}

func TestFakeHerdrRefusesWhatItDoesNotKnow(t *testing.T) {
	_, stderr, err := runFakeHerdr(t, nil, "pane", "split")
	if err == nil || !strings.Contains(stderr, "unsupported") {
		t.Errorf("an unknown call must fail loudly, got %q, %v", stderr, err)
	}
}

// The wake scenario flips the status while a sentinel polls, through a file.
func TestFakeHerdrStatusFileWinsAndMarksTheFirstIdle(t *testing.T) {
	dir := t.TempDir()
	statusFile, seenFile := filepath.Join(dir, "status"), filepath.Join(dir, "seen")
	env := []string{"VXBENCH_STATUS_FILE=" + statusFile, "VXBENCH_SEEN_FILE=" + seenFile, "VXBENCH_AGENT_STATUS=blocked"}

	out, _, err := runFakeHerdr(t, env, "agent", "get", "a")
	if err != nil || !strings.Contains(out, `"agent_status":"blocked"`) {
		t.Errorf("without the file the env var decides, got %q, %v", out, err)
	}
	if err := os.WriteFile(statusFile, []byte("working\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, err = runFakeHerdr(t, env, "agent", "get", "a")
	if err != nil || !strings.Contains(out, `"agent_status":"working"`) {
		t.Errorf("the file wins over the env var, got %q, %v", out, err)
	}
	if _, err := os.Stat(seenFile); err == nil {
		t.Error("a working soldier leaves no sighting")
	}

	if err := os.WriteFile(statusFile, []byte("idle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, err = runFakeHerdr(t, env, "agent", "get", "a")
	if err != nil || !strings.Contains(out, `"agent_status":"idle"`) {
		t.Errorf("want idle, got %q, %v", out, err)
	}
	first, err := os.Stat(seenFile)
	if err != nil {
		t.Fatal("the first idle answer must create the sighting file")
	}
	if _, _, err := runFakeHerdr(t, env, "agent", "get", "a"); err != nil {
		t.Fatal(err)
	}
	again, err := os.Stat(seenFile)
	if err != nil || !again.ModTime().Equal(first.ModTime()) {
		t.Error("later idle answers must not touch the sighting: its time is when the soldier was first seen finished")
	}
}

func TestFakeHerdrIntegrationStatusHasTheShapeDoctorParses(t *testing.T) {
	out, _, err := runFakeHerdr(t, nil, "integration", "status")
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range []string{"pi", "claude", "opencode"} {
		if !strings.Contains(out, agent+": current (v") {
			t.Errorf("want a %q line, got:\n%s", agent, out)
		}
	}
}

func runFake(t *testing.T, name string, env []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", append([]string{name}, args...)...)
	cmd.Env = env
	out, err := cmd.Output()
	return string(out), err
}

func TestFakePiAndOpenCodePrintASupportedVersion(t *testing.T) {
	if out, err := runFake(t, "pi", nil, "--version"); err != nil || out != "1.3.0\n" {
		t.Errorf("pi: %q, %v", out, err)
	}
	if out, err := runFake(t, "opencode", nil, "--version"); err != nil || out != "opencode v2.0.32\n" {
		t.Errorf("opencode: %q, %v", out, err)
	}
	if out, err := runFake(t, "gh", nil, "--version"); err != nil || !strings.HasPrefix(out, "gh version") {
		t.Errorf("gh: %q, %v", out, err)
	}
}

func TestFakePiAndOpenCodeCanBeMadeSlow(t *testing.T) {
	for _, tool := range []struct{ name, env string }{{"pi", "VXBENCH_PI_DELAY"}, {"opencode", "VXBENCH_OPENCODE_DELAY"}} {
		start := time.Now()
		if _, err := runFake(t, tool.name, []string{"PATH=" + os.Getenv("PATH"), tool.env + "=0.2"}, "--version"); err != nil {
			t.Fatal(err)
		}
		if took := time.Since(start); took < 200*time.Millisecond {
			t.Errorf("%s took %v, want at least the 200 ms delay", tool.name, took)
		}
	}
}

// runFakeHC runs the fake hc in dir (its state directory) and returns its
// stdout, stderr and exit code.
func runFakeHC(t *testing.T, dir string, env []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command("sh", append([]string{"hc"}, args...)...)
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "VXFAKE_HC_DIR=" + dir}, env...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return stdout.String(), stderr.String(), code
}

// The fake answers --version like the core, and can play the herdr TUI for the
// version gate.
func TestFakeHCVersion(t *testing.T) {
	out, _, code := runFakeHC(t, t.TempDir(), nil, "--version")
	if code != 0 || strings.TrimSpace(out) != "hc 0.1.16" {
		t.Errorf("--version = %q, %d", out, code)
	}
	out, _, _ = runFakeHC(t, t.TempDir(), []string{"VXFAKE_HC_VERSION_OUT=herdr 0.9.5"}, "--version")
	if strings.TrimSpace(out) != "herdr 0.9.5" {
		t.Errorf("the TUI impersonation = %q", out)
	}
}

// Errors carry the exit code and the {"error":{kind,message,hint}} body on
// stderr, which is what internal/herdr's parser reads.
func TestFakeHCErrors(t *testing.T) {
	dir := t.TempDir()
	_, stderr, code := runFakeHC(t, dir, nil, "get", "t9")
	if code != 3 || !strings.Contains(stderr, `"kind":"not_found"`) {
		t.Errorf("get of an unknown terminal = %d, %q; want exit 3 and not_found on stderr", code, stderr)
	}
	runFakeHC(t, dir, nil, "run", "--bg", "--json")
	_, stderr, code = runFakeHC(t, dir, nil, "rm", "t1")
	if code != 1 || !strings.Contains(stderr, `"kind":"live"`) {
		t.Errorf("rm of a live terminal = %d, %q; want exit 1 and live", code, stderr)
	}
	_, stderr, code = runFakeHC(t, dir, nil, "keys", "t1", "--", "+")
	if code != 6 || !strings.Contains(stderr, "invalid_params") {
		t.Errorf("keys '+' = %d, %q; want exit 6 and invalid_params", code, stderr)
	}
}

func TestFakeHCLabelsAndListing(t *testing.T) {
	dir := t.TempDir()
	out, _, _ := runFakeHC(t, dir, nil, "run", "--bg", "--json", "--label", "a=1", "--label", "b=2")
	if !strings.Contains(out, `"address":"t1"`) {
		t.Fatalf("run = %q", out)
	}
	runFakeHC(t, dir, nil, "run", "--bg", "--json", "--label", "a=1")

	out, _, _ = runFakeHC(t, dir, nil, "ls", "--json", "--label", "a=1")
	if strings.Count(out, `"address"`) != 2 {
		t.Errorf("ls a=1 = %s, want both terminals", out)
	}
	out, _, _ = runFakeHC(t, dir, nil, "ls", "--json", "--label", "a=1", "--label", "b=2")
	if strings.Count(out, `"address"`) != 1 || !strings.Contains(out, `"t1"`) {
		t.Errorf("ls a=1 b=2 = %s, want t1 only", out)
	}
	runFakeHC(t, dir, nil, "label", "t2", "b=2", "a=")
	out, _, _ = runFakeHC(t, dir, nil, "get", "t2")
	var got struct {
		Labels map[string]string `json:"labels"`
		Status any               `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("get is not JSON: %v\n%s", err, out)
	}
	if got.Labels["b"] != "2" || got.Labels["a"] != "" || got.Status != nil {
		t.Errorf("get after label = %+v, want b=2, a removed, status null", got)
	}
}

// A shell that is typed a launch line becomes an agent that reports idle; a
// prompt typed into it works, then is done with the turn counted.
func TestFakeHCAgentTurn(t *testing.T) {
	dir := t.TempDir()
	runFakeHC(t, dir, nil, "run", "--bg", "--json")
	runFakeHC(t, dir, nil, "send", "t1", "--", "env A=1 claude")
	out, _, _ := runFakeHC(t, dir, nil, "get", "t1")
	if !strings.Contains(out, `"status":{"app":"claude-code","state":"idle"}`) {
		t.Fatalf("after the launch line: %s", out)
	}
	out, _, code := runFakeHC(t, dir, []string{"VXFAKE_HC_WORK_SECS=0"}, "send", "t1", "--", "hello")
	if code != 0 || !strings.Contains(out, `"started":true`) || !strings.Contains(out, `"status":"idle"`) {
		t.Errorf("send = %q, %d", out, code)
	}
	time.Sleep(1100 * time.Millisecond)
	out, _, _ = runFakeHC(t, dir, nil, "ls", "--json")
	if !strings.Contains(out, `"status":"done"`) || !strings.Contains(out, `"turn":1`) {
		t.Errorf("after the turn: %s", out)
	}
}

// runFakeApp runs the fake herdr-app in dir (its state directory).
func runFakeApp(t *testing.T, dir string, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("sh", append([]string{"herdr-app"}, args...)...)
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "VXFAKE_APP_DIR=" + dir}, env...)
	var stdout strings.Builder
	cmd.Stdout = &stdout
	err := cmd.Run()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return stdout.String(), code
}

// The fake lists its tabs only from the Nth ls on, and records renames.
func TestFakeHerdrApp(t *testing.T) {
	dir := t.TempDir()
	env := []string{"VXFAKE_APP_TABS=a,b", "VXFAKE_APP_APPEAR_AFTER=2"}
	if out, _ := runFakeApp(t, dir, env, "ls"); strings.Contains(out, `"id":"a"`) {
		t.Errorf("the first ls must not show the tab yet: %q", out)
	}
	if out, _ := runFakeApp(t, dir, env, "ls"); !strings.Contains(out, `"id":"a"`) || !strings.Contains(out, `"id":"b"`) {
		t.Errorf("the second ls must show both tabs: %q", out)
	}
	if _, code := runFakeApp(t, dir, nil, "do", "tab.rename", "--id", "a", "--name", "vx-a"); code != 0 {
		t.Errorf("rename exit = %d", code)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "renames")); string(raw) != "a vx-a\n" {
		t.Errorf("renames = %q", raw)
	}
	if _, code := runFakeApp(t, dir, []string{"VXFAKE_APP_RENAME_FAIL=1"}, "do", "tab.rename", "--id", "a", "--name", "x"); code == 0 {
		t.Error("a refused rename must fail")
	}
	if _, code := runFakeApp(t, dir, []string{"VXFAKE_APP_LS_FAIL=1"}, "ls"); code == 0 {
		t.Error("ls with no window must fail")
	}
	if _, code := runFakeApp(t, dir, nil, "status"); code == 0 {
		t.Error("an unknown call must fail loudly")
	}
}
