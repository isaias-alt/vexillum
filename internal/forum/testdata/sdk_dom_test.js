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
    const entries = selector.split(",");
    const matches = (el) => {
      if (selector === "[data-forum-ui]") return el.getAttribute("data-forum-ui") !== null;
      const tag = el.tagName.toLowerCase();
      return entries.some((e) => (e === "a[href]" ? tag === "a" && el.getAttribute("href") !== null : tag === e));
    };
    for (let el = this; el; el = el.parentElement) if (matches(el)) return el;
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

// Annotation mode. Off: nothing is touched. On: plain content annotates on a
// click; the artifact's own controls keep acting, and only Alt/Option+click
// annotates them.
const annotations = () => posts.filter((m) => m.type === "forum:annotate");
const clickOn = (target, extra) => dispatch("click", Object.assign({ target }, extra));
const save = main.children[1]; // a button
const paragraph = main.children[4];
const input = main.children[2];

let e = clickOn(save);
assert.ok(!e.defaultPrevented && !e.stopped, "annotation off: a click must reach the artifact untouched");
assert.strictEqual(annotations().length, 0);

sendToSDK({ type: "forum:mode", on: true });

// Plain content: a click annotates and never reaches the artifact.
e = clickOn(paragraph);
assert.ok(e.defaultPrevented && e.stopped, "annotation on: a click on content annotates");
assert.strictEqual(annotations().length, 1);
// Compared as JSON: the SDK runs in its own vm realm, so prototypes differ.
assert.strictEqual(JSON.stringify(annotations()[0].context), JSON.stringify(dom.elementContext(paragraph)));
assert.strictEqual(JSON.stringify(Object.keys(annotations()[0].rect).sort()), JSON.stringify(["h", "w", "x", "y"]));
for (const type of ["mousedown", "pointerdown", "pointerup", "dblclick"]) {
  assert.ok(dispatch(type, { target: paragraph }).stopped, type + " on content must not reach the artifact");
}

// Controls act normally: nothing is prevented, stopped or annotated, for every kind.
const radio = new El("input", { type: "radio", name: "p" });
const label = new El("label", {}, [new El("span", {}, [], "Plan A")]);
const select = new El("select", {}, [new El("option", {}, [], "one")]);
const summary = new El("summary", {}, [], "More");
const formLink = new El("a", { href: "#next" }, [], "next");
const form = new El("form", {}, [formLink, radio, label, select, summary, new El("textarea")]);
new El("div", {}, [form]);
const before = annotations().length;
for (const [name, target] of [["button", save], ["text input", input], ["radio", radio], ["label", label], ["text inside a label", label.children[0]], ["select", select], ["option", select.children[0]], ["summary", summary], ["link inside a form", formLink], ["textarea", form.children[5]]]) {
  for (const type of ["click", "mousedown", "pointerdown", "pointerup", "mouseup"]) {
    const ev = dispatch(type, { target });
    assert.ok(!ev.defaultPrevented && !ev.stopped, name + ": a plain " + type + " must act normally in annotation mode");
  }
}
assert.strictEqual(annotations().length, before, "controls are not annotated by a plain click");
// A link outside any form is content, not a control.
assert.ok(clickOn(new El("a", { href: "#x" }, [], "see")).defaultPrevented, "a link in prose annotates");
const afterLink = annotations().length;

// Alt/Option+click annotates a control instead of acting, and does not fire it.
e = clickOn(radio, { altKey: true });
assert.ok(e.defaultPrevented && e.stopped, "Alt+click on a control annotates it without firing it");
assert.strictEqual(annotations().length, afterLink + 1);
assert.strictEqual(annotations()[afterLink].context.tag, "input");
e = clickOn(save, { altKey: true });
assert.ok(e.defaultPrevented, "Alt+click on a button must not press it");
for (const type of ["mousedown", "pointerdown", "pointerup"]) assert.ok(dispatch(type, { target: radio, altKey: true }).stopped);
assert.ok(dispatch("mousedown", { target: radio, altKey: true }).defaultPrevented, "Alt+press on a control must not focus or open it");
assert.ok(!dispatch("mousedown", { target: paragraph }).defaultPrevented, "a press on content stays free so text can be selected");

// Overlay clicks are never annotated.
assert.ok(!dispatch("click", { target: overlay.children[0] }).defaultPrevented);

sendToSDK({ type: "forum:mode", on: false });
assert.ok(!clickOn(paragraph).defaultPrevented, "switching annotation off restores normal clicks");

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
