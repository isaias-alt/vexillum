//go:build unix

package forum_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The whiteboard's buttons are small forum buttons on the --fr-* tokens, in
// both themes, and in fullscreen nothing in the frame's header sits under the
// embed's floating way-back control. Measured in real Chrome on the real frame
// and the real overlay, at a normal and at a narrow window.
const buttonsScript = `
const inline = await boardReady(0);
const box = (el) => {
  const r = el.getBoundingClientRect(), c = getComputedStyle(el);
  return { x: r.left, y: r.top, w: r.width, h: r.height, bg: c.backgroundColor, color: c.color, fs: c.fontSize };
};
const inFrame = (iframe, sel) => {
  const el = iframe.contentDocument.querySelector(sel);
  const w = iframe.contentWindow, r = el.getBoundingClientRect(), c = w.getComputedStyle(el);
  const f = iframe.getBoundingClientRect();
  return { x: f.left + r.left, y: f.top + r.top, w: r.width, h: r.height, bg: c.backgroundColor, color: c.color, fs: c.fontSize, hidden: r.width === 0 };
};
const inlineQueue = inFrame(inline, ".vxb-btn-primary");
const inlineFull = inFrame(inline, ".vxb-tools .vxb-btn:last-child");

inline.contentDocument.querySelector(".vxb-tools .vxb-btn:last-child").click();
const overlay = await until(() => document.getElementById("vxb-overlay"), 15000, "the overlay");
const full = await boardReady(overlayFrame);
const back = overlay.querySelector("button");
const controls = {
  remark: inFrame(full, ".vxb-remark"),
  queue: inFrame(full, ".vxb-btn-primary"),
};
const hits = {};
for (const [name, c] of Object.entries(controls)) {
  const hit = document.elementFromPoint(c.x + c.w / 2, c.y + c.h / 2);
  hits[name] = hit === full ? "frame" : (hit ? hit.tagName + "." + hit.className : "nothing");
}
const backBox = box(back);
const overlaps = Object.entries(controls).filter(([, c]) => c.x < backBox.x + backBox.w && c.x + c.w > backBox.x && c.y < backBox.y + backBox.h && c.y + c.h > backBox.y).map(([n]) => n);
report("result", JSON.stringify({
  inlineQueue, inlineFull, back: backBox, controls, hits, overlaps,
  backFirst: overlay.querySelector("button") === overlay.querySelectorAll("button")[0] && overlay.children[0].contains(back),
  fullscreenButtonHidden: inFrame(full, ".vxb-tools .vxb-btn:last-child").hidden,
  scrollable: document.documentElement.scrollWidth <= window.innerWidth,
}));
`

func TestWhiteboard_RealChrome_ButtonsAreSmallForumButtonsInBothThemes(t *testing.T) {
	type rect struct {
		X, Y, W, H float64
		Bg, Color  string
		Hidden     bool
	}
	type report struct {
		InlineQueue, InlineFull, Back rect
		Controls                      map[string]rect
		Hits                          map[string]string
		Overlaps                      []string
		BackFirst                     bool
		FullscreenButtonHidden        bool
	}
	for theme, want := range map[string]struct{ surface, accent, accentText, text string }{
		"dark":  {"rgb(28, 31, 36)", "rgb(111, 161, 203)", "rgb(15, 34, 51)", "rgb(233, 234, 236)"},
		"light": {"rgb(255, 255, 255)", "rgb(31, 78, 121)", "rgb(255, 255, 255)", "rgb(26, 29, 34)"},
	} {
		for _, width := range []int{1300, 640} {
			t.Run(fmt.Sprintf("%s/%dpx", theme, width), func(t *testing.T) {
				env := newEnv(t, time.Minute)
				key := env.open().Key
				got := runLab(t, env, key, theme, []string{"flowchart LR\n  A --> B"}, buttonsScript, width, 900, 120*time.Second)
				t.Logf("buttons: %s", got)
				var r report
				if err := json.Unmarshal([]byte(got), &r); err != nil {
					t.Fatalf("bad report: %v\n%s", err, got)
				}
				// Small: the forum's 28px control, not a 34px+ slab.
				for name, b := range map[string]rect{"Queue feedback": r.InlineQueue, "Fullscreen": r.InlineFull, "Back to page": r.Back} {
					if b.H < 24 || b.H > 30 {
						t.Errorf("%s is %.1fpx tall, want the forum's small button (about 28px)", name, b.H)
					}
				}
				if r.InlineQueue.Bg != want.accent || r.InlineQueue.Color != want.accentText {
					t.Errorf("Queue feedback is not the accent primary button: %+v", r.InlineQueue)
				}
				if r.InlineFull.Bg != want.surface || r.InlineFull.Color != want.text {
					t.Errorf("Fullscreen is not a plain surface button: %+v", r.InlineFull)
				}
				if r.Back.Bg != want.accent || r.Back.Color != want.accentText {
					t.Errorf("Back to page is not on the accent tokens: %+v", r.Back)
				}
				for name, c := range map[string]rect{"Queue feedback": r.InlineQueue, "Back to page": r.Back} {
					if strings.Contains(c.Bg, "244, 201, 93") {
						t.Errorf("%s still paints the old gold", name)
					}
				}
				if !r.FullscreenButtonHidden {
					t.Error("the Fullscreen button is still offered inside fullscreen")
				}
				if !r.BackFirst {
					t.Error("the way back is not the first button of the overlay")
				}
				// The way back never covers a control of the frame's header.
				if len(r.Overlaps) != 0 {
					t.Errorf("the way-back control overlaps %v: back %+v controls %+v", r.Overlaps, r.Back, r.Controls)
				}
				for name, hit := range r.Hits {
					if hit != "frame" {
						t.Errorf("the point at the middle of %q hits %s, not the frame: it is covered", name, hit)
					}
				}
			})
		}
	}
}
