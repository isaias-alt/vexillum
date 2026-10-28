package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/doctorcheck"
	"github.com/isaias-alt/vexillum/internal/scaffold"
	"github.com/isaias-alt/vexillum/internal/slot"
)

func readCommander(t *testing.T, projectDir string) []string {
	t.Helper()
	cfg, err := scaffold.ReadConfig(filepath.Join(projectDir, ".vexillum"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Commander
}

func TestInit_CommanderFlagParsing(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		wantErr string
	}{
		{[]string{"--commander"}, "needs a value"},
		{[]string{"--commander", "codex"}, `unknown harness "codex"`},
		{[]string{"--commander="}, "needs a value"},
		{[]string{"--commander=claude", "--commander", "gemini"}, `unknown harness "gemini"`},
	} {
		_, _, err := parseSetupArgs(setupInit, tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%v: err = %v, want %q", tc.args, err, tc.wantErr)
		}
	}
	// It is init's flag: upgrade reads the config instead.
	if _, _, err := parseSetupArgs(setupUpgrade, []string{"--commander", "pi"}); err == nil || !strings.Contains(err.Error(), "unknown upgrade flag") {
		t.Errorf("upgrade --commander: err = %v, want an unknown flag", err)
	}

	opts, _, err := parseSetupArgs(setupInit, []string{"--commander", "pi", "--commander=claude", "--commander", "pi", "--commander=opencode"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"pi", "claude", "opencode"}; !slices.Equal(opts.Commander, want) {
		t.Errorf("Commander = %v, want %v (flag order, no repeats)", opts.Commander, want)
	}
	opts, _, err = parseSetupArgs(setupInit, []string{"--yes"})
	if err != nil || opts.Commander != nil {
		t.Errorf("without the flag Commander = %v, err = %v, want nil", opts.Commander, err)
	}
}

// The default is claude: it is recorded in config.json and everything
// Claude-shaped init wrote before the field existed is still written.
func TestInit_DefaultCommanderIsClaude(t *testing.T) {
	projectDir, home := newProject(t)
	if r := doInit(projectDir, home, setupOptions{Yes: true}, "", false); r.code != 0 {
		t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
	}
	if got := readCommander(t, projectDir); !slices.Equal(got, []string{"claude"}) {
		t.Errorf("config commander = %v, want [claude]", got)
	}
	mustStat(t, filepath.Join(projectDir, ".claude", "settings.json"))
	mustStat(t, filepath.Join(projectDir, "CLAUDE.md"))
}

// A commander that is not Claude Code has no use for the CLAUDE.md import or
// the Stop hook, so init leaves them out; the block, the skills and the
// config are still written.
func TestInit_NonClaudeCommanderSkipsClaudeOnlyFiles(t *testing.T) {
	for _, harness := range []string{"pi", "opencode"} {
		t.Run(harness, func(t *testing.T) {
			projectDir, home := newProject(t)
			writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), englishAgents)
			r := doInit(projectDir, home, setupOptions{Yes: true, Commander: []string{harness}}, "", false)
			if r.code != 0 {
				t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
			}
			if got := readCommander(t, projectDir); !slices.Equal(got, []string{harness}) {
				t.Errorf("config commander = %v, want [%s]", got, harness)
			}
			notExist(t, filepath.Join(projectDir, "CLAUDE.md"))
			notExist(t, filepath.Join(projectDir, ".claude", "settings.json"))
			if !strings.Contains(readFile(t, filepath.Join(projectDir, "AGENTS.md")), "Vexillum commander") {
				t.Error("AGENTS.md has no vexillum block")
			}
			mustStat(t, filepath.Join(projectDir, ".claude", "skills", "vexillum", "SKILL.md"))
			if strings.Contains(r.out, "Stop hook") || strings.Contains(r.out, "CLAUDE.md") {
				t.Errorf("the notice mentions Claude-only files:\n%s", r.out)
			}

			// Upgrade reads the config, so it keeps leaving them out.
			if r := doUpgrade(projectDir, home, setupOptions{Yes: true}, "", false); r.code != 0 {
				t.Fatalf("upgrade exit %d: %s%s", r.code, r.out, r.errOut)
			}
			notExist(t, filepath.Join(projectDir, "CLAUDE.md"))
			notExist(t, filepath.Join(projectDir, ".claude", "settings.json"))
		})
	}
}

