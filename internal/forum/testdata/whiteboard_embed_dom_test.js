// Drives assets/whiteboard-embed.js under node against a small fake browser:
// a tree of nodes with the handful of DOM calls the script makes, a clock the
// test advances by hand, and a scripted artifact bridge (window.forum.__rpc).
// Frames are fake too: each iframe records what the embed posts to it, and the
// test plays the frame's side of the vxb1. conversation.
//
// Usage: node whiteboard_embed_dom_test.js <path to whiteboard-embed.js>
// Prints "ok" and exits 0 when every check passes.
const assert = require("assert");
const fs = require("fs");
const vm = require("vm");

const embedPath = process.argv[2];
const source = fs.readFileSync(embedPath, "utf8");

// ---------------------------------------------------------------- fake DOM

class Node {
  constructor(tag) {
    this.tagName = String(tag).toUpperCase();
    this.attrs = {};
    this.children = [];
    this.parentNode = null;
    this.style = {};
    this.listeners = {};
    this.hidden = false;
    this.textValue = "";
    this.focused = false;
  }
  get id() { return this.attrs.id || ""; }
  get className() { return this.attrs.class || ""; }
  set className(v) { this.attrs.class = v; }
  get src() { return this.attrs.src || ""; }
  get isConnected() {
    let n = this;
    while (n.parentNode) n = n.parentNode;
    return n === doc.documentElement;
  }
  get textContent() { return this.textValue + this.children.map((c) => c.textContent).join(""); }
  set textContent(v) { this.children = []; this.textValue = String(v); }
  setAttribute(k, v) { this.attrs[k] = String(v); }
  getAttribute(k) { return k in this.attrs ? this.attrs[k] : null; }
  hasAttribute(k) { return k in this.attrs; }
  removeAttribute(k) { delete this.attrs[k]; }
  appendChild(child) { return this.insertBefore(child, null); }
  insertBefore(child, ref) {
    if (child.parentNode) child.remove();
    child.parentNode = this;
    const at = ref ? this.children.indexOf(ref) : -1;
    if (at < 0) this.children.push(child); else this.children.splice(at, 0, child);
    return child;
  }
  remove() {
    if (!this.parentNode) return;
    this.parentNode.children = this.parentNode.children.filter((c) => c !== this);
    this.parentNode = null;
  }
  addEventListener(type, fn, capture) { (this.listeners[type] ||= []).push({ fn, capture: !!capture }); }
  focus() { if (doc.activeElement) doc.activeElement.focused = false; this.focused = true; doc.activeElement = this; }
  click() { for (const l of this.listeners.click || []) l.fn({ target: this, stopPropagation() {}, preventDefault() {} }); }
  get contentWindow() { return this.win; }
  descendants() {
    const out = [];
    for (const c of this.children) { out.push(c, ...c.descendants()); }
    return out;
  }
  matches(sel) {
    return sel.split(",").some((part) => {
      const m = /^([a-z0-9]*)((?:\.[\w-]+)*)$/i.exec(part.trim());
      if (!m) return false;
      if (m[1] && this.tagName !== m[1].toUpperCase()) return false;
      const classes = this.className.split(/\s+/);
      return m[2].split(".").filter(Boolean).every((c) => classes.includes(c));
    });
  }
  querySelectorAll(sel) { return this.descendants().filter((n) => n.matches(sel)); }
  querySelector(sel) { return this.querySelectorAll(sel)[0] || null; }
}

const doc = {
  readyState: "complete",
  visibilityState: "visible",
  activeElement: null,
  listeners: {},
  addEventListener(type, fn, capture) { (this.listeners[type] ||= []).push({ fn, capture: !!capture }); },
  createElement: (tag) => new Node(tag),
  createTextNode: (text) => Object.assign(new Node("#text"), { textValue: String(text) }),
  getElementById: (id) => doc.documentElement.descendants().find((n) => n.id === id) || null,
  querySelectorAll: (sel) => doc.documentElement.querySelectorAll(sel),
};
doc.documentElement = new Node("html");
doc.head = doc.documentElement.appendChild(new Node("head"));
doc.body = doc.documentElement.appendChild(new Node("body"));
doc.documentElement.matchesDoc = true;

// ------------------------------------------------------------------ world

