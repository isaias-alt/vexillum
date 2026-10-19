package soldier

import (
	"fmt"
	"strings"

	"github.com/isaias-alt/vexillum/internal/state"
)

// PiHarness is the Harness for the `pi` CLI (the Pi coding agent), both
// one-shot (Command, used by the tribunal's reviewer and fixer) and
// interactive inside a herdr pane (HerdrAgentKind/HerdrExtraArgs, used by
// RunInHerdr).
//
// Everything below was checked against the docs and code that ship inside
// Pi 1.3.0 (the scout report that proposed it measured 0.85.1).
//
// Pi has no permission prompts and no skip flag, so the soldier needs
// nothing like Claude's --dangerously-skip-permissions: it runs with the
// full host access of the invoking OS user either way, and the real safety
// control is the same landing approval (camp.Land). Pi also has no
// AskUserQuestion-style selector, so vexillum gives the soldier an ask_user
// tool of its own (LaunchArgs, internal/piask) and answers its question
// through a file. A soldier without the tool asks through the
// needs-decision: line (see needsDecisionInstructions) and is answered as
// free text.
type PiHarness struct{}

const (
	piBinary         = "pi"
	piHerdrAgentKind = "pi"

	// piApproveArg trusts the project-local resources of the camp for this
	// process. Pi asks "Trust project folder?" in its interactive mode
	// whenever the working folder has .pi settings, extensions, skills,
	// prompts, themes or system-prompt files, or a .agents/skills directory
	// in it or in a parent; that dialog stops the session until someone
	// answers, and nobody is at a soldier's pane. A camp is a worktree of a
	// project the general already trusted enough to run init on, and the
	// soldier can already run any command as the invoking user, so loading
	// the project's own extensions grants it nothing more. What it does mean:
	// a project whose committed .pi/ holds an extension that does something
	// unwanted will run it. Without the flag the dialog would block the
	// soldier at startup, and answering it with a key press would save the
	// decision in ~/.pi/agent/trust.json for the camp's folder or its parent.
	piApproveArg = "--approve"
)

// piHerdrExtraArgs are the fixed pi CLI args every soldier's interactive
// herdr session starts with (HerdrExtraArgs). Besides piApproveArg,
// --tui-mode regular keeps the transcript in the terminal's normal
// scrollback: Pi has run fullscreen on the alternate screen by default since
// 1.0.0, where there is no scrollback and herdr could only read what is
// painted at that moment, while vexillum reads the transcript to record the
// soldier's final report and to find its needs-decision: line.
var piHerdrExtraArgs = []string{piApproveArg, "--tui-mode", "regular"}

// piReadOnlyArgs narrow a one-shot run to reading: an allowlist of Pi's
// read-only built-in tools, and no MCP, since --tools alone keeps MCP tools
// (their reach is set by the servers, not by --tools).
var piReadOnlyArgs = []string{"--tools", "read,grep,find,ls", "--no-mcp"}

// piThinkingLevels are the values of Pi's --thinking flag, from `pi --help`.
// Pi clamps a level the chosen model does not support to the nearest one it
// does, so a valid level is not a promise it is used as given.
var piThinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

func (PiHarness) Binary() string {
	return piBinary
}

// ValidateModelEffort checks effort against Pi's thinking levels and model by
// shape only: Pi's models are whatever its providers offer, a catalog that
// grows and differs per machine, so there is no fixed list to hold a value
// against. The check is advisory: a model that has the right shape but does
// not exist is Pi's error to report when the soldier starts.
func (PiHarness) ValidateModelEffort(model, effort string) error {
	if model != "" && !validPiModel(model) {
		return fmt.Errorf("model %q for harness pi must be provider/id, for example anthropic/claude-sonnet-5 (run 'pi --list-models' to see what is available)", model)
	}
	if effort != "" && !isPiThinkingLevel(effort) {
		return fmt.Errorf("unknown effort %q for harness pi (expected one of %s; it becomes --thinking, which Pi clamps to what the model supports)", effort, strings.Join(piThinkingLevels, ", "))
	}
	return nil
}

func isPiThinkingLevel(s string) bool {
	for _, l := range piThinkingLevels {
		if l == s {
			return true
		}
	}
	return false
}

// validPiModel reports whether m looks like provider/id: a provider name
// (letters, digits, dot, dash, underscore, starting with a letter or digit),
// a slash, and a non-empty id with no whitespace. The id may itself contain
// slashes (gateway providers nest them) and a colon (OpenRouter's ":free").
func validPiModel(m string) bool {
	provider, id, ok := strings.Cut(m, "/")
	if !ok || provider == "" || id == "" || strings.ContainsAny(m, " \t\r\n") {
		return false
	}
	for i, r := range provider {
		alnum := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if alnum || i > 0 && (r == '.' || r == '-' || r == '_') {
			continue
		}
		return false
	}
	return true
}

// Launch passes the task's model and effort as pi's own flags.
func (h PiHarness) Launch(task state.Task) (LaunchSpec, error) {
	return LaunchSpec{Args: h.ModelEffortArgs(task)}, nil
}

// ModelEffortArgs maps the task's model to --model and its effort to
// --thinking. The thinking level is never folded into --model as a ":level"
// suffix: a model id can itself end in a colon segment (OpenRouter's
// "...:free"), and --thinking overrides a suffix anyway.
func (PiHarness) ModelEffortArgs(task state.Task) []string {
	var args []string
	if task.Model != "" {
		args = append(args, "--model", task.Model)
	}
	if task.Effort != "" {
		args = append(args, "--thinking", task.Effort)
	}
	return args
}

// Command builds the CommandSpec that runs task's prompt through `pi -p`,
// unattended, inside the camp, with no session saved. Flags come first and
// the prompt last, after "--", so a prompt that begins with a dash is not
// read as an option. Print mode has no way to ask about project trust, so
// the camp's own .pi resources are skipped, and --approve is not passed:
// the tribunal does not need them.
//
// Like the soldier, it has full host access under the invoking OS user; see
// PiHarness for why that needs no flag. opts.ReadOnly adds piReadOnlyArgs.
func (h PiHarness) Command(task state.Task, opts CommandOptions) CommandSpec {
	args := []string{"-p", "--no-session"}
	args = append(args, h.ModelEffortArgs(task)...)
	if opts.ReadOnly {
		args = append(args, piReadOnlyArgs...)
	}
	args = append(args, "--", task.Prompt)
	return CommandSpec{
		Command: piBinary,
		Args:    args,
	}
}

func (PiHarness) HerdrAgentKind() string {
	return piHerdrAgentKind
}

func (PiHarness) HerdrExtraArgs() []string {
	return append([]string{}, piHerdrExtraArgs...)
}

// TrustDialog is the zero value: HerdrExtraArgs passes --approve, so Pi never
// asks, and a dialog that still shows up is not one to dismiss blindly.
func (PiHarness) TrustDialog() TrustDialog {
	return TrustDialog{}
}

func (PiHarness) QuestionModal() bool {
	return false
}
