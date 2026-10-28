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
