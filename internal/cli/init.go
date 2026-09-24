package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/scaffold"
)

const initUsage = `Prepare the current project to be orchestrated by vexillum.

Usage:
  vexillum init [--global]

--global scaffolds the commander rules once for every project on this
machine (~/.claude/rules/vexillum.md) instead of the current project. Opt-in
only - it makes the commander persona apply to every Claude Code session on
this machine, not just vexillum projects. Without it, init only ever touches
the current project.
`

type localConfig struct {
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

// readLocalConfig reads config.json directly inside configDir - the
// project's .vexillum/ for local scaffolds, or vexillumHome itself for the
// global scaffold (see runInitGlobal).
func readLocalConfig(configDir string) (localConfig, error) {
	data, err := os.ReadFile(filepath.Join(configDir, "config.json"))
	if err != nil {
		return localConfig{}, err
	}
	var cfg localConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return localConfig{}, err
	}
	return cfg, nil
}

// recordScaffoldHash updates the stored content hash for
// .claude/rules/vexillum.md in configDir/config.json, preserving the rest
// of the config.
func recordScaffoldHash(configDir string, updated bool) error {
	if !updated {
		return nil
	}
	cfg, err := readLocalConfig(configDir)
	if err != nil {
		return err
	}
	cfg.VexillumRuleHash = hashContent(scaffold.VexillumCommanderRules)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(configDir, "config.json"), data, 0o644)
}

// Init runs the "vexillum init" command.
func Init(args []string) int {
	global := false
	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Print(initUsage)
			return 0
		case "--global":
			global = true
		default:
			fmt.Fprintf(os.Stderr, "vexillum: unknown init flag %q\n", a)
			fmt.Fprint(os.Stderr, initUsage)
			return 1
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "vexillum: cannot determine home directory: %v\n", err)
		return 1
	}
	vexillumHome := filepath.Join(home, ".vexillum")

	if global {
		return runInitGlobal(vexillumHome, home, os.Stdout, os.Stderr)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "vexillum: cannot determine current directory: %v\n", err)
		return 1
	}

	return runInit(cwd, vexillumHome, os.Stdout, os.Stderr)
}

func runInit(projectDir, vexillumHome string, stdout, stderr io.Writer) int {
	if err := refuseInsideVexillumHome(projectDir, vexillumHome); err != nil {
		fmt.Fprintln(stderr, "vexillum:", err)
		return 1
	}

	if !isGitRepo(projectDir) {
		fmt.Fprintln(stderr, "vexillum: current directory is not a git repository")
		fmt.Fprintln(stderr, "vexillum requires git; run 'git init' first.")
		return 1
	}

	created, err := ensureDir(vexillumHome)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot create %s: %v\n", vexillumHome, err)
		return 1
	}
	if created {
		fmt.Fprintf(stdout, "Created %s\n", vexillumHome)
	}

	configDir := filepath.Join(projectDir, ".vexillum")
	alreadyInitialized := projectAlreadyInitialized(projectDir)
	if !alreadyInitialized {
		if err := writeLocalConfig(configDir); err != nil {
			fmt.Fprintf(stderr, "vexillum: cannot write .vexillum/config.json: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "Created .vexillum/config.json")
	}

	ruleDir := filepath.Join(projectDir, ".claude", "rules")
	if err := os.MkdirAll(ruleDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot create %s: %v\n", ruleDir, err)
		return 1
	}
	ruleCreated, err := writeFileIfMissing(ruleDir, "vexillum.md", scaffold.VexillumCommanderRules)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot write .claude/rules/vexillum.md: %v\n", err)
		return 1
	}

	// .claude/settings.json isn't vexillum's file - it's the user's own
	// Claude Code config. Merge our Stop hook in carefully; a malformed
	// existing file is a pre-existing problem, warn but don't fail init
	// over it.
	hookAdded, hookErr := ensureSentinelHook(projectDir)
	if hookErr != nil {
		fmt.Fprintf(stderr, "vexillum: warning: could not add the sentinel Stop hook: %v\n", hookErr)
	} else if hookAdded {
		fmt.Fprintln(stdout, "Added vexillum sentinel Stop hook to .claude/settings.json")
	}

	if alreadyInitialized {
		if !ruleCreated && !hookAdded {
			fmt.Fprintln(stdout, "Project already initialized (found .vexillum/config.json). Nothing to do.")
			return 0
		}
		// Healing an older init that predates one of these: the scaffold
		// itself isn't new, but restore what's missing.
		if ruleCreated {
			fmt.Fprintln(stdout, "Created missing .claude/rules/vexillum.md")
			if err := recordScaffoldHash(configDir, ruleCreated); err != nil {
				fmt.Fprintf(stderr, "vexillum: warning: could not record scaffold hash: %v\n", err)
			}
		}
		fmt.Fprintln(stdout, "Project already initialized (found .vexillum/config.json); restored the missing piece(s) above.")
		return 0
	}

	if ruleCreated {
		fmt.Fprintln(stdout, "Created .claude/rules/vexillum.md")
	} else {
		fmt.Fprintln(stdout, ".claude/rules/vexillum.md already exists, left untouched")
	}
	if err := recordScaffoldHash(configDir, ruleCreated); err != nil {
		fmt.Fprintf(stderr, "vexillum: warning: could not record scaffold hash: %v\n", err)
	}

	fmt.Fprintln(stdout, "vexillum initialized.")
	return 0
}

