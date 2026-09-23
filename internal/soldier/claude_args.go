package soldier

import (
	"fmt"

	"github.com/isaias-alt/vexillum/internal/state"
)

// allowedModels and allowedEfforts are the values vexillum accepts for a
// soldier's --model/--effort flags, straight from `claude --help` on the
// supported Claude Code CLI version. vexillum never interprets them for
// routing (ADR-05 reserves that judgment for the commander's own
// AGENTS.md) - it only checks a value is one claude itself understands
// before passing it through verbatim.
var (
	allowedModels = map[string]bool{
		"haiku":  true,
		"sonnet": true,
		"opus":   true,
		"fable":  true,
	}
	allowedEfforts = map[string]bool{
		"low":    true,
		"medium": true,
		"high":   true,
		"xhigh":  true,
		"max":    true,
	}
)

// ValidateModelEffort checks model and effort against the fixed set of
// values claude accepts. Either may be empty, meaning "don't pass this
// flag - let claude use its own default".
func ValidateModelEffort(model, effort string) error {
	if model != "" && !allowedModels[model] {
		return fmt.Errorf("unknown model %q (expected one of haiku, sonnet, opus, fable)", model)
	}
	if effort != "" && !allowedEfforts[effort] {
		return fmt.Errorf("unknown effort %q (expected one of low, medium, high, xhigh, max)", effort)
	}
	return nil
}

// claudeModelEffortArgs builds the extra CLI args for task's Model/Effort,
// omitting either that's unset so claude falls back to its own default.
func claudeModelEffortArgs(task state.Task) []string {
	var args []string
	if task.Model != "" {
		args = append(args, "--model", task.Model)
	}
	if task.Effort != "" {
		args = append(args, "--effort", task.Effort)
	}
	return args
}
