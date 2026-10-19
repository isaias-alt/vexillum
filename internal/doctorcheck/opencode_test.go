package doctorcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const openCodeProfile = `{"profiles": {"oc-scout": {"model": "opencode/glm-5.3-flash-free", "effort": "low", "when": "x", "harness": "opencode"}}}`

// ocBin is a directory holding an opencode that prints version.
func ocBin(t *testing.T, version string) string {
	t.Helper()
	return binDir(t, map[string]string{"opencode": "echo '" + version + "'"})
}

func result(rs []Result, name string) (Result, bool) {
	for _, r := range rs {
		if r.Name == name {
			return r, true
		}
	}
	return Result{}, false
}

func TestOpenCodeChecks_NothingWithoutOpenCodeOrAProfileForIt(t *testing.T) {
	t.Setenv("PATH", binDir(t, map[string]string{"herdr": "exit 0"}))
	if got := OpenCodeChecks(projectWithProfiles(t, ""), t.TempDir(), false); len(got) != 0 {
		t.Errorf("expected no OpenCode checks, got %s", names(got))
	}
}

func TestOpenCodeChecks_ProfileWithoutOpenCodeWarns(t *testing.T) {
	t.Setenv("PATH", binDir(t, map[string]string{"herdr": "exit 0"}))
	got := OpenCodeChecks(projectWithProfiles(t, openCodeProfile), t.TempDir(), false)
	if len(got) != 1 || got[0].Name != "OpenCode" || !got[0].Warn || got[0].Required {
		t.Fatalf("got %+v", got)
	}
	if !strings.Contains(got[0].Detail, "brew install opencode") {
		t.Errorf("detail %q has no install hint", got[0].Detail)
	}
}

func TestOpenCodeChecks_VersionsAreClassified(t *testing.T) {
	cases := []struct {
		out      string
		wantOK   bool
		wantText string
	}{
		{"opencode v2.0.32", true, "2.0.32 (V2)"},
		{"1.19.3", true, "1.19.3 (V1)"},
		{"1.18.29", true, "1.18.29 (V1)"},
		{"1.17.6", false, "older than 1.18.29"},
		{"3.1.0", false, "not a version vexillum knows"},
		{"garbage", false, "no version number"},
	}
	for _, c := range cases {
		t.Run(c.out, func(t *testing.T) {
			dir := ocBin(t, c.out)
			t.Setenv("PATH", dir)
			got := openCodeChecks(projectWithProfiles(t, ""), t.TempDir(), dir, false)
			v, ok := result(got, "OpenCode version")
			if !ok {
				t.Fatalf("no version line in %s", names(got))
			}
			if v.OK != c.wantOK || v.Warn == c.wantOK || v.Required || !strings.Contains(v.Detail, c.wantText) {
				t.Errorf("version result = %+v, want ok=%v mentioning %q", v, c.wantOK, c.wantText)
			}
			if first, _ := result(got, "OpenCode"); !first.OK {
				t.Errorf("the binary line = %+v", first)
			}
		})
	}
}

// An old 1.x ahead of a 2.x in PATH answers every dispatch: doctor names both.
func TestOpenCodeChecks_V1ShadowingV2Warns(t *testing.T) {
	v1, v2 := ocBin(t, "1.14.30"), ocBin(t, "opencode v2.0.32")
	pathEnv := v1 + string(os.PathListSeparator) + v2
	t.Setenv("PATH", pathEnv)

	got := openCodeChecks(projectWithProfiles(t, ""), t.TempDir(), pathEnv, false)
	shadow, ok := result(got, "OpenCode V1 shadowing V2 in PATH")
	if !ok || !shadow.Warn || shadow.Required {
		t.Fatalf("expected a shadow warning, got %s", names(got))
	}
	for _, want := range []string{filepath.Join(v1, "opencode"), "1.14.30", filepath.Join(v2, "opencode"), "2.0.32"} {
		if !strings.Contains(shadow.Detail, want) {
			t.Errorf("detail %q does not mention %q", shadow.Detail, want)
		}
	}
	// The one that wins is the 1.x, and it is too old: that is reported too.
	if v, _ := result(got, "OpenCode version"); !v.Warn || !strings.Contains(v.Detail, "older than 1.18.29") {
		t.Errorf("version = %+v", v)
	}
}

func TestOpenCodeChecks_NoShadowWhenV2WinsOrThereIsOneInstall(t *testing.T) {
	v1, v2 := ocBin(t, "1.19.3"), ocBin(t, "opencode v2.0.32")
	for name, pathEnv := range map[string]string{
		"v2 first":        v2 + string(os.PathListSeparator) + v1,
		"only one":        v2,
		"same file twice": v2 + string(os.PathListSeparator) + v2,
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("PATH", pathEnv)
			if _, ok := result(openCodeChecks(projectWithProfiles(t, ""), t.TempDir(), pathEnv, false), "OpenCode V1 shadowing V2 in PATH"); ok {
				t.Error("unexpected shadow warning")
			}
		})
	}
}

func TestOpenCodeChecks_HerdrIntegration(t *testing.T) {
	for _, c := range []struct {
		status   string
		wantOK   bool
		wantWarn bool
		want     string
	}{
		{"opencode: current (v13) (/x)", true, false, "current (v13)"},
		{"opencode: not installed (/x)", false, true, "herdr integration install opencode"},
		{"opencode: outdated (v9 < v13) (/x)", false, true, "restart OpenCode"},
	} {
		t.Run(c.status, func(t *testing.T) {
			dir := binDir(t, map[string]string{
				"opencode": "echo opencode v2.0.32",
				"herdr":    `echo '` + c.status + `'`,
			})
			t.Setenv("PATH", dir)
			got := openCodeChecks(projectWithProfiles(t, ""), t.TempDir(), dir, false)
			r, ok := result(got, "herdr integration for opencode")
			if !ok || r.OK != c.wantOK || r.Warn != c.wantWarn || r.Required || !strings.Contains(r.Detail, c.want) {
				t.Errorf("integration = %+v (found %v), want ok=%v warn=%v mentioning %q", r, ok, c.wantOK, c.wantWarn, c.want)
			}
		})
	}

	// herdr itself missing is the "herdr" check's to report.
	dir := ocBin(t, "opencode v2.0.32")
	t.Setenv("PATH", dir)
	if _, ok := result(openCodeChecks(projectWithProfiles(t, ""), t.TempDir(), dir, false), "herdr integration for opencode"); ok {
		t.Error("an integration line without herdr on PATH")
	}
}
