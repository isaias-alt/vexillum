package doctorcheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

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

// SentinelRunning reports whether the polling sentinel is alive, and how
// many Stop hook awaits are waiting on it. Never a failure: the sentinel
// starts by itself with the next dispatch, so not running is normal while
// nothing is dispatched.
func SentinelRunning(vexillumHome string) Result {
	waiting := sentinel.LiveAwaiters(vexillumHome)
	if sentinel.IsRunning(vexillumHome) {
		return Result{Name: sentinelName, OK: true, Detail: fmt.Sprintf("running, %d Stop hook await(s) waiting", waiting)}
	}
	return Result{Name: sentinelName, OK: true, Detail: fmt.Sprintf("not running (the next '%s dispatch' starts it), %d Stop hook await(s) waiting", cmdname.Name, waiting)}
}
