package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/doctorcheck"
	"github.com/isaias-alt/vexillum/internal/install"
	"github.com/isaias-alt/vexillum/internal/scaffold"
	"github.com/isaias-alt/vexillum/internal/slot"
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
// vercel-labs/skills, the CLI quota-tool's own README recommends), so
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
// vexillum scaffold already in place (as vx init would leave it).
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
	if !bytes.Contains(out.Bytes(), []byte("vx init")) {
		t.Errorf("expected output to suggest running 'vx init', got:\n%s", out.String())
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

// B1-01: quota-tool installed globally (homeDir/.claude/skills/) is reported
// installed.
func TestDoctor_AxiInstalledGlobally(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()
	writeSkillFile(t, homeDir, "quota-tool")

	var out bytes.Buffer
	runDoctor(projectDir, vexillumHome, homeDir, &out)

	if !bytes.Contains(out.Bytes(), []byte("[installed] quota-tool")) {
		t.Errorf("expected quota-tool reported installed, got:\n%s", out.String())
	}
}

// B1-02: quota-tool installed at project level only (no global install) is
// also reported installed - either location is enough.
func TestDoctor_AxiInstalledAtProjectLevel(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	writeSkillFile(t, projectDir, "quota-tool")
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	runDoctor(projectDir, vexillumHome, homeDir, &out)

	if !bytes.Contains(out.Bytes(), []byte("[installed] quota-tool")) {
		t.Errorf("expected quota-tool reported installed, got:\n%s", out.String())
	}
}

// B1-03: quota-tool not installed anywhere - reported with the exact
// install command from its own README.
func TestDoctor_AxiNotInstalled(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	runDoctor(projectDir, vexillumHome, homeDir, &out)

	want := "[not installed] quota-tool - install with: npx skills add upstream --skill quota-tool -g"
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
			writeSkillFile(t, homeDir, "quota-tool")
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

// forum-tool was replaced by `vx forum`: doctor must no longer report it.
func TestDoctor_ForumNotListed(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	writeSkillFile(t, projectDir, "forum")
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	runDoctor(projectDir, vexillumHome, homeDir, &out)

	if bytes.Contains(out.Bytes(), []byte("forum")) {
		t.Errorf("doctor must not mention forum any more, got:\n%s", out.String())
	}
}

// B3-01: chrome-devtools-tool installed globally.
func TestDoctor_ChromeDevtoolsInstalled(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()
	writeSkillFile(t, homeDir, "chrome-devtools-tool")

	var out bytes.Buffer
	runDoctor(projectDir, vexillumHome, homeDir, &out)

	if !bytes.Contains(out.Bytes(), []byte("[installed] chrome-devtools-tool")) {
		t.Errorf("expected chrome-devtools-tool reported installed, got:\n%s", out.String())
	}
}

// B3-02: chrome-devtools-tool not installed - same recommended shape as
// quota-tool (--skill matches the repo name, with -g).
func TestDoctor_ChromeDevtoolsNotInstalled(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()
	homeDir := t.TempDir()

	var out bytes.Buffer
	runDoctor(projectDir, vexillumHome, homeDir, &out)

	want := "[not installed] chrome-devtools-tool - install with: npx skills add upstream --skill chrome-devtools-tool -g"
	if !bytes.Contains(out.Bytes(), []byte(want)) {
		t.Errorf("expected exact install hint, got:\n%s", out.String())
	}
}

// gh not installed is reported, informational only - it never affects
// the exit code.
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

// The first-party skills ship in the binary, so doctor reports their state
// against the embedded copy and no longer hints at "npx skills add" for them.
func TestDoctor_FirstPartySkills(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir, home := newProject(t)
	vexillumHome := t.TempDir()
	if r := doInit(projectDir, home, setupOptions{Yes: true}, "", false); r.code != 0 {
		t.Fatal(r.out, r.errOut)
	}
	homeDir := t.TempDir()

	run := func() string {
		var out bytes.Buffer
		if code := runDoctor(projectDir, vexillumHome, homeDir, &out); code != 0 {
			t.Fatalf("exit %d\n%s", code, out.String())
		}
		return out.String()
	}
	out := run()
	for _, want := range []string{
		"[ok] AGENTS.md vexillum block - current (en)",
		"[ok] CLAUDE.md imports AGENTS.md",
		"[ok] skill vexillum - installed, current",
		"[ok] skill forum - installed, current",
		"[ok] skill muster - installed, current",
		"[ok] models.json - valid, 5 profiles",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "npx skills add isaias-alt") {
		t.Errorf("stale install hint for an embedded skill:\n%s", out)
	}
	if !strings.Contains(out, "npx skills add upstream") {
		t.Errorf("third-party hints must stay:\n%s", out)
	}

	skill := filepath.Join(projectDir, ".claude", "skills", "forum", "SKILL.md")
	writeFileT(t, skill, readFile(t, skill)+"\nmine\n")
	if out := run(); !strings.Contains(out, "[warn] skill forum - installed, edited by hand") {
		t.Errorf("edited skill not reported:\n%s", out)
	}
	os.RemoveAll(filepath.Join(projectDir, ".claude", "skills", "muster"))
	if out := run(); !strings.Contains(out, "[missing] skill muster - missing") {
		t.Errorf("missing skill not reported:\n%s", out)
	}
	// A skill installed outside the project does not count.
	writeFileT(t, filepath.Join(homeDir, ".claude", "skills", "muster", "SKILL.md"), "x")
	if out := run(); !strings.Contains(out, "[missing] skill muster - missing") {
		t.Errorf("a skill in the home directory must not satisfy the project:\n%s", out)
	}
}

func TestDoctor_SlotStates(t *testing.T) {
	core, _ := install.SlotTemplate(slot.LangEN)
	current, _ := slot.Upsert(englishAgents, core, false)
	stale, _ := slot.Upsert(englishAgents, "## Vexillum commander\n\nOld.\n", false)
	tests := []struct {
		name, agents, want string
	}{
		{"absent", englishAgents, "[missing] AGENTS.md vexillum block - absent"},
		{"no file", "", "[missing] CLAUDE.md vexillum block - absent"},
		{"current", current, "[ok] AGENTS.md vexillum block - current (en)"},
		{"stale", stale, "[warn] AGENTS.md vexillum block - stale"},
		{"drifted", strings.Replace(current, "## Vexillum commander", "## Mine", 1), "[warn] AGENTS.md vexillum block - drifted"},
		{"malformed", englishAgents + "<!-- BEGIN VEXILLUM v:1 hash:00000000 -->\n", "[warn] AGENTS.md vexillum block - malformed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
			projectDir := initializedProject(t)
			if tc.agents != "" {
				writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), tc.agents)
			}
			var out bytes.Buffer
			if code := runDoctor(projectDir, t.TempDir(), t.TempDir(), &out); code != 0 {
				t.Fatalf("slot state must never fail doctor: exit %d\n%s", code, out.String())
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Errorf("missing %q in:\n%s", tc.want, out.String())
			}
		})
	}
}

