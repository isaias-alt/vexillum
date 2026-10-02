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

// Every composer action looks equally disabled: the primary fill and the
// danger text must not survive the disabled state, or a finished session shows
// some buttons less greyed out than others.
func TestChromeCSS_DisabledButtonsShareOneLook(t *testing.T) {
	data, err := os.ReadFile("assets/chrome/forum.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(data)
	idx := strings.Index(css, ".btn:disabled, .btn-primary:disabled, .btn-danger:disabled {")
	if idx < 0 {
		t.Fatal("no shared disabled rule for .btn, .btn-primary and .btn-danger")
	}
	rule := css[idx : idx+strings.Index(css[idx:], "}")]
	for _, want := range []string{"background:", "border-color:", "color:", "opacity:"} {
		if !strings.Contains(rule, want) {
			t.Errorf("the shared disabled rule does not set %s", want)
		}
	}
}

// A layout pass belongs to the document that ran it, not to whatever the chrome
// shows when it arrives, and a failed report is retried rather than lost.
func TestChromeLayoutPass_VersionedToItsDocumentAndRetried(t *testing.T) {
	runChromeNodeTest(t, "chrome_layout_version_test.js")
}

// "Listening", "working" (prompts delivered, no new poll or reply yet) and
// "not listening" are three distinct states in the panel, and working falls
// back to the warning when its window runs out.
func TestChromePresence_WorkingIsDistinctFromNotListening(t *testing.T) {
	runChromeNodeTest(t, "chrome_working_test.js")
}

// The blocking "agent is working" overlay: shown from the server's state,
// inert behind it, no way out before the grace period, cleared by any answer.
func TestChromeWorkingOverlay_BlocksUntilAnsweredWithAGraceEscape(t *testing.T) {
	runChromeNodeTest(t, "chrome_overlay_test.js")
}

// Round counter, conversation grouped by round with message status, and the
// per-question sent report to the artifact.
func TestChromeRounds_CounterGroupsAndStatuses(t *testing.T) {
	runChromeNodeTest(t, "chrome_rounds_test.js")
}

// Markup contract of the overlay: an accessible modal dialog, initially
// hidden, whose only control (Stop waiting) starts hidden too.
func TestChromeWorkingOverlay_MarkupIsAModalWithAHiddenWayOut(t *testing.T) {
	data, err := os.ReadFile("assets/chrome/chrome.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	start := strings.Index(html, `id="workingBackdrop"`)
	if start < 0 {
		t.Fatal("chrome.html has no working overlay")
	}
	block := html[start : start+strings.Index(html[start:], `id="endedBackdrop"`)]
	if !strings.Contains(html[start:start+40], "hidden") {
		t.Error("the overlay must start hidden")
	}
	for _, want := range []string{`role="dialog"`, `aria-modal="true"`, `aria-labelledby="workingTitle"`, `aria-describedby=`, `tabindex="-1"`, "Agent is working on your feedback"} {
		if !strings.Contains(block, want) {
			t.Errorf("overlay markup missing %s", want)
		}
	}
	buttons := regexp.MustCompile(`<button\b[^>]*>`).FindAllString(block, -1)
	if len(buttons) != 1 || !strings.Contains(buttons[0], `id="workingStop"`) || !strings.Contains(buttons[0], "hidden") {
		t.Errorf("the only control must be the initially hidden Stop waiting button, got %v", buttons)
	}
}

// The Marks switch and the annotation badges the chrome asks the artifact to draw.
func TestChromeMarks_SwitchAndAnnotationReport(t *testing.T) {
	runChromeNodeTest(t, "chrome_marks_test.js")
}

// The round counter is a toolbar control like its neighbours: it sits in the
// same row (topbar-actions) and has the same box (padding, border, radius,
// type) as the switches.
func TestChromeRoundChip_IsAToolbarControlWithTheSwitchBox(t *testing.T) {
	htmlData, err := os.ReadFile("assets/chrome/chrome.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlData)
	actions := html[strings.Index(html, `class="topbar-actions"`):strings.Index(html, "</header>")]
	for _, id := range []string{"layoutBtn", "annotateSwitch", "marksSwitch", "themeSwitch", "roundChip", "sessionBadge"} {
		if !strings.Contains(actions, `id="`+id+`"`) {
			t.Errorf("%s is not in the top bar's controls row", id)
		}
	}
	cssData, err := os.ReadFile("assets/chrome/forum.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssData)
	rule := func(sel string) string {
		i := strings.Index(css, "\n"+sel+" {")
		if i < 0 {
			t.Fatalf("no %s rule", sel)
		}
		return css[i : i+strings.Index(css[i:], "}")]
	}
	for _, want := range []string{"padding: var(--fr-space-1) var(--fr-space-3)", "border: 1px solid var(--fr-border-strong)", "border-radius: 999px", "font-size: var(--fr-text-size-small)"} {
		if !strings.Contains(rule(".switch"), want) || !strings.Contains(rule(".round-chip"), want) {
			t.Errorf("the switches and the round chip no longer share %q", want)
		}
	}
}
