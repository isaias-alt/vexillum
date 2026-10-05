// Run by layout_dom_test.go: when the audit of assets/chrome/forum-layout.js
// runs and what it sends, against a fake document and fake timers. Geometry is
// covered elsewhere; here the page's width is the only thing that can fail.
// Prints "ok" or throws.
const fs = require("fs");
const vm = require("vm");
const assert = require("assert");

const source = fs.readFileSync(process.argv[2], "utf8");
const tick = () => new Promise((resolve) => setImmediate(resolve));

function makeWorld({ framed = true, version = "v-test" } = {}) {
  let clock = 0;
  let nextId = 1;
  const timers = new Map();
  const posts = [];
  const writes = [];
  const listeners = {};
  const observers = [];

  const guard = (name, target) =>
    new Proxy(target, {
      set(_, prop) { writes.push(name + "." + String(prop)); return true; },
      get(t, prop) {
        if (["appendChild", "setAttribute", "removeAttribute", "insertBefore", "remove", "scrollTo", "scrollBy", "focus", "click"].includes(prop)) return () => writes.push(name + "()." + prop);
        return t[prop];
      },
    });

  const root = { clientWidth: 1100, clientHeight: 800, scrollHeight: 800, scrollWidth: 1100, clientLeft: 0, clientTop: 0, parentElement: null, nodeType: 1, hasAttribute: () => false, getAttribute: () => null };
  const bodyEl = { clientWidth: 1100, clientHeight: 800, parentElement: root, nodeType: 1, hasAttribute: () => false, getAttribute: () => null };
  const world = { posts, writes, listeners, observers, root, bodyEl };

  const win = {
    parent: null,
    innerWidth: 1100, innerHeight: 800, scrollX: 0, scrollY: 0,
    addEventListener(type, fn) { (listeners[type] ||= []).push(fn); },
    removeEventListener() {},
    getComputedStyle: () => ({ overflowX: "visible", overflowY: "visible", opacity: "1", visibility: "visible", position: "static" }),
    setTimeout(fn, ms) { const id = nextId++; timers.set(id, { at: clock + (ms || 0), fn }); return id; },
    clearTimeout(id) { timers.delete(id); },
    requestAnimationFrame(fn) { return win.setTimeout(() => fn(clock), 16); },
    MutationObserver: class { constructor(cb) { this.cb = cb; observers.push(this); } observe() {} disconnect() {} },
    Date: { now: () => clock },
    URL, Promise, Map, Set, WeakMap, Math, Number, JSON, Object, Array, Infinity,
    postMessage() { throw new Error("the audit must only post to its parent"); },
  };
  win.parent = framed ? { postMessage(message, target) { posts.push({ message, target }); } } : win;
  win.window = win;
  win.document = {
    readyState: "complete",
    fonts: { ready: Promise.resolve(), status: "loaded" },
    getAnimations: () => [],
    documentElement: guard("html", root),
    scrollingElement: guard("html", root),
    body: guard("body", bodyEl),
    createTreeWalker: () => ({ nextNode: () => null }),
    querySelectorAll: () => [],
    createRange: () => ({ selectNodeContents() {} }),
    elementFromPoint: () => null,
    getElementById: () => null,
    currentScript: { src: "http://localhost/forum-assets/forum-layout.js?av=" + version },
    addEventListener() { writes.push("document.addEventListener"); },
  };
  vm.createContext(win);

  world.win = win;
  world.start = () => vm.runInContext(source, win);
  world.now = () => clock;
  // Advance fake time in small steps, letting promises settle between timers.
  world.advance = async (ms) => {
    const end = clock + ms;
    for (;;) {
      await tick();
      let due = null;
      for (const [id, t] of timers) if (t.at <= end && (!due || t.at < due.t.at)) due = { id, t };
      if (!due) break;
      timers.delete(due.id);
      clock = Math.max(clock, due.t.at);
      due.t.fn();
    }
    clock = end;
    await tick();
  };
  world.mutate = (inForumUi = false) => {
    for (const o of observers) o.cb([{ type: "childList", target: { nodeType: 1, closest: () => (inForumUi ? {} : null), parentElement: null } }]);
  };
  world.resize = (width) => { win.innerWidth = width; for (const fn of listeners.resize || []) fn({ type: "resize" }); };
  return world;
}

const wire = (p) => p.message;

