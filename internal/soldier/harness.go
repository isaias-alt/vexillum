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

// CommandOptions tune how Harness.Command builds the one-shot process.
type CommandOptions struct {
	// ReadOnly asks for a run that cannot change files, for a caller that
	// only wants to look (a reviewer). A harness enforces it with its own CLI
	// flags where it has any (Pi: an allowlist of read-only tools); one with
	// none to offer (ClaudeHarness) ignores it, and the caller's prompt is
	// then what keeps the run read-only.
	ReadOnly bool
}

// TrustDialog describes the first-run "do you trust this folder" dialog a
// harness's interactive session can stop on, so startAgent can recognize and
// dismiss it. The zero value means the harness has none to dismiss.
type TrustDialog struct {
	// Marker is text, lowercase, that the pane shows while the dialog is up.
	Marker string
	// Keys are the logical keys (herdr agent send-keys) that accept it.
	Keys []string
}

// Harness abstracts the agent CLI vexillum drives to carry out a
// soldier's prompt. vexillum supports several harnesses (Claude Code, Pi and
// OpenCode, see AGENTS.md's multi-harness rule), each selected per model
// profile and recorded on the task as state.Task.Harness; HarnessFor is the
// registry that turns that name into an implementation. Adding a harness
// means implementing this interface and registering it there, instead of
// editing every place that hardcodes one CLI's shape.
//
// ClaudeHarness and PiHarness are the implementations registered so far:
// OpenCode is a reserved name that HarnessFor refuses with a clear error
// until its phase lands.
type Harness interface {
	// Binary is the executable this harness's CLI runs as: the one dispatch
	// and doctor look for on PATH.
	Binary() string

	// ValidateModelEffort checks model/effort against what this harness's
	// CLI accepts: a fixed set for Claude, shape-only and advisory for a
	// harness whose models are not a closed list (Pi). Either may be empty,
	// meaning "don't pass this flag - let the harness use its own default".
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
	Command(task state.Task, opts CommandOptions) CommandSpec

	// HerdrAgentKind is the agent kind string passed to herdr when
	// starting a soldier's interactive pane (herdr.Client.AgentStart) -
	// herdr's own vocabulary for which CLI it launches there.
	HerdrAgentKind() string

	// HerdrExtraArgs returns the fixed extra CLI args herdr should pass
	// when starting the soldier's interactive agent (RunInHerdr), on top of task's Model/Effort args (added
	// separately - see ModelEffortArgs).
	HerdrExtraArgs() []string

	// TrustDialog describes the folder-trust dialog the interactive session
	// may open on in a never-seen camp, or the zero value when there is
	// nothing for startAgent to dismiss (a harness that never asks, or one
	// started with a flag that skips the question).
	TrustDialog() TrustDialog

	// QuestionModal reports whether the harness has a native question
	// selector whose rendering vexillum parses (Claude Code's
	// AskUserQuestion, see modal.go). When false, a blocked soldier's
	// question is read as prose and answered as free text through the normal
	// herdr prompt path, and the soldier's prompt does not mention a
	// question tool it does not have.
	QuestionModal() bool
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
	HarnessPi:     PiHarness{},
}

// reservedHarnesses are names vexillum has set aside for harnesses with no
// implementation yet, in the order error messages list them. A model profile
// may name one so the file does not have to change when it lands, but
// HarnessFor refuses it.
var reservedHarnesses = []string{HarnessOpenCode}

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
	claudeBinary         = "claude"
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
//
// opts.ReadOnly is ignored: Claude Code has no flag here that narrows it to
// reading, so a read-only caller relies on its prompt.
func (h ClaudeHarness) Command(task state.Task, _ CommandOptions) CommandSpec {
	args := []string{"-p", task.Prompt, claudeSkipPermissionsArg}
	args = append(args, h.ModelEffortArgs(task)...)
	return CommandSpec{
		Command: "claude",
		Args:    args,
	}
}

func (ClaudeHarness) Binary() string {
	return claudeBinary
}

func (ClaudeHarness) HerdrAgentKind() string {
	return claudeHerdrAgentKind
}

func (ClaudeHarness) HerdrExtraArgs() []string {
	return append([]string{}, claudeHerdrExtraArgs...)
}

// TrustDialog is Claude Code's one-time workspace trust dialog. A camp is a
// worktree of a project the general already trusted enough to run init on,
// so it is accepted by moving to the "Yes" entry and confirming.
func (ClaudeHarness) TrustDialog() TrustDialog {
	return TrustDialog{Marker: "trust this folder", Keys: []string{"down", "enter"}}
}

func (ClaudeHarness) QuestionModal() bool {
	return true
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
	return HarnessCommandWith(harness, task, CommandOptions{})
}

// HarnessCommandWith is HarnessCommand with CommandOptions.
func HarnessCommandWith(harness string, task state.Task, opts CommandOptions) (CommandSpec, error) {
	h, err := HarnessFor(harness)
	if err != nil {
		return CommandSpec{}, err
	}
	return h.Command(task, opts), nil
}

// HarnessBinary is the executable of the harness registered under harness
// ("" means claude).
func HarnessBinary(harness string) (string, error) {
	h, err := HarnessFor(harness)
	if err != nil {
		return "", err
	}
	return h.Binary(), nil
}
