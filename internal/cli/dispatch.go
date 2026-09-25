package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/report"
	"github.com/isaias-alt/vexillum/internal/sentinel"
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
--dangerously-skip-permissions: the soldier has full host access under
the invoking OS user (no container, chroot, or other sandbox) - the
worktree only bounds where its commits land, not what it can read,
write, or exfiltrate elsewhere on the machine. Nothing reaches the
project's real history until 'vexillum land' is explicitly approved;
never dispatch against a prompt, repository, or machine where reading
sensitive host state would be a problem. Requires HERDR_WORKSPACE_ID -
run this from inside a herdr-managed pane.

Returns quickly: it only waits out a short quick-settle probe, not the
soldier's whole task. A trivial prompt may finish within that window and
report its result immediately; anything else is left running and
auto-starts a sentinel (if one isn't already watching this project) to
record its final status - see 'vexillum sentinel'.
`

const landUsage = `Land a finished mission's work into this project's base branch.

Usage:
  vexillum land <task-id>

Fast-forwards this project's own checkout to the mission's branch.
Refuses (leaving everything untouched) unless this checkout is clean and
the merge is a clean fast-forward - never forces or rebases anything.

For a task already shipped through the no-mistakes gate ('vexillum
ship'), this instead merges the real pull request on GitHub - the camp's
own branch is no longer the source of truth once no-mistakes may have
applied fixes to it in its own isolated worktree. Requires "gh". Refuses
unless the pull request is open, not a draft, mergeable, and every check
is green; the merge is bound to the exact head just verified.
`

const releaseUsage = `Release a soldier's camp back to the pool once its work has landed.

Usage:
  vexillum release <task-id> [--force]

Refuses unless the camp is clean and (for a mission) landed. For a scout,
also refuses unless its final report exists at
~/.vexillum/projects/<project>/reports/<agent-name>.md - the report is
the scout's work product, the same way a mission's is its landed commit.
--force skips the report check (never the clean/landed camp check) - an
explicit, logged escape hatch, never silent.

On success, returns the worktree to the pool for reuse and closes the
herdr pane.
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

// ensureSentinelRunning best-effort auto-starts "vexillum sentinel"
// detached in the background if one isn't already watching vexillumHome.
// Dispatch now returns after only a short quick-settle probe (see
// soldier.RunInHerdr) - for anything but a trivial prompt, the sentinel
// is what eventually records the soldier's real outcome, so a dispatch
// with no sentinel running would otherwise strand that task Running
// forever. A failure here is reported but never fails dispatch itself:
// the soldier is already started regardless: 'vexillum sentinel' remains
// available to start by hand if this doesn't work.
func ensureSentinelRunning(vexillumHome string, stderr io.Writer) {
	if sentinel.IsRunning(vexillumHome) {
		return
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: warning: could not auto-start the sentinel: %v\n", err)
		return
	}

	logPath := filepath.Join(vexillumHome, "sentinel.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: warning: could not auto-start the sentinel: %v\n", err)
		return
	}
	defer logFile.Close()

	cmd := exec.Command(exe, "sentinel")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	// Detach from this process's session so the sentinel survives long
	// after this dispatch invocation (and whatever shell launched it)
	// exits.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "vexillum: warning: could not auto-start the sentinel: %v\n", err)
		return
	}
	fmt.Fprintf(stderr, "vexillum: auto-started sentinel (pid %d), logging to %s\n", cmd.Process.Pid, logPath)
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
	if !projectAlreadyInitialized(projectDir) {
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

// acquireAndRunInHerdr acquires a camp for task and runs it in a real
// herdr pane - the exact sequence both a fresh dispatch and a re-dispatch
// (internal/cli.runRedispatch) go through once task is ready to run, kept
// in one place so the two commands can't drift out of step with each
// other.
//
// task is persisted with its camp fields set the moment camp.Acquire
// returns, not left to soldier.RunInHerdr's own first save (which only
// happens after a successful CreateTab). camp.Acquire durably leases a
// pool slot to task.ID in pool.json; if that lease succeeds but CreateTab
// then fails, RunInHerdr returns before saving anything of its own -
// without this earlier save, that would leave a pool slot permanently
// leased to a task ID no task file on disk ever references (unrecoverable,
// since both 'vexillum release' and 'vexillum redispatch' require
// state.Load - and, to resolve the right camp, a populated CampSlot - to
// succeed first). Saving the camp fields here, before RunInHerdr is even
// called, means a CreateTab failure still leaves a loadable task that
// already knows which slot it owns; the fallback save below then marks it
// Failed so it can be released normally (the worktree has no commits yet,
// so camp.Release's landed-check passes trivially). Re-dispatch
// (runRedispatch) already saves task in its own reset state before
// calling this, but with zeroed camp fields (it hasn't acquired a fresh
// camp yet at that point) - this save is what records the new camp it
// gets here, same as a fresh dispatch.
func acquireAndRunInHerdr(projectDir, vexillumHome, workspaceID string, task state.Task, client herdr.Client, stdout, stderr io.Writer) int {
	projectRoot, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: resolving project root: %v\n", err)
		return 1
	}

	c, err := camp.Acquire(projectDir, vexillumHome, task.ID)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: acquiring camp: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "task_id=%s kind=%s camp_slot=%d camp_branch=%s\n", task.ID, task.Kind, c.Slot, c.Branch)

	task.CampSlot = c.Slot
	task.CampPath = c.Path
	task.CampBranch = c.Branch
	task.UpdatedAt = time.Now().UTC()
	if err := state.Save(projectRoot, task); err != nil {
		// The slot camp.Acquire just leased above is durable in pool.json
		// regardless of whether this save succeeds - if it's left leased
		// with no task file ever referencing it, that's the exact leak
		// this function exists to prevent, just moved one step earlier.
		// The worktree is still fresh (no commits, clean), so Release's
		// landed-check passes trivially - give the slot back rather than
		// stranding it.
		fmt.Fprintf(stderr, "vexillum: persisting acquired camp: %v\n", err)
		if releaseErr := camp.Release(c, task.ID); releaseErr != nil {
			fmt.Fprintf(stderr, "vexillum: releasing camp slot %d after failed save: %v\n", c.Slot, releaseErr)
		}
		return 1
	}

	result, runErr := soldier.RunInHerdr(vexillumHome, workspaceID, task, c, client)
	fmt.Fprintf(stdout, "status=%s\n", result.Status)
	if result.HerdrPaneID != "" {
		fmt.Fprintf(stdout, "herdr: workspace=%s tab=%s pane=%s agent=%s\n", result.HerdrWorkspaceID, result.HerdrTabID, result.HerdrPaneID, result.HerdrAgentName)
	}
	if result.Status == state.StatusRunning {
		fmt.Fprintln(stdout, "still running past the quick-settle probe - the sentinel will record its final status")
	}
	if result.Output != "" {
		fmt.Fprintln(stdout, "output:")
		fmt.Fprintln(stdout, result.Output)
	}

	if runErr != nil {
		if result.HerdrTabID == "" {
			// RunInHerdr never got past CreateTab (the only failure path
			// that leaves HerdrTabID unset - every other one, e.g.
			// startAgent or the final save, sets it first) - so it never
			// reached any of its own save points, and the only persisted
			// state for this task is the one above. Force it to Failed and
			// persist so this task is left in a normal, releasable state
			// instead of stuck Pending with a camp slot nothing else can
			// find its way back to. A later failure (after RunInHerdr's own
			// saves already ran) is left as RunInHerdr recorded it - it
			// already reflects the task's real outcome, and overwriting it
			// here would discard that.
			result.Status = state.StatusFailed
			result.Output = runErr.Error()
			result.UpdatedAt = time.Now().UTC()
			if saveErr := state.Save(projectRoot, result); saveErr != nil {
				fmt.Fprintf(stderr, "vexillum: persisting failed state (after: %v): %v\n", runErr, saveErr)
				return 1
			}
		}
		fmt.Fprintf(stderr, "vexillum: %v\n", runErr)
		return 1
	}
	return 0
}

