package cli

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

const decideUsage = `Answer a blocked task's open question so it can continue.

Usage:
  ` + cmdname.Name + ` decide <task-id> <answer>
  ` + cmdname.Name + ` decide <task-id> --dismiss

Only a task in status "blocked" can be answered. Delivers <answer> to the
soldier's still-open herdr pane and records it against the task's structured
decision, not just as more prose in the transcript. That decision is the
field "decision" of '` + cmdname.Name + ` status --json'. If the open question came from Claude Code's
AskUserQuestion selector, <answer> (an option's exact text, or its 1-based
rendered number) is delivered as a single key press - the same mechanism a
human picking from that menu would use - unless it resolves to "Type
something.", which falls back to plain text like every other decision.

Once the answer is delivered it is also recorded on the task as an amendment
(field "amendments" in '` + cmdname.Name + ` status --json'), together with the question it answered,
so a mission's tribunal review counts it as part of the mission's intent. A
selector pick is recorded as the option's text. --dismiss records nothing.

On a fast settle the task's status updates immediately: running if the
soldier is still going, blocked again if it asks another question, done or
failed if it settled that quickly. Otherwise the task is left running for
the sentinel to record the eventual settle, exactly like a fresh dispatch.

--dismiss clears a task that was marked blocked by mistake - the soldier was
never really asking anything - without sending it anything: no prompt, no key
press. The task becomes whatever its pane says it is: running if the soldier
is still working, done (or unconfirmed) if it has settled, interrupted if the
pane is gone. It is refused if the pane is genuinely blocked on a question.
The dismissed question stays on the task, marked dismissed, and does not
block it again; '` + cmdname.Name + ` status --json' shows it. Use it when '` + cmdname.Name + ` prompt' or
'` + cmdname.Name + ` ship' refuse a task as blocked and there is nothing to answer.

If the task is actually "interrupted" (its herdr pane is gone - vexillum
just hadn't noticed yet), this marks it interrupted and refuses: there is
nothing left to answer against. Use '` + cmdname.Name + ` redispatch' instead.
`

// Decide runs the "vx decide" command.
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
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}

	taskID := args[0]
	if slices.Contains(args[1:], "--dismiss") {
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, cmdname.Name+": --dismiss takes only the task id, there is no answer to send")
			return 1
		}
		return runDecideDismiss(projectDir, vexillumHome, taskID, herdr.CLI{}, os.Stdout, os.Stderr)
	}
	answer := strings.Join(args[1:], " ")

	return runDecide(projectDir, vexillumHome, taskID, answer, herdr.CLI{}, os.Stdout, os.Stderr)
}

func runDecide(projectDir, vexillumHome, taskID, answer string, client herdr.Client, stdout, stderr io.Writer) int {
	projectRoot, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}

	task, err := state.Load(projectRoot, taskID)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": loading task %s: %v\n", taskID, err)
		return 1
	}

	if task.Status != state.StatusBlocked {
		if task.Status == state.StatusInterrupted {
			fmt.Fprintf(stderr, cmdname.Name+": task %s is interrupted, not blocked - its herdr pane is already gone, there's nothing left to answer; use '"+cmdname.Name+" redispatch %s' instead\n", taskID, taskID)
		} else {
			fmt.Fprintf(stderr, cmdname.Name+": task %s is %s, not blocked - only a blocked task can be answered\n", taskID, task.Status)
		}
		return 1
	}

	result, err := soldier.AnswerBlocked(projectRoot, task, answer, client)
	fmt.Fprintf(stdout, "status=%s\n", result.Status)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "answered task %s, now %s\n", taskID, result.Status)
	return 0
}

// runDecideDismiss is "vx decide <task-id> --dismiss": it clears a task
// that is blocked by mistake without sending its soldier anything (see
// soldier.DismissBlocked).
func runDecideDismiss(projectDir, vexillumHome, taskID string, client herdr.Client, stdout, stderr io.Writer) int {
	if err := state.ValidateID(taskID); err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}
	projectRoot, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}

	task, err := state.Load(projectRoot, taskID)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": loading task %s: %v\n", taskID, err)
		return 1
	}
	if task.Status != state.StatusBlocked {
		fmt.Fprintf(stderr, cmdname.Name+": task %s is %s, not blocked - there is nothing to dismiss\n", taskID, task.Status)
		return 1
	}

	result, err := soldier.DismissBlocked(projectRoot, task, client)
	fmt.Fprintf(stdout, "status=%s\n", result.Status)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "dismissed the question on task %s, now %s\n", taskID, result.Status)
	return 0
}
