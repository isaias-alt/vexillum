package cli

import (
	"fmt"
	"strings"
)

// Command describes one vexillum subcommand. The registry below is the
// single source of truth for the command list: the general usage text
// printed by `vexillum --help`, the docs generator (tools/docgen), and the
// README command table all read from it. Each Usage value is the very same
// const the command prints for its own -h, so the two can never diverge.
type Command struct {
	// Name is the word typed after "vexillum".
	Name string
	// Summary is the one-line description shown in the command list.
	Summary string
	// Usage is the full help text printed by `vexillum <Name> -h`.
	Usage string
}

// commands is in display order: the order of `vexillum --help`. When adding
// a command, add its case to the switch in cmd/vexillum/main.go too - a test
// there fails if the two drift.
var commands = []Command{
	{"init", "Prepare the current project to be orchestrated by vexillum", initUsage},
	{"upgrade", "Refresh an already-initialized project's scaffold to the latest", upgradeUsage},
	{"doctor", "Report on the health of the vexillum environment", doctorUsage},
	{"dispatch", "Dispatch a soldier (mission or scout) into an isolated camp", dispatchUsage},
	{"redispatch", "Re-dispatch an interrupted task from its original prompt", redispatchUsage},
	{"decide", "Answer a blocked task's open question so it can continue", decideUsage},
	{"status", "Report the current project's fleet of tasks (read-only)", statusUsage},
	{"land", "Land a finished mission's work into the base branch", landUsage},
	{"ship", "Push a finished mission through vexillum's own tribunal pipeline for a real PR", shipUsage},
	{"release", "Release a soldier's camp back to the pool", releaseUsage},
	{"sentinel", "Watch dispatched soldiers and record status changes", sentinelUsage},
	{"forum", "Open an HTML artifact for visual review and collect the user's feedback", forumUsage},
	{"banner", "Publish an HTML artifact to a public URL, or update one already published", bannerUsage},
}

// Commands returns a copy of the command registry, in display order.
func Commands() []Command {
	out := make([]Command, len(commands))
	copy(out, commands)
	return out
}

// GeneralUsage is the text printed by `vexillum`, `vexillum -h` and
// `vexillum --help`, with the command list rendered from the registry.
func GeneralUsage() string {
	width := 0
	for _, c := range commands {
		if len(c.Name) > width {
			width = len(c.Name)
		}
	}

	var b strings.Builder
	b.WriteString("vexillum is a CLI orchestrator for code agents.\n\n")
	b.WriteString("Usage:\n  vexillum <command> [flags]\n\n")
	b.WriteString("Commands:\n")
	for _, c := range commands {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, c.Name, c.Summary)
	}
	b.WriteString("\nFlags:\n")
	b.WriteString("  -h, --help      Show this help message\n")
	b.WriteString("  -v, --version   Show version information\n")
	return b.String()
}
