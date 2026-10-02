package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/install"
	"github.com/isaias-alt/vexillum/internal/scaffold"
	"github.com/isaias-alt/vexillum/internal/slot"
	"github.com/isaias-alt/vexillum/skills"
)

// copyTree copies the directory src into dst (created), files only, keeping
// relative paths.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		writeFileT(t, filepath.Join(dst, rel), string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// oldScaffoldProject lays down a project exactly as the previous vexillum
// left it: .claude/rules/vexillum.md with its hash in config.json, the
// sentinel hook in settings.json, a Spanish AGENTS.md and a CLAUDE.md that
// imports it. The fixture is a golden copy of that output.
func oldScaffoldProject(t *testing.T) (projectDir, vexillumHome string) {
	t.Helper()
	projectDir, vexillumHome = newProject(t)
	copyTree(t, filepath.Join("testdata", "old-scaffold"), projectDir)
	return projectDir, vexillumHome
}

func TestUpgrade_RefusesUninitializedProject(t *testing.T) {
	projectDir, home := newProject(t)
	r := doUpgrade(projectDir, home, setupOptions{Yes: true}, "", false)
	if r.code == 0 || !strings.Contains(r.errOut, "init") {
		t.Fatalf("exit %d: %s", r.code, r.errOut)
	}
	notExist(t, filepath.Join(projectDir, "AGENTS.md"))
}

func TestUpgrade_RefusesInsideVexillumHome(t *testing.T) {
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")
	camp := filepath.Join(vexillumHome, "p-abc12345", "1", "p")
	if err := os.MkdirAll(camp, 0o755); err != nil {
		t.Fatal(err)
	}
	if r := doUpgrade(camp, vexillumHome, setupOptions{Yes: true}, "", false); r.code == 0 {
		t.Fatal("expected a refusal inside a camp")
	}
}

