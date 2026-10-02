package scaffold

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/cmdname"
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
	// Both legacy spellings carry the old executable name and must be
	// replaced in place with the current command, without a duplicate.
	for _, legacyCmd := range []string{LegacySentinelHookCommand, LegacyRenamedSentinelHookCommand} {
		t.Run(legacyCmd, func(t *testing.T) {
			dir := t.TempDir()
			settingsDir := filepath.Join(dir, ".claude")
			os.MkdirAll(settingsDir, 0o755)
			legacy := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"` + legacyCmd + `"}]}]}}`
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
			if len(stop) != 1 {
				t.Fatalf("expected the legacy group to be reused, got %d Stop groups", len(stop))
			}
			entries := stop[0].(map[string]any)["hooks"].([]any)
			if len(entries) != 1 {
				t.Fatalf("expected exactly one hook entry, got %d", len(entries))
			}
			entry := entries[0].(map[string]any)
			if entry["command"] != SentinelHookCommand {
				t.Errorf("expected the legacy entry to be replaced with %q, got %q", SentinelHookCommand, entry["command"])
			}
			if async, _ := entry["asyncRewake"].(bool); !async {
				t.Error("expected the replacement to be an async hook")
			}

			// A second pass has nothing left to migrate.
			again, err := EnsureSentinelHook(dir)
			if err != nil {
				t.Fatalf("EnsureSentinelHook (second call): %v", err)
			}
			if again {
				t.Error("expected added=false once migrated")
			}
		})
	}
}

func TestSentinelHookCommandUsesCurrentName(t *testing.T) {
	if want := cmdname.Name + " sentinel await"; SentinelHookCommand != want {
		t.Errorf("SentinelHookCommand = %q, want %q", SentinelHookCommand, want)
	}
	for _, legacy := range []string{LegacySentinelHookCommand, LegacyRenamedSentinelHookCommand} {
		if legacy == SentinelHookCommand || !strings.HasPrefix(legacy, "vexillum ") {
			t.Errorf("legacy command %q must be the old-name spelling, distinct from %q", legacy, SentinelHookCommand)
		}
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

func TestSentinelHookNeeded(t *testing.T) {
	dir := t.TempDir()
	need, err := SentinelHookNeeded(dir)
	if err != nil || !need {
		t.Fatalf("fresh project: need=%v err=%v", need, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude")); !os.IsNotExist(err) {
		t.Error("SentinelHookNeeded must not write anything")
	}
	if _, err := EnsureSentinelHook(dir); err != nil {
		t.Fatal(err)
	}
	if need, err := SentinelHookNeeded(dir); err != nil || need {
		t.Errorf("after ensure: need=%v err=%v", need, err)
	}
}
