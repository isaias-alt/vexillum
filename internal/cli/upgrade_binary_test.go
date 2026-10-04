package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/selfupdate"
)

type fakeReleases struct {
	releases []selfupdate.Release
	assets   map[string][]byte
}

func (f *fakeReleases) Releases(context.Context) ([]selfupdate.Release, error) {
	return f.releases, nil
}

func (f *fakeReleases) Asset(_ context.Context, tag, name string) ([]byte, error) {
	data, ok := f.assets[tag+"/"+name]
	if !ok {
		return nil, fmt.Errorf("downloading %s: 404 Not Found", name)
	}
	return data, nil
}

type fakeBrewCLI struct{ calls int }

func (f *fakeBrewCLI) Upgrade(context.Context, string) error { f.calls++; return nil }

// publishVx publishes tag with an archive for the host platform holding
// binary, and checksums that match it unless badSum.
func publishVx(t *testing.T, src *fakeReleases, tag string, binary []byte, badSum bool) {
	t.Helper()
	name := fmt.Sprintf("vexillum_%s_%s_%s.tar.gz", strings.TrimPrefix(tag, "v"), runtime.GOOS, runtime.GOARCH)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "vx", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binary); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	sum := sha256.Sum256(buf.Bytes())
	hexSum := hex.EncodeToString(sum[:])
	if badSum {
		hexSum = strings.Repeat("0", 64)
	}
	src.assets = map[string][]byte{
		tag + "/" + name:       buf.Bytes(),
		tag + "/checksums.txt": []byte(hexSum + "  " + name + "\n"),
	}
}

// binaryHarness runs upgradeBinary with fakes: no network, no brew, no exec.
type binaryHarness struct {
	env      *binaryEnv
	out, err bytes.Buffer
	exe      string
	execs    [][]string
	execErr  error
	ready    bool
	src      *fakeReleases
	brew     *fakeBrewCLI
}

func newBinaryHarness(t *testing.T, current string, releases ...selfupdate.Release) *binaryHarness {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "vx")
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	h := &binaryHarness{exe: exe, ready: true, src: &fakeReleases{releases: releases}, brew: &fakeBrewCLI{}}
	h.env = &binaryEnv{
		updater: func(out io.Writer) (*selfupdate.Updater, error) {
			return &selfupdate.Updater{Source: h.src, Brew: h.brew, Current: current, Exe: h.exe, Out: out}, nil
		},
		exec: func(path string, argv, _ []string) error {
			h.execs = append(h.execs, argv)
			return h.execErr
		},
		projectReady: func() bool { return h.ready },
		stdout:       &h.out,
		stderr:       &h.err,
	}
	return h
}

func stable(tag string) selfupdate.Release { return selfupdate.Release{Tag: tag} }

func TestUpgradeBinary_CheckPrintsAndChangesNothing(t *testing.T) {
	h := newBinaryHarness(t, "0.3.0", stable("v0.4.0"))
	done, code := upgradeBinary(setupOptions{Check: true}, []string{"--check"}, h.env)
	if !done || code != 0 {
		t.Fatalf("done=%v code=%d", done, code)
	}
	if want := "Update available: 0.3.0 -> 0.4.0 (stable channel). Run 'vx upgrade' to install it.\n"; h.out.String() != want {
		t.Fatalf("out = %q, want %q", h.out.String(), want)
	}
	if got, _ := os.ReadFile(h.exe); string(got) != "old binary" || len(h.execs) != 0 {
		t.Fatalf("--check changed things: binary %q, execs %v", got, h.execs)
	}
}

func TestUpgradeBinary_CheckUpToDate(t *testing.T) {
	h := newBinaryHarness(t, "0.4.0", stable("v0.4.0"))
	done, code := upgradeBinary(setupOptions{Check: true}, nil, h.env)
	if !done || code != 0 || !strings.Contains(h.out.String(), "vx 0.4.0 is up to date") {
		t.Fatalf("done=%v code=%d out=%q", done, code, h.out.String())
	}
}

func TestUpgradeBinary_CheckOnCanaryChannel(t *testing.T) {
	h := newBinaryHarness(t, "0.3.0", stable("v0.3.0"), selfupdate.Release{Tag: "v0.4.0-canary.2", Prerelease: true})
	upgradeBinary(setupOptions{Check: true, Channel: selfupdate.Canary}, nil, h.env)
	if want := "Update available: 0.3.0 -> 0.4.0-canary.2 (canary channel). Run 'vx upgrade --channel canary' to install it.\n"; h.out.String() != want {
		t.Fatalf("out = %q, want %q", h.out.String(), want)
	}
}

func TestUpgradeBinary_UpToDateGoesOnToTheScaffold(t *testing.T) {
	h := newBinaryHarness(t, "0.4.0", stable("v0.4.0"))
	done, code := upgradeBinary(setupOptions{}, nil, h.env)
	if done || code != 0 || len(h.execs) != 0 {
		t.Fatalf("done=%v code=%d execs=%v", done, code, h.execs)
	}
	if !strings.Contains(h.out.String(), "up to date") {
		t.Fatalf("out = %q", h.out.String())
	}
}