// runInitGlobal scaffolds the commander rules once for every project on
// this machine (~/.claude/rules/vexillum.md) instead of the current
// project - opt-in via 'vexillum init --global'. Applies to every Claude
// Code session on this machine, not just vexillum projects; see initUsage
// for why this isn't the default.
func runInitGlobal(vexillumHome, home string, stdout, stderr io.Writer) int {
	created, err := ensureDir(vexillumHome)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot create %s: %v\n", vexillumHome, err)
		return 1
	}
	if created {
		fmt.Fprintf(stdout, "Created %s\n", vexillumHome)
	}

	alreadyInitialized := globalAlreadyInitialized(vexillumHome)
	if !alreadyInitialized {
		if err := writeLocalConfig(vexillumHome); err != nil {
			fmt.Fprintf(stderr, "vexillum: cannot write %s: %v\n", filepath.Join(vexillumHome, "config.json"), err)
			return 1
		}
		fmt.Fprintf(stdout, "Created %s\n", filepath.Join(vexillumHome, "config.json"))
	}

	ruleDir := filepath.Join(home, ".claude", "rules")
	if err := os.MkdirAll(ruleDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot create %s: %v\n", ruleDir, err)
		return 1
	}
	ruleCreated, err := writeFileIfMissing(ruleDir, "vexillum.md", scaffold.VexillumCommanderRules)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot write %s: %v\n", filepath.Join(ruleDir, "vexillum.md"), err)
		return 1
	}

	if alreadyInitialized {
		if !ruleCreated {
			fmt.Fprintln(stdout, "Already initialized globally (found "+filepath.Join(vexillumHome, "config.json")+"). Nothing to do.")
			return 0
		}
		fmt.Fprintln(stdout, "Created missing ~/.claude/rules/vexillum.md")
		if err := recordScaffoldHash(vexillumHome, ruleCreated); err != nil {
			fmt.Fprintf(stderr, "vexillum: warning: could not record scaffold hash: %v\n", err)
		}
		fmt.Fprintln(stdout, "Already initialized globally; restored the missing piece above.")
		return 0
	}

	if ruleCreated {
		fmt.Fprintln(stdout, "Created ~/.claude/rules/vexillum.md")
	} else {
		fmt.Fprintln(stdout, "~/.claude/rules/vexillum.md already exists, left untouched")
	}
	if err := recordScaffoldHash(vexillumHome, ruleCreated); err != nil {
		fmt.Fprintf(stderr, "vexillum: warning: could not record scaffold hash: %v\n", err)
	}

	fmt.Fprintln(stdout, "vexillum initialized globally - this applies to every Claude Code session on this machine.")
	return 0
}

func globalAlreadyInitialized(vexillumHome string) bool {
	_, err := os.Stat(filepath.Join(vexillumHome, "config.json"))
	return err == nil
}