// Golden: a project initialized with the OLD scaffold (rules file + hook) is
// migrated: the unedited rules file goes, the block and the skills arrive,
// the user's own text and settings are kept.
func TestUpgrade_MigratesOldScaffold(t *testing.T) {
	projectDir, home := oldScaffoldProject(t)
	userAgents := readFile(t, filepath.Join(projectDir, "AGENTS.md"))
	claudeBefore := readFile(t, filepath.Join(projectDir, "CLAUDE.md"))

	r := doUpgrade(projectDir, home, setupOptions{Yes: true}, "", false)
	if r.code != 0 {
		t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
	}

	notExist(t, filepath.Join(projectDir, ".claude", "rules", "vexillum.md"))
	notExist(t, filepath.Join(projectDir, ".claude", "rules"))

	agents := readFile(t, filepath.Join(projectDir, "AGENTS.md"))
	if !strings.HasPrefix(agents, userAgents) {
		t.Error("the user's AGENTS.md text was changed")
	}
	core, _ := install.SlotTemplate(slot.LangES)
	if ins := slot.Inspect(agents, core); ins.State != slot.StateCurrent {
		t.Errorf("block is %v, want current in Spanish", ins.State)
	}
	if readFile(t, filepath.Join(projectDir, "CLAUDE.md")) != claudeBefore {
		t.Error("CLAUDE.md already imported AGENTS.md and must not change")
	}

	var settings map[string]any
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(projectDir, ".claude", "settings.json"))), &settings); err != nil {
		t.Fatal(err)
	}
	if settings["permissions"] == nil || strings.Count(readFile(t, filepath.Join(projectDir, ".claude", "settings.json")), sentinelHookCommand) != 1 {
		t.Errorf("settings.json not preserved: %v", settings)
	}

	for _, n := range skills.Names() {
		mustStat(t, filepath.Join(projectDir, ".claude", "skills", n, "SKILL.md"))
	}
	mustStat(t, filepath.Join(projectDir, ".vexillum", "models.json"))

	cfg, err := scaffold.ReadConfig(filepath.Join(projectDir, ".vexillum"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VexillumRuleHash != "" || cfg.Skills["vexillum"] == "" {
		t.Errorf("config after migration = %+v", cfg)
	}

	// Running it again is a no-op.
	before, _ := snapshotTree(t, projectDir)
	again := doUpgrade(projectDir, home, setupOptions{Yes: true}, "", false)
	after, _ := snapshotTree(t, projectDir)
	if again.code != 0 || !strings.Contains(again.out, "Nothing to change") || before != after {
		t.Errorf("second upgrade: exit %d, %s", again.code, again.out)
	}
}

// The notice lists the removal, and without --yes and a terminal nothing
// happens.
func TestUpgrade_NoticeAndNonTTY(t *testing.T) {
	projectDir, home := oldScaffoldProject(t)
	before, _ := snapshotTree(t, projectDir)
	r := doUpgrade(projectDir, home, setupOptions{}, "", false)
	if r.code == 0 || !strings.Contains(r.out, ".claude/rules/vexillum.md") || !strings.Contains(r.errOut, "--yes") {
		t.Fatalf("exit %d\nout: %s\nerr: %s", r.code, r.out, r.errOut)
	}
	if after, _ := snapshotTree(t, projectDir); before != after {
		t.Error("a refused upgrade changed files")
	}
	r = doUpgrade(projectDir, home, setupOptions{}, "n\n", true)
	if r.code != 0 || !strings.Contains(r.out, "Nothing was changed.") {
		t.Fatalf("declined: exit %d, %s", r.code, r.out)
	}
	if after, _ := snapshotTree(t, projectDir); before != after {
		t.Error("a declined upgrade changed files")
	}
}

// An edited rules file, or one of unknown provenance, is left alone and
// reported as redundant.
func TestUpgrade_EditedRulesFileKept(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, projectDir string)
	}{
		{"edited", func(t *testing.T, p string) {
			path := filepath.Join(p, ".claude", "rules", "vexillum.md")
			writeFileT(t, path, readFile(t, path)+"\nmy rule\n")
		}},
		{"no recorded hash", func(t *testing.T, p string) {
			writeFileT(t, filepath.Join(p, ".vexillum", "config.json"), `{"version":1,"initialized_at":"2026-01-01T00:00:00Z"}`)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectDir, home := oldScaffoldProject(t)
			tc.mutate(t, projectDir)
			rules := filepath.Join(projectDir, ".claude", "rules", "vexillum.md")
			before := readFile(t, rules)
			r := doUpgrade(projectDir, home, setupOptions{Yes: true}, "", false)
			if r.code != 0 || !strings.Contains(r.out, "redundant") {
				t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
			}
			if readFile(t, rules) != before {
				t.Error("the rules file must be kept")
			}
			// The migration itself still happened.
			if ins := slotInAgents(t, projectDir); ins.State == slot.StateAbsent {
				t.Error("the block was not written")
			}
			// And upgrade --force does not delete it either.
			doUpgrade(projectDir, home, setupOptions{Yes: true, Force: true}, "", false)
			if readFile(t, rules) != before {
				t.Error("--force must not remove a rules file the user edited")
			}
		})
	}
}

func slotInAgents(t *testing.T, projectDir string) slot.Inspection {
	t.Helper()
	return slot.Inspect(readFile(t, filepath.Join(projectDir, "AGENTS.md")), "")
}

// initedProject returns a project that init --yes set up.
func initedProject(t *testing.T, opts setupOptions) (projectDir, home string) {
	t.Helper()
	projectDir, home = newProject(t)
	opts.Yes = true
	if r := doInit(projectDir, home, opts, "", false); r.code != 0 {
		t.Fatalf("init: %s%s", r.out, r.errOut)
	}
	return projectDir, home
}

