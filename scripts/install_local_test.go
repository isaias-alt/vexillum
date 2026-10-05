package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeMain stands in for cmd/vx: it prints the VCS revision stamped by the Go
// toolchain, which is what install-local.sh checks against HEAD.
const fakeMain = `package main

import (
	"fmt"
	"runtime/debug"
)

func main() {
	rev := "unknown"
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				rev = s.Value[:7]
			}
		}
	}
	fmt.Printf("vx dev (%s)\n", rev)
}
`

// installRepo is a temp checkout on branch canary with a buildable cmd/vx.
func installRepo(t *testing.T) *gitRepo {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not available")
	}
	if _, err := os.ReadFile("install-local.sh"); err != nil {
		t.Fatal(err)
	}
	r := &gitRepo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "canary")
	write := func(rel, content string) {
		p := filepath.Join(r.dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/fake\n\ngo 1.21\n")
	write("cmd/vx/main.go", fakeMain)
	r.git("add", ".")
	r.git("commit", "-q", "-m", "init")
	return r
}

func runInstall(t *testing.T, repo *gitRepo, dest string, extraEnv []string, args ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs("install-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = append(os.Environ(),
		"VX_INSTALL_REPO="+repo.dir,
		"VX_INSTALL_DIR="+dest,
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestInstallLocalFreshAndRollback(t *testing.T) {
	r := installRepo(t)
	dest := filepath.Join(t.TempDir(), "bin")

	out, err := runInstall(t, r, dest, nil)
	if err != nil {
		t.Fatalf("first install: %v\n%s", err, out)
	}
	head := r.git("rev-parse", "--short=7", "HEAD")
	got, err := exec.Command(filepath.Join(dest, "vx"), "--version").Output()
	if err != nil || !strings.Contains(string(got), head) {
		t.Fatalf("installed vx reports %q (err %v), want HEAD %s", got, err, head)
	}
	if _, err := os.Stat(filepath.Join(dest, "vx.prev")); err == nil {
		t.Error("vx.prev must not exist after the first install")
	}

	// A second install keeps the first binary as vx.prev.
	first, err := os.ReadFile(filepath.Join(dest, "vx"))
	if err != nil {
		t.Fatal(err)
	}
	r.commit("second")
	if out, err := runInstall(t, r, dest, nil); err != nil {
		t.Fatalf("second install: %v\n%s", err, out)
	}
	prev, err := os.ReadFile(filepath.Join(dest, "vx.prev"))
	if err != nil {
		t.Fatalf("vx.prev missing: %v", err)
	}
	if string(prev) != string(first) {
		t.Error("vx.prev is not the previously installed binary")
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("destination holds %d entries, want vx and vx.prev only", len(entries))
	}
}

func TestInstallLocalGuards(t *testing.T) {
	r := installRepo(t)
	dest := filepath.Join(t.TempDir(), "bin")

	// Dirty checkout (an untracked file counts, as it does for Go's VCS stamp).
	if err := os.WriteFile(filepath.Join(r.dir, "scratch.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := runInstall(t, r, dest, nil); err == nil || !strings.Contains(out, "not clean") {
		t.Fatalf("dirty checkout: err %v, output %q", err, out)
	}
	if _, err := os.Stat(filepath.Join(dest, "vx")); err == nil {
		t.Error("a refused install must not write the destination")
	}
	if out, err := runInstall(t, r, dest, nil, "--force"); err != nil {
		t.Fatalf("--force on a dirty checkout: %v\n%s", err, out)
	}

	// Wrong branch.
	if err := os.Remove(filepath.Join(r.dir, "scratch.txt")); err != nil {
		t.Fatal(err)
	}
	r.git("checkout", "-q", "-b", "feature")
	if out, err := runInstall(t, r, dest, nil); err == nil || !strings.Contains(out, "not 'canary'") {
		t.Fatalf("wrong branch: err %v, output %q", err, out)
	}
	if out, err := runInstall(t, r, dest, []string{"VX_INSTALL_FORCE=1"}); err != nil {
		t.Fatalf("VX_INSTALL_FORCE on a feature branch: %v\n%s", err, out)
	}
	if out, err := runInstall(t, r, dest, []string{"VX_INSTALL_BASE=feature"}); err != nil {
		t.Fatalf("VX_INSTALL_BASE=feature: %v\n%s", err, out)
	}
}
