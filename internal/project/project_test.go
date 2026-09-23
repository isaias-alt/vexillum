package project_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isaias-alt/vexillum/internal/project"
)

// C1-01: Key is a pure function of the absolute project path - same
// input, same output, every time.
func TestKey_StableForSamePath(t *testing.T) {
	dir := t.TempDir()
	if project.Key(dir) != project.Key(dir) {
		t.Fatalf("expected Key to be stable for the same path")
	}
}

// C1-02: two different project directories - even with the same base
// name - get different keys, since Key hashes the full absolute path,
// not just the base name.
func TestKey_DiffersForDifferentPaths(t *testing.T) {
	a := filepath.Join(t.TempDir(), "myproject")
	b := filepath.Join(t.TempDir(), "myproject")
	if project.Key(a) == project.Key(b) {
		t.Fatalf("expected different keys for different absolute paths sharing a base name, got %q for both", project.Key(a))
	}
}

// C1-03: Root lays out a project under vexillumHome/projects/<key>.
func TestRoot_NamespacesUnderProjectsDir(t *testing.T) {
	home := t.TempDir()
	projectDir := t.TempDir()

	root, err := project.Root(home, projectDir)
	if err != nil {
		t.Fatalf("Root: %v", err)
	}

	resolvedProject, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		resolvedProject = projectDir
	}
	want := filepath.Join(home, "projects", project.Key(resolvedProject))
	if root != want {
		t.Errorf("Root(%q, %q) = %q, want %q", home, projectDir, root, want)
	}
}

// C1-04: Root normalizes symlinks before hashing, so two paths to the
// same directory - one direct, one through a symlink - resolve to the
// identical project root. This is what keeps "vexillum dispatch"
// (resolved from a plain cwd) and "vexillum sentinel drain" (resolved
// from "git rev-parse --show-toplevel", which chases symlinks) agreeing
// on the same project.
func TestRoot_NormalizesSymlinks(t *testing.T) {
	home := t.TempDir()
	real := t.TempDir()

	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "link-to-project")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks not supported here: %v", err)
	}

	rootViaReal, err := project.Root(home, real)
	if err != nil {
		t.Fatalf("Root(real): %v", err)
	}
	rootViaLink, err := project.Root(home, link)
	if err != nil {
		t.Fatalf("Root(link): %v", err)
	}
	if rootViaReal != rootViaLink {
		t.Errorf("expected the real path and a symlink to it to resolve to the same root, got %q and %q", rootViaReal, rootViaLink)
	}
}

// C1-05: AllRoots is empty, not an error, when nothing has ever been
// namespaced under vexillumHome.
func TestAllRoots_EmptyWhenNoProjectsDir(t *testing.T) {
	home := t.TempDir()
	roots, err := project.AllRoots(home)
	if err != nil {
		t.Fatalf("AllRoots: %v", err)
	}
	if len(roots) != 0 {
		t.Errorf("expected no roots, got %v", roots)
	}
}

// C1-06: AllRoots lists every project namespaced under vexillumHome -
// what the sentinel sweeps in one Tick.
func TestAllRoots_ListsEveryProject(t *testing.T) {
	home := t.TempDir()
	projectA := t.TempDir()
	projectB := t.TempDir()

	rootA, err := project.Root(home, projectA)
	if err != nil {
		t.Fatalf("Root(a): %v", err)
	}
	rootB, err := project.Root(home, projectB)
	if err != nil {
		t.Fatalf("Root(b): %v", err)
	}
	for _, r := range []string{rootA, rootB} {
		if err := os.MkdirAll(r, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", r, err)
		}
	}

	roots, err := project.AllRoots(home)
	if err != nil {
		t.Fatalf("AllRoots: %v", err)
	}
	if len(roots) != 2 {
		t.Fatalf("expected 2 roots, got %v", roots)
	}
	got := map[string]bool{roots[0]: true, roots[1]: true}
	if !got[rootA] || !got[rootB] {
		t.Errorf("expected AllRoots to include both %q and %q, got %v", rootA, rootB, roots)
	}
}
