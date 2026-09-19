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
	dir := filepath.Dir(path)

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	data = append(data, '\n')

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
