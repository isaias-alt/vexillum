// Run by layout_dom_test.go: the pure geometry of assets/chrome/forum-layout.js
// (judge, agree, shape) and its selectors, without a browser. The sampler that
// reads a live document is covered by the real-Chrome tests. Prints "ok" or throws.
const fs = require("fs");
const vm = require("vm");
const assert = require("assert");

function loadKit() {
  const win = { parent: null, addEventListener() {} };
  win.parent = win; // not framed: the test kit is exposed
  const context = vm.createContext({ window: win, document: { currentScript: null }, console, WeakMap, Map, Set, Math, Number, JSON, Object, Array });
  vm.runInContext(fs.readFileSync(process.argv[2], "utf8"), context);
  return win.__forumLayoutKit;
}
const raw = loadKit();
assert.ok(raw, "the kit is exposed outside the chrome");
assert.ok(Object.isFrozen(raw) && Object.isFrozen(raw.thresholds) && Object.isFrozen(raw.budget) && Object.isFrozen(raw.timing), "the kit is frozen");
// Results come out of another realm; a JSON round trip makes them comparable.
const plain = (v) => JSON.parse(JSON.stringify(v));
const kit = { ...raw, judge: (s) => plain(raw.judge(s)), agree: (a, b) => plain(raw.agree(a, b)), shape: (f) => plain(raw.shape(f)), thresholds: raw.thresholds, budget: raw.budget };
const T = kit.thresholds;

// ----------------------------------------------------------------- builders
const reachAll = { l: 0, t: 0, r: 1100, b: 5000 };
const page = (over = {}) => ({ sideways: true, overflowX: 0, reach: reachAll, ...over });
const rect = (l, t, r, b) => ({ l, t, r, b });
const clip = (l, t, r, b, over = {}) => ({ ref: "box", l, t, r, b, x: true, y: true, softX: false, softY: false, ...over });
const text = (over = {}) => ({ role: "text", ref: "p", rect: rect(10, 10, 210, 30), lineH: 20, slack: 0, clips: [], scrollX: false, scrollY: false, pinned: false, cover: null, ...over });
const control = (over = {}) => ({ role: "control", ref: "btn", rect: rect(10, 10, 110, 40), lineH: 30, slack: 0, clips: [], scrollX: false, scrollY: false, pinned: false, cover: null, ...over });
const roomy = page({ reach: { l: -20000, t: -20000, r: 20000, b: 20000 } }); // reach is not what these cases are about
const judge = (items, p = page()) => kit.judge({ page: p, items });
const kinds = (f) => f.map((x) => `${x.kind}:${x.axis}:${Math.sign(x.overflow_px)}`);

// A text item cut on its right edge by `px`.
const cutRight = (px, over = {}) => text({ rect: rect(10, 10, 610, 30), clips: [clip(0, 0, 610 - px, 100)], ...over });

// ------------------------------------------------- clipped-text: failing/benign
{
  const f = judge([cutRight(550)]);
  assert.deepStrictEqual(kinds(f), ["clipped-text:horizontal:1"]);
  assert.strictEqual(f[0].overflow_px, 550);
  assert.strictEqual(f[0].ref, "box", "the finding points at the container that cuts");
  assert.deepStrictEqual(judge([text({ rect: rect(10, 10, 100, 30), clips: [clip(0, 0, 210, 100)] })]), [], "text inside its container is benign");
  // left edge is negative
  assert.deepStrictEqual(kinds(judge([text({ rect: rect(-60, 10, 140, 30), clips: [clip(0, 0, 210, 100)] })], roomy)), ["clipped-text:horizontal:-1"]);
  // vertical: bottom positive, top negative
  assert.deepStrictEqual(kinds(judge([text({ rect: rect(10, 10, 210, 90), lineH: 20, clips: [clip(0, 0, 210, 40)] })])), ["clipped-text:vertical:1"]);
  assert.deepStrictEqual(kinds(judge([text({ rect: rect(10, -30, 210, 30), lineH: 20, clips: [clip(0, 0, 210, 100)] })], roomy)), ["clipped-text:vertical:-1"]);
}

