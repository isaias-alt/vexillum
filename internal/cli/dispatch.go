package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/sentinel"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

const dispatchUsage = `Dispatch a soldier (mission or scout) into an isolated camp.

Usage:
  vexillum dispatch <prompt> [--kind mission|scout]

A mission changes code and delivers something to land; a scout only
investigates and reports back (default: mission).

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
  vexillum release <task-id>

Refuses unless the camp is clean and (for a mission) landed. On success,
returns the worktree to the pool for reuse and closes the herdr pane.
`

// Dispatch runs the "vexillum dispatch" command.
func Dispatch(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(dispatchUsage)
		return 0
	}

	prompt, kind, err := parseDispatchArgs(args)
	if err != nil {
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

	return runDispatch(projectDir, vexillumHome, workspaceID, prompt, kind, herdr.CLI{}, os.Stdout, os.Stderr)
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

func parseDispatchArgs(args []string) (prompt string, kind state.Kind, err error) {
	kind = state.KindMission
	var promptParts []string
	for i := 0; i < len(args); i++ {
		if args[i] != "--kind" {
			promptParts = append(promptParts, args[i])
			continue
		}
		if i+1 >= len(args) {
			return "", "", fmt.Errorf("--kind requires a value (mission or scout)")
		}
		i++
		switch args[i] {
		case "mission":
			kind = state.KindMission
		case "scout":
			kind = state.KindScout
		default:
			return "", "", fmt.Errorf("unknown kind %q, expected mission or scout", args[i])
		}
	}
	if len(promptParts) == 0 {
		return "", "", fmt.Errorf("missing prompt")
	}
	return strings.Join(promptParts, " "), kind, nil
}

func runDispatch(projectDir, vexillumHome, workspaceID, prompt string, kind state.Kind, client herdr.Client, stdout, stderr io.Writer) int {
	if !projectAlreadyInitialized(projectDir) {
		fmt.Fprintln(stderr, "vexillum: project not initialized, run 'vexillum init' first")
		return 1
	}

	task, err := state.New(kind, prompt)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: creating task: %v\n", err)
		return 1
	}

	return acquireAndRunInHerdr(projectDir, vexillumHome, workspaceID, task, client, stdout, stderr)
}

// acquireAndRunInHerdr acquires a camp for task and runs it in a real
// herdr pane - the exact sequence both a fresh dispatch and a re-dispatch
// (internal/cli.runRedispatch) go through once task is ready to run, kept
// in one place so the two commands can't drift out of step with each
// other.
func acquireAndRunInHerdr(projectDir, vexillumHome, workspaceID string, task state.Task, client herdr.Client, stdout, stderr io.Writer) int {
	c, err := camp.Acquire(projectDir, vexillumHome, task.ID)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: acquiring camp: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "task_id=%s kind=%s camp_slot=%d camp_branch=%s\n", task.ID, task.Kind, c.Slot, c.Branch)

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

	return runRelease(projectDir, vexillumHome, homeDir, args[0], herdr.CLI{}, os.Stdout, os.Stderr)
}

func runRelease(projectDir, vexillumHome, homeDir, taskID string, client herdr.Client, stdout, stderr io.Writer) int {
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
