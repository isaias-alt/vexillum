//go:build unix

package forum_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
)

// Reopening a board with a stored record, in real Chrome on the real frame:
// the four rows of the opening decision, driven by seeding the record through
// the server before the page loads.

const reopenDiagram = "flowchart LR\n  A --> B"

func rect(id string, x float64, link string) map[string]any {
	el := map[string]any{
		"id": id, "type": "rectangle", "x": x, "y": 150.0, "width": 160.0, "height": 80.0,
		"angle": 0, "strokeColor": "#1e1e1e", "backgroundColor": "transparent", "fillStyle": "solid",
		"strokeWidth": 2, "strokeStyle": "solid", "roughness": 1, "opacity": 100, "groupIds": []string{},
		"frameId": nil, "roundness": nil, "seed": 1, "version": 1, "versionNonce": 1, "isDeleted": false,
		"boundElements": nil, "updated": 1, "link": nil, "locked": false, "index": "a0",
	}
	if link != "" {
		el["link"] = link
	}
	return el
}

// seedBoard stores a record for board 0 through the public route.
func seedBoard(t *testing.T, env *testEnv, key, digest string, current, pristine []map[string]any) {
	t.Helper()
	record := map[string]any{
		"format": 2, "digest": digest, "measure_gen": 99,
		"current":  map[string]any{"elements": current, "appState": map[string]any{}, "files": map[string]any{}},
		"pristine": map[string]any{"elements": pristine},
	}
	if resp, body := env.browser("PUT", "/api/s/"+key+"/boards/0", key, record); resp.StatusCode != 200 {
		t.Fatalf("seeding the record = %d %s", resp.StatusCode, body)
	}
}

func currentDigest(t *testing.T, key string) string {
	t.Helper()
	sources := forum.ExtractMermaidSources(labPage(key, "", "dark", []string{reopenDiagram}, ""))
	if len(sources) != 1 {
		t.Fatalf("expected one diagram, got %d", len(sources))
	}
	return sources[0].Hash
}

// The page reads the stored record back through the bridge once the board is
// ready (and, for the rebuild cases, after its first autosave).
const reopenScript = `
const frame = await boardReady(0);
const choice = frame.contentDocument.querySelector(".vxb-choice");
const banner = frame.contentDocument.querySelector(".vxb-banner");
const stored = await window.forum.__rpc("board.read", { ordinal: 0 });
report("result", JSON.stringify({
  choice: !!choice, banner: banner && !banner.hidden ? banner.textContent : "",
  digest: stored.record && stored.record.digest,
  ids: stored.record ? stored.record.current.elements.map((e) => e.id) : [],
}));
`

type reopenResult struct {
	Choice bool
	Banner string
	Digest string
	IDs    []string
}

