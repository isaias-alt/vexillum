package doctorcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/scaffold"
)

func TestBinary_Found(t *testing.T) {
	// "go" is guaranteed to be on PATH in this test environment.
	result := Binary("Go toolchain", "go", true)
	if !result.OK {
		t.Errorf("expected OK=true for a binary on PATH, got %+v", result)
	}
}

func TestBinary_MissingRequired(t *testing.T) {
	result := Binary("Nonexistent", "vexillum-does-not-exist-binary", true)
	if result.OK {
		t.Error("expected OK=false for a missing binary")
	}
	if !result.Required {
		t.Error("expected Required=true to be preserved")
	}
	if result.Detail == "" {
		t.Error("expected a detail message explaining the miss")
	}
}

func TestBinary_MissingOptional(t *testing.T) {
	result := Binary("Nonexistent", "vexillum-does-not-exist-binary", false)
	if result.OK {
		t.Error("expected OK=false for a missing binary")
	}
	if result.Required {
		t.Error("expected Required=false to be preserved")
	}
}

func TestVexillumHome(t *testing.T) {
	dir := t.TempDir()
	result := VexillumHome(dir)
	if !result.OK {
		t.Errorf("expected an existing writable directory to be OK, got %+v", result)
	}

	missing := VexillumHome(filepath.Join(dir, "does-not-exist"))
	if missing.OK {
		t.Error("expected a missing path to not be OK")
	}
}

func TestProjectInitialized_DelegatesToScaffold(t *testing.T) {
	dir := t.TempDir()

	before := ProjectInitialized(dir)
	if before.OK {
		t.Fatal("expected OK=false before the project is scaffolded")
	}

	if err := scaffold.WriteConfig(filepath.Join(dir, ".vexillum")); err != nil {
		t.Fatalf("scaffold.WriteConfig: %v", err)
	}

	after := ProjectInitialized(dir)
	if !after.OK {
		t.Error("expected OK=true once .vexillum/config.json exists")
	}
}

func TestGitRepo_DelegatesToScaffold(t *testing.T) {
	result := GitRepo(t.TempDir())
	if result.OK {
		t.Error("expected OK=false for a directory that isn't a git repository")
	}
}

// fakeVX writes an executable named like the command into a fresh
// directory and returns the file's path.
func fakeVX(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), cmdname.Name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("writing fake %s: %v", cmdname.Name, err)
	}
	return path
}

func TestVXShadow_OurselvesFirstIsOK(t *testing.T) {
	self := fakeVX(t)
	other := fakeVX(t)
	pathEnv := filepath.Dir(self) + string(os.PathListSeparator) + filepath.Dir(other)

	result := vxShadow(self, pathEnv)
	if !result.OK || result.Warn {
		t.Errorf("expected ok when this executable is the first on PATH, got %+v", result)
	}
}

func TestVXShadow_OtherEarlierWarnsAndNamesIt(t *testing.T) {
	self := fakeVX(t)
	other := fakeVX(t)
	pathEnv := filepath.Dir(other) + string(os.PathListSeparator) + filepath.Dir(self)

	result := vxShadow(self, pathEnv)
	if !result.Warn || result.OK {
		t.Fatalf("expected a warning when another %s is earlier in PATH, got %+v", cmdname.Name, result)
	}
	// Both sides are reported, by real path (t.TempDir may sit behind a symlink).
	if !strings.Contains(result.Detail, realPath(other)) && !strings.Contains(result.Detail, other) {
		t.Errorf("expected the conflicting path %s in the detail, got %q", other, result.Detail)
	}
	if !strings.Contains(result.Detail, realPath(self)) {
		t.Errorf("expected this executable %s in the detail, got %q", realPath(self), result.Detail)
	}
}

func TestVXShadow_OtherOnlyAfterUsIsOK(t *testing.T) {
	self := fakeVX(t)
	other := fakeVX(t)
	pathEnv := filepath.Dir(self) + string(os.PathListSeparator) + filepath.Dir(other)
	if result := vxShadow(self, pathEnv); result.Warn {
		t.Errorf("a vx later in PATH must not warn, got %+v", result)
	}
}

func TestVXShadow_SymlinkToUsIsNotAConflict(t *testing.T) {
	self := fakeVX(t)
	linkDir := t.TempDir()
	if err := os.Symlink(self, filepath.Join(linkDir, cmdname.Name)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// Earlier PATH entry is a symlink (like a brew bin link) to this very binary.
	pathEnv := linkDir + string(os.PathListSeparator) + filepath.Dir(self)
	if result := vxShadow(self, pathEnv); !result.OK || result.Warn {
		t.Errorf("expected ok for a symlink that resolves to this executable, got %+v", result)
	}
}

func TestVXShadow_NotOnPathIsOK(t *testing.T) {
	self := fakeVX(t)
	result := vxShadow(self, t.TempDir())
	if !result.OK || result.Warn {
		t.Errorf("expected ok when no vx is on PATH at all, got %+v", result)
	}
}

func TestVXShadow_IgnoresNonExecutableAndRelativeEntries(t *testing.T) {
	self := fakeVX(t)

	// A same-named file without the execute bit is not something a shell
	// would run, so it does not shadow us.
	nonExec := filepath.Join(t.TempDir(), cmdname.Name)
	if err := os.WriteFile(nonExec, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Neither does a name that only exists as a directory.
	dirEntry := t.TempDir()
	if err := os.Mkdir(filepath.Join(dirEntry, cmdname.Name), 0o755); err != nil {
		t.Fatal(err)
	}

	sep := string(os.PathListSeparator)
	pathEnv := strings.Join([]string{"", ".", "relative/bin", filepath.Dir(nonExec), dirEntry, filepath.Dir(self)}, sep)
	if result := vxShadow(self, pathEnv); !result.OK || result.Warn {
		t.Errorf("expected ok, got %+v", result)
	}
}
