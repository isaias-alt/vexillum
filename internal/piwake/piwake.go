// Package piwake is what vexillum puts in a project so a commander that runs
// in Pi is woken the way a Claude Code commander is: the wake extension
// (vexillum-wake.ts, embedded here) in .pi/extensions/, and the Pi project
// settings that let Pi see the skills vexillum installs in .claude/skills/.
//
// Claude Code is woken by an async Stop hook that execs "vx sentinel await" and
// reads its exit 2 as a message. Pi has no such hook, so the extension runs the
// same await as a child of Pi and, on exit 2, hands the text to the model with
// pi.sendUserMessage. That is in-process: the general's unsent draft in the
// editor is left alone, which typing the text into the pane ("herdr agent
// prompt") would not do.
//
// Both files are the project's own, committed like .claude/settings.json, and so
// they reach every checkout, soldier camps included. The extension is written to
// do nothing there (see the header of vexillum-wake.ts). Pi asks the general
// once, the first time it runs in the folder, whether to trust it: project
// extensions and settings do not load otherwise (see TrustState).
//
// The extension is a copy of the embedded one, so, like a skill, it has three
// states against what the binary carries: current, stale (untouched since vx
// wrote it, safe to refresh) and edited (the user's own changes, left alone
// unless upgrade is forced). The hash vx last wrote is recorded in
// .vexillum/config.json (scaffold.Config.Extensions).
package piwake

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
	"github.com/isaias-alt/vexillum/internal/scaffold"
	"github.com/isaias-alt/vexillum/internal/slot"
)

//go:embed vexillum-wake.ts
var extensionTemplate string

const (
	// ExtensionRel is the extension's path in a project, slash-separated:
	// the key under which its hash is recorded in config.json.
	ExtensionRel = ".pi/extensions/vexillum-wake.ts"

	// SettingsRel is Pi's project settings file.
	SettingsRel = ".pi/settings.json"

	// SkillsRef is the entry the settings need under "skills" for Pi to see
	// the skills in .claude/skills/. Pi reads it relative to the .pi/
	// directory the settings file is in.
	SkillsRef = "../.claude/skills"

	// BackupRel is where an edited extension is saved, relative to the
	// project's .vexillum/ directory, before it is overwritten.
	BackupRel = "backups/pi/vexillum-wake.ts"

	scriptPlaceholder = `"__VX_AWAIT_SCRIPT__"`
)

// Source is the extension as vx writes it: the embedded file with the await
// script filled in, which is the very script of the Claude Code Stop hook
// (scaffold.SentinelHookScript), so both find vx the same way.
func Source() []byte {
	var lit bytes.Buffer
	enc := json.NewEncoder(&lit)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(scaffold.SentinelHookScript); err != nil {
		panic(fmt.Sprintf("encoding the await script: %v", err)) // a string always encodes
	}
	return []byte(strings.Replace(extensionTemplate, scriptPlaceholder, strings.TrimSpace(lit.String()), 1))
}

// Hash is the content hash of Source, what config.json records.
func Hash() string { return hashOf(Source()) }

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// State is how the installed extension compares with Source.
type State int

const (
	// Missing: there is no extension file.
	Missing State = iota
	// Current: identical to Source.
	Current
	// Stale: untouched since vx wrote it (it matches the recorded hash) but
	// older than Source: safe to refresh.
	Stale
	// Edited: differs from Source and from what vx last wrote (or nothing was
	// recorded): the user's own changes.
	Edited
)

func (s State) String() string {
	switch s {
	case Missing:
		return "missing"
	case Current:
		return "current"
	case Stale:
		return "stale"
	case Edited:
		return "edited"
	}
	return fmt.Sprintf("State(%d)", int(s))
}

// Path is where the extension lives in projectDir.
func Path(projectDir string) string {
	return filepath.Join(projectDir, filepath.FromSlash(ExtensionRel))
}

// Inspect classifies the installed extension. recorded is the hash vx stored
// the last time it wrote the file ("" when unknown).
func Inspect(projectDir, recorded string) (State, error) {
	data, err := os.ReadFile(Path(projectDir))
	if errors.Is(err, fs.ErrNotExist) {
		return Missing, nil
	}
	if err != nil {
		return Missing, fmt.Errorf("reading %s: %w", ExtensionRel, err)
	}
	have := hashOf(data)
	switch {
	case have == Hash():
		return Current, nil
	case recorded != "" && recorded == have:
		return Stale, nil
	}
	return Edited, nil
}

