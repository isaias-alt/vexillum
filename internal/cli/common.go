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
	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/doctorcheck"
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
// correctly-run 'vx land'/'strike' for that slot number would
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

// herdrPreconditions checks, before anything is created, that a command which
// starts a soldier can work here: it runs inside a herdr pane
// (HERDR_WORKSPACE_ID set) and both herdr and claude are on PATH. command
// names the caller in the message ("dispatch", "redispatch"). It returns the
// workspace id, or one error naming everything that is missing and how to fix
// it, so the user sees it once and nothing (task, camp lease, sentinel) has
// been left behind. getenv and lookPath are injected so tests control them.
func herdrPreconditions(command string, getenv func(string) string, lookPath func(string) (string, error)) (string, error) {
	return harnessPreconditions(command, soldier.HarnessClaude, getenv, lookPath)
}

// inHerdrApp reports whether this terminal belongs to the Herdr desktop app
// (herdr-core), which sets HC_TERMINAL_ID and none of the HERDR_* variables,
// rather than to a herdr TUI pane.
func inHerdrApp(getenv func(string) string) bool {
	return getenv("HC_TERMINAL_ID") != "" && getenv("HERDR_WORKSPACE_ID") == ""
}

// herdrAppMessage is the problem shown when command runs in a Herdr app
// terminal: a different product from the herdr TUI vexillum runs on.
func herdrAppMessage(command string) string {
	return "this terminal belongs to the Herdr app (HC_TERMINAL_ID is set), not the herdr TUI - " +
		cmdname.Name + " currently runs on the herdr TUI, so run " + command + " from a herdr TUI pane " +
		"(or try the experimental app backend with " + herdr.BackendEnv + "=hc)"
}

// harnessPreconditions is herdrPreconditions for a soldier that runs on the
// harness registered under harness ("" means claude): the CLI that must be on
// PATH next to herdr is that harness's own, not always claude. A harness this
// build cannot drive is an error too, before anything is started.
//
// With VX_BACKEND=hc the terminal backend is herdr-core instead: the command
// must run in a Herdr app terminal (HC_TERMINAL_ID) and a real hc binary must
// be on PATH, and no workspace id comes back (the space is chosen from the
// project, see backendWorkspace). The opencode harness is allowed there on
// OpenCode 2.x only, which vexillum launches with its own status plugin.
func harnessPreconditions(command, harness string, getenv func(string) string, lookPath func(string) (string, error)) (string, error) {
	workspaceID := getenv("HERDR_WORKSPACE_ID")
	harnessBinary, err := soldier.HarnessBinary(harness)
	if err != nil {
		return "", fmt.Errorf("cannot %s from here, nothing was started: %w", command, err)
	}

	var problems []string
	multiplexer := "herdr"
	switch {
	case herdr.WantsHC(getenv):
		multiplexer = ""
		workspaceID = ""
		if getenv("HC_TERMINAL_ID") == "" {
			problems = append(problems, herdr.BackendEnv+"=hc is set but HC_TERMINAL_ID is not - "+command+" must run from inside a Herdr app terminal")
		}
		if _, _, err := herdr.ResolveHC(lookPath, herdr.HCVersionOf); err != nil {
			problems = append(problems, err.Error())
		}
	case workspaceID == "" && inHerdrApp(getenv):
		problems = append(problems, herdrAppMessage(command))
	case workspaceID == "":
		problems = append(problems, "HERDR_WORKSPACE_ID is not set - "+command+" must run from inside a herdr-managed pane")
	}
	harnessFound := true
	tools := []string{harnessBinary}
	if multiplexer != "" {
		tools = []string{multiplexer, harnessBinary}
	}
	for _, tool := range tools {
		if _, err := lookPath(tool); err != nil {
			problem := tool + " is not on PATH"
			if hint := doctorcheck.InstallHint(tool); hint != "" {
				problem += " (" + hint + ")"
			}
			problems = append(problems, problem)
			if tool == harnessBinary {
				harnessFound = false
			}
		}
	}
	// A harness whose CLI has generations that launch differently needs one
	// vexillum can drive. Asked only once the binary is known to be there.
	if harnessFound {
		major, version, err := soldier.DetectHarnessVersion(harness)
		switch {
		case err != nil:
			problems = append(problems, err.Error())
		case herdr.WantsHC(getenv) && harness == soldier.HarnessOpenCode && major != 2:
			// herdr-core reads a status from the OSC 7501 an agent prints and
			// OpenCode prints none. An OpenCode 2.x soldier is launched with a
			// plugin of vexillum's that does (internal/opencodeosc); V1 has a
			// different plugin API and none here, so its soldier would only
			// stall. Refused before anything is created.
			problems = append(problems, fmt.Sprintf("the opencode harness needs OpenCode 2.x under %s=hc - opencode %s has no status plugin for herdr-core, so its soldier would stall; install 2.x or use the herdr TUI backend (unset %s)", herdr.BackendEnv, version, herdr.BackendEnv))
		}
	}
	if len(problems) == 0 {
		return workspaceID, nil
	}
	return "", fmt.Errorf("cannot %s from here, nothing was started:\n  - %s\nRun '%s doctor' to check your setup", command, strings.Join(problems, "\n  - "), cmdname.Name)
}

