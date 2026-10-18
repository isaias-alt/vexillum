package soldier_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

func newTask(t *testing.T) state.Task {
	t.Helper()
	task, err := state.New(state.KindMission, "say hello")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	return task
}

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
			err := soldier.ValidateModelEffort("", c.model, c.effort)
			if c.wantErr && err == nil {
				t.Fatal("expected an error")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// HarnessCommand (the one-shot path) appends the same --model/--effort
// args as the herdr path (see TestRunInHerdr_PassesModelAndEffort),
// omitting whichever one is unset.
func TestHarnessCommand_ModelEffort(t *testing.T) {
	task := newTask(t)
	task.Model = "opus"
	task.Effort = "high"

	cmd, err := soldier.HarnessCommand("claude", task)
	if err != nil {
		t.Fatalf("HarnessCommand: %v", err)
	}

	want := []string{"-p", task.Prompt, "--dangerously-skip-permissions", "--model", "opus", "--effort", "high"}
	if !slices.Equal(cmd.Args, want) {
		t.Errorf("got args %v, want %v", cmd.Args, want)
	}
}

func TestHarnessCommand_NoModelEffort(t *testing.T) {
	task := newTask(t)

	cmd, err := soldier.HarnessCommand("claude", task)
	if err != nil {
		t.Fatalf("HarnessCommand: %v", err)
	}

	want := []string{"-p", task.Prompt, "--dangerously-skip-permissions"}
	if !slices.Equal(cmd.Args, want) {
		t.Errorf("got args %v, want %v", cmd.Args, want)
	}
}

func TestHarnessFor_EmptyNameMeansClaude(t *testing.T) {
	for _, name := range []string{"", "claude"} {
		h, err := soldier.HarnessFor(name)
		if err != nil {
			t.Fatalf("HarnessFor(%q): %v", name, err)
		}
		if _, ok := h.(soldier.ClaudeHarness); !ok {
			t.Errorf("HarnessFor(%q) = %T, want ClaudeHarness", name, h)
		}
		if h.HerdrAgentKind() != "claude" {
			t.Errorf("HarnessFor(%q).HerdrAgentKind() = %q, want claude", name, h.HerdrAgentKind())
		}
	}
}

// OpenCode is reserved: selecting it fails with an error that says so, and
// that it is not a typo.
func TestHarnessFor_ReservedNamesAreNotImplemented(t *testing.T) {
	for _, name := range []string{"opencode"} {
		h, err := soldier.HarnessFor(name)
		if err == nil {
			t.Fatalf("HarnessFor(%q) = %T, want an error", name, h)
		}
		for _, want := range []string{name, "not implemented yet", "supported: claude, pi"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("HarnessFor(%q) error %q does not mention %q", name, err, want)
			}
		}
		if strings.Contains(err.Error(), "unknown harness") {
			t.Errorf("HarnessFor(%q) error %q calls a reserved name unknown", name, err)
		}
	}
}

func TestHarnessFor_UnknownName(t *testing.T) {
	_, err := soldier.HarnessFor("gemini")
	if err == nil {
		t.Fatal("expected an error for an unknown harness")
	}
	for _, want := range []string{`unknown harness "gemini"`, "supported: claude", "pi", "opencode"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestKnownHarness(t *testing.T) {
	for _, name := range []string{"", "claude", "pi", "opencode"} {
		if !soldier.KnownHarness(name) {
			t.Errorf("KnownHarness(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"Claude", "gemini", "codex", " "} {
		if soldier.KnownHarness(name) {
			t.Errorf("KnownHarness(%q) = true, want false", name)
		}
	}
}

func TestValidateModelEffort_UnknownHarness(t *testing.T) {
	if err := soldier.ValidateModelEffort("opencode", "sonnet", "high"); err == nil {
		t.Error("expected an error validating against a harness that is not implemented")
	}
	if err := soldier.ValidateModelEffort("claude", "sonnet", "high"); err != nil {
		t.Errorf("claude sonnet/high: %v", err)
	}
}

func TestHarnessCommand_UnknownHarness(t *testing.T) {
	task := newTask(t)
	for _, name := range []string{"opencode", "gemini"} {
		if _, err := soldier.HarnessCommand(name, task); err == nil {
			t.Errorf("HarnessCommand(%q): expected an error", name)
		}
	}
}
