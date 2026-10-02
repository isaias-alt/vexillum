package slot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ClaudeImportLine is the line vexillum adds to CLAUDE.md so Claude Code
// loads AGENTS.md (and with it the slot).
const ClaudeImportLine = "@AGENTS.md"

// ClaudeAction says what edit CLAUDE.md needs.
type ClaudeAction int

const (
	// ClaudeOK: CLAUDE.md already imports AGENTS.md (or is AGENTS.md).
	ClaudeOK ClaudeAction = iota
	// ClaudeCreate: CLAUDE.md does not exist; create it with the import.
	ClaudeCreate
	// ClaudeAppend: CLAUDE.md exists without the import; append the line.
	ClaudeAppend
)

func (a ClaudeAction) String() string {
	switch a {
	case ClaudeOK:
		return "ok"
	case ClaudeCreate:
		return "create"
	case ClaudeAppend:
		return "append"
	}
	return fmt.Sprintf("ClaudeAction(%d)", int(a))
}

// ClaudeImport is the result of EnsureClaudeImport. Nothing has been
// written: the caller asks for consent, then writes NewContent to Path
// (WriteFile) when Action is not ClaudeOK.
type ClaudeImport struct {
	Path    string
	Exists  bool
	Imports bool
	Action  ClaudeAction
	// NewContent is the full CLAUDE.md after the minimal edit; empty for
	// ClaudeOK.
	NewContent string
}

// EnsureClaudeImport inspects projectDir/CLAUDE.md. CLAUDE.md that is a
// symlink (or hard link) to AGENTS.md counts as importing it.
func EnsureClaudeImport(projectDir string) (ClaudeImport, error) {
	res := ClaudeImport{Path: filepath.Join(projectDir, "CLAUDE.md")}
	data, err := os.ReadFile(res.Path)
	if errors.Is(err, os.ErrNotExist) {
		res.Action, res.NewContent = ClaudeCreate, ClaudeImportLine+"\n"
		return res, nil
	}
	if err != nil {
		return res, fmt.Errorf("reading %s: %w", res.Path, err)
	}
	res.Exists = true

	if sameFile(res.Path, filepath.Join(projectDir, "AGENTS.md")) {
		res.Imports = true
		return res, nil
	}
	res.Imports = ImportsAgents(string(data))
	if !res.Imports {
		res.Action, res.NewContent = ClaudeAppend, AppendImport(string(data))
	}
	return res, nil
}

func sameFile(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

var importRE = regexp.MustCompile(`(^|[\s(])@(\./)?AGENTS\.md($|[\s)]|[.,;:!?]($|\s))`)

// ImportsAgents reports whether CLAUDE.md content imports AGENTS.md: a
// "@AGENTS.md" or "@./AGENTS.md" token outside fenced code and inline code,
// which is where Claude Code resolves @imports.
func ImportsAgents(content string) bool {
	var fenceCh byte
	fenceN := 0
	d := parse(content) // reuse the line splitter only; markers are irrelevant
	for i := range d.lines {
		t := d.text(i)
		if fenceN > 0 {
			if closesFence(t, fenceCh, fenceN) {
				fenceN = 0
			}
			continue
		}
		if ch, n, ok := opensFence(t); ok {
			fenceCh, fenceN = ch, n
			continue
		}
		if importRE.MatchString(inlineCodeRE.ReplaceAllString(t, " ")) {
			return true
		}
	}
	return false
}

// AppendImport returns content with the import line added at the end, on its
// own line, keeping the file's line-ending style.
func AppendImport(content string) string {
	if content == "" {
		return ClaudeImportLine + "\n"
	}
	eol := eolOf(content)
	if !strings.HasSuffix(content, "\n") {
		content += eol
	}
	return content + ClaudeImportLine + eol
}
