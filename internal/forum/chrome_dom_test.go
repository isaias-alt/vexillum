package forum_test

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// runChromeNodeTest runs one of testdata/chrome_*_test.js against the real
// forum-chrome.js under node (skipped where node is not installed: it is a
// test-time convenience, never a runtime dependency).
func runChromeNodeTest(t *testing.T, script string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/"+script, "assets/chrome/forum-chrome.js").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("%s failed: %v\n%s", script, err, out)
	}
}

// The session-ended dialog appears from any snapshot that says ended, cannot
// be dismissed, and goes away when the session is reopened.
func TestChromeEndedDialog_StatesAndNoWayOut(t *testing.T) {
	runChromeNodeTest(t, "chrome_ended_test.js")
}

// Markup contract of the dialog: an accessible, modal, initially hidden
// dialog with exactly one control (copy), and nothing that dismisses it.
func TestChromeEndedDialog_MarkupIsModalAndHasNoCloseControl(t *testing.T) {
	data, err := os.ReadFile("assets/chrome/chrome.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	start := strings.Index(html, `id="endedBackdrop"`)
	if start < 0 {
		t.Fatal("chrome.html has no ended dialog")
	}
	end := strings.Index(html[start:], `<script type="application/json"`)
	block := html[start : start+end]

	if !strings.Contains(html[start-30:start+30], "hidden") {
		t.Error("the dialog must start hidden")
	}
	for _, want := range []string{`role="dialog"`, `aria-modal="true"`, `aria-labelledby="endedTitle"`, `aria-describedby=`, `tabindex="-1"`} {
		if !strings.Contains(block, want) {
			t.Errorf("dialog markup missing %s", want)
		}
	}
	buttons := regexp.MustCompile(`<button\b[^>]*>`).FindAllString(block, -1)
	if len(buttons) != 1 || !strings.Contains(buttons[0], `id="endedCopy"`) {
		t.Errorf("the only control in the dialog must be the copy button, got %v", buttons)
	}
	lower := strings.ToLower(block)
	for _, banned := range []string{"×", "&times;", "aria-label=\"close", "data-dismiss", "dismiss", "autofocus"} {
		if strings.Contains(lower, banned) {
			t.Errorf("dialog must not offer a way out: found %q", banned)
		}
	}
	// And the script never closes it other than through the state.
	js, _ := os.ReadFile("assets/chrome/forum-chrome.js")
	if strings.Contains(string(js), "endedBackdrop.hidden = true") && strings.Count(string(js), "endedBackdrop.hidden = true") > 1 {
		t.Error("the dialog must be hidden from exactly one place (a reopened session)")
	}
}

// The live feed reconnects by itself and two tabs of one session converge.
func TestChromeLiveFeed_ReconnectsAndTabsConverge(t *testing.T) {
	runChromeNodeTest(t, "chrome_live_test.js")
}

// The Layout issues tray: it renders what the server says, queues only what
// the user selected and never sends, and the passes the artifact reports are
// bounded and tagged with the artifact version before they go out.
func TestChromeLayoutTray_RendersQueuesAndReports(t *testing.T) {
	runChromeNodeTest(t, "chrome_layout_test.js")
}
