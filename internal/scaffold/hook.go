package scaffold

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/slot"
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
	// including a camp's own headless Claude Code turn (e.g.
	// internal/tribunal's review step, run directly via os/exec, not a
	// herdr pane). runSentinelAwaitGuarded (see cli/sentinel.go) is what
	// keeps that turn from sitting blocked on a wake the sentinel can
	// never produce for it: it only actually waits inside a
	// herdr-managed pane (HERDR_WORKSPACE_ID set), same as dispatch and
	// redispatch already require of their own caller.
	//
	// The command does not depend on the PATH of the shell Claude Code
	// runs hooks in: that shell may not have the directory vx was
	// installed to (a bare "vx sentinel await" died with "command not
	// found" and the commander was never woken, silently). It is a
	// one-line POSIX sh wrapper that tries "command -v vx" and then the
	// known install locations, so it needs no extra file in the project
	// and no absolute path baked in (settings.json is committed and
	// shared across machines). When vx cannot be found at all it prints
	// SentinelHookNotFoundMessage to stderr and exits 0, so the failure
	// is visible without blocking Claude Code. exec keeps the await
	// process a direct child of the hook runner, which its owner tracking
	// (see internal/sentinel) relies on.
	SentinelHookCommand = `sh -c 'for v in "$(command -v ` + cmdname.Name + ` 2>/dev/null)" ` +
		`"$HOME/.local/bin/` + cmdname.Name + `" /opt/homebrew/bin/` + cmdname.Name + ` ` +
		`/usr/local/bin/` + cmdname.Name + ` /home/linuxbrew/.linuxbrew/bin/` + cmdname.Name + `; do ` +
		`[ -f "$v" ] && [ -x "$v" ] && exec "$v" sentinel await; done; ` +
		`echo "` + SentinelHookNotFoundMessage + `" >&2; exit 0'`

	// SentinelHookNotFoundMessage is the line SentinelHookCommand prints to
	// stderr when no vx binary can be found. It must stay free of double
	// quotes, single quotes, backticks and dollar signs, since it is
	// embedded in the shell wrapper above.
	SentinelHookNotFoundMessage = cmdname.Name + ": not found, so the sentinel Stop hook cannot wake the commander. " +
		"Install " + cmdname.Name + " (brew or curl) or put its directory on PATH, then run: " + cmdname.Name + " doctor"

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
// (LegacySentinelHookCommand), its hook registered under the old
// executable name (LegacyRenamedSentinelHookCommand) and the bare
// PATH-dependent one (BareSentinelHookCommand) are upgraded in place, not
// duplicated; hooks that are not vexillum's are left as they are. A
// malformed existing file is left untouched and reported as an error
// rather than risk corrupting it.
func EnsureSentinelHook(projectDir string) (added bool, err error) {
	path := filepath.Join(projectDir, ".claude", "settings.json")
	out, changed, err := mergeSentinelHook(path)
	if err != nil || !changed {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	// settings.json is the user's own file: write it atomically, keeping
	// its permissions and any symlink it is behind.
	if err := slot.WriteFile(path, string(out)); err != nil {
		return false, err
	}
	return true, nil
}

// SentinelHookNeeded reports whether EnsureSentinelHook would change
// projectDir's .claude/settings.json, without writing anything. A malformed
// settings file is an error, as it is for EnsureSentinelHook.
func SentinelHookNeeded(projectDir string) (bool, error) {
	_, changed, err := mergeSentinelHook(filepath.Join(projectDir, ".claude", "settings.json"))
	return changed, err
}

// mergeSentinelHook computes the settings file content with the sentinel
// hook merged in and whether that differs from what is on disk.
func mergeSentinelHook(path string) (out []byte, changed bool, err error) {

	settings := map[string]any{}
	data, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		if jsonErr := json.Unmarshal(data, &settings); jsonErr != nil {
			return nil, false, fmt.Errorf("%s has invalid JSON, leaving it untouched: %w", path, jsonErr)
		}
	case os.IsNotExist(readErr):
		// settings stays the empty map created above.
	default:
		return nil, false, readErr
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
		return nil, false, nil
	}

	// The hook command holds shell characters (">&", "&&") that the
	// default encoder would write as \u003e and \u0026.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(settings); err != nil {
		return nil, false, err
	}
	return buf.Bytes(), true, nil
}

// SentinelHookState is what a project's .claude/settings.json holds for
// the sentinel Stop hook.
type SentinelHookState struct {
	// Present is whether a Stop hook of vexillum's is registered.
	Present bool
	// Command is that hook's command line as registered.
	Command string
	// Current is whether it is exactly what EnsureSentinelHook would
	// write now.
	Current bool
}

// InspectSentinelHook reads projectDir's .claude/settings.json and reports
// the sentinel Stop hook in it, without writing anything. A missing file
// is simply no hook; a malformed one is an error.
func InspectSentinelHook(projectDir string) (SentinelHookState, error) {
	path := filepath.Join(projectDir, ".claude", "settings.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return SentinelHookState{}, nil
	}
	if err != nil {
		return SentinelHookState{}, err
	}
	var settings struct {
		Hooks struct {
			Stop []struct {
				Hooks []struct {
					Command     string `json:"command"`
					AsyncRewake bool   `json:"asyncRewake"`
					Timeout     any    `json:"timeout"`
				} `json:"hooks"`
			} `json:"Stop"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return SentinelHookState{}, fmt.Errorf("%s has invalid JSON: %w", path, err)
	}

	for _, group := range settings.Hooks.Stop {
		for _, h := range group.Hooks {
			switch h.Command {
			case SentinelHookCommand:
				return SentinelHookState{Present: true, Command: h.Command, Current: h.AsyncRewake && h.Timeout != nil}, nil
			}
		}
	}
	return SentinelHookState{}, nil
}
