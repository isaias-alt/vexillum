package forum_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The embed script is browser JS with no Go counterpart, so it is driven under
// node against a fake DOM, a hand-advanced clock and a scripted artifact bridge
// (testdata/whiteboard_embed_dom_test.js): the start handshake and its token
// rules, autosave, the final-state exchange, fullscreen, unload, the live theme
// and Queue feedback. Skipped where node is not installed: node is a test-time
// convenience, never a runtime dependency.
func TestWhiteboardEmbed_ProtocolAgainstAFakeDOM(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/whiteboard_embed_dom_test.js", "assets/whiteboard-embed.js").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("whiteboard embed dom test failed: %v\n%s", err, out)
	}
}

// The DOM-free logic behind the frame (record, opening decision, edit
// detection and summaries, node fitting, link filter) and the plain-DOM
// pieces of the frame page are ES modules with their own node:test suites next
// to the bundle sources.
func TestWhiteboardCore_NodeSuite(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	files, err := filepath.Glob("../../tools/whiteboard-bundle/test/*.test.js")
	if err != nil || len(files) == 0 {
		t.Fatalf("no whiteboard node test files found: %v", err)
	}
	out, err := exec.Command(node, append([]string{"--test"}, files...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("whiteboard node tests failed: %v\n%s", err, out)
	}
}

// build.js copies the embed script out of the bundle sources; a hand edit of
// either copy that skips the build would otherwise ship unnoticed.
func TestWhiteboardEmbed_EmbeddedCopyMatchesItsSource(t *testing.T) {
	source, err := os.ReadFile("../../tools/whiteboard-bundle/src/whiteboard-embed.js")
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := os.ReadFile("assets/whiteboard-embed.js")
	if err != nil {
		t.Fatal(err)
	}
	if string(source) != string(embedded) {
		t.Error("internal/forum/assets/whiteboard-embed.js differs from tools/whiteboard-bundle/src/whiteboard-embed.js: run the whiteboard bundle build")
	}
}