func TestUpgrade_BlockStateTable(t *testing.T) {
	core, _ := install.SlotTemplate(slot.LangEN)
	// A block as an older version wrote it: untouched (hash matches) but not the template.
	oldBody := "## Vexillum commander\n\nOld text.\n"
	stale, _ := slot.Upsert(englishAgents, oldBody, false)
	current, _ := slot.Upsert(englishAgents, core, false)
	edited := strings.Replace(current, "## Vexillum commander", "## My commander", 1)
	malformed := englishAgents + "\n<!-- BEGIN VEXILLUM v:1 hash:00000000 -->\nhalf a block\n"
	dup := current + "\n" + current

	tests := []struct {
		name    string
		agents  string
		opts    setupOptions
		stdin   string
		tty     bool
		want    string // expected final AGENTS.md; "" means current-block check
		wantOut string
	}{
		{name: "stale is refreshed", agents: stale, opts: setupOptions{Yes: true}, wantOut: "Updated the vexillum block"},
		{name: "current is untouched", agents: current, opts: setupOptions{Yes: true}, want: current, wantOut: "Nothing to change"},
		{name: "drifted: diff, no overwrite", agents: edited, opts: setupOptions{Yes: true}, want: edited, wantOut: "--- your edit"},
		{name: "drifted with --force: backup then replace", agents: edited, opts: setupOptions{Yes: true, Force: true}, wantOut: "Saved your edited block"},
		{name: "malformed, repairs confirmed", agents: malformed, opts: setupOptions{}, stdin: "y\ny\ny\n", tty: true, wantOut: "remove orphan BEGIN marker"},
		{name: "malformed, repairs declined", agents: malformed, opts: setupOptions{}, stdin: "y\ny\nn\n", tty: true, want: malformed, wantOut: "left as it is"},
		{name: "malformed, --yes accepts the repairs", agents: malformed, opts: setupOptions{Yes: true}, wantOut: "Saved the file as it was"},
		{name: "duplicate blocks", agents: dup, opts: setupOptions{Yes: true}, wantOut: "remove duplicate block"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projectDir, home := initedProject(t, setupOptions{Skills: boolp(false)})
			agents := filepath.Join(projectDir, "AGENTS.md")
			writeFileT(t, agents, tc.agents)
			opts := tc.opts
			opts.Skills = boolp(false)
			r := doUpgrade(projectDir, home, opts, tc.stdin, tc.tty)
			if r.code != 0 {
				t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
			}
			if !strings.Contains(r.out, tc.wantOut) {
				t.Errorf("output lacks %q:\n%s", tc.wantOut, r.out)
			}
			got := readFile(t, agents)
			if tc.want != "" {
				if got != tc.want {
					t.Errorf("AGENTS.md changed:\n%s", got)
				}
				return
			}
			if ins := slot.Inspect(got, core); ins.State != slot.StateCurrent {
				t.Errorf("block is %v, want current:\n%s", ins.State, got)
			}
			if !strings.HasPrefix(got, englishAgents) {
				t.Error("user text was changed")
			}
		})
	}

	t.Run("force saves the user's version", func(t *testing.T) {
		projectDir, home := initedProject(t, setupOptions{Skills: boolp(false)})
		writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), edited)
		doUpgrade(projectDir, home, setupOptions{Yes: true, Force: true, Skills: boolp(false)}, "", false)
		if b := readFile(t, filepath.Join(projectDir, filepath.FromSlash(slot.BackupRelPath))); !strings.Contains(b, "## My commander") {
			t.Errorf("backup = %q", b)
		}
	})
	t.Run("repair backs up the whole file", func(t *testing.T) {
		projectDir, home := initedProject(t, setupOptions{Skills: boolp(false)})
		writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), malformed)
		doUpgrade(projectDir, home, setupOptions{Yes: true, Skills: boolp(false)}, "", false)
		if b := readFile(t, filepath.Join(projectDir, filepath.FromSlash(install.AgentsBackupRelPath))); b != malformed {
			t.Errorf("backup = %q", b)
		}
	})
}

