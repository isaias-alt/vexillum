package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/sentinel"
)

const sentinelUsage = `Watch dispatched soldiers and record status changes.

Usage:
  vexillum sentinel         Poll forever (foreground; run it backgrounded)
  vexillum sentinel drain   Print and acknowledge pending wakes right now
  vexillum sentinel await   Block until a wake arrives, or time out (for the
                             async Stop hook - see 'vexillum init')

The sentinel polls every tracked "running" task's live herdr status.
When one settles (done or blocked), it persists the change and records a
durable wake. "drain" checks once, instantly: {"decision":"block",...}
if something's already pending, {} otherwise. "await" is what the async
Stop hook actually calls - it blocks (re-checking every few seconds)
until a wake shows up or it times out, so the commander gets woken even
if its turn already ended before a soldier settled, not just when a wake
happens to already be pending at the exact moment a turn is ending.
"await" exits immediately, without waiting, outside a herdr-managed pane
(no HERDR_WORKSPACE_ID) - the hook is committed into the project and so
reaches any other tool's own Claude Code turns too, which have no task
for the sentinel to track.
`

const (
	sentinelPollInterval      = 5 * time.Second
	sentinelAwaitPollInterval = 5 * time.Second
	// sentinelAwaitMaxWait stays comfortably under sentinelHookTimeoutSeconds
	// (the registered hook's own timeout) so this always exits 0 cleanly
	// on its own, well before Claude Code would have to kill it - Claude
	// Code re-fires the hook on every turn end regardless, so a shorter
	// self-imposed deadline costs nothing.
	sentinelAwaitMaxWait = 55 * time.Minute
)

// sentinelMode classifies args into which action "vexillum sentinel" should
// take. Any args[0] other than "drain"/"await"/"-h"/"--help" is an error -
// without this, an unrecognized subcommand (a typo, a guess like "status")
// used to fall through silently to starting the infinite polling loop.
func sentinelMode(args []string) (mode string, err error) {
	if len(args) == 0 {
		return "run", nil
	}
	switch args[0] {
	case "-h", "--help":
		return "help", nil
	case "drain":
		return "drain", nil
	case "await":
		return "await", nil
	default:
		return "", fmt.Errorf("unknown sentinel subcommand %q", args[0])
	}
}

// Sentinel runs the "vexillum sentinel" command.
func Sentinel(args []string) int {
	mode, err := sentinelMode(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		fmt.Fprint(os.Stderr, sentinelUsage)
		return 1
	}
	if mode == "help" {
		fmt.Print(sentinelUsage)
		return 0
	}

	_, vexillumHome, err := resolveDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		return 1
	}

	if mode == "drain" {
		return runSentinelDrain(vexillumHome, os.Stdout, os.Stderr)
	}
	if mode == "await" {
		return runSentinelAwaitGuarded(vexillumHome, sentinelAwaitMaxWait, sentinelAwaitPollInterval, os.Stderr, os.Getenv("HERDR_WORKSPACE_ID"))
	}

	release, err := sentinel.AcquireLock(vexillumHome)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		return 1
	}
	defer release()

	fmt.Fprintf(os.Stdout, "sentinel: polling %s every %s (ctrl-c to stop)\n", vexillumHome, sentinelPollInterval)
	sentinel.Run(vexillumHome, herdr.CLI{}, sentinelPollInterval, os.Stdout)
	return 0
}

func runSentinelDrain(vexillumHome string, stdout, stderr io.Writer) int {
	wakes, err := sentinel.Drain(vexillumHome)
	if err != nil {
		fmt.Fprintln(stderr, "vexillum:", err)
		return 1
	}

	if len(wakes) == 0 {
		fmt.Fprintln(stdout, "{}")
		return 0
	}

	out, err := json.Marshal(map[string]string{"decision": "block", "reason": wakeReason(wakes)})
	if err != nil {
		fmt.Fprintln(stderr, "vexillum:", err)
		return 1
	}
	fmt.Fprintln(stdout, string(out))
	return 0
}

// runSentinelAwaitGuarded is the actual entry point the async Stop hook
// reaches. It only makes sense for a genuine vexillum-managed turn - the
// commander's own interactive session, or a dispatched soldier's - both
// of which always run inside a herdr-managed pane (vexillum dispatch and
// redispatch already require the same HERDR_WORKSPACE_ID from their
// caller; see cli/dispatch.go). workspaceID is that same env var, read
// by the caller.
//
// The Stop hook itself is registered in .claude/settings.json, which
// ensureSentinelHook writes into the project directory and which git
// then tracks like any other file - so it travels into every checkout
// of the repo, including a worktree a completely different tool creates
// for its own purposes. A third-party tool that runs its own headless
// Claude Code turn there - no-mistakes' review/test/lint steps, which
// each shell out to "claude -p ..." inside an isolated run worktree
// checked out from a vexillum-initialized repo, is the case that
// surfaced this - inherits the hook too, with no HERDR_WORKSPACE_ID in
// its environment (no-mistakes' daemon isn't a herdr pane). Without this
// guard, that unrelated turn would sit blocked in runSentinelAwait for
// up to maxWait: the sentinel has no task tracking an invocation it
// never dispatched, so a wake for it can never arrive. An empty
// workspaceID means exactly that - exit 0 immediately, the same "let
// the turn end quietly" result runSentinelAwait itself returns on a
// real timeout, just without waiting first.
func runSentinelAwaitGuarded(vexillumHome string, maxWait, pollInterval time.Duration, stderr io.Writer, workspaceID string) int {
	if workspaceID == "" {
		return 0
	}
	return runSentinelAwait(vexillumHome, maxWait, pollInterval, stderr)
}

// runSentinelAwait is what the async Stop hook actually invokes
// (registered with "asyncRewake": true - see ensureSentinelHook). Unlike
// runSentinelDrain's instant check, it blocks, re-checking for a wake
// every pollInterval, until one shows up or maxWait elapses (the real
// CLI passes sentinelAwaitMaxWait/sentinelAwaitPollInterval; tests pass
// short durations instead of actually waiting up to 55 minutes).
// Verified live: a real asyncRewake Stop hook exiting 2 with a stderr
// message woke an idle Claude Code session on its own, delivered as
// "Stop hook feedback" with no new user prompt - exit 2 + stderr is the
// block signal here, not runSentinelDrain's JSON on stdout, matching
// that verified mechanism exactly.
func runSentinelAwait(vexillumHome string, maxWait, pollInterval time.Duration, stderr io.Writer) int {
	deadline := time.Now().Add(maxWait)
	for {
		wakes, err := sentinel.Drain(vexillumHome)
		if err == nil && len(wakes) > 0 {
			fmt.Fprintln(stderr, wakeReason(wakes))
			return 2
		}
		if time.Now().After(deadline) {
			return 0
		}
		time.Sleep(pollInterval)
	}
}

func wakeReason(wakes []sentinel.Wake) string {
	var lines []string
	for _, w := range wakes {
		lines = append(lines, fmt.Sprintf("- %s %s: %s -> %s", w.Kind, w.TaskID, w.OldStatus, w.NewStatus))
	}
	return "A vexillum soldier's status changed:\n" +
		strings.Join(lines, "\n") +
		"\nCheck on it (and report to the general, or land/release as appropriate) before ending your turn."
}
