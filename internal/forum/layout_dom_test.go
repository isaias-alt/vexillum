package forum_test

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// The classifiers decide what counts as a layout failure, so they are tested
// as pure functions under node (skipped where node is not installed: it is a
// test-time convenience, never a runtime dependency). The audit itself needs a
// real layout engine and is checked in Chrome against the built binary.
func TestLayoutClassifiers(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/layout_dom_test.js", "assets/chrome/forum-layout.js").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("layout classifiers failed: %v\n%s", err, out)
	}
}

// The audit reports to the chrome with a message the chrome must handle, and
// it reports nothing else: no network, no queueing, no prompt API.
func TestLayoutAudit_OnlyTalksToTheChromeAndNeverToTheAgent(t *testing.T) {
	read := func(name string) string {
		data, err := os.ReadFile("assets/chrome/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	layout, chrome := read("forum-layout.js"), read("forum-chrome.js")

	sent := regexp.MustCompile(`type: "(forum:[a-z-]+)"`).FindAllStringSubmatch(layout, -1)
	if len(sent) != 1 || sent[0][1] != "forum:layout" {
		t.Fatalf("the audit must send exactly one message type, forum:layout; got %v", sent)
	}
	if !strings.Contains(chrome, `case "forum:layout"`) {
		t.Error("the chrome never handles forum:layout")
	}
	for _, banned := range []string{"queuePrompt", "sendQueuedPrompts", "fetch(", "XMLHttpRequest", "__rpc", "forum:rpc", "sendBeacon"} {
		if strings.Contains(layout, banned) {
			t.Errorf("the passive audit must not use %q: it can only report to the chrome", banned)
		}
	}
	// The chrome's only route for it is the diagnostics endpoint - never the queue.
	idx := strings.Index(chrome, "function sendLayoutPass")
	if idx < 0 {
		t.Fatal("sendLayoutPass missing")
	}
	body := chrome[idx : idx+600]
	if !strings.Contains(body, `"/layout/diagnostics"`) || strings.Contains(body, `"/queue"`) || strings.Contains(body, `"/send"`) {
		t.Errorf("a diagnostic pass must go to /layout/diagnostics only:\n%s", body)
	}
}
