package soldier_test

import (
	"slices"
	"testing"

	"github.com/isaias-alt/vexillum/internal/soldier"
)

func TestValidateModelEffort(t *testing.T) {
	cases := []struct {
		name    string
		model   string
		effort  string
		wantErr bool
	}{
		{"both empty", "", "", false},
		{"known model, empty effort", "sonnet", "", false},
		{"empty model, known effort", "", "medium", false},
		{"haiku low", "haiku", "low", false},
		{"opus high", "opus", "high", false},
		{"fable xhigh", "fable", "xhigh", false},
		{"max effort", "", "max", false},
		{"unknown model", "gpt-5", "", true},
		{"unknown effort", "", "extreme", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := soldier.ValidateModelEffort(c.model, c.effort)
			if c.wantErr && err == nil {
				t.Fatal("expected an error")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// ClaudeCommand (the headless path) appends the same --model/--effort
// args as the herdr path (see TestRunInHerdr_PassesModelAndEffort),
// omitting whichever one is unset.
func TestClaudeCommand_ModelEffort(t *testing.T) {
	task := newTask(t)
	task.Model = "opus"
	task.Effort = "high"

	cmd := soldier.ClaudeCommand(task)

	want := []string{"-p", task.Prompt, "--dangerously-skip-permissions", "--model", "opus", "--effort", "high"}
	if !slices.Equal(cmd.Args, want) {
		t.Errorf("got args %v, want %v", cmd.Args, want)
	}
}

func TestClaudeCommand_NoModelEffort(t *testing.T) {
	task := newTask(t)

	cmd := soldier.ClaudeCommand(task)

	want := []string{"-p", task.Prompt, "--dangerously-skip-permissions"}
	if !slices.Equal(cmd.Args, want) {
		t.Errorf("got args %v, want %v", cmd.Args, want)
	}
}
