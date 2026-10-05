//go:build unix

package forum_test

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The whiteboard only works through a three-window chain (chrome > artifact >
// frame) and a postMessage handshake across it, which nothing but a real
// browser exercises. The frame autosaves its record once the embed's start
// message has arrived and the Mermaid text converted, so a record on disk
// proves the whole chain: hello -> start -> render -> save -> board.write.
//
// Both conversions run: a flowchart becomes editable shapes, a pie chart is
// not natively convertible and embeds as an image on the same canvas.
func TestWhiteboard_RealChrome_FrameHandshakeRendersAndSavesTheScene(t *testing.T) {
	for name, tc := range map[string]struct {
		source string
		check  func(t *testing.T, elementTypes []string)
	}{
		"flowchart (editable shapes)": {
			source: "flowchart LR\n  A[Start] --> B{Ok?}\n  B -- yes --> C[Done]",
			check: func(t *testing.T, types []string) {
				if !contains(types, "rectangle") || !contains(types, "diamond") || !contains(types, "arrow") {
					t.Errorf("a flowchart should come out as shapes and arrows, got %v", types)
				}
			},
		},
		"pie (image fallback)": {
			source: "pie title Pets\n  \"Dogs\" : 3\n  \"Cats\" : 2",
			check: func(t *testing.T, types []string) {
				for _, typ := range types {
					if typ != "image" {
						t.Errorf("a pie chart should be images only, got %v", types)
						return
					}
				}
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			record := whiteboardSavesRecord(t, tc.source)
			var rec struct {
				Format     int    `json:"format"`
				Digest     string `json:"digest"`
				MeasureGen int    `json:"measure_gen"`
				SavedAt    string `json:"saved_at"`
				Current    struct {
					Elements []struct {
						Type string `json:"type"`
					} `json:"elements"`
				} `json:"current"`
				Pristine struct {
					Elements []json.RawMessage `json:"elements"`
				} `json:"pristine"`
			}
			if err := json.Unmarshal(record, &rec); err != nil {
				t.Fatalf("the stored record is not JSON: %v\n%s", err, record)
			}
			if rec.Format != 2 || rec.Digest == "" || rec.MeasureGen < 1 || rec.SavedAt == "" {
				t.Errorf("record header = format %d digest %q measure_gen %d saved_at %q", rec.Format, rec.Digest, rec.MeasureGen, rec.SavedAt)
			}
			if len(rec.Pristine.Elements) == 0 || len(rec.Pristine.Elements) != len(rec.Current.Elements) {
				t.Errorf("a record saved straight after conversion keeps the same elements as its reference: current %d, pristine %d", len(rec.Current.Elements), len(rec.Pristine.Elements))
			}
			var types []string
			for _, el := range rec.Current.Elements {
				types = append(types, el.Type)
			}
			tc.check(t, types)
		})
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// whiteboardSavesRecord opens the artifact in the forum chrome and waits for
// the first autosave of board 0; it returns the stored file.
func whiteboardSavesRecord(t *testing.T, source string) []byte {
	t.Helper()
	chrome := headlessChrome(t)
	env := newEnv(t, time.Minute)
	key := env.open().Key
	env.setArtifact(`<!doctype html><html><head><meta charset="utf-8"><title>wb</title></head><body>
<h2>Flow</h2><div class="mermaid">` + source + `
</div></body></html>`)

	cmd := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--hide-scrollbars",
		"--window-size=1500,800", "--user-data-dir="+t.TempDir(), env.ts.URL+"/session/"+key)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("chrome: %v", err)
	}
	defer func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() }()

	path := filepath.Join(env.home, "forums", key, "whiteboards", "0.json")
	for deadline := time.Now().Add(45 * time.Second); time.Now().Before(deadline); time.Sleep(300 * time.Millisecond) {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return data
		}
	}
	t.Fatalf("the whiteboard never saved a record (%s): the frame and the artifact did not complete the start handshake", path)
	return nil
}

