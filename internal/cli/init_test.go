package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/install"
	"github.com/isaias-alt/vexillum/internal/scaffold"
	"github.com/isaias-alt/vexillum/internal/slot"
	"github.com/isaias-alt/vexillum/skills"
)

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}
}

func mustStat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	return info
}

// result is what a setup run printed and returned.
type result struct {
	code        int
	out, errOut string
}

// setupTestEnv builds an environment reading stdin, as a terminal when
// interactive is true.
func setupTestEnv(opts setupOptions, stdin string, interactive bool) (*setupEnv, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return newSetupEnv(opts, strings.NewReader(stdin), interactive, &out, &errOut), &out, &errOut
}

func boolp(b bool) *bool { return &b }

// initYes runs init with --yes: the non-interactive "accept everything" path.
func initYes(projectDir, vexillumHome string, stdout, stderr io.Writer) int {
	env := newSetupEnv(setupOptions{Yes: true}, strings.NewReader(""), false, stdout, stderr)
	return runInit(env, projectDir, vexillumHome)
}

func doInit(projectDir, vexillumHome string, opts setupOptions, stdin string, interactive bool) result {
	env, out, errOut := setupTestEnv(opts, stdin, interactive)
	code := runInit(env, projectDir, vexillumHome)
	return result{code, out.String(), errOut.String()}
}

func doUpgrade(projectDir, vexillumHome string, opts setupOptions, stdin string, interactive bool) result {
	env, out, errOut := setupTestEnv(opts, stdin, interactive)
	code := runUpgrade(env, projectDir, vexillumHome)
	return result{code, out.String(), errOut.String()}
}

func newProject(t *testing.T) (projectDir, vexillumHome string) {
	t.Helper()
	projectDir = t.TempDir()
	initGitRepo(t, projectDir)
	return projectDir, filepath.Join(t.TempDir(), ".vexillum")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func writeFileT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func notExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s not to exist (err: %v)", path, err)
	}
}

const spanishAgents = "# Guia del proyecto\n\nEsta es la guia para los agentes que trabajan en este repositorio y que debe seguir cuando se hace un cambio en el codigo.\n"
const englishAgents = "# Project guide\n\nThis is the guide for the agents that work in this repository and that they should follow when they make a change to the code.\n"

