package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const repoRoot = "../.."

// TestGeneratedDocsAreUpToDate is the docs gate: it fails when the
// committed command reference (or the README table) differs from what the
// generator produces from the registry in internal/cli.
func TestGeneratedDocsAreUpToDate(t *testing.T) {
	files, err := generate(repoRoot)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	const fix = "run `go run ./tools/docgen` from the repository root and commit the result"
	for rel, want := range files {
		got, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("%s is not committed (%v): %s", rel, err, fix)
			continue
		}
		if string(got) != want {
			t.Errorf("%s is out of date: %s", rel, fix)
		}
	}

	stale, err := staleFiles(repoRoot, files)
	if err != nil {
		t.Fatalf("staleFiles: %v", err)
	}
	for _, rel := range stale {
		t.Errorf("%s is no longer generated: %s", rel, fix)
	}
}

func TestWriteIsIdempotentAndRemovesStalePages(t *testing.T) {
	root := t.TempDir()
	readme := "# x\n\n" + readmeStart + "\nold\n" + readmeEnd + "\n"
	if err := os.WriteFile(filepath.Join(root, readmePath), []byte(readme), 0o644); err != nil {
		t.Fatal(err)
	}
	stalePath := filepath.Join(root, filepath.FromSlash(referenceDir), "gone.mdx")
	if err := os.MkdirAll(filepath.Dir(stalePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stalePath, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := write(root); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Errorf("stale page was not removed (stat err: %v)", err)
	}
	first, err := os.ReadFile(filepath.Join(root, readmePath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), "| `vexillum init` |") {
		t.Errorf("README table was not spliced in:\n%s", first)
	}

	if err := write(root); err != nil {
		t.Fatalf("second write: %v", err)
	}
	second, _ := os.ReadFile(filepath.Join(root, readmePath))
	if string(first) != string(second) {
		t.Error("write is not idempotent")
	}
}

func TestWriteFailureLeavesTreeUntouched(t *testing.T) {
	root := t.TempDir()
	// A README without markers makes generation fail.
	if err := os.WriteFile(filepath.Join(root, readmePath), []byte("no markers"), 0o644); err != nil {
		t.Fatal(err)
	}
	stalePath := filepath.Join(root, filepath.FromSlash(referenceDir), "gone.mdx")
	if err := os.MkdirAll(filepath.Dir(stalePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stalePath, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := write(root); err == nil {
		t.Fatal("want an error for a README without markers")
	}
	entries, err := os.ReadDir(filepath.Dir(stalePath))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "gone.mdx" {
		t.Errorf("tree was modified despite the failure: %v", entries)
	}
}

func TestStaleCleanupOnlyTouchesOwnedMDX(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, filepath.FromSlash(referenceDir))
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"notes.md", "gone.mdx"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stale, err := staleFiles(root, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 || stale[0] != referenceDir+"/gone.mdx" {
		t.Errorf("stale = %v, want only gone.mdx", stale)
	}
}

func TestSpliceReadmeRequiresMarkers(t *testing.T) {
	if _, err := spliceReadme("no markers", "t"); err == nil {
		t.Error("want an error when markers are missing")
	}
	if _, err := spliceReadme(readmeEnd+readmeStart, "t"); err == nil {
		t.Error("want an error when markers are misordered")
	}
}

func TestCodeFenceOutgrowsBackticks(t *testing.T) {
	if got := codeFence("plain"); got != "```" {
		t.Errorf("codeFence(plain) = %q", got)
	}
	if got := codeFence("has ``` inside"); got != "````" {
		t.Errorf("codeFence(with fence) = %q", got)
	}
}
