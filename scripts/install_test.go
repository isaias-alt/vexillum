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
// STUB_FIXTURES, so no test touches the network.
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
case "$url" in
  */releases/latest) echo '{"tag_name": "v` + fakeVersion + `"}'; exit 0 ;;
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
	name := fmt.Sprintf("vexillum_%s_%s_%s.tar.gz", fakeVersion, runtime.GOOS, runtime.GOARCH)
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

func (e *env) brewLog() string {
	b, _ := os.ReadFile(filepath.Join(e.state, "brew.log"))
	return string(b)
}

// run executes install.sh with a minimal PATH (stubs first, then system tools)
// and an isolated HOME. It returns combined output and the exit error.
func (e *env) run() (string, error) {
	e.t.Helper()
	cmd := exec.Command("bash", "install.sh")
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
