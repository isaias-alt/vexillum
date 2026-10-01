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
  setAttribute(name, value) { this.attrs[name] = String(value); }
  getAttribute(name) { return name in this.attrs ? this.attrs[name] : null; }
  closest(selector) {
    // The forum-ui marker, or a comma list of plain tag names.
    const tags = selector.split(",");
    for (let el = this; el; el = el.parentElement) {
      if (selector === "[data-forum-ui]" ? el.getAttribute("data-forum-ui") !== null : tags.includes(el.tagName.toLowerCase())) return el;
    }
    return null;
  }
  getBoundingClientRect() { return { left: 10, top: 20, width: 100, height: 30 }; }
  get isConnected() { return true; }
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
  // Listeners are recorded so a test can dispatch events; the parent window
  // is a recorder, which is where the SDK posts its messages to the chrome.
  const handlers = { document: {}, window: {} };
  const record = (where) => (type, fn) => { (handlers[where][type] = handlers[where][type] || []).push(fn); };
  document.addEventListener = record("document");
  const posts = [];
  const parent = { postMessage: (message) => posts.push(message) };
  const win = { document, addEventListener: record("window"), requestAnimationFrame() { return 1; }, setTimeout() { return 1; }, getSelection: () => null, parent };
  vm.runInNewContext(fs.readFileSync(process.argv[2], "utf8"), { window: win, document, CSS: { escape: (s) => s }, Object, Promise, Map, Error, JSON, setTimeout() {}, clearTimeout() {} });
  const dispatch = (type, event) => {
    const e = Object.assign({ type, altKey: false, defaultPrevented: false, stopped: false, preventDefault() { this.defaultPrevented = true; }, stopPropagation() { this.stopped = true; } }, event);
    for (const fn of handlers.document[type] || []) fn(e);
    return e;
  };
  const sendToSDK = (message) => (handlers.window.message || []).forEach((fn) => fn({ source: parent, data: message }));
  return { dom: win.forum.__dom, posts, dispatch, sendToSDK };
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
const { dom, posts, dispatch, sendToSDK } = load(tree);

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

// Annotation mode: off leaves the artifact alone; on annotates instead of
// acting; Alt/Option held opts out of the interception.
const save = main.children[1];
const click = (extra) => dispatch("click", Object.assign({ target: save }, extra));
let e = click();
assert.ok(!e.defaultPrevented && !e.stopped, "annotation off: a click must reach the artifact untouched");
assert.strictEqual(posts.filter((m) => m.type === "forum:annotate").length, 0);

sendToSDK({ type: "forum:mode", on: true });
e = click();
assert.ok(e.defaultPrevented && e.stopped, "annotation on: the click annotates instead of acting");
const annotate = posts.filter((m) => m.type === "forum:annotate");
assert.strictEqual(annotate.length, 1);
// Compared as JSON: the SDK runs in its own vm realm, so prototypes differ.
assert.strictEqual(JSON.stringify(annotate[0].context), JSON.stringify({ tag: "button", selector: 'button[data-testid="save"]', text: "Save" }));
assert.strictEqual(JSON.stringify(Object.keys(annotate[0].rect).sort()), JSON.stringify(["h", "w", "x", "y"]));
for (const type of ["mousedown", "pointerdown", "pointerup", "dblclick", "submit"]) {
  const ev = dispatch(type, { target: save });
  assert.ok(ev.stopped, type + " must not reach the artifact in annotation mode");
}

e = click({ altKey: true });
assert.ok(!e.defaultPrevented && !e.stopped, "Alt+click acts on the control natively");
assert.strictEqual(posts.filter((m) => m.type === "forum:annotate").length, 1, "Alt+click does not annotate");
assert.ok(!dispatch("mousedown", { target: save, altKey: true }).stopped);

// Overlay clicks are never annotated.
const overlayClick = dispatch("click", { target: overlay.children[0] });
assert.ok(!overlayClick.defaultPrevented);

sendToSDK({ type: "forum:mode", on: false });
assert.ok(!click().defaultPrevented, "switching annotation off restores normal clicks");

// Ctrl/Cmd+I asks the chrome to toggle the mode, wherever focus is.
dispatch("keydown", { key: "i", ctrlKey: true, metaKey: false, shiftKey: false });
assert.ok(posts.some((m) => m.type === "forum:toggle-mode"));

// The chrome's theme reaches the artifact's <html>; garbage is dark.
sendToSDK({ type: "forum:theme", theme: "light" });
assert.strictEqual(tree.getAttribute("data-fr-theme"), "light");
sendToSDK({ type: "forum:theme", theme: "dark" });
assert.strictEqual(tree.getAttribute("data-fr-theme"), "dark");
sendToSDK({ type: "forum:theme", theme: "<script>" });
assert.strictEqual(tree.getAttribute("data-fr-theme"), "dark");

console.log("ok");
