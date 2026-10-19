package doctorcheck

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/piwake"
	"github.com/isaias-alt/vexillum/internal/scaffold"
)

const (
	piWakeName     = "Pi wake extension"
	piSkillsName   = "Pi skills reference"
	piTrustName    = "Pi project trust"
	piResolvesName = "Pi wake finds " + cmdname.Name
)

// PiWake reports on what vexillum puts in a project for a Pi commander, the
// counterpart of StopHooks for Claude Code: whether the wake extension is in
// .pi/extensions/ and equal to the copy this binary carries, whether
// .pi/settings.json lets Pi see the skills in .claude/skills/, whether Pi has
// been told to trust the folder (without which neither loads), and whether the
// extension's await finds a vx binary from the environment doctor runs in, which
// is the one Pi is started from. Everything is read-only, and nothing is
// Required: a missing piece is something to fix with init or upgrade.
func PiWake(projectDir, homeDir string) []Result {
	cfg, _ := scaffold.ReadConfig(filepath.Join(projectDir, ".vexillum"))
	fix := "run '" + cmdname.Name + " upgrade'"

	var out []Result
	state, err := piwake.Inspect(projectDir, cfg.Extensions[piwake.ExtensionRel])
	switch {
	case err != nil:
		out = append(out, Result{Name: piWakeName, Warn: true, Detail: err.Error()})
	case state == piwake.Missing:
		out = append(out, Result{Name: piWakeName, Warn: true, Detail: "not installed (" + piwake.ExtensionRel + "), so the commander is never woken when a soldier finishes; run '" + cmdname.Name + " init'"})
	case state == piwake.Stale:
		out = append(out, Result{Name: piWakeName, Warn: true, Detail: "installed, stale, older than this binary's copy; " + fix})
	case state == piwake.Edited:
		out = append(out, Result{Name: piWakeName, Warn: true, Detail: "installed, edited by hand; '" + cmdname.Name + " upgrade --force' replaces it (after a backup)"})
	default:
		out = append(out, Result{Name: piWakeName, OK: true, Detail: "installed, current (" + piwake.ExtensionRel + ")"})
	}

	switch ok, err := piwake.SkillsReferenced(projectDir); {
	case err != nil:
		out = append(out, Result{Name: piSkillsName, Warn: true, Detail: err.Error()})
	case !ok:
		out = append(out, Result{Name: piSkillsName, Warn: true, Detail: piwake.SettingsRel + " does not list " + piwake.SkillsRef + ", so Pi does not see the vexillum skills; run '" + cmdname.Name + " init'"})
	default:
		out = append(out, Result{Name: piSkillsName, OK: true, Detail: piwake.SettingsRel + " lists " + piwake.SkillsRef})
	}

	out = append(out, PiTrust(projectDir, homeDir))
	out = append(out, probeAwait(piResolvesName, scaffold.SentinelHookScript, probeEnv(homeDir), "the environment doctor runs in", "",
		"install "+cmdname.Name+" where the extension looks (brew install, or the curl installer, which puts it in ~/.local/bin) or put its directory on PATH before starting pi"))
	return out
}

// probeEnv is the environment doctor runs in, minus what marks a herdr pane (a
// vx that is found must not wait for a wake in the probe) and with HOME as
// given.
func probeEnv(homeDir string) []string {
	env := []string{"HOME=" + homeDir}
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k == "HOME" || strings.HasPrefix(k, "HERDR_") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// piAgentDir is Pi's own state directory: PI_CODING_AGENT_DIR, else
// ~/.pi/agent.
func piAgentDir(homeDir string) string {
	dir := os.Getenv("PI_CODING_AGENT_DIR")
	switch {
	case dir == "":
		return filepath.Join(homeDir, ".pi", "agent")
	case dir == "~":
		return homeDir
	case strings.HasPrefix(dir, "~/"):
		return filepath.Join(homeDir, dir[2:])
	}
	return dir
}

// PiTrust reports whether Pi has been told to trust the project folder. A
// project's .pi/extensions and .pi/settings.json only load once it is trusted:
// Pi asks the first time it runs there ("Trust project folder?") and remembers
// the answer in <agent dir>/trust.json, keyed by the canonical folder path,
// the closest entry of the folder or a parent winning. With no saved answer
// the agent settings' defaultProjectTrust applies ("ask" unless set). Both
// files are only read.
func PiTrust(projectDir, homeDir string) Result {
	agentDir := piAgentDir(homeDir)
	dir, err := filepath.Abs(projectDir)
	if err != nil {
		return Result{Name: piTrustName, Warn: true, Detail: err.Error()}
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}

	decision, at, err := savedTrust(filepath.Join(agentDir, "trust.json"), dir)
	if err != nil {
		return Result{Name: piTrustName, Warn: true, Detail: err.Error()}
	}
	what := "the wake extension and the vexillum skills"
	switch {
	case decision == nil:
	case *decision:
		return Result{Name: piTrustName, OK: true, Detail: "trusted (" + at + " in " + filepath.Join(agentDir, "trust.json") + ")"}
	default:
		return Result{Name: piTrustName, Warn: true, Detail: "Pi was told not to trust " + at + ", so it skips " + what + " there; run /trust inside pi to change it"}
	}

	switch mode := defaultProjectTrust(filepath.Join(agentDir, "settings.json")); mode {
	case "always":
		return Result{Name: piTrustName, OK: true, Detail: "no saved answer, but defaultProjectTrust is always"}
	case "never":
		return Result{Name: piTrustName, Warn: true, Detail: "defaultProjectTrust is never and there is no saved answer, so Pi skips " + what + "; run /trust inside pi in this folder"}
	}
	return Result{Name: piTrustName, Warn: true, Detail: "not trusted yet: Pi will ask \"Trust project folder?\" once, the first time you run it here; until you answer Trust, " + what + " do not load"}
}

// savedTrust finds the closest saved decision for dir in Pi's trust file (a JSON
// object of folder path to true, false or null). A missing file, or no entry for
// dir or its parents, is a nil decision; at is the folder the entry is for.
func savedTrust(path, dir string) (decision *bool, at string, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("could not read %s: %w", path, err)
	}
	var entries map[string]*bool
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, "", fmt.Errorf("%s is not a trust file Pi would accept: %w", path, err)
	}
	for cur := dir; ; {
		if v, ok := entries[cur]; ok && v != nil {
			return v, cur, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return nil, "", nil
		}
		cur = parent
	}
}

// defaultProjectTrust is the agent settings' defaultProjectTrust ("ask" when it
// is not set or cannot be read).
func defaultProjectTrust(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "ask"
	}
	var s struct {
		Mode string `json:"defaultProjectTrust"`
	}
	if json.Unmarshal(data, &s) != nil || s.Mode == "" {
		return "ask"
	}
	return s.Mode
}
