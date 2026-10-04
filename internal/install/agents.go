package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
	"github.com/isaias-alt/vexillum/internal/commander"
	"github.com/isaias-alt/vexillum/internal/slot"
)

// The two files that can hold the vexillum block.
const (
	AgentsFile = "AGENTS.md"
	ClaudeFile = "CLAUDE.md"
)

// SlotFile is the file that holds, or will hold, the vexillum block: the
// project's AGENTS.md or its CLAUDE.md, as found on disk.
type SlotFile struct {
	// Name is AgentsFile or ClaudeFile.
	Name    string
	Path    string
	Exists  bool
	Content string
}

// ReadSlotFile reads projectDir/name (AgentsFile or ClaudeFile). A missing
// file is not an error: Exists is false and Content empty.
func ReadSlotFile(projectDir, name string) (SlotFile, error) {
	f := SlotFile{Name: name, Path: filepath.Join(projectDir, name)}
	data, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, fmt.Errorf("reading %s: %w", f.Path, err)
	}
	f.Exists = true
	f.Content = string(data)
	return f, nil
}

// LocateSlotFile finds the file that holds the vexillum block, in order: an
// AGENTS.md that has one; a CLAUDE.md that has one; an AGENTS.md that exists
// without one; and otherwise CLAUDE.md (created if missing), the default.
// choose is true only in that last case, when there is no AGENTS.md and no
// block anywhere, so the person may still prefer a new AGENTS.md.
func LocateSlotFile(projectDir string) (file SlotFile, choose bool, err error) {
	agents, err := ReadSlotFile(projectDir, AgentsFile)
	if err != nil {
		return agents, false, err
	}
	if slot.Inspect(agents.Content, "").State != slot.StateAbsent {
		return agents, false, nil
	}
	claude, err := ReadSlotFile(projectDir, ClaudeFile)
	if err != nil {
		return claude, false, err
	}
	if slot.Inspect(claude.Content, "").State != slot.StateAbsent {
		return claude, false, nil
	}
	if agents.Exists {
		return agents, false, nil
	}
	return claude, true, nil
}

// LangSource says where a language choice came from.
type LangSource int

const (
	// LangFlag: given explicitly with --lang.
	LangFlag LangSource = iota
	// LangBlock: the language of the vexillum block already in the file.
	LangBlock
	// LangDetected: guessed from the user's own text in the file.
	LangDetected
	// LangDefault: nothing to go on, the default (English).
	LangDefault
)

// ResolveLang picks the language for the vexillum block, in order: the flag
// (when not empty), the language of a well-formed block already in content
// (so a rerun does not flip it because the surrounding text reads
// differently), the language detected in the user's own text, and finally
// English.
func ResolveLang(content string, flag slot.Lang) (slot.Lang, LangSource) {
	if flag != "" {
		return flag, LangFlag
	}
	if ins := slot.Inspect(content, ""); ins.Body != "" {
		if lang, conf := slot.DetectLanguage(ins.Body); conf > 0 {
			return lang, LangBlock
		}
	}
	if lang, conf := slot.DetectLanguage(content); conf > 0 {
		return lang, LangDetected
	}
	return slot.LangEN, LangDefault
}

// SlotTemplate renders the core body for lang, the text that belongs between
// the markers.
func SlotTemplate(lang slot.Lang) (string, error) {
	return commander.Core(string(lang))
}

// InspectSlot classifies the block in content against the template for lang.
func InspectSlot(content string, lang slot.Lang) (slot.Inspection, string, error) {
	tmpl, err := SlotTemplate(lang)
	if err != nil {
		return slot.Inspection{}, "", err
	}
	return slot.Inspect(content, tmpl), tmpl, nil
}

// SlotFileBackupRelPath is where SaveSlotFileBackup keeps a whole-file copy
// of the named file (AGENTS.md or CLAUDE.md), relative to the project
// directory.
func SlotFileBackupRelPath(name string) string {
	return ".vexillum/" + name + ".backup"
}

// SaveSlotFileBackup writes content (the full file as it was) to
// SlotFileBackupRelPath atomically and returns the path. Call it before a
// repair that removes text.
func SaveSlotFileBackup(projectDir, name, content string) (string, error) {
	p := filepath.Join(projectDir, filepath.FromSlash(SlotFileBackupRelPath(name)))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", filepath.Dir(p), err)
	}
	if err := atomicfile.Write(p, []byte(content)); err != nil {
		return "", fmt.Errorf("saving %s backup: %w", name, err)
	}
	return p, nil
}
