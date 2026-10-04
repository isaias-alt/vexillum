// Package yolo stores the per-project "yolo" setting: whether the commander
// lands a finished, verified mission right away instead of asking the general
// first.
//
// The setting lives in <projectDir>/.vexillum/yolo.json, next to models.json,
// and follows the same convention: it is not git-ignored, so it is committed
// with the project. The file is JSON, in English:
//
//	{"enabled": true}
//
// Off is the default: a missing file means off. Unknown fields are rejected so
// a typo does not silently turn the setting off. This package only stores and
// reads the flag; what the commander does when it is on is written in the
// vexillum skill, not here.
package yolo

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
)

// FileName is the name of the yolo file inside a project's .vexillum/
// directory.
const FileName = "yolo.json"

type file struct {
	Enabled bool `json:"enabled"`
}

// Path returns the yolo file's path for projectDir.
func Path(projectDir string) string {
	return filepath.Join(projectDir, ".vexillum", FileName)
}

// Enabled reports whether yolo is on for projectDir. A missing file is off. A
// file that cannot be read or is not valid is an error naming the file, never
// a silent "off" or "on".
func Enabled(projectDir string) (bool, error) {
	path := Path(projectDir)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	var f file
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return false, fmt.Errorf("%s: invalid JSON: %w", path, err)
	}
	if dec.More() {
		return false, fmt.Errorf("%s: invalid JSON: unexpected data after the top-level object", path)
	}
	return f.Enabled, nil
}

// Set turns yolo on or off for projectDir with an atomic write, creating
// .vexillum/ when it does not exist yet.
func Set(projectDir string, on bool) error {
	dir := filepath.Join(projectDir, ".vexillum")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	return atomicfile.WriteJSON(Path(projectDir), file{Enabled: on})
}
