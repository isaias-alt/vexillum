package scripts

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Docs describe how vexillum works, not where the project is in its release
// history. A sentence that is true today and false after the next release
// ("no rc has been published yet", "the first stable is v0.1.0") goes stale
// without anyone touching the file. Point to the live source instead (the
// GitHub releases page) or say it without a date.
//
// timeBound is the small list of known-bad phrasings; add to it only when a
// stale sentence of a new shape shows up. Each entry has an English and a
// Spanish form where the docs are translated.
var timeBound = []struct {
	name string
	re   *regexp.Regexp
}{
	{"nothing published yet", regexp.MustCompile(`(?i)has been published yet|todavía no se publicó|aún no se publicó`)},
	{"the first stable is a version", regexp.MustCompile("(?i)the first stable is `?v\\d|el primer stable es `?v\\d")},
	{"the latest stable is a version", regexp.MustCompile("(?i)(latest|current) stable( release)? (is|es) `?v\\d|(último|actual) stable es `?v\\d")},
	{"a count of built-in profiles", regexp.MustCompile(`(?i)\ball \d+ (built-in )?(model )?profiles|los \d+ perfiles`)},
}

// versionLiteral matches a concrete release-looking version. Placeholders such
// as vX.Y.Z do not match.
var versionLiteral = regexp.MustCompile(`\bv\d+\.\d+\.\d+(?:-[0-9A-Za-z.]+)?`)

// allowedVersions lists the version literals the docs may keep, by file suffix
// and literal, each with the reason it cannot go stale. They are versions of
// other tools that a recorded live check was run against, not vexillum
// releases.
var allowedVersions = []struct {
	file, literal, why string
}{
	{"opencode-commander.mdx", "v2.0.32", "the OpenCode release the API routes were exercised on"},
	{"opencode-commander-checklist.md", "v2.0.32", "the OpenCode release the live check ran on"},
	{"opencode-soldier-checklist.md", "v1.18.29", "the OpenCode 1.x source the flags were read from"},
	{"opencode-soldier-checklist.md", "v1.19.3", "the OpenCode 1.x source the flags were read from"},
}

// timeBoundFiles lists the hand-written docs: the top-level markdown files, the
// docs/ tree and the site docs in both languages. Generated pages are left
// out; their Go source is the place to fix them.
func timeBoundFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	for _, name := range []string{"README.md", "CONTRIBUTING.md", "SECURITY.md", filepath.Join("bench", "README.md")} {
		files = append(files, filepath.Join(root, name))
	}
	for _, dir := range []string{"docs", filepath.Join("site", "content", "docs")} {
		base := filepath.Join(root, dir)
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !(strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".mdx")) {
				return nil
			}
			slash := filepath.ToSlash(path)
			for _, generated := range []string{"/reference/cli/", "/reference/skills/", "/reference/slot-block.mdx"} {
				if strings.Contains(slash, generated) {
					return nil
				}
			}
			files = append(files, path)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", base, err)
		}
	}
	return files
}

func timeBoundHit(line string) (string, bool) {
	for _, p := range timeBound {
		if p.re.MatchString(line) {
			return p.name, true
		}
	}
	return "", false
}

func versionAllowed(file, literal string) bool {
	for _, a := range allowedVersions {
		if strings.HasSuffix(filepath.ToSlash(file), a.file) && a.literal == literal {
			return true
		}
	}
	return false
}

func TestDocsHaveNoTimeBoundStatements(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range timeBoundFiles(t, root) {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(root, file)
		for i, line := range strings.Split(string(data), "\n") {
			if name, ok := timeBoundHit(line); ok {
				t.Errorf("%s:%d: %s (say it without a date, or link the GitHub releases page): %s", rel, i+1, name, strings.TrimSpace(line))
			}
			for _, lit := range versionLiteral.FindAllString(line, -1) {
				if !versionAllowed(rel, lit) {
					t.Errorf("%s:%d: version literal %s (use vX.Y.Z, or allow-list it in allowedVersions with a reason): %s", rel, i+1, lit, strings.TrimSpace(line))
				}
			}
		}
	}
}

func TestTimeBoundPatternsMatch(t *testing.T) {
	bad := []string{
		"No rc has been published yet; find the available versions on the releases page.",
		"Todavía no se publicó ningún rc.",
		"The first stable is `v0.1.0` and rc builds appear later.",
		"El primer stable es `v0.1.0` y los rc aparecen más adelante.",
		"The latest stable is v0.3.1.",
		"`vx init` writes all 17 profiles to a new project's `models.json`",
		"con los 17 perfiles de modelo",
	}
	for _, line := range bad {
		if _, ok := timeBoundHit(line); !ok {
			t.Errorf("no pattern flags %q", line)
		}
	}
	good := []string{
		"Find the available versions on the GitHub releases page.",
		"Running the script without `--version` installs the latest stable.",
		"`vx init` writes every built-in profile to a new project's `models.json`",
		"Replace `<rc-tag>` with a real rc tag, of the form `vX.Y.Z-rc.N`.",
	}
	for _, line := range good {
		if name, ok := timeBoundHit(line); ok {
			t.Errorf("%s flags %q", name, line)
		}
	}
}

func TestVersionLiteralPattern(t *testing.T) {
	for _, line := range []string{"installs v0.3.1", "tag v0.2.2-canary.20261029.g5bcc193", "(`v2.0.32`)"} {
		if !versionLiteral.MatchString(line) {
			t.Errorf("versionLiteral misses %q", line)
		}
	}
	for _, line := range []string{"a tag like vX.Y.Z", "vX.Y.Z-rc.N", "vX.Y.Z-canary.<date>.g<sha7>", "version 0.3.1 of hc"} {
		if lit := versionLiteral.FindString(line); lit != "" {
			t.Errorf("versionLiteral flags %q in %q", lit, line)
		}
	}
	if !versionAllowed("site/content/docs/en/guides/opencode-commander.mdx", "v2.0.32") {
		t.Error("allow-listed literal rejected")
	}
	if versionAllowed("site/content/docs/en/get-started/release-channels.mdx", "v2.0.32") {
		t.Error("allow-list applies to the wrong file")
	}
}