// ----------------------------------------------- boundaries on both sides
{
  const below = (v) => v * 0.99, above = (v) => v * 1.01;
  assert.deepStrictEqual(judge([cutRight(below(T.hiddenTextPx))]), [], "just under the pixel floor is silent");
  assert.strictEqual(judge([cutRight(above(T.hiddenTextPx))]).length, 1, "just over the pixel floor is reported");
  // letter-spacing slack is subtracted before the floor applies
  assert.deepStrictEqual(judge([cutRight(T.hiddenTextPx * 0.99 + 4, { slack: 4 })]), [], "a trailing letter-spacing gap is not a lost character");
  assert.strictEqual(judge([cutRight(T.hiddenTextPx * 1.01 + 4, { slack: 4 })]).length, 1);
  // vertical floor is a share of the line
  const vert = (share) => text({ rect: rect(10, 10, 210, 30 + share * 20), lineH: 20, clips: [clip(0, 0, 400, 30)] });
  assert.deepStrictEqual(judge([vert(below(T.hiddenTextLines))]), [], "a sliver of a line is silent");
  assert.strictEqual(judge([vert(above(T.hiddenTextLines))]).length, 1, "most of half a line is reported");
  // text pushed far out of the start side is the image-replacement idiom
  assert.deepStrictEqual(judge([text({ rect: rect(-9999, 10, -9799, 30), clips: [clip(0, 0, 210, 100)] })], roomy), []);
  const farEdge = T.farAwayPx;
  assert.deepStrictEqual(judge([text({ rect: rect(-farEdge - 50, 10, 100, 30), clips: [clip(0, 0, 210, 100)] })], roomy), [], "at or past the far limit on the start side is deliberate");
  assert.strictEqual(judge([text({ rect: rect(-(farEdge * 0.99) , 10, 100, 30), clips: [clip(0, 0, 210, 100)] })], roomy).length >= 1, true, "just inside the far limit is reported");
}

// ------------------------------------------------------- cut-off-control
{
  const cut = (px, w = 100) => control({ rect: rect(10, 10, 10 + w, 40), clips: [clip(0, 0, 10 + w - px, 100)] });
  assert.deepStrictEqual(judge([cut(T.controlCutPx * 0.99)]), [], "under the pixel floor");
  assert.deepStrictEqual(judge([cut(T.controlCutPx * 1.01, 1000)]), [], "over the floor but under the share floor");
  assert.strictEqual(judge([cut(T.controlCutPx * 1.01, 20)]).length, 1, "a small control is held to the pixel floor, its share is large");
  assert.deepStrictEqual(kinds(judge([cut(20)])), ["cut-off-control:horizontal:1"]);
  assert.deepStrictEqual(judge([cut(100 * T.controlCutMin * 0.99)]), [], "just under the minimum share");
  assert.strictEqual(judge([cut(100 * T.controlCutMin * 1.01 + 0.01)]).length, 1, "just over the minimum share");
  assert.deepStrictEqual(judge([cut(100 * T.controlCutMax * 1.02)]), [], "mostly hidden reads as hidden on purpose");
  assert.strictEqual(judge([cut(100 * T.controlCutMax * 0.98)]).length, 1, "just under the maximum share");
}

// -------------------------------------------------------------- wide-page
{
  assert.deepStrictEqual(judge([], page({ overflowX: T.wideByPx * 0.99 })), []);
  const f = judge([], page({ overflowX: T.wideByPx * 1.01 }));
  assert.deepStrictEqual(kinds(f), ["wide-page:horizontal:1"]);
  assert.strictEqual(f[0].ref, null, "the page has no element");
  assert.deepStrictEqual(judge([], page({ sideways: false, overflowX: 900 })), [], "a page that cannot scroll sideways has no scrollbar to blame");
}

