package scripts

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The docs never pin the stable channel to a version: the install script
// installs the latest stable release on its own, so a pin goes stale at the
// next release. Version examples use the placeholders vX.Y.Z and
// vX.Y.Z-rc.N, and the script URL points at the default branch.
var docPins = []struct {
	name string
	re   *regexp.Regexp
}{
	{"install script URL pinned to a release tag", regexp.MustCompile(`vexillum/v\d+\.\d+\.\d+[^/\s]*/scripts/install\.sh`)},
	{"--version with a literal version", regexp.MustCompile(`--version\s+v?\d+\.\d+\.\d+`)},
	{"stable channel row with a literal version", regexp.MustCompile("(?i)^\\|\\s*stable\\s*\\|\\s*`v\\d+\\.\\d+\\.\\d+`")},
	{"INSTALL_SCRIPT_REF set to a release tag", regexp.MustCompile(`INSTALL_SCRIPT_REF\s*=\s*"v?\d+\.\d+\.\d+`)},
	{"install help example with a literal version", regexp.MustCompile(`(?i)(for example|such as)\s+v\d+\.\d+\.\d+`)},
}

// docPinFiles lists the files that show install commands or the channel
// table: the README, the install script, and the site's docs and install ref.
func docPinFiles(t *testing.T, root string) []string {
	t.Helper()
	files := []string{
		filepath.Join(root, "README.md"),
		filepath.Join(root, "scripts", "install.sh"),
		filepath.Join(root, "site", "lib", "install-script.ts"),
	}
	docs := filepath.Join(root, "site", "content", "docs")
	err := filepath.WalkDir(docs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".mdx") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", docs, err)
	}
	return files
}

func TestDocsDoNotPinAStableVersion(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range docPinFiles(t, root) {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(root, file)
		for i, line := range strings.Split(string(data), "\n") {
			for _, pin := range docPins {
				if pin.re.MatchString(line) {
					t.Errorf("%s:%d: %s (use the canary ref or a vX.Y.Z placeholder): %s", rel, i+1, pin.name, strings.TrimSpace(line))
				}
			}
		}
	}
}

func TestDocPinPatternsMatch(t *testing.T) {
	bad := []string{
		"curl -fsSL https://raw.githubusercontent.com/isaias-alt/vexillum/v0.3.0/scripts/install.sh | bash",
		"curl ... | bash -s -- --version v0.1.1",
		"| stable | `v0.3.0` | The official release |",
		`export const INSTALL_SCRIPT_REF = "v0.3.0";`,
		"(for example v0.1.0 or",
	}
	for _, line := range bad {
		hit := false
		for _, pin := range docPins {
			hit = hit || pin.re.MatchString(line)
		}
		if !hit {
			t.Errorf("no pattern flags %q", line)
		}
	}
	good := []string{
		"curl -fsSL https://raw.githubusercontent.com/isaias-alt/vexillum/canary/scripts/install.sh | bash -s -- --version <tag>",
		"| stable | `vX.Y.Z` | The official release |",
		`export const INSTALL_SCRIPT_REF = "canary";`,
	}
	for _, line := range good {
		for _, pin := range docPins {
			if pin.re.MatchString(line) {
				t.Errorf("%s flags %q", pin.name, line)
			}
		}
	}
}
