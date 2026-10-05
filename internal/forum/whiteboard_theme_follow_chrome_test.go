//go:build unix

package forum_test

import (
	"encoding/json"
	"testing"
	"time"
)

// The whiteboard follows a live theme switch: the chrome flips the artifact's
// <html data-fr-theme> (forum-sdk.js does it on "forum:theme") and everything
// repaints without a reload and without losing the scene. In real Chrome, on
// the real embed and frame: the frame chrome, the Excalidraw canvas host, the
// lock affordance, the embed's own box and the fullscreen overlay, in both
// directions (dark to light and back).
const themeFollowScript = `
const inline = await boardReady(0);
const idoc = inline.contentDocument, iwin = inline.contentWindow;
// Marks that a reload or a remount would wipe.
window.__alive = "page"; iwin.__alive = "frame";
idoc.querySelector(".excalidraw").setAttribute("data-mark", "same-editor");
idoc.querySelector("canvas").setAttribute("data-mark", "same-canvas");

const paint = (iframe) => {
  const d = iframe.contentDocument, w = iframe.contentWindow;
  const c = (el, p) => w.getComputedStyle(el)[p];
  return {
    attr: d.documentElement.getAttribute("data-fr-theme"),
    body: c(d.body, "backgroundColor"),
    bar: c(d.querySelector(".vxb-bar"), "backgroundColor"),
    queue: c(d.querySelector(".vxb-btn-primary"), "backgroundColor"),
    editorDark: d.querySelector(".excalidraw").classList.contains("theme--dark"),
    editorPrimary: w.getComputedStyle(d.querySelector(".excalidraw")).getPropertyValue("--color-primary").trim().toLowerCase(),
    scheme: iframe.style.colorScheme,
    intact: d.querySelector(".excalidraw").getAttribute("data-mark") === "same-editor" && d.querySelector("canvas").getAttribute("data-mark") === "same-canvas",
    alive: (window.__alive || "") + "/" + (w.__alive || ""),
  };
};
const embed = () => {
  const slot = document.querySelector(".vxb-slot");
  const unlock = slot.querySelector(".vxb-unlock");
  const cs = (el) => getComputedStyle(el);
  return {
    slotAttr: slot.getAttribute("data-vxb-palette"),
    slot: cs(slot).backgroundColor,
    slotLine: cs(slot).borderTopColor,
    unlock: unlock ? cs(unlock).backgroundColor : "",
    unlockText: unlock ? cs(unlock).color : "",
  };
};
const flip = async (to) => {
  document.documentElement.setAttribute("data-fr-theme", to);
  await until(() => inline.contentDocument.documentElement.getAttribute("data-fr-theme") === to, 10000, "the frame to switch");
  await sleep(500);
};

const steps = {};
steps.dark1 = { frame: paint(inline), embed: embed() };
await flip("light");
steps.light = { frame: paint(inline), embed: embed() };
await flip("dark");
steps.dark2 = { frame: paint(inline), embed: embed() };

// Fullscreen, then the switch with the overlay and its frame open.
idoc.querySelector(".vxb-tools .vxb-btn:last-child").click();
const overlay = await until(() => document.getElementById("vxb-overlay"), 15000, "the overlay");
const full = await boardReady(overlayFrame);
const overlayPaint = () => ({
  attr: overlay.getAttribute("data-vxb-palette"),
  bg: getComputedStyle(overlay).backgroundColor,
  back: getComputedStyle(overlay.querySelector("button")).backgroundColor,
  frame: paint(full),
  inline: paint(inline),
});
steps.fullDark = overlayPaint();
await flip("light");
await until(() => full.contentDocument.documentElement.getAttribute("data-fr-theme") === "light", 10000, "the fullscreen frame to switch");
await sleep(500);
steps.fullLight = overlayPaint();
report("result", JSON.stringify(steps));
`

