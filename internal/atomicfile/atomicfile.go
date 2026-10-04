// Package atomicfile writes files atomically: to a temp file in the same
// directory, then renamed into place, so a reader never observes a
// partially written file. Used by every package that persists state to
// ~/.vexillum/ (tasks in Capa 2, camp pool state in Capa 3).
package atomicfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WriteJSON marshals v as indented JSON and writes it atomically to path.
// The parent directory must already exist.
func WriteJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	data = append(data, '\n')
	return Write(path, data)
}

// Write writes data atomically to path: to a temp file in the same
// directory, then renamed into place, so a reader never observes a
// partially written file. Unlike WriteJSON, data is written as-is - used
// for non-JSON content (e.g. the whiteboard feedback PNG in
// internal/forum). The parent directory must already exist.
func Write(path string, data []byte) error {
	return write(path, data, 0, false)
}

// WriteMode is Write for a file that must end up with the given permission
// bits (an executable, say) and must be on disk before it replaces path: the
// temp file is chmod-ed, then synced, then renamed into place.
func WriteMode(path string, data []byte, perm os.FileMode) error {
	return write(path, data, perm, true)
}

// write is the shared body. perm 0 keeps the temp file's own mode (0600).
func write(path string, data []byte, perm os.FileMode, sync bool) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file for %s: %w", path, err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if perm != 0 {
		if err := tmp.Chmod(perm); err != nil {
			tmp.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("setting the mode of %s: %w", path, err)
		}
	}
	if sync {
		if err := tmp.Sync(); err != nil {
			tmp.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("syncing %s: %w", path, err)
		}
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing temp file for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("committing %s: %w", path, err)
	}
	return nil
}
