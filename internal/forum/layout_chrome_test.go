//go:build unix

package forum_test

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// headlessChrome finds a Chrome/Chromium to drive; tests that need real
// layout skip without one (it is a test-time convenience, never a runtime
// dependency).
func headlessChrome(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	for _, p := range []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("no Chrome/Chromium installed")
	return ""
}

// auditInRealChrome opens the session in headless Chrome through the real
// forum server - the real chrome page, with the artifact in its sandboxed
// iframe - and returns what the passive audit filed in the session's inbox as
// "rule selector" lines, once ready(found) says the pass that matters is in.
// Chrome is killed afterwards (it would linger on its own).
func auditInRealChrome(t *testing.T, artifact string, ready func(found []string) bool) []string {
	t.Helper()
	chrome := headlessChrome(t)
	env := newEnv(t, time.Minute)
	key := env.open().Key
	env.setArtifact(artifact)

	cmd := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--hide-scrollbars",
		"--window-size=1500,800", "--user-data-dir="+t.TempDir(), env.ts.URL+"/session/"+key)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("chrome: %v", err)
	}
	defer func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() }()

	var found []string
	for deadline := time.Now().Add(45 * time.Second); time.Now().Before(deadline); time.Sleep(300 * time.Millisecond) {
		found = found[:0]
		for _, w := range env.snapshot(key).LayoutWarnings {
			found = append(found, w.Rule+" "+w.Selector)
		}
		if ready(found) {
			time.Sleep(500 * time.Millisecond) // anything else the same pass filed
			found = found[:0]
			for _, w := range env.snapshot(key).LayoutWarnings {
				found = append(found, w.Rule+" "+w.Selector)
			}
			return found
		}
	}
	t.Fatalf("the audit never filed the expected finding; inbox: %v", found)
	return nil
}

func has(found []string, kind, selectorPart string) bool {
	for _, f := range found {
		if strings.HasPrefix(f, kind+" ") && strings.Contains(f, selectorPart) {
			return true
		}
	}
	return false
}

// The artifact the commander reported as undetected: a 2400px box whose text
// sits at its left edge, and a positioned paragraph under a 95%-opaque block.
const brokenOnPurpose = `<!doctype html><html><head><meta charset="utf-8"><title>roto</title></head><body style="margin:0;font-family:sans-serif">
<h1>Artifact roto a proposito</h1>
<div id="wide" style="width:2400px;background:#fcc;padding:8px">Esta caja desborda horizontalmente la pagina</div>
<div style="position:relative;height:80px"><p id="covered" style="position:absolute;left:10px;top:10px">Texto tapado</p><div style="position:absolute;left:0;top:0;width:300px;height:60px;background:#369;opacity:.95"></div></div>
</body></html>`

func TestLayoutAudit_RealChrome_ReportsAnObviousOverflowAndAPositionedCoveredText(t *testing.T) {
	found := auditInRealChrome(t, brokenOnPurpose, func(f []string) bool {
		return has(f, "page-horizontal-overflow", "html") && has(f, "overlapping-text", "covered")
	})
	if !has(found, "page-horizontal-overflow", "html") {
		t.Errorf("a 2400px box in a 1100px viewport was not reported as a sideways-scrolling page; findings: %v", found)
	}
	if !has(found, "overlapping-text", "covered") {
		t.Errorf("a paragraph under an opaque block was not reported as covered; findings: %v", found)
	}
}

// What must stay silent, in the same real layout engine.
func TestLayoutAudit_RealChrome_StaysSilentOnDeliberatePatterns(t *testing.T) {
	// A real failure rides along as the positive control: once it is filed the
	// pass is in, and nothing else may have been filed with it.
	found := auditInRealChrome(t, `<!doctype html><html><head><meta charset="utf-8"><title>ok</title></head><body style="margin:0;font-family:sans-serif">
<h1>Fine</h1>
<div id="control" style="width:120px;overflow:hidden;white-space:nowrap">this control sentence is cut off by its box</div>
<div id="scroller" style="width:300px;overflow-x:auto;white-space:nowrap;border:1px solid #888"><div style="width:2400px;background:#cfc">wide on purpose, scrolls inside its own box</div></div>
<div id="ellipsis" style="width:200px;overflow:hidden;white-space:nowrap;text-overflow:ellipsis">deliberately truncated with an ellipsis for sure</div>
<div style="display:none;width:3000px">never rendered</div>
<div id="slide-stack" style="position:relative;height:60px"><p id="slide" style="position:absolute;left:0;top:0;opacity:0">hidden slide that is covered anyway</p><div style="position:absolute;left:0;top:0;width:300px;height:50px;background:#369">top slide</div></div>
<div style="overflow-x:hidden;width:100%"><div style="width:3000px;background:#eee;height:10px"></div></div>
</body></html>`, func(f []string) bool { return has(f, "clipped-text", "control") })
	if len(found) != 1 {
		t.Errorf("only the control should be reported, got: %v", found)
	}
}
