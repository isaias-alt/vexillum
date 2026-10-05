import assert from "node:assert/strict";
import test from "node:test";

import {
  ANGLE_TOLERANCE,
  MEASURE_GEN,
  OPEN_ASK,
  OPEN_CONVERT,
  OPEN_REOPEN,
  POSITION_TOLERANCE,
  RECORD_FORMAT,
  EDIT_SUMMARY_LINE_LIMIT,
  EDIT_SUMMARY_LINE_WIDTH,
  breakTagsToNewlines,
  buildRecord,
  cloneScene,
  compareScenes,
  decideOpening,
  findRepeatedIds,
  fitNodesToLabels,
  hasEdits,
  inscribedSizeOf,
  isImageBoard,
  outerSizeFor,
  readRecord,
  recordNeedsMeasuring,
  safeLinkTarget,
  scrubThemeFields,
  summarizeEdits,
  uniquifyIds,
} from "../src/whiteboard-core.js";

// ------------------------------------------------------------ fixtures

// A monospace "font": 10px per character, 20px per line.
const measure = ({ text }) => {
  const lines = String(text).split("\n");
  return { width: Math.max(...lines.map((l) => l.length)) * 10, height: lines.length * 20 };
};

let seq = 0;
const shape = (id, label, over = {}) => ({
  id, type: "rectangle", x: 0, y: 0, width: 100, height: 50, angle: 0, version: 1, isDeleted: false,
  boundElements: label === null ? [] : [{ id: `${id}-t`, type: "text" }], ...over,
});
const labelFor = (id, text, over = {}) => ({
  id: `${id}-t`, type: "text", containerId: id, text, originalText: text, x: 0, y: 0, width: 10, height: 20,
  fontSize: 20, fontFamily: 1, lineHeight: 1.25, textAlign: "center", verticalAlign: "middle", version: 1, isDeleted: false, ...over,
});
const node = (id, text, over = {}) => [shape(id, text, over), labelFor(id, text)];
const arrow = (id, from, to, over = {}) => ({
  id, type: "arrow", x: 100, y: 25, width: 100, height: 0, points: [[0, 0], [100, 0]], angle: 0, version: 1, isDeleted: false,
  startBinding: from ? { elementId: from, focus: 0, gap: 1 } : null,
  endBinding: to ? { elementId: to, focus: 0, gap: 1 } : null,
  startArrowhead: null, endArrowhead: "arrow", boundElements: [], ...over,
});
const text = (id, value, over = {}) => ({
  id, type: "text", containerId: null, text: value, originalText: value, x: 0, y: 0, width: 50, height: 20, angle: 0, version: 1, isDeleted: false, ...over,
});
const copy = (x) => JSON.parse(JSON.stringify(x));
const byId = (els, id) => els.find((e) => e.id === id);

// -------------------------------------------------- 4.1 after conversion

test("break tags in any case, spacing and closing style become newlines in labels", () => {
  const input = [
    { id: "a", type: "rectangle", label: { text: "one<br>two<BR/>three< br />four<Br  / >five" } },
    { id: "b", type: "text", text: "x<br/>y" },
    { id: "c", type: "arrow", label: { text: "yes<br />no" } },
    { id: "d", type: "rectangle" },
  ];
  const out = breakTagsToNewlines(input);
  assert.equal(out[0].label.text, "one\ntwo\nthree\nfour\nfive");
  assert.equal(out[1].text, "x\ny");
  assert.equal(out[2].label.text, "yes\nno");
  assert.equal(out[3].label, undefined);
  assert.equal(input[0].label.text.includes("<br>"), true, "the input is not mutated");
});

test("a rectangle grows to hold its label plus padding, around its own centre", () => {
  const [box, label] = node("n", "hello world!!"); // 13 chars -> 130 x 20
  const out = fitNodesToLabels([box, label], measure);
  const grown = byId(out, "n");
  assert.ok(grown.width >= 130 + 2 * 16, `width ${grown.width}`);
  assert.ok(grown.height >= 20 + 2 * 12);
  assert.equal(grown.x + grown.width / 2, 50, "centre x unchanged");
  assert.equal(grown.y + grown.height / 2, 25, "centre y unchanged");
});

