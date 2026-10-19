package piwake_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/piwake"
	"github.com/isaias-alt/vexillum/internal/scaffold"
)

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The extension carries the very script of the Claude Code Stop hook, so both
// find vx the same way, and no placeholder is left in what vx writes.
func TestSourceCarriesTheHookScript(t *testing.T) {
	src := string(piwake.Source())
	if strings.Contains(src, "__VX_AWAIT_SCRIPT__") {
		t.Fatal("the placeholder was not filled in")
	}
	m := regexp.MustCompile(`(?m)^const AWAIT_SCRIPT = (".*");$`).FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("no AWAIT_SCRIPT line in:\n%s", src)
	}
	var got string
	if err := json.Unmarshal([]byte(m[1]), &got); err != nil {
		t.Fatalf("the script literal is not a JSON string: %v", err)
	}
	if got != scaffold.SentinelHookScript {
		t.Errorf("script = %q, want %q", got, scaffold.SentinelHookScript)
	}
	if !strings.Contains(scaffold.SentinelHookCommand, got) {
		t.Error("the hook command no longer contains the script")
	}
	sum := sha256.Sum256(piwake.Source())
	if piwake.Hash() != hex.EncodeToString(sum[:]) {
		t.Error("Hash is not the hex sha256 of Source")
	}
}

func TestInspectAndInstall(t *testing.T) {
	dir := t.TempDir()
	if st, err := piwake.Inspect(dir, ""); err != nil || st != piwake.Missing {
		t.Fatalf("empty project: %v, %v", st, err)
	}

	hash, err := piwake.Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hash != piwake.Hash() {
		t.Errorf("Install returned %s, want %s", hash, piwake.Hash())
	}
	info, err := os.Stat(piwake.Path(dir))
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("installed file: %v, %v", info, err)
	}
	if st, _ := piwake.Inspect(dir, ""); st != piwake.Current {
		t.Errorf("after install: %v", st)
	}

	// An older copy that vx wrote (its hash is recorded) is stale; one nobody
	// recorded, or that differs from the record, is the user's.
	write(t, piwake.Path(dir), "// an older vexillum copy\n")
	older := "ff"
	if st, _ := piwake.Inspect(dir, older); st != piwake.Edited {
		t.Errorf("record that does not match: %v", st)
	}
	if st, _ := piwake.Inspect(dir, ""); st != piwake.Edited {
		t.Errorf("no record: %v", st)
	}
	recorded := hashOfFile(t, piwake.Path(dir))
	if st, _ := piwake.Inspect(dir, recorded); st != piwake.Stale {
		t.Errorf("untouched since vx wrote it: %v", st)
	}

	// Backup keeps the edited copy before an overwrite.
	configDir := filepath.Join(dir, ".vexillum")
	backup, err := piwake.Backup(dir, configDir)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, backup) != "// an older vexillum copy\n" {
		t.Error("the backup is not the copy that was there")
	}
	if _, err := piwake.Install(dir); err != nil {
		t.Fatal(err)
	}
	if st, _ := piwake.Inspect(dir, ""); st != piwake.Current {
		t.Errorf("after refresh: %v", st)
	}
}

// hashOfFile is the hash vx records for a file: the hex sha256 of its content.
func hashOfFile(t *testing.T, path string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(read(t, path)))
	return hex.EncodeToString(sum[:])
}

func TestInstallRefusesASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "mine.ts")
	write(t, target, "// mine\n")
	if err := os.MkdirAll(filepath.Dir(piwake.Path(dir)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, piwake.Path(dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := piwake.Install(dir); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("err = %v, want a symlink refusal", err)
	}
	if read(t, target) != "// mine\n" {
		t.Error("the link's target was written through")
	}
}

func TestEnsureSettings(t *testing.T) {
	ref := `"` + piwake.SkillsRef + `"`

	t.Run("creates the file", func(t *testing.T) {
		dir := t.TempDir()
		if need, err := piwake.SettingsNeeded(dir); err != nil || !need {
			t.Fatalf("SettingsNeeded = %v, %v", need, err)
		}
		if ok, err := piwake.SkillsReferenced(dir); err != nil || ok {
			t.Fatalf("SkillsReferenced = %v, %v", ok, err)
		}
		if added, err := piwake.EnsureSettings(dir); err != nil || !added {
			t.Fatalf("EnsureSettings = %v, %v", added, err)
		}
		var got map[string][]string
		if err := json.Unmarshal([]byte(read(t, piwake.SettingsPath(dir))), &got); err != nil {
			t.Fatal(err)
		}
		if len(got["skills"]) != 1 || got["skills"][0] != piwake.SkillsRef {
			t.Errorf("skills = %v", got["skills"])
		}
		if ok, _ := piwake.SkillsReferenced(dir); !ok {
			t.Error("not referenced after writing")
		}
		before := read(t, piwake.SettingsPath(dir))
		if added, err := piwake.EnsureSettings(dir); err != nil || added {
			t.Errorf("second EnsureSettings = %v, %v, want a no-op", added, err)
		}
		if read(t, piwake.SettingsPath(dir)) != before {
			t.Error("a no-op rewrote the file")
		}
	})

	t.Run("keeps everything else and other skills", func(t *testing.T) {
		dir := t.TempDir()
		write(t, piwake.SettingsPath(dir), `{"theme":"dark","skills":["../my-skills"],"note":"a & b <c>"}`)
		if added, err := piwake.EnsureSettings(dir); err != nil || !added {
			t.Fatalf("EnsureSettings = %v, %v", added, err)
		}
		got := read(t, piwake.SettingsPath(dir))
		for _, want := range []string{`"theme": "dark"`, `"../my-skills"`, ref, `"a & b <c>"`} {
			if !strings.Contains(got, want) {
				t.Errorf("missing %s in:\n%s", want, got)
			}
		}
	})

	t.Run("already listed, with or without a slash", func(t *testing.T) {
		for _, spelling := range []string{piwake.SkillsRef, piwake.SkillsRef + "/"} {
			dir := t.TempDir()
			content := `{"skills":["` + spelling + `"]}`
			write(t, piwake.SettingsPath(dir), content)
			if need, err := piwake.SettingsNeeded(dir); err != nil || need {
				t.Errorf("%s: SettingsNeeded = %v, %v", spelling, need, err)
			}
			if read(t, piwake.SettingsPath(dir)) != content {
				t.Errorf("%s: the file was touched", spelling)
			}
		}
	})

	t.Run("a file it cannot make sense of is left alone", func(t *testing.T) {
		for name, content := range map[string]string{
			"invalid json":    `{"skills": [`,
			"not an object":   `["a"]`,
			"skills not list": `{"skills": "../.claude/skills"}`,
		} {
			dir := t.TempDir()
			write(t, piwake.SettingsPath(dir), content)
			if _, err := piwake.EnsureSettings(dir); err == nil {
				t.Errorf("%s: no error", name)
			}
			if read(t, piwake.SettingsPath(dir)) != content {
				t.Errorf("%s: the file was rewritten", name)
			}
		}
	})
}
