// Package scaffold manages the files vexillum writes into a project (or,
// with --global, into the user's home directory) so Claude Code picks up
// the commander persona and the sentinel Stop hook: .vexillum/config.json
// (or vexillumHome/config.json for the global scaffold),
// .claude/rules/vexillum.md, and the hook entry inside
// .claude/settings.json (see hook.go). "vexillum init" writes these
// fresh; "vexillum upgrade" refreshes them to the latest template without
// clobbering local edits it can't vouch for.
package scaffold

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

//go:generate cp vexillum-commander-rules.md ../../.claude/rules/vexillum.md

// VexillumCommanderRules is the "Vexillum commander rules" product
// scaffold that `vexillum init` writes to .claude/rules/vexillum.md in a
// scaffolded project. It is also this repository's own dogfooded copy at
// .claude/rules/vexillum.md - re-run `go generate ./...` after editing
// this file to keep that copy in sync.
//
//go:embed vexillum-commander-rules.md
var VexillumCommanderRules string

// Config is the schema of .vexillum/config.json (or, for the global
// scaffold, vexillumHome/config.json directly).
type Config struct {
	Version       int       `json:"version"`
	InitializedAt time.Time `json:"initialized_at"`
	// VexillumRuleHash is the sha256 hex digest of the content vexillum
	// itself last wrote to .claude/rules/vexillum.md. 'vexillum upgrade'
	// compares the file's current content against this to tell "still
	// exactly what we wrote" (safe to refresh to the latest template) apart
	// from "the general edited this" (leave it alone). Empty means unknown
	// provenance (predates this tracking, or never written by vexillum).
	VexillumRuleHash string `json:"vexillum_rule_hash,omitempty"`
}

func hashContent(s string) string {
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

// RecordHash updates the stored content hash for .claude/rules/vexillum.md
// in configDir/config.json, preserving the rest of the config.
func RecordHash(configDir string, updated bool) error {
	if !updated {
		return nil
	}
	cfg, err := ReadConfig(configDir)
	if err != nil {
		return err
	}
	cfg.VexillumRuleHash = hashContent(VexillumCommanderRules)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(configDir, "config.json"), data, 0o644)
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
// scaffolded by 'vexillum init' (i.e. has a .vexillum/config.json).
func ProjectInitialized(projectDir string) bool {
	_, err := os.Stat(filepath.Join(projectDir, ".vexillum", "config.json"))
	return err == nil
}

// GlobalInitialized reports whether the global scaffold
// (vexillumHome/config.json, written by 'vexillum init --global') exists.
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

	cfg := Config{Version: 1, InitializedAt: time.Now().UTC()}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	return os.WriteFile(filepath.Join(configDir, "config.json"), data, 0o644)
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

// FileResult is the outcome of bringing a single scaffold file up to date.
type FileResult struct {
	Changed bool
	Status  string
}

// UpgradeFile brings a single scaffold file up to date with the given
// latest template content:
//   - missing entirely: created fresh.
//   - already matches latest: nothing to do.
//   - matches the hash vexillum stored when it last wrote this file: safe
//     to refresh, since nothing has touched it since.
//   - anything else (no stored hash, or content diverged from that hash):
//     the general may have edited it - left untouched and reported
//     instead, unless force is set, which overwrites regardless.
func UpgradeFile(dir, name, latest, storedHash string, force bool) (FileResult, error) {
	path := filepath.Join(dir, name)

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if writeErr := os.WriteFile(path, []byte(latest), 0o644); writeErr != nil {
			return FileResult{}, writeErr
		}
		return FileResult{Changed: true, Status: "created (was missing)"}, nil
	}
	if err != nil {
		return FileResult{}, err
	}

	current := string(data)
	if current == latest {
		return FileResult{Status: "already up to date"}, nil
	}

	safeToRefresh := storedHash != "" && storedHash == hashContent(current)
	if !safeToRefresh && !force {
		return FileResult{Status: "has local changes, left untouched (compare manually, or rerun with --force to overwrite)"}, nil
	}

	if err := os.WriteFile(path, []byte(latest), 0o644); err != nil {
		return FileResult{}, err
	}
	status := "upgraded to the latest template"
	if !safeToRefresh {
		status = "force-upgraded to the latest template (local changes discarded)"
	}
	return FileResult{Changed: true, Status: status}, nil
}
