// Run by sdk_dom_test.go: exercises the pure DOM helpers of
// assets/chrome/forum-sdk.js (selector, element context, selection context)
// against a small fake DOM. There is no browser here, so event wiring,
// overlays and layout are not covered; those are checked against the real
// binary by hand. Prints "ok" or throws.
const fs = require("fs");
const assert = require("assert");
const vm = require("vm");

class El {
  constructor(tag, attrs = {}, children = [], text = "") {
    this.nodeType = 1;
    this.tagName = tag.toUpperCase();
    this.attrs = attrs;
    this.children = children;
    this.parentElement = null;
    this.textContent = text;
    this.innerText = text;
    for (const child of children) child.parentElement = this;
  }
  getAttribute(name) { return name in this.attrs ? this.attrs[name] : null; }
  closest(selector) {
    // Only the forum-ui marker is needed.
    for (let el = this; el; el = el.parentElement) if (selector === "[data-forum-ui]" && el.getAttribute("data-forum-ui") !== null) return el;
    return null;
  }
}

function walk(el, out = []) { out.push(el); el.children.forEach((c) => walk(c, out)); return out; }

// Matches the selector grammar selectorOf emits: segments joined by " > ",
// each tag(#id)?([attr="v"])*(:nth-of-type(n))?.
function matchesSegment(el, seg) {
  const m = /^([a-z0-9-]+)(?:#([^\[:]+))?((?:\[[^\]]+\])*)(?::nth-of-type\((\d+)\))?$/.exec(seg);
  assert.ok(m, "selector segment outside the expected grammar: " + seg);
  if (el.tagName.toLowerCase() !== m[1]) return false;
  if (m[2] && el.getAttribute("id") !== m[2]) return false;
  for (const a of m[3].matchAll(/\[([^=\]]+)="((?:[^"\\]|\\.)*)"\]/g)) if (el.getAttribute(a[1]) !== a[2].replace(/\\(.)/g, "$1")) return false;
  if (m[4]) {
    const same = el.parentElement.children.filter((c) => c.tagName === el.tagName);
    if (same.indexOf(el) + 1 !== Number(m[4])) return false;
  }
  return true;
}
function matchChain(el, segs, i) {
  if (!el || !matchesSegment(el, segs[i])) return false;
  return i === 0 || matchChain(el.parentElement, segs, i - 1);
}

function load(root) {
  const all = walk(root);
  const document = {
    addEventListener() {},
    getElementById: (id) => all.find((e) => e.getAttribute("id") === id) || null,
    querySelectorAll: (selector) => { const segs = selector.split(" > "); return all.filter((e) => matchChain(e, segs, segs.length - 1)); },
    documentElement: root,
  };
  const win = { document, addEventListener() {}, requestAnimationFrame() { return 1; }, setTimeout() {}, getSelection: () => null };
  win.parent = win;
  vm.runInNewContext(fs.readFileSync(process.argv[2], "utf8"), { window: win, document, CSS: { escape: (s) => s }, Object, Promise, Map, Error, JSON, setTimeout() {}, clearTimeout() {} });
  return win.forum.__dom;
}

const li = (t) => new El("li", {}, [], t);
const tree = new El("html", {}, [
  new El("body", {}, [
    new El("div", { id: "main" }, [
      new El("ul", {}, [li("one"), li("two"), li("three")]),
      new El("button", { "data-testid": "save" }, [], "Save"),
      new El("input", { name: "email" }),
      new El("input", { name: "email" }),
      new El("p", {}, [], "  a   long\n text " + "x".repeat(400)),
    ]),
    new El("div", { id: "dup" }, [new El("span", {}, [], "a")]),
    new El("div", { id: "dup" }, [new El("span", {}, [], "b")]),
    new El("section", {}, [new El("p", {}, [], "x"), new El("p", {}, [], "y")]),
    new El("div", { "data-forum-ui": "annotation" }, [new El("p", {}, [], "overlay")]),
  ]),
]);
const dom = load(tree);

// Every element resolves back to itself through its own selector.
for (const el of walk(tree)) assert.deepStrictEqual(walkResolve(el), [el], "round trip " + el.tagName);
function walkResolve(el) {
  const segs = dom.selectorOf(el).split(" > ");
  return walk(tree).filter((e) => matchChain(e, segs, segs.length - 1));
}

const main = walk(tree).find((e) => e.getAttribute("id") === "main");
assert.strictEqual(dom.selectorOf(main), "div#main");
const items = main.children[0].children;
assert.strictEqual(dom.selectorOf(items[1]), "li:nth-of-type(2)", "second li (shortest unique path)");
assert.strictEqual(dom.selectorOf(main.children[1]), 'button[data-testid="save"]');
assert.match(dom.selectorOf(main.children[3]), /input\[name="email"\]:nth-of-type\(2\)/);
// A duplicated id is not a usable anchor.
const dups = walk(tree).filter((e) => e.getAttribute("id") === "dup");
assert.ok(!dom.selectorOf(dups[0].children[0]).includes("#dup"));

// Element context: tag, selector, whitespace-collapsed text capped at 240.
const ctx = dom.elementContext(main.children[4]);
assert.strictEqual(ctx.tag, "p");
assert.strictEqual(ctx.text.length, 240);
assert.ok(ctx.text.startsWith("a long text xxx"));

// Selection context.
const text = (parent) => ({ nodeType: 3, parentElement: parent });
const selection = (str, container, collapsed = false) => ({ rangeCount: 1, toString: () => str, getRangeAt: () => ({ collapsed, commonAncestorContainer: container }) });
let sel = dom.selectionContext(selection("  hello \n world ", text(items[0])));
assert.strictEqual(sel.context.tag, "text");
assert.strictEqual(sel.context.text, "hello world");
assert.strictEqual(sel.context.selector, "li:nth-of-type(1)");
assert.strictEqual(sel.context.target, undefined);
sel = dom.selectionContext(selection("y".repeat(900), text(main.children[4])));
assert.strictEqual(sel.context.text.length, 500);
assert.strictEqual(sel.context.target.type, "text-range");
assert.strictEqual(sel.context.target.text.length, 900);
assert.strictEqual(dom.selectionContext(selection("x", text(items[0]), true)), null, "collapsed");
assert.strictEqual(dom.selectionContext(selection("   ", text(items[0]))), null, "blank");
const overlay = walk(tree).find((e) => e.getAttribute("data-forum-ui"));
assert.strictEqual(dom.selectionContext(selection("overlay", text(overlay.children[0]))), null, "forum ui is never annotated");
assert.strictEqual(dom.selectionContext(null), null);

console.log("ok");
