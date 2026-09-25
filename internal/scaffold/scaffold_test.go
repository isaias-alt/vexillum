package scaffold

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDir(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "sub", "dir")

	created, err := EnsureDir(target)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if !created {
		t.Error("expected created=true for a fresh directory")
	}
	if info, statErr := os.Stat(target); statErr != nil || !info.IsDir() {
		t.Fatalf("expected %s to exist as a directory", target)
	}

	createdAgain, err := EnsureDir(target)
	if err != nil {
		t.Fatalf("EnsureDir (second call): %v", err)
	}
	if createdAgain {
		t.Error("expected created=false when the directory already exists")
	}
}

func TestWriteConfigAndReadConfig(t *testing.T) {
	dir := t.TempDir()

	if ProjectInitialized(dir) {
		t.Fatal("expected ProjectInitialized to be false before WriteConfig")
	}

	if err := WriteConfig(dir); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	cfg, err := ReadConfig(dir)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if cfg.Version != 1 {
		t.Errorf("expected Version 1, got %d", cfg.Version)
	}
	if cfg.InitializedAt.IsZero() {
		t.Error("expected InitializedAt to be set")
	}
	if cfg.VexillumRuleHash != "" {
		t.Errorf("expected a fresh config to have no recorded hash, got %q", cfg.VexillumRuleHash)
	}
}

func TestGlobalInitialized(t *testing.T) {
	home := t.TempDir()

	if GlobalInitialized(home) {
		t.Fatal("expected GlobalInitialized to be false before WriteConfig")
	}
	if err := WriteConfig(home); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
	if !GlobalInitialized(home) {
		t.Error("expected GlobalInitialized to be true after WriteConfig")
	}
}

func TestRecordHash(t *testing.T) {
	dir := t.TempDir()
	if err := WriteConfig(dir); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	// updated=false is a no-op - the config stays without a hash.
	if err := RecordHash(dir, false); err != nil {
		t.Fatalf("RecordHash(false): %v", err)
	}
	cfg, err := ReadConfig(dir)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if cfg.VexillumRuleHash != "" {
		t.Fatal("expected no hash recorded when updated=false")
	}

	if err := RecordHash(dir, true); err != nil {
		t.Fatalf("RecordHash(true): %v", err)
	}
	cfg, err = ReadConfig(dir)
	if err != nil {
		t.Fatalf("ReadConfig (after RecordHash): %v", err)
	}
	if cfg.VexillumRuleHash != hashContent(VexillumCommanderRules) {
		t.Errorf("expected the recorded hash to match VexillumCommanderRules' own hash, got %q", cfg.VexillumRuleHash)
	}
}

func TestWriteFileIfMissing(t *testing.T) {
	dir := t.TempDir()

	created, err := WriteFileIfMissing(dir, "note.txt", "first")
	if err != nil {
		t.Fatalf("WriteFileIfMissing: %v", err)
	}
	if !created {
		t.Error("expected created=true for a missing file")
	}

	createdAgain, err := WriteFileIfMissing(dir, "note.txt", "second")
	if err != nil {
		t.Fatalf("WriteFileIfMissing (second call): %v", err)
	}
	if createdAgain {
		t.Error("expected created=false once the file already exists")
	}

	data, err := os.ReadFile(filepath.Join(dir, "note.txt"))
	if err != nil {
		t.Fatalf("reading note.txt: %v", err)
	}
	if string(data) != "first" {
		t.Errorf("expected the original content to survive, got %q", string(data))
	}
}

func TestIsGitRepo(t *testing.T) {
	if IsGitRepo(t.TempDir()) {
		t.Error("expected a fresh temp dir to not be a git repository")
	}
}

func TestUpgradeFile_CreatesWhenMissing(t *testing.T) {
	dir := t.TempDir()

	result, err := UpgradeFile(dir, "rule.md", "latest content", "", false)
	if err != nil {
		t.Fatalf("UpgradeFile: %v", err)
	}
	if !result.Changed {
		t.Error("expected Changed=true when the file was missing")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "rule.md"))
	if string(data) != "latest content" {
		t.Errorf("expected the file to be created with the latest content, got %q", string(data))
	}
}

func TestUpgradeFile_AlreadyUpToDate(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "rule.md"), []byte("latest content"), 0o644)

	result, err := UpgradeFile(dir, "rule.md", "latest content", "", false)
	if err != nil {
		t.Fatalf("UpgradeFile: %v", err)
	}
	if result.Changed {
		t.Error("expected Changed=false when content already matches")
	}
	if result.Status != "already up to date" {
		t.Errorf("unexpected status: %q", result.Status)
	}
}

func TestUpgradeFile_SafeRefreshWhenHashMatches(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "rule.md"), []byte("old content"), 0o644)

	result, err := UpgradeFile(dir, "rule.md", "new content", hashContent("old content"), false)
	if err != nil {
		t.Fatalf("UpgradeFile: %v", err)
	}
	if !result.Changed {
		t.Error("expected Changed=true when the stored hash matches the file's current content")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "rule.md"))
	if string(data) != "new content" {
		t.Errorf("expected the file to be refreshed, got %q", string(data))
	}
}

func TestUpgradeFile_LeavesLocalEditsUntouchedWithoutForce(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "rule.md"), []byte("hand-edited content"), 0o644)

	result, err := UpgradeFile(dir, "rule.md", "new content", hashContent("something else"), false)
	if err != nil {
		t.Fatalf("UpgradeFile: %v", err)
	}
	if result.Changed {
		t.Error("expected Changed=false when the stored hash doesn't match and force isn't set")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "rule.md"))
	if string(data) != "hand-edited content" {
		t.Error("expected the hand-edited content to be left untouched")
	}
}

func TestUpgradeFile_ForceOverwritesLocalEdits(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "rule.md"), []byte("hand-edited content"), 0o644)

	result, err := UpgradeFile(dir, "rule.md", "new content", hashContent("something else"), true)
	if err != nil {
		t.Fatalf("UpgradeFile: %v", err)
	}
	if !result.Changed {
		t.Error("expected Changed=true with --force")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "rule.md"))
	if string(data) != "new content" {
		t.Errorf("expected --force to overwrite local edits, got %q", string(data))
	}
}
