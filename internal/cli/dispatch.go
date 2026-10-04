package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/models"
	"github.com/isaias-alt/vexillum/internal/scaffold"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

const dispatchUsage = `Dispatch a soldier (mission or scout) into an isolated camp.

Usage:
  ` + cmdname.Name + ` dispatch <prompt> [--kind mission|scout] [--profile <name>] [--model <model>] [--effort <level>]

A mission changes code and delivers something to land; a scout only
investigates and reports back (default: mission).

--model and --effort are passed straight through to the real claude CLI
(--model: haiku, sonnet, opus, fable; --effort: low, medium, high,
xhigh, max). vexillum only validates the value is one claude accepts -
it never picks one for you.

--profile <name> looks the model and effort up in the profile table
('` + cmdname.Name + ` models' lists it: <project>/.vexillum/models.json over the
optional ~/.vexillum/models.json over the built-in defaults). An explicit
--model or --effort still wins over the profile. An unknown profile is
an error listing the valid ones; "default" selects the table's default
entry. Which profile fits a task is your call. See the vexillum skill.

Runs a real, interactive Claude Code session in a herdr pane, inside a
fresh git worktree isolated from this project's own working tree, with
--dangerously-skip-permissions: the soldier has full host access under
the invoking OS user (no container, chroot, or other sandbox) - the
worktree only bounds where its commits land, not what it can read,
write, or exfiltrate elsewhere on the machine. Nothing reaches the
project's real history until '` + cmdname.Name + ` land' is explicitly approved;
never dispatch against a prompt, repository, or machine where reading
sensitive host state would be a problem. Requires HERDR_WORKSPACE_ID -
run this from inside a herdr-managed pane.

Returns quickly: it only waits out a short quick-settle probe, not the
soldier's whole task. A trivial prompt may finish within that window and
report its result immediately; anything else is left running and
auto-starts a sentinel (if one isn't already watching this project) to
record its final status. See '` + cmdname.Name + ` sentinel'.
`

// Dispatch runs the "vx dispatch" command.
func Dispatch(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(dispatchUsage)
		return 0
	}

	prompt, kind, model, effort, profile, err := parseDispatchArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}

	projectDir, vexillumHome, err := resolveDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}

	model, effort, err = resolveProfile(projectDir, vexillumHome, profile, model, effort)
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}
	if err := soldier.ValidateModelEffort(model, effort); err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}

	workspaceID := os.Getenv("HERDR_WORKSPACE_ID")
	if workspaceID == "" {
		fmt.Fprintln(os.Stderr, cmdname.Name+": HERDR_WORKSPACE_ID is not set - dispatch must run from inside a herdr-managed pane")
		return 1
	}

	ensureSentinelRunning(vexillumHome, os.Stderr)

	return runDispatch(projectDir, vexillumHome, workspaceID, prompt, kind, model, effort, herdr.CLI{}, os.Stdout, os.Stderr)
}

// resolveProfile fills model and effort from the named profile, leaving any
// value the caller passed explicitly untouched. An empty profile is a no-op.
func resolveProfile(projectDir, vexillumHome, profile, model, effort string) (string, string, error) {
	if profile == "" {
		return model, effort, nil
	}
	table, err := models.Load(projectDir, vexillumHome)
	if err != nil {
		return "", "", err
	}
	choice, err := table.Lookup(profile)
	if err != nil {
		return "", "", err
	}
	if model == "" {
		model = choice.Model
	}
	if effort == "" {
		effort = choice.Effort
	}
	return model, effort, nil
}

func parseDispatchArgs(args []string) (prompt string, kind state.Kind, model, effort, profile string, err error) {
	kind = state.KindMission
	var promptParts []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--kind":
			if i+1 >= len(args) {
				return "", "", "", "", "", fmt.Errorf("--kind requires a value (mission or scout)")
			}
			i++
			switch args[i] {
			case "mission":
				kind = state.KindMission
			case "scout":
				kind = state.KindScout
			default:
				return "", "", "", "", "", fmt.Errorf("unknown kind %q, expected mission or scout", args[i])
			}
		case "--model":
			if i+1 >= len(args) {
				return "", "", "", "", "", fmt.Errorf("--model requires a value")
			}
			i++
			model = args[i]
		case "--profile":
			if i+1 >= len(args) {
				return "", "", "", "", "", fmt.Errorf("--profile requires a value (see '%s models')", cmdname.Name)
			}
			i++
			profile = args[i]
		case "--effort":
			if i+1 >= len(args) {
				return "", "", "", "", "", fmt.Errorf("--effort requires a value")
			}
			i++
			effort = args[i]
		default:
			promptParts = append(promptParts, args[i])
		}
	}
	if len(promptParts) == 0 {
		return "", "", "", "", "", fmt.Errorf("missing prompt")
	}
	return strings.Join(promptParts, " "), kind, model, effort, profile, nil
}

func runDispatch(projectDir, vexillumHome, workspaceID, prompt string, kind state.Kind, model, effort string, client herdr.Client, stdout, stderr io.Writer) int {
	if !scaffold.ProjectInitialized(projectDir) {
		fmt.Fprintln(stderr, cmdname.Name+": project not initialized, run '"+cmdname.Name+" init' first")
		return 1
	}

	task, err := state.New(kind, prompt)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": creating task: %v\n", err)
		return 1
	}
	task.Model = model
	task.Effort = effort

	return acquireAndRunInHerdr(projectDir, vexillumHome, workspaceID, task, client, stdout, stderr)
}
