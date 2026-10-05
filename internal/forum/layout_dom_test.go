package forum_test

import (
	"os/exec"
	"strings"
	"testing"
)

func runLayoutNode(t *testing.T, script string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/"+script, "assets/chrome/forum-layout.js").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("%s failed: %v\n%s", script, err, out)
	}
}

// The pure geometry of the audit: what each rule reports, what stays silent,
// the threshold boundaries, the two-observation rule and selector determinism.
func TestLayoutAudit_GeometryRulesAndBoundaries(t *testing.T) {
	runLayoutNode(t, "layout_dom_test.js")
}

// When the audit runs and what it sends, against a fake DOM with fake timers.
func TestLayoutAudit_SchedulingAndWire(t *testing.T) {
	runLayoutNode(t, "layout_schedule_test.js")
}
