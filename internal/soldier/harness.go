package soldier

import (
	"fmt"

	"github.com/isaias-alt/vexillum/internal/state"
)

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

	// claudeSkipPermissionsArg is safe here specifically because the
	// camp's git worktree isolation bounds what the soldier can affect -
	// see Command's and RunInHerdr's own doc comments.
	claudeSkipPermissionsArg = "--dangerously-skip-permissions"
)

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
// It runs with --dangerously-skip-permissions: the camp's git worktree
// isolation is what makes that acceptable here, since a soldier can only
// ever affect its own disposable worktree, not the project's own working
// tree or anything outside it.
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
	return []string{claudeSkipPermissionsArg}
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
