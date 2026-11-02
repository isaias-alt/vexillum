package doctorcheck

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/opencodeosc"
	"github.com/isaias-alt/vexillum/internal/project"
)

func hcEnvOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestHCBackend(t *testing.T) {
	look := func(name string) (string, error) {
		if name == "herdr-core" {
			return "/opt/herdr-core", nil
		}
		return "", errors.New("not found")
	}
	version := func(string) (string, error) { return "0.1.16", nil }
	hello := func(string) error { return nil }

	t.Run("all good", func(t *testing.T) {
		got := hcBackend(hcEnvOf(map[string]string{"HC_TERMINAL_ID": "t3", "HC_SESSION": "work"}), look, version, hello)
		want := map[string]string{
			"herdr-core":          "/opt/herdr-core (hc 0.1.16)",
			"herdr-core protocol": "generation 1",
			"Herdr app terminal":  "terminal t3, session work",
			"herdr-app":           "not on PATH",
		}
		if len(got) != len(want) {
			t.Fatalf("got %d results, want %d: %+v", len(got), len(want), got)
		}
		for _, r := range got {
			if !r.OK || r.Warn || !strings.Contains(r.Detail, want[r.Name]) {
				t.Errorf("%s = %+v, want ok and %q", r.Name, r, want[r.Name])
			}
		}
	})
	t.Run("no core is a required failure and the rest is skipped", func(t *testing.T) {
		got := hcBackend(hcEnvOf(nil), func(string) (string, error) { return "", errors.New("no") }, version, hello)
		last := got[len(got)-1]
		if len(got) != 1 || last.Name != "herdr-core" || last.OK || !last.Required || !strings.Contains(last.Detail, "not on PATH") {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("a TUI named like the core is not the core", func(t *testing.T) {
		got := hcBackend(hcEnvOf(nil), look, func(string) (string, error) { return "", errors.New(`--version printed "herdr 0.9.5"`) }, hello)
		last := got[len(got)-1]
		if last.OK || !strings.Contains(last.Detail, "herdr 0.9.5") {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("a hello failure warns, it does not fail doctor", func(t *testing.T) {
		got := hcBackend(hcEnvOf(map[string]string{"HC_TERMINAL_ID": "t1"}), look, version, func(string) error { return errors.New("generation 2") })
		for _, r := range got {
			if r.Name == "herdr-core protocol" && (!r.Warn || r.Required || !strings.Contains(r.Detail, "generation 2")) {
				t.Errorf("hello failure = %+v", r)
			}
		}
	})
	t.Run("outside an app terminal it warns", func(t *testing.T) {
		got := hcBackend(hcEnvOf(nil), look, version, hello)
		last := got[len(got)-2]
		if last.Name != "Herdr app terminal" || !last.Warn || !strings.Contains(last.Detail, "HC_TERMINAL_ID is not set") {
			t.Errorf("got %+v", last)
		}
	})
}

func TestHCBackendReportsHerdrAppInformationally(t *testing.T) {
	look := func(name string) (string, error) { return "/opt/" + name, nil }
	got := hcBackend(hcEnvOf(nil), look, func(string) (string, error) { return "0.1.16", nil }, func(string) error { return nil })
	last := got[len(got)-1]
	if last.Name != "herdr-app" || !last.OK || last.Warn || last.Required || !strings.Contains(last.Detail, "/opt/herdr-app") {
		t.Errorf("got %+v", last)
	}
}

func TestBackendChoice(t *testing.T) {
	for _, c := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"unset is the TUI by default", nil, "herdr TUI - default (VX_BACKEND is not set)"},
		{"explicit TUI", map[string]string{"VX_BACKEND": "herdr"}, "herdr TUI - chosen by VX_BACKEND=herdr"},
		{"explicit hc", map[string]string{"VX_BACKEND": "hc"}, "herdr-core, experimental (the herdr TUI is not used) - chosen by VX_BACKEND=hc"},
		{"detected in an app terminal", map[string]string{"HC_TERMINAL_ID": "t1"}, "herdr-core, experimental (the herdr TUI is not used) - detected from the Herdr app terminal (HC_TERMINAL_ID is set, VX_BACKEND is not)"},
		{"explicit TUI wins in an app terminal", map[string]string{"HC_TERMINAL_ID": "t1", "VX_BACKEND": "herdr"}, "herdr TUI - chosen by VX_BACKEND=herdr"},
	} {
		r := backendChoice(hcEnvOf(c.env))
		if r.Name != "terminal backend" || !r.OK || r.Warn || r.Required || r.Detail != c.want {
			t.Errorf("%s: got %+v, want ok %q", c.name, r, c.want)
		}
	}
	r := backendChoice(hcEnvOf(map[string]string{"VX_BACKEND": "zellij"}))
	if r.Name != "VX_BACKEND" || r.OK || !r.Required || !strings.Contains(r.Detail, `"zellij"`) {
		t.Errorf("a typo must be a required failure, got %+v", r)
	}
}

func TestHCOpenCodePlugin(t *testing.T) {
	const name = "opencode status plugin"
	// Each case gets its own project and home: the plugin lives under the
	// project's state in the home.
	setup := func(t *testing.T, profiles, version string) (project, home, pathEnv string) {
		t.Helper()
		project = projectWithProfiles(t, profiles)
		home = t.TempDir()
		if version != "" {
			pathEnv = ocBin(t, version)
		} else {
			pathEnv = binDir(t, map[string]string{"herdr": "exit 0"})
		}
		return
	}
	root := func(t *testing.T, projectDir, home string) string {
		t.Helper()
		r, err := project.Root(home, projectDir)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	t.Run("no opencode profile", func(t *testing.T) {
		project, home, pathEnv := setup(t, "", "opencode v2.0.32")
		if got := hcOpenCodePlugin(project, home, pathEnv, nil); len(got) != 0 {
			t.Errorf("got %+v, want nothing", got)
		}
	})

	t.Run("opencode missing or unreadable is not repeated here", func(t *testing.T) {
		project, home, pathEnv := setup(t, openCodeProfile, "")
		if got := hcOpenCodePlugin(project, home, pathEnv, nil); len(got) != 0 {
			t.Errorf("got %+v, want nothing (OpenCodeChecks reports it)", got)
		}
		project, home, pathEnv = setup(t, openCodeProfile, "garbage")
		if got := hcOpenCodePlugin(project, home, pathEnv, nil); len(got) != 0 {
			t.Errorf("unreadable version: got %+v, want nothing", got)
		}
	})

	t.Run("v1 has no plugin", func(t *testing.T) {
		project, home, pathEnv := setup(t, openCodeProfile, "1.19.3")
		got := hcOpenCodePlugin(project, home, pathEnv, nil)
		if len(got) != 1 || got[0].Name != name || !got[0].Warn || got[0].Required || !strings.Contains(got[0].Detail, "1.19.3") || !strings.Contains(got[0].Detail, "oc-scout") {
			t.Errorf("got %+v, want one non-required warning naming 1.19.3 and oc-scout", got)
		}
	})

	t.Run("v2 not installed yet", func(t *testing.T) {
		project, home, pathEnv := setup(t, openCodeProfile, "opencode v2.0.32")
		got := hcOpenCodePlugin(project, home, pathEnv, nil)
		if len(got) != 1 || got[0].Name != name || !got[0].OK || got[0].Warn || got[0].Required || !strings.Contains(got[0].Detail, "not installed yet") {
			t.Errorf("got %+v, want an ok line saying it is installed at the next dispatch", got)
		}
	})

	t.Run("v2 current and stale", func(t *testing.T) {
		project, home, pathEnv := setup(t, openCodeProfile, "opencode v2.0.32")
		r := root(t, project, home)
		if _, err := opencodeosc.Install(r); err != nil {
			t.Fatal(err)
		}
		got := hcOpenCodePlugin(project, home, pathEnv, nil)
		if len(got) != 1 || !got[0].OK || got[0].Required || !strings.Contains(got[0].Detail, "(current)") || !strings.Contains(got[0].Detail, opencodeosc.Path(r)) {
			t.Errorf("current: got %+v", got)
		}
		if err := os.WriteFile(opencodeosc.Path(r), []byte("// old\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got = hcOpenCodePlugin(project, home, pathEnv, nil)
		if len(got) != 1 || !got[0].OK || got[0].Required || !strings.Contains(got[0].Detail, "differs") {
			t.Errorf("stale: got %+v", got)
		}
	})
}
