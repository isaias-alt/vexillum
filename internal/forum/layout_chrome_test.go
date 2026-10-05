//go:build unix

package forum_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
)

// headlessChrome returns a Chrome or Chromium binary, or skips the test:
// Chrome is a test-time convenience, never a runtime dependency.
func headlessChrome(t *testing.T) string {
	t.Helper()
	candidates := []string{os.Getenv("VX_TEST_CHROME"), "google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium"}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if path, err := exec.LookPath(c); err == nil {
			return path
		}
	}
	t.Skip("no Chrome or Chromium found (set VX_TEST_CHROME)")
	return ""
}

// auditMessage is the "forum:layout" message the audit posts to its parent.
type auditMessage struct {
	Type                   string               `json:"type"`
	ArtifactVersion        string               `json:"artifact_version"`
	Complete               bool                 `json:"complete"`
	TargetPresenceComplete bool                 `json:"target_presence_complete"`
	ViewportWidth          float64              `json:"viewport_width"`
	Findings               []forum.AuditFinding `json:"findings"`
}

// kinds returns "kind selector" for every finding, for readable assertions.
func (m auditMessage) listed() []string {
	out := make([]string, len(m.Findings))
	for i, f := range m.Findings {
		out[i] = f.Kind + " " + f.Selector
	}
	return out
}

func (m auditMessage) find(kind, selector string) (forum.AuditFinding, bool) {
	for _, f := range m.Findings {
		if f.Kind == kind && f.Selector == selector {
			return f, true
		}
	}
	return forum.AuditFinding{}, false
}

