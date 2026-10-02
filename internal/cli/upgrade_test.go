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
)

// vx upgrade refuses to run against a project that was never
// initialized, pointing at 'vx init' instead.
func TestUpgrade_RefusesUninitializedProject(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var stdout, stderr bytes.Buffer
	code := runUpgrade(projectDir, vexillumHome, false, &stdout, &stderr)

	if code == 0 {
		t.Fatal("expected non-zero exit code for an uninitialized project")
	}
	if stderr.String() == "" {
		t.Error("expected an error message on stderr")
	}
}

// A project initialized with the current binary and never touched by hand
// is refreshed to the latest template on upgrade (verifies the mechanism
// end to end, even though today's template equals what init just wrote).
func TestUpgrade_RefreshesUntouchedScaffold(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var buf bytes.Buffer
	if code := runInit(projectDir, vexillumHome, &buf, &buf); code != 0 {
		t.Fatalf("init failed: exit %d: %s", code, buf.String())
	}

	var stdout, stderr bytes.Buffer
	code := runUpgrade(projectDir, vexillumHome, false, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}

	rule, err := os.ReadFile(filepath.Join(projectDir, ".claude", "rules", "vexillum.md"))
	if err != nil {
		t.Fatalf("reading .claude/rules/vexillum.md: %v", err)
	}
	if string(rule) != productVexillumRule {
		t.Error("expected .claude/rules/vexillum.md to match the latest template after upgrade")
	}
}

