package soldier

import (
	"fmt"

	"github.com/isaias-alt/vexillum/internal/state"
)

// CommandSpec is the process a Harness builds to run a soldier's prompt.
type CommandSpec struct {
	Command string
	Args    []string
}

// Harness abstracts the agent CLI vexillum drives to carry out a
// soldier's prompt. It exists so a future harness beyond Claude Code
// (Pi, deferred past v1 - AGENTS.md's "Harness único: Claude Code en la
// v1") can be added by implementing this interface, instead of editing
// every place that currently hardcodes Claude Code's own CLI shape.
//
// v1 has exactly one implementation, ClaudeHarness - this is seam, not
// the multi-harness abstraction itself: nothing here selects between
// harnesses, and defaultHarness is still the only one vexillum ever
// constructs.
type Harness interface {
	// ValidateModelEffort checks model/effort against the fixed set of
	// values this harness's CLI accepts. Either may be empty, meaning
	// "don't pass this flag - let the harness use its own default".
	ValidateModelEffort(model, effort string) error

	// ModelEffortArgs builds the extra CLI args for task's Model/Effort,
	// omitting either that's unset. Shared between Command (the headless
	// path) and RunInHerdr (the interactive path) so both stay in sync.
	ModelEffortArgs(task state.Task) []string

	// Command builds the CommandSpec that runs task's prompt through the
	// real harness CLI, unattended, inside a camp (internal/soldier.Run,
	// the Layer 3 headless path).
	Command(task state.Task) CommandSpec

	// HerdrAgentKind is the agent kind string passed to herdr when
	// starting a soldier's interactive pane (herdr.Client.AgentStart) -
	// herdr's own vocabulary for which CLI it launches there.
	HerdrAgentKind() string

	// HerdrExtraArgs returns the fixed extra CLI args herdr should pass
	// when starting the soldier's interactive agent (RunInHerdr, the
	// Layer 4 path), on top of task's Model/Effort args (added
	// separately - see ModelEffortArgs).
	HerdrExtraArgs() []string
}

// defaultHarness is the Harness this build of vexillum drives. v1 only
// ever constructs ClaudeHarness here - this is the one place that would
// need to pick a different Harness once a second one exists.
var defaultHarness Harness = ClaudeHarness{}

// ClaudeHarness is vexillum's only Harness implementation in v1. It
// wraps the real `claude` CLI, both headless (Command, used by Run) and
// interactive inside a herdr pane (HerdrAgentKind/HerdrExtraArgs, used
// by RunInHerdr).
type ClaudeHarness struct{}

const (
	claudeHerdrAgentKind = "claude"

	// claudeSkipPermissionsArg gives the soldier full host access under
	// the invoking OS user - there is no container, chroot, or other
	// OS-level sandbox bounding it. See Command's and RunInHerdr's own
	// doc comments for what that actually means and what the real
	// safety control is.
	claudeSkipPermissionsArg = "--dangerously-skip-permissions"
)

// claudeHerdrExtraArgs are the fixed claude CLI args every soldier's
// interactive herdr session starts with (HerdrExtraArgs), on top of
// claudeSkipPermissionsArg. Claude Code CLI's prompt-suggestions feature
// can leave a suggested completion sitting in the pane's input box after
// a soldier's turn ends - text nobody typed or submitted, easy for the
// commander to mistake for something the soldier itself left behind.
// It's disabled the same way firstmate disables it for its own workers
// (CLAUDE_CODE_ENABLE_PROMPT_SUGGESTION=false), just via the equivalent
// CLI flag instead of an env var. This only applies to a soldier's own
// interactive session - never the commander's, which the general starts
// directly, not vexillum; and never the headless Command path, which has
// no pane input box for a suggestion to sit in.
var claudeHerdrExtraArgs = []string{claudeSkipPermissionsArg, "--prompt-suggestions", "false"}

// claudeAllowedModels and claudeAllowedEfforts are the values vexillum
// accepts for a soldier's --model/--effort flags, straight from `claude
// --help` on the supported Claude Code CLI version. vexillum never
// interprets them for routing (ADR-05 reserves that judgment for the
// commander's own AGENTS.md) - it only checks a value is one claude
// itself understands before passing it through verbatim.
var (
	claudeAllowedModels = map[string]bool{
		"haiku":  true,
		"sonnet": true,
		"opus":   true,
		"fable":  true,
	}
	claudeAllowedEfforts = map[string]bool{
		"low":    true,
		"medium": true,
		"high":   true,
		"xhigh":  true,
		"max":    true,
	}
)

func (ClaudeHarness) ValidateModelEffort(model, effort string) error {
	if model != "" && !claudeAllowedModels[model] {
		return fmt.Errorf("unknown model %q (expected one of haiku, sonnet, opus, fable)", model)
	}
	if effort != "" && !claudeAllowedEfforts[effort] {
		return fmt.Errorf("unknown effort %q (expected one of low, medium, high, xhigh, max)", effort)
	}
	return nil
}

func (ClaudeHarness) ModelEffortArgs(task state.Task) []string {
	var args []string
	if task.Model != "" {
		args = append(args, "--model", task.Model)
	}
	if task.Effort != "" {
		args = append(args, "--effort", task.Effort)
	}
	return args
}

// Command builds the production CommandSpec that runs task's prompt
// through the real Claude Code CLI, unattended, inside the camp.
//
// It runs with --dangerously-skip-permissions: a soldier has full host
// access under the invoking OS user - there is no container, chroot, or
// other OS-level sandbox. The camp's git worktree isolation only bounds
// where a soldier's commits can land, not what its process can read,
// write, or exfiltrate elsewhere on the machine (credentials, SSH keys,
// a sibling project's .env, etc.). The real safety control is the
// landing approval step (camp.Land / vexillum land): nothing a soldier
// does reaches the project's real history until a human explicitly
// approves it. Never dispatch a soldier against a prompt, repository, or
// machine where reading sensitive host state would be a problem.
func (h ClaudeHarness) Command(task state.Task) CommandSpec {
	args := []string{"-p", task.Prompt, claudeSkipPermissionsArg}
	args = append(args, h.ModelEffortArgs(task)...)
	return CommandSpec{
		Command: "claude",
		Args:    args,
	}
}

func (ClaudeHarness) HerdrAgentKind() string {
	return claudeHerdrAgentKind
}

func (ClaudeHarness) HerdrExtraArgs() []string {
	return append([]string{}, claudeHerdrExtraArgs...)
}

// ValidateModelEffort checks model and effort against defaultHarness's
// fixed set of accepted values. Either may be empty, meaning "don't pass
// this flag - let the harness use its own default".
func ValidateModelEffort(model, effort string) error {
	return defaultHarness.ValidateModelEffort(model, effort)
}

// ClaudeCommand builds the production CommandSpec for task via
// defaultHarness. Named after Claude Code specifically (rather than
// "HarnessCommand") because v1 has exactly one harness and callers
// already reason about it as "the claude command" - see Harness's own
// doc comment for why that's still fine to leave as a seam today.
func ClaudeCommand(task state.Task) CommandSpec {
	return defaultHarness.Command(task)
}
