// Package install holds the read and write logic behind "vx init", "vx
// upgrade" and "vx doctor" for the three things vexillum puts in a user's
// project: the first-party skills under .claude/skills/, the vexillum block
// in AGENTS.md, and the language the block is written in. Inspection is
// separate from writing so doctor can share it while staying read-only.
package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
	"github.com/isaias-alt/vexillum/skills"
)

// SkillState is how an installed skill compares with the embedded copy.
type SkillState int

const (
	// SkillMissing: the skill is not installed (no SKILL.md).
	SkillMissing SkillState = iota
	// SkillCurrent: identical to the embedded copy.
	SkillCurrent
	// SkillStale: untouched since vexillum installed it (it still matches
	// the recorded hash) but older than the embedded copy: safe to refresh.
	SkillStale
	// SkillEdited: differs from the embedded copy and from what vexillum
	// last wrote (or nothing was ever recorded): the user's own changes.
	SkillEdited
)

func (s SkillState) String() string {
	switch s {
	case SkillMissing:
		return "missing"
	case SkillCurrent:
		return "current"
	case SkillStale:
		return "stale"
	case SkillEdited:
		return "edited"
	}
	return fmt.Sprintf("SkillState(%d)", int(s))
}

// SkillStatus is the result of InspectSkill.
type SkillStatus struct {
	Name  string
	State SkillState
	// Dir is where the skill lives (or would live) on disk.
	Dir string
	// Linked is true when Dir is a symlink, as in a repository that
	// dogfoods its own skills/ directory. Such a skill is the user's
	// to manage: init and upgrade never write through the link.
	Linked bool
	// Hash is the content hash of what is installed ("" when missing).
	Hash string
}

// ignoredFile reports files that are never part of a skill's content, so a
// stray macOS .DS_Store does not make a pristine skill look edited.
func ignoredFile(name string) bool { return name == ".DS_Store" }

// readInstalled reads every regular file under dir, relative and
// slash-separated, skipping ignored ones. dir is resolved through a symlink
// first; symlinks inside the skill are not followed.
func readInstalled(dir string) ([]skills.File, error) {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	var files []skills.File
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() || ignoredFile(d.Name()) {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		files = append(files, skills.File{Path: filepath.ToSlash(rel), Content: data})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// InspectSkill classifies the skill name under skillsDir (a .claude/skills
// directory) against the embedded copy. recorded is the hash vexillum stored
// the last time it installed the skill ("" when unknown).
func InspectSkill(skillsDir, name, recorded string) (SkillStatus, error) {
	st := SkillStatus{Name: name, Dir: filepath.Join(skillsDir, name)}
	info, err := os.Lstat(st.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	st.Linked = info.Mode()&os.ModeSymlink != 0

	if _, err := os.Stat(filepath.Join(st.Dir, "SKILL.md")); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return st, nil
		}
		return st, fmt.Errorf("checking %s: %w", st.Dir, err)
	}
	files, err := readInstalled(st.Dir)
	if err != nil {
		return st, fmt.Errorf("reading skill %s: %w", name, err)
	}
	st.Hash = skills.HashFiles(files)
	want, err := skills.Hash(name)
	if err != nil {
		return st, err
	}
	switch {
	case st.Hash == want:
		st.State = SkillCurrent
	case recorded != "" && recorded == st.Hash:
		st.State = SkillStale
	default:
		st.State = SkillEdited
	}
	return st, nil
}

// InstallSkill writes every file of the embedded skill name into
// skillsDir/<name>/, each one atomically. Files that were in the directory
// but are not part of the embedded skill (left over from an older version)
// are removed afterwards, along with directories that end up empty. The
// caller is responsible for deciding that overwriting is acceptable. It
// refuses to write through a symlinked skill directory.
func InstallSkill(skillsDir, name string) error {
	files, err := skills.Files(name)
	if err != nil {
		return err
	}
	dir := filepath.Join(skillsDir, name)
	if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink, refusing to write through it", dir)
	}
	keep := map[string]bool{}
	for _, f := range files {
		dest := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", filepath.Dir(dest), err)
		}
		if err := atomicfile.Write(dest, f.Content); err != nil {
			return fmt.Errorf("writing skill %s: %w", name, err)
		}
		if err := os.Chmod(dest, 0o644); err != nil {
			return fmt.Errorf("setting mode on %s: %w", dest, err)
		}
		keep[f.Path] = true
	}
	return pruneExtras(dir, keep)
}

// pruneExtras removes regular files under dir that are not in keep, then any
// directory left empty (deepest first).
func pruneExtras(dir string, keep map[string]bool) error {
	var dirs []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != dir {
				dirs = append(dirs, p)
			}
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if !keep[path.Clean(filepath.ToSlash(rel))] && !ignoredFile(d.Name()) {
			return os.Remove(p)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("pruning %s: %w", dir, err)
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		// os.Remove only succeeds on an empty directory.
		_ = os.Remove(d)
	}
	return nil
}

// BackupSkill copies the installed skill at skillsDir/<name> to
// backupRoot/<name>, replacing an earlier backup, and returns the path. Call
// it before overwriting an edited skill.
func BackupSkill(skillsDir, name, backupRoot string) (string, error) {
	files, err := readInstalled(filepath.Join(skillsDir, name))
	if err != nil {
		return "", fmt.Errorf("reading skill %s for backup: %w", name, err)
	}
	dest := filepath.Join(backupRoot, name)
	if err := os.RemoveAll(dest); err != nil {
		return "", fmt.Errorf("clearing old backup %s: %w", dest, err)
	}
	for _, f := range files {
		p := filepath.Join(dest, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", err
		}
		if err := atomicfile.Write(p, f.Content); err != nil {
			return "", fmt.Errorf("backing up skill %s: %w", name, err)
		}
	}
	return dest, nil
}

// SkillList joins skill names for display: "a, b and c".
func SkillList(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// displayOrder is the order skills are listed and asked about: the one the
// commander always needs first.
var displayOrder = []string{"vexillum", "forum", "muster"}

// SkillNames returns the embedded skills in display order, with any skill
// not named in displayOrder appended (a test keeps the two in step).
func SkillNames() []string {
	out := append([]string(nil), displayOrder...)
	seen := map[string]bool{}
	for _, n := range out {
		seen[n] = true
	}
	for _, n := range skills.Names() {
		if !seen[n] {
			out = append(out, n)
		}
	}
	return out
}