(async () => {
  // 1. One audit after the page settles, and it says what it found.
  {
    const w = makeWorld();
    w.root.scrollWidth = 2400;
    w.start();
    await w.advance(100);
    assert.strictEqual(w.posts.length, 0, "nothing is sent before the page has settled");
    await w.advance(3000);
    assert.strictEqual(w.posts.length, 1, "exactly one audit after settle");
    const m = wire(w.posts[0]);
    assert.strictEqual(m.type, "forum:layout");
    assert.strictEqual(m.artifact_version, "v-test", "stamped with the version the server put in the script address");
    assert.strictEqual(m.complete, true);
    assert.strictEqual(m.target_presence_complete, true);
    assert.strictEqual(m.viewport_width, 1100);
    assert.deepStrictEqual(JSON.parse(JSON.stringify(m.findings)), [{ kind: "wide-page", selector: "", axis: "horizontal", overflow_px: 1300 }]);
    assert.strictEqual(w.posts[0].target, "*");
    assert.deepStrictEqual(w.writes, [], "the audit never writes to the artifact");
    // Nothing else is audited later on its own, whatever the page does.
    w.mutate();
    await w.advance(5000);
    assert.strictEqual(w.posts.length, 1, "a mutation after the pass does not start another");
  }

  // 2. Two observations must agree: a finding in only one sample is dropped.
  {
    const w = makeWorld();
    w.root.scrollWidth = 2400;
    w.start();
    // The first observation happens after the quiet window; the second after
    // two frames and a pause. Make the page fine from 600 ms on.
    await w.advance(560);
    w.root.scrollWidth = 1100;
    await w.advance(3000);
    assert.strictEqual(w.posts.length, 1);
    assert.deepStrictEqual(JSON.parse(JSON.stringify(wire(w.posts[0]).findings)), [], "present in one sample only: not reported");
  }

  // 3. Resize is debounced, supersedes a pass in flight, and only that listener exists.
  {
    const w = makeWorld();
    w.root.scrollWidth = 2400;
    w.start();
    await w.advance(200); // still settling
    w.resize(900);
    await w.advance(100);
    w.resize(950);
    w.resize(1000);
    await w.advance(5000);
    assert.strictEqual(w.posts.length, 1, "the pass in flight was superseded and the burst of resizes ran once");
    assert.strictEqual(wire(w.posts[0]).viewport_width, 1000);
    // And after a pass, a resize burst yields exactly one more.
    for (let i = 0; i < 6; i++) { w.resize(1200 + i); await w.advance(50); }
    assert.strictEqual(w.posts.length, 1, "no audit while the window is still being dragged");
    await w.advance(5000);
    assert.strictEqual(w.posts.length, 2);
    assert.strictEqual(wire(w.posts[1]).viewport_width, 1205);
    assert.deepStrictEqual(Object.keys(w.listeners), ["resize"], "the audit listens to resize and nothing else (no hover, animation or mutation-driven audits)");
  }

  // 4. Identical consecutive results are not re-sent.
  {
    const w = makeWorld();
    w.start();
    await w.advance(3000);
    assert.strictEqual(w.posts.length, 1);
    w.resize(1100); // same width, same page: same result
    await w.advance(5000);
    assert.strictEqual(w.posts.length, 1, "an identical result is not sent twice");
    w.root.scrollWidth = 2000;
    w.resize(1100);
    await w.advance(5000);
    assert.strictEqual(w.posts.length, 2, "a different result is");
  }

  // 5. A pass that throws is sent once as uncertainty, never as proof.
  {
    const w = makeWorld();
    w.win.document.createTreeWalker = () => { throw new Error("boom"); };
    w.start();
    await w.advance(5000);
    assert.strictEqual(w.posts.length, 1);
    const m = wire(w.posts[0]);
    assert.strictEqual(m.complete, false);
    assert.strictEqual(m.target_presence_complete, false);
    assert.deepStrictEqual(JSON.parse(JSON.stringify(m.findings)), []);
    w.resize(1100);
    await w.advance(5000);
    assert.strictEqual(w.posts.length, 1, "the same failure is not sent again");
  }

  // 6. A page that never settles still gets a pass, marked incomplete, after the cap.
  {
    const w = makeWorld();
    w.root.scrollWidth = 2400;
    w.start();
    for (let t = 0; t < 12000; t += 100) { w.mutate(); await w.advance(100); }
    assert.strictEqual(w.posts.length, 1, "one pass once the cap is reached");
    const m = wire(w.posts[0]);
    assert.strictEqual(m.complete, false);
    assert.strictEqual(m.target_presence_complete, false, "a page still mutating cannot prove an element is gone");
  }

  // 7. The chrome's own injected UI never delays or taints an audit.
  {
    const w = makeWorld();
    w.start();
    for (let t = 0; t < 3000; t += 100) { w.mutate(true); await w.advance(100); }
    assert.strictEqual(w.posts.length, 1);
    assert.strictEqual(wire(w.posts[0]).complete, true, "mutations inside [data-forum-ui] are not the page changing");
  }

  // 8. Still loading: the audit waits for load, finite animations, and fonts.
  {
    const w = makeWorld();
    w.win.document.readyState = "loading";
    w.start();
    await w.advance(5000);
    assert.strictEqual(w.posts.length, 0, "not before the page has loaded");
    w.win.document.readyState = "complete";
    for (const fn of w.listeners.load || []) fn({ type: "load" });
    await w.advance(3000);
    assert.strictEqual(w.posts.length, 1);
    assert.strictEqual(wire(w.posts[0]).complete, true);
  }
  {
    // A running finite animation holds the audit until it ends.
    const w = makeWorld();
    let finish;
    const finished = new Promise((r) => { finish = r; });
    let running = true;
    w.win.document.getAnimations = () => (running ? [{ playState: "running", finished, effect: { getComputedTiming: () => ({ endTime: 1000 }), target: null } }] : []);
    w.start();
    await w.advance(2000);
    assert.strictEqual(w.posts.length, 0, "a finite animation is still running");
    running = false;
    finish();
    await w.advance(3000);
    assert.strictEqual(w.posts.length, 1);
    assert.strictEqual(wire(w.posts[0]).complete, true);
  }

  // A tab nobody is looking at paints no frames; the audit must not wait for one.
  {
    const w = makeWorld();
    w.win.requestAnimationFrame = () => 0; // never fires
    w.root.scrollWidth = 2400;
    w.start();
    await w.advance(4000);
    assert.strictEqual(w.posts.length, 1, "a background tab is still audited");
    assert.strictEqual(wire(w.posts[0]).complete, true);
  }

  // 9. Outside the chrome nothing runs and nothing is posted.
  {
    const w = makeWorld({ framed: false });
    w.start();
    await w.advance(5000);
    assert.strictEqual(w.posts.length, 0);
    assert.ok(w.win.__forumLayoutKit, "the test kit is exposed instead");
  }

  console.log("ok");
  process.exit(0);
})().catch((err) => {
  console.error(err);
  process.exit(1);
});