// ------------------------------------------------------------- unreachable
{
  const at = (l, r, over = {}) => text({ rect: rect(l, 100, r, 120), ...over });
  assert.deepStrictEqual(kinds(judge([at(-300, -100)])), ["unreachable-text:horizontal:-1"]);
  assert.deepStrictEqual(kinds(judge([control({ rect: rect(-300, 50, -100, 80) })])), ["unreachable-control:horizontal:-1"]);
  // the end sides count only when the viewport does not scroll that way
  const fixed = page({ sideways: false, reach: { l: 0, t: 0, r: 1100, b: 5000 } });
  assert.deepStrictEqual(kinds(judge([at(1200, 1400)], fixed)), ["unreachable-text:horizontal:1"]);
  assert.deepStrictEqual(judge([at(1200, 1400)], page({ reach: { l: 0, t: 0, r: 1500, b: 5000 } })), [], "scrollable reach covers it");
  assert.deepStrictEqual(kinds(judge([text({ rect: rect(10, -400, 210, -380) })])), ["unreachable-text:vertical:-1"]);
  // boundaries: pixels and share
  const pxBeyondEdge = (px, w = 200) => at(-px, w - px);
  assert.deepStrictEqual(judge([pxBeyondEdge(T.beyondReachPx * 0.99, 10)]), [], "under the pixel floor");
  assert.deepStrictEqual(judge([pxBeyondEdge(T.beyondReachPx * 4, 8 * 4 / T.beyondReachShare + 100)]), [], "under the share floor");
  assert.strictEqual(judge([pxBeyondEdge(T.beyondReachShare * 200 * 1.01)]).length, 1, "just over the share");
  assert.deepStrictEqual(judge([pxBeyondEdge(T.beyondReachShare * 200 * 0.99)]), [], "just under the share");
  // parked far away on purpose, either side
  assert.deepStrictEqual(judge([at(-9999 - 200, -9999)]), []);
  assert.deepStrictEqual(judge([at(T.farAwayPx * 2 + 1100, T.farAwayPx * 2 + 1300)], fixed), []);
  assert.strictEqual(judge([at(-T.farAwayPx * 0.9 - 200, -T.farAwayPx * 0.9)]).length, 1);
  // pinned (fixed, sticky, transformed) items are placed on purpose
  assert.deepStrictEqual(judge([at(-300, -100, { pinned: true })]), []);
}

// ------------------------------------------------------------ buried-text
{
  const buried = (share, counted = 24) => text({ cover: { share, px: share * 200, counted } });
  assert.deepStrictEqual(judge([buried(T.buriedShare * 0.99)]), [], "under the share");
  const f = judge([buried(T.buriedShare * 1.01)]);
  assert.deepStrictEqual(kinds(f), ["buried-text:horizontal:1"]);
  assert.ok(f[0].overflow_px > 0);
  assert.deepStrictEqual(judge([buried(1, T.buriedMinPoints - 1)]), [], "too few testable points prove nothing");
  assert.strictEqual(judge([buried(1, T.buriedMinPoints)]).length, 1);
  assert.deepStrictEqual(judge([text({ cover: null })]), []);
}

// ------------------------------------------------------------ silent cases
{
  const hard = (over) => cutRight(300, over);
  assert.deepStrictEqual(judge([text({ rect: rect(10, 10, 210, 30), clips: [clip(0, 0, 100, 100, { softX: true })] })]), [], "ellipsis: deliberately shortened");
  assert.deepStrictEqual(judge([text({ rect: rect(10, 10, 210, 90), lineH: 20, clips: [clip(0, 0, 400, 40, { softY: true })] })]), [], "line clamp: deliberately shortened");
  assert.deepStrictEqual(judge([hard({ scrollX: true })]), [], "a scroll container is deliberately scrollable");
  assert.deepStrictEqual(judge([control({ rect: rect(10, 10, 110, 40), clips: [clip(0, 0, 90, 100)], scrollX: true })]), []);
  assert.deepStrictEqual(judge([text({ rect: rect(300, 10, 500, 30), clips: [clip(0, 0, 210, 100)] })]), [], "wholly outside its container: collapsed or tucked away");
  assert.deepStrictEqual(judge([cutRight(300, { pinned: true })]).map((f) => f.kind), ["clipped-text"], "pinned does not silence a real container cut");
  // axis scrolling is per axis: a horizontal scroller still reports a vertical cut
  assert.deepStrictEqual(kinds(judge([text({ rect: rect(10, 10, 210, 90), lineH: 20, scrollX: true, clips: [clip(0, 0, 210, 40)] })])), ["clipped-text:vertical:1"]);
}