const (
	// sentinelHookCommand is registered as an async Stop hook
	// ("asyncRewake": true, a generous "timeout"): it blocks, polling
	// for a wake, until one appears or the timeout elapses - so the
	// commander gets woken even if its turn already ended before a
	// soldier settled, not just when a wake happens to already be
	// pending at the exact moment a turn is ending. Verified live: a
	// real asyncRewake Stop hook that sleeps then exits 2 with a stderr
	// message woke an idle Claude Code session on its own, no new user
	// prompt sent - "Stop hook feedback" arrived automatically.
	//
	// This gets written into .claude/settings.json in the project
	// directory, which git tracks - so it reaches every checkout,
	// including a worktree an unrelated tool creates for its own
	// headless Claude Code turn (e.g. no-mistakes' review/test/lint
	// steps). runSentinelAwaitGuarded (see cli/sentinel.go) is what
	// keeps that turn from sitting blocked on a wake the sentinel can
	// never produce for it: it only actually waits inside a
	// herdr-managed pane (HERDR_WORKSPACE_ID set), same as dispatch and
	// redispatch already require of their own caller.
	sentinelHookCommand = "vexillum sentinel await"

	// legacySentinelHookCommand is the older, synchronous-only hook
	// (an instant check-and-return, registered without asyncRewake)
	// this replaces. ensureSentinelHook detects and upgrades it in
	// place instead of leaving a stale, redundant hook alongside the
	// new one.
	legacySentinelHookCommand = "vexillum sentinel drain"

	// sentinelHookTimeoutSeconds bounds how long a single async hook
	// invocation may block. runSentinelAwait's own internal deadline
	// (sentinelAwaitMaxWait) stays comfortably under this so it always
	// exits 0 cleanly on its own before Claude Code would have to kill
	// it - Claude Code re-fires this hook on every turn end regardless,
	// so a shorter self-imposed deadline costs nothing.
	sentinelHookTimeoutSeconds = 3600
)

// ensureSentinelHook merges the async sentinel Stop hook into the
// project's .claude/settings.json, so a soldier's status change surfaces
// to the commander instead of it quietly ending its turn - even if that
// turn already ended before anything settled. Reads and merges rather
// than overwriting - existing hooks and settings are preserved untouched.
// An older project's synchronous-only hook (legacySentinelHookCommand)
// is upgraded in place, not duplicated. A malformed existing file is
// left untouched and reported as an error rather than risk corrupting it.
func ensureSentinelHook(projectDir string) (added bool, err error) {
	path := filepath.Join(projectDir, ".claude", "settings.json")

	settings := map[string]any{}
	data, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		if jsonErr := json.Unmarshal(data, &settings); jsonErr != nil {
			return false, fmt.Errorf("%s has invalid JSON, leaving it untouched: %w", path, jsonErr)
		}
	case os.IsNotExist(readErr):
		// settings stays the empty map created above.
	default:
		return false, readErr
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	stopGroups, _ := hooks["Stop"].([]any)

	newEntry := map[string]any{
		"type":        "command",
		"command":     sentinelHookCommand,
		"asyncRewake": true,
		"timeout":     sentinelHookTimeoutSeconds,
	}

	found := false
	changed := false
	for _, g := range stopGroups {
		group, ok := g.(map[string]any)
		if !ok {
			continue
		}
		entries, _ := group["hooks"].([]any)
		for i, e := range entries {
			entry, ok := e.(map[string]any)
			if !ok {
				continue
			}
			switch cmd, _ := entry["command"].(string); cmd {
			case sentinelHookCommand:
				found = true
				asyncOK, _ := entry["asyncRewake"].(bool)
				if !asyncOK || entry["timeout"] == nil {
					entries[i] = newEntry
					group["hooks"] = entries
					changed = true
				}
			case legacySentinelHookCommand:
				entries[i] = newEntry
				group["hooks"] = entries
				found = true
				changed = true
			}
		}
	}

	if !found {
		stopGroups = append(stopGroups, map[string]any{
			"hooks": []any{newEntry},
		})
		hooks["Stop"] = stopGroups
		settings["hooks"] = hooks
		changed = true
	}

	if !changed {
		return false, nil
	}

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return false, err
	}
	out = append(out, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// ensureDir creates path if it doesn't exist. Returns whether it created it.
func ensureDir(path string) (created bool, err error) {
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

func projectAlreadyInitialized(projectDir string) bool {
	_, err := os.Stat(filepath.Join(projectDir, ".vexillum", "config.json"))
	return err == nil
}

// writeLocalConfig creates a fresh config.json directly inside configDir -
// the project's .vexillum/ for local scaffolds, or vexillumHome itself for
// the global scaffold (see runInitGlobal).
func writeLocalConfig(configDir string) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}

	cfg := localConfig{Version: 1, InitializedAt: time.Now().UTC()}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	return os.WriteFile(filepath.Join(configDir, "config.json"), data, 0o644)
}

func writeFileIfMissing(projectDir, name, content string) (created bool, err error) {
	path := filepath.Join(projectDir, name)

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
