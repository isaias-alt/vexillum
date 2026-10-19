package soldier

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
)

// provisionedSidecar is the file, in the camp's own git directory, that
// remembers which files vexillum wrote into the camp for a soldier and what
// they contained. It is how a camp reused for a later task tells its own
// leftovers from a file the project owns: git does not remove an ignored
// file when a camp is reset, so the previous soldier's files are still there
// when the slot comes back.
const provisionedSidecar = "vexillum-provisioned.json"

type provisionedRecord struct {
	// Files maps a camp-relative path (slash separated) to the SHA-256 of
	// the content vexillum wrote there.
	Files map[string]string `json:"files"`
}

// provisionFiles writes spec's files into the camp and excludes them from
// git, or fails without leaving anything behind.
//
// It never overwrites and never merges: if any path of spec.Files, or any of
// spec.RefuseIfPresent, already exists in the camp, the launch fails and the
// error names it. A camp is a fresh checkout, so a file that is there belongs
// to the project (it is tracked), apart from the leftovers of an earlier
// soldier of vexillum's, which retractProvisioned removed first. The general
// has not decided a merge policy for a project that carries its own
// OpenCode configuration; until then the choice is theirs, to move the file
// or to run the task on another harness.
func provisionFiles(campPath string, spec LaunchSpec) error {
	if len(spec.Files) == 0 {
		return nil
	}
	for _, path := range append(filePaths(spec.Files), spec.RefuseIfPresent...) {
		if _, err := os.Lstat(filepath.Join(campPath, filepath.FromSlash(path))); err == nil {
			return collisionError(campPath, path, spec)
		} else if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			return fmt.Errorf("checking %s in the camp: %w", path, err)
		}
	}

	var written []string
	undo := func() {
		for _, p := range written {
			_ = os.Remove(filepath.Join(campPath, filepath.FromSlash(p)))
			pruneEmptyDirs(campPath, filepath.Dir(filepath.FromSlash(p)))
		}
	}
	record := provisionedRecord{Files: map[string]string{}}
	for _, f := range spec.Files {
		full := filepath.Join(campPath, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			undo()
			return fmt.Errorf("creating the directory for %s in the camp: %w", f.Path, err)
		}
		// O_EXCL: a file that appeared since the check above is a collision
		// too, never an overwrite.
		out, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			undo()
			if errors.Is(err, fs.ErrExist) {
				return collisionError(campPath, f.Path, spec)
			}
			return fmt.Errorf("writing %s in the camp: %w", f.Path, err)
		}
		_, werr := out.Write(f.Content)
		cerr := out.Close()
		written = append(written, f.Path)
		if werr != nil || cerr != nil {
			undo()
			return fmt.Errorf("writing %s in the camp: %w", f.Path, errors.Join(werr, cerr))
		}
		sum := sha256.Sum256(f.Content)
		record.Files[f.Path] = hex.EncodeToString(sum[:])
	}

	if err := ensureExcluded(campPath, spec.GitExclude); err != nil {
		undo()
		return err
	}
	if dir, err := gitDirOf(campPath); err == nil {
		if err := atomicfile.WriteJSON(filepath.Join(dir, provisionedSidecar), record); err != nil {
			undo()
			return fmt.Errorf("recording the files written into the camp: %w", err)
		}
	}
	return nil
}

func filePaths(files []ProvisionedFile) []string {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	return paths
}

// collisionError names a path that is already in the camp and says what
// vexillum will and will not do about it.
func collisionError(campPath, path string, spec LaunchSpec) error {
	owner := "already exists in the camp"
	if trackedInGit(campPath, path) {
		owner = "is tracked by the project"
	}
	return fmt.Errorf("cannot start this soldier: %s %s. vexillum writes its own %s into the camp for this soldier and never overwrites or merges a file of the project's (no merge policy has been decided): move or remove that file in the project, or dispatch this task on another harness", path, owner, strings.Join(filePaths(spec.Files), " and "))
}

// trackedInGit reports whether path is tracked in the camp's repository.
func trackedInGit(campPath, path string) bool {
	cmd := exec.Command("git", "ls-files", "--error-unmatch", "--", path)
	cmd.Dir = campPath
	return cmd.Run() == nil
}

// retractProvisioned removes the files vexillum wrote into this camp for an
// earlier soldier, but only each one that still holds exactly what vexillum
// wrote: a file someone changed is left, and then counts as the project's.
// It is best effort and silent: a camp with no sidecar, or one that does not
// exist, has nothing to retract.
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

// ensureExcluded adds each pattern to the repository's info/exclude, once.
// info/exclude belongs to the repository, not to the worktree: it is shared
// by the project's own checkout and every camp, so the patterns hide a
// project's untracked opencode.json in its main checkout too. Tracked files
// are not affected by it. Concurrent soldiers append under an exclusive lock.
func ensureExcluded(campPath string, patterns []string) error {
	if len(patterns) == 0 {
		return nil
	}
	cmd := exec.Command("git", "rev-parse", "--git-path", "info/exclude")
	cmd.Dir = campPath
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("locating the repository's info/exclude: %w", err)
	}
	path := strings.TrimSpace(string(out))
	if !filepath.IsAbs(path) {
		path = filepath.Join(campPath, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating the directory of info/exclude: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("opening info/exclude: %w", err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("locking info/exclude: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	var existing bytes.Buffer
	if _, err := existing.ReadFrom(f); err != nil {
		return fmt.Errorf("reading info/exclude: %w", err)
	}
	have := map[string]bool{}
	for _, line := range strings.Split(existing.String(), "\n") {
		have[strings.TrimSpace(line)] = true
	}
	var add strings.Builder
	if existing.Len() > 0 && !bytes.HasSuffix(existing.Bytes(), []byte("\n")) {
		add.WriteString("\n")
	}
	header := "# vexillum: files written into a camp for an OpenCode soldier"
	missing := false
	for _, p := range patterns {
		if !have[p] {
			missing = true
		}
	}
	if !missing {
		return nil
	}
	if !have[header] {
		add.WriteString(header + "\n")
	}
	for _, p := range patterns {
		if !have[p] {
			add.WriteString(p + "\n")
		}
	}
	if _, err := f.Seek(0, 2); err != nil {
		return fmt.Errorf("writing info/exclude: %w", err)
	}
	if _, err := f.WriteString(add.String()); err != nil {
		return fmt.Errorf("writing info/exclude: %w", err)
	}
	return nil
}
