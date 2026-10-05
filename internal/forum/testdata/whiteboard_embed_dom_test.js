// Run by whiteboard_embed_test.go: drives assets/whiteboard-embed.js against a
// small fake DOM and a scripted forum chrome (window.forum.__rpc), playing the
// part of the whiteboard frames by hand. Prints "ok" or throws.
const fs = require("fs");
const assert = require("assert");
const vm = require("vm");

const source = fs.readFileSync(process.argv[2], "utf8");

// Objects built inside the vm context have that context's prototypes, which
// deepStrictEqual would call different; compare their JSON form instead.
const plain = (value) => (value === undefined ? undefined : JSON.parse(JSON.stringify(value)));
const sameJSON = (actual, expected, message) => assert.deepStrictEqual(plain(actual), plain(expected), message);
const tick = () => new Promise((resolve) => setImmediate(resolve));
const settle = async () => {
  for (let i = 0; i < 6; i++) await tick();
};

// ----------------------------------------------------------------- fake DOM

function makeWorld({ diagrams = 2, rpc } = {}) {
  const listeners = { window: {}, document: {} };
  const observers = [];
  const timers = [];
  let timerId = 0;
  let now = 0;
  const rootAttrs = { "data-fr-theme": "dark" }; // the server renders the chrome's theme on <html>

  const on = (bucket) => (type, fn) => {
    (listeners[bucket][type] = listeners[bucket][type] || []).push(fn);
  };

  function element(tag) {
    const node = {
      tag,
      style: {},
      children: [],
      attrs: {},
      textContent: "",
      append(...kids) {
        this.children.push(...kids);
      },
      setAttribute(k, v) {
        this.attrs[k] = String(v);
      },
      getAttribute(k) {
        return k in this.attrs ? this.attrs[k] : null;
      },
      removeAttribute(k) {
        delete this.attrs[k];
        delete this[k];
      },
      replaceWith(other) {
        this.replacedBy = other;
      },
    };
    if (tag === "iframe") {
      const received = [];
      const srcSets = [];
      let src = "";
      Object.defineProperty(node, "src", {
        get: () => src,
        set: (value) => {
          src = value;
          srcSets.push(value);
        },
      });
      node.srcSets = srcSets;
      node.received = received;
      node.contentWindow = { postMessage: (message) => received.push(message) };
    }
    return node;
  }

  const body = element("body");
  const containers = Array.from({ length: diagrams }, () => element("div"));
  const document = {
    readyState: "complete",
    body,
    documentElement: {
      getAttribute: (k) => (k in rootAttrs ? rootAttrs[k] : null),
      setAttribute: (k, v) => {
        rootAttrs[k] = String(v);
        observers.forEach((notify) => notify());
      },
    },
    createElement: element,
    querySelectorAll: (selector) => (selector === ".mermaid" ? containers : []),
    addEventListener: on("document"),
  };

  const calls = [];
  const win = {
    document,
    addEventListener: on("window"),
    setTimeout: (fn, ms) => {
      timers.push({ id: ++timerId, at: now + ms, fn });
      return timerId;
    },
    clearTimeout: (id) => {
      const at = timers.findIndex((t) => t.id === id);
      if (at >= 0) timers.splice(at, 1);
    },
    matchMedia: () => ({ matches: false, addEventListener() {} }),
    forum: {
      __rpc: async (op, payload) => {
        calls.push({ op, payload });
        return rpc(op, payload);
      },
    },
  };
  win.window = win;
  const MutationObserver = function (callback) {
    return { observe: () => observers.push(callback) };
  };
  win.MutationObserver = MutationObserver;
  vm.runInNewContext(source, { window: win, document, MutationObserver, Array, Promise, String, Number, Object, encodeURIComponent });

  const world = {
    document,
    calls,
    containers,
    iframes: containers.map((c) => c.replacedBy.children[0]),
    wrappers: containers.map((c) => c.replacedBy),
    overlay: () => body.children.find((c) => c.id === "vxWhiteboardOverlay"),
    fire: (type, event) => (listeners.window[type] || []).forEach((fn) => fn(event)),
    fromFrame(iframe, type, fields) {
      this.fire("message", { source: iframe.contentWindow, data: { type: "vx-whiteboard:" + type, ...fields } });
    },
    advance(ms) {
      now += ms;
      for (const due of timers.filter((t) => t.at <= now).sort((a, b) => a.at - b.at)) {
        const at = timers.indexOf(due);
        if (at >= 0) {
          timers.splice(at, 1);
          due.fn();
        }
      }
    },
    setTheme: (theme) => document.documentElement.setAttribute("data-fr-theme", theme),
    ofType: (iframe, type) => iframe.received.filter((m) => m.type === "vx-whiteboard:" + type),
  };
  return world;
}

