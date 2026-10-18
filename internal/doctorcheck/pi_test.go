package doctorcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// binDir is a PATH holding only the named scripts, each a /bin/sh body.
func binDir(t *testing.T, scripts map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// projectWithProfiles is a project dir with a .vexillum/models.json.
func projectWithProfiles(t *testing.T, modelsJSON string) string {
	t.Helper()
	dir := t.TempDir()
	if modelsJSON != "" {
		if err := os.MkdirAll(filepath.Join(dir, ".vexillum"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".vexillum", "models.json"), []byte(modelsJSON), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const piProfile = `{"profiles": {"pi-scout": {"model": "anthropic/claude-haiku-4-5", "effort": "low", "when": "x", "harness": "pi"}}}`

func names(rs []Result) string {
	var n []string
	for _, r := range rs {
		n = append(n, r.Name)
	}
	return strings.Join(n, ", ")
}

// A Claude-only setup, with no pi anywhere, gets no Pi line at all.
func TestPiChecks_NothingWithoutPiOrAPiProfile(t *testing.T) {
	t.Setenv("PATH", binDir(t, map[string]string{"herdr": "exit 0"}))
	if got := PiChecks(projectWithProfiles(t, ""), t.TempDir()); len(got) != 0 {
		t.Errorf("expected no Pi checks, got %s", names(got))
	}
}

// A profile that selects pi on a machine without it is a warning that names
// the fix, never a failure.
func TestPiChecks_PiProfileWithoutPi(t *testing.T) {
	t.Setenv("PATH", binDir(t, map[string]string{"herdr": "exit 0"}))
	got := PiChecks(projectWithProfiles(t, piProfile), t.TempDir())
	if len(got) != 1 || got[0].Name != "Pi" || !got[0].Warn || got[0].Required {
		t.Fatalf("got %+v", got)
	}
	if !strings.Contains(got[0].Detail, "pi.dev/install.sh") {
		t.Errorf("detail %q has no install hint", got[0].Detail)
	}
}

func TestPiChecks_PiProfileIsReadFromTheGlobalFileToo(t *testing.T) {
	t.Setenv("PATH", binDir(t, nil))
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "models.json"), []byte(piProfile), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := PiChecks(projectWithProfiles(t, ""), home); len(got) != 1 || !got[0].Warn {
		t.Errorf("expected the global pi profile to be noticed, got %+v", got)
	}
}

// pi on PATH with no profile for it still reports, with a current version and
// the integration installed: everything ok.
func TestPiChecks_PiPresent_AllGood(t *testing.T) {
	t.Setenv("PATH", binDir(t, map[string]string{
		"pi":    `echo 1.3.0`,
		"herdr": `echo "pi: installed (v9) (/x/herdr-agent-state.ts)"`,
	}))
	got := PiChecks(projectWithProfiles(t, ""), t.TempDir())
	if names(got) != "Pi, Pi version, herdr integration for pi" {
		t.Fatalf("got %s", names(got))
	}
	for _, r := range got {
		if !r.OK || r.Warn || r.Required {
			t.Errorf("%s: %+v", r.Name, r)
		}
	}
	if got[1].Detail != "1.3.0" {
		t.Errorf("version detail = %q", got[1].Detail)
	}
}

func TestPiChecks_OtherMajorVersionWarns(t *testing.T) {
	t.Setenv("PATH", binDir(t, map[string]string{"pi": `echo 0.85.1`}))
	got := PiChecks(projectWithProfiles(t, ""), t.TempDir())
	if names(got) != "Pi, Pi version" {
		t.Fatalf("got %s (herdr is not on PATH, so no integration line)", names(got))
	}
	if !got[1].Warn || !strings.Contains(got[1].Detail, "0.85.1") || !strings.Contains(got[1].Detail, "1.") {
		t.Errorf("version result %+v", got[1])
	}
}

func TestPiChecks_BrokenVersionCommandWarns(t *testing.T) {
	t.Setenv("PATH", binDir(t, map[string]string{"pi": `exit 3`}))
	got := PiChecks(projectWithProfiles(t, ""), t.TempDir())
	if len(got) < 2 || !got[1].Warn {
		t.Fatalf("got %+v", got)
	}
}

func TestPiChecks_IntegrationStates(t *testing.T) {
	cases := []struct {
		name, herdr, want string
		ok                bool
	}{
		{"not installed", `echo "pi: not installed (/x/herdr-agent-state.ts)"`, "herdr integration install pi", false},
		{"outdated", `echo "pi: outdated (v8 < v9) (/x/herdr-agent-state.ts)"`, "herdr integration install pi", false},
		{"no line for pi", `echo "claude: installed"`, "no line", false},
		{"status command fails", `exit 1`, "could not read", false},
		{"installed", `echo "pi: installed (v9) (/x)"`, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("PATH", binDir(t, map[string]string{"pi": `echo 1.3.0`, "herdr": c.herdr}))
			got := PiChecks(projectWithProfiles(t, ""), t.TempDir())
			if len(got) != 3 {
				t.Fatalf("got %s", names(got))
			}
			r := got[2]
			if r.OK != c.ok || r.Warn == c.ok || r.Required {
				t.Errorf("result %+v, want ok=%v", r, c.ok)
			}
			if !strings.Contains(r.Detail, c.want) {
				t.Errorf("detail %q does not contain %q", r.Detail, c.want)
			}
		})
	}
}