test("a shape that is already big enough is left alone, and refitting is idempotent", () => {
  const els = node("n", "hi", { width: 200, height: 100 });
  const once = fitNodesToLabels(els, measure);
  assert.equal(byId(once, "n").width, 200);
  const twice = fitNodesToLabels(once, measure);
  assert.deepEqual(twice, once);
  assert.equal(byId(twice, "n").version, byId(once, "n").version, "no version churn on a second pass");
});

test("ellipse and diamond are sized from the inscribed rectangle", () => {
  assert.deepEqual(inscribedSizeOf("rectangle", 100, 50), { width: 100, height: 50 });
  const e = inscribedSizeOf("ellipse", 200, 100);
  assert.ok(Math.abs(e.width - 141.42) < 0.01 && Math.abs(e.height - 70.71) < 0.01);
  assert.deepEqual(inscribedSizeOf("diamond", 200, 100), { width: 100, height: 50 });
  const back = outerSizeFor("ellipse", e.width, e.height);
  assert.ok(Math.abs(back.width - 200) < 1e-9);
  assert.deepEqual(outerSizeFor("diamond", 100, 50), { width: 200, height: 100 });

  for (const type of ["ellipse", "diamond"]) {
    const out = fitNodesToLabels(node("n", "twelve chars", { type, width: 10, height: 10 }), measure);
    const g = byId(out, "n");
    const inner = inscribedSizeOf(type, g.width, g.height);
    assert.ok(inner.width >= 120 + 32 - 1 && inner.height >= 20 + 24 - 1, `${type} inner ${JSON.stringify(inner)}`);
  }
});

test("the label is centred in the node for every alignment combination", () => {
  for (const textAlign of ["left", "center", "right"]) {
    for (const verticalAlign of ["top", "middle", "bottom"]) {
      const box = shape("n", "x", { x: 40, y: 60, width: 300, height: 120 });
      const label = labelFor("n", "abc", { textAlign, verticalAlign, x: 999, y: 999 });
      const out = fitNodesToLabels([box, label], measure);
      const b = byId(out, "n");
      const t = byId(out, "n-t");
      assert.equal(t.x + t.width / 2, b.x + b.width / 2, `${textAlign}/${verticalAlign} x`);
      assert.equal(t.y + t.height / 2, b.y + b.height / 2, `${textAlign}/${verticalAlign} y`);
    }
  }
});

test("wrapped label text is restored to its explicit line breaks before sizing", () => {
  const box = shape("n", "x", { width: 40, height: 40 });
  const label = labelFor("n", "aaaa\nbbbbbbbb", { text: "aaaa\nbbbb\nbbbb" });
  const t = byId(fitNodesToLabels([box, label], measure), "n-t");
  assert.equal(t.text, "aaaa\nbbbbbbbb");
  assert.equal(t.height, 40);
});

test("attached arrow ends follow a grown node; unattached arrows are untouched", () => {
  const els = [
    ...node("a", "a-very-long-label-here", { x: 0, y: 0, width: 100, height: 50 }),
    ...node("b", "b", { x: 300, y: 0, width: 100, height: 50 }),
    arrow("attached", "a", "b", { x: 100, y: 25, points: [[0, 0], [200, 0]], width: 200 }),
    arrow("free", null, null, { x: 100, y: 200, points: [[0, 0], [50, 0]], width: 50 }),
  ];
  const out = fitNodesToLabels(els, measure);
  const a = byId(out, "a");
  assert.ok(a.width > 100);
  const arr = byId(out, "attached");
  assert.equal(arr.x, a.x + a.width, "start sits on the grown node's right edge");
  assert.equal(arr.x + arr.points[1][0], 300, "the end at the untouched node stays put");
  assert.equal(arr.points[0][0], 0);
  assert.equal(byId(out, "free"), els.find((e) => e.id === "free"), "unattached arrow keeps identity");
});

