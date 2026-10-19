package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/piwake"
	"github.com/isaias-alt/vexillum/internal/scaffold"
)

func piInit(t *testing.T) (projectDir, home string, r result) {
	t.Helper()
	projectDir, home = newProject(t)
	writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), englishAgents)
	r = doInit(projectDir, home, setupOptions{Yes: true, Commander: []string{"pi"}}, "", false)
	if r.code != 0 {
		t.Fatalf("init exit %d: %s%s", r.code, r.out, r.errOut)
	}
	return projectDir, home, r
}

func recordedExtension(t *testing.T, projectDir string) string {
	t.Helper()
	cfg, err := scaffold.ReadConfig(filepath.Join(projectDir, ".vexillum"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Extensions[piwake.ExtensionRel]
}

// A pi commander gets the wake extension, the settings that let Pi see the
// skills, and the config records what was written; init says Pi will ask to
// trust the folder.
func TestInit_PiCommanderInstallsTheWake(t *testing.T) {
	projectDir, home, r := piInit(t)

	if got := readFile(t, piwake.Path(projectDir)); got != string(piwake.Source()) {
		t.Error("the installed extension is not the embedded one")
	}
	var settings struct {
		Skills []string `json:"skills"`
	}
	if err := json.Unmarshal([]byte(readFile(t, piwake.SettingsPath(projectDir))), &settings); err != nil || len(settings.Skills) != 1 || settings.Skills[0] != "../.claude/skills" {
		t.Errorf("settings = %+v, %v", settings, err)
	}
	if got := recordedExtension(t, projectDir); got != piwake.Hash() {
		t.Errorf("recorded hash = %q, want %q", got, piwake.Hash())
	}
	for _, want := range []string{
		".pi/extensions/vexillum-wake.ts (wakes a Pi commander)", // the notice
		".pi/settings.json (lets Pi see the vexillum skills)",
		"Installed .pi/extensions/vexillum-wake.ts",
		`Pi will ask "Trust project folder?" the first time you run it here`,
		"Answer Trust once",
	} {
		if !strings.Contains(r.out, want) {
			t.Errorf("init output lacks %q:\n%s", want, r.out)
		}
	}
	notExist(t, filepath.Join(projectDir, ".claude", "settings.json"))

	// Running it again changes nothing, and does not repeat the news.
	before := mustStat(t, piwake.Path(projectDir)).ModTime()
	again := doInit(projectDir, home, setupOptions{Yes: true}, "", false)
	if again.code != 0 || !strings.Contains(again.out, "already initialized and up to date") {
		t.Errorf("second init: %d %s%s", again.code, again.out, again.errOut)
	}
	if !mustStat(t, piwake.Path(projectDir)).ModTime().Equal(before) {
		t.Error("a second init rewrote the extension")
	}
}

// A Claude-only project is left exactly as it was: no .pi and no Pi words.
func TestInit_ClaudeCommanderWritesNoPiFiles(t *testing.T) {
	projectDir, home := newProject(t)
	r := doInit(projectDir, home, setupOptions{Yes: true}, "", false)
	if r.code != 0 {
		t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
	}
	notExist(t, filepath.Join(projectDir, ".pi"))
	if strings.Contains(r.out, "Pi will") || strings.Contains(r.out, ".pi/") {
		t.Errorf("the output mentions Pi:\n%s", r.out)
	}
	if cfg, err := scaffold.ReadConfig(filepath.Join(projectDir, ".vexillum")); err != nil || len(cfg.Extensions) != 0 {
		t.Errorf("config extensions = %v, %v", cfg.Extensions, err)
	}
}

// With claude and pi both, each gets what it needs.
func TestInit_PiAndClaudeCommanders(t *testing.T) {
	projectDir, home := newProject(t)
	r := doInit(projectDir, home, setupOptions{Yes: true, Commander: []string{"claude", "pi"}}, "", false)
	if r.code != 0 {
		t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
	}
	mustStat(t, piwake.Path(projectDir))
	mustStat(t, filepath.Join(projectDir, ".claude", "settings.json"))
}

// The Pi settings file may be the user's own: its other keys stay, the notice
// names it as theirs, and a file Pi would not parse is left alone with a warning.
func TestInit_PiSettingsAreMergedNotReplaced(t *testing.T) {
	projectDir, home := newProject(t)
	writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), englishAgents)
	writeFileT(t, piwake.SettingsPath(projectDir), `{"theme":"dark","skills":["../mine"]}`)

	r := doInit(projectDir, home, setupOptions{Yes: true, Commander: []string{"pi"}}, "", false)
	if r.code != 0 {
		t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
	}
	got := readFile(t, piwake.SettingsPath(projectDir))
	for _, want := range []string{`"theme": "dark"`, `"../mine"`, `"../.claude/skills"`} {
		if !strings.Contains(got, want) {
			t.Errorf("settings lack %s:\n%s", want, got)
		}
	}
	if !strings.Contains(r.out, `add ../.claude/skills to "skills"`) || strings.Contains(r.out, "lets Pi see the vexillum skills") {
		t.Errorf("the notice should list the settings among your files:\n%s", r.out)
	}

	broken, home2 := newProject(t)
	writeFileT(t, filepath.Join(broken, "AGENTS.md"), englishAgents)
	writeFileT(t, piwake.SettingsPath(broken), `{"skills": [`)
	r = doInit(broken, home2, setupOptions{Yes: true, Commander: []string{"pi"}}, "", false)
	if r.code != 0 {
		t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
	}
	if readFile(t, piwake.SettingsPath(broken)) != `{"skills": [` {
		t.Error("a settings file that is not JSON was rewritten")
	}
	if !strings.Contains(r.errOut, "could not add the vexillum skills to .pi/settings.json") {
		t.Errorf("no warning in:\n%s", r.errOut)
	}
	mustStat(t, piwake.Path(broken))
}

