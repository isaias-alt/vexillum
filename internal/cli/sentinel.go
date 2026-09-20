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
  vexillum sentinel drain   Print and acknowledge pending wakes (for a Stop hook)

The sentinel polls every tracked "running" task's live herdr status.
When one settles (done or blocked), it persists the change and records a
durable wake. "drain" is what a Claude Code Stop hook calls: if there are
unacknowledged wakes, it prints {"decision":"block","reason":"..."} so
the commander keeps working instead of ending its turn quietly;
otherwise it prints {}.
`

const sentinelPollInterval = 5 * time.Second

// sentinelMode classifies args into which action "vexillum sentinel" should
// take. Any args[0] other than "drain"/"-h"/"--help" is an error - without
// this, an unrecognized subcommand (a typo, a guess like "status") used to
// fall through silently to starting the infinite polling loop.
func sentinelMode(args []string) (mode string, err error) {
	if len(args) == 0 {
		return "run", nil
	}
	switch args[0] {
	case "-h", "--help":
		return "help", nil
	case "drain":
		return "drain", nil
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

	var lines []string
	for _, w := range wakes {
		lines = append(lines, fmt.Sprintf("- %s %s: %s -> %s", w.Kind, w.TaskID, w.OldStatus, w.NewStatus))
	}
	reason := "A vexillum soldier's status changed while you were about to stop:\n" +
		strings.Join(lines, "\n") +
		"\nCheck on it (and report to the general, or land/release as appropriate) before ending your turn."

	out, err := json.Marshal(map[string]string{"decision": "block", "reason": reason})
	if err != nil {
		fmt.Fprintln(stderr, "vexillum:", err)
		return 1
	}
	fmt.Fprintln(stdout, string(out))
	return 0
}
