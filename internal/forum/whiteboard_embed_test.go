package forum_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The embed script is browser JS with no Go counterpart, so it is driven under
// node against a fake DOM and a scripted chrome (testdata/whiteboard_embed_dom_test.js):
// the ready/init handshake, saving, queue feedback, fullscreen teardown and the
// live theme switch. Skipped where node is not installed: node is a test-time
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

// The pure logic behind the frame (edit summaries, start decisions, label
// fixing) is a plain ES module with its own node:test suite next to the bundle
// sources.
func TestWhiteboardCore_NodeSuite(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "--test", "../../tools/whiteboard-bundle/test/whiteboard-core.test.js").CombinedOutput()
	if err != nil {
		t.Fatalf("whiteboard core tests failed: %v\n%s", err, out)
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