// The whole flow with --yes and no terminal: AGENTS.md block, CLAUDE.md
// import, skills, models.json, config, gitignore and hook, and no rules file.
func TestInit_YesWritesEverything(t *testing.T) {
	projectDir, home := newProject(t)
	r := doInit(projectDir, home, setupOptions{Yes: true}, "", false)
	if r.code != 0 {
		t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
	}
	mustStat(t, home)
	mustStat(t, filepath.Join(projectDir, ".vexillum", "config.json"))
	mustStat(t, filepath.Join(projectDir, ".vexillum", "models.json"))
	mustStat(t, filepath.Join(projectDir, ".vexillum", ".gitignore"))
	notExist(t, filepath.Join(projectDir, ".claude", "rules", "vexillum.md"))

	core, _ := install.SlotTemplate(slot.LangEN)
	if ins := slot.Inspect(readFile(t, filepath.Join(projectDir, "AGENTS.md")), core); ins.State != slot.StateCurrent {
		t.Errorf("AGENTS.md block state = %v, want current", ins.State)
	}
	if got := readFile(t, filepath.Join(projectDir, "CLAUDE.md")); got != "@AGENTS.md\n" {
		t.Errorf("CLAUDE.md = %q", got)
	}
	cfg, err := scaffold.ReadConfig(filepath.Join(projectDir, ".vexillum"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range skills.Names() {
		want, _ := skills.Hash(name)
		if cfg.Skills[name] != want {
			t.Errorf("config hash for skill %s = %q, want %q", name, cfg.Skills[name], want)
		}
		mustStat(t, filepath.Join(projectDir, ".claude", "skills", name, "SKILL.md"))
	}
	findStopHookEntry(t, parseSettings(t, readFile(t, filepath.Join(projectDir, ".claude", "settings.json"))), sentinelHookCommand)
	if !strings.Contains(r.out, "will change these files") {
		t.Errorf("expected the notice, got: %s", r.out)
	}
}

// Running init twice changes nothing and says so.
func TestInit_Idempotent(t *testing.T) {
	projectDir, home := newProject(t)
	if r := doInit(projectDir, home, setupOptions{Yes: true}, "", false); r.code != 0 {
		t.Fatalf("first init: %s%s", r.out, r.errOut)
	}
	before, err := snapshotTree(t, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	r := doInit(projectDir, home, setupOptions{Yes: true}, "", false)
	if r.code != 0 || !strings.Contains(r.out, "Nothing to change") {
		t.Fatalf("second init: exit %d, out: %s%s", r.code, r.out, r.errOut)
	}
	if strings.Contains(r.out, "will change") {
		t.Errorf("second init printed a notice: %s", r.out)
	}
	after, _ := snapshotTree(t, projectDir)
	if before != after {
		t.Error("second init changed the project")
	}
	// Even without --yes and without a terminal: nothing to do is not an error.
	if r := doInit(projectDir, home, setupOptions{}, "", false); r.code != 0 {
		t.Errorf("idempotent rerun without --yes: exit %d: %s%s", r.code, r.out, r.errOut)
	}
}

// Non-TTY: without --yes nothing is written, the notice is printed, and the
// exit code is non-zero with an actionable message.
func TestInit_NonTTYWithoutYes(t *testing.T) {
	projectDir, home := newProject(t)
	r := doInit(projectDir, home, setupOptions{}, "", false)
	if r.code == 0 {
		t.Fatal("expected a non-zero exit")
	}
	if !strings.Contains(r.out, "AGENTS.md") || !strings.Contains(r.errOut, "--yes") {
		t.Errorf("notice or hint missing:\nout: %s\nerr: %s", r.out, r.errOut)
	}
	notExist(t, filepath.Join(projectDir, ".vexillum"))
	notExist(t, filepath.Join(projectDir, "AGENTS.md"))
	notExist(t, home)

	// --skills / --no-skills alone do not make it act either.
	if r := doInit(projectDir, home, setupOptions{Skills: boolp(false)}, "", false); r.code == 0 {
		t.Error("--no-skills without --yes must still refuse")
	}
	notExist(t, filepath.Join(projectDir, "AGENTS.md"))
}

func TestInit_SkillsFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		flag *bool
		want bool
	}{{"skills", boolp(true), true}, {"no-skills", boolp(false), false}} {
		t.Run(tc.name, func(t *testing.T) {
			projectDir, home := newProject(t)
			r := doInit(projectDir, home, setupOptions{Yes: true, Skills: tc.flag}, "", false)
			if r.code != 0 {
				t.Fatalf("%s%s", r.out, r.errOut)
			}
			_, err := os.Stat(filepath.Join(projectDir, ".claude", "skills", "vexillum", "SKILL.md"))
			if (err == nil) != tc.want {
				t.Errorf("skills installed = %v, want %v", err == nil, tc.want)
			}
			// Declining twice is idempotent too.
			if again := doInit(projectDir, home, setupOptions{Yes: true, Skills: tc.flag}, "", false); !strings.Contains(again.out, "Nothing to change") {
				t.Errorf("second run: %s", again.out)
			}
		})
	}
}

// Interactive consent: the notice, then Continue? [Y/n].
func TestInit_Consent(t *testing.T) {
	t.Run("no stops before writing anything", func(t *testing.T) {
		projectDir, home := newProject(t)
		r := doInit(projectDir, home, setupOptions{}, "n\n", true)
		if r.code != 0 || !strings.Contains(r.out, "Nothing was changed.") {
			t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
		}
		notExist(t, filepath.Join(projectDir, ".vexillum"))
		notExist(t, filepath.Join(projectDir, "AGENTS.md"))
		notExist(t, filepath.Join(projectDir, "CLAUDE.md"))
		notExist(t, home)
	})
	t.Run("empty answers take the defaults (yes)", func(t *testing.T) {
		projectDir, home := newProject(t)
		// Continue? / language / skills, all default.
		r := doInit(projectDir, home, setupOptions{}, "\n\n\n", true)
		if r.code != 0 {
			t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
		}
		for _, want := range []string{"Continue? [Y/n]", "Install the vexillum, forum and muster skills? [Y/n]", "Use en for the vexillum block? [Y/n]"} {
			if !strings.Contains(r.out, want) {
				t.Errorf("missing prompt %q in:\n%s", want, r.out)
			}
		}
		mustStat(t, filepath.Join(projectDir, ".claude", "skills", "forum", "SKILL.md"))
	})
	t.Run("declining the skills", func(t *testing.T) {
		projectDir, home := newProject(t)
		r := doInit(projectDir, home, setupOptions{}, "y\ny\nn\n", true)
		if r.code != 0 {
			t.Fatalf("%s%s", r.out, r.errOut)
		}
		notExist(t, filepath.Join(projectDir, ".claude", "skills"))
		mustStat(t, filepath.Join(projectDir, "AGENTS.md"))
	})
	t.Run("closed input is an error, not a silent yes", func(t *testing.T) {
		projectDir, home := newProject(t)
		r := doInit(projectDir, home, setupOptions{}, "", true)
		if r.code == 0 {
			t.Fatalf("expected failure, got: %s", r.out)
		}
		notExist(t, filepath.Join(projectDir, "AGENTS.md"))
	})
}

// Language: detection is shown and can be confirmed or overridden; --lang
// skips the question; no text defaults to English.
func TestInit_Language(t *testing.T) {
	tests := []struct {
		name     string
		agents   string
		opts     setupOptions
		stdin    string
		wantLang slot.Lang
		wantOut  string
	}{
		{"spanish detected and confirmed", spanishAgents, setupOptions{Skills: boolp(false)}, "y\ny\n", slot.LangES, "Detected language: es"},
		{"detected, overridden to en", spanishAgents, setupOptions{Skills: boolp(false)}, "y\nn\nen\n", slot.LangEN, "Language (en/es): "},
		{"no text defaults to en and can be overridden", "", setupOptions{Skills: boolp(false)}, "y\nn\nes\n", slot.LangES, "No language detected in AGENTS.md (it does not exist yet), defaulting to en"},
		{"bad override is asked again", englishAgents, setupOptions{Skills: boolp(false)}, "y\nn\nfr\nes\n", slot.LangES, "Please type en or es."},
		{"--lang skips the question", spanishAgents, setupOptions{Skills: boolp(false), Lang: slot.LangEN}, "y\n", slot.LangEN, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projectDir, home := newProject(t)
			if tc.agents != "" {
				writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), tc.agents)
			}
			r := doInit(projectDir, home, tc.opts, tc.stdin, true)
			if r.code != 0 {
				t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
			}
			if !strings.Contains(r.out, tc.wantOut) {
				t.Errorf("output lacks %q:\n%s", tc.wantOut, r.out)
			}
			want, _ := install.SlotTemplate(tc.wantLang)
			if ins := slot.Inspect(readFile(t, filepath.Join(projectDir, "AGENTS.md")), want); ins.State != slot.StateCurrent {
				t.Errorf("block is %v, want current in %s", ins.State, tc.wantLang)
			}
			if tc.opts.Lang != "" && strings.Contains(r.out, "for the vexillum block?") {
				t.Errorf("--lang must skip the language question:\n%s", r.out)
			}
		})
	}
}

