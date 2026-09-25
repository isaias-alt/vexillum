package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

const decideUsage = `Answer a blocked task's open question so it can continue.

Usage:
  vexillum decide <task-id> <answer>

Only a task in status "blocked" can be answered. Delivers <answer> to the
soldier's still-open herdr pane - the same client.AgentPrompt mechanism
dispatch/redispatch already use to submit a prompt - and records it against
the task's structured decision (see 'vexillum status --json', field
"decision"), not just as more prose in the transcript.

On a fast settle the task's status updates immediately: running if the
soldier is still going, blocked again if it asks another question, done or
failed if it settled that quickly. Otherwise the task is left running for
the sentinel to record the eventual settle, exactly like a fresh dispatch.

If the task is actually "interrupted" (its herdr pane is gone - vexillum
just hadn't noticed yet), this marks it interrupted and refuses: there is
nothing left to answer against. Use 'vexillum redispatch' instead.
`

// Decide runs the "vexillum decide" command.
func Decide(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(decideUsage)
		return 0
	}
	if len(args) < 2 {
		fmt.Print(decideUsage)
		return 1
	}

	projectDir, vexillumHome, err := resolveDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		return 1
	}

	taskID := args[0]
	answer := strings.Join(args[1:], " ")

	return runDecide(projectDir, vexillumHome, taskID, answer, herdr.CLI{}, os.Stdout, os.Stderr)
}

func runDecide(projectDir, vexillumHome, taskID, answer string, client herdr.Client, stdout, stderr io.Writer) int {
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

	if task.Status != state.StatusBlocked {
		if task.Status == state.StatusInterrupted {
			fmt.Fprintf(stderr, "vexillum: task %s is interrupted, not blocked - its herdr pane is already gone, there's nothing left to answer; use 'vexillum redispatch %s' instead\n", taskID, taskID)
		} else {
			fmt.Fprintf(stderr, "vexillum: task %s is %s, not blocked - only a blocked task can be answered\n", taskID, task.Status)
		}
		return 1
	}

	result, err := soldier.AnswerBlocked(projectRoot, task, answer, client)
	fmt.Fprintf(stdout, "status=%s\n", result.Status)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "answered task %s, now %s\n", taskID, result.Status)
	return 0
}