// The block can live in CLAUDE.md when the project has no AGENTS.md: doctor
// reports it there and has no import to check.
func TestDoctor_ClaudeMDSlot(t *testing.T) {
	core, _ := install.SlotTemplate(slot.LangEN)
	current, _ := slot.Upsert(englishAgents, core, false)
	stale, _ := slot.Upsert(englishAgents, "## Vexillum commander\n\nOld.\n", false)
	tests := []struct {
		name, claude, want string
	}{
		{"current", current, "[ok] CLAUDE.md vexillum block - current (en)"},
		{"stale", stale, "[warn] CLAUDE.md vexillum block - stale"},
		{"drifted", strings.Replace(current, "## Vexillum commander", "## Mine", 1), "[warn] CLAUDE.md vexillum block - drifted"},
		{"malformed", englishAgents + "<!-- BEGIN VEXILLUM v:1 hash:00000000 -->\n", "[warn] CLAUDE.md vexillum block - malformed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
			projectDir := initializedProject(t)
			writeFileT(t, filepath.Join(projectDir, "CLAUDE.md"), tc.claude)
			var out bytes.Buffer
			if code := runDoctor(projectDir, t.TempDir(), t.TempDir(), &out); code != 0 {
				t.Fatalf("slot state must never fail doctor: exit %d\n%s", code, out.String())
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Errorf("missing %q in:\n%s", tc.want, out.String())
			}
			if strings.Contains(out.String(), "imports AGENTS.md") || strings.Contains(out.String(), "AGENTS.md vexillum block") {
				t.Errorf("a block in CLAUDE.md has no AGENTS.md checks:\n%s", out.String())
			}
		})
	}

	t.Run("after init with the default", func(t *testing.T) {
		t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
		projectDir, home := newProject(t)
		if r := doInit(projectDir, home, setupOptions{Yes: true}, "", false); r.code != 0 {
			t.Fatal(r.out, r.errOut)
		}
		var out bytes.Buffer
		runDoctor(projectDir, t.TempDir(), t.TempDir(), &out)
		if !strings.Contains(out.String(), "[ok] CLAUDE.md vexillum block - current (en)") || strings.Contains(out.String(), "imports AGENTS.md") {
			t.Errorf("doctor after init:\n%s", out.String())
		}
	})
	t.Run("an AGENTS.md that appears later keeps the report on CLAUDE.md", func(t *testing.T) {
		t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
		projectDir := initializedProject(t)
		writeFileT(t, filepath.Join(projectDir, "CLAUDE.md"), current)
		writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), englishAgents)
		var out bytes.Buffer
		runDoctor(projectDir, t.TempDir(), t.TempDir(), &out)
		if !strings.Contains(out.String(), "[ok] CLAUDE.md vexillum block - current (en)") {
			t.Errorf("doctor:\n%s", out.String())
		}
	})
}

