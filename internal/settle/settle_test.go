package settle_test

import (
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/settle"
)

const footer = "\n\n───────────────\n❯\n───────────────\n  Haiku 4.5 | Context: 21% used of 200k\n  paste again to expand\n"

func TestBackgroundWorkPending(t *testing.T) {
	cases := []struct {
		name    string
		output  string
		want    string
		pending bool
	}{
		{"one shell", "⏺ waiting for background job\n\n✻ Brewed for 6s · done 3:37 PM · 1 shell still running" + footer, "1 shell still running", true},
		{"several agents", "✽ Worked for 2m 1s · 3 local agents still running" + footer, "3 local agents still running", true},
		{"turn over, nothing running", "⏺ done\n\n✻ Sautéed for 3s · done 3:38 PM" + footer, "", false},
		{"no summary line", "just some text\n", "", false},
		{"empty", "", "", false},
		{"old turn scrolled far above", "✻ Brewed for 6s · done 3:37 PM · 1 shell still running\n" + strings.Repeat("later line\n", 30) + "✻ Sautéed for 3s · done 3:38 PM" + footer, "", false},
		{"earlier turn right above a finished one", "✻ Brewed for 6s · done 3:37 PM · 1 shell still running\n\n⏺ Background command completed\n\n⏺ report\n\n✻ Sautéed for 3s · done 3:38 PM" + footer, "", false},
		{"prose mentioning it without the separator", "the server is still running in the background\n", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, pending := settle.BackgroundWorkPending(tc.output)
			if pending != tc.pending || got != tc.want {
				t.Errorf("BackgroundWorkPending = %q, %v; want %q, %v", got, pending, tc.want, tc.pending)
			}
		})
	}
}

func TestTurnInProgress(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{"spinner line", "⏺ Bash(go test ./...)\n\n· Tomfoolering… (5m 30s · ↓ 34.2k tokens · still thinking with high effort)" + footer, true},
		{"running tool", "⏺ Bash(go test ./...)\n  ⎿  Running… (3m 12s)\n\n❯\n", true},
		{"spinner under an hours timer", "✻ Cogitating… (1h 2m 3s · ↑ 3k tokens)" + footer, true},
		{"esc to interrupt hint", "⏺ Reading\n\n  esc to interrupt\n❯\n", true},
		{"finished turn", "⏺ done\n\n✻ Sautéed for 3s · done 3:38 PM" + footer, false},
		{"finished turn above a spinner is history", "· Tomfoolering… (5m 30s)\n\n⏺ report\n\n✻ Sautéed for 3s · done 3:38 PM" + footer, false},
		{"old spinner scrolled far above", "· Tomfoolering… (5m 30s)\n" + strings.Repeat("later line\n", 30) + "❯\n", false},
		{"prose with an ellipsis", "the build is slow… (it took a while)\n", false},
		{"no summary line", "just some text\n", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line, got := settle.TurnInProgress(tc.output)
			if got != tc.want || (got && line == "") {
				t.Errorf("TurnInProgress = %q, %v; want %v", line, got, tc.want)
			}
		})
	}
}

func TestTurnNotOver(t *testing.T) {
	cases := []struct {
		name    string
		output  string
		want    bool
		contain string
	}{
		{"in progress", "· Tomfoolering… (5m 30s)" + footer, true, "turn in progress"},
		{"background work", "✻ Brewed for 6s · done 3:37 PM · 1 shell still running" + footer, true, "1 shell still running"},
		{"over", "✻ Sautéed for 3s · done 3:38 PM" + footer, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			why, got := settle.TurnNotOver(tc.output)
			if got != tc.want || !strings.Contains(why, tc.contain) {
				t.Errorf("TurnNotOver = %q, %v; want %v containing %q", why, got, tc.want, tc.contain)
			}
		})
	}
}