// An existing AGENTS.md keeps every byte of the user's text; the block is
// appended. A block already there (same language) is not duplicated.
func TestInit_ExistingAgents(t *testing.T) {
	t.Run("without a block", func(t *testing.T) {
		projectDir, home := newProject(t)
		writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), englishAgents)
		if r := doInit(projectDir, home, setupOptions{Yes: true}, "", false); r.code != 0 {
			t.Fatal(r.out, r.errOut)
		}
		got := readFile(t, filepath.Join(projectDir, "AGENTS.md"))
		if !strings.HasPrefix(got, englishAgents) {
			t.Errorf("user text was changed:\n%s", got)
		}
		if strings.Count(got, "BEGIN VEXILLUM") != 1 {
			t.Error("expected exactly one block")
		}
	})
	t.Run("with a current block", func(t *testing.T) {
		projectDir, home := newProject(t)
		core, _ := install.SlotTemplate(slot.LangEN)
		content, _ := slot.Upsert(englishAgents, core, false)
		writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), content)
		if r := doInit(projectDir, home, setupOptions{Yes: true}, "", false); r.code != 0 {
			t.Fatal(r.out, r.errOut)
		}
		if got := readFile(t, filepath.Join(projectDir, "AGENTS.md")); got != content {
			t.Error("a current block must leave AGENTS.md byte for byte as it was")
		}
	})
	t.Run("with an edited block: reported, untouched", func(t *testing.T) {
		projectDir, home := newProject(t)
		core, _ := install.SlotTemplate(slot.LangEN)
		content, _ := slot.Upsert(englishAgents, core, false)
		content = strings.Replace(content, "## Vexillum commander", "## My commander", 1)
		writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), content)
		r := doInit(projectDir, home, setupOptions{Yes: true}, "", false)
		if r.code != 0 || !strings.Contains(r.out, "edited by hand") {
			t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
		}
		if got := readFile(t, filepath.Join(projectDir, "AGENTS.md")); got != content {
			t.Error("init must not touch an edited block")
		}
	})
	t.Run("with a malformed block: reported, untouched", func(t *testing.T) {
		projectDir, home := newProject(t)
		content := englishAgents + "\n<!-- BEGIN VEXILLUM v:1 hash:00000000 -->\nhalf a block\n"
		writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), content)
		r := doInit(projectDir, home, setupOptions{Yes: true}, "", false)
		if r.code != 0 || !strings.Contains(r.out, "malformed") {
			t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
		}
		if got := readFile(t, filepath.Join(projectDir, "AGENTS.md")); got != content {
			t.Error("init must not touch a malformed block")
		}
	})
	t.Run("a rerun keeps the block's language", func(t *testing.T) {
		projectDir, home := newProject(t)
		writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), englishAgents)
		if r := doInit(projectDir, home, setupOptions{Yes: true, Lang: slot.LangES}, "", false); r.code != 0 {
			t.Fatal(r.out, r.errOut)
		}
		r := doInit(projectDir, home, setupOptions{Yes: true}, "", false)
		if !strings.Contains(r.out, "Nothing to change") {
			t.Errorf("rerun without --lang flipped the language: %s", r.out)
		}
	})
}