func TestDoctor_ClaudeImportAndLegacyRules(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	core, _ := install.SlotTemplate(slot.LangEN)
	current, _ := slot.Upsert(englishAgents, core, false)
	writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), current)
	writeFileT(t, filepath.Join(projectDir, ".claude", "rules", "vexillum.md"), "# old\n")

	var out bytes.Buffer
	runDoctor(projectDir, t.TempDir(), t.TempDir(), &out)
	for _, want := range []string{
		"[warn] CLAUDE.md imports AGENTS.md - CLAUDE.md does not exist, so Claude Code will not see",
		"[warn] .claude/rules/vexillum.md - from an older vexillum",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
	writeFileT(t, filepath.Join(projectDir, "CLAUDE.md"), "# x\n")
	out.Reset()
	runDoctor(projectDir, t.TempDir(), t.TempDir(), &out)
	if !strings.Contains(out.String(), "CLAUDE.md has no @AGENTS.md line") {
		t.Errorf("not importing not reported:\n%s", out.String())
	}
}

func TestDoctor_ModelsInvalid(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	writeFileT(t, filepath.Join(projectDir, ".vexillum", "models.json"), `{"default": {"model": "gpt"}}`)
	var out bytes.Buffer
	if code := runDoctor(projectDir, t.TempDir(), t.TempDir(), &out); code != 0 {
		t.Fatalf("invalid models.json must only warn: exit %d", code)
	}
	if !strings.Contains(out.String(), "[warn] models.json - invalid:") || !strings.Contains(out.String(), "models.json") {
		t.Errorf("invalid models not reported:\n%s", out.String())
	}
}

