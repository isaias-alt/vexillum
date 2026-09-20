package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// vexillum upgrade refuses to run against a project that was never
// initialized, pointing at 'vexillum init' instead.
func TestUpgrade_RefusesUninitializedProject(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var stdout, stderr bytes.Buffer
	code := runUpgrade(projectDir, vexillumHome, &stdout, &stderr)

	if code == 0 {
		t.Fatal("expected non-zero exit code for an uninitialized project")
	}
	if stderr.String() == "" {
		t.Error("expected an error message on stderr")
	}
}

// A project initialized with the current binary and never touched by hand
// is refreshed to the latest template on upgrade (verifies the mechanism
// end to end, even though today's template equals what init just wrote).
func TestUpgrade_RefreshesUntouchedScaffold(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var buf bytes.Buffer
	if code := runInit(projectDir, vexillumHome, &buf, &buf); code != 0 {
		t.Fatalf("init failed: exit %d: %s", code, buf.String())
	}

	var stdout, stderr bytes.Buffer
	code := runUpgrade(projectDir, vexillumHome, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}

	agents, err := os.ReadFile(filepath.Join(projectDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("reading AGENTS.md: %v", err)
	}
	if string(agents) != productAgentsMD {
		t.Error("expected AGENTS.md to match the latest template after upgrade")
	}
}

// upgrade never overwrites AGENTS.md/CLAUDE.md that were hand-edited after
// vexillum wrote them - the whole point of the command is to be safe to run
// blindly.
func TestUpgrade_LeavesHandEditedFilesUntouched(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var buf bytes.Buffer
	if code := runInit(projectDir, vexillumHome, &buf, &buf); code != 0 {
		t.Fatalf("init failed: exit %d: %s", code, buf.String())
	}

	agentsPath := filepath.Join(projectDir, "AGENTS.md")
	customContent := []byte("# My custom commander instructions\n")
	if err := os.WriteFile(agentsPath, customContent, 0o644); err != nil {
		t.Fatalf("writing custom AGENTS.md: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runUpgrade(projectDir, vexillumHome, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}

	got, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("reading AGENTS.md: %v", err)
	}
	if !bytes.Equal(got, customContent) {
		t.Errorf("AGENTS.md was overwritten: got %q, want %q", got, customContent)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("left untouched")) {
		t.Errorf("expected output to report AGENTS.md as left untouched, got: %s", stdout.String())
	}
}

// A project initialized before hash-tracking existed (no stored hash in
// config.json) whose AGENTS.md content no longer matches the latest
// template is ambiguous - it could be stock content from an older vexillum
// version, or a hand edit. upgrade treats that ambiguity conservatively: it
// does not overwrite, and says so.
func TestUpgrade_TreatsUnknownProvenanceAsCustomized(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var buf bytes.Buffer
	if code := runInit(projectDir, vexillumHome, &buf, &buf); code != 0 {
		t.Fatalf("init failed: exit %d: %s", code, buf.String())
	}

	// Simulate a pre-hash-tracking project: strip the stored hashes, and
	// make the on-disk content diverge from today's template.
	if err := os.WriteFile(filepath.Join(projectDir, "AGENTS.md"), []byte("# stale pre-hash content\n"), 0o644); err != nil {
		t.Fatalf("writing stale AGENTS.md: %v", err)
	}
	cfgPath := filepath.Join(projectDir, ".vexillum", "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"version":1,"initialized_at":"2026-01-01T00:00:00Z"}`), 0o644); err != nil {
		t.Fatalf("stripping stored hash: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runUpgrade(projectDir, vexillumHome, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}

	got, err := os.ReadFile(filepath.Join(projectDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("reading AGENTS.md: %v", err)
	}
	if string(got) != "# stale pre-hash content\n" {
		t.Errorf("expected AGENTS.md to be left untouched without a stored hash, got: %s", got)
	}
}

// upgrade recreates a scaffold file that was deleted entirely.
func TestUpgrade_RecreatesMissingFile(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var buf bytes.Buffer
	if code := runInit(projectDir, vexillumHome, &buf, &buf); code != 0 {
		t.Fatalf("init failed: exit %d: %s", code, buf.String())
	}

	claudePath := filepath.Join(projectDir, "CLAUDE.md")
	if err := os.Remove(claudePath); err != nil {
		t.Fatalf("removing CLAUDE.md: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runUpgrade(projectDir, vexillumHome, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	mustStat(t, claudePath)
}

// upgrade is safe to run repeatedly: a second run makes no further changes
// once everything is already current.
func TestUpgrade_IdempotentOnSecondRun(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var buf bytes.Buffer
	if code := runInit(projectDir, vexillumHome, &buf, &buf); code != 0 {
		t.Fatalf("init failed: exit %d: %s", code, buf.String())
	}
	if code := runUpgrade(projectDir, vexillumHome, &buf, &buf); code != 0 {
		t.Fatalf("first upgrade failed: exit %d: %s", code, buf.String())
	}

	agentsBefore, err := os.ReadFile(filepath.Join(projectDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("reading AGENTS.md: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runUpgrade(projectDir, vexillumHome, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("already up to date")) {
		t.Errorf("expected output to report AGENTS.md as already up to date, got: %s", stdout.String())
	}

	agentsAfter, err := os.ReadFile(filepath.Join(projectDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("reading AGENTS.md after second upgrade: %v", err)
	}
	if !bytes.Equal(agentsBefore, agentsAfter) {
		t.Error("AGENTS.md changed on a second upgrade run with nothing to do")
	}
}

// upgrade adds the sentinel Stop hook to a project initialized before that
// hook existed, same as init's own healing path.
func TestUpgrade_AddsMissingSentinelHook(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var buf bytes.Buffer
	if code := runInit(projectDir, vexillumHome, &buf, &buf); code != 0 {
		t.Fatalf("init failed: exit %d: %s", code, buf.String())
	}

	settingsPath := filepath.Join(projectDir, ".claude", "settings.json")
	if err := os.Remove(settingsPath); err != nil {
		t.Fatalf("removing settings.json to simulate a pre-hook project: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runUpgrade(projectDir, vexillumHome, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("reading settings.json: %v", err)
	}
	if !bytes.Contains(data, []byte(sentinelHookCommand)) {
		t.Errorf("expected settings.json to contain the sentinel hook, got: %s", data)
	}
}
