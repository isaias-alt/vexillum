package scaffold

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// SentinelHookCommand is registered as an async Stop hook
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
	SentinelHookCommand = "vexillum sentinel await"

	// LegacySentinelHookCommand is the older, synchronous-only hook
	// (an instant check-and-return, registered without asyncRewake)
	// this replaces. EnsureSentinelHook detects and upgrades it in
	// place instead of leaving a stale, redundant hook alongside the
	// new one.
	LegacySentinelHookCommand = "vexillum sentinel drain"

	// SentinelHookTimeoutSeconds bounds how long a single async hook
	// invocation may block. runSentinelAwait's own internal deadline
	// (sentinelAwaitMaxWait) stays comfortably under this so it always
	// exits 0 cleanly on its own before Claude Code would have to kill
	// it - Claude Code re-fires this hook on every turn end regardless,
	// so a shorter self-imposed deadline costs nothing.
	SentinelHookTimeoutSeconds = 3600
)

// EnsureSentinelHook merges the async sentinel Stop hook into the
// project's .claude/settings.json, so a soldier's status change surfaces
// to the commander instead of it quietly ending its turn - even if that
// turn already ended before anything settled. Reads and merges rather
// than overwriting - existing hooks and settings are preserved untouched.
// An older project's synchronous-only hook (LegacySentinelHookCommand)
// is upgraded in place, not duplicated. A malformed existing file is
// left untouched and reported as an error rather than risk corrupting it.
func EnsureSentinelHook(projectDir string) (added bool, err error) {
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
		"command":     SentinelHookCommand,
		"asyncRewake": true,
		"timeout":     SentinelHookTimeoutSeconds,
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
			case SentinelHookCommand:
				found = true
				asyncOK, _ := entry["asyncRewake"].(bool)
				if !asyncOK || entry["timeout"] == nil {
					entries[i] = newEntry
					group["hooks"] = entries
					changed = true
				}
			case LegacySentinelHookCommand:
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
