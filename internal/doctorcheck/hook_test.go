package doctorcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/scaffold"
)

// installFakeVX puts a vx stub that exits at once into dir.
func installFakeVX(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, cmdname.Name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// sandboxHookProbe keeps any vx installed on the machine running the test out
// of the hook probe: its PATH holds nothing but sh (which the hook command's
// own "sh -c" needs), and the absolute install directories the hook also looks
// in are moved under a temp dir. What is left to find is what the test installs.
// It returns that PATH.
func sandboxHookProbe(t *testing.T) (bin string) {
	t.Helper()
	root := t.TempDir()
	bin = filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/bin/sh", filepath.Join(bin, "sh")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(SetHookProbe(bin, func(command string) string {
		out := scaffold.RelocateHookInstallDirs(command, filepath.Join(root, "dirs"))
		if command == scaffold.SentinelHookCommand && out == command {
			t.Error("RelocateHookInstallDirs left the hook command as it was")
		}
		return out
	}))
	return bin
}

func byName(t *testing.T, results []Result, name string) Result {
	t.Helper()
	for _, r := range results {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no %q result in %+v", name, results)
	return Result{}
}

func writeStopHook(t *testing.T, projectDir, command string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(projectDir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Built with Sprintf %q, which is valid JSON for these ASCII commands.
	settings := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":` + jsonString(command) + `,"asyncRewake":true,"timeout":3600}]}]}}`
	if err := os.WriteFile(filepath.Join(projectDir, ".claude", "settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}
}

func jsonString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func TestStopHooks_ResolvesWithMinimalEnvironment(t *testing.T) {
	sandboxHookProbe(t)
	stubSentinelPIDs(t, nil, nil)
	projectDir, home, vexillumHome := t.TempDir(), t.TempDir(), t.TempDir()
	if _, err := scaffold.EnsureSentinelHook(projectDir); err != nil {
		t.Fatal(err)
	}
	installFakeVX(t, filepath.Join(home, ".local", "bin"))

	results := StopHooks(projectDir, vexillumHome, home)

	if r := byName(t, results, hookName); !r.OK || r.Warn {
		t.Errorf("hook presence = %+v, want ok", r)
	}
	if r := byName(t, results, hookResolves); !r.OK || r.Warn {
		t.Errorf("resolution = %+v, want ok", r)
	}
	if r := byName(t, results, sentinelName); !r.OK || !strings.Contains(r.Detail, "not running") {
		t.Errorf("sentinel = %+v, want ok and not running", r)
	}
}

func TestStopHooks_UnresolvableBinaryWarnsWithTheFix(t *testing.T) {
	sandboxHookProbe(t)
	projectDir, home := t.TempDir(), t.TempDir()
	if _, err := scaffold.EnsureSentinelHook(projectDir); err != nil {
		t.Fatal(err)
	}
	// No vx anywhere the hook looks: the wrapper exits 0 after printing its
	// not-found line, which must still be a warning.

	r := byName(t, StopHooks(projectDir, t.TempDir(), home), hookResolves)

	if !r.Warn || r.OK {
		t.Fatalf("resolution = %+v, want a warning", r)
	}
	for _, want := range []string{"not found", "PATH=" + hookProbePath, "brew", "~/.local/bin"} {
		if !strings.Contains(r.Detail, want) {
			t.Errorf("detail %q should mention %q", r.Detail, want)
		}
	}
	if strings.Contains(r.Detail, "upgrade") {
		t.Errorf("an up-to-date hook cannot be fixed by upgrading: %q", r.Detail)
	}
}

func TestStopHooks_HookAbsent(t *testing.T) {
	projectDir := t.TempDir()

	results := StopHooks(projectDir, t.TempDir(), t.TempDir())

	r := byName(t, results, hookName)
	if !r.Warn || !strings.Contains(r.Detail, "not registered") || !strings.Contains(r.Detail, cmdname.Name+" upgrade") {
		t.Errorf("absent hook = %+v", r)
	}
	for _, res := range results {
		if res.Name == hookResolves {
			t.Errorf("nothing to probe without a hook, got %+v", res)
		}
	}
}

func TestStopHooks_OnlyForeignHooksCountAsAbsent(t *testing.T) {
	projectDir := t.TempDir()
	writeStopHook(t, projectDir, "notify-me stopped")

	if r := byName(t, StopHooks(projectDir, t.TempDir(), t.TempDir()), hookName); !r.Warn {
		t.Errorf("a foreign Stop hook is not ours, got %+v", r)
	}
}

func TestStopHooks_MalformedSettingsWarns(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".claude", "settings.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}

	results := StopHooks(projectDir, t.TempDir(), t.TempDir())

	if len(results) != 1 || !results[0].Warn {
		t.Errorf("results = %+v, want one warning", results)
	}
}

func TestHookResolves_ProbeEnvironmentIsMinimal(t *testing.T) {
	// The probe must not leak the caller's environment into the hook: a
	// command that only works when some variable is set must fail it.
	t.Setenv("VX_PROBE_LEAK", "1")
	r := HookResolves(`[ -n "$VX_PROBE_LEAK" ] || { echo "`+scaffold.SentinelHookNotFoundMessage+`" >&2; exit 0; }`, t.TempDir())
	if !r.Warn {
		t.Errorf("the caller's environment leaked into the probe: %+v", r)
	}
}

func TestHookResolves_StillWaitingCountsAsFound(t *testing.T) {
	// A found vx that blocks waiting for a wake is the normal case for a
	// real await; it is stopped by the probe and counts as found. Use a
	// sleep: exec makes it the process the probe stops.
	old := hookProbeTimeout
	hookProbeTimeout = 300 * time.Millisecond
	defer func() { hookProbeTimeout = old }()
	r := HookResolves("exec sleep 30", t.TempDir())
	if !r.OK {
		t.Errorf("a hook still waiting when the probe ends should count as found: %+v", r)
	}
}