// Install writes Source to the project, atomically, and returns its hash. It
// refuses to write through a symlink: a linked extension is the user's to
// manage.
func Install(projectDir string) (string, error) {
	p := Path(projectDir)
	if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%s is a symlink, refusing to write through it", ExtensionRel)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", filepath.Dir(p), err)
	}
	src := Source()
	if err := atomicfile.Write(p, src); err != nil {
		return "", fmt.Errorf("writing %s: %w", ExtensionRel, err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		return "", fmt.Errorf("setting mode on %s: %w", p, err)
	}
	return hashOf(src), nil
}

// Backup copies the installed extension to configDir/BackupRel, replacing an
// earlier backup, and returns the path. Call it before overwriting an edited one.
func Backup(projectDir, configDir string) (string, error) {
	data, err := os.ReadFile(Path(projectDir))
	if err != nil {
		return "", fmt.Errorf("reading %s for backup: %w", ExtensionRel, err)
	}
	dest := filepath.Join(configDir, filepath.FromSlash(BackupRel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if err := atomicfile.Write(dest, data); err != nil {
		return "", fmt.Errorf("backing up %s: %w", ExtensionRel, err)
	}
	return dest, nil
}

// SettingsPath is Pi's project settings file in projectDir.
func SettingsPath(projectDir string) string {
	return filepath.Join(projectDir, filepath.FromSlash(SettingsRel))
}

// referencesSkills reports whether skills, the "skills" setting, already lists
// SkillsRef (spelled with or without a trailing slash).
func referencesSkills(skills []any) bool {
	for _, e := range skills {
		if s, ok := e.(string); ok && path.Clean(s) == SkillsRef {
			return true
		}
	}
	return false
}

// SkillsReferenced reports whether the project's .pi/settings.json lists
// SkillsRef under "skills". A missing file is false, not an error; one that is
// not valid JSON, or whose "skills" is not a list, is an error.
func SkillsReferenced(projectDir string) (bool, error) {
	_, changed, err := mergeSettings(SettingsPath(projectDir))
	return !changed, err
}

// SettingsNeeded reports whether EnsureSettings would change the project's
// .pi/settings.json, without writing anything.
func SettingsNeeded(projectDir string) (bool, error) {
	_, changed, err := mergeSettings(SettingsPath(projectDir))
	return changed, err
}

// EnsureSettings adds SkillsRef to the "skills" list of the project's
// .pi/settings.json, creating the file when it is missing. Everything else in
// it is kept. A file that is not valid JSON (or has a "skills" that is not a
// list) is left untouched and reported as an error. Returns whether it wrote.
func EnsureSettings(projectDir string) (bool, error) {
	p := SettingsPath(projectDir)
	out, changed, err := mergeSettings(p)
	if err != nil || !changed {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return false, err
	}
	// settings.json is the user's own file: keep its mode and any symlink.
	if err := slot.WriteFile(p, string(out)); err != nil {
		return false, err
	}
	return true, nil
}

func mergeSettings(p string) (out []byte, changed bool, err error) {
	settings := map[string]any{}
	data, readErr := os.ReadFile(p)
	switch {
	case readErr == nil:
		if jsonErr := json.Unmarshal(data, &settings); jsonErr != nil {
			return nil, false, fmt.Errorf("%s has invalid JSON, leaving it untouched: %w", SettingsRel, jsonErr)
		}
		if settings == nil {
			return nil, false, fmt.Errorf("%s is not a JSON object, leaving it untouched", SettingsRel)
		}
	case errors.Is(readErr, fs.ErrNotExist):
	default:
		return nil, false, readErr
	}

	var skills []any
	if cur, ok := settings["skills"]; ok {
		if skills, ok = cur.([]any); !ok {
			return nil, false, fmt.Errorf("%s has a \"skills\" that is not a list, leaving it untouched", SettingsRel)
		}
	}
	if referencesSkills(skills) {
		return nil, false, nil
	}
	settings["skills"] = append(skills, SkillsRef)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // keep what the user wrote readable: no \u0026
	enc.SetIndent("", "  ")
	if err := enc.Encode(settings); err != nil {
		return nil, false, err
	}
	return buf.Bytes(), true, nil
}