// CLAUDE.md: missing is created; importing is left alone; an existing one
// without the import is edited only after a question.
func TestInit_ClaudeMD(t *testing.T) {
	noSkills := setupOptions{Skills: boolp(false)}
	yesNoSkills := setupOptions{Yes: true, Skills: boolp(false)}
	t.Run("missing", func(t *testing.T) {
		projectDir, home := newProject(t)
		doInit(projectDir, home, yesNoSkills, "", false)
		if got := readFile(t, filepath.Join(projectDir, "CLAUDE.md")); got != "@AGENTS.md\n" {
			t.Errorf("CLAUDE.md = %q", got)
		}
	})
	t.Run("already importing", func(t *testing.T) {
		projectDir, home := newProject(t)
		const own = "# Mine\n\n@AGENTS.md\n"
		writeFileT(t, filepath.Join(projectDir, "CLAUDE.md"), own)
		doInit(projectDir, home, yesNoSkills, "", false)
		if got := readFile(t, filepath.Join(projectDir, "CLAUDE.md")); got != own {
			t.Errorf("CLAUDE.md changed: %q", got)
		}
	})
	t.Run("not importing, accepted", func(t *testing.T) {
		projectDir, home := newProject(t)
		writeFileT(t, filepath.Join(projectDir, "CLAUDE.md"), "# Mine\n")
		// Continue? / language / CLAUDE.md edit
		r := doInit(projectDir, home, noSkills, "y\ny\ny\n", true)
		if r.code != 0 {
			t.Fatal(r.out, r.errOut)
		}
		if got := readFile(t, filepath.Join(projectDir, "CLAUDE.md")); got != "# Mine\n@AGENTS.md\n" {
			t.Errorf("CLAUDE.md = %q", got)
		}
	})
	t.Run("not importing, declined: warns", func(t *testing.T) {
		projectDir, home := newProject(t)
		writeFileT(t, filepath.Join(projectDir, "CLAUDE.md"), "# Mine\n")
		r := doInit(projectDir, home, noSkills, "y\ny\nn\n", true)
		if r.code != 0 {
			t.Fatal(r.out, r.errOut)
		}
		if got := readFile(t, filepath.Join(projectDir, "CLAUDE.md")); got != "# Mine\n" {
			t.Errorf("CLAUDE.md edited despite the refusal: %q", got)
		}
		if !strings.Contains(r.errOut, "will not see the vexillum block") {
			t.Errorf("no warning: %s", r.errOut)
		}
		mustStat(t, filepath.Join(projectDir, "AGENTS.md"))
	})
	t.Run("a symlink to AGENTS.md counts as importing", func(t *testing.T) {
		projectDir, home := newProject(t)
		writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), englishAgents)
		if err := os.Symlink("AGENTS.md", filepath.Join(projectDir, "CLAUDE.md")); err != nil {
			t.Fatal(err)
		}
		doInit(projectDir, home, yesNoSkills, "", false)
		if info, _ := os.Lstat(filepath.Join(projectDir, "CLAUDE.md")); info.Mode()&os.ModeSymlink == 0 {
			t.Error("the CLAUDE.md symlink was replaced")
		}
		if !strings.Contains(readFile(t, filepath.Join(projectDir, "CLAUDE.md")), "BEGIN VEXILLUM") {
			t.Error("the block was not written through the symlink")
		}
	})
}

