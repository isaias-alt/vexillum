package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/scaffold"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

const dispatchUsage = `Dispatch a soldier (mission or scout) into an isolated camp.

Usage:
  vexillum dispatch <prompt> [--kind mission|scout] [--model <model>] [--effort <level>]

A mission changes code and delivers something to land; a scout only
investigates and reports back (default: mission).

--model and --effort are passed straight through to the real claude CLI
(--model: haiku, sonnet, opus, fable; --effort: low, medium, high,
xhigh, max). vexillum only validates the value is one claude accepts -
it never picks one for you; see the product AGENTS.md's "Choosing a
model and effort" section for how to choose.

Runs a real, interactive Claude Code session in a herdr pane, inside a
fresh git worktree isolated from this project's own working tree, with
--dangerously-skip-permissions (the worktree isolation bounds the blast
radius; nothing reaches the project's real history until 'vexillum land'
is explicitly approved). Requires HERDR_WORKSPACE_ID - run this from
inside a herdr-managed pane.

Returns quickly: it only waits out a short quick-settle probe, not the
soldier's whole task. A trivial prompt may finish within that window and
report its result immediately; anything else is left running and
auto-starts a sentinel (if one isn't already watching this project) to
record its final status - see 'vexillum sentinel'.
`

// Dispatch runs the "vexillum dispatch" command.
func Dispatch(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(dispatchUsage)
		return 0
	}

	prompt, kind, model, effort, err := parseDispatchArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		return 1
	}
	if err := soldier.ValidateModelEffort(model, effort); err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		return 1
	}

	projectDir, vexillumHome, err := resolveDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		return 1
	}

	workspaceID := os.Getenv("HERDR_WORKSPACE_ID")
	if workspaceID == "" {
		fmt.Fprintln(os.Stderr, "vexillum: HERDR_WORKSPACE_ID is not set - dispatch must run from inside a herdr-managed pane")
		return 1
	}

	ensureSentinelRunning(vexillumHome, os.Stderr)

	return runDispatch(projectDir, vexillumHome, workspaceID, prompt, kind, model, effort, herdr.CLI{}, os.Stdout, os.Stderr)
}

func parseDispatchArgs(args []string) (prompt string, kind state.Kind, model, effort string, err error) {
	kind = state.KindMission
	var promptParts []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--kind":
			if i+1 >= len(args) {
				return "", "", "", "", fmt.Errorf("--kind requires a value (mission or scout)")
			}
			i++
			switch args[i] {
			case "mission":
				kind = state.KindMission
			case "scout":
				kind = state.KindScout
			default:
				return "", "", "", "", fmt.Errorf("unknown kind %q, expected mission or scout", args[i])
			}
		case "--model":
			if i+1 >= len(args) {
				return "", "", "", "", fmt.Errorf("--model requires a value")
			}
			i++
			model = args[i]
		case "--effort":
			if i+1 >= len(args) {
				return "", "", "", "", fmt.Errorf("--effort requires a value")
			}
			i++
			effort = args[i]
		default:
			promptParts = append(promptParts, args[i])
		}
	}
	if len(promptParts) == 0 {
		return "", "", "", "", fmt.Errorf("missing prompt")
	}
	return strings.Join(promptParts, " "), kind, model, effort, nil
}

func runDispatch(projectDir, vexillumHome, workspaceID, prompt string, kind state.Kind, model, effort string, client herdr.Client, stdout, stderr io.Writer) int {
	if !scaffold.ProjectInitialized(projectDir) {
		fmt.Fprintln(stderr, "vexillum: project not initialized, run 'vexillum init' first")
		return 1
	}

	task, err := state.New(kind, prompt)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: creating task: %v\n", err)
		return 1
	}
	task.Model = model
	task.Effort = effort

	return acquireAndRunInHerdr(projectDir, vexillumHome, workspaceID, task, client, stdout, stderr)
}