// Land runs the "vexillum land" command.
func Land(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(landUsage)
		return 0
	}
	if len(args) == 0 {
		fmt.Print(landUsage)
		return 1
	}

	projectDir, vexillumHome, err := resolveDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		return 1
	}

	return runLand(projectDir, vexillumHome, args[0], os.Stdout, os.Stderr)
}

func runLand(projectDir, vexillumHome, taskID string, stdout, stderr io.Writer) int {
	projectRoot, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		fmt.Fprintln(stderr, "vexillum:", err)
		return 1
	}

	task, err := state.Load(projectRoot, taskID)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: loading task %s: %v\n", taskID, err)
		return 1
	}

	if task.Status == state.StatusShipped {
		return mergeShippedPR(projectDir, task, stdout, stderr)
	}

	c, err := camp.Resolve(projectDir, vexillumHome, task.CampSlot)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: resolving camp: %v\n", err)
		return 1
	}

	if err := camp.Land(c); err != nil {
		fmt.Fprintf(stderr, "vexillum: land refused: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "landed: fast-forwarded %s to %s\n", projectDir, c.Branch)
	return 0
}

// Release runs the "vexillum release" command.
func Release(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(releaseUsage)
		return 0
	}
	if len(args) == 0 {
		fmt.Print(releaseUsage)
		return 1
	}

	taskID, force, err := parseReleaseArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		return 1
	}

	projectDir, vexillumHome, err := resolveDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		return 1
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum: cannot determine home directory:", err)
		return 1
	}

	return runRelease(projectDir, vexillumHome, homeDir, taskID, force, herdr.CLI{}, os.Stdout, os.Stderr)
}

