package soldier

import (
	"fmt"
	"sort"
	"strings"

	"github.com/isaias-alt/vexillum/internal/state"
)

// CommandSpec is the process a Harness builds to run a soldier's prompt.
type CommandSpec struct {
	Command string
	Args    []string
}

// Harness abstracts the agent CLI vexillum drives to carry out a
// soldier's prompt. vexillum supports several harnesses (Claude Code, Pi and
// OpenCode, see AGENTS.md's multi-harness rule), each selected per model
// profile and recorded on the task as state.Task.Harness; HarnessFor is the
// registry that turns that name into an implementation. Adding a harness
// means implementing this interface and registering it there, instead of
// editing every place that hardcodes one CLI's shape.
//
// ClaudeHarness is the only implementation registered so far: Pi and
// OpenCode are reserved names that HarnessFor refuses with a clear error
// until their phase lands.
type Harness interface {
	// ValidateModelEffort checks model/effort against the fixed set of
	// values this harness's CLI accepts. Either may be empty, meaning
	// "don't pass this flag - let the harness use its own default".
	ValidateModelEffort(model, effort string) error

	// ModelEffortArgs builds the extra CLI args for task's Model/Effort,
	// omitting either that's unset. Shared between Command (the one-shot
	// path the tribunal's reviewer and fixer use) and RunInHerdr (a
	// soldier's interactive path) so both stay in sync.
	ModelEffortArgs(task state.Task) []string

	// Command builds the CommandSpec that runs task's prompt through the
	// real harness CLI, unattended, inside a camp. A soldier does not use
	// it (it runs interactively, see HerdrAgentKind): the tribunal's
	// reviewer and fixer do (internal/tribunal).
	Command(task state.Task) CommandSpec

	// HerdrAgentKind is the agent kind string passed to herdr when
	// starting a soldier's interactive pane (herdr.Client.AgentStart) -
	// herdr's own vocabulary for which CLI it launches there.
	HerdrAgentKind() string

	// HerdrExtraArgs returns the fixed extra CLI args herdr should pass
	// when starting the soldier's interactive agent (RunInHerdr), on top of task's Model/Effort args (added
	// separately - see ModelEffortArgs).
	HerdrExtraArgs() []string
}

// The harness names a model profile may select and a task records. HarnessClaude
// is the default, the one a profile or task without a harness means.
const (
	HarnessClaude   = "claude"
	HarnessPi       = "pi"
	HarnessOpenCode = "opencode"
)

// harnesses is the registry HarnessFor reads: the implemented harnesses,
// keyed by name.
var harnesses = map[string]Harness{
	HarnessClaude: ClaudeHarness{},
}

// reservedHarnesses are names vexillum has set aside for harnesses with no
// implementation yet, in the order error messages list them. A model profile
// may name one so the file does not have to change when it lands, but
// HarnessFor refuses it.
var reservedHarnesses = []string{HarnessPi, HarnessOpenCode}

// HarnessFor returns the Harness registered under name. An empty name means
// HarnessClaude, which is what a task or profile written before the harness
// field existed carries. A reserved name that is not implemented yet and an
// unknown name are different errors, so a typo is not mistaken for a
// harness that is merely on its way.
func HarnessFor(name string) (Harness, error) {
	if name == "" {
		name = HarnessClaude
	}
	if h, ok := harnesses[name]; ok {
		return h, nil
	}
	if isReservedHarness(name) {
		return nil, fmt.Errorf("harness %q is not implemented yet (supported: %s)", name, strings.Join(implementedHarnessNames(), ", "))
	}
	return nil, fmt.Errorf("unknown harness %q (supported: %s; reserved, not implemented yet: %s)",
		name, strings.Join(implementedHarnessNames(), ", "), strings.Join(reservedHarnesses, ", "))
}

// KnownHarness reports whether name is a harness vexillum knows about,
// implemented or reserved. models.json uses it to reject a typo in a
// profile's "harness" at load time without requiring the harness to be
// implemented yet. An empty name is known: it means HarnessClaude.
func KnownHarness(name string) bool {
	if name == "" {
		return true
	}
	if _, ok := harnesses[name]; ok {
		return true
	}
	return isReservedHarness(name)
}

func isReservedHarness(name string) bool {
	for _, r := range reservedHarnesses {
		if r == name {
			return true
		}
	}
	return false
}

func implementedHarnessNames() []string {
	names := make([]string, 0, len(harnesses))
	for n := range harnesses {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ClaudeHarness is the Harness for the `claude` CLI (Claude Code). It wraps the
// real `claude` CLI, both one-shot (Command, used by the tribunal's
// reviewer and fixer) and interactive inside a herdr pane
// (HerdrAgentKind/HerdrExtraArgs, used by RunInHerdr).
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
// It is turned off with the equivalent CLI flag (the
// CLAUDE_CODE_ENABLE_PROMPT_SUGGESTION=false env var would do the same). This only applies to a soldier's own
// interactive session - never the commander's, which the general starts
// directly, not vexillum; and never the one-shot Command path, which has
// no pane input box for a suggestion to sit in.
var claudeHerdrExtraArgs = []string{claudeSkipPermissionsArg, "--prompt-suggestions", "false"}

// claudeAllowedModels and claudeAllowedEfforts are the values vexillum
// accepts for a soldier's --model/--effort flags, straight from `claude
// --help` on the supported Claude Code CLI version. vexillum never
// interprets them for routing (that judgment belongs to the commander's
// own AGENTS.md) - it only checks a value is one claude
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
// landing approval step (camp.Land / vx land): nothing a soldier
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

// ValidateModelEffort checks model and effort against the fixed set of
// accepted values of the harness registered under harness ("" means
// claude). Either may be empty, meaning "don't pass this flag - let the
// harness use its own default". An unknown or unimplemented harness is the
// error HarnessFor returns.
func ValidateModelEffort(harness, model, effort string) error {
	h, err := HarnessFor(harness)
	if err != nil {
		return err
	}
	return h.ValidateModelEffort(model, effort)
}

// HarnessCommand builds the production CommandSpec for task through the
// harness registered under harness ("" means claude). The tribunal's
// reviewer and fixer run through it, so they use the same CLI as the
// mission they judge.
func HarnessCommand(harness string, task state.Task) (CommandSpec, error) {
	h, err := HarnessFor(harness)
	if err != nil {
		return CommandSpec{}, err
	}
	return h.Command(task), nil
}
