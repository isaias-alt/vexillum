package doctorcheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/isaias-alt/vexillum/internal/buildinfo"
	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/scaffold"
	"github.com/isaias-alt/vexillum/internal/sentinel"
)

const (
	hookName     = "sentinel Stop hook"
	hookResolves = "sentinel Stop hook finds " + cmdname.Name
	sentinelName = "sentinel"

	// hookProbePath is the bare PATH the hook is probed with: what a
	// shell that never sourced the user's profile has.
	hookProbePath = "/usr/bin:/bin"
)

// hookProbeTimeout bounds the probe. A hook that found vx and is waiting
// for a wake is killed at this point and counts as found. A variable so
// tests need not wait it out.
var hookProbeTimeout = 5 * time.Second

// StopHooks reports on the project's sentinel Stop hook, the thing that
// wakes the commander when a soldier finishes: whether it is registered,
// whether its command finds a vx binary under a minimal environment, and
// whether a sentinel is running. A hook that cannot find vx is a warning
// with the exact fix. Read-only.
func StopHooks(projectDir, vexillumHome, homeDir string) []Result {
	state, err := scaffold.InspectSentinelHook(projectDir)
	if err != nil {
		return []Result{{Name: hookName, Warn: true, Detail: err.Error()}}
	}

	up := "run '" + cmdname.Name + " upgrade'"
	var out []Result
	if !state.Present {
		out = append(out, Result{Name: hookName, Warn: true, Detail: "not registered in .claude/settings.json, so the commander is never woken when a soldier finishes; " + up})
	} else {
		if state.Current {
			out = append(out, Result{Name: hookName, OK: true, Detail: "registered in .claude/settings.json"})
		} else {
			out = append(out, Result{Name: hookName, Warn: true, Detail: "registered but out of date; " + up})
		}
		out = append(out, HookResolves(state.Command, homeDir))
	}
	out = append(out, SentinelRunning(vexillumHome))
	return out
}

// HookResolves runs the hook command the way Claude Code would from a
// shell with nothing in its environment but HOME and a bare PATH
// ("env -i HOME=<home> PATH=/usr/bin:/bin sh -c <command>") and reports
// whether it found a vx binary. A vx that is found and waits for a wake is
// stopped after a few seconds, which still counts as found. The command is
// run from an empty directory, outside any herdr pane, so a vx that is
// found has nothing to wait for and exits at once.
func HookResolves(command, homeDir string) Result {
	fix := "install " + cmdname.Name + " where the hook looks (brew install, or the curl installer, which puts it in ~/.local/bin) or put its directory on PATH"
	if command != scaffold.SentinelHookCommand {
		fix = "run '" + cmdname.Name + " upgrade' to rewrite the hook so it does not depend on PATH, and " + fix
	}

	dir, err := os.MkdirTemp("", "vx-hook-probe-")
	if err != nil {
		return Result{Name: hookResolves, Warn: true, Detail: "could not run the probe: " + err.Error()}
	}
	defer os.RemoveAll(dir)

	ctx, cancel := context.WithTimeout(context.Background(), hookProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/env", "-i", "HOME="+homeDir, "PATH="+hookProbePath, "sh", "-c", command)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// A shell that does not exec its last command leaves a child behind
	// when the probe times out, so signal the whole group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second

	runErr := cmd.Run()

	var exitErr *exec.ExitError
	switch {
	case ctx.Err() != nil:
		return Result{Name: hookResolves, OK: true, Detail: "found with a minimal environment (it was still waiting when the probe stopped it)"}
	case strings.Contains(stderr.String(), scaffold.SentinelHookNotFoundMessage), errors.As(runErr, &exitErr) && exitErr.ExitCode() == 127:
		return Result{Name: hookResolves, Warn: true, Detail: "not found with a minimal environment (HOME and PATH=" + hookProbePath + " only), so the commander is never woken; " + fix}
	case runErr != nil && !errors.As(runErr, &exitErr):
		return Result{Name: hookResolves, Warn: true, Detail: "could not run the probe: " + runErr.Error()}
	}
	return Result{Name: hookResolves, OK: true, Detail: "found with a minimal environment"}
}

// liveSentinelPIDs lists every "vx sentinel" process of the user. A variable
// so tests can stand in for ps (see SetLiveSentinelPIDs).
var liveSentinelPIDs = sentinel.LivePIDs

// SetLiveSentinelPIDs swaps the process listing SentinelRunning counts
// sentinels with and returns a function that restores it. For tests, which
// must not depend on the sentinels running on the machine they run on.
func SetLiveSentinelPIDs(f func() ([]int, error)) (restore func()) {
	old := liveSentinelPIDs
	liveSentinelPIDs = f
	return func() { liveSentinelPIDs = old }
}

// SentinelRunning reports whether the polling sentinel is alive, and how
// many Stop hook awaits are waiting on it. Not running is never a failure: the
// sentinel starts by itself with the next dispatch, so not running is normal
// while nothing is dispatched.
//
// It warns, never fails, about the two ways a sentinel goes wrong over time:
// more than one sentinel process alive at once (they race over the same tasks),
// and a live sentinel that is not the installed binary - a different version,
// or the same version string ("dev") whose executable was replaced on disk
// since it started. Either way it names the pid to stop; the next dispatch,
// prompt or Stop hook await then starts a current one.
func SentinelRunning(vexillumHome string) Result {
	waiting := sentinel.LiveAwaiters(vexillumHome)
	running := sentinel.IsRunning(vexillumHome)

	var problems []string
	if pids, err := liveSentinelPIDs(); err == nil && len(pids) > 1 {
		problems = append(problems, fmt.Sprintf("%d sentinel processes are alive (pids %s) and compete for the same tasks - stop them with 'kill <pid>' and let a single fresh one start", len(pids), joinInts(pids)))
	}
	if running {
		problems = append(problems, staleSentinelProblem(vexillumHome)...)
	}
	if len(problems) > 0 {
		return Result{Name: sentinelName, Warn: true, Detail: strings.Join(problems, "; ") + fmt.Sprintf(" (the next '%s dispatch' starts a current one)", cmdname.Name)}
	}

	if running {
		return Result{Name: sentinelName, OK: true, Detail: fmt.Sprintf("running, %d Stop hook await(s) waiting", waiting)}
	}
	return Result{Name: sentinelName, OK: true, Detail: fmt.Sprintf("not running (the next '%s dispatch' starts it), %d Stop hook await(s) waiting", cmdname.Name, waiting)}
}

// staleSentinelProblem describes how the live sentinel differs from the
// installed binary, as zero or one problem.
func staleSentinelProblem(vexillumHome string) []string {
	info, ok := sentinel.LiveInfo(vexillumHome)
	if !ok {
		return nil
	}
	switch {
	case info.Build.Version != buildinfo.Version:
		return []string{fmt.Sprintf("the running sentinel (pid %d) is version %s but this binary is %s - stop it with 'kill %d'", info.PID, info.Build.Version, buildinfo.Version, info.PID)}
	case info.Build.Stale() != "":
		return []string{fmt.Sprintf("the running sentinel (pid %d) started from a binary that has since changed (%s) - it retires on its own within seconds, or stop it with 'kill %d'", info.PID, info.Build.Stale(), info.PID)}
	}
	return nil
}

func joinInts(ns []int) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}