func TestInit_SeveralCommandersIncludingClaude(t *testing.T) {
	projectDir, home := newProject(t)
	writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), englishAgents)
	r := doInit(projectDir, home, setupOptions{Yes: true, Commander: []string{"pi", "claude"}}, "", false)
	if r.code != 0 {
		t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
	}
	if got := readCommander(t, projectDir); !slices.Equal(got, []string{"pi", "claude"}) {
		t.Errorf("config commander = %v, want [pi claude]", got)
	}
	if got := readFile(t, filepath.Join(projectDir, "CLAUDE.md")); got != "@AGENTS.md\n" {
		t.Errorf("CLAUDE.md = %q", got)
	}
	findStopHookEntry(t, parseSettings(t, readFile(t, filepath.Join(projectDir, ".claude", "settings.json"))), sentinelHookCommand)
}

// init on an initialized project records a different commander set and, now
// that claude is among them, brings the Claude files in.
func TestInit_CommanderFlagUpdatesExistingConfig(t *testing.T) {
	projectDir, home := newProject(t)
	writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), englishAgents)
	if r := doInit(projectDir, home, setupOptions{Yes: true, Commander: []string{"pi"}}, "", false); r.code != 0 {
		t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
	}
	cfgBefore, _ := scaffold.ReadConfig(filepath.Join(projectDir, ".vexillum"))

	// Same set again: nothing to do.
	r := doInit(projectDir, home, setupOptions{Yes: true, Commander: []string{"pi"}}, "", false)
	if r.code != 0 || !strings.Contains(r.out, "Nothing to change") {
		t.Fatalf("second run: exit %d: %s%s", r.code, r.out, r.errOut)
	}

	r = doInit(projectDir, home, setupOptions{Yes: true, Commander: []string{"pi", "claude"}}, "", false)
	if r.code != 0 {
		t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
	}
	if !strings.Contains(r.out, "commander: pi, claude") {
		t.Errorf("the notice does not mention the commander change:\n%s", r.out)
	}
	cfg, _ := scaffold.ReadConfig(filepath.Join(projectDir, ".vexillum"))
	if !slices.Equal(cfg.Commander, []string{"pi", "claude"}) {
		t.Errorf("config commander = %v, want [pi claude]", cfg.Commander)
	}
	if !cfg.InitializedAt.Equal(cfgBefore.InitializedAt) || len(cfg.Skills) != len(cfgBefore.Skills) {
		t.Errorf("the rest of config.json changed: before %+v, after %+v", cfgBefore, cfg)
	}
	mustStat(t, filepath.Join(projectDir, "CLAUDE.md"))
	mustStat(t, filepath.Join(projectDir, ".claude", "settings.json"))
}

// A config.json from before the commander field means a claude commander,
// and init or upgrade does not rewrite it just to say so.
func TestUpgrade_ConfigWithoutCommanderStaysClaude(t *testing.T) {
	projectDir, home := newProject(t)
	writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), englishAgents)
	if r := doInit(projectDir, home, setupOptions{Yes: true}, "", false); r.code != 0 {
		t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
	}
	cfgPath := filepath.Join(projectDir, ".vexillum", "config.json")
	var raw map[string]any
	if err := json.Unmarshal([]byte(readFile(t, cfgPath)), &raw); err != nil {
		t.Fatal(err)
	}
	delete(raw, "commander")
	old, _ := json.MarshalIndent(raw, "", "  ")
	writeFileT(t, cfgPath, string(old))

	r := doUpgrade(projectDir, home, setupOptions{Yes: true}, "", false)
	if r.code != 0 || !strings.Contains(r.out, "Nothing to change") {
		t.Fatalf("upgrade: exit %d: %s%s", r.code, r.out, r.errOut)
	}
	if got := readFile(t, cfgPath); got != string(old) {
		t.Errorf("config.json was rewritten:\n%s", got)
	}
}