// models.json is created once and never overwritten.
func TestInit_ModelsNeverOverwritten(t *testing.T) {
	projectDir, home := newProject(t)
	custom := `{"default": {"effort": "high"}}`
	writeFileT(t, filepath.Join(projectDir, ".vexillum", "models.json"), custom)
	if r := doInit(projectDir, home, setupOptions{Yes: true}, "", false); r.code != 0 {
		t.Fatal(r.out, r.errOut)
	}
	if got := readFile(t, filepath.Join(projectDir, ".vexillum", "models.json")); got != custom {
		t.Errorf("models.json overwritten: %q", got)
	}
}

// Skills that are edited are never touched by init; stale ones are reported.
func TestInit_SkillsLeftAlone(t *testing.T) {
	projectDir, home := newProject(t)
	if r := doInit(projectDir, home, setupOptions{Yes: true}, "", false); r.code != 0 {
		t.Fatal(r.out, r.errOut)
	}
	skill := filepath.Join(projectDir, ".claude", "skills", "forum", "SKILL.md")
	edited := readFile(t, skill) + "\nmy note\n"
	writeFileT(t, skill, edited)
	r := doInit(projectDir, home, setupOptions{Yes: true}, "", false)
	if r.code != 0 || !strings.Contains(r.out, "Skill forum: edited by hand") {
		t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
	}
	if readFile(t, skill) != edited {
		t.Error("init overwrote an edited skill")
	}
}

// A symlinked skill directory (this repository dogfoods skills/) is never
// written through.
func TestInit_SymlinkedSkillIsLeftAlone(t *testing.T) {
	projectDir, home := newProject(t)
	src := filepath.Join(t.TempDir(), "my-forum")
	writeFileT(t, filepath.Join(src, "SKILL.md"), "---\nname: forum\n---\ncustom\n")
	if err := os.MkdirAll(filepath.Join(projectDir, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(src, filepath.Join(projectDir, ".claude", "skills", "forum")); err != nil {
		t.Fatal(err)
	}
	r := doInit(projectDir, home, setupOptions{Yes: true}, "", false)
	if r.code != 0 {
		t.Fatal(r.out, r.errOut)
	}
	if got := readFile(t, filepath.Join(src, "SKILL.md")); !strings.Contains(got, "custom") {
		t.Error("init wrote through a symlinked skill")
	}
	mustStat(t, filepath.Join(projectDir, ".claude", "skills", "vexillum", "SKILL.md"))
}

func TestInit_OldRulesFileHint(t *testing.T) {
	projectDir, home := newProject(t)
	writeFileT(t, filepath.Join(projectDir, ".claude", "rules", "vexillum.md"), "# old\n")
	r := doInit(projectDir, home, setupOptions{Yes: true}, "", false)
	if !strings.Contains(r.out, "vx upgrade") || readFile(t, filepath.Join(projectDir, ".claude", "rules", "vexillum.md")) != "# old\n" {
		t.Errorf("init must leave the old rules file and point at upgrade: %s", r.out)
	}
}

func TestInit_FlagParsing(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		wantErr string
	}{
		{[]string{"--skills", "--no-skills"}, "contradict"},
		{[]string{"--lang", "fr"}, "unsupported language"},
		{[]string{"--lang"}, "needs a value"},
		{[]string{"--force"}, "unknown init flag"},
		{[]string{"--bogus"}, "unknown init flag"},
	} {
		_, _, err := parseSetupArgs(setupInit, tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%v: err = %v, want %q", tc.args, err, tc.wantErr)
		}
	}
	opts, _, err := parseSetupArgs(setupInit, []string{"--yes", "--lang=es", "--no-skills", "--global"})
	if err != nil || !opts.Yes || opts.Lang != slot.LangES || opts.Skills == nil || *opts.Skills || !opts.Global {
		t.Errorf("opts = %+v, err = %v", opts, err)
	}
	if opts, _, err := parseSetupArgs(setupUpgrade, []string{"--force"}); err != nil || !opts.Force {
		t.Errorf("upgrade --force: %+v %v", opts, err)
	}
}

func TestInit_OutsideGitRepo(t *testing.T) {
	projectDir := t.TempDir()
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")
	r := doInit(projectDir, vexillumHome, setupOptions{Yes: true}, "", false)
	if r.code == 0 || r.errOut == "" {
		t.Fatal("expected a failure with a message outside a git repository")
	}
	notExist(t, vexillumHome)
	notExist(t, filepath.Join(projectDir, ".vexillum"))
}

func TestInit_VexillumHomeNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, permission checks don't apply")
	}
	projectDir, _ := newProject(t)
	readOnlyParent := t.TempDir()
	if err := os.Chmod(readOnlyParent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnlyParent, 0o700) })
	vexillumHome := filepath.Join(readOnlyParent, ".vexillum")

	r := doInit(projectDir, vexillumHome, setupOptions{Yes: true}, "", false)
	if r.code == 0 || !strings.Contains(r.errOut, vexillumHome) {
		t.Fatalf("exit %d, stderr: %s", r.code, r.errOut)
	}
	notExist(t, filepath.Join(projectDir, ".vexillum"))
}

