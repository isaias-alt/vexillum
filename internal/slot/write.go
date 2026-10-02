package slot

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
)

// BackupRelPath is where SaveBackup keeps the user's version of the block,
// relative to the project directory.
const BackupRelPath = ".vexillum/agents-md-slot.backup.md"

// SaveBackup writes the user's edited body to BackupRelPath under projectDir
// (atomically, creating .vexillum/ if needed), to be called before a forced
// overwrite. An earlier backup is replaced. It returns the path written.
func SaveBackup(projectDir, body string) (string, error) {
	path := filepath.Join(projectDir, filepath.FromSlash(BackupRelPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := atomicfile.Write(path, []byte(canonical(body)+"\n")); err != nil {
		return "", fmt.Errorf("saving slot backup: %w", err)
	}
	return path, nil
}

// WriteFile atomically writes content to path for a user-owned file such as
// AGENTS.md or CLAUDE.md. Unlike atomicfile.Write alone it keeps the existing
// file's permission bits (temp files are 0600) and writes through a symlink
// instead of replacing the link with a regular file. A new file gets 0644.
// The parent directory must exist.
func WriteFile(path string, content string) error {
	target := path
	mode := os.FileMode(0o644)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	}
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}
	if err := atomicfile.Write(target, []byte(content)); err != nil {
		return err
	}
	if err := os.Chmod(target, mode); err != nil {
		return fmt.Errorf("setting mode on %s: %w", target, err)
	}
	return nil
}
