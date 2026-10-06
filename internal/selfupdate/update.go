// Package selfupdate updates the vx binary itself: it finds the newest
// release of a channel, and either hands the job to Homebrew or replaces the
// executable with the release's verified binary. Everything that touches the
// outside world (GitHub, brew, the file system probes) sits behind a small
// interface or function field so the logic runs offline in tests.
package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
	"github.com/isaias-alt/vexillum/internal/cmdname"
)

const (
	// productName is the Homebrew formula and the release archive prefix, as
	// in scripts/install.sh.
	productName = "vexillum"
	// InstallCommand reinstalls vx from scratch. It tracks the canary branch,
	// not a release tag, so the hint stays valid for every release.
	InstallCommand = "curl -fsSL https://raw.githubusercontent.com/isaias-alt/vexillum/canary/scripts/install.sh | bash"
	// CanaryInstallCommand installs the latest canary build.
	CanaryInstallCommand = InstallCommand + " -s -- --channel canary"
	// devVersion is what a build outside the release pipeline reports.
	devVersion = "dev"
)

// Method is how the running vx was installed.
type Method int

const (
	// MethodUnknown is an install vx cannot update by itself.
	MethodUnknown Method = iota
	// MethodBrew is a Homebrew install: brew owns the binary.
	MethodBrew
	// MethodDirect is the install script's download: vx replaces the file.
	MethodDirect
)

// Errors a caller can tell apart; each is wrapped with a message that says
// what to do next.
var (
	ErrUnsupportedInstall = errors.New("cannot update this installation")
	ErrNotWritable        = errors.New("cannot write to the install directory")
	ErrBrewChannel        = errors.New("the Homebrew install only follows stable releases")
)

// Brew runs Homebrew.
type Brew interface {
	// Upgrade runs "brew upgrade <formula>".
	Upgrade(ctx context.Context, formula string) error
}

// Updater holds everything an update needs. Source, Brew and Exe are
// required; the rest have working defaults.
type Updater struct {
	Source Source
	Brew   Brew
	// Current is the running version ("0.4.0", or "dev").
	Current string
	// Exe is the running executable with symlinks resolved.
	Exe string
	// GoBinDirs are the directories "go install" writes to: a binary there is
	// a source build, not a release.
	GoBinDirs []string
	// OS and Arch default to the running platform; they pick the archive.
	OS, Arch string
	// CheckWritable fails when dir cannot take a new file. Defaults to
	// creating and removing a probe file.
	CheckWritable func(dir string) error
	// BinaryVersion reports the version of the vx at the given path. Defaults
	// to running it with --version.
	BinaryVersion func(ctx context.Context, exe string) (string, error)
	// Out receives progress lines; nil discards them.
	Out io.Writer
}

// Status compares the running version with the newest release of a channel.
type Status struct {
	Channel Channel
	Current string
	Latest  Release
	// Newer is true when Latest is newer than Current, or when the two cannot
	// be compared (a build that is not a release).
	Newer bool
}

// Check looks up the newest release of the channel and compares. It changes
// nothing.
func (u *Updater) Check(ctx context.Context, ch Channel) (Status, error) {
	releases, err := u.Source.Releases(ctx)
	if err != nil {
		return Status{}, fmt.Errorf("could not reach GitHub Releases: %w", err)
	}
	latest, err := Latest(releases, ch)
	if err != nil {
		return Status{}, err
	}
	return Status{Channel: ch, Current: u.Current, Latest: latest, Newer: isNewer(latest, u.Current)}, nil
}

// isNewer is false only when current is a version that is the same as or
// newer than r. Anything it cannot read as a version ("dev") counts as older.
func isNewer(r Release, current string) bool {
	rv, err := parseVersion(r.Tag)
	if err != nil {
		return false
	}
	cv, err := parseVersion(current)
	if err != nil {
		return true
	}
	return compare(rv, cv) > 0
}

// Result is the outcome of Upgrade.
type Result struct {
	Status
	// Updated is true when the binary on disk is now a newer one.
	Updated bool
	// Exe is the path of the new binary, ready to run, when Updated; empty
	// when it could not be located.
	Exe string
	// BrewBehind is set when brew ran but did not install the release: the
	// formula has not caught up with it yet.
	BrewBehind bool
}

// Upgrade updates the binary to the newest release of the channel, unless the
// running one is already that or newer.
func (u *Updater) Upgrade(ctx context.Context, ch Channel) (Result, error) {
	st, err := u.Check(ctx, ch)
	if err != nil {
		return Result{}, err
	}
	res := Result{Status: st}
	if !st.Newer {
		return res, nil
	}

	m := u.method()
	if m != MethodUnknown {
		u.say("Updating %s %s -> %s (%s channel)", cmdname.Name, u.Current, st.Latest.Version(), ch)
	}
	switch m {
	case MethodBrew:
		return u.upgradeBrew(ctx, res)
	case MethodDirect:
		return u.upgradeDirect(ctx, res)
	}
	return Result{}, fmt.Errorf("%w: %s", ErrUnsupportedInstall, u.unsupportedReason())
}