test("an arrow attached at both ends to grown nodes keeps both ends on the outlines", () => {
  const els = [
    ...node("a", "aaaaaaaaaaaaaaaa", { x: 0, y: 0, width: 100, height: 50 }),
    ...node("b", "bbbbbbbbbbbbbbbb", { x: 300, y: 0, width: 100, height: 50 }),
    arrow("e", "a", "b", { x: 100, y: 25, points: [[0, 0], [200, 0]], width: 200 }),
  ];
  const out = fitNodesToLabels(els, measure);
  const a = byId(out, "a"), b = byId(out, "b"), e = byId(out, "e");
  assert.equal(e.x, a.x + a.width);
  assert.equal(e.x + e.points[1][0], b.x);
  assert.equal(e.width, b.x - (a.x + a.width));
});

test("duplicate ids are reported among live elements and made unique", () => {
  const els = [{ id: "e" }, { id: "e" }, { id: "e", isDeleted: true }, { id: "f" }, { id: "f" }, { id: "g" }];
  assert.deepEqual(findRepeatedIds(els), ["e", "f"]);
  const fixed = uniquifyIds(els);
  assert.deepEqual(findRepeatedIds(fixed), []);
  assert.equal(new Set(fixed.filter((e) => !e.isDeleted).map((e) => e.id)).size, 5, "all five live ids distinct");
  assert.equal(fixed[0].id, "e", "the first holder keeps its id");
  assert.equal(els[1].id, "e", "the input is not mutated");
  const clash = uniquifyIds([{ id: "x" }, { id: "x" }, { id: "x~2" }]);
  assert.equal(new Set(clash.map((e) => e.id)).size, 3);
});

test("a scene of only images is an image board; shapes or emptiness are not", () => {
  assert.equal(isImageBoard([{ type: "image" }]), true);
  assert.equal(isImageBoard([{ type: "image" }, { type: "image", isDeleted: true }, { type: "rectangle", isDeleted: true }]), true);
  assert.equal(isImageBoard([{ type: "image" }, { type: "rectangle" }]), false);
  assert.equal(isImageBoard([]), false);
});

test("an old record (stale measurement scheme) is flagged for re-measuring, a current one is not", () => {
  assert.equal(recordNeedsMeasuring({ measure_gen: MEASURE_GEN - 1 }), true);
  assert.equal(recordNeedsMeasuring({}), true);
  assert.equal(recordNeedsMeasuring({ measure_gen: MEASURE_GEN }), false);
  assert.equal(recordNeedsMeasuring({ measure_gen: MEASURE_GEN + 1 }), false);
  assert.equal(recordNeedsMeasuring(null), false);
});

// ------------------------------------------------ 4.2 stored and sent

test("theme fields never survive in a scrubbed scene, other state does", () => {
  const scene = { elements: [], appState: { theme: "dark", viewBackgroundColor: "#000", scrollX: 3, zoom: { value: 2 } }, files: {} };
  const out = scrubThemeFields(scene);
  assert.deepEqual(out.appState, { scrollX: 3, zoom: { value: 2 } });
  assert.equal(scene.appState.theme, "dark", "the input is untouched");
});

test("buildRecord assembles format, digest, scheme, scrubbed current and the reference; deleted elements are dropped", () => {
  const ref = [...node("a", "A")];
  const scene = { elements: [...ref, { id: "gone", type: "rectangle", isDeleted: true }], appState: { theme: "light", scrollX: 1 }, files: { f: { id: "f" } } };
  const rec = buildRecord({ scene, referenceElements: ref, digest: "abc123" });
  assert.equal(rec.format, RECORD_FORMAT);
  assert.equal(rec.digest, "abc123");
  assert.equal(rec.measure_gen, MEASURE_GEN);
  assert.equal(rec.current.elements.length, 2);
  assert.deepEqual(rec.current.appState, { scrollX: 1 });
  assert.deepEqual(rec.current.files, { f: { id: "f" } });
  assert.equal(rec.pristine.elements.length, 2);
  assert.equal(rec.saved_at, undefined, "the server sets saved_at");
  assert.notEqual(rec.current.elements[0], scene.elements[0], "deep copy");
  assert.equal(buildRecord({ scene, referenceElements: ref, digest: "d", measureGen: 7 }).measure_gen, 7);
  assert.equal(buildRecord({ scene, digest: "d" }).pristine, null, "no reference stays absent");
});