func TestWhiteboard_RealChrome_FollowsALiveThemeSwitch(t *testing.T) {
	env := newEnv(t, time.Minute)
	key := env.open().Key
	got := runLab(t, env, key, "dark", []string{"flowchart LR\n  A --> B"}, themeFollowScript, 1300, 900, 120*time.Second)
	t.Logf("steps: %s", got)

	type framePaint struct {
		Attr, Body, Bar, Queue, EditorPrimary, Scheme, Alive string
		EditorDark, Intact                                   bool
	}
	type embedPaint struct{ SlotAttr, Slot, SlotLine, Unlock, UnlockText string }
	type full struct {
		Attr, Bg, Back string
		Frame, Inline  framePaint
	}
	var steps struct {
		Dark1, Light, Dark2 struct {
			Frame framePaint
			Embed embedPaint
		}
		FullDark, FullLight full
	}
	if err := json.Unmarshal([]byte(got), &steps); err != nil {
		t.Fatalf("bad report: %v\n%s", err, got)
	}

	// Token values: forum-tokens.css, dark and light.
	darkFrame := framePaint{Attr: "dark", Body: "rgb(21, 23, 26)", Bar: "rgb(32, 36, 42)", Queue: "rgb(111, 161, 203)", EditorPrimary: "#6fa1cb", Scheme: "dark", EditorDark: true, Intact: true, Alive: "page/frame"}
	lightFrame := framePaint{Attr: "light", Body: "rgb(242, 241, 236)", Bar: "rgb(236, 235, 228)", Queue: "rgb(31, 78, 121)", EditorPrimary: "#1f4e79", Scheme: "light", EditorDark: false, Intact: true, Alive: "page/frame"}
	darkEmbed := embedPaint{SlotAttr: "dark", Slot: "rgb(21, 23, 26)", SlotLine: "rgb(58, 63, 71)", Unlock: "rgb(111, 161, 203)", UnlockText: "rgb(15, 34, 51)"}
	lightEmbed := embedPaint{SlotAttr: "light", Slot: "rgb(242, 241, 236)", SlotLine: "rgb(194, 192, 184)", Unlock: "rgb(31, 78, 121)", UnlockText: "rgb(255, 255, 255)"}

	check := func(name string, got, want framePaint) {
		t.Helper()
		if got != want {
			t.Errorf("%s: frame is %+v, want %+v", name, got, want)
		}
	}
	checkEmbed := func(name string, got, want embedPaint) {
		t.Helper()
		if got != want {
			t.Errorf("%s: embed is %+v, want %+v", name, got, want)
		}
	}
	check("dark at the start", steps.Dark1.Frame, darkFrame)
	checkEmbed("dark at the start", steps.Dark1.Embed, darkEmbed)
	check("after dark to light", steps.Light.Frame, lightFrame)
	checkEmbed("after dark to light", steps.Light.Embed, lightEmbed)
	check("after light to dark", steps.Dark2.Frame, darkFrame)
	checkEmbed("after light to dark", steps.Dark2.Embed, darkEmbed)

	if steps.FullDark.Attr != "dark" || steps.FullDark.Bg != "rgb(21, 23, 26)" || steps.FullDark.Back != "rgb(111, 161, 203)" {
		t.Errorf("fullscreen overlay in dark: %+v", steps.FullDark)
	}
	check("fullscreen frame, dark", steps.FullDark.Frame, framePaint{Attr: "dark", Body: darkFrame.Body, Bar: darkFrame.Bar, Queue: darkFrame.Queue, EditorPrimary: darkFrame.EditorPrimary, Scheme: "dark", EditorDark: true, Intact: false, Alive: "page/"})
	if steps.FullLight.Attr != "light" || steps.FullLight.Bg != "rgb(242, 241, 236)" || steps.FullLight.Back != "rgb(31, 78, 121)" {
		t.Errorf("fullscreen overlay in light: %+v", steps.FullLight)
	}
	check("fullscreen frame, light", steps.FullLight.Frame, framePaint{Attr: "light", Body: lightFrame.Body, Bar: lightFrame.Bar, Queue: lightFrame.Queue, EditorPrimary: lightFrame.EditorPrimary, Scheme: "light", EditorDark: false, Intact: false, Alive: "page/"})
}
