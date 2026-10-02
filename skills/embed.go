// Package skills embeds vexillum's first-party Claude Code skills (vexillum,
// forum, muster) into the binary, straight from the directories next to this
// file, so there is exactly one copy of each skill's content: the one in the
// repository. `vx init` and `vx upgrade` write them out to
// .claude/skills/<name>/ from here, with no Node or network involved.
//
// A skill is a directory holding SKILL.md plus optional supporting files
// (playbooks, references). Every regular file under it is part of the skill.
package skills

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"sort"
)

//go:embed all:vexillum all:forum all:muster
var content embed.FS

// File is one file of a skill.
type File struct {
	// Path is slash-separated and relative to the skill's directory, for
	// example "SKILL.md" or "playbooks/plan.md".
	Path    string
	Content []byte
}

// Names lists the embedded skills, sorted. Every entry is a directory that
// holds a SKILL.md.
func Names() []string {
	entries, err := fs.ReadDir(content, ".")
	if err != nil {
		// The embed directive guarantees the root exists; this cannot
		// happen on a built binary.
		panic(fmt.Sprintf("skills: reading embedded root: %v", err))
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// Files returns every file of the named skill, sorted by path, SKILL.md
// included. The Content slices are copies. An unknown name is an error.
func Files(name string) ([]File, error) {
	if _, err := fs.Stat(content, path.Join(name, "SKILL.md")); err != nil {
		return nil, fmt.Errorf("unknown skill %q (available: %v)", name, Names())
	}
	var files []File
	err := fs.WalkDir(content, name, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := content.ReadFile(p)
		if err != nil {
			return err
		}
		files = append(files, File{Path: p[len(name)+1:], Content: data})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading skill %q: %w", name, err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// ReadFile returns one file of the named skill by its path relative to the
// skill's directory, for example ReadFile("forum", "SKILL.md").
func ReadFile(name, rel string) ([]byte, error) {
	if _, err := fs.Stat(content, path.Join(name, "SKILL.md")); err != nil {
		return nil, fmt.Errorf("unknown skill %q (available: %v)", name, Names())
	}
	data, err := content.ReadFile(path.Join(name, rel))
	if err != nil {
		return nil, fmt.Errorf("skill %q has no file %q", name, rel)
	}
	return data, nil
}

// Hash returns a stable content hash of the whole skill: the sha256 hex digest
// over every file's path and content in path order. It changes when any file
// is added, removed, renamed or edited, so `vx upgrade` and `vx doctor` can
// tell an installed copy that is current from one that is outdated or edited.
func Hash(name string) (string, error) {
	files, err := Files(name)
	if err != nil {
		return "", err
	}
	return HashFiles(files), nil
}

// HashFiles computes the same digest as Hash for an arbitrary set of files,
// for example the files read back from an installed skill directory. The
// order of files does not matter.
func HashFiles(files []File) string {
	sorted := append([]File(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	h := sha256.New()
	for _, f := range sorted {
		// Length-prefix each field so no path/content split can collide.
		fmt.Fprintf(h, "%d:%s\n%d:", len(f.Path), f.Path, len(f.Content))
		h.Write(f.Content)
		h.Write([]byte("\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}
