//go:build unix

package forum_test

import (
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The whiteboard's buttons are forum buttons: small, on the --fr-* tokens, in
// both themes. Measured as computed styles in real Chrome, on the two surfaces
// the user sees: the fullscreen "Close" button the embed puts in the artifact,
// and the frame's own header buttons (Queue feedback, Fullscreen), whose
// stylesheet is the one the frame page loads. The old behaviour was a ~34px
// yellow (#f4c95d) button in both places.
const whiteboardButtonsArtifact = `<!doctype html><html data-fr-theme="__THEME__"><head><meta charset="utf-8"><title>wb</title>
<link rel="stylesheet" href="/forum-assets/forum-tokens.css">
<link rel="stylesheet" href="/whiteboard-assets/whiteboard.css">
</head><body data-vexillum-whiteboard-theme="__THEME__">
<div class="mermaid">flowchart LR
  A --> B
</div>
<div id="wbHeader" style="padding-right:12px"><button id="wbQueue" type="button">Queue feedback</button><button id="wbFullscreen" type="button">Fullscreen</button><button id="wbPlain" type="button" disabled>Plain</button></div>
<script>
const report = (v) => fetch("/__report?v=" + encodeURIComponent(v), { mode: "no-cors" });
const box = (el) => { const r = el.getBoundingClientRect(), c = getComputedStyle(el); return { h: Math.round(r.height), w: Math.round(r.width), bg: c.backgroundColor, color: c.color, fs: c.fontSize, fw: c.fontWeight }; };
setTimeout(() => report("frame " + JSON.stringify({ queue: box(document.getElementById("wbQueue")), full: box(document.getElementById("wbFullscreen")) })), 1500);
let ready = null;
window.addEventListener("message", (e) => { if (e.data && e.data.type === "vx-whiteboard:ready" && !ready) ready = { id: e.data.channelId, source: e.source }; });
const timer = setInterval(() => {
  if (!ready) return;
  clearInterval(timer);
  setTimeout(() => {
    window.dispatchEvent(new MessageEvent("message", { data: { type: "vx-whiteboard:maximize", diagramIndex: 0, channelId: ready.id }, source: ready.source }));
    const poll = setInterval(() => {
      const o = document.getElementById("vxWhiteboardOverlay");
      if (!o || o.style.display !== "block") return;
      clearInterval(poll);
      report("close " + JSON.stringify(box(o.querySelector("button"))));
    }, 200);
  }, 2500);
}, 200);
</script></body></html>`

func TestWhiteboard_RealChrome_ButtonsAreSmallForumButtonsInBothThemes(t *testing.T) {
	chrome := headlessChrome(t)
	type sample struct {
		Queue, Full, Close struct {
			H, W      int
			Bg, Color string
		}
	}
	for theme, want := range map[string]struct{ surface, accent, text string }{
		"dark":  {"rgb(28, 31, 36)", "rgb(111, 161, 203)", "rgb(233, 234, 236)"},
		"light": {"rgb(255, 255, 255)", "rgb(31, 78, 121)", "rgb(26, 29, 34)"},
	} {
		t.Run(theme, func(t *testing.T) {
			env := newEnv(t, time.Minute)
			key := env.open().Key
			env.setArtifact(strings.ReplaceAll(whiteboardButtonsArtifact, "__THEME__", theme))
			var mu sync.Mutex
			reports := map[string]string{}
			env.wrap(func(inner http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/forum-assets/forum-theme.js" {
						// The chrome keeps its theme in localStorage, so seed the choice
						// the way the user's toggle would, before the real script reads it.
						w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
						_, _ = w.Write([]byte(`try { localStorage.setItem("forum-theme", "` + theme + `"); } catch (e) {}` + "\n"))
						inner.ServeHTTP(w, r)
						return
					}
					if r.URL.Path == "/__report" {
						v := r.URL.Query().Get("v")
						name, body, _ := strings.Cut(v, " ")
						mu.Lock()
						reports[name] = body
						mu.Unlock()
						return
					}
					inner.ServeHTTP(w, r)
				})
			})
			cmd := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--no-first-run", "--window-size=1500,900",
				"--user-data-dir="+t.TempDir(), env.ts.URL+"/session/"+key)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := cmd.Start(); err != nil {
				t.Fatalf("chrome: %v", err)
			}
			t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() })
			get := func(name string) string {
				for deadline := time.Now().Add(45 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
					mu.Lock()
					v, ok := reports[name]
					mu.Unlock()
					if ok {
						return v
					}
				}
				t.Fatalf("never got the %q measurement", name)
				return ""
			}
			frame, closeBtn := get("frame"), get("close")
			t.Logf("frame buttons: %s", frame)
			t.Logf("fullscreen Close: %s", closeBtn)
			for name, got := range map[string]string{"frame buttons": frame, "Close": closeBtn} {
				if strings.Contains(got, "244, 201, 93") {
					t.Errorf("%s still use the yellow Forum colour: %s", name, got)
				}
			}
			if strings.Contains(frame, `"h":0`) {
				t.Fatalf("the frame buttons were never laid out: %s", frame)
			}
			// Height (px) of a 13px button with 4px padding is ~27; the old ones were ~34-36.
			for _, h := range []string{`"h":3`, `"h":4`} {
				if strings.Contains(frame, h) || strings.Contains(closeBtn, h) {
					t.Errorf("a button is %s tall, the old big size: frame %s close %s", h, frame, closeBtn)
				}
			}
			if !strings.Contains(closeBtn, `"bg":"`+want.surface+`"`) || !strings.Contains(closeBtn, `"color":"`+want.text+`"`) {
				t.Errorf("Close is not on the %s surface/text tokens: %s", theme, closeBtn)
			}
			if !strings.Contains(frame, `"queue":{"h":`) || !strings.Contains(frame, `"bg":"`+want.accent+`"`) {
				t.Errorf("Queue feedback is not the accent primary button: %s", frame)
			}
			if !strings.Contains(frame, `"full":{`) || strings.Count(frame, `"bg":"`+want.surface+`"`) != 1 {
				t.Errorf("Fullscreen is not a plain surface button like Close: %s", frame)
			}
		})
	}
}
