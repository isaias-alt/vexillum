package soldier_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/isaias-alt/vexillum/internal/opencodeosc"
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

// Only an OpenCode 2.x soldier gets a status reporter, and what it gets is one
// TUI-only variable carrying the plugins list, written outside the camp. The
// value is exactly what lands on the hc launch line, so a change here is a
// change to how an OpenCode soldier starts.
func TestReporterEnv(t *testing.T) {
	root := t.TempDir()
	got, err := soldier.OpenCodeHarness{Major: 2}.ReporterEnv(root)
	if err != nil {
		t.Fatalf("ReporterEnv: %v", err)
	}
	want := map[string]string{"OPENCODE_CLI_CONFIG_CONTENT": `{"plugins":["` + filepath.Join(root, "opencode", "osc7501") + `"]}`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReporterEnv = %#v, want %#v", got, want)
	}
	if st, err := opencodeosc.Inspect(root); err != nil || st != opencodeosc.Current {
		t.Errorf("plugin state = %v, %v; want installed and current", st, err)
	}

	for _, major := range []int{0, 1} {
		other := t.TempDir()
		env, err := soldier.OpenCodeHarness{Major: major}.ReporterEnv(other)
		if err != nil || len(env) != 0 {
			t.Errorf("Major %d ReporterEnv = %v, %v; want nothing (the plugin is for OpenCode 2.x)", major, env, err)
		}
		if st, _ := opencodeosc.Inspect(other); st != opencodeosc.Missing {
			t.Errorf("Major %d installed the plugin", major)
		}
	}

	for _, h := range []soldier.Harness{soldier.ClaudeHarness{}, soldier.PiHarness{}} {
		if _, ok := h.(soldier.StatusReporterHarness); ok {
			t.Errorf("%T is a StatusReporterHarness: it reports its own status", h)
		}
	}
}
