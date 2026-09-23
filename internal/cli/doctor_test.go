package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// fakeBinDir creates a directory containing an executable stub for each
// name given, so tests can control exactly which tools "exist" on PATH
// without depending on what's actually installed on the machine running
// the tests. It always links in the real git binary, since doctor's own
// git-repo check (and the initGitRepo test helper) need a working git
// regardless of which tools a given test is exercising.
func fakeBinDir(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()

	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("git not found on PATH: %v", err)
	}
	if err := os.Symlink(realGit, filepath.Join(dir, "git")); err != nil {
		t.Fatalf("linking real git: %v", err)
	}

	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("writing fake binary %s: %v", name, err)
		}
	}
	return dir
}

// writeStub (over)writes an executable stub named name in dir, running
// script as its body - for tests that need a fake binary to produce
// specific output (e.g. "herdr --version"), not just exit 0.
func writeStub(t *testing.T, dir, name, script string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatalf("writing stub %s: %v", name, err)
	}
}

// writeSkillFile creates <dir>/.claude/skills/<name>/SKILL.md - the real
// on-disk layout "npx skills add" leaves behind (verified against
// vercel-labs/skills, the CLI quota-axi's own README recommends), so
// tests can simulate an installed AXI skill without running that CLI.
func writeSkillFile(t *testing.T, dir, name string) {
	t.Helper()
	skillDir := filepath.Join(dir, ".claude", "skills", name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("creating skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: "+name+"\n---\n"), 0o644); err != nil {
		t.Fatalf("writing SKILL.md: %v", err)
	}
}

// initializedProject returns a project dir that is a git repo with the
// vexillum scaffold already in place (as vexillum init would leave it).
func initializedProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	initGitRepo(t, dir)
	if err := writeLocalConfig(filepath.Join(dir, ".vexillum")); err != nil {
		t.Fatalf("writing local config: %v", err)
	}
	return dir
}

// L1-07: full environment, everything ok, exit 0.
func TestDoctor_FullEnvironment(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	code := runDoctor(projectDir, vexillumHome, homeDir, &out)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput:\n%s", code, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("Environment ready")) {
		t.Errorf("expected a ready summary, got:\n%s", out.String())
	}
}

// L1-08: Claude Code missing, rest ok, exit != 0.
func TestDoctor_MissingClaudeCode(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "herdr", "tmux"))
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	code := runDoctor(projectDir, vexillumHome, homeDir, &out)

	if code == 0 {
		t.Fatalf("expected non-zero exit, got 0\noutput:\n%s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("[missing] Claude Code")) {
		t.Errorf("expected Claude Code to be reported missing, got:\n%s", out.String())
	}
}

// L1-09: herdr missing, rest ok, exit != 0.
func TestDoctor_MissingHerdr(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "tmux"))
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	code := runDoctor(projectDir, vexillumHome, homeDir, &out)

	if code == 0 {
		t.Fatalf("expected non-zero exit, got 0\noutput:\n%s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("[missing] herdr")) {
		t.Errorf("expected herdr to be reported missing, got:\n%s", out.String())
	}
}

