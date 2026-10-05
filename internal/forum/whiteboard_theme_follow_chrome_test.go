//go:build unix

package forum_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The whiteboard follows a live theme switch: the chrome flips the artifact's
// <html data-fr-theme> (forum-sdk.js does it on "forum:theme"), and the embed
// must repaint its own boxes and tell every running frame, inline and
// fullscreen, without a reload. The frames' insides are opaque to the artifact,
// so this measures what the artifact can see in real Chrome: the colour scheme
// of both iframes and the overlay's Close button. The fullscreen overlay is
// opened the way the frame's Fullscreen button does it, by a "maximize" message
// from the real inline frame's window.
const whiteboardThemeFollowArtifact = `<!doctype html><html data-fr-theme="dark"><head><meta charset="utf-8"><title>wb</title>
</head><body>
<div class="mermaid">flowchart LR
  A --> B
</div>
<script>
const report = (k, v) => fetch("/__report?k=" + k + "&v=" + encodeURIComponent(v), { mode: "no-cors" });
const snap = () => {
  const overlay = document.getElementById("vxWhiteboardOverlay");
  return {
    inline: document.querySelector("iframe").style.colorScheme,
    overlay: overlay ? overlay.querySelector("iframe").style.colorScheme : null,
    overlaySrc: overlay ? overlay.querySelector("iframe").getAttribute("src") : null,
    close: overlay ? overlay.querySelector("button").style.color : null,
    shown: overlay ? overlay.style.display : null,
  };
};
let ready = null;
window.addEventListener("message", (e) => { if (e.data && e.data.type === "vx-whiteboard:ready" && !ready) ready = { id: e.data.channelId, source: e.source }; });
const wait = setInterval(() => {
  if (!ready) return;
  clearInterval(wait);
  setTimeout(() => {
    window.dispatchEvent(new MessageEvent("message", { data: { type: "vx-whiteboard:maximize", diagramIndex: 0, channelId: ready.id }, source: ready.source }));
    const poll = setInterval(() => {
      const overlay = document.getElementById("vxWhiteboardOverlay");
      if (!overlay || overlay.style.display !== "block") return;
      clearInterval(poll);
      const before = snap();
      document.documentElement.setAttribute("data-fr-theme", "light");
      setTimeout(() => report("both", JSON.stringify({ before, after: snap() })), 400);
    }, 200);
  }, 2500);
}, 200);
</script></body></html>`

func TestWhiteboard_RealChrome_FollowsALiveThemeSwitch(t *testing.T) {
	chrome := headlessChrome(t)
	env := newEnv(t, time.Minute)
	key := env.open().Key
	env.setArtifact(whiteboardThemeFollowArtifact)
	reports := captureReports(t, env)

	got := reportFromChrome(t, chrome, env.ts.URL+"/session/"+key, reports, "both")
	var snaps struct{ Before, After map[string]any }
	if err := json.Unmarshal([]byte(got), &snaps); err != nil {
		t.Fatalf("bad report %q: %v", got, err)
	}
	before, after := fmt.Sprint(snaps.Before), fmt.Sprint(snaps.After)
	t.Logf("before: %s\nafter:  %s", before, after)

	for _, want := range []string{"inline:dark", "overlay:dark", "theme=dark", "shown:block", "#E9EAEC"} {
		if !strings.Contains(before, want) {
			t.Errorf("before the switch: %s lacks %s", before, want)
		}
	}
	for _, want := range []string{"inline:light", "overlay:light", "#1A1D22", "shown:block"} {
		if !strings.Contains(after, want) {
			t.Errorf("after the switch: %s lacks %s", after, want)
		}
	}
}