// backendWorkspace is the workspace id a soldier's tab is created in. On the
// herdr TUI it is the caller's own workspace (workspaceID, from
// harnessPreconditions). herdr-core has no "current workspace" a process can
// read, so with the hc backend vexillum picks its own space for the project,
// vx-<project>, which the Herdr app creates when it does not know it.
func backendWorkspace(workspaceID, vexillumHome, projectDir string) (string, error) {
	if !herdr.UsingHC() {
		return workspaceID, nil
	}
	root, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		return "", fmt.Errorf("resolving project root: %w", err)
	}
	return herdr.HCSpace(root), nil
}

// commanderPane is the terminal the running command was typed in: the herdr
// pane (HERDR_PANE_ID), or with VX_BACKEND=hc the Herdr app terminal
// (HC_TERMINAL_ID). Empty outside either, which means nothing is known about
// the commander.
func commanderPane(getenv func(string) string) string {
	if herdr.WantsHC(getenv) {
		return getenv("HC_TERMINAL_ID")
	}
	return getenv("HERDR_PANE_ID")
}

// inManagedTerminal reports whether the process runs in a terminal its backend
// manages: a herdr pane (HERDR_WORKSPACE_ID) or, with VX_BACKEND=hc, a Herdr app
// terminal (HC_TERMINAL_ID).
func inManagedTerminal(getenv func(string) string) bool {
	if herdr.WantsHC(getenv) {
		return getenv("HC_TERMINAL_ID") != ""
	}
	return getenv("HERDR_WORKSPACE_ID") != ""
}

// requireTaskBackend refuses to act on a soldier through the wrong terminal
// backend: its ids belong to the backend it was dispatched on, and another one
// would report it gone (a strike would even close nothing and say it had). A
// task that never got a terminal has nothing to mismatch.
func requireTaskBackend(task state.Task, client herdr.Client) error {
	if task.HerdrTabID == "" || herdr.SameBackend(task.Backend, client) {
		return nil
	}
	dispatched := task.Backend
	if dispatched == "" {
		dispatched = herdr.BackendHerdr
	}
	hint := "unset " + herdr.BackendEnv
	if dispatched == herdr.BackendHC {
		hint = "set " + herdr.BackendEnv + "=" + herdr.BackendHC
	}
	return fmt.Errorf("task %s was dispatched on the %s backend, but this command runs on the %s one - %s and try again", task.ID, dispatched, herdr.NameOf(client), hint)
}

// backendMismatchWarning says, for a dispatch on the backend want, that the
// sentinel already running watches soldiers on another one: it would report
// this soldier gone. Empty when they match or when there is no such sentinel.
func backendMismatchWarning(info sentinel.Info, running bool, want string) string {
	if !running {
		return ""
	}
	has := info.Backend
	if has == "" {
		has = herdr.BackendHerdr
	}
	if has == want {
		return ""
	}
	return fmt.Sprintf("the sentinel already running (pid %d, %s) watches soldiers on the %s backend and cannot supervise this one on %s - stop it (kill %d) so the next %s command starts one that can; a sentinel serves one backend at a time",
		info.PID, info.Build.Version, has, want, info.PID, cmdname.Name)
}

// CheckBackend reports a VX_BACKEND value vexillum does not know, so a typo
// fails loudly instead of running on the herdr TUI.
func CheckBackend() error {
	_, err := herdr.BackendName(os.Getenv)
	return err
}