func runReopen(t *testing.T, digestOf func(key string) string, current, pristine []map[string]any, script string) reopenResult {
	t.Helper()
	env := newEnv(t, time.Minute)
	key := env.open().Key
	seedBoard(t, env, key, digestOf(key), current, pristine)
	got := runLab(t, env, key, "dark", []string{reopenDiagram}, script, 1300, 900, 90*time.Second)
	t.Logf("%s", got)
	var r reopenResult
	if err := json.Unmarshal([]byte(got), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestWhiteboard_RealChrome_ReopenWithTheSameDiagramOpensTheSavedScene(t *testing.T) {
	r := runReopen(t, func(key string) string { return currentDigest(t, key) },
		[]map[string]any{rect("mine", 300, "")}, []map[string]any{rect("mine", 100, "")}, reopenScript)
	if r.Choice || r.Banner != "" {
		t.Errorf("a matching digest should just open the saved scene: %+v", r)
	}
	if len(r.IDs) != 1 || r.IDs[0] != "mine" {
		t.Errorf("the saved scene was replaced: %v", r.IDs)
	}
}

func TestWhiteboard_RealChrome_ReopenAfterATextChangeWithoutEditsRebuildsSilently(t *testing.T) {
	same := []map[string]any{rect("old", 100, "")}
	r := runReopen(t, func(string) string { return "an-older-digest" }, same, same, `
const frame = await boardReady(0);
const choice = !!frame.contentDocument.querySelector(".vxb-choice");
// The rebuilt scene autosaves under the new digest.
const stored = await until(async () => { const s = await window.forum.__rpc("board.read", { ordinal: 0 }); return s.record && s.record.digest !== "an-older-digest" ? s : null; }, 20000, "the rebuilt scene to be saved");
report("result", JSON.stringify({ choice, banner: "", digest: stored.record.digest, ids: stored.record.current.elements.map((e) => e.id) }));
`)
	if r.Choice {
		t.Error("the reviewer was asked although nothing had been edited")
	}
	if len(r.IDs) == 0 || contains(r.IDs, "old") {
		t.Errorf("the scene was not rebuilt from the diagram text: %v", r.IDs)
	}
}

func TestWhiteboard_RealChrome_ReopenAfterATextChangeWithEditsAsksAndBothChoicesWork(t *testing.T) {
	edited := []map[string]any{rect("mine", 300, "")}
	untouched := []map[string]any{rect("mine", 100, "")}
	askScript := func(pick int) string {
		settle := "await window.forum.__rpc('board.read', { ordinal: 0 }).then((s) => s.record)"
		if pick == 0 {
			settle = `await until(async () => { const s = await window.forum.__rpc("board.read", { ordinal: 0 }); return s.record && s.record.digest !== "an-older-digest" ? s.record : null; }, 20000, "the rebuilt scene to be saved")`
		}
		return strings.NewReplacer("__PICK__", strconv.Itoa(pick), "__SETTLE__", settle).Replace(askTemplate)
	}
	type asked struct {
		reopenResult
		Buttons      []string
		FocusedFirst bool
		Named        bool
	}
	run := func(pick int) asked {
		env := newEnv(t, time.Minute)
		key := env.open().Key
		seedBoard(t, env, key, "an-older-digest", edited, untouched)
		got := runLab(t, env, key, "dark", []string{reopenDiagram}, askScript(pick), 1300, 900, 90*time.Second)
		t.Logf("%s", got)
		var a asked
		if err := json.Unmarshal([]byte(got), &a); err != nil {
			t.Fatal(err)
		}
		return a
	}

	discard := run(0)
	if len(discard.Buttons) != 2 || !discard.FocusedFirst || !discard.Named {
		t.Errorf("the choice needs two named buttons with focus on the first: %+v", discard)
	}
	if contains(discard.IDs, "mine") || discard.Digest == "an-older-digest" || discard.Banner != "" {
		t.Errorf("discarding should start over from the new diagram: %+v", discard)
	}

	keep := run(1)
	if !contains(keep.IDs, "mine") || keep.Digest != "an-older-digest" {
		t.Errorf("keeping should continue with the saved scene: %+v", keep)
	}
	if !strings.Contains(strings.ToLower(keep.Banner), "predates") {
		t.Errorf("keeping leaves no persistent notice that the scene predates the diagram: %q", keep.Banner)
	}
}

// The reviewer's choice: wait for it, check what it offers, press the button
// at index __PICK__, then read back what ended up stored.
const askTemplate = `
const d = await until(() => { const f = boardFrame(0); const doc = f && f.contentDocument; return doc && doc.querySelector(".vxb-choice") ? doc : null; }, 60000, "the choice");
const group = d.querySelector(".vxb-choice");
const buttons = [...group.querySelectorAll("button")];
const names = buttons.map((b) => b.textContent);
const focusedFirst = d.activeElement === buttons[0];
const label = d.getElementById(group.getAttribute("aria-labelledby"));
buttons[__PICK__].click();
const frame = await boardReady(0);
const banner = frame.contentDocument.querySelector(".vxb-banner");
const record = __SETTLE__;
report("result", JSON.stringify({
  choice: true, buttons: names, focusedFirst, named: !!label && label.textContent.length > 0,
  banner: banner && !banner.hidden ? banner.textContent : "", digest: record.digest, ids: record.current.elements.map((e) => e.id),
}));
`

// Links inside a scene, in real Chrome: an http(s) link asks first, in a modal
// dialog that shows the whole address, starts on Cancel, is cancelled by
// Escape and returns focus; a javascript: link is blocked with a visible
// message and never offers to open.
func TestWhiteboard_RealChrome_LinksAskFirstAndUnsafeOnesAreBlocked(t *testing.T) {
	env := newEnv(t, time.Minute)
	key := env.open().Key
	const safe = "https://example.com/path?x=1#frag"
	elements := []map[string]any{rect("one", 200, safe), rect("two", 520, "javascript:alert(1)")}
	seedBoard(t, env, key, currentDigest(t, key), elements, elements)
	got := runLab(t, env, key, "dark", []string{reopenDiagram}, `
const frame = await boardReady(0);
const d = frame.contentDocument, w = frame.contentWindow;
await sleep(500);
const canvas = d.querySelector("canvas.interactive");
canvas.setPointerCapture = () => {}; canvas.releasePointerCapture = () => {}; canvas.hasPointerCapture = () => false;
const r = canvas.getBoundingClientRect();
const fire = (type, x, y) => canvas.dispatchEvent(new w.PointerEvent(type, { bubbles: true, cancelable: true, clientX: r.left + x, clientY: r.top + y, pointerId: 1, pointerType: "mouse", isPrimary: true, button: 0, buttons: type === "pointerup" ? 0 : 1 }));
// The shapes have a transparent fill, so a click has to land on the outline.
const select = async (x) => { fire("pointermove", x, 190); fire("pointerdown", x, 190); fire("pointerup", x, 190); await sleep(400); };
const anchor = () => until(() => d.querySelector("a.excalidraw-hyperlinkContainer-link"), 5000, "the link control");

await select(200);
const link = await anchor();
const opener = d.activeElement;
link.click();
const dialog = await until(() => d.querySelector(".vxb-dialog"), 5000, "the confirmation");
const labelled = d.getElementById(dialog.getAttribute("aria-labelledby"));
const first = {
  role: dialog.getAttribute("role"), modal: dialog.getAttribute("aria-modal"), named: !!labelled && labelled.textContent.length > 0,
  showsUrl: dialog.textContent.includes("https://example.com/path?x=1#frag"),
  focused: d.activeElement && d.activeElement.textContent,
};
d.querySelector(".vxb-scrim").dispatchEvent(new w.KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
await sleep(200);
first.closed = !d.querySelector(".vxb-dialog");
first.focusBack = d.activeElement === opener;

// Unselect, then the unsafe one.
fire("pointerdown", 1000, 400); fire("pointerup", 1000, 400); await sleep(300);
await select(520);
const unsafe = await anchor();
unsafe.click();
await sleep(500);
const blocked = { dialog: !!d.querySelector(".vxb-dialog"), status: d.querySelector(".vxb-status").textContent };
report("result", JSON.stringify({ first, blocked }));
`, 1300, 900, 90*time.Second)
	t.Logf("%s", got)
	var r struct {
		First struct {
			Role, Modal, Focused               string
			Named, ShowsURL, Closed, FocusBack bool
		}
		Blocked struct {
			Dialog bool
			Status string
		}
	}
	if err := json.Unmarshal([]byte(got), &r); err != nil {
		t.Fatal(err)
	}
	f := r.First
	if f.Role != "dialog" || f.Modal != "true" || !f.Named || !f.ShowsURL {
		t.Errorf("the confirmation is not a named modal showing the address: %+v", f)
	}
	if f.Focused != "Cancel" {
		t.Errorf("focus starts on %q, want the safe action (Cancel)", f.Focused)
	}
	if !f.Closed || !f.FocusBack {
		t.Errorf("Escape should cancel and return focus: %+v", f)
	}
	if r.Blocked.Dialog || !strings.Contains(r.Blocked.Status, "Blocked") {
		t.Errorf("a javascript: link must be blocked with a visible message and no dialog: %+v", r.Blocked)
	}
}