func parseReleaseArgs(args []string) (taskID string, force bool, err error) {
	for _, a := range args {
		if a == "--force" {
			force = true
			continue
		}
		if taskID != "" {
			return "", false, fmt.Errorf("unexpected extra argument %q", a)
		}
		taskID = a
	}
	if taskID == "" {
		return "", false, fmt.Errorf("missing task id")
	}
	return taskID, force, nil
}

func runRelease(projectDir, vexillumHome, homeDir, taskID string, force bool, client herdr.Client, stdout, stderr io.Writer) int {
	projectRoot, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		fmt.Fprintln(stderr, "vexillum:", err)
		return 1
	}

	task, err := state.Load(projectRoot, taskID)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: loading task %s: %v\n", taskID, err)
		return 1
	}

	// A scout's report is its work product, the same way a mission's
	// landed commit is (checked by camp.Release itself, via
	// soldier.ReleaseInHerdr below) - a mission never has one to check
	// (internal/report's package doc), so this only ever applies to a
	// scout. --force is the explicit, logged escape hatch, mirroring
	// firstmate's own teardown gate ("REFUSED: scout task $ID has no
	// report ... use --force after explicit discard approval").
	if task.Kind == state.KindScout && !force && !report.Exists(projectRoot, task.HerdrAgentName) {
		fmt.Fprintf(stderr, "vexillum: release refused: scout task %s has no report at %s\n", taskID, report.Path(projectRoot, task.HerdrAgentName))
		fmt.Fprintln(stderr, "vexillum: the report is the work product - have the soldier write it, or pass --force to release anyway")
		return 1
	}

	c, err := camp.Resolve(projectDir, vexillumHome, task.CampSlot)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: resolving camp: %v\n", err)
		return 1
	}

	if err := soldier.ReleaseInHerdr(task, c, client, homeDir); err != nil {
		fmt.Fprintf(stderr, "vexillum: release refused: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "released: camp returned to the pool, herdr pane closed.")
	return 0
}

func resolveDirs() (projectDir, vexillumHome string, err error) {
	projectDir, err = os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("cannot determine current directory: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	vexillumHome = filepath.Join(home, ".vexillum")

	if err := refuseInsideVexillumHome(projectDir, vexillumHome); err != nil {
		return "", "", err
	}
	return projectDir, vexillumHome, nil
}

// refuseInsideVexillumHome reports an error if projectDir is itself
// under vexillumHome - i.e. running a project command from inside a
// camp's own worktree, not the real project root. Caught live: a
// commander cd'd into a task's camp to inspect it, then dispatched a
// second mission without cd-ing back. That created an entirely separate
// camp pool keyed off the camp's own path hash - invisible to every
// future command run correctly from the real project root, and a
// correctly-run 'vexillum land'/'release' for that slot number would
// have resolved the WRONG camp in the real project's own pool.
func refuseInsideVexillumHome(projectDir, vexillumHome string) error {
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		return err
	}
	absHome, err := filepath.Abs(vexillumHome)
	if err != nil {
		return err
	}
	absProject = filepath.Clean(absProject)
	absHome = filepath.Clean(absHome)

	if absProject == absHome || strings.HasPrefix(absProject, absHome+string(filepath.Separator)) {
		return fmt.Errorf("running from inside %s, which looks like a vexillum-managed camp, not a project root - cd back to the real project and try again", absProject)
	}
	return nil
}
