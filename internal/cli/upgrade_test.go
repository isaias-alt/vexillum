package cli

import (
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

// spanishProject lays down an initialized project that has a Spanish
// AGENTS.md, a CLAUDE.md that imports it and a settings.json with only the
// user's own permissions, so upgrade has the block, the skills and the
// sentinel hook to write.
func spanishProject(t *testing.T) (projectDir, vexillumHome string) {
	t.Helper()
	projectDir, vexillumHome = newProject(t)
	copyTree(t, filepath.Join("testdata", "spanish-project"), projectDir)
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
func TestUpgrade_WritesTheSpanishBlock(t *testing.T) {
	projectDir, home := spanishProject(t)
	userAgents := readFile(t, filepath.Join(projectDir, "AGENTS.md"))
	claudeBefore := readFile(t, filepath.Join(projectDir, "CLAUDE.md"))

	r := doUpgrade(projectDir, home, setupOptions{Yes: true}, "", false)
	if r.code != 0 {
		t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
	}

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
	if settings["permissions"] == nil || countStopHookCommand(settings, sentinelHookCommand) != 1 || countStopHookCommand(settings, "") != 1 {
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
	if cfg.Skills["vexillum"] == "" {
		t.Errorf("config after upgrade = %+v", cfg)
	}

	// Running it again is a no-op.
	before, _ := snapshotTree(t, projectDir)
	again := doUpgrade(projectDir, home, setupOptions{Yes: true}, "", false)
	after, _ := snapshotTree(t, projectDir)
	if again.code != 0 || !strings.Contains(again.out, "Nothing to change") || before != after {
		t.Errorf("second upgrade: exit %d, %s", again.code, again.out)
	}
}

// The notice lists what will change, and without --yes and a terminal
// nothing happens.
func TestUpgrade_NoticeAndNonTTY(t *testing.T) {
	projectDir, home := spanishProject(t)
	before, _ := snapshotTree(t, projectDir)
	r := doUpgrade(projectDir, home, setupOptions{}, "", false)
	if r.code == 0 || !strings.Contains(r.out, "add the vexillum block") || !strings.Contains(r.errOut, "--yes") {
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

func slotInAgents(t *testing.T, projectDir string) slot.Inspection {
	t.Helper()
	return slot.Inspect(readFile(t, filepath.Join(projectDir, "AGENTS.md")), "")
}

// initedProject returns a project that init --yes set up, with the block in
// AGENTS.md (the project starts with one).
func initedProject(t *testing.T, opts setupOptions) (projectDir, home string) {
	t.Helper()
	projectDir, home = newProject(t)
	writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), englishAgents)
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
		if b := readFile(t, filepath.Join(projectDir, filepath.FromSlash(install.SlotFileBackupRelPath(install.AgentsFile)))); b != malformed {
			t.Errorf("backup = %q", b)
		}
	})
}

// A project whose block lives in CLAUDE.md (no AGENTS.md): upgrade keeps that
// block fresh there, never creates an AGENTS.md and never adds an import.
func TestUpgrade_ClaudeMDSlot(t *testing.T) {
	core, _ := install.SlotTemplate(slot.LangEN)
	stale, _ := slot.Upsert(englishAgents, "## Vexillum commander\n\nOld text.\n", false)
	current, _ := slot.Upsert(englishAgents, core, false)
	edited := strings.Replace(current, "## Vexillum commander", "## My commander", 1)
	malformed := englishAgents + "\n<!-- BEGIN VEXILLUM v:1 hash:00000000 -->\nhalf a block\n"

	// claudeProject is a project initialized with the CLAUDE.md slot whose
	// CLAUDE.md is then replaced by content.
	claudeProject := func(t *testing.T, content string) (projectDir, home, claude string) {
		t.Helper()
		projectDir, home = newProject(t)
		if r := doInit(projectDir, home, setupOptions{Yes: true, Skills: boolp(false)}, "", false); r.code != 0 {
			t.Fatalf("init: %s%s", r.out, r.errOut)
		}
		notExist(t, filepath.Join(projectDir, "AGENTS.md"))
		claude = filepath.Join(projectDir, "CLAUDE.md")
		writeFileT(t, claude, content)
		return
	}
	upgrade := func(t *testing.T, projectDir, home string, opts setupOptions) result {
		t.Helper()
		opts.Skills = boolp(false)
		r := doUpgrade(projectDir, home, opts, "", false)
		if r.code != 0 {
			t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
		}
		notExist(t, filepath.Join(projectDir, "AGENTS.md"))
		return r
	}

	t.Run("stale is refreshed in CLAUDE.md", func(t *testing.T) {
		projectDir, home, claude := claudeProject(t, stale)
		r := upgrade(t, projectDir, home, setupOptions{Yes: true})
		if !strings.Contains(r.out, "Updated the vexillum block in CLAUDE.md") {
			t.Errorf("output lacks the CLAUDE.md update:\n%s", r.out)
		}
		got := readFile(t, claude)
		if ins := slot.Inspect(got, core); ins.State != slot.StateCurrent {
			t.Errorf("block is %v, want current:\n%s", ins.State, got)
		}
		if !strings.HasPrefix(got, englishAgents) || slot.ImportsAgents(got) {
			t.Errorf("user text changed or an import was added:\n%s", got)
		}
	})
	t.Run("current is untouched", func(t *testing.T) {
		projectDir, home, claude := claudeProject(t, current)
		if r := upgrade(t, projectDir, home, setupOptions{Yes: true}); !strings.Contains(r.out, "Nothing to change") {
			t.Errorf("output:\n%s", r.out)
		}
		if got := readFile(t, claude); got != current {
			t.Errorf("CLAUDE.md changed:\n%s", got)
		}
	})
	t.Run("drifted: diff, no overwrite; --force backs up then replaces", func(t *testing.T) {
		projectDir, home, claude := claudeProject(t, edited)
		if r := upgrade(t, projectDir, home, setupOptions{Yes: true}); !strings.Contains(r.out, "CLAUDE.md: the vexillum block was edited by hand") || !strings.Contains(r.out, "--- your edit") {
			t.Errorf("output:\n%s", r.out)
		}
		if got := readFile(t, claude); got != edited {
			t.Error("an edited block must not be overwritten without --force")
		}
		upgrade(t, projectDir, home, setupOptions{Yes: true, Force: true})
		if ins := slot.Inspect(readFile(t, claude), core); ins.State != slot.StateCurrent {
			t.Errorf("block is %v, want current", ins.State)
		}
		if b := readFile(t, filepath.Join(projectDir, filepath.FromSlash(slot.BackupRelPath))); !strings.Contains(b, "## My commander") {
			t.Errorf("backup = %q", b)
		}
	})
	t.Run("malformed: repair saves the whole CLAUDE.md first", func(t *testing.T) {
		projectDir, home, claude := claudeProject(t, malformed)
		r := upgrade(t, projectDir, home, setupOptions{Yes: true})
		if !strings.Contains(r.out, "CLAUDE.md: the vexillum block is malformed") {
			t.Errorf("output:\n%s", r.out)
		}
		if b := readFile(t, filepath.Join(projectDir, filepath.FromSlash(install.SlotFileBackupRelPath(install.ClaudeFile)))); b != malformed {
			t.Errorf("backup = %q", b)
		}
		if ins := slot.Inspect(readFile(t, claude), core); ins.State != slot.StateCurrent {
			t.Errorf("block is %v, want current", ins.State)
		}
	})
	t.Run("an AGENTS.md that appears later does not take the block", func(t *testing.T) {
		projectDir, home, claude := claudeProject(t, stale)
		writeFileT(t, filepath.Join(projectDir, "AGENTS.md"), englishAgents)
		r := doUpgrade(projectDir, home, setupOptions{Yes: true, Skills: boolp(false)}, "", false)
		if r.code != 0 {
			t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
		}
		if got := readFile(t, filepath.Join(projectDir, "AGENTS.md")); got != englishAgents {
			t.Errorf("AGENTS.md was touched:\n%s", got)
		}
		if ins := slot.Inspect(readFile(t, claude), core); ins.State != slot.StateCurrent {
			t.Errorf("block in CLAUDE.md is %v, want current", ins.State)
		}
	})
	t.Run("a project with no block anywhere is asked for the file too", func(t *testing.T) {
		projectDir, home, claude := claudeProject(t, englishAgents)
		// slot file: no / Continue? / language / CLAUDE.md import
		r := doUpgrade(projectDir, home, setupOptions{Skills: boolp(false)}, "n\ny\ny\ny\n", true)
		if r.code != 0 {
			t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
		}
		if ins := slot.Inspect(readFile(t, filepath.Join(projectDir, "AGENTS.md")), core); ins.State != slot.StateCurrent {
			t.Errorf("AGENTS.md block is %v, want current", ins.State)
		}
		if got := readFile(t, claude); !slot.ImportsAgents(got) {
			t.Errorf("CLAUDE.md does not import AGENTS.md:\n%s", got)
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
	projectDir, home := spanishProject(t)
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
	projectDir, home := spanishProject(t)
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

// countStopHookCommand counts the Stop hook entries whose command is
// command, or every Stop hook entry when command is empty.
func countStopHookCommand(settings map[string]any, command string) int {
	hooks, _ := settings["hooks"].(map[string]any)
	stopGroups, _ := hooks["Stop"].([]any)
	n := 0
	for _, g := range stopGroups {
		group, _ := g.(map[string]any)
		entries, _ := group["hooks"].([]any)
		for _, e := range entries {
			entry, _ := e.(map[string]any)
			if cmd, _ := entry["command"].(string); command == "" || cmd == command {
				n++
			}
		}
	}
	return n
}

// Without consent (no --yes, no terminal) nothing is rewritten.
func TestUpgrade_WithoutConsentLeavesSettingsAlone(t *testing.T) {
	projectDir, home := spanishProject(t)
	before := readFile(t, filepath.Join(projectDir, ".claude", "settings.json"))

	doUpgrade(projectDir, home, setupOptions{}, "", false)

	if after := readFile(t, filepath.Join(projectDir, ".claude", "settings.json")); after != before {
		t.Errorf("settings.json changed without consent:\n%s", after)
	}
}
