package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/scaffold"
)

// A setup with no OpenCode prints no OpenCode line.
func TestDoctor_NoOpenCodeLinesWithoutOpenCode(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr"))
	var out bytes.Buffer
	if code := runDoctor(initializedProject(t), t.TempDir(), t.TempDir(), &out); code != 0 {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
	if strings.Contains(out.String(), "OpenCode") || strings.Contains(out.String(), "opencode") {
		t.Errorf("a setup with no opencode printed an OpenCode line:\n%s", out.String())
	}
}

// An opencode profile on a machine without opencode warns, and the Claude
// environment stays ready.
func TestDoctor_OpenCodeProfileWithoutOpenCodeWarnsButStaysReady(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr"))
	project := initializedProject(t)
	if err := os.WriteFile(filepath.Join(project, ".vexillum", "models.json"), []byte(`{"profiles": {"oc-scout": {"model": "opencode/glm-5.3-flash-free", "effort": "low", "when": "x", "harness": "opencode"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := runDoctor(project, t.TempDir(), t.TempDir(), &out); code != 0 {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "[warn] OpenCode - a model profile uses harness opencode but opencode is not on PATH") {
		t.Errorf("expected the OpenCode warning, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Environment ready") {
		t.Errorf("expected a ready summary, got:\n%s", out.String())
	}
}

// With opencode 2.x and herdr's integration current, every line is ok.
func TestDoctor_OpenCodePresentReportsVersionAndIntegration(t *testing.T) {
	dir := fakeBinDir(t, "claude")
	writeStub(t, dir, "opencode", `echo "opencode v2.0.32"`)
	writeStub(t, dir, "herdr", `case "$1" in --version) echo "herdr 0.9.5";; integration) echo "opencode: current (v13) (/x)";; esac`)
	t.Setenv("PATH", dir)

	var out bytes.Buffer
	if code := runDoctor(initializedProject(t), t.TempDir(), t.TempDir(), &out); code != 0 {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
	for _, want := range []string{"[ok] OpenCode\n", "[ok] OpenCode version - 2.0.32 (V2)", "[ok] herdr integration for opencode - current (v13)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
}

// An opencode commander also gets the wake-path lines: not registered yet is
// an optional miss (it registers itself on first use), a stopped service is
// fine while no commander is open, and doctor never starts the service.
func TestDoctor_OpenCodeCommanderReportsTheWakePath(t *testing.T) {
	dir := fakeBinDir(t, "claude")
	log := filepath.Join(t.TempDir(), "opencode-calls")
	writeStub(t, dir, "opencode", `printf '%s\n' "$*" >> '`+log+`'
case "$1 $2" in
  "service status") echo stopped;;
  *) echo "opencode v2.0.32";;
esac`)
	writeStub(t, dir, "herdr", `case "$1" in --version) echo "herdr 0.9.5";; integration) echo "opencode: current (v13) (/x)";; esac`)
	t.Setenv("PATH", dir)

	project := initializedProject(t)
	cfg, err := scaffold.ReadConfig(filepath.Join(project, ".vexillum"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Commander = []string{"opencode"}
	if err := scaffold.SaveConfig(filepath.Join(project, ".vexillum"), cfg); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if code := runDoctor(project, t.TempDir(), t.TempDir(), &out); code != 0 {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
	for _, want := range []string{
		"[ok] OpenCode\n",
		"[missing] OpenCode commander registered - no OpenCode commander has registered in this project yet",
		"[ok] OpenCode wake API - the OpenCode background service is not running in this environment",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
	if data, _ := os.ReadFile(log); strings.Contains(string(data), "api ") {
		t.Errorf("doctor ran opencode api:\n%s", data)
	}
}

// A project whose commander is only Claude gets none of those lines, even with
// opencode installed.
func TestDoctor_NoWakePathLinesForAClaudeCommander(t *testing.T) {
	dir := fakeBinDir(t, "claude", "herdr")
	writeStub(t, dir, "opencode", `echo "opencode v2.0.32"`)
	t.Setenv("PATH", dir)
	var out bytes.Buffer
	runDoctor(initializedProject(t), t.TempDir(), t.TempDir(), &out)
	for _, unwanted := range []string{"OpenCode commander registered", "OpenCode wake API", "OpenCode reads the vexillum block"} {
		if strings.Contains(out.String(), unwanted) {
			t.Errorf("a claude commander got %q:\n%s", unwanted, out.String())
		}
	}
}
