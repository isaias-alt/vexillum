package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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

// L1-01: init in a clean project creates ~/.vexillum/, the local scaffold
// and AGENTS.md, and reports what it did, exit 0.
func TestInit_CleanProject(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var stdout, stderr bytes.Buffer
	code := runInit(projectDir, vexillumHome, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	mustStat(t, vexillumHome)
	mustStat(t, filepath.Join(projectDir, ".vexillum", "config.json"))
	mustStat(t, filepath.Join(projectDir, "AGENTS.md"))
	mustStat(t, filepath.Join(projectDir, "CLAUDE.md"))

	claudeMD, err := os.ReadFile(filepath.Join(projectDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("reading CLAUDE.md: %v", err)
	}
	if string(claudeMD) != "@AGENTS.md\n" {
		t.Errorf("expected CLAUDE.md to import AGENTS.md, got %q", claudeMD)
	}

	if out := stdout.String(); out == "" {
		t.Error("expected confirmation output, got none")
	}
}

// Claude Code auto-loads CLAUDE.md, not a bare AGENTS.md, so a project
// initialized before this shim existed never had its AGENTS.md actually
// read. Re-running init on such a project heals the gap: it creates the
// missing CLAUDE.md without touching AGENTS.md or config.json.
func TestInit_HealsMissingClaudeMD(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var buf bytes.Buffer
	if code := runInit(projectDir, vexillumHome, &buf, &buf); code != 0 {
		t.Fatalf("first init failed: exit %d: %s", code, buf.String())
	}

	claudePath := filepath.Join(projectDir, "CLAUDE.md")
	if err := os.Remove(claudePath); err != nil {
		t.Fatalf("removing CLAUDE.md to simulate a pre-fix project: %v", err)
	}
	agentsBefore, err := os.ReadFile(filepath.Join(projectDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("reading AGENTS.md: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runInit(projectDir, vexillumHome, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}

	mustStat(t, claudePath)
	agentsAfter, err := os.ReadFile(filepath.Join(projectDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("reading AGENTS.md after healing: %v", err)
	}
	if !bytes.Equal(agentsBefore, agentsAfter) {
		t.Error("AGENTS.md changed while only CLAUDE.md should have been healed")
	}
}

// L1-02: running init a second time does not duplicate or corrupt anything,
// informs the user it was already initialized, exit 0.
func TestInit_Idempotent(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var buf bytes.Buffer
	if code := runInit(projectDir, vexillumHome, &buf, &buf); code != 0 {
		t.Fatalf("first init failed: exit %d: %s", code, buf.String())
	}

	agentsBefore, err := os.ReadFile(filepath.Join(projectDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("reading AGENTS.md: %v", err)
	}
	configBefore, err := os.ReadFile(filepath.Join(projectDir, ".vexillum", "config.json"))
	if err != nil {
		t.Fatalf("reading config.json: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runInit(projectDir, vexillumHome, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0 on second run, got %d (stderr: %s)", code, stderr.String())
	}
	if stdout.String() == "" {
		t.Error("expected output informing the project was already initialized")
	}

	agentsAfter, err := os.ReadFile(filepath.Join(projectDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("reading AGENTS.md after second run: %v", err)
	}
	if !bytes.Equal(agentsBefore, agentsAfter) {
		t.Error("AGENTS.md changed on second init run")
	}

	configAfter, err := os.ReadFile(filepath.Join(projectDir, ".vexillum", "config.json"))
	if err != nil {
		t.Fatalf("reading config.json after second run: %v", err)
	}
	if !bytes.Equal(configBefore, configAfter) {
		t.Error("config.json changed on second init run")
	}
}

// L1-03: hand-edited AGENTS.md is never silently overwritten by a later
// init run.
func TestInit_DoesNotOverwriteEditedAgentsMD(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var buf bytes.Buffer
	if code := runInit(projectDir, vexillumHome, &buf, &buf); code != 0 {
		t.Fatalf("first init failed: exit %d: %s", code, buf.String())
	}

	agentsPath := filepath.Join(projectDir, "AGENTS.md")
	customContent := []byte("# My custom commander instructions\n")
	if err := os.WriteFile(agentsPath, customContent, 0o644); err != nil {
		t.Fatalf("writing custom AGENTS.md: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runInit(projectDir, vexillumHome, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}

	got, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("reading AGENTS.md: %v", err)
	}
	if !bytes.Equal(got, customContent) {
		t.Errorf("AGENTS.md was overwritten: got %q, want %q", got, customContent)
	}
}

// L1-04: if ~/.vexillum/ is missing but the local scaffold is present,
// init recreates ~/.vexillum/ without touching the local scaffold.
func TestInit_RecreatesMissingVexillumHome(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var buf bytes.Buffer
	if code := runInit(projectDir, vexillumHome, &buf, &buf); code != 0 {
		t.Fatalf("first init failed: exit %d: %s", code, buf.String())
	}

	configBefore, err := os.ReadFile(filepath.Join(projectDir, ".vexillum", "config.json"))
	if err != nil {
		t.Fatalf("reading config.json: %v", err)
	}

	if err := os.RemoveAll(vexillumHome); err != nil {
		t.Fatalf("removing vexillum home: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runInit(projectDir, vexillumHome, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	mustStat(t, vexillumHome)

	configAfter, err := os.ReadFile(filepath.Join(projectDir, ".vexillum", "config.json"))
	if err != nil {
		t.Fatalf("reading config.json after recreation: %v", err)
	}
	if !bytes.Equal(configBefore, configAfter) {
		t.Error("local config.json changed when ~/.vexillum/ was recreated")
	}
}

// L1-05: init outside a git repo fails with a clear message and a non-zero
// exit code, leaving no state behind.
func TestInit_OutsideGitRepo(t *testing.T) {
	projectDir := t.TempDir()
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var stdout, stderr bytes.Buffer
	code := runInit(projectDir, vexillumHome, &stdout, &stderr)

	if code == 0 {
		t.Fatal("expected non-zero exit code outside a git repository")
	}
	if stderr.String() == "" {
		t.Error("expected an error message on stderr")
	}
	if _, err := os.Stat(vexillumHome); !os.IsNotExist(err) {
		t.Error("expected ~/.vexillum/ not to be created outside a git repository")
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".vexillum")); !os.IsNotExist(err) {
		t.Error("expected no local scaffold outside a git repository")
	}
}

// L1-06: if ~/.vexillum/ cannot be created due to permissions, init fails
// with a clear message naming the problem and the path, non-zero exit,
// no partial state.
func TestInit_VexillumHomeNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, permission checks don't apply")
	}

	projectDir := t.TempDir()
	initGitRepo(t, projectDir)

	readOnlyParent := t.TempDir()
	if err := os.Chmod(readOnlyParent, 0o500); err != nil {
		t.Fatalf("chmod read-only parent: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnlyParent, 0o700) })

	vexillumHome := filepath.Join(readOnlyParent, ".vexillum")

	var stdout, stderr bytes.Buffer
	code := runInit(projectDir, vexillumHome, &stdout, &stderr)

	if code == 0 {
		t.Fatal("expected non-zero exit code when ~/.vexillum/ can't be created")
	}
	errMsg := stderr.String()
	if errMsg == "" {
		t.Error("expected an error message on stderr")
	}
	if !bytes.Contains([]byte(errMsg), []byte(vexillumHome)) {
		t.Errorf("expected error message to name the path %s, got: %s", vexillumHome, errMsg)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".vexillum")); !os.IsNotExist(err) {
		t.Error("expected no local scaffold left behind when ~/.vexillum/ creation fails")
	}
}

// vexillum init adds the sentinel Stop hook to a fresh .claude/settings.json.
func TestInit_AddsSentinelStopHook(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var stdout, stderr bytes.Buffer
	if code := runInit(projectDir, vexillumHome, &stdout, &stderr); code != 0 {
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
	if !bytes.Contains(data, []byte(sentinelHookCommand)) {
		t.Errorf("expected settings.json to contain %q, got: %s", sentinelHookCommand, data)
	}
}

// The hook init writes has the async fields set - verified live that a
// real asyncRewake Stop hook wakes an idle Claude Code session with no
// new user prompt, which a synchronous-only hook cannot do.
func TestInit_SentinelStopHookIsAsync(t *testing.T) {
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")

	var stdout, stderr bytes.Buffer
	if code := runInit(projectDir, vexillumHome, &stdout, &stderr); code != 0 {
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
	if !bytes.Contains(data, []byte(sentinelHookCommand)) {
		t.Errorf("expected the sentinel Stop hook to be present, got: %s", data)
	}

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

// vexillum init refuses to run from inside a vexillum-managed camp - see
// TestRefuseInsideVexillumHome for the underlying bug this guards
// against.
func TestInit_RefusesInsideVexillumHome(t *testing.T) {
	vexillumHome := filepath.Join(t.TempDir(), ".vexillum")
	campPath := filepath.Join(vexillumHome, "myproject-abc12345", "1", "myproject")
	if err := os.MkdirAll(campPath, 0o755); err != nil {
		t.Fatalf("mkdir camp path: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runInit(campPath, vexillumHome, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected non-zero exit code when run from inside a camp")
	}
	if stderr.String() == "" {
		t.Error("expected an error message on stderr")
	}
	if _, err := os.Stat(filepath.Join(campPath, ".vexillum")); !os.IsNotExist(err) {
		t.Error("expected no scaffold to be written inside the camp")
	}
}