// auditInChrome serves artifact through the forum server (so the real audit
// script is injected) inside an iframe of a tiny host page, the way the session
// page frames it, and returns the first complete report the audit posts to the
// host. The viewport is 1100 px wide at the given device pixel ratio.
func auditInChrome(t *testing.T, artifact string, dpr float64) auditMessage {
	t.Helper()
	chrome := headlessChrome(t)
	env := newEnv(t, time.Minute)
	key := env.open().Key
	env.setArtifact(artifact)

	messages := make(chan auditMessage, 16)
	env.handle("/__host", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><body style="margin:0"><iframe src="/a/%s/artifact.html" style="width:1100px;height:800px;border:0"></iframe>
<script>addEventListener("message", (e) => { if (e.data && e.data.type === "forum:layout") fetch("/__report", { method: "POST", body: JSON.stringify(e.data) }); });</script>`, key)
	})
	env.handle("/__report", func(w http.ResponseWriter, r *http.Request) {
		var m auditMessage
		if json.NewDecoder(r.Body).Decode(&m) == nil {
			select {
			case messages <- m:
			default:
			}
		}
	})

	cmd := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--window-size=1100,800",
		fmt.Sprintf("--force-device-scale-factor=%v", dpr), "--user-data-dir="+t.TempDir(), env.ts.URL+"/__host")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("chrome: %v", err)
	}
	defer func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() }()

	deadline := time.After(45 * time.Second)
	for {
		select {
		case m := <-messages:
			if m.Complete {
				return m
			}
		case <-deadline:
			t.Fatal("the audit never posted a complete report")
		}
	}
}

// fitScript sizes containers to the exact width of the text they hold, minus a
// shave in px, so a fixture can sit on either side of a threshold. Elements
// opt in with data-shave="<px>".
const fitScript = `<script>for (const box of document.querySelectorAll("[data-shave]")) {
  const inner = box.firstElementChild, w = inner.getBoundingClientRect().width;
  box.style.width = (w - Number(box.dataset.shave)) + "px";
}</script>`

const auditHead = `<!doctype html><html><head><meta charset="utf-8"><title>audit</title><style>
body{margin:0;font:16px/1.4 Helvetica,Arial,sans-serif} .box{overflow:hidden;margin:4px 0}
</style></head><body>`

// A page of failures, one for each outcome the audit protects, with a failing
// fixture per rule kind.
func TestLayoutAudit_RealChrome_ReportsEachKindOfProvableFailure(t *testing.T) {
	msg := auditInChrome(t, auditHead+`
<div style="width:2400px;height:10px;background:#ccc"></div>
<div id="card" style="width:200px;height:40px;overflow:hidden;border:1px solid #888"><span style="white-space:nowrap">Supercalifragilisticexpialidocious_and_more_text</span></div>
<div id="tall" style="width:300px;height:20px;overflow:hidden"><p style="margin:0">One line of text. Two lines of text. Three lines of text. Four lines of text, which together do not fit in twenty pixels at all.</p></div>
<div style="position:relative;height:40px"><p id="under" style="position:absolute;top:0;left:0;margin:0">A headline someone painted over with a white block</p><div style="position:absolute;top:0;left:0;width:100%;height:100%;background:#fff;z-index:2"></div></div>
<div style="width:80px;overflow:hidden"><button id="cut" style="white-space:nowrap;width:140px">Save changes</button></div>
<p id="lost" style="position:absolute;left:-300px;top:300px;margin:0;white-space:nowrap">Parked off the left side</p>
<button id="lostbtn" style="position:absolute;left:-200px;top:340px">Go</button>
`+fitScript+`</body></html>`, 1)

	if !msg.TargetPresenceComplete {
		t.Error("a page that settled must report target presence complete")
	}
	for _, want := range []struct{ kind, selector, axis string }{
		{"wide-page", "", "horizontal"},
		{"clipped-text", "div#card", "horizontal"},
		{"clipped-text", "div#tall", "vertical"},
		{"buried-text", "p#under", "horizontal"},
		{"cut-off-control", "button#cut", "horizontal"},
		{"unreachable-text", "p#lost", "horizontal"},
		{"unreachable-control", "button#lostbtn", "horizontal"},
	} {
		f, ok := msg.find(want.kind, want.selector)
		if !ok {
			t.Errorf("missing %s %q in %v", want.kind, want.selector, msg.listed())
			continue
		}
		if f.Axis != want.axis {
			t.Errorf("%s %q axis = %s, want %s", want.kind, want.selector, f.Axis, want.axis)
		}
	}
	if f, _ := msg.find("wide-page", ""); f.OverflowPx < 1250 || f.OverflowPx > 1350 {
		t.Errorf("a 2400px box in a 1100px viewport overflows by about 1300px, got %v", f.OverflowPx)
	}
	if f, _ := msg.find("unreachable-text", "p#lost"); f.OverflowPx >= 0 {
		t.Errorf("text parked left of the page start must be negative, got %v", f.OverflowPx)
	}
	if f, _ := msg.find("clipped-text", "div#card"); f.OverflowPx <= 0 {
		t.Errorf("text crossing the right edge must be positive, got %v", f.OverflowPx)
	}
}

// Each deliberate pattern of the silent list, next to one positive control so
// the test proves the audit ran: only the control may be reported.
func TestLayoutAudit_RealChrome_StaysSilentOnDeliberatePatterns(t *testing.T) {
	msg := auditInChrome(t, auditHead+`<style>
@keyframes marquee{from{transform:translateX(0)}to{transform:translateX(-600px)}}
@keyframes settle{from{transform:translateX(400px)}to{transform:translateX(0)}}
.sr{position:absolute;left:-9999px}
.sr2{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap}
</style>
<header id="bar" style="position:sticky;top:0;height:60px;background:#fff;z-index:5">Sticky header</header>
<div style="height:300px"></div>
<p id="beneath" style="margin:0">A paragraph that the sticky header covers once the page is scrolled to it.</p>
<p id="ell" style="width:120px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">An ellipsis line that is far too long for its box on purpose</p>
<p id="clamp" style="width:200px;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden">A clamped paragraph with a great deal of text that goes on and on and on so that it needs many more lines than the two it is allowed to show, and then some more.</p>
<div id="scroller" style="width:200px;overflow-x:auto"><div style="width:900px;white-space:nowrap">A scroller with wide content <button id="inscroll">Inside</button></div></div>
<label class="sr" id="srlabel">Search the site</label>
<span class="sr2" id="srspan">Hidden but announced text</span>
<div id="mask" style="width:120px;overflow:hidden;-webkit-mask-image:linear-gradient(#000,#000)"><span style="white-space:nowrap">Masked text that is wider than its box</span></div>
<div id="clip" style="width:120px;height:20px;overflow:hidden;clip-path:inset(0)"><span style="white-space:nowrap">Clip-path text that is wider than its box</span></div>
<div id="hidden" style="display:none"><div style="width:10px;overflow:hidden"><span style="white-space:nowrap">Never rendered</span></div></div>
<div id="invisible" style="visibility:hidden;width:10px;overflow:hidden"><span style="white-space:nowrap">Invisible text</span></div>
<div id="transparent" style="opacity:0;width:10px;overflow:hidden"><span style="white-space:nowrap">Fully transparent text</span></div>
<div id="collapsed" style="height:0;overflow:hidden"><p>Collapsed panel content</p></div>
<div id="marquee" style="width:200px;overflow:hidden"><span style="display:inline-block;white-space:nowrap;animation:marquee 20s linear infinite">An endlessly moving ticker of text that is wider than the window it scrolls through</span></div>
<div id="arriving" style="width:300px;overflow:hidden"><span style="display:inline-block;animation:settle 1s forwards">Text that slides in and ends up whole</span></div>
<div id="control" style="width:200px;height:40px;overflow:hidden"><span style="white-space:nowrap">Positive control that really is cut off by its box, so it must be reported</span></div>
<script>addEventListener("load",()=>{document.getElementById("beneath").scrollIntoView();scrollBy(0,0)})</script>
</body></html>`, 1)

	var kinds []string
	for _, f := range msg.Findings {
		kinds = append(kinds, f.Kind+" "+f.Selector)
	}
	if len(msg.Findings) != 1 || msg.Findings[0].Kind != "clipped-text" || msg.Findings[0].Selector != "div#control" {
		t.Fatalf("findings = %v, want only the positive control clipped-text div#control", kinds)
	}
}

// Benign layouts must read as nothing, at the device pixel ratios the
// thresholds were calibrated on, and the failing neighbours of each threshold
// must still be reported.
func TestLayoutAudit_RealChrome_ThresholdBoundaries(t *testing.T) {
	page := auditHead + `<style>.t{white-space:nowrap}</style>
<div class="box" data-shave="0.3" id="exact"><span class="t">Quarterly revenue summary</span></div>
<div class="box" data-shave="0" id="italic" style="font-style:italic"><span class="t">Offices of fjords</span></div>
<div class="box" data-shave="0.5" id="spaced" style="letter-spacing:.2em"><span class="t">Quarterly revenue</span></div>
<div class="box" data-shave="1" id="over1"><span class="t">Quarterly revenue summary</span></div>
<div class="box" data-shave="3" id="over3"><span class="t">Quarterly revenue summary</span></div>
<div class="box" id="tight" style="line-height:.8;height:12.8px;width:300px"><span>Descending glyphs gjpqy</span></div>
<div class="box" id="short" style="line-height:1.5;height:10px;width:300px"><span>Descending glyphs gjpqy</span></div>
<div style="width:100px;overflow:hidden"><button id="cut3" style="white-space:nowrap;width:103px;padding:0">Save</button></div>
<div style="width:100px;overflow:hidden"><button id="cut20" style="white-space:nowrap;width:120px;padding:0">Save</button></div>
<div style="position:relative;height:24px;width:400px"><p id="partial" style="margin:0;white-space:nowrap">The quick brown fox jumps over the lazy dog</p><div style="position:absolute;left:0;top:0;height:100%;width:20%;background:#fff"></div></div>
<div style="width:calc(100% + 30px);height:6px;background:#eee"></div>
` + fitScript + `</body></html>`

	for _, dpr := range []float64{1, 1.25, 2} {
		msg := auditInChrome(t, page, dpr)
		got := map[string]bool{}
		for _, f := range msg.Findings {
			got[f.Kind+" "+f.Selector] = true
		}
		for _, silent := range []string{"clipped-text div#exact", "clipped-text div#italic", "clipped-text div#spaced", "clipped-text div#over1", "clipped-text div#tight",
			"cut-off-control button#cut3", "buried-text p#partial", "wide-page "} {
			if got[silent] {
				t.Errorf("dpr %v: %q is below its threshold and must stay silent (%v)", dpr, silent, msg.listed())
			}
		}
		for _, loud := range []string{"clipped-text div#over3", "clipped-text div#short", "cut-off-control button#cut20"} {
			if !got[loud] {
				t.Errorf("dpr %v: %q is past its threshold and must be reported (%v)", dpr, loud, msg.listed())
			}
		}
	}
}
