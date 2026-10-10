package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/sentinel"
)

// fakeSource serves releases and assets from memory and records what was
// downloaded.
type fakeSource struct {
	releases   []Release
	releaseErr error
	assets     map[string][]byte // "<tag>/<name>"
	downloads  []string
}

func (f *fakeSource) Releases(context.Context) ([]Release, error) {
	return f.releases, f.releaseErr
}

func (f *fakeSource) Asset(_ context.Context, tag, name string) ([]byte, error) {
	key := tag + "/" + name
	f.downloads = append(f.downloads, key)
	data, ok := f.assets[key]
	if !ok {
		return nil, fmt.Errorf("downloading %s: 404 Not Found", name)
	}
	return data, nil
}

type fakeBrew struct {
	formulas []string
	err      error
}

func (f *fakeBrew) Upgrade(_ context.Context, formula string) error {
	f.formulas = append(f.formulas, formula)
	return f.err
}

func tarGz(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	// Release archives carry other files next to the binary.
	for _, f := range []struct {
		name string
		body []byte
	}{{"README.md", []byte("readme")}, {name, body}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// releaseFixture publishes tag with a darwin/arm64 archive holding newBinary
// and a correct checksums.txt.
func releaseFixture(t *testing.T, src *fakeSource, tag string, newBinary []byte) {
	t.Helper()
	archiveName := fmt.Sprintf("vexillum_%s_darwin_arm64.tar.gz", trimV(tag))
	archive := tarGz(t, "vx", newBinary)
	if src.assets == nil {
		src.assets = map[string][]byte{}
	}
	src.assets[tag+"/"+archiveName] = archive
	src.assets[tag+"/checksums.txt"] = fmt.Appendf(nil, "%s  %s\n%s  other.tar.gz\n", sha(archive), archiveName, strings.Repeat("0", 64))
}

// directUpdater is an Updater for an install-script binary in a temp dir.
func directUpdater(t *testing.T, current string, src Source) (*Updater, string) {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "vx")
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Updater{Source: src, Brew: &fakeBrew{}, Current: current, Exe: exe, OS: "darwin", Arch: "arm64"}, exe
}

func rel(tag string, pre bool) Release { return Release{Tag: tag, Prerelease: pre} }

func TestLatest_StableIgnoresPrereleasesAndDrafts(t *testing.T) {
	got, err := Latest([]Release{
		rel("v0.4.0-canary.1", true),
		rel("v0.3.0", false),
		{Tag: "v0.9.0", Draft: true},
		rel("v0.2.0", false),
		rel("not-a-version", false),
	}, Stable)
	if err != nil || got.Tag != "v0.3.0" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestLatest_CanaryPicksNewestCanaryByVersionOrder(t *testing.T) {
	got, err := Latest([]Release{
		rel("v0.4.0-canary.9", true),
		rel("v0.4.0-canary.10", true),
		rel("v0.5.0-rc.1", true), // a pre-release, but not a canary
		rel("v0.4.0", false),
		rel("v0.3.0-canary.40", true),
	}, Canary)
	if err != nil || got.Tag != "v0.4.0-canary.10" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestLatest_CanaryRequiresPrereleaseFlag(t *testing.T) {
	_, err := Latest([]Release{rel("v0.4.0-canary.1", false)}, Canary)
	if !errors.Is(err, ErrNoRelease) {
		t.Fatalf("err = %v, want ErrNoRelease", err)
	}
}

func TestParseChannel(t *testing.T) {
	for in, want := range map[string]Channel{"": Stable, "stable": Stable, "canary": Canary} {
		if got, err := ParseChannel(in); err != nil || got != want {
			t.Errorf("ParseChannel(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseChannel("nightly"); err == nil {
		t.Error("ParseChannel(nightly) should fail")
	}
}

func TestCompare(t *testing.T) {
	order := []string{"0.3.0-canary.2", "0.3.0-canary.10", "0.3.0", "0.3.1", "0.10.0", "1.0.0-alpha", "1.0.0"}
	for i := 0; i < len(order)-1; i++ {
		a, errA := parseVersion(order[i])
		b, errB := parseVersion(order[i+1])
		if errA != nil || errB != nil {
			t.Fatal(errA, errB)
		}
		if compare(a, b) >= 0 || compare(b, a) <= 0 {
			t.Errorf("%s should be older than %s", order[i], order[i+1])
		}
	}
}

func TestCheck_ReportsNewerAndUpToDate(t *testing.T) {
	src := &fakeSource{releases: []Release{rel("v0.4.0", false)}}
	for _, tc := range []struct {
		current string
		newer   bool
	}{{"0.3.0", true}, {"0.4.0", false}, {"0.5.0", false}, {"0.4.0-canary.3", true}, {"dev", true}} {
		u := &Updater{Source: src, Current: tc.current}
		st, err := u.Check(context.Background(), Stable)
		if err != nil || st.Newer != tc.newer || st.Latest.Tag != "v0.4.0" {
			t.Errorf("current %s: %+v, %v (want newer=%v)", tc.current, st, err, tc.newer)
		}
	}
}

func TestCheck_SourceFailure(t *testing.T) {
	u := &Updater{Source: &fakeSource{releaseErr: errors.New("offline")}, Current: "0.3.0"}
	if _, err := u.Check(context.Background(), Stable); err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("err = %v", err)
	}
}

func TestUpgrade_DirectReplacesBinaryKeepingMode(t *testing.T) {
	src := &fakeSource{releases: []Release{rel("v0.4.0", false)}}
	releaseFixture(t, src, "v0.4.0", []byte("new binary"))
	u, exe := directUpdater(t, "0.3.0", src)
	if err := os.Chmod(exe, 0o711); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	u.Out = &out

	res, err := u.Upgrade(context.Background(), Stable)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Updated || res.Exe != exe || res.Latest.Version() != "0.4.0" {
		t.Fatalf("result = %+v", res)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new binary" {
		t.Fatalf("binary = %q", got)
	}
	if fi, _ := os.Stat(exe); fi.Mode().Perm() != 0o711 {
		t.Fatalf("mode = %v, want 0711", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(exe))
	if len(entries) != 1 {
		t.Fatalf("leftover files next to the binary: %v", entries)
	}
	if !strings.Contains(out.String(), "Checksum verified") {
		t.Fatalf("progress = %q", out.String())
	}
}

func TestUpgrade_AlreadyUpToDateDownloadsNothing(t *testing.T) {
	src := &fakeSource{releases: []Release{rel("v0.4.0", false)}}
	u, exe := directUpdater(t, "0.4.0", src)

	res, err := u.Upgrade(context.Background(), Stable)
	if err != nil || res.Updated || res.Newer {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if len(src.downloads) != 0 {
		t.Fatalf("downloaded %v", src.downloads)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Fatalf("binary changed: %q", got)
	}
}

func TestUpgrade_NeverDowngrades(t *testing.T) {
	src := &fakeSource{releases: []Release{rel("v0.3.0", false)}}
	u, _ := directUpdater(t, "0.4.0-canary.2", src)
	res, err := u.Upgrade(context.Background(), Stable)
	if err != nil || res.Updated || res.Newer {
		t.Fatalf("result = %+v, %v", res, err)
	}
}

func TestUpgrade_ChecksumMismatchLeavesBinaryAlone(t *testing.T) {
	src := &fakeSource{releases: []Release{rel("v0.4.0", false)}}
	releaseFixture(t, src, "v0.4.0", []byte("new binary"))
	src.assets["v0.4.0/checksums.txt"] = []byte(strings.Repeat("a", 64) + "  vexillum_0.4.0_darwin_arm64.tar.gz\n")
	u, exe := directUpdater(t, "0.3.0", src)

	_, err := u.Upgrade(context.Background(), Stable)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Fatalf("binary changed: %q", got)
	}
}

func TestUpgrade_ArchiveNotInChecksumsIsRefused(t *testing.T) {
	src := &fakeSource{releases: []Release{rel("v0.4.0", false)}}
	releaseFixture(t, src, "v0.4.0", []byte("new binary"))
	src.assets["v0.4.0/checksums.txt"] = []byte(strings.Repeat("a", 64) + "  other.tar.gz\n")
	u, exe := directUpdater(t, "0.3.0", src)

	if _, err := u.Upgrade(context.Background(), Stable); err == nil || !strings.Contains(err.Error(), "not listed") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Fatalf("binary changed: %q", got)
	}
}

func TestUpgrade_MissingChecksumsFileIsRefused(t *testing.T) {
	src := &fakeSource{releases: []Release{rel("v0.4.0", false)}}
	releaseFixture(t, src, "v0.4.0", []byte("new binary"))
	delete(src.assets, "v0.4.0/checksums.txt")
	u, exe := directUpdater(t, "0.3.0", src)

	if _, err := u.Upgrade(context.Background(), Stable); err == nil || !strings.Contains(err.Error(), "unverified") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Fatalf("binary changed: %q", got)
	}
}

func TestUpgrade_PicksArchiveForPlatform(t *testing.T) {
	src := &fakeSource{releases: []Release{rel("v0.4.0", false)}}
	releaseFixture(t, src, "v0.4.0", []byte("new binary"))
	u, _ := directUpdater(t, "0.3.0", src)
	u.OS, u.Arch = "linux", "amd64" // the fixture only has darwin/arm64

	if _, err := u.Upgrade(context.Background(), Stable); err == nil || !strings.Contains(err.Error(), "no release for this platform") {
		t.Fatalf("err = %v", err)
	}
	want := "v0.4.0/vexillum_0.4.0_linux_amd64.tar.gz"
	if len(src.downloads) == 0 || src.downloads[0] != want {
		t.Fatalf("downloads = %v, want %s first", src.downloads, want)
	}
}

func TestUpgrade_UnsupportedPlatform(t *testing.T) {
	src := &fakeSource{releases: []Release{rel("v0.4.0", false)}}
	u, _ := directUpdater(t, "0.3.0", src)
	u.OS = "windows"
	if _, err := u.Upgrade(context.Background(), Stable); err == nil || len(src.downloads) != 0 {
		t.Fatalf("err = %v, downloads = %v", err, src.downloads)
	}
}

func TestUpgrade_NotWritableRefusesBeforeDownloading(t *testing.T) {
	src := &fakeSource{releases: []Release{rel("v0.4.0", false)}}
	releaseFixture(t, src, "v0.4.0", []byte("new binary"))
	u, _ := directUpdater(t, "0.3.0", src)
	u.CheckWritable = func(string) error { return os.ErrPermission }

	_, err := u.Upgrade(context.Background(), Stable)
	if !errors.Is(err, ErrNotWritable) || !strings.Contains(err.Error(), "VX_INSTALL_DIR") {
		t.Fatalf("err = %v", err)
	}
	if len(src.downloads) != 0 {
		t.Fatalf("downloaded %v", src.downloads)
	}
}

func TestProbeWritable(t *testing.T) {
	dir := t.TempDir()
	if err := probeWritable(dir); err != nil {
		t.Fatalf("writable dir: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("probe left %v behind", entries)
	}
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	if err := probeWritable(dir); err == nil {
		t.Fatal("a read-only dir should not be writable")
	}
}

func TestUpgrade_UnknownInstallIsRefused(t *testing.T) {
	src := &fakeSource{releases: []Release{rel("v0.4.0", false)}}
	releaseFixture(t, src, "v0.4.0", []byte("new binary"))

	for name, mutate := range map[string]func(u *Updater){
		"dev build":       func(u *Updater) { u.Current = "dev" },
		"renamed binary":  func(u *Updater) { u.Exe = filepath.Join(filepath.Dir(u.Exe), "vexillum-dev") },
		"go install path": func(u *Updater) { u.GoBinDirs = []string{filepath.Dir(u.Exe)} },
	} {
		t.Run(name, func(t *testing.T) {
			src.downloads = nil
			u, _ := directUpdater(t, "0.3.0", src)
			mutate(u)
			_, err := u.Upgrade(context.Background(), Stable)
			if !errors.Is(err, ErrUnsupportedInstall) {
				t.Fatalf("err = %v", err)
			}
			for _, hint := range []string{InstallCommand, "--scaffold-only"} {
				if !strings.Contains(err.Error(), hint) {
					t.Errorf("error does not say %q: %v", hint, err)
				}
			}
			if len(src.downloads) != 0 {
				t.Fatalf("downloaded %v", src.downloads)
			}
		})
	}
}

// brewFixture lays out a Homebrew prefix with the old version in the Cellar
// and the stable opt link pointing at what brew "installed".
func brewFixture(t *testing.T, src Source, brew Brew, current string) (*Updater, string) {
	t.Helper()
	prefix := t.TempDir()
	oldExe := filepath.Join(prefix, "Cellar", "vexillum", current, "bin", "vx")
	newExe := filepath.Join(prefix, "opt", "vexillum", "bin", "vx")
	for _, p := range []string{oldExe, newExe} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("binary"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &Updater{Source: src, Brew: brew, Current: current, Exe: oldExe}, newExe
}

func TestUpgrade_BrewRunsBrewUpgradeAndPointsAtNewBinary(t *testing.T) {
	src := &fakeSource{releases: []Release{rel("v0.4.0", false)}}
	brew := &fakeBrew{}
	u, newExe := brewFixture(t, src, brew, "0.3.0")
	u.BinaryVersion = func(_ context.Context, exe string) (string, error) {
		if exe != newExe {
			t.Errorf("asked the version of %s, want %s", exe, newExe)
		}
		return "0.4.0", nil
	}

	res, err := u.Upgrade(context.Background(), Stable)
	if err != nil {
		t.Fatal(err)
	}
	if len(brew.formulas) != 1 || brew.formulas[0] != "vexillum" {
		t.Fatalf("brew calls = %v", brew.formulas)
	}
	if !res.Updated || res.Exe != newExe || res.BrewBehind {
		t.Fatalf("result = %+v", res)
	}
	if len(src.downloads) != 0 {
		t.Fatalf("a brew install must not download anything: %v", src.downloads)
	}
}

func TestUpgrade_BrewNotYetPublishedIsReported(t *testing.T) {
	src := &fakeSource{releases: []Release{rel("v0.4.0", false)}}
	u, _ := brewFixture(t, src, &fakeBrew{}, "0.3.0")
	u.BinaryVersion = func(context.Context, string) (string, error) { return "0.3.0", nil }

	res, err := u.Upgrade(context.Background(), Stable)
	if err != nil || res.Updated || !res.BrewBehind {
		t.Fatalf("result = %+v, %v", res, err)
	}
}

func TestUpgrade_BrewFailureIsReported(t *testing.T) {
	src := &fakeSource{releases: []Release{rel("v0.4.0", false)}}
	u, _ := brewFixture(t, src, &fakeBrew{err: errors.New("exit status 1")}, "0.3.0")
	if _, err := u.Upgrade(context.Background(), Stable); err == nil || !strings.Contains(err.Error(), "brew upgrade vexillum failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestUpgrade_BrewUpToDateDoesNotRunBrew(t *testing.T) {
	src := &fakeSource{releases: []Release{rel("v0.4.0", false)}}
	brew := &fakeBrew{}
	u, _ := brewFixture(t, src, brew, "0.4.0")
	res, err := u.Upgrade(context.Background(), Stable)
	if err != nil || res.Updated || len(brew.formulas) != 0 {
		t.Fatalf("result = %+v, %v, brew = %v", res, err, brew.formulas)
	}
}

func TestUpgrade_BrewRefusesCanary(t *testing.T) {
	src := &fakeSource{releases: []Release{rel("v0.4.0-canary.1", true)}}
	brew := &fakeBrew{}
	u, _ := brewFixture(t, src, brew, "0.3.0")
	_, err := u.Upgrade(context.Background(), Canary)
	if !errors.Is(err, ErrBrewChannel) || len(brew.formulas) != 0 || !strings.Contains(err.Error(), CanaryInstallCommand) {
		t.Fatalf("err = %v, brew = %v", err, brew.formulas)
	}
}

// The sentinel logic that restarts a sentinel when its binary changes keys on
// the file identity; an atomic replace by Upgrade must trip it, so upgrade
// needs no restart code of its own.
func TestUpgrade_ReplacedBinaryMakesARunningSentinelStale(t *testing.T) {
	src := &fakeSource{releases: []Release{rel("v0.4.0", false)}}
	releaseFixture(t, src, "v0.4.0", []byte("a different, longer new binary"))
	u, exe := directUpdater(t, "0.3.0", src)

	fi, err := os.Stat(exe)
	if err != nil {
		t.Fatal(err)
	}
	// t.TempDir is under a symlink on macOS (/var), and Stale compares the
	// resolved path.
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		t.Fatal(err)
	}
	u.Exe = exe
	build := sentinel.Build{Executable: exe, Resolved: exe, Size: fi.Size(), ModTimeUnixNano: fi.ModTime().UnixNano()}
	if build.Stale() != "" {
		t.Fatalf("fresh build is stale: %s", build.Stale())
	}
	if _, err := u.Upgrade(context.Background(), Stable); err != nil {
		t.Fatal(err)
	}
	if build.Stale() == "" {
		t.Fatal("the sentinel would not notice the replaced binary")
	}
}

func TestExtractBinary(t *testing.T) {
	if got, err := extractBinary(tarGz(t, "./vx", []byte("x")), "vx"); err != nil || string(got) != "x" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := extractBinary(tarGz(t, "other", []byte("x")), "vx"); err == nil {
		t.Fatal("expected an error when vx is not in the archive")
	}
	if _, err := extractBinary([]byte("not gzip"), "vx"); err == nil {
		t.Fatal("expected an error for a corrupt archive")
	}
}
