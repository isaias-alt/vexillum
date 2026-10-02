package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunModelsDefaults(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runModels(t.TempDir(), t.TempDir(), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{
		"scout-facts  (haiku, low)",
		"mission-big  (sonnet, high)",
		"opus-on-request  (opus, high)",
		"default  (sonnet, medium)",
		"Source: built-in defaults only",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	// Profiles are listed in priority order, default last.
	if strings.Index(got, "scout-facts") > strings.Index(got, "mission-big") ||
		strings.Index(got, "opus-on-request") > strings.Index(got, "default  (") {
		t.Errorf("unexpected order:\n%s", got)
	}
}

func TestRunModelsShowsMergedFilesAndErrors(t *testing.T) {
	project, home := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".vexillum"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project, ".vexillum", "models.json")
	if err := os.WriteFile(path, []byte(`{"profiles": {"mission-big": {"model": "opus"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := runModels(project, home, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "mission-big  (opus, high)") || !strings.Contains(out.String(), path) {
		t.Errorf("output:\n%s", out.String())
	}

	if err := os.WriteFile(path, []byte(`{"default": {"effort": "wild"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := runModels(project, home, &out, &errOut); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), path) || !strings.Contains(errOut.String(), "default.effort") {
		t.Errorf("stderr: %s", errOut.String())
	}
}
