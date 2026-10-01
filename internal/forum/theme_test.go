package forum_test

import (
	"os/exec"
	"strings"
	"testing"
)

// The theme logic is browser JS with no Go counterpart, so it is exercised
// under node with a fake window. Skipped where node is not installed: node is
// a test-time convenience only, never a runtime dependency of vexillum.
func TestThemeScript_DefaultsToDarkAndToggles(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/theme_test.js", "assets/chrome/forum-theme.js").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("theme script test failed: %v\n%s", err, out)
	}
}

func TestPrefsScript_AnnotateDefaultsOnAndIsRemembered(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/prefs_test.js", "assets/chrome/forum-prefs.js").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("prefs script test failed: %v\n%s", err, out)
	}
}
