package forum_test

import (
	"os/exec"
	"strings"
	"testing"
)

// The selector and context logic is browser JS with no Go counterpart, so it
// is exercised under node against a fake DOM. Skipped where node is not
// installed: node is a test-time convenience, never a runtime dependency.
func TestSDK_SelectorAndContextHelpers(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/sdk_dom_test.js", "assets/chrome/forum-sdk.js").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("sdk dom test failed: %v\n%s", err, out)
	}
}
