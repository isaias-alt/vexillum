// Package opencodeosc is vexillum's side of the status reporter an OpenCode 2.x
// soldier gets under the hc (herdr-core) backend.
//
// herdr-core reads an agent's status from the OSC 7501 sequence the program
// prints on its terminal, and OpenCode prints none. A soldier's TUI is
// therefore launched with a small TUI plugin of vexillum's own (tui.js,
// embedded here) that watches the pane's own OpenCode session and reports it
// as idle, working, blocked (permission or question), done or error, so hc get,
// wait and send --wait work for OpenCode as they do for Claude Code and Pi.
//
// The plugin is written under the project's state directory, outside the camp
// and outside ~/.config/opencode, and registered through the TUI's own
// environment (OPENCODE_CLI_CONFIG_CONTENT, see ConfigContent): nothing is
// added to the worktree or to the user's OpenCode config, and the variable
// reaches only that soldier's TUI, not the shared background service or any
// other TUI. It replaces the TUI plugins the user's own cli.json lists, for
// that soldier only. Without HC_TERMINAL_ID in its environment the plugin
// prints nothing.
//
// Only OpenCode 2.x is covered: V1 has a different plugin API.
package opencodeosc

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
)

//go:embed tui.js
var pluginSource []byte

const (
	// EnvVar is the OpenCode TUI variable that carries a config layer for
	// the plugins list. Not OPENCODE_CONFIG_CONTENT: that one is read by the
	// shared service when it starts, and a layer it picks up leaks into every
	// other TUI.
	EnvVar = "OPENCODE_CLI_CONFIG_CONTENT"

	pluginDirName = "opencode"
	pluginSubDir  = "osc7501"
	pluginFile    = "tui.js"
)

// Dir is the directory the plugin lives in for a project:
// <project root>/opencode/osc7501. OpenCode 2.x resolves tui.js inside a
// plugin directory.
func Dir(projectRoot string) string {
	return filepath.Join(projectRoot, pluginDirName, pluginSubDir)
}

// Path is the plugin file inside Dir.
func Path(projectRoot string) string {
	return filepath.Join(Dir(projectRoot), pluginFile)
}

// Install writes the embedded plugin under projectRoot, atomically, and
// returns the plugin directory. A copy that is already there with the same
// content is left alone, so a soldier that is running keeps the file it loaded.
func Install(projectRoot string) (string, error) {
	dir := Dir(projectRoot)
	path := Path(projectRoot)
	if cur, err := os.ReadFile(path); err == nil && bytes.Equal(cur, pluginSource) {
		return dir, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	if err := atomicfile.WriteMode(path, pluginSource, 0o600); err != nil {
		return "", fmt.Errorf("installing the opencode status plugin: %w", err)
	}
	return dir, nil
}

// ConfigContent is the value of EnvVar that registers the plugin directory dir
// as the TUI's only plugin.
func ConfigContent(dir string) string {
	// A map of strings always marshals, so there is no error to handle.
	out, _ := json.Marshal(map[string][]string{"plugins": {dir}})
	return string(out)
}

// Source returns the embedded plugin, for tests and docs.
func Source() []byte {
	return append([]byte{}, pluginSource...)
}

// State is how the installed plugin compares with the embedded one.
type State int

const (
	// Missing: there is no plugin file.
	Missing State = iota
	// Current: identical to the embedded plugin.
	Current
	// Stale: there, but different from the embedded plugin (an older vx
	// wrote it, or somebody edited it). The next dispatch rewrites it.
	Stale
)

func (s State) String() string {
	switch s {
	case Missing:
		return "missing"
	case Current:
		return "current"
	case Stale:
		return "stale"
	}
	return fmt.Sprintf("State(%d)", int(s))
}

// Inspect reports the installed plugin's State under projectRoot.
func Inspect(projectRoot string) (State, error) {
	cur, err := os.ReadFile(Path(projectRoot))
	switch {
	case os.IsNotExist(err):
		return Missing, nil
	case err != nil:
		return Missing, fmt.Errorf("reading %s: %w", Path(projectRoot), err)
	case bytes.Equal(cur, pluginSource):
		return Current, nil
	}
	return Stale, nil
}