// ensureSentinelRunning best-effort auto-starts "vx sentinel"
// detached in the background if one isn't already watching vexillumHome, or
// if the one that is runs from a binary that has since been replaced on disk
// (it retires on its own; the new one waits for it - see
// sentinel.AcquireLockRetiring).
// Dispatch (and redispatch) return as soon as the soldier is seen working or
// has settled (see soldier.RunInHerdr) - for anything but a trivial prompt, the
// sentinel is what eventually records the soldier's real outcome, so a
// dispatch with no sentinel running would otherwise strand that task
// Running forever. A failure here is reported but never fails dispatch
// itself: the soldier is already started regardless: 'vx sentinel'
// remains available to start by hand if this doesn't work.
func ensureSentinelRunning(vexillumHome string, stderr io.Writer) {
	if sentinel.IsRunning(vexillumHome) && !sentinel.HolderStale(vexillumHome) {
		info, ok := sentinel.LiveInfo(vexillumHome)
		if msg := backendMismatchWarning(info, ok, herdr.NameOf(herdr.Default())); msg != "" {
			fmt.Fprintln(stderr, cmdname.Name+": warning: "+msg)
		}
		return
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": warning: could not auto-start the sentinel: %v\n", err)
		return
	}

	logPath := filepath.Join(vexillumHome, "sentinel.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": warning: could not auto-start the sentinel: %v\n", err)
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
		fmt.Fprintf(stderr, cmdname.Name+": warning: could not auto-start the sentinel: %v\n", err)
		return
	}
	fmt.Fprintf(stderr, cmdname.Name+": auto-started sentinel (pid %d), logging to %s\n", cmd.Process.Pid, logPath)
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
// since both 'vx strike' and 'vx redispatch' require
// state.Load - and, to resolve the right camp, a populated CampSlot - to
// succeed first). Saving the camp fields here, before RunInHerdr is even
// called, means a CreateTab failure still leaves a loadable task that
// already knows which slot it owns; the fallback save below then marks it
// Failed so it can be struck normally (the worktree has no commits yet,
// so camp.Strike's landed-check passes trivially). Re-dispatch
// (runRedispatch) already saves task in its own reset state before
// calling this, but with zeroed camp fields (it hasn't acquired a fresh
// camp yet at that point) - this save is what records the new camp it
// gets here, same as a fresh dispatch.
func acquireAndRunInHerdr(projectDir, vexillumHome, workspaceID string, task state.Task, client herdr.Client, stdout, stderr io.Writer) int {
	projectRoot, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": resolving project root: %v\n", err)
		return 1
	}

	announceCommander(&task, vexillumHome, projectRoot, projectDir, stderr)

	c, err := camp.Acquire(projectDir, vexillumHome, task.ID)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": acquiring camp: %v\n", err)
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
		// The worktree is still fresh (no commits, clean), so Strike's
		// landed-check passes trivially - give the slot back rather than
		// stranding it.
		fmt.Fprintf(stderr, cmdname.Name+": persisting acquired camp: %v\n", err)
		if strikeErr := camp.Strike(c, task.ID); strikeErr != nil {
			fmt.Fprintf(stderr, cmdname.Name+": striking camp slot %d after failed save: %v\n", c.Slot, strikeErr)
		}
		return 1
	}

	result, runErr := soldier.RunInHerdr(vexillumHome, workspaceID, task, c, client)
	shown := result.Status
	if runErr != nil && result.HerdrTabID == "" {
		// RunInHerdr failed before it created anything; the failed state is
		// recorded below, so say so here instead of the pending it returned.
		shown = state.StatusFailed
	}
	fmt.Fprintf(stdout, "status=%s\n", shown)
	if result.HerdrPaneID != "" {
		fmt.Fprintf(stdout, "herdr: workspace=%s tab=%s pane=%s agent=%s\n", result.HerdrWorkspaceID, result.HerdrTabID, result.HerdrPaneID, result.HerdrAgentName)
	}
	if result.Status == state.StatusRunning {
		fmt.Fprintln(stdout, "still running - the sentinel will record its final status")
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
			// persist so this task is left in a normal, strikable state
			// instead of stuck Pending with a camp slot nothing else can
			// find its way back to. A later failure (after RunInHerdr's own
			// saves already ran) is left as RunInHerdr recorded it - it
			// already reflects the task's real outcome, and overwriting it
			// here would discard that.
			result.Status = state.StatusFailed
			result.Output = runErr.Error()
			result.UpdatedAt = time.Now().UTC()
			if saveErr := state.Save(projectRoot, result); saveErr != nil {
				fmt.Fprintf(stderr, cmdname.Name+": persisting failed state (after: %v): %v\n", runErr, saveErr)
				return 1
			}
		}
		fmt.Fprintf(stderr, cmdname.Name+": %v\n", runErr)
		return 1
	}
	return 0
}