test("cloneScene is a JSON round trip", () => {
  const a = { x: [1, { y: 2 }], u: undefined };
  const b = cloneScene(a);
  assert.deepEqual(b, { x: [1, { y: 2 }] });
  b.x[1].y = 9;
  assert.equal(a.x[1].y, 2);
});

test("readRecord accepts only the current format and shape", () => {
  const good = { format: 2, digest: "d", measure_gen: 1, saved_at: "t", current: { elements: [] }, pristine: { elements: [] } };
  assert.ok(readRecord(good));
  assert.equal(readRecord({ ...good, format: 1 }), null);
  assert.equal(readRecord({ ...good, format: undefined }), null);
  assert.equal(readRecord({ source_hash: "x", scene: {} }), null);
  assert.equal(readRecord(null), null);
  assert.equal(readRecord({ ...good, current: null }), null);
  assert.equal(readRecord({ ...good, pristine: null }).pristine, null);
});

// ---- edit detection

test("without edits the scenes compare equal, and cosmetic or bookkeeping fields do not count", () => {
  const ref = [...node("a", "A"), arrow("e", "a", null)];
  const cur = copy(ref);
  for (const el of cur) {
    Object.assign(el, { strokeColor: "#f00", backgroundColor: "#0f0", fillStyle: "solid", roughness: 2, opacity: 50, seed: 9, version: 77, versionNonce: 5, index: "a9", updated: 123, fontSize: 36, strokeStyle: "dashed", strokeWidth: 4 });
  }
  assert.equal(hasEdits(ref, cur), false);
  assert.deepEqual(compareScenes(ref, cur), []);
});

test("deleted elements are invisible to detection", () => {
  const ref = [...node("a", "A")];
  const cur = [...copy(ref), { ...shape("x", null), isDeleted: true }];
  assert.equal(hasEdits(ref, cur), false);
  const removedByReviewer = copy(ref).map((e) => ({ ...e, isDeleted: true }));
  assert.equal(hasEdits(ref, removedByReviewer), true);
  const refWithDeleted = [...copy(ref), { ...shape("y", null), isDeleted: true }];
  assert.equal(hasEdits(refWithDeleted, ref), false);
});

test("position and size tolerance: exactly the tolerance is noise, beyond it is an edit", () => {
  const ref = node("a", "A", { x: 10, y: 10, width: 100, height: 50 });
  const nudge = (patch) => ref.map((e) => (e.id === "a" ? { ...e, ...patch } : e));
  const T = POSITION_TOLERANCE;
  assert.equal(hasEdits(ref, nudge({ x: 10 + T })), false);
  assert.equal(hasEdits(ref, nudge({ x: 10 - T })), false);
  assert.equal(hasEdits(ref, nudge({ x: 10 + T + 0.1 })), true);
  assert.equal(hasEdits(ref, nudge({ y: 10 - T - 0.1 })), true);
  assert.equal(hasEdits(ref, nudge({ width: 100 + T })), false);
  assert.equal(hasEdits(ref, nudge({ width: 100 + T + 0.1 })), true);
  assert.equal(hasEdits(ref, nudge({ height: 50 - T - 0.1 })), true);
  assert.equal(hasEdits(ref, nudge({ angle: ANGLE_TOLERANCE })), false);
  assert.equal(hasEdits(ref, nudge({ angle: ANGLE_TOLERANCE + 0.01 })), true);
});

test("content, identity and connectivity edits count", () => {
  const ref = [...node("a", "A"), ...node("b", "B"), arrow("e", "a", "b")];
  const edit = (fn) => { const c = copy(ref); fn(c); return c; };
  assert.equal(hasEdits(ref, edit((c) => { byId(c, "a-t").originalText = "A2"; byId(c, "a-t").text = "A2"; })), true, "label text");
  assert.equal(hasEdits(ref, edit((c) => c.push(text("t", "note")))), true, "added element");
  assert.equal(hasEdits(ref, edit((c) => c.splice(0, 2))), true, "removed element");
  assert.equal(hasEdits(ref, edit((c) => { byId(c, "e").endBinding = null; })), true, "detached end");
  assert.equal(hasEdits(ref, edit((c) => { byId(c, "e").endBinding.elementId = "a"; })), true, "reconnected end");
  assert.equal(hasEdits(ref, edit((c) => { byId(c, "e").startArrowhead = "arrow"; })), true, "arrowhead");
  assert.equal(hasEdits(ref, edit((c) => { byId(c, "a").link = "https://x.test"; })), true, "link");
  assert.equal(hasEdits(ref, edit((c) => { byId(c, "a").type = "ellipse"; })), true, "type");
});