function makeWorld(options = {}) {
  doc.documentElement.children.forEach((c) => (c.children = []));
  doc.documentElement.attrs = options.theme === null ? {} : { "data-fr-theme": options.theme || "dark" };
  doc.activeElement = null;
  doc.visibilityState = "visible";
  doc.listeners = {};

  const timers = [];
  let now = 0;
  const winListeners = {};
  const observers = [];
  const calls = [];
  const store = new Map(); // ordinal -> record
  const knobs = { failWrite: false, failSubmit: false, failList: !!options.failList, hold: null };

  const blocks = (options.blocks || ["flowchart LR\n A-->B", "sequenceDiagram\n A->>B: hi", "pie\n \"a\": 1"]).map((text) => {
    const block = new Node("div");
    block.className = "mermaid";
    block.textValue = text;
    doc.body.appendChild(block);
    return block;
  });
  const listing = options.listing || blocks.map((b, i) => ({ ordinal: i, text: b.textValue, digest: "digest-" + i }));

  async function rpc(op, payload) {
    calls.push({ op, payload: JSON.parse(JSON.stringify(payload || {})) });
    if (knobs.hold) await knobs.hold;
    switch (op) {
      case "board.list":
        if (knobs.failList) throw new Error("no chrome");
        return { diagrams: listing };
      case "board.read":
        return { record: store.has(payload.ordinal) ? store.get(payload.ordinal) : null };
      case "board.write":
        if (knobs.failWrite) throw new Error("disk full");
        store.set(payload.ordinal, payload.body);
        return {};
      case "board.submit":
        if (knobs.failSubmit) throw new Error("session ended");
        return { prompt_uid: "p" };
      default:
        throw new Error("unsupported operation");
    }
  }

  const sandbox = {
    document: doc,
    console,
    Promise,
    JSON,
    Object,
    Array,
    String,
    Number,
    Math,
    Error,
    RegExp,
    Set,
    Map,
    Date,
    setTimeout(fn, ms) { const t = { fn, at: now + (ms || 0), live: true }; timers.push(t); return t; },
    clearTimeout(t) { if (t) t.live = false; },
    MutationObserver: class { constructor(cb) { observers.push(cb); } observe() {} },
    matchMedia: () => ({ matches: false, addEventListener() {} }),
    addEventListener(type, fn) { (winListeners[type] ||= []).push(fn); },
    forum: { __rpc: rpc },
    fetch() { throw new Error("the embed must not call fetch"); },
    XMLHttpRequest: function () { throw new Error("the embed must not use XHR"); },
  };
  sandbox.window = sandbox;
  doc.defaultView = sandbox;
  vm.createContext(sandbox);

  const world = {
    sandbox, calls, store, knobs, blocks,
    slots: () => doc.body.children.filter((c) => c.className.split(/\s+/).includes("vxb-slot")),
    overlay: () => doc.getElementById("vxb-overlay"),
    frames: () => doc.documentElement.querySelectorAll("iframe"),
    inlineFrame: (ordinal) => world.slots()[ordinal].querySelector("iframe"),
    overlayFrame: () => (world.overlay() ? world.overlay().querySelector("iframe") : null),
    callsOf: (op) => calls.filter((c) => c.op === op),
    async settle() { for (let i = 0; i < 30; i++) await Promise.resolve(); },
    async tick(ms) {
      now += ms;
      for (const t of timers.slice()) {
        if (t.live && t.at <= now) { t.live = false; t.fn(); }
      }
      await world.settle();
    },
    fire(type, event) { for (const fn of winListeners[type] || []) fn(event); },
    fireDoc(type, event) { for (const l of doc.listeners[type] || []) l.fn(event); },
    flipTheme(value) {
      if (value === null) delete doc.documentElement.attrs["data-fr-theme"];
      else doc.documentElement.attrs["data-fr-theme"] = value;
      observers.forEach((cb) => cb([]));
    },
    // The frame side: one message from an iframe's window to the embed.
    fromFrame(frame, kind, fields) {
      world.fire("message", { source: frame.win, data: { type: "vxb1." + kind, ...fields } });
    },
    sent(frame, kind) {
      return frame.win.received.filter((m) => m.type === "vxb1." + kind);
    },
  };

  // Every iframe the embed makes gets a window that records what it receives.
  const create = doc.createElement.bind(doc);
  doc.createElement = (tag) => {
    const node = create(tag);
    if (tag === "iframe") {
      node.win = { received: [], postMessage(data) { this.received.push(data); } };
      node.focus = function () { Node.prototype.focus.call(this); };
    }
    return node;
  };
  vm.runInContext(source, sandbox, { filename: "whiteboard-embed.js" });
  return world;
}

let tokenSeed = 0;
const newToken = () => (++tokenSeed).toString(16).padStart(32, "a");

// Mounts the world and lets the embed list the boards.
async function boot(options) {
  const world = makeWorld(options);
  await world.settle();
  return world;
}

