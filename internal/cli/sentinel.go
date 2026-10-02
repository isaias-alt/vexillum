package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/sentinel"
)

const sentinelUsage = `Watch dispatched soldiers and record status changes.

Usage:
  ` + cmdname.Name + ` sentinel         Poll forever (foreground; run it backgrounded)
  ` + cmdname.Name + ` sentinel drain   Print and acknowledge pending wakes right now
  ` + cmdname.Name + ` sentinel await   Block until a wake arrives, or time out (for the
                             async Stop hook - see '` + cmdname.Name + ` init')

The sentinel polls every tracked "running" task's live herdr status,
across every project it's ever seen (one sentinel process per machine -
see internal/sentinel.Tick). When one settles (done or blocked), it
persists the change and records a durable wake, scoped to that task's own
project. "drain"/"await" resolve which project to act on from the
current directory (git toplevel, namespaced the same way "vx
dispatch" namespaces a project's camps) - not a repo, or a toplevel
that's itself a vexillum-managed camp, means nothing to drain here, so
both are silent no-ops rather than an error.

"drain" checks once, instantly: {"decision":"block",...} if something's
already pending for that project, {} otherwise. "await" is what the async
Stop hook actually calls - it blocks (re-checking every few seconds)
until a wake shows up or it times out, so the commander gets woken even
if its turn already ended before a soldier settled, not just when a wake
happens to already be pending at the exact moment a turn is ending.
"await" exits immediately, without waiting, outside a herdr-managed pane
(no HERDR_WORKSPACE_ID) - the hook is committed into the project and so
reaches any other tool's own Claude Code turns too, which have no task
for the sentinel to track.

A wake can also be forum feedback: the forum listener (see "vx forum")
stores a user's prompt in the project's inbox and records a forum wake, which
both "drain" and "await" report as "forum session <file>: N new messages
(ended: false). Run vx forum inbox." It is the same hook and the same
once-only delivery; a forum wake whose messages were already confirmed is
dropped.

Each "await" records itself under ~/.vexillum/sentinel-awaiters/ and never
outlives the hook that launched it: it exits as soon as its parent process
is gone, and a newer "await" from the same session and project replaces the
previous turn's. Starting "vx sentinel" or any "await" also stops orphaned
ones left behind by a dead session. A process is only ever signaled after
it is verified to be a "vx sentinel await" - never by name.
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

// sentinelMode classifies args into which action "vx sentinel" should
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

// Sentinel runs the "vx sentinel" command.
func Sentinel(args []string) int {
	mode, err := sentinelMode(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		fmt.Fprint(os.Stderr, sentinelUsage)
		return 1
	}
	if mode == "help" {
		fmt.Print(sentinelUsage)
		return 0
	}

	if mode == "drain" || mode == "await" {
		return runSentinelDrainOrAwait(mode)
	}

	_, vexillumHome, err := resolveDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}

	release, err := sentinel.AcquireLock(vexillumHome)
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}
	defer release()

	if n := sentinel.ReapAwaiters(vexillumHome); n > 0 {
		fmt.Fprintf(os.Stdout, "sentinel: stopped %d orphaned await process(es)\n", n)
	}

	fmt.Fprintf(os.Stdout, "sentinel: polling %s every %s (ctrl-c to stop)\n", vexillumHome, sentinelPollInterval)
	sentinel.Run(vexillumHome, herdr.CLI{}, sentinelPollInterval, os.Stdout)
	return 0
}

// runSentinelDrainOrAwait resolves which project "drain"/"await" should
// act on from the current directory (see resolveDrainTarget) and
// dispatches to the matching helper. An unresolvable project - cwd isn't
// in a git repository, or its toplevel is itself a vexillum-managed camp -
// is a silent no-op for both, not an error: drain prints the same "{}" it
// would for a project with nothing pending, and await exits 0 immediately,
// the same as it does outside a herdr-managed pane.
func runSentinelDrainOrAwait(mode string) int {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+": cannot determine home directory:", err)
		return 1
	}
	vexillumHome := filepath.Join(home, ".vexillum")

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}

	projectRoot, err := resolveDrainTarget(cwd, vexillumHome)
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}

	if mode == "drain" {
		if projectRoot == "" {
			fmt.Fprintln(os.Stdout, "{}")
			return 0
		}
		return runSentinelDrain(projectRoot, os.Stdout, os.Stderr)
	}

	if projectRoot == "" {
		return 0
	}
	return runSentinelAwaitGuarded(os.Getenv("HERDR_WORKSPACE_ID"), func() int {
		return runSentinelAwaitRegistered(vexillumHome, projectRoot)
	})
}

// runSentinelAwaitGuarded is the actual entry point the async Stop hook
// reaches. It only makes sense for a genuine vexillum-managed turn - the
// commander's own interactive session, or a dispatched soldier's - both
// of which always run inside a herdr-managed pane (vx dispatch and
// redispatch already require the same HERDR_WORKSPACE_ID from their
// caller; see cli/dispatch.go). workspaceID is that same env var, read
// by the caller, and run is the real await to start when it is set.
//
// The Stop hook itself is registered in .claude/settings.json, which
// ensureSentinelHook writes into the project directory and which git
// then tracks like any other file - so it travels into every checkout
// of the repo, including a mission's own camp. A headless Claude Code
// turn run there outside a herdr pane - internal/tribunal's review
// step, which shells out to "claude -p ..." directly from "vx
// ship", is the case that surfaced this - inherits the hook too, with no
// HERDR_WORKSPACE_ID in its environment. Without this guard, that turn
// would sit blocked in runSentinelAwait for up to maxWait: the sentinel
// has no task tracking an invocation it never dispatched, so a wake for
// it can never arrive. An empty workspaceID means exactly that - exit 0
// immediately, the same "let the turn end quietly" result runSentinelAwait
// itself returns on a real timeout, just without waiting first.
func runSentinelAwaitGuarded(workspaceID string, run func() int) int {
	if workspaceID == "" {
		return 0
	}
	return run()
}

// runSentinelAwaitRegistered runs the real await: registered under
// vexillumHome (see sentinel.RegisterAwaiter), tied to the lifetime of the
// process that launched it, and quietly exiting 0 on SIGTERM/SIGHUP/SIGINT
// (what a superseding or reaping newer await sends, or the hook runner
// closing us down).
func runSentinelAwaitRegistered(vexillumHome, projectRoot string) int {
	ownerPID := os.Getppid()
	if ownerPID <= 1 {
		// Already orphaned at birth: nobody is listening.
		return 0
	}

	release, err := sentinel.RegisterAwaiter(vexillumHome, projectRoot, ownerPID)
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}
	defer release()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	defer signal.Stop(interrupt)

	return runSentinelAwait(projectRoot, sentinelAwaitMaxWait, sentinelAwaitPollInterval, os.Stderr,
		func() bool { return !sentinel.OwnerGone(ownerPID) }, interrupt)
}

// resolveDrainTarget finds the project "drain"/"await" should act on,
// from cwd: the git repository containing it, namespaced under
// vexillumHome the same way camp.Acquire already keys a project's camps
// (see internal/project.Root). Returns "" (not an error) when cwd isn't
// inside a git repository, or when its toplevel is itself a
// vexillum-managed camp (a soldier's own worktree, checked out under
// vexillumHome) - both are "nothing to drain here" cases, not failures: a
// soldier's own Stop hook firing from inside its own camp has nothing to
// drain for itself, same as a stray shell that isn't in a project at all.
//
// The camp check compares against an EvalSymlinks'd copy of vexillumHome,
// not vexillumHome as given: "git rev-parse --show-toplevel" resolves
// symlinks when it walks up to find toplevel, so on a machine where
// vexillumHome's own path involves one (e.g. a temp-dir-rooted
// vexillumHome under macOS's symlinked /var, as every test here uses),
// comparing the resolved toplevel against an unresolved vexillumHome
// would silently fail to recognize a camp as being under it - caught
// live by TestResolveDrainTarget_InsideACampIsANoOp before this
// normalization was added.
func resolveDrainTarget(cwd, vexillumHome string) (string, error) {
	toplevel, err := gitToplevel(cwd)
	if err != nil {
		return "", nil
	}

	resolvedHome := vexillumHome
	if resolved, err := filepath.EvalSymlinks(vexillumHome); err == nil {
		resolvedHome = resolved
	}
	if refuseInsideVexillumHome(toplevel, resolvedHome) != nil {
		return "", nil
	}
	return project.Root(vexillumHome, toplevel)
}

// gitToplevel runs "git rev-parse --show-toplevel" in dir, returning the
// absolute root of the git repository containing it. Any failure (not a
// repository, git not installed, ...) is reported as a plain error -
// resolveDrainTarget treats every such failure the same way: nothing to
// drain here.
func gitToplevel(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel (in %s): %w", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// drainWakes consumes both kinds of pending wake of the project: soldier status
// changes (sentinel.Drain) and forum feedback (sentinel.DrainForum). Whatever
// one drain returned is kept even if the other failed, since a drained wake is
// already consumed.
func drainWakes(projectRoot string) ([]sentinel.Wake, []sentinel.ForumWake, error) {
	wakes, werr := sentinel.Drain(projectRoot)
	forumWakes, ferr := sentinel.DrainForum(projectRoot)
	return wakes, forumWakes, errors.Join(werr, ferr)
}

func runSentinelDrain(projectRoot string, stdout, stderr io.Writer) int {
	wakes, forumWakes, err := drainWakes(projectRoot)
	if err != nil && len(wakes)+len(forumWakes) == 0 {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}

	if len(wakes)+len(forumWakes) == 0 {
		fmt.Fprintln(stdout, "{}")
		return 0
	}

	out, err := json.Marshal(map[string]string{"decision": "block", "reason": wakeReason(wakes, forumWakes)})
	if err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}
	fmt.Fprintln(stdout, string(out))
	return 0
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
func runSentinelAwait(projectRoot string, maxWait, pollInterval time.Duration, stderr io.Writer, stillWanted func() bool, interrupt <-chan os.Signal) int {
	deadline := time.Now().Add(maxWait)
	for {
		wakes, forumWakes, _ := drainWakes(projectRoot)
		if len(wakes)+len(forumWakes) > 0 {
			fmt.Fprintln(stderr, wakeReason(wakes, forumWakes))
			return 2
		}
		if time.Now().After(deadline) || !stillWanted() {
			return 0
		}
		select {
		case <-time.After(pollInterval):
		case <-interrupt:
			return 0
		}
	}
}

// wakeReason is the text the Stop hook (or drain) hands the commander: one
// section for soldier status changes, one for forum feedback waiting in the
// inbox. A forum wake names the session and how many messages wait, never their
// text: that is read with "vx forum inbox".
func wakeReason(wakes []sentinel.Wake, forumWakes []sentinel.ForumWake) string {
	var sections []string
	if len(wakes) > 0 {
		var lines []string
		for _, w := range wakes {
			lines = append(lines, fmt.Sprintf("- %s %s: %s -> %s", w.Kind, w.TaskID, w.OldStatus, w.NewStatus))
		}
		sections = append(sections, "A vexillum soldier's status changed:\n"+
			strings.Join(lines, "\n")+
			"\nCheck on it (and report to the general, or land/release as appropriate) before ending your turn.")
	}
	if len(forumWakes) > 0 {
		var lines []string
		for _, w := range forumWakes {
			noun := "new messages"
			if w.Count == 1 {
				noun = "new message"
			}
			lines = append(lines, fmt.Sprintf("forum session %s: %d %s (ended: %t). Run %s forum inbox.", singleLine(w.File), w.Count, noun, w.Ended, cmdname.Name))
		}
		sections = append(sections, "Forum feedback is waiting for you:\n"+
			strings.Join(lines, "\n")+
			"\nRead it, act on it, and answer with "+cmdname.Name+" forum reply before ending your turn.")
	}
	return strings.Join(sections, "\n\n")
}

// singleLine keeps a user-chosen path from breaking the wake text's lines.
func singleLine(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}