func TestUpgrade_LangFlagSwitchesLanguage(t *testing.T) {
	projectDir, home := initedProject(t, setupOptions{Skills: boolp(false)})
	if r := doUpgrade(projectDir, home, setupOptions{Yes: true, Lang: slot.LangES, Skills: boolp(false)}, "", false); r.code != 0 {
		t.Fatal(r.out, r.errOut)
	}
	core, _ := install.SlotTemplate(slot.LangES)
	if ins := slot.Inspect(readFile(t, filepath.Join(projectDir, "AGENTS.md")), core); ins.State != slot.StateCurrent {
		t.Errorf("block is %v, want current in es", ins.State)
	}
}

func TestUpgrade_ClaudeMDWithoutImport(t *testing.T) {
	projectDir, home := oldScaffoldProject(t)
	writeFileT(t, filepath.Join(projectDir, "CLAUDE.md"), "# Mine\n")
	// Continue? / language / CLAUDE.md edit / skills
	r := doUpgrade(projectDir, home, setupOptions{}, "y\ny\ny\nn\n", true)
	if r.code != 0 {
		t.Fatal(r.out, r.errOut)
	}
	if got := readFile(t, filepath.Join(projectDir, "CLAUDE.md")); got != "# Mine\n@AGENTS.md\n" {
		t.Errorf("CLAUDE.md = %q", got)
	}
	notExist(t, filepath.Join(projectDir, ".claude", "skills"))
}

func TestUpgrade_ModelsNeverOverwritten(t *testing.T) {
	projectDir, home := oldScaffoldProject(t)
	custom := `{"default": {"model": "opus"}}`
	writeFileT(t, filepath.Join(projectDir, ".vexillum", "models.json"), custom)
	doUpgrade(projectDir, home, setupOptions{Yes: true, Force: true}, "", false)
	if got := readFile(t, filepath.Join(projectDir, ".vexillum", "models.json")); got != custom {
		t.Errorf("models.json overwritten: %q", got)
	}
}

