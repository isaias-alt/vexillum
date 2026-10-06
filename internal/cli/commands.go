package cli

import (
	"fmt"
	"strings"

	"github.com/isaias-alt/vexillum/internal/cmdname"
)

// Command describes one vexillum subcommand. The registry below is the
// single source of truth for the command list: the general usage text
// printed by `vx --help`, the docs generator (tools/docgen), and the
// README command table all read from it. Each Usage value is the very same
// const the command prints for its own -h, so the two can never diverge.
type Command struct {
	// Name is the word typed after "vx".
	Name string
	// Summary is the one-line description shown in the command list. It
	// must be the opening words of Usage's first paragraph (a test
	// enforces it), so the two cannot drift apart.
	Summary string
	// Usage is the full help text printed by `vx <Name> -h`.
	Usage string
}

// commands is in display order: the order of `vx --help`. When adding
// a command, add its case to the switch in cmd/vx/main.go too - a test
// there fails if the two drift.
// Its Spanish summary and help go in tools/docgen/cli_es.go - a test there
// fails until they exist.
var commands = []Command{
	{"init", "Prepare the current project to be orchestrated by vexillum", initUsage},
	{"upgrade", "Update vx to the latest release, then refresh the project's vexillum scaffold", upgradeUsage},
	{"doctor", "Report on the health of the vexillum environment", doctorUsage},
	{"dispatch", "Dispatch a soldier (mission or scout) into an isolated camp", dispatchUsage},
	{"models", "List the model and effort profiles a dispatch can use", modelsUsage},
	{"yolo", "Turn yolo mode on or off for this project, or print whether it is on", yoloUsage},
	{"redispatch", "Re-dispatch an interrupted task from its original prompt", redispatchUsage},
	{"decide", "Answer a blocked task's open question so it can continue", decideUsage},
	{"prompt", "Send a follow-up prompt to a soldier that already finished", repromptUsage},
	{"pending", "Record and clear the commander's own pending decisions", pendingUsage},
	{"status", "Report the troop: every mission and scout in the current project", statusUsage},
	{"land", "Land a finished mission's work into this project's base branch", landUsage},
	{"ship", "Ship a finished mission through vexillum's own tribunal pipeline, opening a real pull request", shipUsage},
	{"strike", "Strike a soldier's camp (dismantle it and return it to the pool) once its work has landed", strikeUsage},
	{"sentinel", "Watch dispatched soldiers and record status changes", sentinelUsage},
	{"forum", "Open a local HTML artifact for visual review and collect the user's feedback", forumUsage},
}

// Commands returns a copy of the command registry, in display order.
func Commands() []Command {
	out := make([]Command, len(commands))
	copy(out, commands)
	return out
}

// GeneralUsage is the text printed by `vx`, `vx -h` and
// `vx --help`, with the command list rendered from the registry.
func GeneralUsage() string {
	width := 0
	for _, c := range commands {
		if len(c.Name) > width {
			width = len(c.Name)
		}
	}

	var b strings.Builder
	b.WriteString("vexillum is a CLI orchestrator for code agents.\n\n")
	b.WriteString("Usage:\n  " + cmdname.Name + " <command> [flags]\n\n")
	b.WriteString("Commands:\n")
	for _, c := range commands {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, c.Name, c.Summary)
	}
	b.WriteString("\nFlags:\n")
	b.WriteString("  -h, --help      Show this help message\n")
	b.WriteString("  -v, --version   Show version information\n")
	return b.String()
}
