// The plain-DOM parts of the frame page (status line, link confirmation,
// opening choice, failure panel) against a small fake document. These carry
// the accessibility behaviour that is easy to break without noticing: roles,
// focus placement, the focus trap, Escape and focus return.
import assert from "node:assert/strict";
import test from "node:test";

class FakeNode {
  constructor(tag) {
    this.tag = tag;
    this.attrs = {};
    this.kids = [];
    this.parent = null;
    this.own = "";
    this.handlers = {};
  }
  get className() { return this.attrs.class || ""; }
  set className(v) { this.attrs.class = v; }
  get textContent() { return this.own + this.kids.map((k) => k.textContent).join(""); }
  set textContent(v) { this.kids = []; this.own = String(v); }
  setAttribute(k, v) { this.attrs[k] = String(v); }
  getAttribute(k) { return k in this.attrs ? this.attrs[k] : null; }
  removeAttribute(k) { delete this.attrs[k]; }
  appendChild(n) { if (n.parent) n.remove(); n.parent = this; this.kids.push(n); return n; }
  remove() { if (this.parent) this.parent.kids = this.parent.kids.filter((k) => k !== this); this.parent = null; }
  addEventListener(type, fn) { (this.handlers[type] ||= []).push(fn); }
  removeEventListener(type, fn) { this.handlers[type] = (this.handlers[type] || []).filter((f) => f !== fn); }
  focus() { document.activeElement = this; }
  contains(n) { for (let x = n; x; x = x.parent) if (x === this) return true; return false; }
  all() { return this.kids.flatMap((k) => [k, ...k.all()]); }
  // Only the selectors frame-dom.js uses: tag names, [href], [tabindex]:not(...).
  querySelectorAll(selector) {
    const parts = selector.split(",").map((s) => s.trim());
    return this.all().filter((n) => parts.some((p) => {
      if (p === "[href]") return "href" in n.attrs;
      if (p.startsWith("[tabindex]")) return "tabindex" in n.attrs && n.attrs.tabindex !== "-1";
      return n.tag === p;
    }));
  }
  click() { (this.handlers.click || []).forEach((fn) => fn({ target: this })); }
  // Dispatches a keydown through the capture handlers registered here.
  key(key, extra = {}) {
    const event = { key, shiftKey: false, defaultPrevented: false, preventDefault() { this.defaultPrevented = true; }, stopPropagation() {}, ...extra };
    (this.handlers.keydown || []).forEach((fn) => fn(event));
    return event;
  }
}

const document = {
  activeElement: null,
  body: new FakeNode("body"),
  createElement: (tag) => new FakeNode(tag),
};
globalThis.document = document;

const timers = [];
globalThis.setTimeout = (fn, ms) => { const t = { fn, ms, live: true }; timers.push(t); return t; };
globalThis.clearTimeout = (t) => { if (t) t.live = false; };
const runTimers = () => timers.splice(0).forEach((t) => t.live && t.fn());

const { askOpeningChoice, confirmLinkDialog, createStatus, make, showFailure } = await import("../src/frame-dom.js");

const buttonLabelled = (root, label) => root.querySelectorAll("button").find((b) => b.textContent === label);

test("make builds elements with attributes, text and children", () => {
  const child = make("span", { text: "inner" });
  const node = make("div", { class: "a b", role: "note", text: "x" }, [child]);
  assert.equal(node.className, "a b");
  assert.equal(node.getAttribute("role"), "note");
  assert.equal(node.textContent, "xinner");
});

test("the status line is a polite live region that is always in the tree", () => {
  const status = createStatus();
  assert.equal(status.root.getAttribute("role"), "status");
  assert.equal(status.root.getAttribute("aria-live"), "polite");
  assert.equal(status.root.getAttribute("data-empty"), "");
});

test("a plain note fades away by itself", () => {
  const status = createStatus();
  status.say("Saved.", { tone: "ok" });
  assert.equal(status.root.getAttribute("data-tone"), "ok");
  assert.equal(status.root.textContent, "Saved.");
  runTimers();
  assert.equal(status.root.textContent, "");
  assert.equal(status.root.getAttribute("data-empty"), "");
});

test("a failure stays until dismissed and offers its actions plus Dismiss", () => {
  const status = createStatus();
  let retried = 0;
  status.say("The save failed.", { tone: "error", actions: [{ label: "Retry", run: () => retried++ }] });
  runTimers();
  assert.match(status.root.textContent, /The save failed\./, "no timer clears an error");
  assert.equal(status.root.getAttribute("data-tone"), "error");
  buttonLabelled(status.root, "Retry").click();
  assert.equal(retried, 1);
  buttonLabelled(status.root, "Dismiss").click();
  assert.equal(status.root.textContent, "");
});

