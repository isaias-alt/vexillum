package fakes

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
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
