package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A Claude-only environment prints no Pi line, whatever else it reports.
func TestDoctor_NoPiLinesForAClaudeOnlySetup(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr"))
	var out bytes.Buffer
	code := runDoctor(initializedProject(t), t.TempDir(), t.TempDir(), &out)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
	if strings.Contains(out.String(), "Pi") || strings.Contains(out.String(), "integration") {
		t.Errorf("a setup with no pi printed a Pi line:\n%s", out.String())
	}
}

// A pi profile on a machine without pi warns and does not fail doctor: the
// Claude environment is still ready.
func TestDoctor_PiProfileWithoutPiWarnsButStaysReady(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr"))
	project := initializedProject(t)
	if err := os.WriteFile(filepath.Join(project, ".vexillum", "models.json"), []byte(`{"profiles": {"pi-scout": {"model": "anthropic/claude-haiku-4-5", "effort": "low", "when": "x", "harness": "pi"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	code := runDoctor(project, t.TempDir(), t.TempDir(), &out)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "[warn] Pi - a model profile uses harness pi but pi is not on PATH") {
		t.Errorf("expected the Pi warning, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Environment ready") || !strings.Contains(out.String(), ", Pi,") {
		t.Errorf("expected a ready summary naming the warning, got:\n%s", out.String())
	}
}

// With pi and herdr's integration for it in place, every Pi line is ok.
func TestDoctor_PiPresentReportsVersionAndIntegration(t *testing.T) {
	dir := fakeBinDir(t, "claude")
	writeStub(t, dir, "pi", `echo 1.3.0`)
	writeStub(t, dir, "herdr", `case "$1" in --version) echo "herdr 0.9.5";; integration) echo "pi: installed (v9) (/x)";; esac`)
	t.Setenv("PATH", dir)

	var out bytes.Buffer
	code := runDoctor(initializedProject(t), t.TempDir(), t.TempDir(), &out)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
	for _, want := range []string{"[ok] Pi\n", "[ok] Pi version - 1.3.0", "[ok] herdr integration for pi - installed (v9)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
}

// Without the herdr integration doctor says how to install it, as a warning.
func TestDoctor_PiWithoutHerdrIntegrationWarns(t *testing.T) {
	dir := fakeBinDir(t, "claude")
	writeStub(t, dir, "pi", `echo 1.3.0`)
	writeStub(t, dir, "herdr", `case "$1" in --version) echo "herdr 0.9.5";; integration) echo "pi: not installed (/x)";; esac`)
	t.Setenv("PATH", dir)

	var out bytes.Buffer
	code := runDoctor(initializedProject(t), t.TempDir(), t.TempDir(), &out)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "[warn] herdr integration for pi - not installed") || !strings.Contains(out.String(), "herdr integration install pi") {
		t.Errorf("expected the install hint, got:\n%s", out.String())
	}
}