test("a sticky note is not cleared by the timer", () => {
  const status = createStatus();
  status.say("Preparing...", { sticky: true });
  runTimers();
  assert.equal(status.root.textContent, "Preparing...");
  status.clear();
  assert.equal(status.root.textContent, "");
});

function openDialog(url) {
  const before = make("button", { text: "Fullscreen" });
  document.body.appendChild(before);
  before.focus();
  const answer = confirmLinkDialog(url);
  const scrim = document.body.kids[document.body.kids.length - 1];
  const dialog = scrim.kids[0];
  return { before, answer, scrim, dialog, cancel: buttonLabelled(dialog, "Cancel"), open: buttonLabelled(dialog, "Open link") };
}

test("the link dialog is a named modal that shows the full address and starts on Cancel", () => {
  const url = "https://example.com/a/very/long/path?with=query&and=more#frag";
  const { dialog, cancel, open, scrim } = openDialog(url);
  assert.equal(dialog.getAttribute("role"), "dialog");
  assert.equal(dialog.getAttribute("aria-modal"), "true");
  const labelId = dialog.getAttribute("aria-labelledby");
  const label = dialog.all().find((n) => n.getAttribute("id") === labelId);
  assert.ok(label && label.textContent.length > 0, "the dialog has an accessible name");
  assert.ok(dialog.textContent.includes(url), "the whole address is shown");
  assert.equal(document.activeElement, cancel, "initial focus is the safe action");
  scrim.remove();
  void open;
});

test("Tab and Shift+Tab stay inside the dialog", () => {
  const { scrim, cancel, open } = openDialog("https://example.com/");
  open.focus();
  let ev = scrim.key("Tab");
  assert.ok(ev.defaultPrevented);
  assert.equal(document.activeElement, cancel, "Tab from the last control wraps to the first");
  ev = scrim.key("Tab", { shiftKey: true });
  assert.ok(ev.defaultPrevented);
  assert.equal(document.activeElement, open, "Shift+Tab from the first control wraps to the last");
  document.activeElement = null;
  ev = scrim.key("Tab");
  assert.equal(document.activeElement, cancel, "focus that escaped is pulled back in");
  scrim.remove();
});

test("Escape cancels and focus returns to what had it", async () => {
  const { scrim, answer, before } = openDialog("https://example.com/");
  scrim.key("Escape");
  assert.equal(await answer, false);
  assert.equal(scrim.parent, null, "the dialog is gone");
  assert.equal(document.activeElement, before);
});

test("Cancel answers false and Open link answers true", async () => {
  const one = openDialog("https://example.com/");
  one.cancel.click();
  assert.equal(await one.answer, false);
  const two = openDialog("mailto:a@b.co");
  two.open.click();
  assert.equal(await two.answer, true);
  assert.equal(document.activeElement, two.before);
});

test("the opening choice has two real buttons and focus starts on the first", async () => {
  const host = make("div");
  document.body.appendChild(host);
  const pending = askOpeningChoice(host);
  const group = host.kids[0];
  assert.equal(group.getAttribute("role"), "group");
  assert.ok(group.getAttribute("aria-labelledby"));
  const buttons = group.querySelectorAll("button");
  assert.equal(buttons.length, 2);
  assert.equal(document.activeElement, buttons[0]);
  buttons[1].click();
  assert.equal(await pending, "keep");
  assert.equal(host.kids.length, 0, "the choice goes away once made");

  const again = askOpeningChoice(host);
  host.kids[0].querySelectorAll("button")[0].click();
  assert.equal(await again, "rebuild");
});

test("the failure panel is announced, says why and keeps the diagram text readable", () => {
  const host = make("div");
  showFailure(host, "This diagram could not be converted", new Error("bad syntax on line 2"), "flowchart LR\n  A -->");
  const panel = host.kids[0];
  assert.equal(panel.getAttribute("role"), "alert");
  assert.match(panel.textContent, /could not be converted/);
  assert.match(panel.textContent, /bad syntax on line 2/);
  const pre = panel.all().find((n) => n.tag === "pre");
  assert.ok(pre && pre.textContent === "flowchart LR\n  A -->");
  showFailure(host, "Nope", "plain string", "");
  assert.equal(host.kids.length, 1, "it replaces the previous panel");
  assert.ok(!host.kids[0].all().some((n) => n.tag === "pre"), "no empty source block when there is no text");
});
