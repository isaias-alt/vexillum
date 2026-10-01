package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/cli"
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

const hostileSummary = "a < b { c } * d ` e & f > g | h _ i"

func TestEscapeHelpersNeutralizeSpecials(t *testing.T) {
	cases := []struct {
		name string
		fn   func(string) string
		want string
	}{
		{"markdown", escapeMarkdown, "a \\< b { c } \\* d \\` e \\& f \\> g \\| h \\_ i"},
		{"mdx", escapeMDX, "a \\< b \\{ c \\} \\* d \\` e \\& f \\> g \\| h \\_ i"},
	}
	for _, tc := range cases {
		if got := tc.fn(hostileSummary); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestYAMLStringQuotesFrontmatterValues(t *testing.T) {
	if got, want := yamlString(`say "hi": a\b`), `"say \"hi\": a\\b"`; got != want {
		t.Errorf("yamlString = %q, want %q", got, want)
	}
}

func TestTablesEscapeHostileSummaries(t *testing.T) {
	cmds := []cli.Command{{Name: "x", Summary: hostileSummary, Usage: "Usage: vexillum x"}}

	if got := commandTable(cmds); !strings.Contains(got, escapeMarkdown(hostileSummary)) {
		t.Errorf("README table does not use the Markdown escape:\n%s", got)
	}
	if got := indexPage(cmds, localeEN); !strings.Contains(got, escapeMDX(hostileSummary)) {
		t.Errorf("index page does not use the MDX escape:\n%s", got)
	}
	if got := commandPage(cmds[0]); !strings.Contains(got, "description: "+yamlString(hostileSummary)) {
		t.Errorf("frontmatter does not use the YAML quoting:\n%s", got)
	}
}

// The Spanish tree gets only an index (and the folder meta): it links the
// English command pages under /es/docs, and its stale cleanup never runs.
func TestSpanishIndexLinksUnderEsPrefix(t *testing.T) {
	cmds := []cli.Command{{Name: "x", Summary: "do x", Usage: "Usage: vexillum x"}}
	got := indexPage(cmds, localeES)
	if !strings.Contains(got, "(/es/docs/reference/cli/x)") {
		t.Errorf("Spanish index does not link under /es/docs:\n%s", got)
	}
	if strings.Contains(got, "(/docs/") {
		t.Errorf("Spanish index links to the English prefix:\n%s", got)
	}

	files := map[string]string{
		referenceDirES + "/index.mdx": "",
		referenceDirES + "/meta.json": "",
	}
	root := t.TempDir()
	dir := filepath.Join(root, filepath.FromSlash(referenceDirES))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dispatch.mdx"), []byte("traducida a mano"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale, err := staleFiles(root, files)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range stale {
		if strings.Contains(rel, "/es/") {
			t.Errorf("generator claims a hand-translated Spanish page as stale: %s", rel)
		}
	}
}

func TestMetaFileKeepsRegistryOrder(t *testing.T) {
	cmds := []cli.Command{{Name: "b"}, {Name: "a"}}
	want := "{\n  \"title\": \"CLI\",\n  \"pages\": [\"index\", \"b\", \"a\"]\n}\n"
	if got := metaFile(cmds); got != want {
		t.Errorf("metaFile = %q, want %q", got, want)
	}
}