test("a record without a reference counts as edited exactly when it has a scene", () => {
  assert.equal(hasEdits(undefined, node("a", "A")), true);
  assert.equal(hasEdits(null, []), false);
  assert.equal(hasEdits(null, [{ ...shape("x", null), isDeleted: true }]), false);
});

// ---- edit summary

test("no change is signalled with a single clear line", () => {
  const ref = node("a", "A");
  assert.deepEqual(summarizeEdits(ref, copy(ref)).length, 1);
  assert.match(summarizeEdits(ref, copy(ref))[0], /^no content changes/);
});

test("a new labeled node is one line, not two", () => {
  const ref = node("a", "A");
  const cur = [...copy(ref), ...node("n", "Cache", { x: 300, y: 40 })];
  const lines = summarizeEdits(ref, cur);
  assert.equal(lines.length, 1);
  assert.match(lines[0], /^added rectangle "Cache" \[id n\] at \(300,40\)/);
});

test("a renamed bound label is one line on its shape", () => {
  const ref = node("a", "Old");
  const cur = copy(ref);
  byId(cur, "a-t").originalText = "New";
  byId(cur, "a-t").text = "New";
  const lines = summarizeEdits(ref, cur);
  assert.equal(lines.length, 1);
  assert.match(lines[0], /relabeled rectangle \[id a\]: "Old" -> "New"/);
});

test("an added arrow with both ends attached is one line naming both ends", () => {
  const ref = [...node("a", "Src"), ...node("b", "Dst")];
  const cur = [...copy(ref), arrow("e", "a", "b")];
  const lines = summarizeEdits(ref, cur);
  assert.equal(lines.length, 1);
  assert.match(lines[0], /^added arrow from "Src" to "Dst" \[id e\]/);
});

test("moves, resizes, removals and reconnections are described concretely", () => {
  const ref = [...node("a", "A"), ...node("b", "B"), ...node("c", "C"), arrow("e", "a", "b")];
  const cur = copy(ref);
  byId(cur, "a").x += 40;
  byId(cur, "a").y -= 10;
  byId(cur, "b").width = 180;
  cur.splice(cur.findIndex((x) => x.id === "c"), 2);
  byId(cur, "e").endBinding = null;
  const lines = summarizeEdits(ref, cur);
  assert.ok(lines.some((l) => /^removed rectangle "C" \[id c\]/.test(l)), lines.join("\n"));
  assert.ok(lines.some((l) => /^moved rectangle "A" \[id a\] by \(\+40,-10\)/.test(l)));
  assert.ok(lines.some((l) => /^resized rectangle "B" \[id b\]: 100x50 -> 180x50/.test(l)));
  assert.ok(lines.some((l) => /^reconnected arrow \[id e\]: end "B" -> a free end/.test(l)));
  assert.equal(lines.length, 4);
  assert.ok(lines[0].startsWith("removed"), "structural changes first");
  assert.ok(lines.at(-1).startsWith("moved"), "layout changes last");
});

