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