func TestInit_RecreatesMissingVexillumHome(t *testing.T) {
	projectDir, home := newProject(t)
	if r := doInit(projectDir, home, setupOptions{Yes: true}, "", false); r.code != 0 {
		t.Fatal(r.out, r.errOut)
	}
	before := readFile(t, filepath.Join(projectDir, ".vexillum", "config.json"))
	if err := os.RemoveAll(home); err != nil {
		t.Fatal(err)
	}
	// Nothing in the project needs writing, so the home is not required to
	// exist for an idempotent rerun; a change forces it to be recreated.
	if err := os.Remove(filepath.Join(projectDir, ".vexillum", "models.json")); err != nil {
		t.Fatal(err)
	}
	if r := doInit(projectDir, home, setupOptions{Yes: true}, "", false); r.code != 0 {
		t.Fatal(r.out, r.errOut)
	}
	mustStat(t, home)
	if readFile(t, filepath.Join(projectDir, ".vexillum", "config.json")) != before {
		t.Error("config.json changed")
	}
}

func TestInit_RefusesInsideVexillumHome(t *testing.T) {
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")
	campPath := filepath.Join(vexillumHome, "myproject-abc12345", "1", "myproject")
	if err := os.MkdirAll(campPath, 0o755); err != nil {
		t.Fatal(err)
	}
	r := doInit(campPath, vexillumHome, setupOptions{Yes: true}, "", false)
	if r.code == 0 || r.errOut == "" {
		t.Fatal("expected a refusal inside a camp")
	}
	notExist(t, filepath.Join(campPath, ".vexillum"))
}

func TestInit_IgnoresForumArtifacts(t *testing.T) {
	projectDir, home := newProject(t)
	if r := doInit(projectDir, home, setupOptions{Yes: true}, "", false); r.code != 0 {
		t.Fatal(r.out, r.errOut)
	}
	ignorePath := filepath.Join(projectDir, ".vexillum", ".gitignore")
	if got := readFile(t, ignorePath); got != "forum/\n" {
		t.Errorf(".vexillum/.gitignore = %q", got)
	}
	if out, err := exec.Command("git", "-C", projectDir, "check-ignore", ".vexillum/forum/plan.html").CombinedOutput(); err != nil {
		t.Errorf("forum artifact not ignored: %v %s", err, out)
	}
	if err := exec.Command("git", "-C", projectDir, "check-ignore", "-q", ".vexillum/config.json").Run(); err == nil {
		t.Error(".vexillum/config.json must not be ignored")
	}
	writeFileT(t, ignorePath, "custom\n")
	if r := doInit(projectDir, home, setupOptions{Yes: true}, "", false); r.code != 0 {
		t.Fatal(r.out)
	}
	if readFile(t, ignorePath) != "custom\n" {
		t.Error("edited .vexillum/.gitignore overwritten")
	}
	os.Remove(ignorePath)
	doInit(projectDir, home, setupOptions{Yes: true}, "", false)
	mustStat(t, ignorePath)
}

