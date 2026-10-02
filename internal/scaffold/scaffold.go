// Package scaffold manages the files vexillum writes into a project (or,
// with --global, into the user's home directory) so Claude Code picks up
// the commander persona and the sentinel Stop hook: .vexillum/config.json
// (or vexillumHome/config.json for the global scaffold) and the hook entry
// inside .claude/settings.json (see hook.go). The commander rules themselves
// live in AGENTS.md and the skills (internal/slot, internal/install); this
// package only keeps the bookkeeping around them.
package scaffold

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
)

// Config is the schema of .vexillum/config.json (or, for the global
// scaffold, vexillumHome/config.json directly).
type Config struct {
	Version       int       `json:"version"`
	InitializedAt time.Time `json:"initialized_at"`
	// VexillumRuleHash is the sha256 hex digest of the content vexillum
	// itself last wrote to a .claude/rules/vexillum.md file. Projects no
	// longer get that file (the rules moved into AGENTS.md and the
	// skills), so for them it only marks an old scaffold: 'vx upgrade'
	// removes the file when it still hashes to this, and leaves an edited
	// one alone. The global scaffold still writes that file and keeps its
	// hash here. Empty means unknown provenance.
	VexillumRuleHash string `json:"vexillum_rule_hash,omitempty"`
	// Skills maps a first-party skill name to the content hash (see
	// skills.HashFiles) of what vexillum last installed under
	// .claude/skills/<name>/. 'vx upgrade' and 'vx doctor' compare the
	// installed files against it to tell "untouched, safe to refresh"
	// from "edited by the user".
	Skills map[string]string `json:"skills,omitempty"`
}

// HashContent returns the sha256 hex digest of s, the format used by
// Config.VexillumRuleHash.
func HashContent(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ReadConfig reads config.json directly inside configDir - the project's
// .vexillum/ for local scaffolds, or vexillumHome itself for the global
// scaffold (see WriteConfig).
func ReadConfig(configDir string) (Config, error) {
	data, err := os.ReadFile(filepath.Join(configDir, "config.json"))
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// SaveConfig atomically writes cfg as configDir/config.json.
func SaveConfig(configDir string, cfg Config) error {
	return atomicfile.WriteJSON(filepath.Join(configDir, "config.json"), cfg)
}

// EnsureDir creates path if it doesn't exist. Returns whether it created it.
func EnsureDir(path string) (created bool, err error) {
	if _, statErr := os.Stat(path); statErr == nil {
		return false, nil
	} else if !os.IsNotExist(statErr) {
		return false, statErr
	}

	if err := os.MkdirAll(path, 0o755); err != nil {
		return false, err
	}
	return true, nil
}

// ProjectInitialized reports whether projectDir has already been
// scaffolded by 'vx init' (i.e. has a .vexillum/config.json).
func ProjectInitialized(projectDir string) bool {
	_, err := os.Stat(filepath.Join(projectDir, ".vexillum", "config.json"))
	return err == nil
}

// GlobalInitialized reports whether the global scaffold
// (vexillumHome/config.json, written by 'vx init --global') exists.
func GlobalInitialized(vexillumHome string) bool {
	_, err := os.Stat(filepath.Join(vexillumHome, "config.json"))
	return err == nil
}

// WriteConfig creates a fresh config.json directly inside configDir - the
// project's .vexillum/ for local scaffolds, or vexillumHome itself for the
// global scaffold.
func WriteConfig(configDir string) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}

	return SaveConfig(configDir, Config{Version: 1, InitializedAt: time.Now().UTC()})
}

// WriteFileIfMissing writes content to dir/name unless it already exists.
// Returns whether it created it.
func WriteFileIfMissing(dir, name, content string) (created bool, err error) {
	path := filepath.Join(dir, name)

	if _, statErr := os.Stat(path); statErr == nil {
		return false, nil
	} else if !os.IsNotExist(statErr) {
		return false, statErr
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// IsGitRepo reports whether dir is inside a git working tree.
func IsGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// forumIgnore is the content of .vexillum/.gitignore: forum artifacts (the
// HTML pages an agent writes for human review) are scratch, while the rest of
// .vexillum/ (config.json) is committed.
const forumIgnore = "forum/\n"

// EnsureForumIgnore makes sure .vexillum/forum/ is git-ignored by writing
// configDir/.gitignore when it is missing. An existing file is left alone,
// whatever it contains. Returns whether it created the file.
func EnsureForumIgnore(configDir string) (created bool, err error) {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return false, err
	}
	return WriteFileIfMissing(configDir, ".gitignore", forumIgnore)
}
