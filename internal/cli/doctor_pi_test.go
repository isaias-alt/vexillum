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

// The checks that start processes run side by side (see doctorcheck.RunAll),
// and the report still reads in the order of the checks, the same on every
// run, with a single `herdr integration status` for Pi and OpenCode together.
func TestDoctor_ReportOrderIsStableAndHerdrIntegrationIsReadOnce(t *testing.T) {
	statusCalls := filepath.Join(t.TempDir(), "status-calls")
	dir := fakeBinDir(t, "claude")
	writeStub(t, dir, "pi", `echo 1.3.0`)
	writeStub(t, dir, "opencode", `echo 2.0.32`)
	writeStub(t, dir, "herdr", `case "$1" in
--version) echo "herdr 0.9.5";;
integration) echo x >> '`+statusCalls+`'; printf '%s\n' "pi: installed (v9) (/p)" "opencode: installed (v9) (/o)";;
esac`)
	t.Setenv("PATH", dir)

	project := initializedProject(t)
	vexillumHome, home := t.TempDir(), t.TempDir()
	var first bytes.Buffer
	if code := runDoctor(project, vexillumHome, home, &first); code != 0 {
		t.Fatalf("exit %d\n%s", code, first.String())
	}

	// The lines of the report, in the order the checks are listed in runDoctor.
	want := []string{
		"[ok] Claude Code", "[ok] herdr", "[ok] herdr version - 0.9.5", "[missing] GitHub CLI (gh)",
		"[ok] another vx earlier in PATH", "[ok] ~/.vexillum/", "[ok] project initialized", "[ok] git repository",
		"[ok] Pi\n", "[ok] Pi version - 1.3.0", "[ok] herdr integration for pi - installed (v9)",
		"[ok] OpenCode\n", "[ok] OpenCode version - 2.0.32 (V2)", "[ok] herdr integration for opencode - installed (v9)",
		"] CLAUDE.md vexillum block", "] skill vexillum", "[ok] models.json", "[ok] yolo mode", "[warn] sentinel Stop hook", "[ok] sentinel - ",
	}
	report := first.String()
	at := 0
	for _, line := range want {
		i := strings.Index(report[at:], line)
		if i < 0 {
			t.Fatalf("%q is missing, or out of order, in:\n%s", line, report)
		}
		at += i + len(line)
	}
	if b, _ := os.ReadFile(statusCalls); strings.Count(string(b), "x") != 1 {
		t.Errorf("herdr integration status ran %d times, want 1", strings.Count(string(b), "x"))
	}

	for range 1 {
		var again bytes.Buffer
		runDoctor(project, vexillumHome, home, &again)
		if again.String() != report {
			t.Fatalf("a later run differs:\n--- first\n%s\n--- later\n%s", report, again.String())
		}
	}
}
