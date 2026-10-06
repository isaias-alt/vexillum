package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStderr runs fn with os.Stderr redirected and returns what it wrote.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()
	w.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestHerdrPreconditions(t *testing.T) {
	found := func(string) (string, error) { return "/bin/x", nil }
	missing := func(name string) (string, error) { return "", errors.New("not found: " + name) }
	only := func(have string) func(string) (string, error) {
		return func(name string) (string, error) {
			if name == have {
				return "/bin/" + name, nil
			}
			return "", errors.New("not found: " + name)
		}
	}
	env := func(id string) func(string) string {
		return func(key string) string {
			if key == "HERDR_WORKSPACE_ID" {
				return id
			}
			return ""
		}
	}

	t.Run("everything present returns the workspace id", func(t *testing.T) {
		id, err := herdrPreconditions("dispatch", env("w7"), found)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "w7" {
			t.Errorf("workspace id = %q, want w7", id)
		}
	})

	cases := []struct {
		name     string
		getenv   func(string) string
		lookPath func(string) (string, error)
		want     []string
		notWant  []string
	}{
		{
			name:     "not in a herdr pane",
			getenv:   env(""),
			lookPath: found,
			want:     []string{"HERDR_WORKSPACE_ID is not set - dispatch must run from inside a herdr-managed pane", "vx doctor"},
			notWant:  []string{"is not on PATH"},
		},
		{
			name:     "herdr missing",
			getenv:   env("w1"),
			lookPath: only("claude"),
			want:     []string{"herdr is not on PATH", "brew install herdr", "https://herdr.dev", "vx doctor"},
			notWant:  []string{"HERDR_WORKSPACE_ID", "claude is not on PATH"},
		},
		{
			name:     "claude missing",
			getenv:   env("w1"),
			lookPath: only("herdr"),
			want:     []string{"claude is not on PATH", "https://claude.com/claude-code", "vx doctor"},
			notWant:  []string{"HERDR_WORKSPACE_ID", "herdr is not on PATH"},
		},
		{
			name:     "everything missing is reported at once",
			getenv:   env(""),
			lookPath: missing,
			want:     []string{"HERDR_WORKSPACE_ID is not set", "herdr is not on PATH", "claude is not on PATH", "vx doctor"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := herdrPreconditions("dispatch", c.getenv, c.lookPath)
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error missing %q:\n%s", w, err)
				}
			}
			for _, n := range c.notWant {
				if strings.Contains(err.Error(), n) {
					t.Errorf("error must not mention %q:\n%s", n, err)
				}
			}
		})
	}
}

// dispatchFailurePathEnv sets up an initialized project as the working
// directory, an isolated HOME with no ~/.vexillum yet, and a PATH holding git
// plus the named fake tools, then returns the home. Because ~/.vexillum does
// not exist, a command that wrongly went on to start the sentinel could not
// even open its log, and one that created a task or leased a camp would have
// to create it: both show up as a leftover.
func dispatchFailurePathEnv(t *testing.T, herdrWorkspaceID string, tools ...string) string {
	t.Helper()
	project := initDispatchTestProject(t)
	if err := writeLocalConfig(filepath.Join(project, ".vexillum")); err != nil {
		t.Fatalf("writing local config: %v", err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", fakeBinDir(t, tools...))
	t.Setenv("HERDR_WORKSPACE_ID", herdrWorkspaceID)
	t.Chdir(project)
	return home
}

func requireNothingCreated(t *testing.T, home, stderr string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(home, ".vexillum")); !os.IsNotExist(err) {
		var found []string
		_ = filepath.Walk(filepath.Join(home, ".vexillum"), func(p string, _ os.FileInfo, _ error) error {
			found = append(found, p)
			return nil
		})
		t.Errorf("~/.vexillum must not exist after a refused command (task state, camp lease or sentinel files), found:\n%s", strings.Join(found, "\n"))
	}
	if strings.Contains(stderr, "sentinel") || strings.Contains(stderr, "task_id=") {
		t.Errorf("a refused command must not start the sentinel or a task, stderr:\n%s", stderr)
	}
	if strings.Contains(stderr, "executable file not found") {
		t.Errorf("the raw exec error must not reach the user, stderr:\n%s", stderr)
	}
}

func TestDispatch_RefusesBeforeCreatingAnything(t *testing.T) {
	cases := []struct {
		name        string
		workspaceID string
		tools       []string
		want        []string
	}{
		{"empty environment", "", nil, []string{"HERDR_WORKSPACE_ID is not set", "herdr is not on PATH", "claude is not on PATH"}},
		{"outside a herdr pane", "", []string{"herdr", "claude"}, []string{"HERDR_WORKSPACE_ID is not set"}},
		{"herdr missing", "w1", []string{"claude"}, []string{"herdr is not on PATH"}},
		{"claude missing", "w1", []string{"herdr"}, []string{"claude is not on PATH"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := dispatchFailurePathEnv(t, c.workspaceID, c.tools...)

			var code int
			stderr := captureStderr(t, func() { code = Dispatch([]string{"do a thing"}) })

			if code == 0 {
				t.Fatalf("expected a non-zero exit, stderr:\n%s", stderr)
			}
			for _, w := range c.want {
				if !strings.Contains(stderr, w) {
					t.Errorf("stderr missing %q:\n%s", w, stderr)
				}
			}
			if !strings.Contains(stderr, "vx doctor") {
				t.Errorf("stderr should point to vx doctor:\n%s", stderr)
			}
			requireNothingCreated(t, home, stderr)
		})
	}
}

func TestRedispatch_RefusesBeforeCreatingAnything(t *testing.T) {
	for _, c := range []struct {
		name        string
		workspaceID string
		tools       []string
		want        string
	}{
		{"outside a herdr pane", "", []string{"herdr", "claude"}, "HERDR_WORKSPACE_ID is not set - redispatch must run from inside a herdr-managed pane"},
		{"herdr missing", "w1", []string{"claude"}, "herdr is not on PATH"},
		{"claude missing", "w1", []string{"herdr"}, "claude is not on PATH"},
	} {
		t.Run(c.name, func(t *testing.T) {
			home := dispatchFailurePathEnv(t, c.workspaceID, c.tools...)

			var code int
			stderr := captureStderr(t, func() { code = Redispatch([]string{"0123456789abcdef"}) })

			if code == 0 {
				t.Fatalf("expected a non-zero exit, stderr:\n%s", stderr)
			}
			if !strings.Contains(stderr, c.want) {
				t.Errorf("stderr missing %q:\n%s", c.want, stderr)
			}
			requireNothingCreated(t, home, stderr)
		})
	}
}