// L1-10: tmux missing, rest ok. Policy: tmux is the control backend, not
// primary, so this is a warning and exit stays 0.
func TestDoctor_MissingTmuxIsWarningOnly(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr"))
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	code := runDoctor(projectDir, vexillumHome, homeDir, &out)

	if code != 0 {
		t.Fatalf("expected exit 0 (tmux optional), got %d\noutput:\n%s", code, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("[missing] tmux")) {
		t.Errorf("expected tmux to be reported missing, got:\n%s", out.String())
	}
}

// L1-11: project not initialized, binaries present, exit != 0, suggests
// running init.
func TestDoctor_ProjectNotInitialized(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	code := runDoctor(projectDir, vexillumHome, homeDir, &out)

	if code == 0 {
		t.Fatalf("expected non-zero exit, got 0\noutput:\n%s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("vexillum init")) {
		t.Errorf("expected output to suggest running 'vexillum init', got:\n%s", out.String())
	}
}

// L1-12: doctor never modifies anything on disk.
func TestDoctor_ReadOnly(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	before, err := snapshotTree(t, projectDir)
	if err != nil {
		t.Fatalf("snapshotting project dir: %v", err)
	}
	homeBefore, err := snapshotTree(t, vexillumHome)
	if err != nil {
		t.Fatalf("snapshotting vexillum home: %v", err)
	}
	realHomeBefore, err := snapshotTree(t, homeDir)
	if err != nil {
		t.Fatalf("snapshotting home dir: %v", err)
	}

	var out bytes.Buffer
	runDoctor(projectDir, vexillumHome, homeDir, &out)

	after, err := snapshotTree(t, projectDir)
	if err != nil {
		t.Fatalf("snapshotting project dir after: %v", err)
	}
	homeAfter, err := snapshotTree(t, vexillumHome)
	if err != nil {
		t.Fatalf("snapshotting vexillum home after: %v", err)
	}
	realHomeAfter, err := snapshotTree(t, homeDir)
	if err != nil {
		t.Fatalf("snapshotting home dir after: %v", err)
	}

	if before != after {
		t.Errorf("project dir changed:\nbefore: %s\nafter:  %s", before, after)
	}
	if homeBefore != homeAfter {
		t.Errorf("vexillum home changed:\nbefore: %s\nafter:  %s", homeBefore, homeAfter)
	}
	if realHomeBefore != realHomeAfter {
		t.Errorf("home dir changed:\nbefore: %s\nafter:  %s", realHomeBefore, realHomeAfter)
	}
}

// L1-13: outside a git repo, doctor's git check reports it, exit code
// consistent with the policy chosen for init (L1-05): required, non-zero.
func TestDoctor_OutsideGitRepo(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := t.TempDir()
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	code := runDoctor(projectDir, vexillumHome, homeDir, &out)

	if code == 0 {
		t.Fatalf("expected non-zero exit outside a git repository, got 0\noutput:\n%s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("[missing] git repository")) {
		t.Errorf("expected git repository check to be reported missing, got:\n%s", out.String())
	}
}

// A1-01: herdr reports a supported 0.9.x version - ok line names it, exit 0.
func TestDoctor_HerdrVersionSupported(t *testing.T) {
	dir := fakeBinDir(t, "claude", "tmux")
	writeStub(t, dir, "herdr", "echo 'herdr 0.9.0'")
	t.Setenv("PATH", dir)
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	code := runDoctor(projectDir, vexillumHome, homeDir, &out)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput:\n%s", code, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("[ok] herdr version - 0.9.0")) {
		t.Errorf("expected the detected version reported ok, got:\n%s", out.String())
	}
}

// A1-02: herdr reports a version outside 0.9.x - warning, not a failure:
// exit stays 0.
func TestDoctor_HerdrVersionUnsupportedIsWarningOnly(t *testing.T) {
	dir := fakeBinDir(t, "claude", "tmux")
	writeStub(t, dir, "herdr", "echo 'herdr 0.10.0'")
	t.Setenv("PATH", dir)
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	code := runDoctor(projectDir, vexillumHome, homeDir, &out)

	if code != 0 {
		t.Fatalf("expected exit 0 (warning only), got %d\noutput:\n%s", code, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("[warn] herdr version - 0.10.0")) {
		t.Errorf("expected the unsupported version reported as a warning, got:\n%s", out.String())
	}
}

// A1-03: herdr --version fails or produces unparseable output - warning,
// not a failure.
func TestDoctor_HerdrVersionUndetectableIsWarningOnly(t *testing.T) {
	dir := fakeBinDir(t, "claude", "tmux")
	writeStub(t, dir, "herdr", "exit 1")
	t.Setenv("PATH", dir)
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	code := runDoctor(projectDir, vexillumHome, homeDir, &out)

	if code != 0 {
		t.Fatalf("expected exit 0 (warning only), got %d\noutput:\n%s", code, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("[warn] herdr version - could not determine herdr version")) {
		t.Errorf("expected an undetectable version reported as a warning, got:\n%s", out.String())
	}
}

// A1-04: herdr missing entirely - only the existing "[missing] herdr" line
// from checkBinary, no separate "herdr version" line.
func TestDoctor_HerdrMissingHasNoSeparateVersionLine(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "tmux"))
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	runDoctor(projectDir, vexillumHome, homeDir, &out)

	if bytes.Contains(out.Bytes(), []byte("herdr version")) {
		t.Errorf("expected no separate herdr version line when herdr is missing, got:\n%s", out.String())
	}
}

// B1-01: quota-axi installed globally (homeDir/.claude/skills/) is reported
// installed.
func TestDoctor_AxiInstalledGlobally(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()
	writeSkillFile(t, homeDir, "quota-axi")

	var out bytes.Buffer
	runDoctor(projectDir, vexillumHome, homeDir, &out)

	if !bytes.Contains(out.Bytes(), []byte("[installed] quota-axi")) {
		t.Errorf("expected quota-axi reported installed, got:\n%s", out.String())
	}
}

// B1-02: quota-axi installed at project level only (no global install) is
// also reported installed - either location is enough.
func TestDoctor_AxiInstalledAtProjectLevel(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	writeSkillFile(t, projectDir, "quota-axi")
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	runDoctor(projectDir, vexillumHome, homeDir, &out)

	if !bytes.Contains(out.Bytes(), []byte("[installed] quota-axi")) {
		t.Errorf("expected quota-axi reported installed, got:\n%s", out.String())
	}
}

// B1-03: quota-axi not installed anywhere - reported with the exact
// install command from its own README.
func TestDoctor_AxiNotInstalled(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	runDoctor(projectDir, vexillumHome, homeDir, &out)

	want := "[not installed] quota-axi - install with: npx skills add kunchenguid/quota-axi --skill quota-axi -g"
	if !bytes.Contains(out.Bytes(), []byte(want)) {
		t.Errorf("expected exact install hint, got:\n%s", out.String())
	}
}

// B1-04: AXI status never affects the exit code or the ready summary -
// same otherwise-healthy environment, with and without the skill
// installed, both exit 0 with "Environment ready.".
func TestDoctor_AxiStatusNeverAffectsExitCode(t *testing.T) {
	for _, installed := range []bool{true, false} {
		t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
		projectDir := initializedProject(t)
		vexillumHome := t.TempDir()
		homeDir := t.TempDir()
		if installed {
			writeSkillFile(t, homeDir, "quota-axi")
		}

		var out bytes.Buffer
		code := runDoctor(projectDir, vexillumHome, homeDir, &out)

		if code != 0 {
			t.Fatalf("installed=%v: expected exit 0, got %d\noutput:\n%s", installed, code, out.String())
		}
		if !bytes.Contains(out.Bytes(), []byte("Environment ready")) {
			t.Errorf("installed=%v: expected a ready summary unaffected by AXI status, got:\n%s", installed, out.String())
		}
	}
}

// B2-01: lavish-axi installed - its skill folder name is "lavish", not
// "lavish-axi" (verified against its own README), so the on-disk path and
// the reported name both use "lavish".
func TestDoctor_LavishInstalled(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	writeSkillFile(t, projectDir, "lavish")
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	runDoctor(projectDir, vexillumHome, homeDir, &out)

	if !bytes.Contains(out.Bytes(), []byte("[installed] lavish")) {
		t.Errorf("expected lavish reported installed, got:\n%s", out.String())
	}
}

// B2-02: lavish-axi not installed - its own README recommends installing
// project-local, no "-g", unlike quota-axi's own recommended command.
func TestDoctor_LavishNotInstalled(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	runDoctor(projectDir, vexillumHome, homeDir, &out)

	want := "[not installed] lavish - install with: npx skills add kunchenguid/lavish-axi --skill lavish"
	if !bytes.Contains(out.Bytes(), []byte(want)) {
		t.Errorf("expected exact install hint, got:\n%s", out.String())
	}
	if bytes.Contains(out.Bytes(), []byte("lavish -g")) {
		t.Errorf("expected no -g flag for lavish's install hint (its README doesn't recommend one), got:\n%s", out.String())
	}
}

// B3-01: chrome-devtools-axi installed globally.
func TestDoctor_ChromeDevtoolsInstalled(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()
	writeSkillFile(t, homeDir, "chrome-devtools-axi")

	var out bytes.Buffer
	runDoctor(projectDir, vexillumHome, homeDir, &out)

	if !bytes.Contains(out.Bytes(), []byte("[installed] chrome-devtools-axi")) {
		t.Errorf("expected chrome-devtools-axi reported installed, got:\n%s", out.String())
	}
}

// B3-02: chrome-devtools-axi not installed - same recommended shape as
// quota-axi (--skill matches the repo name, with -g).
func TestDoctor_ChromeDevtoolsNotInstalled(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	runDoctor(projectDir, vexillumHome, homeDir, &out)

	want := "[not installed] chrome-devtools-axi - install with: npx skills add kunchenguid/chrome-devtools-axi --skill chrome-devtools-axi -g"
	if !bytes.Contains(out.Bytes(), []byte(want)) {
		t.Errorf("expected exact install hint, got:\n%s", out.String())
	}
}

// addGitRemote adds a git remote to dir - used to simulate a project that
// already ran "no-mistakes init" (which creates a "no-mistakes" remote)
// without needing the real no-mistakes binary.
func addGitRemote(t *testing.T, dir, name, url string) {
	t.Helper()
	cmd := exec.Command("git", "remote", "add", name, url)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
}

// B4-01: no-mistakes not installed at all.
func TestDoctor_NoMistakesNotInstalled(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	runDoctor(projectDir, vexillumHome, homeDir, &out)

	if !bytes.Contains(out.Bytes(), []byte("[missing] no-mistakes - not found in PATH")) {
		t.Errorf("expected no-mistakes reported not found, got:\n%s", out.String())
	}
}

// B4-02: no-mistakes installed, but this project never ran "no-mistakes
// init" (no "no-mistakes" git remote yet).
func TestDoctor_NoMistakesInstalledButNotGated(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux", "no-mistakes"))
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	runDoctor(projectDir, vexillumHome, homeDir, &out)

	if !bytes.Contains(out.Bytes(), []byte("[missing] no-mistakes - installed, but this project hasn't run 'no-mistakes init'")) {
		t.Errorf("expected no-mistakes reported installed-but-not-gated, got:\n%s", out.String())
	}
}

// B4-03: no-mistakes installed and this project is gated (has the
// "no-mistakes" remote) - ok, and never affects the exit code either way.
func TestDoctor_NoMistakesGated(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux", "no-mistakes"))
	projectDir := initializedProject(t)
	addGitRemote(t, projectDir, "no-mistakes", "/tmp/fake-no-mistakes-gate.git")
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	code := runDoctor(projectDir, vexillumHome, homeDir, &out)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput:\n%s", code, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("[ok] no-mistakes - installed and this project is gated")) {
		t.Errorf("expected no-mistakes reported gated and ok, got:\n%s", out.String())
	}
}

// gh not installed is reported, informational only - it never affects
// the exit code, same posture as no-mistakes above.
func TestDoctor_GitHubCLINotInstalled(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	code := runDoctor(projectDir, vexillumHome, homeDir, &out)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput:\n%s", code, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("[missing] GitHub CLI (gh) - not found in PATH")) {
		t.Errorf("expected gh reported not found, got:\n%s", out.String())
	}
}

// gh installed reports ok.
func TestDoctor_GitHubCLIInstalled(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux", "gh"))
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	code := runDoctor(projectDir, vexillumHome, homeDir, &out)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput:\n%s", code, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("[ok] GitHub CLI (gh)")) {
		t.Errorf("expected gh reported ok, got:\n%s", out.String())
	}
}

// snapshotTree returns a string describing every entry under dir (relative
// path, size, mode, mod time), so two snapshots can be compared for any
// change doctor might have made.
func snapshotTree(t *testing.T, dir string) (string, error) {
	t.Helper()
	var b bytes.Buffer
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		b.WriteString(fmt.Sprintf("%s|%d|%s|%d\n", rel, info.Size(), info.Mode(), info.ModTime().UnixNano()))
		return nil
	})
	return b.String(), err
}