// upgrade never overwrites .claude/rules/vexillum.md that was hand-edited
// after vexillum wrote it - the whole point of the command is to be safe to
// run blindly.
func TestUpgrade_LeavesHandEditedFilesUntouched(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var buf bytes.Buffer
	if code := runInit(projectDir, vexillumHome, &buf, &buf); code != 0 {
		t.Fatalf("init failed: exit %d: %s", code, buf.String())
	}

	rulePath := filepath.Join(projectDir, ".claude", "rules", "vexillum.md")
	customContent := []byte("# My custom commander instructions\n")
	if err := os.WriteFile(rulePath, customContent, 0o644); err != nil {
		t.Fatalf("writing custom rule file: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runUpgrade(projectDir, vexillumHome, false, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}

	got, err := os.ReadFile(rulePath)
	if err != nil {
		t.Fatalf("reading .claude/rules/vexillum.md: %v", err)
	}
	if !bytes.Equal(got, customContent) {
		t.Errorf(".claude/rules/vexillum.md was overwritten: got %q, want %q", got, customContent)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("left untouched")) {
		t.Errorf("expected output to report the rule file as left untouched, got: %s", stdout.String())
	}
}

// A project initialized before hash-tracking existed (no stored hash in
// config.json) whose rule file content no longer matches the latest
// template is ambiguous - it could be stock content from an older vexillum
// version, or a hand edit. upgrade treats that ambiguity conservatively: it
// does not overwrite, and says so.
func TestUpgrade_TreatsUnknownProvenanceAsCustomized(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var buf bytes.Buffer
	if code := runInit(projectDir, vexillumHome, &buf, &buf); code != 0 {
		t.Fatalf("init failed: exit %d: %s", code, buf.String())
	}

	// Simulate a pre-hash-tracking project: strip the stored hash, and
	// make the on-disk content diverge from today's template.
	rulePath := filepath.Join(projectDir, ".claude", "rules", "vexillum.md")
	if err := os.WriteFile(rulePath, []byte("# stale pre-hash content\n"), 0o644); err != nil {
		t.Fatalf("writing stale rule file: %v", err)
	}
	cfgPath := filepath.Join(projectDir, ".vexillum", "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"version":1,"initialized_at":"2026-01-01T00:00:00Z"}`), 0o644); err != nil {
		t.Fatalf("stripping stored hash: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runUpgrade(projectDir, vexillumHome, false, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}

	got, err := os.ReadFile(rulePath)
	if err != nil {
		t.Fatalf("reading .claude/rules/vexillum.md: %v", err)
	}
	if string(got) != "# stale pre-hash content\n" {
		t.Errorf("expected the rule file to be left untouched without a stored hash, got: %s", got)
	}
}

// upgrade recreates a scaffold file that was deleted entirely.
func TestUpgrade_RecreatesMissingFile(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var buf bytes.Buffer
	if code := runInit(projectDir, vexillumHome, &buf, &buf); code != 0 {
		t.Fatalf("init failed: exit %d: %s", code, buf.String())
	}

	rulePath := filepath.Join(projectDir, ".claude", "rules", "vexillum.md")
	if err := os.Remove(rulePath); err != nil {
		t.Fatalf("removing .claude/rules/vexillum.md: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runUpgrade(projectDir, vexillumHome, false, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	mustStat(t, rulePath)
}

// upgrade is safe to run repeatedly: a second run makes no further changes
// once everything is already current.
func TestUpgrade_IdempotentOnSecondRun(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var buf bytes.Buffer
	if code := runInit(projectDir, vexillumHome, &buf, &buf); code != 0 {
		t.Fatalf("init failed: exit %d: %s", code, buf.String())
	}
	if code := runUpgrade(projectDir, vexillumHome, false, &buf, &buf); code != 0 {
		t.Fatalf("first upgrade failed: exit %d: %s", code, buf.String())
	}

	rulePath := filepath.Join(projectDir, ".claude", "rules", "vexillum.md")
	ruleBefore, err := os.ReadFile(rulePath)
	if err != nil {
		t.Fatalf("reading .claude/rules/vexillum.md: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runUpgrade(projectDir, vexillumHome, false, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("already up to date")) {
		t.Errorf("expected output to report the rule file as already up to date, got: %s", stdout.String())
	}

	ruleAfter, err := os.ReadFile(rulePath)
	if err != nil {
		t.Fatalf("reading .claude/rules/vexillum.md after second upgrade: %v", err)
	}
	if !bytes.Equal(ruleBefore, ruleAfter) {
		t.Error(".claude/rules/vexillum.md changed on a second upgrade run with nothing to do")
	}
}

// upgrade adds the sentinel Stop hook to a project initialized before that
// hook existed, same as init's own healing path.
func TestUpgrade_AddsMissingSentinelHook(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var buf bytes.Buffer
	if code := runInit(projectDir, vexillumHome, &buf, &buf); code != 0 {
		t.Fatalf("init failed: exit %d: %s", code, buf.String())
	}

	settingsPath := filepath.Join(projectDir, ".claude", "settings.json")
	if err := os.Remove(settingsPath); err != nil {
		t.Fatalf("removing settings.json to simulate a pre-hook project: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runUpgrade(projectDir, vexillumHome, false, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("reading settings.json: %v", err)
	}
	if !bytes.Contains(data, []byte(sentinelHookCommand)) {
		t.Errorf("expected settings.json to contain the sentinel hook, got: %s", data)
	}
}

// --force overwrites a hand-edited rule file that upgrade would otherwise
// leave untouched - the explicit escape hatch for exactly the case a
// plain upgrade refuses to guess about.
func TestUpgrade_ForceOverwritesHandEditedFile(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var buf bytes.Buffer
	if code := runInit(projectDir, vexillumHome, &buf, &buf); code != 0 {
		t.Fatalf("init failed: exit %d: %s", code, buf.String())
	}

	rulePath := filepath.Join(projectDir, ".claude", "rules", "vexillum.md")
	if err := os.WriteFile(rulePath, []byte("# My custom commander instructions\n"), 0o644); err != nil {
		t.Fatalf("writing custom rule file: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runUpgrade(projectDir, vexillumHome, true, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}

	got, err := os.ReadFile(rulePath)
	if err != nil {
		t.Fatalf("reading .claude/rules/vexillum.md: %v", err)
	}
	if string(got) != productVexillumRule {
		t.Error("expected --force to overwrite the rule file with the latest template")
	}
	if !bytes.Contains(stdout.Bytes(), []byte("discarded")) {
		t.Errorf("expected output to warn that local changes were discarded, got: %s", stdout.String())
	}
}

// --force also covers a project from before hash tracking existed - the
// exact case a plain upgrade can't otherwise tell apart from a real
// hand edit.
func TestUpgrade_ForceCoversUnknownProvenance(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var buf bytes.Buffer
	if code := runInit(projectDir, vexillumHome, &buf, &buf); code != 0 {
		t.Fatalf("init failed: exit %d: %s", code, buf.String())
	}

	rulePath := filepath.Join(projectDir, ".claude", "rules", "vexillum.md")
	if err := os.WriteFile(rulePath, []byte("# stale pre-hash content\n"), 0o644); err != nil {
		t.Fatalf("writing stale rule file: %v", err)
	}
	cfgPath := filepath.Join(projectDir, ".vexillum", "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"version":1,"initialized_at":"2026-01-01T00:00:00Z"}`), 0o644); err != nil {
		t.Fatalf("stripping stored hash: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runUpgrade(projectDir, vexillumHome, true, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}

	got, err := os.ReadFile(rulePath)
	if err != nil {
		t.Fatalf("reading .claude/rules/vexillum.md: %v", err)
	}
	if string(got) != productVexillumRule {
		t.Error("expected --force to overwrite the rule file even without a stored hash")
	}
}

// vx upgrade refuses to run from inside a vexillum-managed camp,
// same guard as init - see TestRefuseInsideVexillumHome.
func TestUpgrade_RefusesInsideVexillumHome(t *testing.T) {
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")
	campPath := filepath.Join(vexillumHome, "myproject-abc12345", "1", "myproject")
	if err := os.MkdirAll(campPath, 0o755); err != nil {
		t.Fatalf("mkdir camp path: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runUpgrade(campPath, vexillumHome, false, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected non-zero exit code when run from inside a camp")
	}
	if stderr.String() == "" {
		t.Error("expected an error message on stderr")
	}
}

// vx upgrade --global refuses to run before 'vx init --global'
// has ever been run - same posture as the local upgrade refusing an
// uninitialized project.
func TestUpgradeGlobal_RefusesUninitialized(t *testing.T) {
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")
	home := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := runUpgradeGlobal(vexillumHome, home, false, &stdout, &stderr)

	if code == 0 {
		t.Fatal("expected non-zero exit code before 'vx init --global' ran")
	}
	if stderr.String() == "" {
		t.Error("expected an error message on stderr")
	}
}

// A machine initialized globally and never touched by hand is refreshed to
// the latest template on upgrade --global.
func TestUpgradeGlobal_RefreshesUntouchedScaffold(t *testing.T) {
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")
	home := t.TempDir()

	var buf bytes.Buffer
	if code := runInitGlobal(vexillumHome, home, &buf, &buf); code != 0 {
		t.Fatalf("global init failed: exit %d: %s", code, buf.String())
	}

	var stdout, stderr bytes.Buffer
	code := runUpgradeGlobal(vexillumHome, home, false, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}

	rule, err := os.ReadFile(filepath.Join(home, ".claude", "rules", "vexillum.md"))
	if err != nil {
		t.Fatalf("reading ~/.claude/rules/vexillum.md: %v", err)
	}
	if string(rule) != productVexillumRule {
		t.Error("expected the global rule file to match the latest template after upgrade")
	}
}

// upgrade --global never overwrites a hand-edited global rule file, same
// guarantee as the local scaffold.
func TestUpgradeGlobal_LeavesHandEditedFileUntouched(t *testing.T) {
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")
	home := t.TempDir()

	var buf bytes.Buffer
	if code := runInitGlobal(vexillumHome, home, &buf, &buf); code != 0 {
		t.Fatalf("global init failed: exit %d: %s", code, buf.String())
	}

	rulePath := filepath.Join(home, ".claude", "rules", "vexillum.md")
	customContent := []byte("# My custom global commander instructions\n")
	if err := os.WriteFile(rulePath, customContent, 0o644); err != nil {
		t.Fatalf("writing custom global rule file: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runUpgradeGlobal(vexillumHome, home, false, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}

	got, err := os.ReadFile(rulePath)
	if err != nil {
		t.Fatalf("reading global rule file: %v", err)
	}
	if !bytes.Equal(got, customContent) {
		t.Errorf("global rule file was overwritten: got %q, want %q", got, customContent)
	}
}

// --force overwrites a hand-edited global rule file, same escape hatch as
// the local scaffold.
func TestUpgradeGlobal_ForceOverwritesHandEditedFile(t *testing.T) {
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")
	home := t.TempDir()

	var buf bytes.Buffer
	if code := runInitGlobal(vexillumHome, home, &buf, &buf); code != 0 {
		t.Fatalf("global init failed: exit %d: %s", code, buf.String())
	}

	rulePath := filepath.Join(home, ".claude", "rules", "vexillum.md")
	if err := os.WriteFile(rulePath, []byte("# My custom global commander instructions\n"), 0o644); err != nil {
		t.Fatalf("writing custom global rule file: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runUpgradeGlobal(vexillumHome, home, true, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}

	got, err := os.ReadFile(rulePath)
	if err != nil {
		t.Fatalf("reading global rule file: %v", err)
	}
	if string(got) != productVexillumRule {
		t.Error("expected --force to overwrite the global rule file with the latest template")
	}
}

// A project initialized before the command was renamed has the old
// template (full of `vexillum <cmd>` examples) and a Stop hook that runs
// the old executable name. upgrade must migrate both: refresh the rules
// (its hash is the stored one, so it is safe to overwrite) and rewrite the
// hook to the current command - whichever legacy spelling it carries.
func TestUpgrade_MigratesProjectInitializedBeforeTheRename(t *testing.T) {
	oldRules, err := os.ReadFile(filepath.Join("testdata", "commander-rules-pre-vx.md"))
	if err != nil {
		t.Fatalf("reading the pre-rename template fixture: %v", err)
	}
	if !strings.Contains(string(oldRules), "`vexillum dispatch`") {
		t.Fatal("fixture should be the template from before the rename")
	}
	sum := sha256.Sum256(oldRules)

	for _, legacyHook := range []string{"vexillum sentinel await", "vexillum sentinel drain"} {
		t.Run(legacyHook, func(t *testing.T) {
			projectDir := t.TempDir()
			initGitRepo(t, projectDir)
			vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

			// Lay down the project exactly as the old binary left it.
			write := func(rel, content string) {
				t.Helper()
				path := filepath.Join(projectDir, rel)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cfg, _ := json.Marshal(map[string]any{
				"version":            1,
				"initialized_at":     "2026-01-01T00:00:00Z",
				"vexillum_rule_hash": hex.EncodeToString(sum[:]),
			})
			write(".vexillum/config.json", string(cfg))
			write(".claude/rules/vexillum.md", string(oldRules))
			write(".claude/settings.json", `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"`+legacyHook+`","asyncRewake":true,"timeout":3600}]}]}}`)

			var stdout, stderr bytes.Buffer
			if code := runUpgrade(projectDir, vexillumHome, false, &stdout, &stderr); code != 0 {
				t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
			}

			rules, err := os.ReadFile(filepath.Join(projectDir, ".claude", "rules", "vexillum.md"))
			if err != nil {
				t.Fatal(err)
			}
			if string(rules) != productVexillumRule {
				t.Error("expected the rules to be refreshed to the current template")
			}
			if strings.Contains(string(rules), "`vexillum dispatch`") || !strings.Contains(string(rules), "`vx dispatch`") {
				t.Error("expected the refreshed rules to use the vx command")
			}

			settings, err := os.ReadFile(filepath.Join(projectDir, ".claude", "settings.json"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(settings), "vexillum sentinel") {
				t.Errorf("expected the legacy hook command to be gone, got: %s", settings)
			}
			if strings.Count(string(settings), sentinelHookCommand) != 1 {
				t.Errorf("expected exactly one %q hook, got: %s", sentinelHookCommand, settings)
			}

			// The stored hash follows the new template, so the next upgrade
			// is a no-op rather than treating the file as customized.
			newCfg, err := os.ReadFile(filepath.Join(projectDir, ".vexillum", "config.json"))
			if err != nil {
				t.Fatal(err)
			}
			newSum := sha256.Sum256([]byte(productVexillumRule))
			if !strings.Contains(string(newCfg), hex.EncodeToString(newSum[:])) {
				t.Errorf("expected config.json to record the new template hash, got: %s", newCfg)
			}
		})
	}
}