// Global: the same core body goes to ~/.claude/rules/vexillum.md, the skills
// to ~/.claude/skills/, after the same consent flow.
func TestInitGlobal(t *testing.T) {
	t.Run("yes", func(t *testing.T) {
		vexillumHome := filepath.Join(t.TempDir(), ".vexillum")
		home := t.TempDir()
		env, out, errOut := setupTestEnv(setupOptions{Yes: true}, "", false)
		if code := runInitGlobal(env, vexillumHome, home); code != 0 {
			t.Fatalf("%s%s", out, errOut)
		}
		core, _ := install.SlotTemplate(slot.LangEN)
		if got := readFile(t, filepath.Join(home, ".claude", "rules", "vexillum.md")); got != core {
			t.Error("global rules file is not the core body")
		}
		for _, n := range skills.Names() {
			mustStat(t, filepath.Join(home, ".claude", "skills", n, "SKILL.md"))
		}
		cfg, _ := scaffold.ReadConfig(vexillumHome)
		if cfg.VexillumRuleHash != scaffold.HashContent(core) || cfg.Skills["vexillum"] == "" {
			t.Errorf("config = %+v", cfg)
		}
		notExist(t, filepath.Join(home, ".vexillum"))
		notExist(t, filepath.Join(home, "AGENTS.md"))

		env, out, errOut = setupTestEnv(setupOptions{Yes: true}, "", false)
		if code := runInitGlobal(env, vexillumHome, home); code != 0 || !strings.Contains(out.String(), "Nothing to change") {
			t.Errorf("rerun: %s%s", out, errOut)
		}
	})
	t.Run("non tty without yes refuses", func(t *testing.T) {
		vexillumHome := filepath.Join(t.TempDir(), ".vexillum")
		home := t.TempDir()
		env, _, errOut := setupTestEnv(setupOptions{}, "", false)
		if code := runInitGlobal(env, vexillumHome, home); code == 0 || !strings.Contains(errOut.String(), "--yes") {
			t.Errorf("code %d, %s", code, errOut)
		}
		notExist(t, filepath.Join(home, ".claude"))
	})
	t.Run("interactive spanish, skills declined", func(t *testing.T) {
		vexillumHome := filepath.Join(t.TempDir(), ".vexillum")
		home := t.TempDir()
		env, out, errOut := setupTestEnv(setupOptions{}, "y\nn\nes\nn\n", true)
		if code := runInitGlobal(env, vexillumHome, home); code != 0 {
			t.Fatalf("%s%s", out, errOut)
		}
		core, _ := install.SlotTemplate(slot.LangES)
		if readFile(t, filepath.Join(home, ".claude", "rules", "vexillum.md")) != core {
			t.Error("expected the Spanish core")
		}
		notExist(t, filepath.Join(home, ".claude", "skills"))
	})
	t.Run("edited rules file is left alone", func(t *testing.T) {
		vexillumHome := filepath.Join(t.TempDir(), ".vexillum")
		home := t.TempDir()
		env, _, _ := setupTestEnv(setupOptions{Yes: true}, "", false)
		runInitGlobal(env, vexillumHome, home)
		path := filepath.Join(home, ".claude", "rules", "vexillum.md")
		writeFileT(t, path, "# mine\n")
		env, out, _ := setupTestEnv(setupOptions{Yes: true}, "", false)
		if code := runInitGlobal(env, vexillumHome, home); code != 0 {
			t.Fatal(out)
		}
		if readFile(t, path) != "# mine\n" {
			t.Error("edited global rules file overwritten")
		}
	})
}

// vx init adds the sentinel Stop hook to a fresh .claude/settings.json.
func TestInit_AddsSentinelStopHook(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var stdout, stderr bytes.Buffer
	if code := initYes(projectDir, vexillumHome, &stdout, &stderr); code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}

	added, err := ensureSentinelHook(projectDir)
	if err != nil {
		t.Fatalf("ensureSentinelHook (re-check): %v", err)
	}
	if added {
		t.Error("expected the hook to already be present (init should have added it), but ensureSentinelHook added it again")
	}

	data, err := os.ReadFile(filepath.Join(projectDir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("reading .claude/settings.json: %v", err)
	}
	findStopHookEntry(t, parseSettings(t, string(data)), sentinelHookCommand)
}

// The hook init writes has the async fields set - verified live that a
// real asyncRewake Stop hook wakes an idle Claude Code session with no
// new user prompt, which a synchronous-only hook cannot do.
func TestInit_SentinelStopHookIsAsync(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var stdout, stderr bytes.Buffer
	if code := initYes(projectDir, vexillumHome, &stdout, &stderr); code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}

	data, err := os.ReadFile(filepath.Join(projectDir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("reading .claude/settings.json: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parsing settings.json: %v", err)
	}
	entry := findStopHookEntry(t, settings, sentinelHookCommand)
	if asyncRewake, _ := entry["asyncRewake"].(bool); !asyncRewake {
		t.Errorf("expected asyncRewake: true on the sentinel hook, got: %+v", entry)
	}
	if entry["timeout"] == nil {
		t.Errorf("expected a timeout on the sentinel hook, got: %+v", entry)
	}
}

