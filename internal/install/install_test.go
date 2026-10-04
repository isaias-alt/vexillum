package install

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/isaias-alt/vexillum/internal/slot"
	"github.com/isaias-alt/vexillum/skills"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The display order names exactly the embedded skills, so a new skill cannot
// be forgotten by the prompts.
func TestSkillNamesCoverEmbeddedSkills(t *testing.T) {
	got := append([]string(nil), displayOrder...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, skills.Names()) {
		t.Errorf("displayOrder %v does not match embedded skills %v", displayOrder, skills.Names())
	}
	if !reflect.DeepEqual(SkillNames(), displayOrder) {
		t.Errorf("SkillNames() = %v", SkillNames())
	}
}

func TestSkillList(t *testing.T) {
	for in, want := range map[string]string{"": "", "a": "a", "a,b": "a and b", "a,b,c": "a, b and c"} {
		var names []string
		for _, r := range in {
			if r != ',' {
				names = append(names, string(r))
			}
		}
		if got := SkillList(names); got != want {
			t.Errorf("SkillList(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInspectInstallSkill(t *testing.T) {
	dir := t.TempDir()
	st, err := InspectSkill(dir, "forum", "")
	if err != nil || st.State != SkillMissing {
		t.Fatalf("missing: %+v %v", st, err)
	}
	if err := InstallSkill(dir, "forum"); err != nil {
		t.Fatal(err)
	}
	if st, _ = InspectSkill(dir, "forum", ""); st.State != SkillCurrent {
		t.Fatalf("after install: %v", st.State)
	}
	// Junk the user's OS adds does not make it edited.
	write(t, filepath.Join(dir, "forum", ".DS_Store"), "x")
	if st, _ = InspectSkill(dir, "forum", ""); st.State != SkillCurrent {
		t.Errorf(".DS_Store counted: %v", st.State)
	}

	// An older version: untouched since install -> stale; unknown -> edited.
	write(t, filepath.Join(dir, "forum", "SKILL.md"), "old")
	write(t, filepath.Join(dir, "forum", "extra", "old.md"), "gone")
	st, _ = InspectSkill(dir, "forum", "")
	if st.State != SkillEdited {
		t.Errorf("no recorded hash: %v, want edited", st.State)
	}
	if st2, _ := InspectSkill(dir, "forum", st.Hash); st2.State != SkillStale {
		t.Errorf("matching recorded hash: %v, want stale", st2.State)
	}
	if st2, _ := InspectSkill(dir, "forum", "other"); st2.State != SkillEdited {
		t.Errorf("other recorded hash: %v, want edited", st2.State)
	}

	// Reinstall prunes the leftovers and the empty directory.
	if err := InstallSkill(dir, "forum"); err != nil {
		t.Fatal(err)
	}
	if st, _ = InspectSkill(dir, "forum", ""); st.State != SkillCurrent {
		t.Errorf("after reinstall: %v", st.State)
	}
	if _, err := os.Stat(filepath.Join(dir, "forum", "extra")); !os.IsNotExist(err) {
		t.Error("empty leftover directory not removed")
	}
	if _, err := InspectSkill(dir, "nope", ""); err != nil {
		t.Errorf("an unknown skill name is just missing on disk: %v", err)
	}
	if err := InstallSkill(dir, "nope"); err == nil {
		t.Error("installing an unknown skill must fail")
	}
}

func TestSymlinkedSkill(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(t.TempDir(), "src")
	files, _ := skills.Files("muster")
	for _, f := range files {
		write(t, filepath.Join(src, f.Path), string(f.Content))
	}
	if err := os.Symlink(src, filepath.Join(dir, "muster")); err != nil {
		t.Fatal(err)
	}
	st, err := InspectSkill(dir, "muster", "")
	if err != nil || !st.Linked || st.State != SkillCurrent {
		t.Fatalf("%+v %v", st, err)
	}
	if err := InstallSkill(dir, "muster"); err == nil {
		t.Error("must refuse to write through a symlink")
	}
}

func TestBackupSkill(t *testing.T) {
	dir, backups := t.TempDir(), t.TempDir()
	write(t, filepath.Join(dir, "forum", "SKILL.md"), "mine")
	write(t, filepath.Join(dir, "forum", "a", "b.md"), "deep")
	dest, err := BackupSkill(dir, "forum", backups)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "a", "b.md")); string(got) != "deep" {
		t.Errorf("backup = %q", got)
	}
}

func TestResolveLang(t *testing.T) {
	es := "Esta es la guia para los agentes que trabajan en este repositorio y que debe seguir cuando se hace un cambio en el codigo.\n"
	core, _ := SlotTemplate(slot.LangES)
	withBlock, _ := slot.Upsert("# Project guide\n\nThis is the guide for the agents that work in this repository and that they should follow when they make a change.\n", core, false)

	tests := []struct {
		name    string
		content string
		flag    slot.Lang
		want    slot.Lang
		src     LangSource
	}{
		{"flag wins", es, slot.LangEN, slot.LangEN, LangFlag},
		{"detected", es, "", slot.LangES, LangDetected},
		{"empty defaults to en", "", "", slot.LangEN, LangDefault},
		{"block language beats the surrounding text", withBlock, "", slot.LangES, LangBlock},
	}
	for _, tc := range tests {
		got, src := ResolveLang(tc.content, tc.flag)
		if got != tc.want || src != tc.src {
			t.Errorf("%s: got (%s, %v), want (%s, %v)", tc.name, got, src, tc.want, tc.src)
		}
	}
}

func TestReadSlotFile(t *testing.T) {
	dir := t.TempDir()
	f, err := ReadSlotFile(dir, AgentsFile)
	if err != nil || f.Exists || f.Content != "" || f.Name != AgentsFile {
		t.Fatalf("%+v %v", f, err)
	}
	write(t, filepath.Join(dir, "AGENTS.md"), "x")
	if f, _ = ReadSlotFile(dir, AgentsFile); !f.Exists || f.Content != "x" {
		t.Errorf("%+v", f)
	}
}

func TestLocateSlotFile(t *testing.T) {
	core, _ := SlotTemplate(slot.LangEN)
	block, _ := slot.Upsert("# Mine\n", core, false)

	tests := []struct {
		name       string
		agents     string // "" means the file does not exist
		claude     string
		wantFile   string
		wantChoose bool
	}{
		{"nothing at all: CLAUDE.md is proposed", "", "", ClaudeFile, true},
		{"only a CLAUDE.md without a block: still proposed", "", "# Mine\n", ClaudeFile, true},
		{"AGENTS.md without a block", "# Mine\n", "", AgentsFile, false},
		{"AGENTS.md without a block, CLAUDE.md imports it", "# Mine\n", "@AGENTS.md\n", AgentsFile, false},
		{"block in AGENTS.md", block, "@AGENTS.md\n", AgentsFile, false},
		{"block in CLAUDE.md, no AGENTS.md", "", block, ClaudeFile, false},
		{"block in CLAUDE.md, AGENTS.md without one", "# Mine\n", block, ClaudeFile, false},
		{"blocks in both: AGENTS.md wins", block, block, AgentsFile, false},
	}
	for _, tc := range tests {
		dir := t.TempDir()
		if tc.agents != "" {
			write(t, filepath.Join(dir, "AGENTS.md"), tc.agents)
		}
		if tc.claude != "" {
			write(t, filepath.Join(dir, "CLAUDE.md"), tc.claude)
		}
		f, choose, err := LocateSlotFile(dir)
		if err != nil || f.Name != tc.wantFile || choose != tc.wantChoose {
			t.Errorf("%s: got (%s, choose=%v, %v), want (%s, choose=%v)", tc.name, f.Name, choose, err, tc.wantFile, tc.wantChoose)
		}
	}
}

func TestSaveSlotFileBackup(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{AgentsFile, ClaudeFile} {
		p, err := SaveSlotFileBackup(dir, name, "was "+name)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(dir, ".vexillum", name+".backup"); p != want {
			t.Errorf("path = %s, want %s", p, want)
		}
		if got, _ := os.ReadFile(p); string(got) != "was "+name {
			t.Errorf("backup = %q", got)
		}
	}
}