test("a summary is bounded in lines and in line length, and says when it truncated", () => {
  const ref = [];
  const cur = [];
  for (let i = 0; i < 60; i++) {
    ref.push(shape(`n${i}`, null, { x: i * 10 }));
    cur.push(shape(`n${i}`, null, { x: i * 10 + 50 }));
  }
  const lines = summarizeEdits(ref, cur);
  assert.equal(lines.length, EDIT_SUMMARY_LINE_LIMIT);
  assert.ok(EDIT_SUMMARY_LINE_LIMIT <= 50 && EDIT_SUMMARY_LINE_WIDTH <= 300);
  assert.match(lines.at(-1), /^\.\.\. and 21 more changes not listed/);
  assert.ok(lines.every((l) => l.length <= EDIT_SUMMARY_LINE_WIDTH && !l.includes("\n")));

  const long = text("t", "x".repeat(500) + "\nsecond line");
  const out = summarizeEdits([], [long]);
  assert.equal(out.length, 1);
  assert.ok(out[0].length <= EDIT_SUMMARY_LINE_WIDTH);
  assert.ok(out[0].includes("..."), "the long label is visibly cut");
  assert.equal(summarizeEdits([], [long], { maxLines: 3, maxLineChars: 40 })[0].length <= 40, true);
});

test("an arrow attached at both ends does not report its own geometry when its shapes move", () => {
  const ref = [...node("a", "A"), ...node("b", "B", { x: 300 }), arrow("e", "a", "b")];
  const cur = copy(ref);
  byId(cur, "b").x += 100;
  byId(cur, "e").points = [[0, 0], [300, 0]];
  byId(cur, "e").width = 300;
  const lines = summarizeEdits(ref, cur);
  assert.equal(lines.length, 1);
  assert.match(lines[0], /^moved rectangle "B"/);
});

test("a summary with no reference says so instead of listing everything as new", () => {
  const lines = summarizeEdits(null, node("a", "A"));
  assert.equal(lines.length, 1);
  assert.match(lines[0], /no converted reference/);
});

// ------------------------------------------------ 4.3 opening decision

test("opening decision table", () => {
  const ref = node("a", "A");
  const edited = [...copy(ref), ...node("z", "Z", { x: 400 })];
  const rec = (over) => ({ format: 2, digest: "d1", measure_gen: 1, current: { elements: copy(ref) }, pristine: { elements: copy(ref) }, ...over });

  assert.equal(decideOpening({ record: null, digest: "d1" }), OPEN_CONVERT, "no record");
  assert.equal(decideOpening({ record: rec({ current: { elements: [] } }), digest: "d1" }), OPEN_CONVERT, "no scene");
  assert.equal(decideOpening({ record: rec(), digest: "d1" }), OPEN_REOPEN, "same digest");
  assert.equal(decideOpening({ record: rec({ current: { elements: edited } }), digest: "d1" }), OPEN_REOPEN, "same digest with edits");
  assert.equal(decideOpening({ record: rec(), digest: "d2" }), OPEN_CONVERT, "other digest, no edits: silent rebuild");
  assert.equal(decideOpening({ record: rec({ current: { elements: edited } }), digest: "d2" }), OPEN_ASK, "other digest with edits");
  assert.equal(decideOpening({ record: rec({ pristine: null }), digest: "d2" }), OPEN_ASK, "no reference but a scene counts as edited");
  assert.equal(decideOpening({ record: rec({ pristine: null }), digest: "d1" }), OPEN_REOPEN);
});

// ------------------------------------------------ 4.4 links

test("only http, https and mailto links are returned, normalized", () => {
  assert.equal(safeLinkTarget("https://example.com/a?b=1"), "https://example.com/a?b=1");
  assert.equal(safeLinkTarget("HTTP://x"), "http://x/");
  assert.equal(safeLinkTarget(" mailto:a@b "), "mailto:a@b");
  assert.equal(safeLinkTarget("MAILTO:a@b"), "mailto:a@b");
  assert.equal(safeLinkTarget("javascript:alert(1)"), null);
  assert.equal(safeLinkTarget("JaVaScRiPt:alert(1)"), null);
  assert.equal(safeLinkTarget("data:text/html,<b>x</b>"), null);
  assert.equal(safeLinkTarget("file:///etc/passwd"), null);
  assert.equal(safeLinkTarget("//x"), null);
  assert.equal(safeLinkTarget("/relative/path"), null);
  assert.equal(safeLinkTarget("example.com"), null);
  assert.equal(safeLinkTarget(""), null);
  assert.equal(safeLinkTarget(null), null);
  assert.equal(safeLinkTarget(42), null);
});