// doctorOutput runs doctor for a project initialized with commander, against
// a PATH that holds exactly the tools named.
func doctorOutput(t *testing.T, commander []string, tools ...string) (int, string) {
	t.Helper()
	t.Setenv("PATH", fakeBinDir(t, tools...))
	t.Cleanup(doctorcheck.SetLiveSentinelPIDs(func() ([]int, error) { return nil, nil }))
	// The fake vx below exits at once; a loaded machine must not make the
	// probe give up on it first and report "still waiting".
	t.Cleanup(doctorcheck.SetHookProbeTimeout(time.Minute))
	projectDir, _ := newProject(t)
	writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), englishAgents)
	if r := doInit(projectDir, t.TempDir(), setupOptions{Yes: true, Commander: commander}, "", false); r.code != 0 {
		t.Fatalf("init exit %d: %s%s", r.code, r.out, r.errOut)
	}
	// The Stop hook probe looks for vx under $HOME/.local/bin among other
	// places; a fake there makes the "finds vx" line the same on a machine
	// with vx installed and on CI without it.
	homeDir := t.TempDir()
	fakeVXDir := filepath.Join(homeDir, ".local", "bin")
	if err := os.MkdirAll(fakeVXDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeStub(t, fakeVXDir, "vx", "exit 0")
	var out bytes.Buffer
	code := runDoctor(projectDir, t.TempDir(), homeDir, &out)
	return code, out.String()
}

