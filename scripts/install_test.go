package scripts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	fakeVersion = "9.9.9"
	fakeVx      = "#!/bin/sh\necho \"vx " + fakeVersion + "\"\n"
	fakeOld     = "#!/bin/sh\necho \"vexillum 0.1.9\"\n"
)

// brewStub models the bits of brew the installer touches. STUB_STATE holds
// the "installed" marker and a call log; STUB_PREFIX is the fake brew prefix.
// STUB_PROVIDES names the binary that install and upgrade drop into the prefix
// ("none" drops nothing).
const brewStub = `#!/bin/sh
echo "$*" >> "$STUB_STATE/brew.log"
case "$1" in
  list) [ -f "$STUB_STATE/installed" ] ;;
  --prefix) echo "$STUB_PREFIX" ;;
  install|upgrade)
    mkdir -p "$STUB_PREFIX/bin"
    case "$STUB_PROVIDES" in
      vx) printf '#!/bin/sh\necho "vx ` + fakeVersion + `"\n' > "$STUB_PREFIX/bin/vx"; chmod +x "$STUB_PREFIX/bin/vx" ;;
      vexillum) printf '#!/bin/sh\necho "vexillum 0.1.9"\n' > "$STUB_PREFIX/bin/vexillum"; chmod +x "$STUB_PREFIX/bin/vexillum" ;;
    esac
    touch "$STUB_STATE/installed"
    ;;
  *) exit 1 ;;
esac
`

// curlStub serves the GitHub API, the release archive and checksums.txt from
// STUB_FIXTURES, so no test touches the network. Every requested URL is
// appended to STUB_STATE/curl.log. The releases list (the canary lookup) comes
// from STUB_FIXTURES/releases.json and the latest-release answer from
// STUB_FIXTURES/latest.json when those files exist.
const curlStub = `#!/bin/sh
out=""; url=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift ;;
    -*) ;;
    *) url="$1" ;;
  esac
  shift
done
echo "$url" >> "$STUB_STATE/curl.log"
case "$url" in
  */releases/latest)
    if [ -f "$STUB_FIXTURES/latest.json" ]; then cat "$STUB_FIXTURES/latest.json"; else echo '{"tag_name": "v` + fakeVersion + `"}'; fi
    exit 0 ;;
  */releases\?per_page=*)
    if [ -f "$STUB_FIXTURES/releases.json" ]; then cat "$STUB_FIXTURES/releases.json"; exit 0; fi
    exit 22 ;;
  */checksums.txt) src="$STUB_FIXTURES/checksums.txt" ;;
  *.tar.gz) src="$STUB_FIXTURES/archive.tar.gz" ;;
  *) exit 22 ;;
esac
if [ -n "$out" ]; then cp "$src" "$out"; else cat "$src"; fi
`

type env struct {
	t        *testing.T
	home     string
	bin      string // stub dir, first on PATH
	state    string
	prefix   string
	fixtures string
	extra    []string
}

func writeExec(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("install.sh supports macOS and Linux only")
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("install.sh supports amd64 and arm64 only")
	}
	// Reading the script registers it as an input of the test, so the go test
	// cache is invalidated when install.sh changes.
	if _, err := os.ReadFile("install.sh"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	e := &env{
		t:        t,
		home:     filepath.Join(root, "home"),
		bin:      filepath.Join(root, "stubs"),
		state:    filepath.Join(root, "state"),
		prefix:   filepath.Join(root, "brewprefix"),
		fixtures: filepath.Join(root, "fixtures"),
	}
	for _, d := range []string{e.home, e.bin, e.state, e.fixtures} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeExec(t, filepath.Join(e.bin, "curl"), curlStub)
	e.writeFixtures(fakeVx)
	return e
}

// writeFixtures builds the release archive (containing a single vx file) and a
// matching checksums.txt.
func (e *env) writeFixtures(vxContent string) {
	e.t.Helper()
	e.writeFixturesFor(fakeVersion, vxContent)
}

