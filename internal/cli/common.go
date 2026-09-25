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
	"github.com/isaias-alt/vexillum/internal/sentinel"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

// resolveDirs resolves the current project directory and vexillumHome
// (~/.vexillum) every project-scoped command starts from.
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

// ensureSentinelRunning best-effort auto-starts "vexillum sentinel"
// detached in the background if one isn't already watching vexillumHome.
// Dispatch (and redispatch) return after only a short quick-settle probe
// (see soldier.RunInHerdr) - for anything but a trivial prompt, the
// sentinel is what eventually records the soldier's real outcome, so a
// dispatch with no sentinel running would otherwise strand that task
// Running forever. A failure here is reported but never fails dispatch
// itself: the soldier is already started regardless: 'vexillum sentinel'
// remains available to start by hand if this doesn't work.
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

// acquireAndRunInHerdr acquires a camp for task and runs it in a real
// herdr pane - the exact sequence both a fresh dispatch and a re-dispatch
// (runRedispatch, in redispatch.go) go through once task is ready to run,
// kept in one place so the two commands can't drift out of step with
// each other.
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