// The default (claude) commander gets exactly the doctor report it got
// before commanders existed.
func TestDoctor_DefaultCommanderOutputIsUnchanged(t *testing.T) {
	code, out := doctorOutput(t, nil, "claude", "herdr")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "[ok] another vx earlier in PATH") {
			l = "[ok] another vx earlier in PATH - <varies>"
		}
		lines = append(lines, l)
	}
	want := []string{
		"[ok] Claude Code",
		"[ok] herdr",
		`[warn] herdr version - could not determine herdr version: unexpected herdr --version output: ""`,
		"[missing] GitHub CLI (gh) - not found in PATH (optional - only needed for 'vx ship'/'vx land' on a shipped mission) - install it with 'brew install gh' or from https://cli.github.com",
		"[ok] another vx earlier in PATH - <varies>",
		"[ok] ~/.vexillum/",
		"[ok] project initialized",
		"[ok] git repository",
		"[ok] AGENTS.md vexillum block - current (en)",
		"[ok] CLAUDE.md imports AGENTS.md",
		"[ok] skill vexillum - installed, current",
		"[ok] skill forum - installed, current",
		"[ok] skill muster - installed, current",
		"[ok] models.json - valid, 5 profiles",
		"[ok] yolo mode - off",
		"[ok] sentinel Stop hook - registered in .claude/settings.json",
		"[ok] sentinel Stop hook finds vx - found with a minimal environment",
		"[ok] sentinel - not running (the next 'vx dispatch' starts it), 0 sentinel await(s) waiting",
		"",
		"Environment ready (optional: GitHub CLI (gh) missing; warnings: herdr version).",
		"",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("doctor output changed.\n got:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

// The commander harness's binary is the required one: a pi commander needs
// pi and not claude.
func TestDoctor_PiCommanderRequiresPiNotClaude(t *testing.T) {
	code, out := doctorOutput(t, []string{"pi"}, "pi", "herdr")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, want := range []string{"[missing] Claude Code - claude not found in PATH (optional)", "[ok] Pi\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"sentinel Stop hook", "CLAUDE.md imports"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("a pi commander has no %q, got:\n%s", unwanted, out)
		}
	}
	if !strings.Contains(out, "[ok] sentinel - ") {
		t.Errorf("the sentinel is still reported:\n%s", out)
	}

	code, out = doctorOutput(t, []string{"pi"}, "claude", "herdr")
	if code == 0 || !strings.Contains(out, "[missing] Pi - pi not found in PATH") {
		t.Errorf("without pi a pi commander is not ready (exit %d):\n%s", code, out)
	}
}

func TestDoctor_OpenCodeCommanderRequiresOpenCode(t *testing.T) {
	code, out := doctorOutput(t, []string{"opencode"}, "claude", "herdr")
	if code == 0 || !strings.Contains(out, "[missing] OpenCode - opencode not found in PATH") {
		t.Errorf("without opencode an opencode commander is not ready (exit %d):\n%s", code, out)
	}
	if !strings.Contains(out, "[ok] Claude Code") {
		t.Errorf("claude is still reported:\n%s", out)
	}
}

// Several commanders: each one's binary is required, and the Claude files are
// checked because claude is one of them.
func TestDoctor_SeveralCommandersRequireEachBinary(t *testing.T) {
	code, out := doctorOutput(t, []string{"claude", "pi"}, "claude", "herdr")
	if code == 0 || !strings.Contains(out, "[missing] Pi - pi not found in PATH") {
		t.Errorf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "[ok] sentinel Stop hook - registered") {
		t.Errorf("the Stop hook is checked when claude is a commander:\n%s", out)
	}
}

func TestDoctor_UnknownCommanderWarns(t *testing.T) {
	t.Setenv("PATH", fakeBinDir(t, "claude", "herdr"))
	projectDir := initializedProject(t)
	cfg, err := scaffold.ReadConfig(filepath.Join(projectDir, ".vexillum"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Commander = []string{"codex"}
	if err := scaffold.SaveConfig(filepath.Join(projectDir, ".vexillum"), cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	code := runDoctor(projectDir, t.TempDir(), t.TempDir(), &out)
	if code != 0 {
		t.Errorf("a typo in the config must not fail doctor, exit %d", code)
	}
	if want := `[warn] commander harnesses - config.json names "codex", which vexillum does not know`; !strings.Contains(out.String(), want) {
		t.Errorf("expected %q in:\n%s", want, out.String())
	}
	if !strings.Contains(out.String(), "[ok] Claude Code") {
		t.Errorf("the default applies when nothing valid is left:\n%s", out.String())
	}
}

// OpenCode reads AGENTS.md first, so with neither file in the project an
// opencode commander's block goes to a new AGENTS.md, without the question
// about CLAUDE.md that a Claude-only project is asked.
func TestInit_OpenCodeCommanderWithNoRulesFilePutsTheBlockInAGENTS(t *testing.T) {
	t.Run("alone", func(t *testing.T) {
		projectDir, home := newProject(t)
		// A terminal whose answers are all the defaults.
		r := doInit(projectDir, home, setupOptions{Commander: []string{"opencode"}}, strings.Repeat("\n", 8), true)
		if r.code != 0 {
			t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
		}
		if strings.Contains(r.out, "instead of creating an AGENTS.md") {
			t.Errorf("an OpenCode commander was asked whether the block may go to CLAUDE.md:\n%s", r.out)
		}
		if !strings.Contains(readFile(t, filepath.Join(projectDir, "AGENTS.md")), "Vexillum commander") {
			t.Error("AGENTS.md has no vexillum block")
		}
		notExist(t, filepath.Join(projectDir, "CLAUDE.md"))
		notExist(t, filepath.Join(projectDir, ".claude", "settings.json"))
		mustStat(t, filepath.Join(projectDir, ".claude", "skills", "vexillum", "SKILL.md"))
	})
	t.Run("with claude", func(t *testing.T) {
		projectDir, home := newProject(t)
		r := doInit(projectDir, home, setupOptions{Yes: true, Commander: []string{"opencode", "claude"}}, "", false)
		if r.code != 0 {
			t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
		}
		if !strings.Contains(readFile(t, filepath.Join(projectDir, "AGENTS.md")), "Vexillum commander") {
			t.Error("AGENTS.md has no vexillum block")
		}
		if got := readFile(t, filepath.Join(projectDir, "CLAUDE.md")); got != "@AGENTS.md\n" {
			t.Errorf("CLAUDE.md = %q, want only the import", got)
		}
	})
}

// A CLAUDE.md of the user's keeps the block: OpenCode falls back to it while
// there is no AGENTS.md, and a new AGENTS.md would hide the user's own rules.
func TestInit_OpenCodeCommanderKeepsAnExistingCLAUDEmd(t *testing.T) {
	projectDir, home := newProject(t)
	writeFileT(t, filepath.Join(projectDir, "CLAUDE.md"), "# my own rules\n")
	r := doInit(projectDir, home, setupOptions{Yes: true, Commander: []string{"opencode"}}, "", false)
	if r.code != 0 {
		t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
	}
	notExist(t, filepath.Join(projectDir, "AGENTS.md"))
	claude := readFile(t, filepath.Join(projectDir, "CLAUDE.md"))
	if !strings.Contains(claude, "# my own rules") || !strings.Contains(claude, "Vexillum commander") {
		t.Errorf("CLAUDE.md = %q", claude)
	}
	if strings.Contains(r.errOut, "ignores CLAUDE.md") {
		t.Errorf("a block OpenCode can read was reported as shadowed:\n%s", r.errOut)
	}
}

// A block in CLAUDE.md next to an AGENTS.md is invisible to OpenCode: init
// says so and moves nothing.
func TestInit_OpenCodeCommanderWarnsWhenAGENTShidesTheBlock(t *testing.T) {
	projectDir, home := newProject(t)
	if r := doInit(projectDir, home, setupOptions{Yes: true}, "", false); r.code != 0 {
		t.Fatalf("first init: %s%s", r.out, r.errOut)
	}
	if !strings.Contains(readFile(t, filepath.Join(projectDir, "CLAUDE.md")), "Vexillum commander") {
		t.Fatal("setup: the block should be in CLAUDE.md")
	}
	writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), "# rules added later\n")

	r := doInit(projectDir, home, setupOptions{Yes: true, Commander: []string{"opencode"}}, "", false)
	if r.code != 0 {
		t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
	}
	for _, want := range []string{"warning:", "OpenCode reads that one and ignores CLAUDE.md", "Delete the block from CLAUDE.md", "init"} {
		if !strings.Contains(r.errOut, want) {
			t.Errorf("stderr lacks %q:\n%s", want, r.errOut)
		}
	}
	if got := readFile(t, filepath.Join(projectDir, "AGENTS.md")); got != "# rules added later\n" {
		t.Errorf("AGENTS.md was changed: %q", got)
	}

	// Following the advice fixes it.
	claude := readFile(t, filepath.Join(projectDir, "CLAUDE.md"))
	out, err := slot.Remove(claude)
	if err != nil {
		t.Fatal(err)
	}
	writeFileT(t, filepath.Join(projectDir, "CLAUDE.md"), out)
	r = doInit(projectDir, home, setupOptions{Yes: true, Commander: []string{"opencode"}}, "", false)
	if r.code != 0 || strings.Contains(r.errOut, "ignores CLAUDE.md") {
		t.Fatalf("after moving the block: exit %d\n%s%s", r.code, r.out, r.errOut)
	}
	if !strings.Contains(readFile(t, filepath.Join(projectDir, "AGENTS.md")), "Vexillum commander") {
		t.Error("the block did not reach AGENTS.md")
	}
}