// Skills: an untouched older copy is refreshed (and its leftover files
// removed), an edited one is reported and skipped, --force overwrites it
// after a backup, and --no-skills leaves everything.
func TestUpgrade_Skills(t *testing.T) {
	// makeStale rewrites forum as an "older version": different content plus
	// a file the current version no longer has, with its hash recorded as
	// if vexillum had installed it.
	makeStale := func(t *testing.T, projectDir string) string {
		dir := filepath.Join(projectDir, ".claude", "skills", "forum")
		writeFileT(t, filepath.Join(dir, "SKILL.md"), "---\nname: forum\ndescription: old\n---\nold\n")
		writeFileT(t, filepath.Join(dir, "playbooks", "gone.md"), "removed upstream\n")
		files, err := os.ReadDir(filepath.Join(dir, "playbooks"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if f.Name() != "gone.md" {
				os.Remove(filepath.Join(dir, "playbooks", f.Name()))
			}
		}
		st, err := install.InspectSkill(filepath.Join(projectDir, ".claude", "skills"), "forum", "")
		if err != nil {
			t.Fatal(err)
		}
		cfg, _ := scaffold.ReadConfig(filepath.Join(projectDir, ".vexillum"))
		cfg.Skills["forum"] = st.Hash
		if err := scaffold.SaveConfig(filepath.Join(projectDir, ".vexillum"), cfg); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	t.Run("stale is refreshed", func(t *testing.T) {
		projectDir, home := initedProject(t, setupOptions{})
		dir := makeStale(t, projectDir)
		r := doUpgrade(projectDir, home, setupOptions{Yes: true}, "", false)
		if r.code != 0 || !strings.Contains(r.out, "Refreshed skill forum") {
			t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
		}
		st, _ := install.InspectSkill(filepath.Join(projectDir, ".claude", "skills"), "forum", "")
		if st.State != install.SkillCurrent {
			t.Errorf("forum is %v after refresh", st.State)
		}
		notExist(t, filepath.Join(dir, "playbooks", "gone.md"))
		cfg, _ := scaffold.ReadConfig(filepath.Join(projectDir, ".vexillum"))
		if want, _ := skills.Hash("forum"); cfg.Skills["forum"] != want {
			t.Error("config hash not updated")
		}
	})
	t.Run("edited is skipped, force overwrites after a backup", func(t *testing.T) {
		projectDir, home := initedProject(t, setupOptions{})
		skill := filepath.Join(projectDir, ".claude", "skills", "muster", "SKILL.md")
		edited := readFile(t, skill) + "\nmine\n"
		writeFileT(t, skill, edited)

		r := doUpgrade(projectDir, home, setupOptions{Yes: true}, "", false)
		if r.code != 0 || !strings.Contains(r.out, "Skill muster: edited by hand") || readFile(t, skill) != edited {
			t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
		}

		r = doUpgrade(projectDir, home, setupOptions{Yes: true, Force: true}, "", false)
		if r.code != 0 || !strings.Contains(r.out, "Overwrote skill muster") {
			t.Fatalf("force: %s%s", r.out, r.errOut)
		}
		if readFile(t, skill) == edited {
			t.Error("--force did not overwrite")
		}
		backup := readFile(t, filepath.Join(projectDir, ".vexillum", "backups", "skills", "muster", "SKILL.md"))
		if backup != edited {
			t.Error("backup does not hold the edited version")
		}
	})
	t.Run("--no-skills leaves them alone", func(t *testing.T) {
		projectDir, home := initedProject(t, setupOptions{})
		dir := makeStale(t, projectDir)
		before := readFile(t, filepath.Join(dir, "SKILL.md"))
		doUpgrade(projectDir, home, setupOptions{Yes: true, Skills: boolp(false)}, "", false)
		if readFile(t, filepath.Join(dir, "SKILL.md")) != before {
			t.Error("--no-skills refreshed a skill")
		}
	})
	t.Run("a skill removed on purpose is not brought back", func(t *testing.T) {
		projectDir, home := initedProject(t, setupOptions{})
		if err := os.RemoveAll(filepath.Join(projectDir, ".claude", "skills", "muster")); err != nil {
			t.Fatal(err)
		}
		r := doUpgrade(projectDir, home, setupOptions{Yes: true}, "", false)
		if r.code != 0 || !strings.Contains(r.out, "Nothing to change") {
			t.Fatalf("%s%s", r.out, r.errOut)
		}
		notExist(t, filepath.Join(projectDir, ".claude", "skills", "muster"))
		// --skills brings it back.
		doUpgrade(projectDir, home, setupOptions{Yes: true, Skills: boolp(true)}, "", false)
		mustStat(t, filepath.Join(projectDir, ".claude", "skills", "muster", "SKILL.md"))
	})
}

// A project initialized before the command was renamed has the old rules
// (hash recorded) and a Stop hook that runs the old executable name.
func TestUpgrade_MigratesProjectInitializedBeforeTheRename(t *testing.T) {
	oldRules, err := os.ReadFile(filepath.Join("testdata", "commander-rules-pre-vx.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(oldRules), "`vexillum dispatch`") {
		t.Fatal("fixture should be the template from before the rename")
	}
	sum := sha256.Sum256(oldRules)

	for _, legacyHook := range []string{"vexillum sentinel await", "vexillum sentinel drain"} {
		t.Run(legacyHook, func(t *testing.T) {
			projectDir, home := newProject(t)
			cfg, _ := json.Marshal(map[string]any{
				"version":            1,
				"initialized_at":     "2026-01-01T00:00:00Z",
				"vexillum_rule_hash": hex.EncodeToString(sum[:]),
			})
			writeFileT(t, filepath.Join(projectDir, ".vexillum", "config.json"), string(cfg))
			writeFileT(t, filepath.Join(projectDir, ".claude", "rules", "vexillum.md"), string(oldRules))
			writeFileT(t, filepath.Join(projectDir, ".claude", "settings.json"), `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"`+legacyHook+`","asyncRewake":true,"timeout":3600}]}]}}`)

			r := doUpgrade(projectDir, home, setupOptions{Yes: true}, "", false)
			if r.code != 0 {
				t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
			}
			notExist(t, filepath.Join(projectDir, ".claude", "rules", "vexillum.md"))
			settings := readFile(t, filepath.Join(projectDir, ".claude", "settings.json"))
			if strings.Contains(settings, "vexillum sentinel") || strings.Count(settings, sentinelHookCommand) != 1 {
				t.Errorf("hook not migrated: %s", settings)
			}
		})
	}
}

// Global: an unedited rules file from the previous version is replaced by the
// core; an edited one is kept unless --force, which saves a backup.
func TestUpgradeGlobal(t *testing.T) {
	setup := func(t *testing.T) (vexillumHome, home, rules string) {
		t.Helper()
		vexillumHome = filepath.Join(t.TempDir(), ".vexillum")
		home = t.TempDir()
		rules = filepath.Join(home, ".claude", "rules", "vexillum.md")
		old, err := os.ReadFile(filepath.Join("testdata", "old-scaffold", ".claude", "rules", "vexillum.md"))
		if err != nil {
			t.Fatal(err)
		}
		writeFileT(t, rules, string(old))
		if err := scaffold.WriteConfig(vexillumHome); err != nil {
			t.Fatal(err)
		}
		cfg, _ := scaffold.ReadConfig(vexillumHome)
		cfg.VexillumRuleHash = scaffold.HashContent(string(old))
		if err := scaffold.SaveConfig(vexillumHome, cfg); err != nil {
			t.Fatal(err)
		}
		return
	}
	run := func(opts setupOptions, vexillumHome, home string) (int, string) {
		env, out, errOut := setupTestEnv(opts, "", false)
		code := runUpgradeGlobal(env, vexillumHome, home)
		return code, out.String() + errOut.String()
	}

	t.Run("refuses uninitialized", func(t *testing.T) {
		if code, out := run(setupOptions{Yes: true}, filepath.Join(t.TempDir(), ".vexillum"), t.TempDir()); code == 0 || !strings.Contains(out, "init --global") {
			t.Errorf("%d %s", code, out)
		}
	})
	t.Run("unedited is replaced by the core, skills installed", func(t *testing.T) {
		vexillumHome, home, rules := setup(t)
		if code, out := run(setupOptions{Yes: true}, vexillumHome, home); code != 0 {
			t.Fatal(out)
		}
		core, _ := install.SlotTemplate(slot.LangEN)
		if readFile(t, rules) != core {
			t.Error("rules file is not the core")
		}
		mustStat(t, filepath.Join(home, ".claude", "skills", "vexillum", "SKILL.md"))
		cfg, _ := scaffold.ReadConfig(vexillumHome)
		if cfg.VexillumRuleHash != scaffold.HashContent(core) {
			t.Error("hash not updated")
		}
		if code, out := run(setupOptions{Yes: true}, vexillumHome, home); code != 0 || !strings.Contains(out, "Nothing to change") {
			t.Errorf("rerun: %s", out)
		}
	})
	t.Run("edited is kept, force saves a backup", func(t *testing.T) {
		vexillumHome, home, rules := setup(t)
		writeFileT(t, rules, "# mine\n")
		if code, out := run(setupOptions{Yes: true, Skills: boolp(false)}, vexillumHome, home); code != 0 || readFile(t, rules) != "# mine\n" {
			t.Fatalf("%d %s", code, out)
		}
		if code, out := run(setupOptions{Yes: true, Force: true, Skills: boolp(false)}, vexillumHome, home); code != 0 {
			t.Fatal(out)
		}
		if readFile(t, rules) == "# mine\n" {
			t.Error("--force did not replace the file")
		}
		if readFile(t, filepath.Join(vexillumHome, "backups", "rules-vexillum.md")) != "# mine\n" {
			t.Error("backup missing")
		}
	})
}