// A frame says hello and receives its start message; returns the token.
async function startFrame(world, frame, ordinal) {
  const token = newToken();
  world.fromFrame(frame, "hello", { token, slot: ordinal });
  await world.settle();
  return token;
}

const goodRecord = (digest, extra) => ({
  format: 2, digest, measure_gen: 1,
  current: { elements: [{ id: "n1", type: "rectangle" }], appState: {}, files: {} },
  pristine: { elements: [] },
  ...extra,
});

// ----------------------------------------------------------------- runner

const tests = [];
const test = (name, fn) => tests.push({ name, fn });

// ------------------------------------------------------------------ tests

test("mounts one placement per block, in document order, with distinct titles", async () => {
  const w = await boot();
  const slots = w.slots();
  assert.strictEqual(slots.length, 3);
  assert.deepStrictEqual(w.callsOf("board.list").length, 1);
  const order = doc.body.children.map((c) => (c.className.includes("vxb-slot") ? "slot" : "block"));
  assert.deepStrictEqual(order, ["slot", "block", "slot", "block", "slot", "block"], "each slot sits right before its block");
  assert.ok(w.blocks.every((b) => b.style.display === "none"));
  const titles = slots.map((s) => s.querySelector("iframe").getAttribute("title"));
  assert.strictEqual(new Set(titles).size, 3, "titles tell the boards apart");
  slots.forEach((s, i) => {
    const frame = s.querySelector("iframe");
    assert.strictEqual(frame.src, "/whiteboard-frame?slot=" + i + "&palette=dark");
    assert.ok(frame.getAttribute("sandbox").includes("allow-scripts"));
    assert.ok(!frame.getAttribute("sandbox").includes("allow-same-origin"), "frames stay opaque-origin");
  });
});

