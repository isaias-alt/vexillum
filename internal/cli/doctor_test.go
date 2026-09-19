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

// initializedProject returns a project dir that is a git repo with the
// vexillum scaffold already in place (as vexillum init would leave it).
func initializedProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	initGitRepo(t, dir)
	if err := writeLocalConfig(dir); err != nil {
		t.Fatalf("writing local config: %v", err)
	}
	return dir
}

// L1-07: full environment, everything ok, exit 0.
func TestDoctor_FullEnvironment(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := initializedProject(t)
	vexillumHome := t.TempDir()

	var out bytes.Buffer
	code := runDoctor(projectDir, vexillumHome, &out)

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

	var out bytes.Buffer
	code := runDoctor(projectDir, vexillumHome, &out)

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

	var out bytes.Buffer
	code := runDoctor(projectDir, vexillumHome, &out)

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

	var out bytes.Buffer
	code := runDoctor(projectDir, vexillumHome, &out)

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

	var out bytes.Buffer
	code := runDoctor(projectDir, vexillumHome, &out)

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

	before, err := snapshotTree(t, projectDir)
	if err != nil {
		t.Fatalf("snapshotting project dir: %v", err)
	}
	homeBefore, err := snapshotTree(t, vexillumHome)
	if err != nil {
		t.Fatalf("snapshotting vexillum home: %v", err)
	}

	var out bytes.Buffer
	runDoctor(projectDir, vexillumHome, &out)

	after, err := snapshotTree(t, projectDir)
	if err != nil {
		t.Fatalf("snapshotting project dir after: %v", err)
	}
	homeAfter, err := snapshotTree(t, vexillumHome)
	if err != nil {
		t.Fatalf("snapshotting vexillum home after: %v", err)
	}

	if before != after {
		t.Errorf("project dir changed:\nbefore: %s\nafter:  %s", before, after)
	}
	if homeBefore != homeAfter {
		t.Errorf("vexillum home changed:\nbefore: %s\nafter:  %s", homeBefore, homeAfter)
	}
}

// L1-13: outside a git repo, doctor's git check reports it, exit code
// consistent with the policy chosen for init (L1-05): required, non-zero.
func TestDoctor_OutsideGitRepo(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr", "tmux"))
	projectDir := t.TempDir()
	vexillumHome := t.TempDir()

	var out bytes.Buffer
	code := runDoctor(projectDir, vexillumHome, &out)

	if code == 0 {
		t.Fatalf("expected non-zero exit outside a git repository, got 0\noutput:\n%s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("[missing] git repository")) {
		t.Errorf("expected git repository check to be reported missing, got:\n%s", out.String())
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