func (u *Updater) say(format string, args ...any) {
	if u.Out != nil {
		fmt.Fprintf(u.Out, format+"\n", args...)
	}
}

// method works out how the running vx was installed from its path.
func (u *Updater) method() Method {
	if u.Current == devVersion {
		return MethodUnknown
	}
	if _, ok := brewPrefix(u.Exe); ok {
		return MethodBrew
	}
	if filepath.Base(u.Exe) != cmdname.Name || u.inGoBin() {
		return MethodUnknown
	}
	return MethodDirect
}

func (u *Updater) inGoBin() bool {
	dir := filepath.Dir(u.Exe)
	for _, d := range u.GoBinDirs {
		if d != "" && filepath.Clean(d) == dir {
			return true
		}
	}
	return false
}

func (u *Updater) unsupportedReason() string {
	var why string
	switch {
	case u.Current == devVersion:
		why = "this is a development build, not a release"
	case u.inGoBin():
		why = u.Exe + " is a binary built from source (go install)"
	default:
		why = fmt.Sprintf("%s is neither a Homebrew nor an install-script install of %s", u.Exe, cmdname.Name)
	}
	return fmt.Sprintf("%s. Reinstall with the install script (%s), or refresh only the project's scaffold with '%s upgrade --scaffold-only'", why, InstallCommand, cmdname.Name)
}

// brewPrefix returns the Homebrew prefix when exe lives in a Cellar (always
// the case for a brew install once symlinks are resolved).
func brewPrefix(exe string) (string, bool) {
	p := filepath.ToSlash(exe)
	i := strings.Index(p, "/Cellar/")
	if i < 0 {
		return "", false
	}
	return p[:i], true
}

func (u *Updater) upgradeBrew(ctx context.Context, res Result) (Result, error) {
	if res.Channel == Canary {
		return Result{}, fmt.Errorf("%w. To follow canary, uninstall the formula (brew uninstall %s) and install the canary build with the install script (%s); '%s upgrade --channel canary' keeps it current from then on", ErrBrewChannel, productName, CanaryInstallCommand, cmdname.Name)
	}
	u.say("%s is installed with Homebrew: running 'brew upgrade %s'", cmdname.Name, productName)
	if err := u.Brew.Upgrade(ctx, productName); err != nil {
		return Result{}, fmt.Errorf("brew upgrade %s failed: %w", productName, err)
	}

	// brew keeps a stable link to the active version; the Cellar path of the
	// running binary is the old version's.
	prefix, _ := brewPrefix(u.Exe)
	newExe := filepath.FromSlash(path.Join(prefix, "opt", productName, "bin", cmdname.Name))
	if _, err := os.Stat(newExe); err != nil {
		res.Updated = true
		return res, nil
	}
	got, err := u.binaryVersion(ctx, newExe)
	if err == nil && got == u.Current {
		res.BrewBehind = true
		return res, nil
	}
	res.Updated = true
	res.Exe = newExe
	return res, nil
}

func (u *Updater) binaryVersion(ctx context.Context, exe string) (string, error) {
	if u.BinaryVersion != nil {
		return u.BinaryVersion(ctx, exe)
	}
	return runVersion(ctx, exe)
}

func (u *Updater) upgradeDirect(ctx context.Context, res Result) (Result, error) {
	goos, goarch := u.platform()
	if (goos != "darwin" && goos != "linux") || (goarch != "amd64" && goarch != "arm64") {
		return Result{}, fmt.Errorf("no release for %s/%s (only macOS and Linux on amd64 and arm64)", goos, goarch)
	}

	dir := filepath.Dir(u.Exe)
	if err := u.checkWritable(dir); err != nil {
		return Result{}, fmt.Errorf("%w %s: %v. Reinstall into a directory you can write to with the install script (%s, with VX_INSTALL_DIR set to it)", ErrNotWritable, dir, err, InstallCommand)
	}
	fi, err := os.Stat(u.Exe)
	if err != nil {
		return Result{}, fmt.Errorf("reading %s: %w", u.Exe, err)
	}

	tag, ver := res.Latest.Tag, res.Latest.Version()
	archive := fmt.Sprintf("%s_%s_%s_%s.tar.gz", productName, ver, goos, goarch)
	u.say("Downloading %s...", archive)
	data, err := u.Source.Asset(ctx, tag, archive)
	if err != nil {
		return Result{}, fmt.Errorf("%w (no release for this platform?)", err)
	}
	sums, err := u.Source.Asset(ctx, tag, "checksums.txt")
	if err != nil {
		return Result{}, fmt.Errorf("%w - refusing to install an unverified binary", err)
	}
	if err := verifyChecksum(sums, archive, data); err != nil {
		return Result{}, err
	}
	u.say("Checksum verified")

	bin, err := extractBinary(data, cmdname.Name)
	if err != nil {
		return Result{}, fmt.Errorf("unpacking %s: %w", archive, err)
	}
	if err := atomicfile.WriteMode(u.Exe, bin, fi.Mode().Perm()); err != nil {
		return Result{}, fmt.Errorf("replacing %s: %w", u.Exe, err)
	}
	u.say("Replaced %s", u.Exe)

	res.Updated = true
	res.Exe = u.Exe
	return res, nil
}