// -------------------------------------------------- two-observation agreement
{
  const f = (kind, selector, axis, px) => ({ kind, selector, axis, overflow_px: px });
  const a = [f("clipped-text", "div#a", "horizontal", 10), f("clipped-text", "div#b", "horizontal", 10)];
  const b = [f("clipped-text", "div#a", "horizontal", 30), f("clipped-text", "div#c", "horizontal", 10)];
  assert.deepStrictEqual(kit.agree(a, b), [f("clipped-text", "div#a", "horizontal", 30)], "only what both observations show, with the later magnitude");
  assert.deepStrictEqual(kit.agree(a, []), []);
  assert.deepStrictEqual(kit.agree([], b), []);
  assert.deepStrictEqual(kit.agree([f("clipped-text", "div#a", "vertical", 10)], b), [], "axis is part of the identity");
}

// ----------------------------------------------------------- shape and order
{
  const f = (kind, selector, px) => ({ kind, selector, axis: "horizontal", overflow_px: px });
  const out = kit.shape([f("buried-text", "z", 5), f("wide-page", "", 100), f("clipped-text", "b", 5), f("clipped-text", "a", 5), f("clipped-text", "c", 50), f("clipped-text", "a", 9)]);
  assert.deepStrictEqual(out.findings.map((x) => x.kind + ":" + x.selector + ":" + x.overflow_px), ["wide-page::100", "clipped-text:c:50", "clipped-text:a:9", "clipped-text:b:5", "buried-text:z:5"]);
  assert.strictEqual(out.truncated, false);
  const many = [];
  for (let i = 0; i < kit.budget.findings + 5; i++) many.push(f("clipped-text", "s" + String(i).padStart(3, "0"), 10 + i));
  const cut = kit.shape(many);
  assert.strictEqual(cut.findings.length, kit.budget.findings);
  assert.strictEqual(cut.truncated, true, "a cut list is flagged so the pass is not conclusive");
  assert.deepStrictEqual(kit.shape(many.slice().reverse()), cut, "input order does not matter");
}

// --------------------------------------------------------- selector determinism
{
  class El {
    constructor(tag, attrs = {}, children = []) {
      this.nodeType = 1; this.tagName = tag.toUpperCase(); this.attrs = attrs; this.children = children; this.parentElement = null;
      for (const c of children) c.parentElement = this;
    }
    getAttribute(n) { return n in this.attrs ? this.attrs[n] : null; }
  }
  const p1 = new El("p"), p2 = new El("p"), span = new El("span", { id: "react-aria-12345" });
  const card = new El("div", { id: "card" }, [p1, p2, span]);
  const gen = new El("section", { id: "a1b2c3d4e5f6a7b8" }, [new El("p")]);
  const body = new El("body", {}, [card, gen]);
  const html = new El("html", {}, [body]);
  const all = [html, body, card, p1, p2, span, gen, gen.children[0]];
  const doc = { documentElement: html, body, getElementById: (id) => all.find((e) => e.attrs.id === id) || null };
  assert.strictEqual(kit.selectorFor(p2, doc), "div#card > p:nth-of-type(2)");
  assert.strictEqual(kit.selectorFor(p2, doc), kit.selectorFor(p2, doc), "deterministic");
  assert.strictEqual(kit.selectorFor(card, doc), "div#card");
  assert.strictEqual(kit.selectorFor(span, doc), "div#card > span", "a generated id is never used");
  assert.strictEqual(kit.selectorFor(gen.children[0], doc), "body > section > p", "neither is a hash-like one");
  assert.strictEqual(kit.selectorFor(null, doc), "");
  assert.strictEqual(kit.selectorFor(html, doc), "", "the page itself is empty");
  // bounded length
  let deep = new El("p");
  for (let i = 0; i < 80; i++) deep = new El("div", {}, [deep, new El("div")]);
  const root = new El("body", {}, [deep]);
  const h = new El("html", {}, [root]);
  const leaf = (function find(e) { return e.tagName === "P" ? e : e.children.map(find).find(Boolean); })(deep);
  const long = kit.selectorFor(leaf, { documentElement: h, body: root, getElementById: () => null });
  assert.ok(long.length <= kit.budget.selector && long.endsWith("p"), "bounded: " + long.length);
}

console.log("ok");