// A whiteboard that cannot start must say so, in its own place, with the
// reason and the diagram text: an empty box is indistinguishable from a broken
// one. Opened without the forum chrome (the artifact URL on its own) the
// bridge to the server does not exist, and the board must explain that instead
// of staying blank.
func TestWhiteboard_RealChrome_AFrameThatCannotStartSaysWhy(t *testing.T) {
	chrome := headlessChrome(t)
	env := newEnv(t, time.Minute)
	key := env.open().Key
	env.setArtifact(`<!doctype html><html><head><meta charset="utf-8"><title>wb</title></head><body>
<div class="mermaid">flowchart LR
  A --> B
</div>
<script>setTimeout(() => {
  const slot = document.querySelector(".vxb-slot");
  fetch("/__status?v=" + encodeURIComponent(JSON.stringify({
    text: slot ? slot.textContent : "NO SLOT",
    frames: document.querySelectorAll("iframe").length,
    alert: !!(slot && slot.querySelector("[role=alert]")),
  })), { mode: "no-cors" });
}, 6000);</script></body></html>`)

	reported := make(chan string, 1)
	env.handle("/__status", func(w http.ResponseWriter, r *http.Request) {
		select {
		case reported <- r.URL.Query().Get("v"):
		default:
		}
	})

	cmd := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--window-size=1100,800",
		"--user-data-dir="+t.TempDir(), env.ts.URL+"/a/"+key+"/artifact.html")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("chrome: %v", err)
	}
	defer func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() }()

	select {
	case got := <-reported:
		var r struct {
			Text   string
			Frames int
			Alert  bool
		}
		if err := json.Unmarshal([]byte(got), &r); err != nil {
			t.Fatalf("bad report %q: %v", got, err)
		}
		if !strings.Contains(r.Text, "could not start") || !strings.Contains(r.Text, "vx forum") {
			t.Errorf("the board says %q, want the reason it could not start", r.Text)
		}
		if !strings.Contains(r.Text, "flowchart LR") {
			t.Errorf("the diagram text is not kept readable: %q", r.Text)
		}
		if !r.Alert || r.Frames != 0 {
			t.Errorf("alert=%v frames=%d: want an announced message and no empty frame", r.Alert, r.Frames)
		}
	case <-time.After(45 * time.Second):
		t.Fatal("the artifact never reported")
	}
}

// The frame page names its bundle with the build id, so a browser holding an
// earlier bundle under the old URL cannot run it against this embed script.
func TestWhiteboardFrame_BundleURLsAreVersioned(t *testing.T) {
	env := newEnv(t, time.Minute)
	_, body := env.get("/whiteboard-frame?slot=0&palette=dark")
	for _, want := range []string{"/whiteboard-assets/whiteboard.js?v=", "/whiteboard-assets/whiteboard.css?v=", "/forum-assets/forum-tokens.css"} {
		if !strings.Contains(body, want) {
			t.Errorf("frame page missing %q:\n%s", want, body)
		}
	}
}

// A locked inline board must not trap the page: over its canvas the topmost
// element is the lock cover (not the frame), so wheel and touch scrolling
// belong to the page and nothing can zoom the canvas; the header strip stays
// live, and unlocking hands the canvas over to the frame. The unlock
// affordance is a native, named button.
func TestWhiteboard_RealChrome_ALockedBoardDoesNotTrapTheWheel(t *testing.T) {
	env := newEnv(t, time.Minute)
	key := env.open().Key
	got := runLab(t, env, key, "dark", []string{"flowchart LR\n  A --> B", "flowchart LR\n  C --> D"}, `
const first = await boardReady(0);
await boardReady(1);
const slot = first.closest(".vxb-slot");
const stage = slot.querySelector(".vxb-stage");
const r = stage.getBoundingClientRect();
const hit = (x, y) => { const e = document.elementFromPoint(x, y); return e === first ? "frame" : (e ? (e.className || e.tagName) : "none"); };
const middle = [r.left + r.width / 2, r.top + r.height / 2];
const wheel = new WheelEvent("wheel", { deltaY: 240, bubbles: true, cancelable: true });
document.elementFromPoint(...middle).dispatchEvent(wheel);
const button = slot.querySelector(".vxb-unlock");
const locked = {
  middle: hit(...middle),
  header: hit(r.left + 20, r.top + 20),
  wheelPrevented: wheel.defaultPrevented,
  tabindex: first.getAttribute("tabindex"),
  buttonTag: button.tagName,
  buttonType: button.getAttribute("type"),
  buttonName: button.getAttribute("aria-label"),
  pageScrolls: document.documentElement.scrollHeight > window.innerHeight,
  otherLocked: !!document.querySelectorAll(".vxb-slot")[1].querySelector(".vxb-cover"),
};
button.focus();
const focused = document.activeElement === button;
button.click();
const unlocked = {
  middle: hit(...middle),
  tabindex: first.getAttribute("tabindex"),
  cover: !!slot.querySelector(".vxb-cover"),
  otherStillLocked: !!document.querySelectorAll(".vxb-slot")[1].querySelector(".vxb-cover"),
};
report("result", JSON.stringify({ locked, focused, unlocked }));
`, 1300, 700, 90*time.Second)
	t.Logf("%s", got)
	var r struct {
		Locked struct {
			Middle, Header, Tabindex, ButtonTag, ButtonType, ButtonName string
			WheelPrevented, PageScrolls, OtherLocked                    bool
		}
		Focused  bool
		Unlocked struct {
			Middle, Tabindex        string
			Cover, OtherStillLocked bool
		}
	}
	if err := json.Unmarshal([]byte(got), &r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Locked.Middle, "vxb-cover") {
		t.Errorf("over a locked canvas the topmost element is %q, want the lock cover", r.Locked.Middle)
	}
	if r.Locked.Header != "frame" {
		t.Errorf("the header strip of a locked board is covered (%q): its buttons must stay usable", r.Locked.Header)
	}
	if r.Locked.WheelPrevented {
		t.Error("a wheel event over a locked board was cancelled: the page cannot scroll")
	}
	if r.Locked.Tabindex != "-1" {
		t.Errorf("a locked frame is in the tab order (tabindex %q)", r.Locked.Tabindex)
	}
	if r.Locked.ButtonTag != "BUTTON" || r.Locked.ButtonType != "button" || !strings.Contains(r.Locked.ButtonName, "diagram 1 of 2") {
		t.Errorf("the unlock affordance is %s type=%q name=%q, want a named native button", r.Locked.ButtonTag, r.Locked.ButtonType, r.Locked.ButtonName)
	}
	if !r.Locked.PageScrolls || !r.Locked.OtherLocked || !r.Focused {
		t.Errorf("fixture sanity: %+v focused=%v", r.Locked, r.Focused)
	}
	if r.Unlocked.Middle != "frame" || r.Unlocked.Cover || r.Unlocked.Tabindex != "" {
		t.Errorf("after unlocking: %+v, want the frame under the pointer and no cover", r.Unlocked)
	}
	if !r.Unlocked.OtherStillLocked {
		t.Error("unlocking one board unlocked another")
	}
}

