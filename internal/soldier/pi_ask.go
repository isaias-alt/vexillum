package soldier

import (
	"fmt"
	"time"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/piask"
	"github.com/isaias-alt/vexillum/internal/state"
)

// ExtensionHarness is implemented by a harness whose interactive soldier is
// launched with an extension of vexillum's own (Pi: the ask_user tool, see
// internal/piask). It is separate from Harness so a harness without one is
// untouched: callers ask for it with a type assertion.
type ExtensionHarness interface {
	// LaunchArgs installs the extension outside the camp and returns the
	// extra CLI args that load it for task's soldier, appended after the
	// model and effort args.
	LaunchArgs(projectRoot string, task state.Task) ([]string, error)

	// AskTool is the name of the question tool the extension gives the
	// soldier, which its prompt tells it to call.
	AskTool() string
}

// unblockPollInterval and unblockTimeout bound how long answerAsk waits for
// herdr to stop reporting the soldier as blocked once its question is closed.
const (
	unblockPollInterval = 100 * time.Millisecond
	unblockTimeout      = 3 * time.Second
)

// LaunchArgs installs the ask_user extension under projectRoot and returns
// the args that load it with "-e", plus the flag naming the task's protocol
// directory. -e extensions load before Pi's project-trust gate, so this
// works in a camp Pi has never seen, with no .pi added to it.
func (PiHarness) LaunchArgs(projectRoot string, task state.Task) ([]string, error) {
	if err := state.ValidateID(task.ID); err != nil {
		return nil, err
	}
	path, err := piask.Install(projectRoot)
	if err != nil {
		return nil, err
	}
	return []string{"-e", path, "--" + piask.Flag, piask.Dir(projectRoot, task.ID)}, nil
}

func (PiHarness) AskTool() string {
	return piask.ToolName
}

// openAskQuestion is the question task's soldier has open through ask_user,
// if it has one that is not already answered or dismissed. Only a harness
// with an extension (ExtensionHarness) can have one; any problem reading the
// file means no question, never a guess.
func openAskQuestion(projectRoot string, task state.Task) (piask.Question, bool) {
	h, err := HarnessFor(task.Harness)
	if err != nil {
		return piask.Question{}, false
	}
	if _, ok := h.(ExtensionHarness); !ok || state.ValidateID(task.ID) != nil {
		return piask.Question{}, false
	}
	q, err := piask.ReadQuestion(piask.Dir(projectRoot, task.ID))
	if err != nil {
		return piask.Question{}, false
	}
	if d := task.Decision; d != nil && d.QuestionID == q.ID && !d.AnsweredAt.IsZero() {
		return piask.Question{}, false
	}
	return q, true
}

// HasOpenAskQuestion reports whether task's soldier is waiting in ask_user.
// internal/sentinel uses it to see a Pi soldier as blocked even when herdr
// does not, which is the case when herdr's own Pi integration is not
// installed: that integration is what reports "blocked" for the tool.
func HasOpenAskQuestion(projectRoot string, task state.Task) bool {
	_, ok := openAskQuestion(projectRoot, task)
	return ok
}

// ResolveBlockedDecisionForTask is ResolveBlockedDecisionFor for a task that
// may be blocked on ask_user: the question the extension recorded is the
// Decision, with its options and id, and anything else is read as before.
func ResolveBlockedDecisionForTask(projectRoot string, task state.Task, client herdr.Client, agentName, output string) *state.Decision {
	if q, ok := openAskQuestion(projectRoot, task); ok {
		asked := q.AskedAt
		if asked.IsZero() {
			asked = time.Now()
		}
		return &state.Decision{
			Question:   q.Question,
			Options:    q.Options,
			Kind:       state.DecisionKindAsk,
			QuestionID: q.ID,
			AskedAt:    asked.UTC(),
		}
	}
	return ResolveBlockedDecisionFor(task.Harness, client, agentName, output)
}

// answerAsk delivers answer to the ask_user question task.Decision records:
// it writes the answer file the extension waits on, addressed to the
// question's id, waits for the extension to take it, and then settles like
// any other answer (finishAnswerBlocked).
//
// An answer file is used, and not keys or a prompt typed into the pane,
// because the extension's dialog is Pi's own select or input component: what
// a typed key does depends on which of the two is up and where its cursor
// is, and a prompt submitted to Pi while a tool is running is queued as a
// steering message instead of answering the tool. Pi's dialogs take an
// AbortSignal, which is what lets the extension watch the file and close the
// dialog itself when it appears.
func answerAsk(projectRoot string, task state.Task, answer string, client herdr.Client) (state.Task, error) {
	dir := piask.Dir(projectRoot, task.ID)
	q, err := piask.ReadQuestion(dir)
	if err != nil || q.ID != task.Decision.QuestionID {
		return task, fmt.Errorf("the question is no longer open in the soldier's session (answered in its pane, or the session ended); clear it with '%s decide %s --dismiss'", cmdname.Name, task.ID)
	}

	chosen, option := answer, 0
	if n, label, ok := resolveModalAnswer(q.Options, answer); ok {
		chosen, option = label, n
	}
	if err := piask.WriteAnswer(dir, q, chosen, option); err != nil {
		return task, fmt.Errorf("writing the answer: %w", err)
	}
	if !piask.WaitConsumed(dir, q, piask.ConsumeTimeout) {
		piask.Withdraw(dir)
		return task, fmt.Errorf("the soldier did not pick the answer up within %s; it was withdrawn. Check its pane, then try again or use '%s decide %s --dismiss'", piask.ConsumeTimeout, cmdname.Name, task.ID)
	}
	waitUnblocked(client, task.HerdrAgentName)

	status, err := waitProbe(client, task.HerdrAgentName)
	return finishAnswerBlocked(projectRoot, task, answer, chosen, status, err, client)
}

// waitUnblocked waits for herdr to stop reporting name as blocked. herdr's
// wait without a target state matches blocked too, so asking it right after
// the answer was taken could settle on the state the soldier is just leaving.
// A status that cannot be read, or one that stays blocked, ends the wait.
func waitUnblocked(client herdr.Client, name string) {
	deadline := time.Now().Add(unblockTimeout)
	for time.Now().Before(deadline) {
		live, err := client.AgentStatus(name)
		if err != nil || live != "blocked" {
			return
		}
		time.Sleep(unblockPollInterval)
	}
}
