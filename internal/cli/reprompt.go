package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

const repromptUsage = `Send a follow-up prompt to a soldier that already finished.

Usage:
  ` + cmdname.Name + ` prompt <task-id> <text>

Use this instead of prompting the soldier's pane by hand (herdr agent
prompt): it sets the task back to running before the prompt is delivered and
makes sure a sentinel is watching, so the soldier's next settle is recorded
and wakes the commander again. A re-prompt that bypasses it can finish
without any notice, because the sentinel only reports a task leaving
"running".

Only a task in status "done", "shipped" or "unconfirmed" whose camp has not
been struck can be prompted. A shipped task's soldier is still open, waiting
for its pull request to be merged: prompting it asks for follow-up work. The
task goes to running while the soldier works and comes back as done, not
shipped, since the new commits are not on the pull request yet; run
'` + cmdname.Name + ` ship <task-id>' again to push them to the same pull request. A "blocked" task is answered with '` + cmdname.Name + ` decide'
(or, if it was marked blocked by mistake, cleared with '` + cmdname.Name + ` decide <task-id> --dismiss'), an "interrupted" one needs '` + cmdname.Name + ` redispatch', and a "running" one is already
working - wait for it to settle.

<text> is delivered to the soldier verbatim. It is also recorded on the task
as an amendment, with a timestamp and its source command, once it has been
delivered; the amendments are the field "amendments" of '` + cmdname.Name + ` status --json'.
A mission's tribunal review judges the change against the dispatch prompt followed by these later
instructions, so what you asked for here is not reported as unrequested. The
dispatch prompt itself is never changed. Amendments are written only by this
command and by '` + cmdname.Name + ` decide', into the task's state file, never into a file in the
camp. Each is capped at 2000 characters and a task keeps its 20 most recent.

On a fast settle the task's
status updates immediately: running if the soldier is still going, blocked if
it asks a question, done if it settled that quickly. Otherwise the task is
left running for the sentinel to record the eventual settle, exactly like a
fresh dispatch.
`

// Reprompt runs the "vx prompt" command.
func Reprompt(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(repromptUsage)
		return 0
	}
	if len(args) < 2 {
		fmt.Print(repromptUsage)
		return 1
	}

	projectDir, vexillumHome, err := resolveDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}

	taskID := args[0]
	text := strings.Join(args[1:], " ")

	code := runReprompt(projectDir, vexillumHome, taskID, text, herdr.CLI{}, os.Stdout, os.Stderr)
	if code == 0 {
		// The prompt went out, so the sentinel is what records its settle
		// if it outlives the quick probe - same reasoning as dispatch.
		ensureSentinelRunning(vexillumHome, os.Stderr)
	}
	return code
}

func runReprompt(projectDir, vexillumHome, taskID, text string, client herdr.Client, stdout, stderr io.Writer) int {
	if err := state.ValidateID(taskID); err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}
	if strings.TrimSpace(text) == "" {
		fmt.Fprintln(stderr, cmdname.Name+": the prompt text is empty")
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

	if msg := repromptRefusal(task); msg != "" {
		fmt.Fprintf(stderr, cmdname.Name+": %s\n", msg)
		return 1
	}

	leased, err := camp.LeasedTasks(projectRoot)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": reading camp pool: %v\n", err)
		return 1
	}
	if !leased[task.ID] {
		fmt.Fprintf(stderr, cmdname.Name+": task %s's camp was already struck, its soldier is gone - dispatch a new one instead\n", taskID)
		return 1
	}

	result, err := soldier.Reprompt(projectRoot, task, text, client)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "status=%s\n", result.Status)
	fmt.Fprintf(stdout, "prompted task %s, now %s\n", taskID, result.Status)
	return 0
}

// repromptRefusal explains why task cannot be re-prompted, or returns ""
// when it can.
func repromptRefusal(task state.Task) string {
	switch task.Status {
	case state.StatusDone, state.StatusShipped, state.StatusUnconfirmed:
		if task.HerdrAgentName == "" {
			return fmt.Sprintf("task %s has no herdr agent to prompt", task.ID)
		}
		return ""
	case state.StatusRunning:
		return fmt.Sprintf("task %s is already running - wait for it to settle", task.ID)
	case state.StatusBlocked:
		return blockedHint(task)
	case state.StatusInterrupted:
		return fmt.Sprintf("task %s is interrupted, its herdr pane is gone - use '%s redispatch %s' instead", task.ID, cmdname.Name, task.ID)
	default:
		return fmt.Sprintf("task %s is %s - only a done, shipped or unconfirmed task can be prompted again", task.ID, task.Status)
	}
}

// blockedHint is the refusal for a command that needs a task that is not
// blocked: it names both ways out, answering the question or, when the
// block was a false positive, dismissing it.
func blockedHint(task state.Task) string {
	question := ""
	if task.Decision != nil && task.Decision.Question != "" {
		question = fmt.Sprintf(" (%q)", task.Decision.Question)
	}
	return fmt.Sprintf("task %s is blocked on a question%s - answer it with '%s decide %s <answer>', or if it was marked blocked by mistake clear it with '%s decide %s --dismiss'",
		task.ID, question, cmdname.Name, task.ID, cmdname.Name, task.ID)
}