// A board that fell back to an image says so; a board of editable shapes does
// not carry the label.
func TestWhiteboard_RealChrome_ImageBoardsAreLabeled(t *testing.T) {
	env := newEnv(t, time.Minute)
	key := env.open().Key
	got := runLab(t, env, key, "light", []string{"flowchart LR\n  A --> B", "pie title Pets\n  \"Dogs\" : 3\n  \"Cats\" : 2"}, `
const shapes = await boardReady(0);
const image = await boardReady(1);
const badge = (f) => { const b = f.contentDocument.querySelector(".vxb-badge"); return { shown: !b.hidden && b.getBoundingClientRect().width > 0, text: b.textContent }; };
report("result", JSON.stringify({ shapes: badge(shapes), image: badge(image) }));
`, 1300, 900, 120*time.Second)
	t.Logf("%s", got)
	var r struct {
		Shapes, Image struct {
			Shown bool
			Text  string
		}
	}
	if err := json.Unmarshal([]byte(got), &r); err != nil {
		t.Fatal(err)
	}
	if r.Shapes.Shown {
		t.Error("a board of editable shapes is labeled as an image")
	}
	if !r.Image.Shown || !strings.Contains(strings.ToLower(r.Image.Text), "image") {
		t.Errorf("the pie board is not visibly labeled as an image: %+v", r.Image)
	}
}