// writeFixturesFor is writeFixtures for a release other than the default one:
// the archive name in checksums.txt carries version.
func (e *env) writeFixturesFor(version, vxContent string) {
	e.t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "vx", Mode: 0o755, Size: int64(len(vxContent))}
	if err := tw.WriteHeader(hdr); err != nil {
		e.t.Fatal(err)
	}
	if _, err := tw.Write([]byte(vxContent)); err != nil {
		e.t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		e.t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.fixtures, "archive.tar.gz"), buf.Bytes(), 0o644); err != nil {
		e.t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	name := fmt.Sprintf("vexillum_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
	line := hex.EncodeToString(sum[:]) + "  " + name + "\n"
	if err := os.WriteFile(filepath.Join(e.fixtures, "checksums.txt"), []byte(line), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) withBrew(provides string, installed bool) {
	e.t.Helper()
	writeExec(e.t, filepath.Join(e.bin, "brew"), brewStub)
	e.extra = append(e.extra, "STUB_PROVIDES="+provides)
	if installed {
		if err := os.WriteFile(filepath.Join(e.state, "installed"), nil, 0o644); err != nil {
			e.t.Fatal(err)
		}
	}
}

func (e *env) curlLog() string {
	b, _ := os.ReadFile(filepath.Join(e.state, "curl.log"))
	return string(b)
}

// writeFixture drops a file next to the archive for the curl stub to serve.
func (e *env) writeFixture(name, content string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.fixtures, name), []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) brewLog() string {
	b, _ := os.ReadFile(filepath.Join(e.state, "brew.log"))
	return string(b)
}

// run executes install.sh with a minimal PATH (stubs first, then system tools)
// and an isolated HOME. It returns combined output and the exit error.
func (e *env) run(args ...string) (string, error) {
	e.t.Helper()
	cmd := exec.Command("bash", append([]string{"install.sh"}, args...)...)
	cmd.Env = append([]string{
		"HOME=" + e.home,
		"PATH=" + e.bin + ":/usr/bin:/bin",
		"STUB_STATE=" + e.state,
		"STUB_PREFIX=" + e.prefix,
		"STUB_FIXTURES=" + e.fixtures,
	}, e.extra...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mustSucceed(t *testing.T, out string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, out)
	}
}

func mustFail(t *testing.T, out string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("install.sh succeeded, want failure\n%s", out)
	}
}

func mustContain(t *testing.T, out, want string) {
	t.Helper()
	if !strings.Contains(out, want) {
		t.Errorf("output missing %q\n%s", want, out)
	}
}

func TestInstallFreshBrew(t *testing.T) {
	e := newEnv(t)
	e.withBrew("vx", false)

	out, err := e.run()
	mustSucceed(t, out, err)

	log := e.brewLog()
	if !strings.Contains(log, "install isaias-alt/tap/vexillum") {
		t.Errorf("expected brew install, log:\n%s", log)
	}
	if strings.Contains(log, "upgrade") {
		t.Errorf("fresh install must not upgrade, log:\n%s", log)
	}
	// The brew prefix is not on PATH in this harness: success proves the check
	// looks in the prefix and not only on PATH.
	mustContain(t, out, "vx "+fakeVersion)
}

func TestInstallUpgradesOldFormula(t *testing.T) {
	e := newEnv(t)
	e.withBrew("vx", true)
	writeExec(t, filepath.Join(e.prefix, "bin", "vexillum"), fakeOld)

	out, err := e.run()
	mustSucceed(t, out, err)

	log := e.brewLog()
	if !strings.Contains(log, "upgrade isaias-alt/tap/vexillum") {
		t.Errorf("expected brew upgrade, log:\n%s", log)
	}
	if strings.Contains(log, "install ") {
		t.Errorf("installed formula must not be re-installed, log:\n%s", log)
	}
	mustContain(t, out, "already installed - upgrading")
	mustContain(t, out, "vx "+fakeVersion)
}

func TestInstallDirectDownload(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(e.home, "bin-out")
	e.extra = append(e.extra, "VX_INSTALL_DIR="+dir)

	out, err := e.run()
	mustSucceed(t, out, err)

	mustContain(t, out, "Checksum verified")
	mustContain(t, out, "vx "+fakeVersion)
	if _, err := os.Stat(filepath.Join(dir, "vx")); err != nil {
		t.Errorf("vx not installed into %s: %v", dir, err)
	}
	mustContain(t, out, "is not in your PATH")
}

