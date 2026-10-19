package soldier_test

import (
	"reflect"
	"testing"

	"github.com/isaias-alt/vexillum/internal/soldier"
)

// The launch of the Claude and Pi harnesses is exactly what it was before the
// LaunchSpec learned to prepare a session: their CLI args, no environment and
// no Prepare step. A change here is a change to what those soldiers start with.
func TestLaunch_ClaudeAndPiAreUnchanged(t *testing.T) {
	cases := []struct {
		name string
		got  func() (soldier.LaunchSpec, error)
		want soldier.LaunchSpec
	}{
		{"claude model and effort", func() (soldier.LaunchSpec, error) {
			task := newTask(t)
			task.Model, task.Effort = "sonnet", "high"
			return soldier.ClaudeHarness{}.Launch(task)
		}, soldier.LaunchSpec{Args: []string{"--model", "sonnet", "--effort", "high"}}},
		{"claude defaults", func() (soldier.LaunchSpec, error) {
			return soldier.ClaudeHarness{}.Launch(newTask(t))
		}, soldier.LaunchSpec{}},
		{"pi model and thinking", func() (soldier.LaunchSpec, error) {
			return soldier.PiHarness{}.Launch(piTask(t, "anthropic/claude-sonnet-5", "high"))
		}, soldier.LaunchSpec{Args: []string{"--model", "anthropic/claude-sonnet-5", "--thinking", "high"}}},
		{"pi defaults", func() (soldier.LaunchSpec, error) {
			return soldier.PiHarness{}.Launch(piTask(t, "", ""))
		}, soldier.LaunchSpec{}},
		{"opencode V1", func() (soldier.LaunchSpec, error) {
			return soldier.OpenCodeHarness{Major: 1}.Launch(ocTask(t, 1, "opencode/glm-5.3-flash-free", "medium"))
		}, soldier.LaunchSpec{Args: []string{"--model", "opencode/glm-5.3-flash-free"}, Env: map[string]string{"OPENCODE_DISABLE_AUTOUPDATE": "1"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.got()
			if err != nil {
				t.Fatal(err)
			}
			if got.Prepare != nil {
				t.Error("Prepare is set: only an OpenCode V2 soldier prepares a session")
			}
			got.Prepare = nil
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("Launch = %#v, want %#v", got, c.want)
			}
		})
	}
}
