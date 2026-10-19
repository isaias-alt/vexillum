package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/models"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/scaffold"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

const dispatchUsage = `Dispatch a soldier (mission or scout) into an isolated camp.

Usage:
  ` + cmdname.Name + ` dispatch <prompt> [--kind mission|scout] [--profile <name>] [--model <model>] [--effort <level>]

A mission changes code and delivers something to land; a scout only
investigates and reports back (default: mission).

--model and --effort are passed straight through to the harness's real CLI.
On claude (the default): --model haiku, sonnet, opus or fable; --effort low,
medium, high, xhigh or max. On a profile that selects the pi harness: --model
is a provider/id such as anthropic/claude-sonnet-5 (it becomes pi --model) and
--effort is a thinking level, off, minimal, low, medium, high, xhigh or max (it
becomes pi --thinking, which pi clamps to what the model supports). vexillum
only validates the value has the right shape for the harness - it never picks
one for you.

--profile <name> looks the model and effort up in the profile table
('` + cmdname.Name + ` models' lists it: <project>/.vexillum/models.json over the
optional ~/.vexillum/models.json over the built-in defaults). An explicit
--model or --effort still wins over the profile. An unknown profile is
an error listing the valid ones; "default" selects the table's default
entry. Which profile fits a task is your call. See the vexillum skill.

A profile may also name the harness (the agent CLI) its soldier runs on, in
models.json: "claude" (the default when it names none), "pi" or "opencode". The
harness is recorded on the task and kept by '` + cmdname.Name + ` redispatch'. A pi
soldier is answered as free text when it blocks (it has no question selector
to press a key in), and needs the pi CLI on PATH.

On a profile that selects the opencode harness, --model is provider/model with
an optional #variant, such as opencode/glm-5.3-flash-free or
anthropic/claude-sonnet-5#high ('opencode models' lists them), and --effort is
only a label that is recorded on the task: it is never turned into an OpenCode
variant, because variant names differ per model. Both OpenCode 1.x (from
1.18.29) and 2.x are driven; the version found on PATH is detected at dispatch
and recorded, and re-dispatch, prompt and decide keep using it. On 2.x the
soldier's model goes through .opencode/agents/vx-soldier.md and opencode.json,
which are written into the camp and excluded from git; if the project already
has an opencode.json, opencode.jsonc or .opencode/agents/vx-soldier.md,
dispatch fails naming it, because vexillum never overwrites or merges a
project's OpenCode configuration. An opencode soldier is answered as free
text when it asks a question, and needs the opencode CLI on PATH. See
'` + cmdname.Name + ` doctor'.

Runs a real, interactive session of the harness (Claude Code by default, with
--dangerously-skip-permissions; pi has no permission prompts to skip and is
started with --approve so it never stops to ask whether to trust the camp) in
a herdr pane, inside a fresh git worktree isolated from this project's own
working tree: the soldier has full host access under
the invoking OS user (no container, chroot, or other sandbox) - the
worktree only bounds where its commits land, not what it can read,
write, or exfiltrate elsewhere on the machine. Nothing reaches the
project's real history until '` + cmdname.Name + ` land' is explicitly approved;
never dispatch against a prompt, repository, or machine where reading
sensitive host state would be a problem. Requires HERDR_WORKSPACE_ID (run
this from inside a herdr-managed pane) and both herdr and the harness's CLI
(claude, pi or opencode) on PATH; if any is missing it says so and exits before
creating anything ('` + cmdname.Name + ` doctor' checks your setup).

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

	model, effort, harness, err := resolveProfile(projectDir, vexillumHome, profile, model, effort)
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}
	if err := soldier.ValidateModelEffort(harness, model, effort); err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}

	workspaceID, err := harnessPreconditions("dispatch", harness, os.Getenv, exec.LookPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}

	ensureSentinelRunning(vexillumHome, os.Stderr)
	registerDispatchCommander(projectDir, vexillumHome)

	return runDispatch(projectDir, vexillumHome, workspaceID, prompt, kind, model, effort, harness, herdr.CLI{}, os.Stdout, os.Stderr)
}

// registerDispatchCommander registers the commander that ran "dispatch" in its
// project's registry. Nothing is written for a project that is not initialized,
// which runDispatch refuses anyway.
func registerDispatchCommander(projectDir, vexillumHome string) {
	if !scaffold.ProjectInitialized(projectDir) {
		return
	}
	projectRoot, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		return
	}
	registerCommander(projectRoot, projectDir, os.Stderr)
}

// resolveProfile fills model and effort from the named profile, leaving any
// value the caller passed explicitly untouched, and returns the profile's
// harness name ("" when it sets none, which means claude). An empty profile
// is a no-op that returns the caller's values and no harness.
func resolveProfile(projectDir, vexillumHome, profile, model, effort string) (string, string, string, error) {
	if profile == "" {
		return model, effort, "", nil
	}
	table, err := models.Load(projectDir, vexillumHome)
	if err != nil {
		return "", "", "", err
	}
	choice, err := table.Lookup(profile)
	if err != nil {
		return "", "", "", err
	}
	if model == "" {
		model = choice.Model
	}
	if effort == "" {
		effort = choice.Effort
	}
	return model, effort, choice.Harness, nil
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

// runDispatch creates the task and runs it. harness is the name the chosen
// profile resolved to ("" means claude); it is recorded on the task as the
// harness its soldier, and later its re-dispatch and its tribunal, run on.
func runDispatch(projectDir, vexillumHome, workspaceID, prompt string, kind state.Kind, model, effort, harness string, client herdr.Client, stdout, stderr io.Writer) int {
	if !scaffold.ProjectInitialized(projectDir) {
		fmt.Fprintln(stderr, cmdname.Name+": project not initialized, run '"+cmdname.Name+" init' first")
		return 1
	}
	if harness == "" {
		harness = soldier.HarnessClaude
	}
	if _, err := soldier.HarnessFor(harness); err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}

	task, err := state.New(kind, prompt)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": creating task: %v\n", err)
		return 1
	}
	task.Model = model
	task.Effort = effort
	task.Harness = harness
	// Detected now and recorded, so everything later that drives this soldier
	// (re-dispatch, prompt, decide, the tribunal) uses the same generation of
	// the CLI, whatever is installed by then.
	major, version, err := soldier.DetectHarnessVersion(harness)
	if err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}
	task.HarnessMajor = major
	task.HarnessVersion = version

	return acquireAndRunInHerdr(projectDir, vexillumHome, workspaceID, task, client, stdout, stderr)
}