func TestInstallDirectDownloadRejectsBadChecksum(t *testing.T) {
	e := newEnv(t)
	e.extra = append(e.extra, "VX_INSTALL_DIR="+filepath.Join(e.home, "bin-out"))
	name := fmt.Sprintf("vexillum_%s_%s_%s.tar.gz", fakeVersion, runtime.GOOS, runtime.GOARCH)
	bad := strings.Repeat("0", 64) + "  " + name + "\n"
	if err := os.WriteFile(filepath.Join(e.fixtures, "checksums.txt"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := e.run()
	mustFail(t, out, err)
	mustContain(t, out, "Checksum mismatch")
}

func TestInstallFailsWhenVxMissingAfterBrew(t *testing.T) {
	e := newEnv(t)
	// brew "succeeds" but only the old binary exists: the stale-formula case.
	e.withBrew("vexillum", true)

	out, err := e.run()
	mustFail(t, out, err)

	mustContain(t, out, "no 'vx' executable was found")
	mustContain(t, out, "Only the old 'vexillum' binary is present")
	mustContain(t, out, "brew upgrade isaias-alt/tap/vexillum")
}

func TestInstallFailsWhenNothingInstalled(t *testing.T) {
	e := newEnv(t)
	e.withBrew("none", false)

	out, err := e.run()
	mustFail(t, out, err)

	mustContain(t, out, "no 'vx' executable was found")
	if strings.Contains(out, "old 'vexillum'") {
		t.Errorf("no old binary exists, message must not mention one\n%s", out)
	}
}

func TestInstallFailsWhenVxDoesNotRun(t *testing.T) {
	e := newEnv(t)
	e.writeFixtures("#!/bin/sh\nexit 3\n")
	e.extra = append(e.extra, "VX_INSTALL_DIR="+filepath.Join(e.home, "bin-out"))

	out, err := e.run()
	mustFail(t, out, err)
	mustContain(t, out, "did not report a vx version")
}

func TestInstallRefusesForeignVx(t *testing.T) {
	e := newEnv(t)
	e.withBrew("vx", false)
	writeExec(t, filepath.Join(e.bin, "vx"), "#!/bin/sh\necho \"other-tool 1.0\"\n")

	out, err := e.run()
	mustFail(t, out, err)

	mustContain(t, out, "different 'vx' executable")
	if e.brewLog() != "" {
		t.Errorf("brew must not run when a foreign vx is present, log:\n%s", e.brewLog())
	}
}

// releasesJSON mimics the GitHub releases list: newest first, pre-releases
// included, one tag_name per release.
func releasesJSON(tags ...string) string {
	var items []string
	for _, tag := range tags {
		items = append(items, fmt.Sprintf("  {\n    \"tag_name\": %q,\n    \"prerelease\": %t\n  }", tag, strings.Contains(tag, "-")))
	}
	return "[\n" + strings.Join(items, ",\n") + "\n]\n"
}

const canaryTag = "v9.9.10-canary.20261004.gabc1234"

func TestInstallDefaultChannelIsStableAndUsesLatestRelease(t *testing.T) {
	e := newEnv(t)
	e.extra = append(e.extra, "VX_INSTALL_DIR="+filepath.Join(e.home, "bin-out"))

	out, err := e.run()
	mustSucceed(t, out, err)

	log := e.curlLog()
	mustContain(t, log, "/releases/latest")
	mustContain(t, log, "/releases/download/v"+fakeVersion+"/")
	if strings.Contains(log, "per_page") {
		t.Errorf("the stable channel must not list releases, curl log:\n%s", log)
	}
}

func TestInstallStableRefusesAPreRelease(t *testing.T) {
	e := newEnv(t)
	e.writeFixture("latest.json", `{"tag_name": "v9.9.10-rc.1"}`)

	out, err := e.run("--channel", "stable")
	mustFail(t, out, err)

	mustContain(t, out, "is a pre-release")
	if strings.Contains(e.curlLog(), "/releases/download/") {
		t.Errorf("nothing may be downloaded, curl log:\n%s", e.curlLog())
	}
}

func TestInstallCanaryResolvesLatestCanaryAndSkipsBrew(t *testing.T) {
	e := newEnv(t)
	e.withBrew("vx", false)
	e.writeFixturesFor("9.9.10-canary.20261004.gabc1234", fakeVx)
	// Newest first: an rc and a stable release sit above the older canary and
	// must not be picked; the newest canary wins.
	e.writeFixture("releases.json", releasesJSON(
		"v9.9.11-rc.1", canaryTag, "v9.9.10-canary.20261003.gdef5678", "v9.9.9"))
	e.extra = append(e.extra, "VX_INSTALL_DIR="+filepath.Join(e.home, "bin-out"))

	out, err := e.run("--channel", "canary")
	mustSucceed(t, out, err)

	mustContain(t, out, "Canary build: "+canaryTag)
	mustContain(t, out, "Checksum verified")
	mustContain(t, e.curlLog(), "/releases/download/"+canaryTag+"/vexillum_9.9.10-canary.20261004.gabc1234_")
	if e.brewLog() != "" {
		t.Errorf("canary must never go through brew, log:\n%s", e.brewLog())
	}
	if _, err := os.Stat(filepath.Join(e.home, "bin-out", "vx")); err != nil {
		t.Errorf("vx not installed: %v", err)
	}
}

func TestInstallCanaryAcceptsEqualsForm(t *testing.T) {
	e := newEnv(t)
	e.writeFixturesFor("9.9.10-canary.20261004.gabc1234", fakeVx)
	e.writeFixture("releases.json", releasesJSON(canaryTag))
	e.extra = append(e.extra, "VX_INSTALL_DIR="+filepath.Join(e.home, "bin-out"))

	out, err := e.run("--channel=canary")
	mustSucceed(t, out, err)
	mustContain(t, out, "Canary build: "+canaryTag)
}

func TestInstallCanaryFailsWhenThereIsNone(t *testing.T) {
	e := newEnv(t)
	e.writeFixture("releases.json", releasesJSON("v9.9.11-rc.1", "v9.9.9"))

	out, err := e.run("--channel", "canary")
	mustFail(t, out, err)

	mustContain(t, out, "No canary release found")
	if strings.Contains(e.curlLog(), "/releases/download/") {
		t.Errorf("nothing may be downloaded, curl log:\n%s", e.curlLog())
	}
}

func TestInstallCanaryRejectsBadChecksum(t *testing.T) {
	e := newEnv(t)
	e.writeFixturesFor("9.9.10-canary.20261004.gabc1234", fakeVx)
	e.writeFixture("releases.json", releasesJSON(canaryTag))
	e.extra = append(e.extra, "VX_INSTALL_DIR="+filepath.Join(e.home, "bin-out"))
	name := fmt.Sprintf("vexillum_9.9.10-canary.20261004.gabc1234_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	e.writeFixture("checksums.txt", strings.Repeat("0", 64)+"  "+name+"\n")

	out, err := e.run("--channel", "canary")
	mustFail(t, out, err)
	mustContain(t, out, "Checksum mismatch")
}

func TestInstallPinnedVersionSkipsBrewAndTheAPI(t *testing.T) {
	for _, arg := range []string{"v9.9.8-rc.1", "9.9.8-rc.1"} {
		t.Run(arg, func(t *testing.T) {
			e := newEnv(t)
			e.withBrew("vx", false)
			e.writeFixturesFor("9.9.8-rc.1", fakeVx)
			e.extra = append(e.extra, "VX_INSTALL_DIR="+filepath.Join(e.home, "bin-out"))

			out, err := e.run("--version", arg)
			mustSucceed(t, out, err)

			mustContain(t, out, "Pre-release: v9.9.8-rc.1")
			log := e.curlLog()
			mustContain(t, log, "/releases/download/v9.9.8-rc.1/vexillum_9.9.8-rc.1_")
			if strings.Contains(log, "api.github.com") {
				t.Errorf("a pinned version needs no API lookup, curl log:\n%s", log)
			}
			if e.brewLog() != "" {
				t.Errorf("a pinned version must not go through brew, log:\n%s", e.brewLog())
			}
		})
	}
}

func TestInstallPinnedStableVersion(t *testing.T) {
	e := newEnv(t)
	e.extra = append(e.extra, "VX_INSTALL_DIR="+filepath.Join(e.home, "bin-out"))

	out, err := e.run("--version=v" + fakeVersion)
	mustSucceed(t, out, err)
	mustContain(t, out, "Version: v"+fakeVersion)
}

func TestInstallRejectsBadArguments(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"unknown channel", []string{"--channel", "nightly"}, "Unknown channel: nightly"},
		{"rc is not a channel", []string{"--channel", "rc"}, "Unknown channel: rc"},
		{"channel needs a value", []string{"--channel"}, "--channel needs a value"},
		{"version needs a value", []string{"--version"}, "--version needs a value"},
		{"bad version", []string{"--version", "latest"}, "Invalid version: latest"},
		{"version with a path", []string{"--version", "v1.0.0/../../x"}, "Invalid version"},
		{"both", []string{"--channel", "canary", "--version", "v0.2.0"}, "not both"},
		{"unknown option", []string{"--nope"}, "Unknown option: --nope"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			e.withBrew("vx", false)

			out, err := e.run(c.args...)
			mustFail(t, out, err)
			mustContain(t, out, c.want)
			if e.curlLog() != "" || e.brewLog() != "" {
				t.Errorf("a bad argument must stop before any install step\ncurl:\n%s\nbrew:\n%s", e.curlLog(), e.brewLog())
			}
		})
	}
}

