package scaffold

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readSettings(t *testing.T, projectDir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(projectDir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("reading settings.json: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parsing settings.json: %v", err)
	}
	return settings
}

func TestEnsureSentinelHook_CreatesFresh(t *testing.T) {
	dir := t.TempDir()

	added, err := EnsureSentinelHook(dir)
	if err != nil {
		t.Fatalf("EnsureSentinelHook: %v", err)
	}
	if !added {
		t.Error("expected added=true when settings.json didn't exist yet")
	}

	settings := readSettings(t, dir)
	hooks := settings["hooks"].(map[string]any)
	stop := hooks["Stop"].([]any)
	if len(stop) != 1 {
		t.Fatalf("expected exactly one Stop hook group, got %d", len(stop))
	}
}

func TestEnsureSentinelHook_IdempotentOnSecondCall(t *testing.T) {
	dir := t.TempDir()

	if _, err := EnsureSentinelHook(dir); err != nil {
		t.Fatalf("EnsureSentinelHook: %v", err)
	}
	added, err := EnsureSentinelHook(dir)
	if err != nil {
		t.Fatalf("EnsureSentinelHook (second call): %v", err)
	}
	if added {
		t.Error("expected added=false once the hook is already present")
	}
}

func TestEnsureSentinelHook_UpgradesLegacyInPlace(t *testing.T) {
	dir := t.TempDir()
	settingsDir := filepath.Join(dir, ".claude")
	os.MkdirAll(settingsDir, 0o755)
	legacy := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"vexillum sentinel drain"}]}]}}`
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(legacy), 0o644); err != nil {
		t.Fatalf("writing legacy settings.json: %v", err)
	}

	added, err := EnsureSentinelHook(dir)
	if err != nil {
		t.Fatalf("EnsureSentinelHook: %v", err)
	}
	if !added {
		t.Error("expected added=true when upgrading a legacy hook")
	}

	settings := readSettings(t, dir)
	hooks := settings["hooks"].(map[string]any)
	stop := hooks["Stop"].([]any)
	group := stop[0].(map[string]any)
	entries := group["hooks"].([]any)
	entry := entries[0].(map[string]any)
	if entry["command"] != SentinelHookCommand {
		t.Errorf("expected the legacy entry to be replaced with %q, got %q", SentinelHookCommand, entry["command"])
	}
}

func TestEnsureSentinelHook_RefusesInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	settingsDir := filepath.Join(dir, ".claude")
	os.MkdirAll(settingsDir, 0o755)
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("writing invalid settings.json: %v", err)
	}

	if _, err := EnsureSentinelHook(dir); err == nil {
		t.Error("expected an error for invalid existing JSON")
	}
}
