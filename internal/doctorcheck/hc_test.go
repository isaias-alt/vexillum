package doctorcheck

import (
	"errors"
	"strings"
	"testing"
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
			"terminal backend":    "VX_BACKEND=hc",
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
		if len(got) != 2 || last.Name != "herdr-core" || last.OK || !last.Required || !strings.Contains(last.Detail, "not on PATH") {
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

func TestBackendValue(t *testing.T) {
	t.Setenv("VX_BACKEND", "")
	if r := BackendValue(); r.Name != "" {
		t.Errorf("an unset backend reports nothing, got %+v", r)
	}
	t.Setenv("VX_BACKEND", "hc")
	if r := BackendValue(); r.Name != "" {
		t.Errorf("hc reports nothing here, got %+v", r)
	}
	t.Setenv("VX_BACKEND", "zellij")
	if r := BackendValue(); r.Name != "VX_BACKEND" || r.OK || !r.Required || !strings.Contains(r.Detail, `"zellij"`) {
		t.Errorf("a typo must be a required failure, got %+v", r)
	}
}
