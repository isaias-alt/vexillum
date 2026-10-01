// A tiny fake browser for running assets/chrome/forum-chrome.js under node:
// every element is an auto-created stub that records what the script does to
// it, and the server's live feed is a queue the test pushes snapshots into.
// There is no layout or rendering here; real-browser behavior is checked by
// hand against the built binary. Shared by the chrome_*_test.js files.
const fs = require("fs");
const vm = require("vm");

function makeEnv(chromePath, boot, opts = {}) {
  const elements = new Map();
  const docListeners = {};
  const winListeners = {};
  const doc = { activeElement: null, body: null };

  function stub(id) {
    const listeners = {};
    const el = {
      id, hidden: false, inert: false, disabled: false, value: "", textContent: "", dataset: {}, style: {},
      className: "", title: "", src: "", href: "", type: "", checked: false, files: [],
      children: [], attrs: {}, listeners, isConnected: true,
      offsetWidth: 0, offsetHeight: 0, scrollHeight: 0, scrollTop: 0, clientHeight: 0,
      classList: { add() {}, remove() {}, toggle() {}, contains() { return false; } },
      setAttribute(k, v) { this.attrs[k] = String(v); },
      getAttribute(k) { return k in this.attrs ? this.attrs[k] : null; },
      removeAttribute(k) { delete this.attrs[k]; },
      addEventListener(type, fn, opts) { (listeners[type] ||= []).push({ fn, opts }); },
      removeEventListener() {},
      append(...nodes) { this.children.push(...nodes); },
      appendChild(n) { this.children.push(n); return n; },
      replaceChildren(...nodes) { this.children = nodes; },
      remove() { this.isConnected = false; },
      focus() { doc.activeElement = this; },
      blur() { if (doc.activeElement === this) doc.activeElement = null; },
      click() { for (const l of listeners.click || []) l.fn({ target: this, preventDefault() {}, stopPropagation() {} }); },
      contains(n) { return n === this || this.children.some((c) => c.contains && c.contains(n)); },
      closest() { return null; },
      querySelector() { return null; },
      querySelectorAll() { return []; },
      getBoundingClientRect() { return { left: 0, top: 0, width: 0, height: 0 }; },
      contentWindow: { postMessage() {} },
      select() {}, scrollIntoView() {},
    };
    return el;
  }
  const get = (id) => {
    if (!elements.has(id)) elements.set(id, stub(id));
    return elements.get(id);
  };
  get("forum-boot").textContent = JSON.stringify(boot);
  const app = stub("app");
  elements.set("__app", app);

  Object.assign(doc, {
    getElementById: get,
    querySelector: (sel) => (sel === ".app" ? app : stub(sel)),
    querySelectorAll: () => [],
    createElement: (tag) => Object.assign(stub(tag), { tagName: tag.toUpperCase() }),
    createTextNode: (text) => ({ nodeType: 3, textContent: text }),
    createDocumentFragment: () => stub("fragment"),
    createRange: () => ({ selectNodeContents() {} }),
    addEventListener(type, fn, opts) { (docListeners[type] ||= []).push({ fn, opts }); },
    documentElement: stub("html"),
  });

  // The live feed is an SSE stream: push() delivers one "state" event on the
  // current stream; drop() ends it (a server that went away); failNext(n)
  // makes the next n connection attempts fail.
  const encoder = new TextEncoder();
  const calls = [];
  const streams = []; // open streams: {chunks, readers}
  let failures = opts.failures || 0; // connection attempts that fail before one succeeds
  const backlog = [];
  const makeStream = () => {
    const stream = { queue: [], waiting: [], closed: false };
    streams.push(stream);
    for (const chunk of backlog.splice(0)) stream.queue.push(chunk);
    return {
      ok: true,
      status: 200,
      body: {
        getReader: () => ({
          read: () =>
            new Promise((resolve) => {
              if (stream.queue.length) resolve({ done: false, value: stream.queue.shift() });
              else if (stream.closed) resolve({ done: true });
              else stream.waiting.push(resolve);
            }),
        }),
      },
    };
  };
  const deliver = (chunk) => {
    const open = streams.filter((s) => !s.closed);
    if (!open.length) return backlog.push(chunk);
    const stream = open[open.length - 1];
    if (stream.waiting.length) stream.waiting.shift()({ done: false, value: chunk });
    else stream.queue.push(chunk);
  };
  const fetchStub = (url, init) => {
    calls.push({ url: String(url), init });
    if (String(url).endsWith("/events")) {
      if (failures > 0) {
        failures -= 1;
        return Promise.reject(new Error("connection refused"));
      }
      return Promise.resolve(makeStream());
    }
    return Promise.resolve({ ok: true, status: 200, json: async () => ({}), blob: async () => ({}) });
  };

  const win = {
    document: doc,
    addEventListener(type, fn) { (winListeners[type] ||= []).push(fn); },
    removeEventListener() {},
    forumTheme: { current: () => "dark", toggle() {} },
    forumPrefs: { annotate: () => true, setAnnotate() {} },
    location: { href: "http://127.0.0.1/session/x" },
    innerWidth: 1200, innerHeight: 800,
    getSelection: () => ({ removeAllRanges() {}, addRange() {} }),
  };
  const sandbox = {
    window: win, document: doc, fetch: fetchStub, console, URL: Object.assign(function URL_(...a) { return new URL(...a); }, { createObjectURL: () => 'blob:stub', revokeObjectURL() {} }), Promise, Date, JSON, Math, Map, Set, Object, Array, String, Number, RegExp, Error, Intl,
    setTimeout, clearTimeout, setInterval, clearInterval, queueMicrotask,
    navigator: { clipboard: { writeText: async () => {} } },
    TextDecoder, TextEncoder, AbortController, Blob,
  };
  win.window = win;
  Object.assign(win, sandbox);
  vm.createContext(sandbox);
  sandbox.window = win;
  vm.runInContext(fs.readFileSync(chromePath, "utf8"), sandbox, { filename: chromePath });

  return {
    get,
    app,
    doc,
    calls,
    docListeners,
    winListeners,
    // push delivers one snapshot as the server's answer to the live feed.
    push(snapshot) {
      deliver(encoder.encode("id: " + snapshot.version + "\nevent: state\ndata: " + JSON.stringify(snapshot) + "\n\n"));
    },
    ping() {
      deliver(encoder.encode(": ping\n\n"));
    },
    drop() {
      for (const s of streams) {
        s.closed = true;
        for (const w of s.waiting.splice(0)) w({ done: true });
      }
    },
    failNext(n) {
      failures = n;
    },
    connections: () => calls.filter((c) => c.url.endsWith("/events")).length,
    tick: () => new Promise((resolve) => setTimeout(resolve, 20)),
    key(event) {
      const e = { defaultPrevented: false, stopped: false, preventDefault() { this.defaultPrevented = true; }, stopImmediatePropagation() { this.stopped = true; }, stopPropagation() {}, ...event };
      for (const l of docListeners.keydown || []) {
        if (e.stopped) break;
        l.fn(e);
      }
      return e;
    },
  };
}

module.exports = { makeEnv };
