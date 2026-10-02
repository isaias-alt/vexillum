package slot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveBackup(t *testing.T) {
	dir := t.TempDir()
	path, err := SaveBackup(dir, "my edit\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, ".vexillum", "agents-md-slot.backup.md") {
		t.Errorf("path = %q", path)
	}
	if b, _ := os.ReadFile(path); string(b) != "my edit\n" {
		t.Errorf("content = %q", b)
	}
	if _, err := SaveBackup(dir, "second"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "second\n" {
		t.Errorf("backup not replaced: %q", b)
	}
}

func TestWriteFile(t *testing.T) {
	t.Run("new file is 0644", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "AGENTS.md")
		if err := WriteFile(p, "x"); err != nil {
			t.Fatal(err)
		}
		info, _ := os.Stat(p)
		if info.Mode().Perm() != 0o644 {
			t.Errorf("mode = %v", info.Mode().Perm())
		}
	})
	t.Run("keeps existing mode", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "AGENTS.md")
		if err := os.WriteFile(p, []byte("old"), 0o640); err != nil {
			t.Fatal(err)
		}
		if err := WriteFile(p, "new"); err != nil {
			t.Fatal(err)
		}
		info, _ := os.Stat(p)
		if b, _ := os.ReadFile(p); string(b) != "new" || info.Mode().Perm() != 0o640 {
			t.Errorf("content %q mode %v", b, info.Mode().Perm())
		}
	})
	t.Run("writes through a symlink", func(t *testing.T) {
		dir := t.TempDir()
		real := filepath.Join(dir, "real.md")
		link := filepath.Join(dir, "AGENTS.md")
		if err := os.WriteFile(real, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("real.md", link); err != nil {
			t.Skip("symlinks unavailable:", err)
		}
		if err := WriteFile(link, "new"); err != nil {
			t.Fatal(err)
		}
		if info, _ := os.Lstat(link); info.Mode()&os.ModeSymlink == 0 {
			t.Error("symlink was replaced by a regular file")
		}
		if b, _ := os.ReadFile(real); string(b) != "new" {
			t.Errorf("target = %q", b)
		}
	})
	t.Run("leaves no temp files", func(t *testing.T) {
		dir := t.TempDir()
		if err := WriteFile(filepath.Join(dir, "a.md"), "x"); err != nil {
			t.Fatal(err)
		}
		if es, _ := os.ReadDir(dir); len(es) != 1 {
			t.Errorf("dir has %d entries", len(es))
		}
	})
}
