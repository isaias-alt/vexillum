package doctorcheck

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/scaffold"
)

func projectWithCommander(t *testing.T, commander []string) string {
	t.Helper()
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".vexillum")
	if err := scaffold.WriteConfig(configDir); err != nil {
		t.Fatal(err)
	}
	cfg, err := scaffold.ReadConfig(configDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Commander = commander
	if err := scaffold.SaveConfig(configDir, cfg); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCommanders(t *testing.T) {
	for _, tc := range []struct {
		name     string
		config   []string
		want     []string
		wantWarn string
	}{
		{"none recorded", nil, []string{"claude"}, ""},
		{"one", []string{"pi"}, []string{"pi"}, ""},
		{"several, repeats dropped", []string{"opencode", "claude", "opencode"}, []string{"opencode", "claude"}, ""},
		{"a typo is dropped and reported", []string{"claude", "codex"}, []string{"claude"}, `"codex"`},
		{"nothing valid falls back to the default", []string{"codex"}, []string{"claude"}, `"codex"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, warns := Commanders(projectWithCommander(t, tc.config))
			if !slices.Equal(got, tc.want) {
				t.Errorf("commanders = %v, want %v", got, tc.want)
			}
			switch {
			case tc.wantWarn == "" && len(warns) != 0:
				t.Errorf("unexpected warnings: %+v", warns)
			case tc.wantWarn != "" && (len(warns) != 1 || !warns[0].Warn || !strings.Contains(warns[0].Detail, tc.wantWarn)):
				t.Errorf("warnings = %+v, want one naming %s", warns, tc.wantWarn)
			}
		})
	}
}

func TestCommanders_NotInitializedIsClaude(t *testing.T) {
	got, warns := Commanders(t.TempDir())
	if !slices.Equal(got, []string{"claude"}) || len(warns) != 0 {
		t.Errorf("Commanders = %v, %+v", got, warns)
	}
}

// As a commander, the harness's binary is Required and always reported;
// otherwise nothing changes.
func TestPiChecks_CommanderRequiresTheBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	got := PiChecks(projectWithProfiles(t, ""), t.TempDir(), true, nil)
	if len(got) != 1 || got[0].Name != "Pi" || got[0].OK || !got[0].Required || !strings.Contains(got[0].Detail, "pi not found in PATH") {
		t.Errorf("PiChecks as commander without pi = %+v", got)
	}
	if got := PiChecks(projectWithProfiles(t, ""), t.TempDir(), false, nil); len(got) != 0 {
		t.Errorf("PiChecks as nothing special without pi = %+v, want none", got)
	}
}

func TestOpenCodeChecks_CommanderRequiresTheBinary(t *testing.T) {
	got := openCodeChecks(projectWithProfiles(t, ""), t.TempDir(), t.TempDir(), true, nil)
	if len(got) != 1 || got[0].Name != "OpenCode" || got[0].OK || !got[0].Required || !strings.Contains(got[0].Detail, "opencode not found in PATH") {
		t.Errorf("openCodeChecks as commander without opencode = %+v", got)
	}
	if got := openCodeChecks(projectWithProfiles(t, ""), t.TempDir(), t.TempDir(), false, nil); len(got) != 0 {
		t.Errorf("openCodeChecks as nothing special without opencode = %+v, want none", got)
	}
}
