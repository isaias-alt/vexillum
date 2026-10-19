package piask_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/piask"
)

// The embedded extension is TypeScript that Pi runs, and nobody here can run
// Pi against a model. This runs the very same source under node (which strips
// the types itself) against a stand-in for Pi's extension API, to check the
// protocol end to end: the question file, the blocked events, an answer
// arriving while the dialog is up, one for another id being ignored, the pane
// answering first, and cleanup. It needs node 22.18 or newer and is skipped
// without it; nothing at install or run time needs node but Pi itself.
func TestExtensionUnderNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	out, err := exec.Command(node, "--version").Output()
	if err != nil {
		t.Skipf("node --version: %v", err)
	}
	if !nodeStripsTypes(strings.TrimSpace(string(out))) {
		t.Skipf("node %s does not strip types by default", strings.TrimSpace(string(out)))
	}

	dir := t.TempDir()
	// "typebox" is provided by Pi at runtime; a stand-in satisfies the import.
	stub := filepath.Join(dir, "node_modules", "typebox")
	if err := os.MkdirAll(stub, 0o755); err != nil {
		t.Fatal(err)
	}
	must := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must(filepath.Join(stub, "package.json"), `{"name":"typebox","type":"module","exports":"./index.js"}`)
	must(filepath.Join(stub, "index.js"), `export const Type = { Object: (p) => ({ type: "object", properties: p }), String: (o) => ({ type: "string", ...o }), Array: (i, o) => ({ type: "array", items: i, ...o }), Optional: (x) => x };`)
	ext := filepath.Join(dir, "ask_user.ts")
	must(ext, string(piask.Source()))
	must(filepath.Join(dir, "package.json"), `{"type":"module"}`)
	driver, err := os.ReadFile(filepath.Join("testdata", "driver.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	must(filepath.Join(dir, "driver.mjs"), string(driver))

	cmd := exec.Command(node, filepath.Join(dir, "driver.mjs"), ext, filepath.Join(dir, "asks"))
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(b), "ok") {
		t.Fatalf("the extension failed under node: %v\n%s", err, b)
	}
}

// nodeStripsTypes reports whether a "vMAJOR.MINOR.PATCH" node strips
// TypeScript types without a flag: 22.18 and newer, 24.3 and newer, and 25+.
func nodeStripsTypes(version string) bool {
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(parts) < 2 {
		return false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	switch {
	case major >= 25:
		return true
	case major == 24:
		return minor >= 3
	case major == 22:
		return minor >= 18
	}
	return false
}