func TestUpgradeBinary_ReplacesThenExecsTheNewBinary(t *testing.T) {
	h := newBinaryHarness(t, "0.3.0", stable("v0.4.0"))
	publishVx(t, h.src, "v0.4.0", []byte("new binary"), false)

	args := []string{"--yes", "--lang", "es"}
	done, code := upgradeBinary(setupOptions{Yes: true}, args, h.env)
	if !done || code != 0 {
		t.Fatalf("done=%v code=%d err=%q", done, code, h.err.String())
	}
	if got, _ := os.ReadFile(h.exe); string(got) != "new binary" {
		t.Fatalf("binary = %q", got)
	}
	want := []string{h.exe, "upgrade", "--scaffold-only", "--yes", "--lang", "es"}
	if len(h.execs) != 1 || !reflect.DeepEqual(h.execs[0], want) {
		t.Fatalf("execs = %v, want %v", h.execs, want)
	}
}

func TestUpgradeBinary_ChecksumMismatchStopsWithBinaryUntouched(t *testing.T) {
	h := newBinaryHarness(t, "0.3.0", stable("v0.4.0"))
	publishVx(t, h.src, "v0.4.0", []byte("new binary"), true)

	done, code := upgradeBinary(setupOptions{}, nil, h.env)
	if !done || code != 1 || !strings.Contains(h.err.String(), "checksum mismatch") {
		t.Fatalf("done=%v code=%d err=%q", done, code, h.err.String())
	}
	if got, _ := os.ReadFile(h.exe); string(got) != "old binary" || len(h.execs) != 0 {
		t.Fatalf("binary %q, execs %v", got, h.execs)
	}
}

func TestUpgradeBinary_ExecFailureSaysWhatToDo(t *testing.T) {
	h := newBinaryHarness(t, "0.3.0", stable("v0.4.0"))
	publishVx(t, h.src, "v0.4.0", []byte("new binary"), false)
	h.execErr = errors.New("exec format error")

	done, code := upgradeBinary(setupOptions{}, nil, h.env)
	if !done || code != 1 {
		t.Fatalf("done=%v code=%d", done, code)
	}
	if msg := h.err.String(); !strings.Contains(msg, "run 'vx upgrade' again") || !strings.Contains(msg, "exec format error") {
		t.Fatalf("err = %q", msg)
	}
}

func TestUpgradeBinary_NoProjectOnlyUpdatesTheBinary(t *testing.T) {
	h := newBinaryHarness(t, "0.3.0", stable("v0.4.0"))
	publishVx(t, h.src, "v0.4.0", []byte("new binary"), false)
	h.ready = false

	done, code := upgradeBinary(setupOptions{}, nil, h.env)
	if !done || code != 0 || len(h.execs) != 0 {
		t.Fatalf("done=%v code=%d execs=%v", done, code, h.execs)
	}
	if got, _ := os.ReadFile(h.exe); string(got) != "new binary" {
		t.Fatalf("binary = %q", got)
	}
	if !strings.Contains(h.out.String(), "vx init") {
		t.Fatalf("out = %q", h.out.String())
	}
}

func TestUpgradeBinary_UnknownInstallIsRefusedWithAWayOut(t *testing.T) {
	h := newBinaryHarness(t, "dev", stable("v0.4.0"))
	done, code := upgradeBinary(setupOptions{}, nil, h.env)
	if !done || code != 1 {
		t.Fatalf("done=%v code=%d", done, code)
	}
	for _, want := range []string{selfupdate.InstallCommand, "vx upgrade --scaffold-only"} {
		if !strings.Contains(h.err.String(), want) {
			t.Errorf("err does not mention %q: %q", want, h.err.String())
		}
	}
}

func TestUpgradeBinary_BrewInstallRunsBrewAndCanaryIsRefused(t *testing.T) {
	h := newBinaryHarness(t, "0.3.0", stable("v0.3.0"), selfupdate.Release{Tag: "v0.4.0-canary.1", Prerelease: true})
	prefix := t.TempDir()
	h.exe = filepath.Join(prefix, "Cellar", "vexillum", "0.3.0", "bin", "vx")

	done, code := upgradeBinary(setupOptions{Channel: selfupdate.Canary}, nil, h.env)
	if !done || code != 1 || h.brew.calls != 0 || !strings.Contains(h.err.String(), "stable") {
		t.Fatalf("done=%v code=%d brew=%d err=%q", done, code, h.brew.calls, h.err.String())
	}
}

func TestParseSetupArgs_UpgradeBinaryFlags(t *testing.T) {
	opts, _, err := parseSetupArgs(setupUpgrade, []string{"--channel", "canary", "--check"})
	if err != nil || opts.Channel != selfupdate.Canary || !opts.Check {
		t.Fatalf("opts = %+v, %v", opts, err)
	}
	opts, _, err = parseSetupArgs(setupUpgrade, []string{"--channel=stable", "--scaffold-only"})
	if err != nil || opts.Channel != selfupdate.Stable || !opts.ScaffoldOnly {
		t.Fatalf("opts = %+v, %v", opts, err)
	}
	for _, bad := range [][]string{
		{"--channel"},
		{"--channel", "nightly"},
		{"--check", "--scaffold-only"},
	} {
		if _, _, err := parseSetupArgs(setupUpgrade, bad); err == nil {
			t.Errorf("%v should be rejected", bad)
		}
	}
	// init has no binary to update.
	for _, flag := range []string{"--channel=canary", "--check", "--scaffold-only"} {
		if _, _, err := parseSetupArgs(setupInit, []string{flag}); err == nil {
			t.Errorf("init accepted %s", flag)
		}
	}
}