// A project that is not initialized gets no project checks, just the one
// "project initialized" line.
func TestDoctor_NoProjectChecksWhenNotInitialized(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	var out bytes.Buffer
	runDoctor(projectDir, t.TempDir(), t.TempDir(), &out)
	if strings.Contains(out.String(), "AGENTS.md vexillum block") || strings.Contains(out.String(), "skill forum") {
		t.Errorf("unexpected project checks:\n%s", out.String())
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

// doctor reports a different vx earlier in PATH as a warning (never a
// failure), naming the one that wins.
func TestDoctor_AnotherVXEarlierInPathWarns(t *testing.T) {
	binDir := fakeBinDir(t, "claude", "herdr", "tmux", "vx")
	t.Setenv("PATH", binDir)
	projectDir := initializedProject(t)

	var out bytes.Buffer
	code := runDoctor(projectDir, t.TempDir(), t.TempDir(), &out)

	if code != 0 {
		t.Errorf("a shadowing vx must not fail doctor, got exit %d: %s", code, out.String())
	}
	want := "[warn] another vx earlier in PATH - " + filepath.Join(binDir, "vx")
	if !bytes.Contains(out.Bytes(), []byte(want)) {
		t.Errorf("expected %q in output, got: %s", want, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("warnings: herdr version, another vx earlier in PATH")) {
		t.Errorf("expected the summary to list the warning, got: %s", out.String())
	}
}

func TestDoctor_NoVXConflictIsOK(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)

	var out bytes.Buffer
	runDoctor(projectDir, t.TempDir(), t.TempDir(), &out)

	if !bytes.Contains(out.Bytes(), []byte("[ok] another vx earlier in PATH")) {
		t.Errorf("expected an ok line for the vx PATH check, got: %s", out.String())
	}
}

// doctor probes the project's Stop hook for real, from a bare environment
// (HOME and PATH=/usr/bin:/bin), and warns with the fix when it cannot find
// vx - even though the vx on the test's own PATH is fine.
func TestDoctor_StopHookResolvingVXIsOK(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	if _, err := scaffold.EnsureSentinelHook(projectDir); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".local", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeStub(t, filepath.Join(home, ".local", "bin"), "vx", "exit 0")

	var out bytes.Buffer
	runDoctor(projectDir, t.TempDir(), home, &out)

	for _, want := range []string{"[ok] sentinel Stop hook - registered", "[ok] sentinel Stop hook finds vx", "[ok] sentinel - not running"} {
		if !bytes.Contains(out.Bytes(), []byte(want)) {
			t.Errorf("expected %q in output, got: %s", want, out.String())
		}
	}
	if bytes.Contains(out.Bytes(), []byte("[warn] sentinel")) {
		t.Errorf("no sentinel warning expected, got: %s", out.String())
	}
}

func TestDoctor_StopHookNotFindingVXWarnsWithTheFix(t *testing.T) {
	for _, p := range []string{"/opt/homebrew/bin/vx", "/usr/local/bin/vx", "/home/linuxbrew/.linuxbrew/bin/vx", "/usr/bin/vx", "/bin/vx"} {
		if _, err := os.Stat(p); err == nil {
			t.Skipf("a real vx is installed at %s", p)
		}
	}
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux", "vx"))
	projectDir := initializedProject(t)
	if _, err := scaffold.EnsureSentinelHook(projectDir); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	code := runDoctor(projectDir, t.TempDir(), t.TempDir(), &out)

	if code != 0 {
		t.Errorf("a hook warning must not fail doctor, got exit %d", code)
	}
	for _, want := range []string{"[warn] sentinel Stop hook finds vx - not found with a minimal environment", "install vx where the hook looks", "sentinel Stop hook finds vx)."} {
		if !bytes.Contains(out.Bytes(), []byte(want)) {
			t.Errorf("expected %q in output, got: %s", want, out.String())
		}
	}
}

func TestDoctor_StopHookMissingWarnsToUpgrade(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)

	var out bytes.Buffer
	runDoctor(projectDir, t.TempDir(), t.TempDir(), &out)

	if !bytes.Contains(out.Bytes(), []byte("[warn] sentinel Stop hook - not registered")) {
		t.Errorf("expected a missing-hook warning, got: %s", out.String())
	}
}

// Two sentinels alive at once are reported as a warning with the pids to stop,
// and never fail doctor.
func TestDoctor_WarnsWhenMoreThanOneSentinelIsAlive(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	t.Cleanup(doctorcheck.SetLiveSentinelPIDs(func() ([]int, error) { return []int{31337, 31338}, nil }))
	projectDir := initializedProject(t)
	if _, err := scaffold.EnsureSentinelHook(projectDir); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	code := runDoctor(projectDir, t.TempDir(), t.TempDir(), &out)

	if code != 0 {
		t.Errorf("exit code = %d, want 0: a sentinel warning must not fail doctor", code)
	}
	for _, want := range []string{"[warn] sentinel - 2 sentinel processes are alive (pids 31337, 31338)", "warnings:"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("expected %q in output, got: %s", want, out.String())
		}
	}
}