// A project initialized with an older vexillum has the synchronous-only
// hook (legacySentinelHookCommand, no asyncRewake) registered.
// ensureSentinelHook must upgrade it in place - replace it with the new
// async hook - not leave it as a stale duplicate alongside the new one.
func TestEnsureSentinelHook_UpgradesLegacySyncHook(t *testing.T) {
	projectDir := t.TempDir()
	settingsDir := filepath.Join(projectDir, ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := `{
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "vexillum sentinel drain"}]}]
  }
}`
	settingsPath := filepath.Join(settingsDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(legacy), 0o644); err != nil {
		t.Fatalf("writing legacy settings: %v", err)
	}

	added, err := ensureSentinelHook(projectDir)
	if err != nil {
		t.Fatalf("ensureSentinelHook: %v", err)
	}
	if !added {
		t.Fatal("expected the legacy hook to be upgraded (reported as a change)")
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("reading settings.json: %v", err)
	}
	if bytes.Contains(data, []byte(legacySentinelHookCommand)) {
		t.Errorf("expected the legacy hook command to be gone, got: %s", data)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parsing settings.json: %v", err)
	}
	stopGroups, _ := settings["hooks"].(map[string]any)["Stop"].([]any)
	entryCount := 0
	for _, g := range stopGroups {
		group, _ := g.(map[string]any)
		entries, _ := group["hooks"].([]any)
		entryCount += len(entries)
	}
	if entryCount != 1 {
		t.Errorf("expected exactly one Stop hook entry after upgrading, got %d", entryCount)
	}

	entry := findStopHookEntry(t, settings, sentinelHookCommand)
	if asyncRewake, _ := entry["asyncRewake"].(bool); !asyncRewake {
		t.Errorf("expected the upgraded hook to have asyncRewake: true, got: %+v", entry)
	}

	// Re-running is a no-op: the upgraded hook is already current.
	addedAgain, err := ensureSentinelHook(projectDir)
	if err != nil {
		t.Fatalf("ensureSentinelHook (second call): %v", err)
	}
	if addedAgain {
		t.Error("expected the second call to be a no-op after the upgrade")
	}
}

func parseSettings(t *testing.T, content string) map[string]any {
	t.Helper()
	var settings map[string]any
	if err := json.Unmarshal([]byte(content), &settings); err != nil {
		t.Fatalf("parsing settings.json: %v", err)
	}
	return settings
}

func findStopHookEntry(t *testing.T, settings map[string]any, command string) map[string]any {
	t.Helper()
	hooks, _ := settings["hooks"].(map[string]any)
	stopGroups, _ := hooks["Stop"].([]any)
	for _, g := range stopGroups {
		group, ok := g.(map[string]any)
		if !ok {
			continue
		}
		entries, _ := group["hooks"].([]any)
		for _, e := range entries {
			entry, ok := e.(map[string]any)
			if !ok {
				continue
			}
			if cmd, _ := entry["command"].(string); cmd == command {
				return entry
			}
		}
	}
	t.Fatalf("no Stop hook entry found with command %q in %+v", command, settings)
	return nil
}

// ensureSentinelHook preserves existing settings and hooks, and never
// adds the hook twice.
func TestEnsureSentinelHook_MergesAndDedupes(t *testing.T) {
	projectDir := t.TempDir()
	settingsDir := filepath.Join(projectDir, ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := `{
  "model": "sonnet",
  "hooks": {
    "PostToolUse": [{"matcher": "Write", "hooks": [{"type": "command", "command": "prettier --write"}]}]
  }
}`
	settingsPath := filepath.Join(settingsDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(existing), 0o644); err != nil {
		t.Fatalf("writing existing settings: %v", err)
	}

	added, err := ensureSentinelHook(projectDir)
	if err != nil {
		t.Fatalf("ensureSentinelHook: %v", err)
	}
	if !added {
		t.Fatal("expected the hook to be added")
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("reading settings.json: %v", err)
	}
	if !bytes.Contains(data, []byte(`"model": "sonnet"`)) {
		t.Errorf("expected existing model setting to survive the merge, got: %s", data)
	}
	if !bytes.Contains(data, []byte("prettier --write")) {
		t.Errorf("expected existing PostToolUse hook to survive the merge, got: %s", data)
	}
	findStopHookEntry(t, parseSettings(t, string(data)), sentinelHookCommand)

	addedAgain, err := ensureSentinelHook(projectDir)
	if err != nil {
		t.Fatalf("ensureSentinelHook (second call): %v", err)
	}
	if addedAgain {
		t.Error("expected the second call to be a no-op, not add a duplicate hook")
	}
}

// ensureSentinelHook refuses to touch a settings.json with invalid JSON,
// rather than risk corrupting it.
func TestEnsureSentinelHook_RefusesMalformedSettings(t *testing.T) {
	projectDir := t.TempDir()
	settingsDir := filepath.Join(projectDir, ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.json")
	malformed := []byte("{not valid json")
	if err := os.WriteFile(settingsPath, malformed, 0o644); err != nil {
		t.Fatalf("writing malformed settings: %v", err)
	}

	if _, err := ensureSentinelHook(projectDir); err == nil {
		t.Fatal("expected an error for malformed settings.json")
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("reading settings.json: %v", err)
	}
	if !bytes.Equal(data, malformed) {
		t.Errorf("expected the malformed file to be left untouched, got: %s", data)
	}
}
