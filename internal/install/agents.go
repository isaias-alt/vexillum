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

// Agents is the project's AGENTS.md as found on disk.
type Agents struct {
	Path    string
	Exists  bool
	Content string
}

// ReadAgents reads projectDir/AGENTS.md. A missing file is not an error:
// Exists is false and Content empty.
func ReadAgents(projectDir string) (Agents, error) {
	a := Agents{Path: filepath.Join(projectDir, "AGENTS.md")}
	data, err := os.ReadFile(a.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return a, fmt.Errorf("reading %s: %w", a.Path, err)
	}
	a.Exists = true
	a.Content = string(data)
	return a, nil
}

// LangSource says where a language choice came from.
type LangSource int

const (
	// LangFlag: given explicitly with --lang.
	LangFlag LangSource = iota
	// LangBlock: the language of the vexillum block already in the file.
	LangBlock
	// LangDetected: guessed from the user's own text in AGENTS.md.
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

// AgentsBackupRelPath is where SaveAgentsBackup keeps a whole-file copy of
// AGENTS.md, relative to the project directory.
const AgentsBackupRelPath = ".vexillum/AGENTS.md.backup"

// SaveAgentsBackup writes content (the full AGENTS.md as it was) to
// AgentsBackupRelPath atomically and returns the path. Call it before a
// repair that removes text.
func SaveAgentsBackup(projectDir, content string) (string, error) {
	p := filepath.Join(projectDir, filepath.FromSlash(AgentsBackupRelPath))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", filepath.Dir(p), err)
	}
	if err := atomicfile.Write(p, []byte(content)); err != nil {
		return "", fmt.Errorf("saving AGENTS.md backup: %w", err)
	}
	return p, nil
}