func TestUpgrade_PiWake(t *testing.T) {
	t.Run("a stale copy is refreshed", func(t *testing.T) {
		projectDir, home, _ := piInit(t)
		old := "// an older vexillum wake\n"
		writeFileT(t, piwake.Path(projectDir), old)
		recordHash(t, projectDir, old)

		r := doUpgrade(projectDir, home, setupOptions{Yes: true}, "", false)
		if r.code != 0 || !strings.Contains(r.out, "Refreshed .pi/extensions/vexillum-wake.ts") {
			t.Fatalf("upgrade: %d %s%s", r.code, r.out, r.errOut)
		}
		if readFile(t, piwake.Path(projectDir)) != string(piwake.Source()) || recordedExtension(t, projectDir) != piwake.Hash() {
			t.Error("the extension or its record is not current")
		}
		if strings.Contains(r.out, "Trust project folder") {
			t.Errorf("upgrade repeats the trust note:\n%s", r.out)
		}
	})

	t.Run("an edited copy is kept, and --force overwrites it after a backup", func(t *testing.T) {
		projectDir, home, _ := piInit(t)
		mine := "// my own wake\n"
		writeFileT(t, piwake.Path(projectDir), mine)

		r := doUpgrade(projectDir, home, setupOptions{Yes: true}, "", false)
		if r.code != 0 || !strings.Contains(r.out, "edited by hand, left as it is") {
			t.Fatalf("upgrade: %d %s%s", r.code, r.out, r.errOut)
		}
		if readFile(t, piwake.Path(projectDir)) != mine {
			t.Error("an edited extension was overwritten")
		}

		r = doUpgrade(projectDir, home, setupOptions{Yes: true, Force: true}, "", false)
		if r.code != 0 || !strings.Contains(r.out, "Saved your edited .pi/extensions/vexillum-wake.ts to .vexillum/backups/pi/vexillum-wake.ts") {
			t.Fatalf("forced upgrade: %d %s%s", r.code, r.out, r.errOut)
		}
		if readFile(t, piwake.Path(projectDir)) != string(piwake.Source()) {
			t.Error("--force did not install the embedded copy")
		}
		if readFile(t, filepath.Join(projectDir, ".vexillum", "backups", "pi", "vexillum-wake.ts")) != mine {
			t.Error("the backup is not the edited copy")
		}
	})

	t.Run("a missing copy is installed", func(t *testing.T) {
		projectDir, home, _ := piInit(t)
		if err := os.Remove(piwake.Path(projectDir)); err != nil {
			t.Fatal(err)
		}
		r := doUpgrade(projectDir, home, setupOptions{Yes: true}, "", false)
		if r.code != 0 || !strings.Contains(r.out, "Installed .pi/extensions/vexillum-wake.ts") {
			t.Fatalf("upgrade: %d %s%s", r.code, r.out, r.errOut)
		}
	})

	t.Run("a project that is not a pi commander gets nothing", func(t *testing.T) {
		projectDir, home := newProject(t)
		if r := doInit(projectDir, home, setupOptions{Yes: true}, "", false); r.code != 0 {
			t.Fatalf("init: %s%s", r.out, r.errOut)
		}
		if r := doUpgrade(projectDir, home, setupOptions{Yes: true}, "", false); r.code != 0 {
			t.Fatalf("upgrade: %s%s", r.out, r.errOut)
		}
		notExist(t, filepath.Join(projectDir, ".pi"))
	})
}

func recordHash(t *testing.T, projectDir, content string) {
	t.Helper()
	cfg, err := scaffold.ReadConfig(filepath.Join(projectDir, ".vexillum"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256Hex(content)
	cfg.Extensions = map[string]string{piwake.ExtensionRel: sum}
	if err := scaffold.SaveConfig(filepath.Join(projectDir, ".vexillum"), cfg); err != nil {
		t.Fatal(err)
	}
}

// doctor reports the Pi commander's pieces, and the trust the folder still
// needs; a claude project sees none of it.
func TestDoctor_PiCommanderLines(t *testing.T) {
	projectDir, home, _ := piInit(t)
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	dir := fakeBinDir(t, "claude")
	writeStub(t, dir, "pi", `echo 1.3.0`)
	writeStub(t, dir, "herdr", `case "$1" in --version) echo "herdr 0.9.5";; integration) echo "pi: installed (v9) (/x)";; esac`)
	writeStub(t, dir, "vx", `exit 0`)
	t.Setenv("PATH", dir)

	var out bytes.Buffer
	if code := runDoctor(projectDir, home, t.TempDir(), &out); code != 0 {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
	for _, want := range []string{
		"[ok] Pi wake extension - installed, current (.pi/extensions/vexillum-wake.ts)",
		"[ok] Pi skills reference - .pi/settings.json lists ../.claude/skills",
		`[warn] Pi project trust - not trusted yet: Pi will ask "Trust project folder?" once`,
		"[ok] Pi wake finds vx",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("doctor lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "sentinel Stop hook") {
		t.Errorf("a pi-only project is told about the Claude hook:\n%s", out.String())
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