test("the embed never calls the network itself", async () => {
  assert.ok(!/\bfetch\s*\(/.test(source) && !/XMLHttpRequest|sendBeacon|WebSocket|EventSource/.test(source));
  const w = await boot();
  await startFrame(w, w.inlineFrame(0), 0); // would throw through the sandbox fetch if it tried
});

test("a locked board offers a real button that unlocks that one board", async () => {
  const w = await boot();
  const [first, second] = [w.slots()[0], w.slots()[1]];
  const button = first.querySelector("button");
  assert.ok(button && button.getAttribute("type") === "button", "native button, so Enter and Space activate it");
  assert.ok(/edit/i.test(button.textContent) && /diagram 1 of 3/i.test(button.getAttribute("aria-label")));
  assert.ok(first.querySelector("iframe").getAttribute("tabindex") === "-1", "a locked frame is out of the tab order");
  assert.ok(first.querySelector(".vxb-cover"), "the cover sits over the canvas");
  button.click();
  assert.ok(!first.querySelector(".vxb-cover"), "unlocked");
  assert.strictEqual(first.querySelector("iframe").getAttribute("tabindex"), null);
  assert.ok(second.querySelector(".vxb-cover"), "the other boards stay locked");
});

test("a diagram whose text cannot be found is explained in place", async () => {
  const w = await boot({ listing: [{ ordinal: 0, text: "x", digest: "d" }] });
  const gone = w.slots()[1];
  assert.ok(!gone.querySelector("iframe"), "no frame for it");
  assert.ok(/no longer be found/.test(gone.textContent), gone.textContent);
  assert.ok(gone.textContent.includes("sequenceDiagram"), "the original text stays readable");
});

test("with no forum chrome behind it the board says why and keeps the text", async () => {
  const w = makeWorld({ failList: true });
  await w.settle();
  const slot = w.slots()[0];
  assert.ok(/could not start/i.test(slot.textContent) && /forum/.test(slot.textContent));
  assert.ok(slot.textContent.includes("flowchart LR"));
  assert.strictEqual(w.frames().length, 0);
});

test("the first hello binds the token; a second one with another token is ignored", async () => {
  const w = await boot();
  const frame = w.inlineFrame(1);
  const token = await startFrame(w, frame, 1);
  const starts = w.sent(frame, "start");
  assert.strictEqual(starts.length, 1);
  assert.strictEqual(starts[0].token, token, "every message to the frame carries its token");
  assert.strictEqual(starts[0].slot, 1);
  assert.strictEqual(starts[0].mode, "inline");
  assert.strictEqual(starts[0].text, "sequenceDiagram\n A->>B: hi");
  assert.strictEqual(starts[0].digest, "digest-1");
  assert.strictEqual(starts[0].record, null);
  assert.strictEqual(starts[0].palette, "dark");
  assert.strictEqual(starts[0].total, 3);
  assert.deepStrictEqual(w.callsOf("board.read").map((c) => c.payload.ordinal), [1]);

  w.fromFrame(frame, "hello", { token: newToken(), slot: 1 });
  await w.settle();
  assert.strictEqual(w.sent(frame, "start").length, 1, "no second start");
  // The impostor's token never works for later messages either.
  w.fromFrame(frame, "save", { token: "b".repeat(32), slot: 1, seq: 1, record: goodRecord("digest-1") });
  await w.settle();
  assert.strictEqual(w.callsOf("board.write").length, 0);
});

test("messages from a foreign window, with a short token or for another board are ignored", async () => {
  const w = await boot();
  const frame = w.inlineFrame(0);
  w.fire("message", { source: { postMessage() {} }, data: { type: "vxb1.hello", token: newToken(), slot: 0 } });
  w.fromFrame(frame, "hello", { token: "abc", slot: 0 });
  w.fromFrame(frame, "hello", { token: newToken(), slot: 2 });
  w.fire("message", { source: frame.win, data: "vxb1.hello" });
  w.fire("message", { source: frame.win, data: null });
  w.fire("message", { source: frame.win, data: { type: "other.hello", token: newToken(), slot: 0 } });
  await w.settle();
  assert.strictEqual(w.sent(frame, "start").length, 0);
  assert.strictEqual(w.callsOf("board.read").length, 0);
});

test("a stored record in the new format is handed to the frame, anything else is not", async () => {
  const w = await boot();
  w.store.set(0, goodRecord("digest-0"));
  w.store.set(1, { format: 1, digest: "x", current: {} });
  const a = w.inlineFrame(0), b = w.inlineFrame(1);
  await startFrame(w, a, 0);
  await startFrame(w, b, 1);
  assert.strictEqual(w.sent(a, "start")[0].record.digest, "digest-0");
  assert.strictEqual(w.sent(b, "start")[0].record, null);
});

test("autosave goes through board.write and the outcome is reported back to the frame", async () => {
  const w = await boot();
  const frame = w.inlineFrame(0);
  const token = await startFrame(w, frame, 0);
  const rec = goodRecord("digest-0");
  w.fromFrame(frame, "save", { token, slot: 0, seq: 1, record: rec });
  await w.settle();
  const write = w.callsOf("board.write")[0];
  assert.deepStrictEqual(write.payload, { ordinal: 0, body: rec });
  assert.deepStrictEqual(w.sent(frame, "saved").map((m) => [m.seq, m.ok]), [[1, true]]);

  w.knobs.failWrite = true;
  w.fromFrame(frame, "save", { token, slot: 0, seq: 2, record: rec });
  await w.settle();
  const failed = w.sent(frame, "saved")[1];
  assert.strictEqual(failed.ok, false);
  assert.ok(/disk full/.test(failed.error));

  w.fromFrame(frame, "save", { token, slot: 0, seq: 3, record: { format: 9 } });
  await w.settle();
  assert.strictEqual(w.sent(frame, "saved")[2].ok, false, "a malformed record is refused, not stored");
  assert.strictEqual(w.callsOf("board.write").length, 2);
});

test("writes of one board go out strictly in order", async () => {
  const w = await boot();
  const frame = w.inlineFrame(0);
  const token = await startFrame(w, frame, 0);
  let release;
  w.knobs.hold = new Promise((r) => (release = r));
  w.fromFrame(frame, "save", { token, slot: 0, seq: 1, record: goodRecord("digest-0") });
  w.fromFrame(frame, "save", { token, slot: 0, seq: 2, record: goodRecord("digest-0") });
  await w.settle();
  assert.strictEqual(w.callsOf("board.write").length, 1, "the second write waits for the first");
  w.knobs.hold = null;
  release();
  await w.settle();
  assert.strictEqual(w.callsOf("board.write").length, 2);
});

// Opens fullscreen for board 0 and returns what the tests need next.
async function openFullscreen(w, savedRecord) {
  const frame = w.inlineFrame(0);
  const token = await startFrame(w, frame, 0);
  w.fromFrame(frame, "expand", { token, slot: 0 });
  await w.settle();
  const snap = w.sent(frame, "snap")[0];
  assert.ok(snap && snap.freeze === true, "fullscreen first demands a frozen final state");
  w.fromFrame(frame, "final", { token, slot: 0, reqId: snap.reqId, record: savedRecord || null, unsaved: !!savedRecord });
  await w.settle();
  return { frame, token, snap };
}

test("fullscreen opens only after a successful final-state exchange and persists the record first", async () => {
  const w = await boot();
  assert.ok(!w.overlay());
  const rec = goodRecord("digest-0");
  const { frame } = await openFullscreen(w, rec);
  assert.deepStrictEqual(w.callsOf("board.write")[0].payload, { ordinal: 0, body: rec }, "persisted by the embed");
  const overlay = w.overlay();
  assert.ok(overlay, "overlay exists");
  assert.strictEqual(overlay.getAttribute("role"), "dialog");
  assert.strictEqual(overlay.getAttribute("aria-modal"), "true");
  assert.ok(overlay.getAttribute("aria-label"));
  const first = overlay.children[0].querySelector("button");
  assert.ok(first && /back/i.test(first.textContent), "the way back is a native button and first in the overlay");
  assert.ok(first.focused, "focus moves to it");
  assert.ok(frame.hasAttribute("inert"), "the inline copy is inert meanwhile");
  assert.ok(w.slots()[0].querySelector(".vxb-cover"), "and says it is open elsewhere");
  assert.ok(doc.body.children.filter((c) => c !== overlay).every((c) => c.hasAttribute("inert")), "the page is inert behind it");

  // The fullscreen frame starts from the handed-off state without asking the store.
  const full = w.overlayFrame();
  assert.strictEqual(full.src, "/whiteboard-frame?slot=0&palette=dark");
  const reads = w.callsOf("board.read").length;
  await startFrame(w, full, 0);
  const start = w.sent(full, "start")[0];
  assert.strictEqual(start.mode, "fullscreen");
  assert.deepStrictEqual(start.record, rec);
  assert.strictEqual(w.callsOf("board.read").length, reads);
});

test("only one fullscreen exists at a time", async () => {
  const w = await boot();
  await openFullscreen(w, null);
  const other = w.inlineFrame(1);
  const token = await startFrame(w, other, 1);
  w.fromFrame(other, "expand", { token, slot: 1 });
  await w.settle();
  assert.strictEqual(w.sent(other, "snap").length, 0, "a second request does not even start");
  assert.strictEqual(doc.body.children.filter((c) => c.id === "vxb-overlay").length, 1);
});

test("a frame that does not answer in time keeps fullscreen from opening and is thawed", async () => {
  const w = await boot();
  const frame = w.inlineFrame(0);
  const token = await startFrame(w, frame, 0);
  w.fromFrame(frame, "expand", { token, slot: 0 });
  await w.settle();
  const snap = w.sent(frame, "snap")[0];
  await w.tick(2999);
  assert.ok(!w.overlay() && w.sent(frame, "thaw").length === 0, "still waiting");
  await w.tick(2);
  assert.ok(!w.overlay(), "fullscreen did not proceed");
  assert.strictEqual(w.sent(frame, "thaw").length, 1, "the board is unfrozen");
  assert.ok(/fullscreen/i.test(w.slots()[0].querySelector(".vxb-note").textContent), "and the reviewer is told");
  // A late answer to the settled request changes nothing.
  w.fromFrame(frame, "final", { token, slot: 0, reqId: snap.reqId, record: goodRecord("digest-0"), unsaved: true });
  await w.settle();
  assert.ok(!w.overlay());
  assert.strictEqual(w.callsOf("board.write").length, 0, "the late record is not persisted");
});

test("an answer that is duplicated, unknown or malformed does nothing", async () => {
  const w = await boot();
  const frame = w.inlineFrame(0);
  const token = await startFrame(w, frame, 0);
  w.fromFrame(frame, "expand", { token, slot: 0 });
  await w.settle();
  const snap = w.sent(frame, "snap")[0];
  w.fromFrame(frame, "final", { token, slot: 0, reqId: snap.reqId + 99, record: goodRecord("digest-0"), unsaved: true });
  await w.settle();
  assert.ok(!w.overlay() && w.callsOf("board.write").length === 0, "unknown request id");
  w.fromFrame(frame, "final", { token, slot: 0, reqId: snap.reqId, record: { format: 7 }, unsaved: true });
  await w.settle();
  assert.ok(!w.overlay(), "a malformed state settles the request as failed");
  assert.strictEqual(w.sent(frame, "thaw").length, 1);
});

test("a failed persist of the final state unfreezes the board and keeps fullscreen closed", async () => {
  const w = await boot();
  w.knobs.failWrite = true;
  const frame = w.inlineFrame(0);
  const token = await startFrame(w, frame, 0);
  w.fromFrame(frame, "expand", { token, slot: 0 });
  await w.settle();
  const snap = w.sent(frame, "snap")[0];
  w.fromFrame(frame, "final", { token, slot: 0, reqId: snap.reqId, record: goodRecord("digest-0"), unsaved: true });
  await w.settle();
  assert.ok(!w.overlay());
  assert.strictEqual(w.sent(frame, "thaw").length, 1);
  assert.ok(/disk full/.test(w.slots()[0].querySelector(".vxb-note").textContent), "the reason is visible");
});

test("saves that arrive after a freeze are ignored", async () => {
  const w = await boot();
  const frame = w.inlineFrame(0);
  const token = await startFrame(w, frame, 0);
  w.fromFrame(frame, "expand", { token, slot: 0 });
  await w.settle();
  w.fromFrame(frame, "save", { token, slot: 0, seq: 7, record: goodRecord("digest-0") });
  await w.settle();
  assert.strictEqual(w.callsOf("board.write").length, 0);
  assert.strictEqual(w.sent(frame, "saved").length, 0);
});

test("going back persists the fullscreen state and the inline board starts over from it", async () => {
  const w = await boot();
  await openFullscreen(w, null);
  const oldInline = w.inlineFrame(0);
  const full = w.overlayFrame();
  const fullToken = await startFrame(w, full, 0);
  const back = w.overlay().children[0].querySelector("button");
  back.click();
  await w.settle();
  const snap = w.sent(full, "snap")[0];
  assert.ok(snap && snap.freeze === true);
  assert.ok(w.overlay(), "the overlay stays until the state is safe");
  const edited = goodRecord("digest-0", { current: { elements: [{ id: "edited", type: "rectangle" }], appState: {}, files: {} } });
  w.fromFrame(full, "final", { token: fullToken, slot: 0, reqId: snap.reqId, record: edited, unsaved: true });
  await w.settle();
  assert.ok(!w.overlay(), "overlay gone");
  assert.ok(doc.body.children.every((c) => !c.hasAttribute("inert")), "the page is live again");
  const fresh = w.inlineFrame(0);
  assert.notStrictEqual(fresh, oldInline, "the inline board is a new placement");
  assert.strictEqual(w.store.get(0).current.elements[0].id, "edited");
  await startFrame(w, fresh, 0);
  assert.strictEqual(w.sent(fresh, "start")[0].record.current.elements[0].id, "edited", "it shows what fullscreen saved");
  assert.strictEqual(w.sent(fresh, "start")[0].restoreFocus, true, "focus returns to the control that opened fullscreen");
});

test("Escape goes back through the same safe path, from the page or from the frame", async () => {
  const w = await boot();
  await openFullscreen(w, null);
  const full = w.overlayFrame();
  const token = await startFrame(w, full, 0);
  let prevented = 0;
  w.fireDoc("keydown", { key: "Escape", defaultPrevented: false, preventDefault() { prevented++; } });
  await w.settle();
  assert.strictEqual(prevented, 1);
  assert.strictEqual(w.sent(full, "snap").length, 1, "Escape asks for the final state first");
  assert.ok(w.overlay());

  const w2 = await boot();
  await openFullscreen(w2, null);
  const full2 = w2.overlayFrame();
  const token2 = await startFrame(w2, full2, 0);
  w2.fromFrame(full2, "leave", { token: token2, slot: 0 });
  await w2.settle();
  assert.strictEqual(w2.sent(full2, "snap").length, 1, "Escape pressed inside the frame takes the same path");
  void token;
});

test("when the fullscreen frame stays silent, leaving needs a deliberate second action", async () => {
  const w = await boot();
  await openFullscreen(w, null);
  const full = w.overlayFrame();
  await startFrame(w, full, 0);
  w.overlay().children[0].querySelector("button").click();
  await w.settle();
  await w.tick(3001);
  const overlay = w.overlay();
  assert.ok(overlay, "still open: nothing was saved");
  const leave = overlay.querySelector(".vxb-leave");
  assert.ok(leave && leave.hidden === false, "an explicit leave-without-saving control appears");
  assert.ok(/without saving/i.test(leave.textContent));
  assert.strictEqual(w.sent(full, "thaw").length, 1, "the fullscreen board is editable again");
  leave.click();
  await w.settle();
  assert.ok(!w.overlay(), "the second action leaves");
  assert.ok(w.inlineFrame(0), "and the inline board is back");
});

test("a fullscreen frame that cannot start shows why and its way back still works", async () => {
  const w = await boot();
  await openFullscreen(w, null);
  await w.tick(45001);
  const overlay = w.overlay();
  const note = overlay.querySelector(".vxb-overlay-note");
  assert.ok(note && note.hidden === false && /vx forum stop/.test(note.textContent));
  overlay.children[0].querySelector("button").click();
  await w.settle();
  assert.ok(!w.overlay(), "a frame that never started has nothing to save, so back leaves at once");
});

test("a frame that never reports in is called broken after the startup wait, with the text kept", async () => {
  const w = await boot();
  const slot = w.slots()[0];
  await startFrame(w, w.inlineFrame(1), 1); // a board that did report in is not touched by its timer
  assert.ok(/loading/i.test(slot.querySelector(".vxb-note").textContent));
  await w.tick(44999);
  assert.ok(slot.querySelector("iframe"));
  await w.tick(2);
  assert.ok(!slot.querySelector("iframe"), "no empty box left behind");
  assert.ok(/reload/i.test(slot.textContent) && /vx forum stop/.test(slot.textContent));
  assert.ok(slot.textContent.includes("flowchart LR"), "the diagram text is still readable");
  assert.ok(w.slots()[1].querySelector("iframe"));
});

test("a frame that reported in is never called broken", async () => {
  const w = await boot();
  const frame = w.inlineFrame(0);
  await startFrame(w, frame, 0);
  await w.tick(60000);
  assert.ok(w.slots()[0].querySelector("iframe"));
});

test("leaving the page asks every live frame for a final state without waiting", async () => {
  const w = await boot();
  const a = w.inlineFrame(0), b = w.inlineFrame(1);
  const ta = await startFrame(w, a, 0);
  await startFrame(w, b, 1);
  w.fire("pagehide", {});
  await w.settle();
  const snapA = w.sent(a, "snap")[0];
  assert.ok(snapA && snapA.freeze === false, "unload does not freeze or block");
  assert.strictEqual(w.sent(b, "snap").length, 1);
  const rec = goodRecord("digest-0");
  w.fromFrame(a, "final", { token: ta, slot: 0, reqId: snapA.reqId, record: rec, unsaved: true });
  await w.settle();
  assert.deepStrictEqual(w.callsOf("board.write")[0].payload, { ordinal: 0, body: rec });
  // A frame with nothing unsaved costs no write.
  const snapB = w.sent(b, "snap")[0];
  w.fromFrame(b, "final", { token: w.sent(b, "start")[0].token, slot: 1, reqId: snapB.reqId, record: goodRecord("digest-1"), unsaved: false });
  await w.settle();
  assert.strictEqual(w.callsOf("board.write").length, 1);
});

test("hiding the tab flushes too", async () => {
  const w = await boot();
  const a = w.inlineFrame(0);
  await startFrame(w, a, 0);
  doc.visibilityState = "hidden";
  w.fireDoc("visibilitychange", {});
  await w.settle();
  assert.strictEqual(w.sent(a, "snap").length, 1);
});

test("a theme change repaints the embed and reaches every started frame", async () => {
  const w = await boot();
  const a = w.inlineFrame(0), c = w.inlineFrame(2);
  await startFrame(w, a, 0);
  assert.strictEqual(w.slots()[0].getAttribute("data-vxb-palette"), "dark");
  w.flipTheme("light");
  await w.settle();
  w.slots().forEach((s) => {
    assert.strictEqual(s.getAttribute("data-vxb-palette"), "light");
    assert.strictEqual(s.querySelector("iframe").style.colorScheme, "light");
  });
  const pal = w.sent(a, "palette");
  assert.strictEqual(pal.length, 1);
  assert.strictEqual(pal[0].palette, "light");
  assert.strictEqual(w.sent(c, "palette").length, 0, "a frame that has not started gets it in its start message");
  await startFrame(w, c, 2);
  assert.strictEqual(w.sent(c, "start")[0].palette, "light");
  w.flipTheme("light");
  await w.settle();
  assert.strictEqual(w.sent(a, "palette").length, 1, "no change, no message");
});

test("a theme change reaches the fullscreen overlay and its frame", async () => {
  const w = await boot();
  await openFullscreen(w, null);
  const full = w.overlayFrame();
  await startFrame(w, full, 0);
  w.flipTheme("light");
  await w.settle();
  assert.strictEqual(w.overlay().getAttribute("data-vxb-palette"), "light");
  assert.strictEqual(full.style.colorScheme, "light");
  assert.strictEqual(w.sent(full, "palette")[0].palette, "light");
});

test("with no theme attribute the embed falls back to the OS preference and sets the attribute", async () => {
  const w = await boot({ theme: null });
  assert.strictEqual(doc.documentElement.getAttribute("data-fr-theme"), "dark", "the stub matchMedia says not light");
  assert.strictEqual(w.slots()[0].getAttribute("data-vxb-palette"), "dark");
});

test("Queue feedback forwards the remark and edit lines to board.submit, bounded", async () => {
  const w = await boot();
  const frame = w.inlineFrame(0);
  const token = await startFrame(w, frame, 0);
  const lines = Array.from({ length: 80 }, (_, i) => "line " + i + " " + "x".repeat(400));
  w.fromFrame(frame, "submit", {
    token, slot: 0, reqId: 11,
    current: { elements: [], appState: {}, files: {} },
    png: "data:image/png;base64,AAAA",
    editLines: lines,
    remark: "r".repeat(5000),
  });
  await w.settle();
  const call = w.callsOf("board.submit")[0];
  assert.strictEqual(call.payload.ordinal, 0);
  const body = call.payload.body;
  assert.strictEqual(body.edit_lines.length, 50);
  assert.ok(body.edit_lines.every((l) => l.length <= 300));
  assert.strictEqual(body.remark.length, 1000);
  assert.strictEqual(body.png, "data:image/png;base64,AAAA");
  assert.deepStrictEqual(w.sent(frame, "queued").map((m) => [m.reqId, m.ok]), [[11, true]]);
});

test("Queue feedback drops a preview that is not a PNG data URL, and keeps the remark", async () => {
  const w = await boot();
  const frame = w.inlineFrame(0);
  const token = await startFrame(w, frame, 0);
  w.fromFrame(frame, "submit", { token, slot: 0, reqId: 1, current: { elements: [] }, png: "data:text/html;base64,AAAA", editLines: ["a", 5, null], remark: "keep me" });
  await w.settle();
  const body = w.callsOf("board.submit")[0].payload.body;
  assert.ok(!("png" in body));
  assert.deepStrictEqual(body.edit_lines, ["a"]);
  assert.strictEqual(body.remark, "keep me");
});

test("Queue feedback failure is reported to the frame, and a second press while one is in flight does nothing", async () => {
  const w = await boot();
  const frame = w.inlineFrame(0);
  const token = await startFrame(w, frame, 0);
  let release;
  w.knobs.hold = new Promise((r) => (release = r));
  const msg = { token, slot: 0, current: { elements: [] }, editLines: [], remark: "" };
  w.fromFrame(frame, "submit", { ...msg, reqId: 1 });
  w.fromFrame(frame, "submit", { ...msg, reqId: 2 });
  await w.settle();
  assert.strictEqual(w.callsOf("board.submit").length, 1, "one request in flight");
  const refused = w.sent(frame, "queued").find((m) => m.reqId === 2);
  assert.ok(refused && refused.ok === false);
  w.knobs.hold = null;
  w.knobs.failSubmit = true;
  release();
  await w.settle();
  const first = w.sent(frame, "queued").find((m) => m.reqId === 1);
  assert.strictEqual(first.ok, false);
  assert.ok(/session ended/.test(first.error), "the server's reason reaches the frame");
  // The slot is free again for the retry.
  w.knobs.failSubmit = false;
  w.fromFrame(frame, "submit", { ...msg, reqId: 3 });
  await w.settle();
  assert.strictEqual(w.sent(frame, "queued").find((m) => m.reqId === 3).ok, true);
});

test("a submit with no scene is refused", async () => {
  const w = await boot();
  const frame = w.inlineFrame(0);
  const token = await startFrame(w, frame, 0);
  w.fromFrame(frame, "submit", { token, slot: 0, reqId: 1, editLines: [], remark: "" });
  await w.settle();
  assert.strictEqual(w.callsOf("board.submit").length, 0);
  assert.strictEqual(w.sent(frame, "queued")[0].ok, false);
});

test("a page with no Mermaid blocks is left alone", async () => {
  const w = makeWorld({ blocks: [] });
  await w.settle();
  assert.strictEqual(w.callsOf("board.list").length, 0);
  assert.strictEqual(w.frames().length, 0);
});

// ------------------------------------------------------------------ run

(async () => {
  let failed = 0;
  for (const { name, fn } of tests) {
    try {
      await fn();
    } catch (error) {
      failed++;
      console.error("FAIL " + name + "\n  " + String((error && error.stack) || error).split("\n").slice(0, 6).join("\n  "));
    }
  }
  if (failed) {
    console.error(failed + " of " + tests.length + " embed checks failed");
    process.exit(1);
  }
  console.log("ok " + tests.length + " embed checks");
})();
