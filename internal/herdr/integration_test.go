package herdr

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// A trimmed capture of `herdr integration status` from herdr 0.9.5.
const integrationStatusSample = `pi: not installed (/home/me/.pi/agent/extensions/herdr-agent-state.ts)
omp: not installed (/home/me/.omp/agent/extensions/herdr-omp-agent-state.ts)
claude: outdated (v8 < v11) (/home/me/.claude/hooks/herdr-agent-state.sh)
opencode: installed (v9) (/home/me/.config/opencode/plugins/herdr-agent-state.js)
letta (experimental): not installed (/home/me/.letta/hooks/herdr-agent-session.sh)
`

func TestParseIntegrationStatus(t *testing.T) {
	tests := []struct {
		name       string
		out        string
		agent      string
		want       IntegrationState
		wantDetail string
		wantErr    bool
	}{
		{name: "not installed", out: integrationStatusSample, agent: "pi", want: IntegrationNotInstalled, wantDetail: "not installed (/home/me/.pi/agent/extensions/herdr-agent-state.ts)"},
		{name: "outdated", out: integrationStatusSample, agent: "claude", want: IntegrationOutdated, wantDetail: "outdated (v8 < v11) (/home/me/.claude/hooks/herdr-agent-state.sh)"},
		{name: "installed, whatever the wording", out: integrationStatusSample, agent: "opencode", want: IntegrationInstalled, wantDetail: "installed (v9) (/home/me/.config/opencode/plugins/herdr-agent-state.js)"},
		{name: "an agent whose name has a qualifier", out: integrationStatusSample, agent: "letta", want: IntegrationNotInstalled, wantDetail: "not installed (/home/me/.letta/hooks/herdr-agent-session.sh)"},
		{name: "a name that is only a prefix does not match", out: integrationStatusSample, agent: "o", wantErr: true},
		{name: "no line for the agent", out: integrationStatusSample, agent: "gemini", wantErr: true},
		{name: "empty output", out: "", agent: "pi", wantErr: true},
		{name: "a line with no name before the colon", out: ": nothing\n", agent: "pi", wantErr: true},
		{name: "windows line endings", out: "pi: installed (v9)\r\n", agent: "pi", want: IntegrationInstalled, wantDetail: "installed (v9)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, detail, err := parseIntegrationStatus(tt.out, tt.agent)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("got %v, %q, nil; want an error", got, detail)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want || detail != tt.wantDetail {
				t.Errorf("got %v, %q; want %v, %q", got, detail, tt.want, tt.wantDetail)
			}
		})
	}
}

// One reader runs `herdr integration status` once, whoever asks and however
// many ask at the same time, and each caller reads its own agent's line.
func TestIntegrationReader_RunsHerdrOnce(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := "#!/bin/sh\necho x >> '" + log + "'\nprintf '%s\\n' 'pi: not installed (/p)' 'opencode: installed (v9) (/o)'\n"
	if err := os.WriteFile(filepath.Join(dir, "herdr"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	var r IntegrationReader
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if state, _, err := r.Status("pi"); err != nil || state != IntegrationNotInstalled {
				t.Errorf("pi = %v, %v", state, err)
			}
		}()
		go func() {
			defer wg.Done()
			if state, _, err := r.Status("opencode"); err != nil || state != IntegrationInstalled {
				t.Errorf("opencode = %v, %v", state, err)
			}
		}()
	}
	wg.Wait()
	if _, _, err := r.Status("claude"); err == nil {
		t.Error("an agent with no line should be an error")
	}

	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(b), "x"); got != 1 {
		t.Errorf("herdr integration status ran %d times, want 1", got)
	}
}

// A herdr that fails fails every question, from the one run.
func TestIntegrationReader_FailureIsTheAnswerForEveryAgent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "herdr"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	var r IntegrationReader
	for _, name := range []string{"pi", "opencode"} {
		if _, _, err := r.Status(name); err == nil || !strings.Contains(err.Error(), "running herdr integration status") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}
