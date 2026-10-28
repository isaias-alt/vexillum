package doctorcheck

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/piwake"
	"github.com/isaias-alt/vexillum/internal/scaffold"
)

func writeT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// piProject is a project with the Pi commander's files in place and a Pi agent
// directory of its own (PI_CODING_AGENT_DIR), so nothing reads the real ~/.pi.
func piProject(t *testing.T) (projectDir, agentDir string) {
	t.Helper()
	projectDir = t.TempDir()
	if resolved, err := filepath.EvalSymlinks(projectDir); err == nil {
		projectDir = resolved
	}
	hash, err := piwake.Install(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := piwake.EnsureSettings(projectDir); err != nil {
		t.Fatal(err)
	}
	recordExtension(t, projectDir, hash)
	agentDir = t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	return projectDir, agentDir
}

// recordExtension writes a pi-commander config.json recording hash as what vx
// last wrote for the extension.
func recordExtension(t *testing.T, projectDir, hash string) {
	t.Helper()
	dir := filepath.Join(projectDir, ".vexillum")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := scaffold.Config{Version: 1, Commander: []string{"pi"}, Extensions: map[string]string{piwake.ExtensionRel: hash}}
	if err := scaffold.SaveConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
}

func TestPiWake_AllInPlace(t *testing.T) {
	projectDir, agentDir := piProject(t)
	writeT(t, filepath.Join(agentDir, "trust.json"), `{"`+projectDir+`": true}`)
	bin := t.TempDir()
	installFakeVX(t, bin)
	t.Setenv("PATH", bin+":/usr/bin:/bin")

	results := PiWake(projectDir, t.TempDir())

	for _, name := range []string{piWakeName, piSkillsName, piTrustName, piResolvesName} {
		if r := byName(t, results, name); !r.OK || r.Warn {
			t.Errorf("%s = %+v, want ok", name, r)
		}
	}
}

func TestPiWake_ExtensionStates(t *testing.T) {
	projectDir, _ := piProject(t)
	write := func(content string) { writeT(t, piwake.Path(projectDir), content) }
	cases := []struct {
		name  string
		setup func()
		want  string
	}{
		{"missing", func() { _ = os.Remove(piwake.Path(projectDir)) }, "not installed"},
		{"edited", func() { write("// mine\n") }, "edited by hand"},
		{"stale", func() {
			write("// an older copy\n")
			recordExtension(t, projectDir, sha256Hex("// an older copy\n"))
		}, "stale"},
	}
	for _, tc := range cases {
		tc.setup()
		r := byName(t, PiWake(projectDir, t.TempDir()), piWakeName)
		if !r.Warn || !strings.Contains(r.Detail, tc.want) {
			t.Errorf("%s: %+v, want a warning with %q", tc.name, r, tc.want)
		}
	}
}

func TestPiWake_SkillsReference(t *testing.T) {
	projectDir, _ := piProject(t)

	writeT(t, piwake.SettingsPath(projectDir), `{"theme":"dark"}`)
	r := byName(t, PiWake(projectDir, t.TempDir()), piSkillsName)
	if !r.Warn || !strings.Contains(r.Detail, "../.claude/skills") || !strings.Contains(r.Detail, "vx init") {
		t.Errorf("not listed: %+v", r)
	}

	writeT(t, piwake.SettingsPath(projectDir), `{broken`)
	if r := byName(t, PiWake(projectDir, t.TempDir()), piSkillsName); !r.Warn || !strings.Contains(r.Detail, "invalid JSON") {
		t.Errorf("broken settings: %+v", r)
	}
}

func TestPiTrust(t *testing.T) {
	projectDir, agentDir := piProject(t)
	home := t.TempDir()
	trust := filepath.Join(agentDir, "trust.json")
	settings := filepath.Join(agentDir, "settings.json")

	// Nothing saved: Pi will ask once.
	r := PiTrust(projectDir, home)
	if !r.Warn || !strings.Contains(r.Detail, "Pi will ask") || !strings.Contains(r.Detail, "once") {
		t.Errorf("no answer yet: %+v", r)
	}

	// The folder, a parent, and the closest answer winning.
	writeT(t, trust, `{"`+projectDir+`": true}`)
	if r := PiTrust(projectDir, home); !r.OK || !strings.Contains(r.Detail, "trusted") {
		t.Errorf("trusted folder: %+v", r)
	}
	writeT(t, trust, `{"`+filepath.Dir(projectDir)+`": true}`)
	if r := PiTrust(projectDir, home); !r.OK {
		t.Errorf("trusted parent: %+v", r)
	}
	writeT(t, trust, `{"`+filepath.Dir(projectDir)+`": true, "`+projectDir+`": false}`)
	if r := PiTrust(projectDir, home); !r.Warn || !strings.Contains(r.Detail, "not to trust") || !strings.Contains(r.Detail, "/trust") {
		t.Errorf("declined folder under a trusted parent: %+v", r)
	}
	// A null entry is Pi's "forget", not a decision.
	writeT(t, trust, `{"`+projectDir+`": null}`)
	if r := PiTrust(projectDir, home); !r.Warn || !strings.Contains(r.Detail, "Pi will ask") {
		t.Errorf("null entry: %+v", r)
	}

	// With no saved answer, the agent setting decides.
	_ = os.Remove(trust)
	writeT(t, settings, `{"defaultProjectTrust":"always"}`)
	if r := PiTrust(projectDir, home); !r.OK {
		t.Errorf("always: %+v", r)
	}
	writeT(t, settings, `{"defaultProjectTrust":"never"}`)
	if r := PiTrust(projectDir, home); !r.Warn || !strings.Contains(r.Detail, "never") {
		t.Errorf("never: %+v", r)
	}

	// A trust file Pi would reject is said so, not guessed at.
	writeT(t, trust, `[1]`)
	if r := PiTrust(projectDir, home); !r.Warn || !strings.Contains(r.Detail, "trust.json") {
		t.Errorf("bad file: %+v", r)
	}
}

// Without PI_CODING_AGENT_DIR the agent directory is ~/.pi/agent, and a symlinked
// project folder is looked up by its canonical path, the way Pi saves it.
func TestPiTrust_DefaultDirAndSymlinks(t *testing.T) {
	projectDir, _ := piProject(t)
	t.Setenv("PI_CODING_AGENT_DIR", "")
	home := t.TempDir()
	writeT(t, filepath.Join(home, ".pi", "agent", "trust.json"), `{"`+projectDir+`": true}`)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(projectDir, link); err != nil {
		t.Fatal(err)
	}
	if r := PiTrust(link, home); !r.OK {
		t.Errorf("through a symlink: %+v", r)
	}
}

func TestPiWake_VXNotFoundWarns(t *testing.T) {
	t.Setenv("PATH", sandboxHookProbe(t))
	projectDir, _ := piProject(t)

	r := byName(t, PiWake(projectDir, t.TempDir()), piResolvesName)

	if !r.Warn || r.OK {
		t.Fatalf("resolution = %+v, want a warning", r)
	}
	for _, want := range []string{"not found", "brew", "~/.local/bin", "PATH"} {
		if !strings.Contains(r.Detail, want) {
			t.Errorf("detail %q should mention %q", r.Detail, want)
		}
	}
}

// A vx that is found must not sit waiting for a wake because doctor itself runs
// inside a herdr pane.
func TestPiWake_ProbeDropsTheHerdrEnvironment(t *testing.T) {
	t.Setenv("HERDR_WORKSPACE_ID", "w1")
	t.Setenv("HERDR_PANE_ID", "w1:p1")
	for _, kv := range probeEnv("/home/x") {
		if strings.HasPrefix(kv, "HERDR_") {
			t.Errorf("the probe kept %s", kv)
		}
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// The probe's verdicts, whatever is installed on the machine the tests run on.
func TestProbeAwait_Verdicts(t *testing.T) {
	env := []string{"HOME=" + t.TempDir(), "PATH=/usr/bin:/bin"}
	notFound := "echo '" + scaffold.SentinelHookNotFoundMessage + "' >&2; exit 0"
	for _, tc := range []struct {
		name, command string
		ok, warn      bool
		detail        string
	}{
		{"found", "exit 0", true, false, "found with the pane's environment"},
		{"printed not found", notFound, false, true, "not found with the pane's environment"},
		{"exit 127", "exit 127", false, true, "not found with the pane's environment"},
	} {
		r := probeAwait("probe", tc.command, env, "the pane's environment", "", "fix it")
		if r.OK != tc.ok || r.Warn != tc.warn || !strings.Contains(r.Detail, tc.detail) {
			t.Errorf("%s: %+v, want ok=%v warn=%v with %q", tc.name, r, tc.ok, tc.warn, tc.detail)
		}
		if tc.warn && !strings.Contains(r.Detail, "fix it") {
			t.Errorf("%s: the fix is missing from %q", tc.name, r.Detail)
		}
	}
}
