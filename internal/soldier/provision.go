package soldier

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// provisionedSidecar is the file, in the camp's own git directory, that an
// earlier vexillum wrote to remember which files it had put into the camp for
// an OpenCode 2.x soldier and what they contained. vexillum no longer writes
// anything into a camp (an OpenCode soldier now gets its model through a
// session, see OpenCodeHarness), but a camp provisioned by an older version
// may still hold those files: git does not remove an ignored file when a camp
// is reset, so they are still there when the slot comes back.
const provisionedSidecar = "vexillum-provisioned.json"

type provisionedRecord struct {
	// Files maps a camp-relative path (slash separated) to the SHA-256 of
	// the content the older vexillum wrote there.
	Files map[string]string `json:"files"`
}

// retractProvisioned removes the files an older vexillum wrote into this camp
// for an earlier OpenCode 2.x soldier, but only each one that still holds
// exactly what it wrote: a file someone changed is left, and then counts as the
// project's. It runs before every launch, whatever the harness, so a camp
// reused by any soldier does not keep another's config. It is best effort and
// silent: a camp with no sidecar, or one that does not exist, has nothing to
// retract.
func retractProvisioned(campPath string) {
	dir, err := gitDirOf(campPath)
	if err != nil {
		return
	}
	sidecar := filepath.Join(dir, provisionedSidecar)
	raw, err := os.ReadFile(sidecar)
	if err != nil {
		return
	}
	var record provisionedRecord
	if json.Unmarshal(raw, &record) != nil {
		_ = os.Remove(sidecar)
		return
	}
	for path, want := range record.Files {
		full := filepath.Join(campPath, filepath.FromSlash(path))
		content, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		if sum := sha256.Sum256(content); hex.EncodeToString(sum[:]) == want {
			_ = os.Remove(full)
			pruneEmptyDirs(campPath, filepath.Dir(filepath.FromSlash(path)))
		}
	}
	_ = os.Remove(sidecar)
}

// pruneEmptyDirs removes dir (relative to root) and its parents up to root,
// while they are empty. It never removes root.
func pruneEmptyDirs(root, dir string) {
	for dir != "." && dir != "" && dir != string(filepath.Separator) {
		if err := os.Remove(filepath.Join(root, dir)); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// gitDirOf is the git directory of the camp worktree: the .git directory of a
// plain checkout, or the one a linked worktree's .git file points to.
func gitDirOf(campPath string) (string, error) {
	dotGit := filepath.Join(campPath, ".git")
	info, err := os.Stat(dotGit)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return dotGit, nil
	}
	raw, err := os.ReadFile(dotGit)
	if err != nil {
		return "", err
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir:")
	if !ok {
		return "", fmt.Errorf("%s is not a gitdir pointer", dotGit)
	}
	target = strings.TrimSpace(target)
	if !filepath.IsAbs(target) {
		target = filepath.Join(campPath, target)
	}
	return target, nil
}
