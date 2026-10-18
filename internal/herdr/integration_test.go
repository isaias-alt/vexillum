package herdr

import "testing"

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
