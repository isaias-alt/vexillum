// Run by layout_dom_test.go: the pure classifiers of assets/chrome/forum-layout.js.
// There is no browser here, so the audit itself (which needs real layout) is
// checked against the real binary in Chrome. Prints "ok" or throws.
const fs = require("fs");
const assert = require("assert");
const vm = require("vm");

const win = {};
win.parent = win; // not framed: only the classifiers are exposed
vm.runInNewContext(fs.readFileSync(process.argv[2], "utf8"), { window: win, Object, Number, Math, Array, Set, String, JSON, Boolean });
const L = win.forumLayout;
assert.ok(L && Object.isFrozen(L), "forumLayout is exposed and frozen");

const box = { left: 0, right: 200, top: 0, bottom: 100 };
const frag = (left, right, top = 10, bottom = 30) => ({ left, right, top, bottom, width: right - left, height: bottom - top });

// --- classifySevereTextOverflow: text that really crosses a clipping box
const clipX = { box, overflowX: "hidden", overflowY: "visible" };
let r = L.classifySevereTextOverflow({ fragments: [frag(150, 320)], ...clipX });
assert.deepStrictEqual({ kind: r.kind, axis: r.axis, px: r.overflowPx }, { kind: "clipped-text", axis: "horizontal", px: 120 }, "text cut off by overflow:hidden is severe");
assert.strictEqual(L.classifySevereTextOverflow({ fragments: [frag(10, 190)], ...clipX }), null, "text that fits is fine");
assert.strictEqual(L.classifySevereTextOverflow({ fragments: [frag(150, 320)], ...clipX, isTruncated: true }), null, "an ellipsis or line-clamp is the author's intent");
assert.strictEqual(L.classifySevereTextOverflow({ fragments: [frag(150, 320)], ...clipX, isVisuallyHidden: true }), null, "screen-reader-only text stays silent");
assert.strictEqual(L.classifySevereTextOverflow({ fragments: [frag(150, 320)], box, overflowX: "auto", overflowY: "visible" }), null, "an intentional scroller is not a failure");
assert.strictEqual(L.classifySevereTextOverflow({ fragments: [frag(150, 320)], box, overflowX: "scroll", overflowY: "visible" }), null);
assert.strictEqual(L.classifySevereTextOverflow({ fragments: [frag(150, 320)], box, overflowX: "visible", overflowY: "visible" }), null, "visible overflow on the x axis is not clipping");
assert.strictEqual(L.classifySevereTextOverflow({ fragments: [frag(150, 201)], ...clipX }), null, "a pixel of ink is not severe");
assert.strictEqual(L.classifySevereTextOverflow({ fragments: [], ...clipX }), null);
assert.strictEqual(L.classifySevereTextOverflow({ fragments: [frag(10, 50)], box: null, overflowX: "hidden", overflowY: "hidden" }), null);
r = L.classifySevereTextOverflow({ fragments: [frag(10, 90, 150, 170)], box, overflowX: "visible", overflowY: "hidden" });
assert.strictEqual(r && r.axis, "vertical", "a line below a box that clips vertically is severe");

// --- classifyMaterialRectEscape
assert.strictEqual(L.classifyMaterialRectEscape({ rect: { left: 0, right: 100, top: 0, bottom: 20, width: 100, height: 20 }, boundary: box }), null, "inside is fine");
r = L.classifyMaterialRectEscape({ rect: { left: 190, right: 290, top: 0, bottom: 20, width: 100, height: 20 }, boundary: box, axes: ["horizontal"] });
assert.deepStrictEqual({ side: r.side, axis: r.axis, px: r.overflowPx }, { side: "end", axis: "horizontal", px: 90 });
r = L.classifyMaterialRectEscape({ rect: { left: -60, right: 40, top: 0, bottom: 20, width: 100, height: 20 }, boundary: box, axes: ["horizontal"] });
assert.strictEqual(r.side, "start");
assert.strictEqual(L.classifyMaterialRectEscape({ rect: { left: 198, right: 202, top: 0, bottom: 20, width: 4, height: 20 }, boundary: box, axes: ["horizontal"], minOutsidePx: 4 }), null, "under the pixel threshold");
assert.strictEqual(L.classifyMaterialRectEscape({ rect: { left: 0, right: NaN, width: NaN }, boundary: box }), null, "garbage geometry is ignored");

// --- isMaterialPageOverflow: cosmetic deltas never count, escaped content is required
assert.strictEqual(L.isMaterialPageOverflow({ overflowPx: 10, viewportWidth: 1200, hasEscapedContent: true }), false);
assert.strictEqual(L.isMaterialPageOverflow({ overflowPx: 300, viewportWidth: 1200, hasEscapedContent: false }), false, "a wide document with nothing escaping is not a failure");
assert.strictEqual(L.isMaterialPageOverflow({ overflowPx: 300, viewportWidth: 1200, hasEscapedContent: true }), true);
assert.strictEqual(L.isMaterialPageOverflow({ overflowPx: 50, viewportWidth: 1200, hasEscapedContent: true }), false, "5% of the viewport is the floor");

// --- findStableLayoutFindings: still settling is not a failure
const f = (kind, selector, axis = "horizontal") => ({ kind, selector, axis, severity: "error" });
const stable = L.findStableLayoutFindings([f("clipped-text", "p"), f("clipped-control", "b")], [f("clipped-text", "p"), f("overlapping-text", "h2")]);
assert.deepStrictEqual(stable.map((x) => x.selector), ["p"], "only what both samples agree on survives");
assert.deepStrictEqual(L.findStableLayoutFindings(null, [f("clipped-text", "p")]), []);

// --- isNearTotalOcclusion
assert.strictEqual(L.isNearTotalOcclusion({ occludedSamples: 9, totalSamples: 9 }), true);
assert.strictEqual(L.isNearTotalOcclusion({ occludedSamples: 5, totalSamples: 9 }), false, "half covered still reads");
assert.strictEqual(L.isNearTotalOcclusion({ occludedSamples: 4, totalSamples: 4 }), false, "too few samples to be sure");

console.log("ok");
