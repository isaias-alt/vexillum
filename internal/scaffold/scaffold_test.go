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

func TestSaveConfigRoundTripsSkillHashes(t *testing.T) {
	dir := t.TempDir()
	if err := WriteConfig(dir); err != nil {
		t.Fatal(err)
	}
	cfg, err := ReadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Skills != nil {
		t.Errorf("a fresh config has no skill hashes, got %v", cfg.Skills)
	}
	cfg.Skills = map[string]string{"forum": "abc"}
	cfg.VexillumRuleHash = HashContent("x")
	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := ReadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Skills["forum"] != "abc" || got.VexillumRuleHash != HashContent("x") || got.Version != 1 {
		t.Errorf("round trip lost data: %+v", got)
	}
}