// The chrome's artifact bridge answers exactly the four board operations and
// refuses everything else, including the ones that used to exist and any
// ordinal outside 0..999. Driven from an artifact inside the real chrome.
func TestWhiteboard_RealChrome_ArtifactBridgeServesOnlyTheBoardOperations(t *testing.T) {
	chrome := headlessChrome(t)
	env := newEnv(t, time.Minute)
	key := env.open().Key
	env.setArtifact(`<!doctype html><html><head><meta charset="utf-8"><title>bridge</title></head><body><p>no diagrams here</p>
<div id="m" style="display:none"></div>
<script>
const report = (k, v) => fetch("/__report?k=" + k + "&v=" + encodeURIComponent(v), { mode: "no-cors" });
const outcome = async (op, payload) => {
  try { return { ok: await window.forum.__rpc(op, payload) }; }
  catch (e) { return { error: String((e && e.message) || e) }; }
};
(async () => {
  const record = { format: 2, digest: "d", measure_gen: 1, current: { elements: [{ id: "a" }], appState: { theme: "dark", scrollX: 3 }, files: {} }, pristine: { elements: [] } };
  const res = {};
  res.list = await outcome("board.list", {});
  res.readEmpty = await outcome("board.read", { ordinal: 0 });
  res.write = await outcome("board.write", { ordinal: 0, body: record });
  res.readBack = await outcome("board.read", { ordinal: 0 });
  res.badOrdinal = await outcome("board.read", { ordinal: 1000 });
  res.negative = await outcome("board.write", { ordinal: -1, body: record });
  res.fraction = await outcome("board.read", { ordinal: 1.5 });
  res.submitNoScene = await outcome("board.submit", { ordinal: 0, body: { current: record.current, edit_lines: ["x"], remark: "r" } });
  res.old1 = await outcome("whiteboard.load", { index: 0 });
  res.old2 = await outcome("whiteboard.sources", {});
  res.old3 = await outcome("whiteboard.save", { index: 0, body: record });
  res.old4 = await outcome("whiteboard.feedback", { index: 0, body: {} });
  res.unknown = await outcome("board.delete", { ordinal: 0 });
  report("bridge", JSON.stringify(res));
})();
</script></body></html>`)
	reports := captureReports(t, env)
	got := reportFromChrome(t, chrome, env.ts.URL+"/session/"+key, reports, "bridge")
	var r map[string]struct {
		Ok    json.RawMessage `json:"ok"`
		Error string          `json:"error"`
	}
	if err := json.Unmarshal([]byte(got), &r); err != nil {
		t.Fatalf("bad report %q: %v", got, err)
	}
	if !strings.Contains(string(r["list"].Ok), `"diagrams"`) {
		t.Errorf("board.list = %+v", r["list"])
	}
	if string(r["readEmpty"].Ok) != `{"record":null}` {
		t.Errorf("board.read before any save = %+v", r["readEmpty"])
	}
	if string(r["write"].Ok) != `{}` {
		t.Errorf("board.write = %+v, want {}", r["write"])
	}
	if !strings.Contains(string(r["readBack"].Ok), `"digest":"d"`) || strings.Contains(string(r["readBack"].Ok), `"theme"`) {
		t.Errorf("board.read after a save = %s (the theme field must have been stripped)", r["readBack"].Ok)
	}
	for _, name := range []string{"badOrdinal", "negative", "fraction"} {
		if !strings.Contains(r[name].Error, "invalid board ordinal") {
			t.Errorf("%s = %+v, want invalid board ordinal", name, r[name])
		}
	}
	// The artifact in this fixture has no Mermaid block, so a submit still
	// works: it only needs an open session.
	if r["submitNoScene"].Error != "" {
		t.Errorf("board.submit = %+v", r["submitNoScene"])
	}
	for _, name := range []string{"old1", "old2", "old3", "old4", "unknown"} {
		if !strings.Contains(r[name].Error, "unsupported operation") {
			t.Errorf("%s = %+v, want unsupported operation", name, r[name])
		}
	}
}

// Every control of a board has an accessible name, in the locked state, in the
// frame's header and in the fullscreen overlay, and every iframe has a title
// that tells the boards apart. (The name is computed the simple way: label,
// text, title or placeholder; that is what the page authors for.)
func TestWhiteboard_RealChrome_EveryControlHasAnAccessibleName(t *testing.T) {
	env := newEnv(t, time.Minute)
	key := env.open().Key
	got := runLab(t, env, key, "dark", []string{"flowchart LR\n  A --> B", "flowchart LR\n  C --> D"}, `
const inline = await boardReady(0);
await boardReady(1);
const nameOf = (el) => (el.getAttribute("aria-label") || el.textContent || el.getAttribute("title") || el.getAttribute("placeholder") || "").trim();
const unnamed = [];
const audit = (root, where) => root.querySelectorAll("button, input, iframe, [role=dialog], [role=button]").forEach((el) => {
  const name = el.tagName === "IFRAME" ? (el.getAttribute("title") || "") : nameOf(el);
  if (!name) unnamed.push(where + ": " + el.tagName + "." + el.className);
});
// Only the controls the whiteboard owns: its slots, its frames' header, its overlay.
document.querySelectorAll(".vxb-slot").forEach((slot, i) => audit(slot, "slot " + i));
audit(inline.contentDocument.querySelector(".vxb-bar"), "frame header");
inline.contentDocument.querySelector(".vxb-tools .vxb-btn:last-child").click();
const overlay = await until(() => document.getElementById("vxb-overlay"), 15000, "the overlay");
const full = await boardReady(overlayFrame);
audit(overlay, "overlay");
audit(full.contentDocument.querySelector(".vxb-bar"), "fullscreen header");
const titles = [...document.querySelectorAll("iframe")].map((f) => f.getAttribute("title"));
report("result", JSON.stringify({ unnamed, titles, overlayName: overlay.getAttribute("aria-label") }));
`, 1300, 900, 90*time.Second)
	t.Logf("%s", got)
	var r struct {
		Unnamed []string
		Titles  []string
	}
	if err := json.Unmarshal([]byte(got), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Unnamed) != 0 {
		t.Errorf("controls without an accessible name: %v", r.Unnamed)
	}
	seen := map[string]bool{}
	for _, title := range r.Titles {
		if title == "" || seen[title] {
			t.Errorf("iframe titles must be present and tell the boards apart: %v", r.Titles)
			break
		}
		seen[title] = true
	}
}
