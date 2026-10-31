package cli

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/project"
)

// fakeHCPath is the fake hc: its --version answers like the real core, so it
// stands in for the herdr-core binary on PATH.
func fakeHCPath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "bench", "fakes", "hc"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestHCPreconditions(t *testing.T) {
	fake := fakeHCPath(t)
	look := func(have ...string) func(string) (string, error) {
		return func(name string) (string, error) {
			for _, h := range have {
				if name == h {
					if name == "herdr-core" || name == "hc" {
						return fake, nil
					}
					return "/bin/" + name, nil
				}
			}
			return "", errors.New("not found: " + name)
		}
	}
	app := map[string]string{"VX_BACKEND": "hc", "HC_TERMINAL_ID": "t1"}

	t.Run("an app terminal with the core and claude is enough, and no herdr TUI", func(t *testing.T) {
		id, err := herdrPreconditions("dispatch", envMap(app), look("herdr-core", "claude"))
		if err != nil || id != "" {
			t.Fatalf("got %q, %v; want no workspace id (the space comes from the project) and no error", id, err)
		}
	})
	t.Run("a plain terminal is refused", func(t *testing.T) {
		_, err := herdrPreconditions("dispatch", envMap(map[string]string{"VX_BACKEND": "hc"}), look("herdr-core", "claude"))
		if err == nil || !strings.Contains(err.Error(), "Herdr app terminal") {
			t.Fatalf("got %v, want the app-terminal message", err)
		}
		if strings.Contains(err.Error(), "HERDR_WORKSPACE_ID") {
			t.Errorf("hc mode must not ask for HERDR_WORKSPACE_ID: %v", err)
		}
	})
	t.Run("the core must be on PATH", func(t *testing.T) {
		_, err := herdrPreconditions("dispatch", envMap(app), look("claude"))
		if err == nil || !strings.Contains(err.Error(), "herdr-core is not on PATH") {
			t.Fatalf("got %v, want the missing core named", err)
		}
	})
	t.Run("a herdr TUI answering to the core's name is refused", func(t *testing.T) {
		t.Setenv("VXFAKE_HC_VERSION_OUT", "herdr 0.9.5")
		_, err := herdrPreconditions("dispatch", envMap(app), look("herdr-core", "hc", "claude"))
		if err == nil || !strings.Contains(err.Error(), "herdr TUI") {
			t.Fatalf("got %v, want the TUI rejected", err)
		}
	})
	t.Run("the harness is still needed", func(t *testing.T) {
		_, err := herdrPreconditions("dispatch", envMap(app), look("herdr-core"))
		if err == nil || !strings.Contains(err.Error(), "claude is not on PATH") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("the herdr backend still wants a pane", func(t *testing.T) {
		_, err := herdrPreconditions("dispatch", envMap(map[string]string{"HC_TERMINAL_ID": "t1"}), look("herdr", "claude"))
		if err == nil || !strings.Contains(err.Error(), "VX_BACKEND=hc") {
			t.Fatalf("got %v, want the app terminal message to point at VX_BACKEND=hc", err)
		}
	})
}

func TestCommanderPaneAndManagedTerminal(t *testing.T) {
	tui := map[string]string{"HERDR_WORKSPACE_ID": "w1", "HERDR_PANE_ID": "w1:p2"}
	hc := map[string]string{"VX_BACKEND": "hc", "HC_TERMINAL_ID": "t5"}
	both := map[string]string{"VX_BACKEND": "hc", "HC_TERMINAL_ID": "t5", "HERDR_WORKSPACE_ID": "w1", "HERDR_PANE_ID": "w1:p2"}

	for _, c := range []struct {
		name    string
		env     map[string]string
		pane    string
		managed bool
	}{
		{"herdr pane", tui, "w1:p2", true},
		{"app terminal with the hc backend", hc, "t5", true},
		{"app terminal without it", map[string]string{"HC_TERMINAL_ID": "t5"}, "", false},
		{"the selected backend wins", both, "t5", true},
		{"nowhere", nil, "", false},
	} {
		if got := commanderPane(envMap(c.env)); got != c.pane {
			t.Errorf("%s: commanderPane = %q, want %q", c.name, got, c.pane)
		}
		if got := inManagedTerminal(envMap(c.env)); got != c.managed {
			t.Errorf("%s: inManagedTerminal = %v, want %v", c.name, got, c.managed)
		}
	}
}

// The Stop hook's await runs from an app terminal when the hc backend is
// selected, and still explains itself when it is not.
func TestRunSentinelAwaitGuarded_HCBackend(t *testing.T) {
	var stderr bytes.Buffer
	ran := false
	code := runSentinelAwaitGuarded(envMap(map[string]string{"VX_BACKEND": "hc", "HC_TERMINAL_ID": "t1"}), &stderr, func() int { ran = true; return 7 })
	if code != 7 || !ran {
		t.Fatalf("code = %d, ran = %v; want the await to run in an app terminal", code, ran)
	}

	stderr.Reset()
	ran = false
	code = runSentinelAwaitGuarded(envMap(map[string]string{"HC_TERMINAL_ID": "t1"}), &stderr, func() int { ran = true; return 7 })
	if code != 0 || ran || !strings.Contains(stderr.String(), "VX_BACKEND=hc") {
		t.Errorf("without the backend: code = %d, ran = %v, stderr = %q; want an explained no-op", code, ran, stderr.String())
	}
}

func TestBackendWorkspace(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()

	t.Setenv("VX_BACKEND", "")
	if got, err := backendWorkspace("w9", home, dir); err != nil || got != "w9" {
		t.Errorf("herdr backend: got %q, %v; want the caller's workspace w9", got, err)
	}

	t.Setenv("VX_BACKEND", "hc")
	root, err := project.Root(home, dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := backendWorkspace("", home, dir)
	if err != nil || got != "vx-"+filepath.Base(root) {
		t.Errorf("hc backend: got %q, %v; want vx-%s", got, err, filepath.Base(root))
	}
	if got != herdr.HCSpace(root) {
		t.Errorf("the space is herdr.HCSpace(root) = %q, got %q", herdr.HCSpace(root), got)
	}
}

func TestCheckBackend(t *testing.T) {
	t.Setenv("VX_BACKEND", "tmux")
	if err := CheckBackend(); err == nil || !strings.Contains(err.Error(), `"tmux"`) {
		t.Errorf("a typo must be rejected naming the value, got %v", err)
	}
	t.Setenv("VX_BACKEND", "hc")
	if err := CheckBackend(); err != nil {
		t.Errorf("hc is valid: %v", err)
	}
}
