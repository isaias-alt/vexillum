package thirdparty

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	noticesPath  = "../../THIRD-PARTY-NOTICES.md"
	bundleDir    = "../../tools/whiteboard-bundle"
	fontsDir     = "../../internal/forum/assets/whiteboard/fonts"
	goModPath    = "../../go.mod"
	regenerateAs = "cd tools/whiteboard-bundle && npm run notices"
)

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type manifest struct {
	LockfileSha256 string `json:"lockfileSha256"`
	Packages       []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Path    string `json:"path"`
	} `json:"packages"`
}

type lockfile struct {
	Packages map[string]struct {
		Version string `json:"version"`
	} `json:"packages"`
}

func TestNoticesHeaderNamesTheGenerator(t *testing.T) {
	text := string(readFile(t, noticesPath))
	head := text
	if len(head) > 600 {
		head = head[:600]
	}
	for _, want := range []string{
		"AUTO-GENERATED",
		"DO NOT EDIT BY HAND",
		"tools/whiteboard-bundle/generate-notices.js",
		regenerateAs,
	} {
		if !strings.Contains(head, want) {
			t.Errorf("THIRD-PARTY-NOTICES.md header lacks %q (it is generated; run %q)", want, regenerateAs)
		}
	}
}

// The lockfile does not say which packages esbuild inlines (every one is a
// devDependency), so the generator records that in bundled-packages.json from
// the build's metafile. This test anchors that list to the lockfile and the
// notices: a lockfile change without a regeneration, or a package missing
// from the notices, fails here.
func TestNoticesCoverEveryBundledPackage(t *testing.T) {
	notices := string(readFile(t, noticesPath))

	var m manifest
	if err := json.Unmarshal(readFile(t, filepath.Join(bundleDir, "bundled-packages.json")), &m); err != nil {
		t.Fatal(err)
	}
	lockBytes := readFile(t, filepath.Join(bundleDir, "package-lock.json"))
	var lock lockfile
	if err := json.Unmarshal(lockBytes, &lock); err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(lockBytes)
	if got := hex.EncodeToString(sum[:]); got != m.LockfileSha256 {
		t.Errorf("package-lock.json changed since the notices were generated (sha256 %s, generated from %s); run %q", got, m.LockfileSha256, regenerateAs)
	}

	// A pruned manifest must not make this test vacuous.
	listed := map[string]bool{}
	for _, p := range m.Packages {
		listed[p.Name] = true
	}
	for _, headline := range []string{
		"@excalidraw/excalidraw", "@excalidraw/mermaid-to-excalidraw", "mermaid", "react", "react-dom", "dompurify",
	} {
		if !listed[headline] {
			t.Errorf("bundled-packages.json lacks the headline package %s; run %q", headline, regenerateAs)
		}
	}
	if len(m.Packages) < 100 {
		t.Errorf("bundled-packages.json lists only %d packages; run %q", len(m.Packages), regenerateAs)
	}

	for _, p := range m.Packages {
		entry, ok := lock.Packages[p.Path]
		if !ok {
			t.Errorf("%s (%s@%s) is not in package-lock.json; run %q", p.Path, p.Name, p.Version, regenerateAs)
			continue
		}
		if entry.Version != p.Version {
			t.Errorf("%s: lockfile has %s, notices were generated for %s; run %q", p.Path, entry.Version, p.Version, regenerateAs)
		}
		if !strings.Contains(notices, "| `"+p.Name+"` | "+p.Version+" |") &&
			!strings.Contains(notices, "`"+p.Name+"` "+p.Version) {
			t.Errorf("THIRD-PARTY-NOTICES.md has no entry for %s %s; run %q", p.Name, p.Version, regenerateAs)
		}
	}
}

func TestNoticesCoverEveryVendoredFontFamily(t *testing.T) {
	notices := strings.ToLower(strings.ReplaceAll(string(readFile(t, noticesPath)), " ", ""))
	entries, err := os.ReadDir(fontsDir)
	if err != nil {
		t.Fatal(err)
	}
	families := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		families++
		if !strings.Contains(notices, "|"+strings.ToLower(e.Name())) {
			t.Errorf("font family %s has no row in the notices font table; run %q", e.Name(), regenerateAs)
		}
	}
	if families == 0 {
		t.Fatal("no font families found under " + fontsDir)
	}
}

var requireLine = regexp.MustCompile(`(?m)^\s*(?:require\s+)?([a-z0-9.\-]+\.[a-z]+/\S+)\s+v\S+`)

func TestNoticesCoverEveryGoModule(t *testing.T) {
	notices := string(readFile(t, noticesPath))
	mod := string(readFile(t, goModPath))
	found := 0
	for _, m := range requireLine.FindAllStringSubmatch(mod, -1) {
		if m[1] == "github.com/isaias-alt/vexillum" {
			continue
		}
		found++
		if !strings.Contains(notices, "`"+m[1]+"`") {
			t.Errorf("go.mod requires %s but THIRD-PARTY-NOTICES.md does not mention it; run %q", m[1], regenerateAs)
		}
	}
	if found == 0 {
		t.Fatal("found no require lines in go.mod")
	}
}