const defaultRpc = (op, payload) => {
  if (op === "whiteboard.sources") {
    return { sources: [{ index: 0, source: "flowchart LR\n A-->B", hash: "h0" }, { index: 1, source: "classDiagram\n A<|--B", hash: "h1" }] };
  }
  if (op === "whiteboard.load") return { whiteboard: payload.index === 1 ? { source_hash: "h1", scene: { elements: [] } } : null };
  return {};
};

async function ready(world, i, channel) {
  world.fromFrame(world.iframes[i], "ready", { diagramIndex: i, channelId: channel });
  await settle();
}

(async () => {
  // ---- containers become boards, in document order
  {
    const world = makeWorld({ diagrams: 3, rpc: defaultRpc });
    assert.strictEqual(world.iframes.length, 3);
    world.iframes.forEach((iframe, i) => {
      assert.strictEqual(iframe.src, "/whiteboard-frame?diagramIndex=" + i + "&theme=dark");
      assert.strictEqual(iframe.sandbox, "allow-scripts allow-popups");
      assert.strictEqual(iframe.style.colorScheme, "dark");
      assert.ok(iframe.style.border.includes("var(--fr-border-strong, #3A3F47)"), iframe.style.border);
      const status = world.wrappers[i].children[1];
      assert.strictEqual(status.attrs.role, "status");
      assert.strictEqual(status.textContent, "Loading whiteboard...");
      assert.strictEqual(status.style.display, "flex");
    });
    assert.strictEqual(world.wrappers[0].style.position, "relative");
  }

  // ---- handshake: ready -> source + saved scene lookups -> init
  {
    const world = makeWorld({ rpc: defaultRpc });
    // a "ready" from an unrelated window is ignored
    world.fire("message", { source: {}, data: { type: "vx-whiteboard:ready", diagramIndex: 0, channelId: "x" } });
    // so is a ready that names another diagram
    world.fromFrame(world.iframes[0], "ready", { diagramIndex: 1, channelId: "x" });
    await settle();
    assert.strictEqual(world.calls.length, 0);

    await ready(world, 1, "chan-1");
    const [init] = world.ofType(world.iframes[1], "init");
    sameJSON(
      { ...init, saved: undefined },
      { type: "vx-whiteboard:init", mode: "inline", diagramIndex: 1, diagramId: "", source: "classDiagram\n A<|--B", sourceHash: "h1", saved: undefined, theme: "dark", channelId: "chan-1" },
    );
    sameJSON(init.saved, { source_hash: "h1", scene: { elements: [] } });
    assert.strictEqual(world.wrappers[1].children[1].style.display, "none", "status clears once initialised");

    // a second ready cannot rebind the channel
    world.fromFrame(world.iframes[1], "ready", { diagramIndex: 1, channelId: "other" });
    await settle();
    assert.strictEqual(world.ofType(world.iframes[1], "init").length, 1);

    // one sources request serves both boards
    await ready(world, 0, "chan-0");
    assert.strictEqual(world.calls.filter((c) => c.op === "whiteboard.sources").length, 1);
    assert.strictEqual(world.ofType(world.iframes[0], "init")[0].saved, null);
  }

  // ---- a board that cannot start says why, and one that never reports says so too
  {
    const world = makeWorld({ rpc: () => { throw new Error("window.forum needs the forum chrome"); } });
    await ready(world, 0, "c");
    const status = world.wrappers[0].children[1];
    assert.strictEqual(status.textContent, "Could not start the whiteboard: window.forum needs the forum chrome");
    assert.strictEqual(status.style.color, "var(--fr-danger, #E08268)");
    world.advance(25000);
    assert.ok(status.textContent.includes("Could not start"), "the explicit failure is not overwritten");

    const quiet = makeWorld({ rpc: defaultRpc });
    quiet.advance(19000);
    assert.strictEqual(quiet.wrappers[0].children[1].textContent, "Loading whiteboard...");
    quiet.advance(2000);
    assert.ok(quiet.wrappers[0].children[1].textContent.startsWith("The whiteboard did not start"));
  }

  // ---- saving
  {
    const failing = { on: false };
    const world = makeWorld({
      rpc: (op, payload) => {
        if (op === "whiteboard.save" && failing.on) throw new Error("disk full");
        return defaultRpc(op, payload);
      },
    });
    await ready(world, 0, "c0");
    const save = { diagramIndex: 0, channelId: "c0", sourceHash: "h0", textMetricsVersion: 1, scene: { elements: [1] }, baseline: { elements: [] } };

    world.fromFrame(world.iframes[0], "save", { ...save, channelId: "wrong" });
    await settle();
    assert.strictEqual(world.calls.filter((c) => c.op === "whiteboard.save").length, 0, "wrong channel refused");

    world.fromFrame(world.iframes[0], "save", save);
    await settle();
    const saved = world.calls.find((c) => c.op === "whiteboard.save");
    sameJSON(saved.payload, {
      index: 0,
      body: { source_hash: "h0", text_metrics_version: 1, scene: { elements: [1] }, baseline: { elements: [] } },
    });
    assert.strictEqual(world.ofType(world.iframes[0], "saveResult").length, 0, "autosaves are not acknowledged");

    world.fromFrame(world.iframes[0], "save", { ...save, flushId: "f1" });
    await settle();
    sameJSON(world.ofType(world.iframes[0], "saveResult")[0], { type: "vx-whiteboard:saveResult", flushId: "f1", ok: true, channelId: "c0" });

    failing.on = true;
    world.fromFrame(world.iframes[0], "save", { ...save, flushId: "f2" });
    await settle();
    sameJSON(world.ofType(world.iframes[0], "saveResult")[1], { type: "vx-whiteboard:saveResult", flushId: "f2", ok: false, error: "disk full", channelId: "c0" });
  }

  // ---- queue feedback: persist first, then publish, bounded summary
  {
    const world = makeWorld({ rpc: defaultRpc });
    await ready(world, 0, "c0");
    const lines = Array.from({ length: 70 }, () => "x".repeat(400)).concat([7, null]);
    world.fromFrame(world.iframes[0], "queueFeedback", {
      diagramIndex: 0, channelId: "c0", sourceHash: "h0", textMetricsVersion: 1, scene: { elements: [] }, baseline: null,
      summaryLines: lines, pngDataUrl: "data:image/png;base64,AAA",
    });
    await settle();
    const ops = world.calls.map((c) => c.op).filter((op) => op.startsWith("whiteboard.") && op !== "whiteboard.sources" && op !== "whiteboard.load");
    sameJSON(ops, ["whiteboard.save", "whiteboard.feedback"]);
    const feedback = world.calls.find((c) => c.op === "whiteboard.feedback").payload;
    assert.strictEqual(feedback.index, 0);
    assert.strictEqual(feedback.body.pngDataUrl, "data:image/png;base64,AAA");
    assert.strictEqual(feedback.body.summaryLines.length, 50);
    assert.ok(feedback.body.summaryLines.every((line) => line.length === 300));
    sameJSON(world.ofType(world.iframes[0], "queueResult"), [{ type: "vx-whiteboard:queueResult", ok: true, channelId: "c0" }]);

    const broken = makeWorld({ rpc: (op, p) => { if (op === "whiteboard.feedback") throw new Error("no agent is listening"); return defaultRpc(op, p); } });
    await ready(broken, 0, "c0");
    broken.fromFrame(broken.iframes[0], "queueFeedback", { diagramIndex: 0, channelId: "c0", scene: {}, summaryLines: [] });
    await settle();
    sameJSON(broken.ofType(broken.iframes[0], "queueResult")[0], { type: "vx-whiteboard:queueResult", ok: false, error: "no agent is listening", channelId: "c0" });
  }

  // ---- fullscreen: teardown, overlay, close, reload
  {
    const world = makeWorld({ rpc: defaultRpc });
    // not ready yet: maximize does nothing
    world.fromFrame(world.iframes[0], "maximize", { diagramIndex: 0, channelId: "c0" });
    await ready(world, 0, "c0");
    assert.strictEqual(world.overlay(), undefined);

    world.fromFrame(world.iframes[0], "maximize", { diagramIndex: 0, channelId: "c0" });
    const [prepare] = world.ofType(world.iframes[0], "prepareTeardown");
    assert.ok(prepare.flushId && prepare.channelId === "c0");
    assert.strictEqual(world.overlay(), undefined, "nothing opens before the frame confirms its save");

    world.fromFrame(world.iframes[0], "teardownReady", { diagramIndex: 0, channelId: "c0", flushId: "stale" });
    await settle();
    assert.strictEqual(world.overlay(), undefined, "a stale flush id is ignored");

    world.fromFrame(world.iframes[0], "teardownReady", { diagramIndex: 0, channelId: "c0", flushId: prepare.flushId });
    await settle();
    const overlay = world.overlay();
    assert.strictEqual(overlay.style.display, "block");
    assert.strictEqual(overlay.style.position, "fixed");
    const [overlayFrame, close] = overlay.children;
    assert.strictEqual(close.tag, "button");
    assert.strictEqual(close.textContent, "× Close");
    assert.strictEqual(overlayFrame.src, "/whiteboard-frame?diagramIndex=0&theme=dark");
    assert.strictEqual(world.iframes[0].style.pointerEvents, "none");

    // the suspended inline board can no longer save over the overlay's scene
    const before = world.calls.length;
    world.fromFrame(world.iframes[0], "save", { diagramIndex: 0, channelId: "c0", scene: {} });
    await settle();
    assert.strictEqual(world.calls.length, before);

    // a second maximize while open is ignored
    world.fromFrame(world.iframes[0], "maximize", { diagramIndex: 0, channelId: "c0" });
    assert.strictEqual(world.ofType(world.iframes[0], "prepareTeardown").length, 1);

    // the overlay frame handshakes in overlay mode
    world.fromFrame(overlayFrame, "ready", { diagramIndex: 0, channelId: "ov" });
    await settle();
    assert.strictEqual(world.ofType(overlayFrame, "init")[0].mode, "overlay");
    // and saves through the same path
    world.fromFrame(overlayFrame, "save", { diagramIndex: 0, channelId: "ov", flushId: "g", sourceHash: "h0", scene: {} });
    await settle();
    assert.strictEqual(world.ofType(overlayFrame, "saveResult")[0].ok, true);

    // closing: teardown first, then hide and reload the inline board
    close.onclick();
    const [overlayPrepare] = world.ofType(overlayFrame, "prepareTeardown");
    assert.strictEqual(overlay.style.display, "block");
    world.fromFrame(overlayFrame, "teardownReady", { diagramIndex: 0, channelId: "ov", flushId: overlayPrepare.flushId });
    await settle();
    assert.strictEqual(overlay.style.display, "none");
    assert.strictEqual(overlayFrame.src, "about:blank");
    assert.strictEqual(world.iframes[0].style.pointerEvents, "");
    assert.strictEqual(world.iframes[0].srcSets.at(-1), "/whiteboard-frame?diagramIndex=0&theme=dark");
    assert.strictEqual(world.wrappers[0].children[1].textContent, "Loading whiteboard...");

    // the reloaded inline frame announces itself afresh with a new channel
    await ready(world, 0, "c0b");
    assert.strictEqual(world.ofType(world.iframes[0], "init").length, 2);
    assert.strictEqual(world.ofType(world.iframes[0], "init")[1].channelId, "c0b");
  }

  // ---- fullscreen failure paths
  {
    // the inline frame says its save failed: nothing opens
    const world = makeWorld({ rpc: defaultRpc });
    await ready(world, 0, "c0");
    world.fromFrame(world.iframes[0], "maximize", { diagramIndex: 0, channelId: "c0" });
    const [prepare] = world.ofType(world.iframes[0], "prepareTeardown");
    world.fromFrame(world.iframes[0], "teardownFailed", { diagramIndex: 0, channelId: "c0", flushId: prepare.flushId, error: "x" });
    await settle();
    assert.strictEqual(world.overlay(), undefined);
    assert.strictEqual(world.iframes[0].style.pointerEvents, undefined);

    // the inline frame stays silent: nothing opens after the wait
    world.fromFrame(world.iframes[0], "maximize", { diagramIndex: 0, channelId: "c0" });
    world.advance(1600);
    await settle();
    assert.strictEqual(world.overlay(), undefined);
  }
  {
    // an overlay frame that will not answer: the second Close press leaves anyway
    const world = makeWorld({ rpc: defaultRpc });
    await ready(world, 0, "c0");
    world.fromFrame(world.iframes[0], "maximize", { diagramIndex: 0, channelId: "c0" });
    const [prepare] = world.ofType(world.iframes[0], "prepareTeardown");
    world.fromFrame(world.iframes[0], "teardownReady", { diagramIndex: 0, channelId: "c0", flushId: prepare.flushId });
    await settle();
    const [overlayFrame, close] = world.overlay().children;
    world.fromFrame(overlayFrame, "ready", { diagramIndex: 0, channelId: "ov" });
    await settle();
    close.onclick();
    world.advance(1600);
    await settle();
    assert.strictEqual(world.overlay().style.display, "block");
    assert.strictEqual(close.textContent, "× Close anyway");
    close.onclick();
    assert.strictEqual(world.overlay().style.display, "none");
    assert.strictEqual(close.textContent, "× Close anyway", "label is reset when the overlay opens again");
  }
  // ---- live theme switch
  {
    const world = makeWorld({ rpc: defaultRpc });
    await ready(world, 0, "c0"); // ready; board 1 stays unstarted
    world.setTheme("dark"); // unchanged: no message
    assert.strictEqual(world.ofType(world.iframes[0], "theme").length, 0);

    world.setTheme("light");
    sameJSON(world.ofType(world.iframes[0], "theme"), [{ type: "vx-whiteboard:theme", theme: "light", channelId: "c0" }]);
    assert.strictEqual(world.ofType(world.iframes[1], "theme").length, 0, "a frame that has not started gets its theme in init");
    assert.strictEqual(world.iframes[0].style.colorScheme, "light");
    assert.ok(world.iframes[0].style.border.includes("#C2C0B8"));
    assert.ok(world.iframes[0].style.background.includes("#F2F1EC"));

    // an overlay opened afterwards starts in the current theme, and follows the next switch
    world.fromFrame(world.iframes[0], "maximize", { diagramIndex: 0, channelId: "c0" });
    const [prepare] = world.ofType(world.iframes[0], "prepareTeardown");
    world.fromFrame(world.iframes[0], "teardownReady", { diagramIndex: 0, channelId: "c0", flushId: prepare.flushId });
    await settle();
    const [overlayFrame, close] = world.overlay().children;
    assert.ok(overlayFrame.src.endsWith("&theme=light"));
    assert.strictEqual(close.style.color, "var(--fr-text, #1A1D22)");
    world.fromFrame(overlayFrame, "ready", { diagramIndex: 0, channelId: "ov" });
    await settle();
    assert.strictEqual(world.ofType(overlayFrame, "init")[0].theme, "light");

    world.setTheme("dark");
    assert.strictEqual(world.ofType(overlayFrame, "theme").at(-1).theme, "dark");
    assert.strictEqual(world.ofType(world.iframes[0], "theme").at(-1).theme, "dark");
    assert.strictEqual(close.style.color, "var(--fr-text, #E9EAEC)");
    assert.strictEqual(world.overlay().style.display, "block", "repainting keeps the overlay open");
    assert.strictEqual(world.overlay().style.background, "var(--fr-scrim, rgba(0,0,0,0.6))");

    // garbage means no forum theme; the OS preference (light here: matchMedia says no dark) decides
    world.setTheme("sepia");
    assert.strictEqual(world.ofType(overlayFrame, "theme").at(-1).theme, "light");
  }

  // ---- the status colour follows the theme too
  {
    const world = makeWorld({ rpc: () => { throw new Error("nope"); } });
    await ready(world, 0, "c");
    world.setTheme("light");
    assert.strictEqual(world.wrappers[0].children[1].style.color, "var(--fr-danger, #A8341F)");
    // and the loading timer of an untouched board is not disturbed by a switch
    const waiting = makeWorld({ rpc: defaultRpc });
    waiting.setTheme("light");
    waiting.advance(21000);
    assert.ok(waiting.wrappers[0].children[1].textContent.startsWith("The whiteboard did not start"));
  }

  // ---- unload flush
  {
    const world = makeWorld({ rpc: defaultRpc });
    await ready(world, 1, "c1");
    world.fire("beforeunload", {});
    sameJSON(world.ofType(world.iframes[1], "flush"), [{ type: "vx-whiteboard:flush", flushId: "wb-unload-1", channelId: "c1" }]);
    assert.strictEqual(world.ofType(world.iframes[0], "flush").length, 0, "an unstarted board has nothing to flush");
  }

  console.log("ok");
})().catch((error) => {
  console.error(error);
  process.exit(1);
});