func TestInstallHelp(t *testing.T) {
	e := newEnv(t)
	e.withBrew("vx", false)

	for _, flag := range []string{"--help", "-h"} {
		out, err := e.run(flag)
		mustSucceed(t, out, err)
		mustContain(t, out, "--channel stable|canary")
		mustContain(t, out, "--version <tag>")
	}
	if e.brewLog() != "" {
		t.Errorf("--help must not install anything, brew log:\n%s", e.brewLog())
	}
}

func TestInstallStableStillUsesBrewWhenAvailable(t *testing.T) {
	e := newEnv(t)
	e.withBrew("vx", false)

	out, err := e.run("--channel", "stable")
	mustSucceed(t, out, err)
	mustContain(t, e.brewLog(), "install isaias-alt/tap/vexillum")
	if e.curlLog() != "" {
		t.Errorf("brew path must not download directly, curl log:\n%s", e.curlLog())
	}
}

func TestInstallPathHintMatchesTheUsersShell(t *testing.T) {
	cases := []struct {
		name  string
		shell string
		want  []string
		not   []string
	}{
		{
			name:  "zsh",
			shell: "/bin/zsh",
			want: []string{
				`echo 'export PATH="$HOME/bin-out:$PATH"' >> ~/.zshrc`,
				"source ~/.zshrc && hash -r",
			},
			not: []string{"~/.bashrc", "fish_add_path"},
		},
		{
			name:  "bash",
			shell: "/usr/bin/bash",
			want: []string{
				`echo 'export PATH="$HOME/bin-out:$PATH"' >> ~/.bashrc`,
				"source ~/.bashrc && hash -r",
			},
			not: []string{"~/.zshrc", "fish_add_path"},
		},
		{
			name:  "fish",
			shell: "/opt/homebrew/bin/fish",
			want:  []string{"fish_add_path $HOME/bin-out", "open a new terminal"},
			not:   []string{"export PATH", "~/.zshrc", "~/.bashrc"},
		},
		{
			name:  "unknown shell",
			shell: "/bin/tcsh",
			want:  []string{`export PATH="$HOME/bin-out:$PATH"`, "shell profile", "hash -r"},
			not:   []string{"~/.zshrc", "~/.bashrc", "fish_add_path"},
		},
		{
			name:  "SHELL unset",
			shell: "",
			want:  []string{`export PATH="$HOME/bin-out:$PATH"`, "hash -r"},
			not:   []string{"~/.zshrc", "~/.bashrc", "fish_add_path"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			dir := filepath.Join(e.home, "bin-out")
			e.extra = append(e.extra, "VX_INSTALL_DIR="+dir, "SHELL="+tc.shell)

			out, err := e.run()
			mustSucceed(t, out, err)

			mustContain(t, out, dir+" is not in your PATH")
			for _, w := range tc.want {
				mustContain(t, out, w)
			}
			for _, n := range tc.not {
				if strings.Contains(out, n) {
					t.Errorf("output must not mention %q\n%s", n, out)
				}
			}
		})
	}
}

func TestInstallNoPathHintWhenDirIsOnPath(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(e.home, "bin-out")
	e.extra = append(e.extra, "VX_INSTALL_DIR="+dir, "PATH="+e.bin+":"+dir+":/usr/bin:/bin")

	out, err := e.run()
	mustSucceed(t, out, err)

	if strings.Contains(out, "is not in your PATH") {
		t.Errorf("no PATH hint expected when the dir is on PATH\n%s", out)
	}
}
