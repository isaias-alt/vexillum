// Adapted from upstream (MIT License, Copyright (c) 2026 Kun
// Chen), the whiteboard block of src/chrome-client.js at v0.1.80 (commit
// a2a199c, lines ~3255-3750). See THIRD-PARTY-NOTICES.md at the vexillum
// repo root.
//
// Structural changes from upstream (beyond forum-whiteboard: -> vx-whiteboard:
// message types and dropping the channelToken HTTP handshake, see
// whiteboard-frame.js's header for why):
//   - Upstream's chrome hosts a *separate* artifact iframe and reaches into it
//     to find `.mermaid` containers, so inline whiteboard iframes are two
//     levels deep and it validates senders via
//     `source.parent === artifactFrame.contentWindow`. Here this script is
//     injected into the artifact page itself, so whiteboard iframes are
//     direct children of `window`, and the sender check is
//     `source.parent === window`.
//   - The artifact runs in a sandboxed iframe (an opaque origin) under the
//     forum chrome, and the whiteboard iframes are direct children of it.
//     This script holds no server credentials: every round trip goes through
//     window.forum.__rpc (forum-sdk.js), which asks the chrome to call the
//     session-scoped, token-guarded API on its behalf.
//   - "Queue feedback" persists the edited scene plus a `.excalidraw`/PNG
//     snapshot to disk and the server queues a prompt tagged "whiteboard"
//     (a bounded edit summary and those two paths) into the same queue as
//     every other feedback, for the user to send to the agent.
//   - No live-reload / chrome-restart flushing: the forum chrome reloads the
//     artifact iframe itself when the file changes, and closing the
//     fullscreen overlay reloads the inline iframe from disk so it picks up
//     whatever the overlay just saved.
//
// Runs inside the artifact page (injected by internal/forum's server before
// </body> when the page contains at least one `.mermaid` container). Finds
// every `.mermaid` container, in document order, and replaces it with a
// sandboxed iframe pointing at /whiteboard-frame - the editable Excalidraw
// view of that diagram's Mermaid source. Owns every server round trip
// (through the chrome); the frames themselves have no server access (see
// whiteboard-frame.js).

(function () {
  "use strict";

  const MERMAID_SELECTOR = ".mermaid";
  const OVERLAY_ID = "vxWhiteboardOverlay";
  const TEARDOWN_TIMEOUT_MS = 1500;

  /** @type {Map<number, { iframe: HTMLIFrameElement, channelId: string, ready: boolean, suspended: boolean }>} */
  const inlineFrames = new Map();
  /** @type {Map<number, { source: string, hash: string }>} */
  const sourceCache = new Map();
  const teardowns = new Map();
  let nextFlushId = 0;

  let overlay = null;
  let overlayIframe = null;
  let overlayCloseButton = null;
  let overlayIndex = null;
  let overlayChannelId = "";
  let overlayReady = false;

  function theme() {
    // The chrome's theme switch wins (the server renders it on <html>); the
    // OS preference only decides when the artifact carries no forum theme.
    const forced = document.documentElement.getAttribute("data-fr-theme");
    if (forced === "dark" || forced === "light") return forced;
    return window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  }

  const rpc = (op, payload) => window.forum.__rpc(op, payload);

  async function fetchMermaidSources() {
    const data = await rpc("whiteboard.sources");
    return Array.isArray(data.sources) ? data.sources : [];
  }

  async function fetchSavedScene(index) {
    try {
      const data = await rpc("whiteboard.load", { index });
      return data.whiteboard || null;
    } catch {
      return null;
    }
  }

  async function persistScene(index, message) {
    await rpc("whiteboard.save", {
      index,
      body: {
        source_hash: String(message.sourceHash || ""),
        text_metrics_version: Number(message.textMetricsVersion) || 0,
        scene: message.scene || null,
        baseline: message.baseline || null,
      },
    });
  }

  async function publishFeedback(index, scene, pngDataUrl, summaryLines) {
    return rpc("whiteboard.feedback", {
      index,
      body: { scene: scene || null, pngDataUrl: String(pngDataUrl || ""), summaryLines: boundedLines(summaryLines) },
    });
  }

  function post(record, message) {
    if (!record || !record.iframe.contentWindow) return;
    record.iframe.contentWindow.postMessage({ ...message, channelId: record.channelId }, "*");
  }

  function postOverlay(message) {
    if (overlayIframe?.contentWindow && overlayChannelId) {
      overlayIframe.contentWindow.postMessage({ ...message, channelId: overlayChannelId }, "*");
    }
  }

  // A frame that has not rendered yet is an empty box, which is
  // indistinguishable from a broken one. Every inline frame therefore sits in a
  // wrapper with a status line over it: "Loading", or the reason it did not
  // start (the frame never reported ready, or the init round trip failed).
  const START_TIMEOUT_MS = 20000;
  const statusTimers = new Map();

  function setStatus(index, text, isError) {
    const record = inlineFrames.get(index);
    if (!record || !record.status) return;
    window.clearTimeout(statusTimers.get(index));
    statusTimers.delete(index);
    record.status.textContent = text || "";
    record.status.style.display = text ? "flex" : "none";
    record.status.style.color = isError ? "#b91c1c" : "inherit";
  }

  function watchStart(index) {
    setStatus(index, "Loading whiteboard...", false);
    statusTimers.set(
      index,
      window.setTimeout(() => {
        const record = inlineFrames.get(index);
        if (record && !record.ready) {
          setStatus(
            index,
            "The whiteboard did not start: its frame never reported ready. Reload the page; if it persists, run `vexillum forum stop` and open the session again so the server restarts with the current build.",
            true,
          );
        }
      }, START_TIMEOUT_MS),
    );
  }

  function makeIframe(index) {
    const iframe = document.createElement("iframe");
    iframe.src = "/whiteboard-frame?diagramIndex=" + encodeURIComponent(String(index));
    iframe.sandbox = "allow-scripts allow-popups";
    iframe.style.width = "100%";
    iframe.style.height = "480px";
    iframe.style.border = "1px solid rgba(128, 128, 128, 0.35)";
    iframe.style.borderRadius = "10px";
    iframe.title = "Whiteboard · diagram " + (index + 1);
    return iframe;
  }

  function replaceMermaidContainers() {
    const containers = Array.from(document.querySelectorAll(MERMAID_SELECTOR));
    containers.forEach((container, index) => {
      const iframe = makeIframe(index);
      const wrapper = document.createElement("div");
      wrapper.style.position = "relative";
      const status = document.createElement("div");
      status.setAttribute("role", "status");
      Object.assign(status.style, {
        position: "absolute",
        inset: "0",
        display: "none",
        alignItems: "center",
        justifyContent: "center",
        padding: "16px",
        textAlign: "center",
        font: "14px/1.5 system-ui, sans-serif",
        pointerEvents: "none",
      });
      wrapper.append(iframe, status);
      container.replaceWith(wrapper);
      inlineFrames.set(index, { iframe, status, channelId: "", ready: false, suspended: false });
      watchStart(index);
    });
    return containers.length;
  }

  async function initFrame(index, record, mode, channelId) {
    try {
      let cached = sourceCache.get(index);
      if (!cached) {
        const sources = await fetchMermaidSources();
        for (const item of sources) sourceCache.set(item.index, { source: item.source, hash: item.hash });
        cached = sourceCache.get(index);
      }
      if (!cached) throw new Error("this diagram's Mermaid source was not found on the page");
      const saved = await fetchSavedScene(index);
      const target = mode === "overlay" ? null : record;
      const message = {
        type: "vx-whiteboard:init",
        mode,
        diagramIndex: index,
        diagramId: "",
        source: cached.source,
        sourceHash: cached.hash,
        saved,
        theme: theme(),
        channelId,
      };
      if (mode === "overlay") postOverlay(message);
      else {
        post(target, message);
        setStatus(index, "", false);
      }
      return true;
    } catch (error) {
      if (mode === "overlay") showOverlayError(describeError(error));
      else setStatus(index, "Could not start the whiteboard: " + describeError(error), true);
      return false;
    }
  }

  function describeError(error) {
    return error instanceof Error ? error.message : String(error);
  }

  function teardownKey(index, placement) {
    return placement + ":" + index;
  }

  function beginTeardown(index, placement) {
    const tkey = teardownKey(index, placement);
    const pending = teardowns.get(tkey);
    if (pending) return pending.promise;
    const flushId = "wb-teardown-" + ++nextFlushId;
    let resolve;
    const promise = new Promise((complete) => {
      resolve = complete;
    });
    teardowns.set(tkey, { flushId, resolve });
    const message = { type: "vx-whiteboard:prepareTeardown", flushId };
    if (placement === "overlay") postOverlay(message);
    else post(inlineFrames.get(index), message);
    const timeout = window.setTimeout(() => finishTeardown(index, { flushId }, placement, false), TEARDOWN_TIMEOUT_MS);
    teardowns.get(tkey).timeout = timeout;
    return promise;
  }

  function finishTeardown(index, message, placement, ok) {
    const tkey = teardownKey(index, placement);
    const pending = teardowns.get(tkey);
    if (!pending || pending.flushId !== String(message.flushId || "")) return;
    window.clearTimeout(pending.timeout);
    teardowns.delete(tkey);
    pending.resolve(ok);
  }

  function handleSave(index, message, placement) {
    const flushId = String(message.flushId || "");
    persistScene(index, message).then(
      () => {
        if (flushId) {
          const target = placement === "overlay" ? null : inlineFrames.get(index);
          const result = { type: "vx-whiteboard:saveResult", flushId, ok: true };
          if (placement === "overlay") postOverlay(result);
          else post(target, result);
        }
      },
      (error) => {
        if (!flushId) return;
        const result = { type: "vx-whiteboard:saveResult", flushId, ok: false, error: describeError(error) };
        if (placement === "overlay") postOverlay(result);
        else post(inlineFrames.get(index), result);
      },
    );
  }

  function boundedLines(lines) {
    return (Array.isArray(lines) ? lines : [])
      .filter((line) => typeof line === "string")
      .slice(0, 50)
      .map((line) => line.slice(0, 300));
  }

  async function handleQueueFeedback(index, message, placement) {
    const reply = (result) => {
      if (placement === "overlay") postOverlay(result);
      else post(inlineFrames.get(index), result);
    };
    try {
      await persistScene(index, message);
      await publishFeedback(index, message.scene, message.pngDataUrl, message.summaryLines);
      reply({ type: "vx-whiteboard:queueResult", ok: true });
    } catch (error) {
      reply({ type: "vx-whiteboard:queueResult", ok: false, error: describeError(error) });
    }
  }

  function ensureOverlay() {
    if (overlay) return;
    overlay = document.createElement("div");
    overlay.id = OVERLAY_ID;
    Object.assign(overlay.style, {
      position: "fixed",
      inset: "0",
      zIndex: "2147483000",
      background: "rgba(0, 0, 0, 0.5)",
      display: "none",
    });
    overlayIframe = document.createElement("iframe");
    overlayIframe.sandbox = "allow-scripts allow-popups";
    overlayIframe.title = "Whiteboard (fullscreen)";
    Object.assign(overlayIframe.style, {
      position: "absolute",
      inset: "0",
      width: "100%",
      height: "100%",
      border: "0",
      background: "#fffbf3",
    });
    overlayCloseButton = document.createElement("button");
    overlayCloseButton.type = "button";
    overlayCloseButton.textContent = "\u00d7 Close";
    // Looks like a forum secondary button: the --fr-* design tokens when the
    // artifact has them, the same palette values otherwise (the embed runs in
    // artifacts that bring their own styles and no tokens).
    const dark = theme() === "dark";
    const color = (name, darkValue, lightValue) => "var(--fr-" + name + ", " + (dark ? darkValue : lightValue) + ")";
    Object.assign(overlayCloseButton.style, {
      position: "absolute",
      top: "8px",
      right: "10px",
      zIndex: "1",
      padding: "4px 10px",
      font: "500 13px/1.3 var(--fr-font-sans, system-ui, sans-serif)",
      cursor: "pointer",
      color: color("text", "#E9EAEC", "#1A1D22"),
      background: color("surface", "#1C1F24", "#FFFFFF"),
      border: "1px solid " + color("border-strong", "#3A3F47", "#C2C0B8"),
      borderRadius: "var(--fr-radius-sm, 4px)",
    });
    overlayCloseButton.onclick = closeOverlay;
    overlay.append(overlayIframe, overlayCloseButton);
    document.body.append(overlay);
  }

  function showOverlayError(text) {
    ensureOverlay();
    overlay.style.display = "block";
    overlayIframe.srcdoc =
      '<p style="font-family:sans-serif;padding:16px;color:#b91c1c;">Could not open the whiteboard: ' +
      String(text || "unknown error").replace(/</g, "&lt;") +
      "</p>";
  }

  function openOverlay(index) {
    if (overlayIndex !== null) return;
    const record = inlineFrames.get(index);
    if (!record || !record.ready) return;
    beginTeardown(index, "inline").then((flushed) => {
      if (!flushed) return;
      record.suspended = true;
      record.iframe.style.pointerEvents = "none";
      ensureOverlay();
      overlayIndex = index;
      overlayReady = false;
      overlayChannelId = "";
      overlay.style.display = "block";
      overlayIframe.src = "/whiteboard-frame?diagramIndex=" + encodeURIComponent(String(index));
    });
  }

  function closeOverlay() {
    const index = overlayIndex;
    if (index === null) return;
    if (!overlayReady) {
      finishCloseOverlay(index);
      return;
    }
    beginTeardown(index, "overlay").then((flushed) => {
      if (flushed) finishCloseOverlay(index);
    });
  }

  function finishCloseOverlay(index) {
    overlay.style.display = "none";
    overlayIframe.src = "about:blank";
    overlayIndex = null;
    overlayReady = false;
    overlayChannelId = "";
    const record = inlineFrames.get(index);
    if (record) {
      record.suspended = false;
      record.iframe.style.pointerEvents = "";
      // Reload the inline iframe fresh so it picks up whatever the overlay
      // just saved, instead of trying to reconcile two live in-memory scenes.
      record.ready = false;
      record.channelId = "";
      watchStart(index);
      record.iframe.src = record.iframe.src;
    }
  }

  function isDirectChild(source, iframe) {
    if (!source || !iframe?.contentWindow) return false;
    try {
      return source === iframe.contentWindow;
    } catch {
      return false;
    }
  }

  function handleInlineMessage(event, message) {
    const index = Number(message.diagramIndex);
    if (!Number.isInteger(index) || index < 0) return;
    const record = inlineFrames.get(index);
    if (!record || !isDirectChild(event.source, record.iframe)) return;

    if (message.type === "vx-whiteboard:ready") {
      if (record.channelId) return;
      record.channelId = String(message.channelId || "");
      if (!record.channelId) return;
      initFrame(index, record, "inline", record.channelId).then((ok) => {
        record.ready = ok;
      });
      return;
    }
    if (!record.channelId || message.channelId !== record.channelId) return;
    if (message.type === "vx-whiteboard:save") handleSave(index, message, "inline");
    else if (message.type === "vx-whiteboard:queueFeedback") handleQueueFeedback(index, message, "inline");
    else if (message.type === "vx-whiteboard:maximize") openOverlay(index);
    else if (message.type === "vx-whiteboard:teardownReady") finishTeardown(index, message, "inline", true);
    else if (message.type === "vx-whiteboard:teardownFailed") finishTeardown(index, message, "inline", false);
  }

  function handleOverlayMessage(event, message) {
    if (overlayIndex === null || !isDirectChild(event.source, overlayIframe)) return;
    const index = Number(message.diagramIndex);
    if (index !== overlayIndex) return;

    if (message.type === "vx-whiteboard:ready") {
      if (overlayChannelId) return;
      overlayChannelId = String(message.channelId || "");
      if (!overlayChannelId) return;
      initFrame(index, null, "overlay", overlayChannelId).then((ok) => {
        overlayReady = ok;
      });
      return;
    }
    if (!overlayReady || message.channelId !== overlayChannelId) return;
    if (message.type === "vx-whiteboard:save") handleSave(index, message, "overlay");
    else if (message.type === "vx-whiteboard:queueFeedback") handleQueueFeedback(index, message, "overlay");
    else if (message.type === "vx-whiteboard:teardownReady") finishTeardown(index, message, "overlay", true);
    else if (message.type === "vx-whiteboard:teardownFailed") finishTeardown(index, message, "overlay", false);
  }

  window.addEventListener("message", (event) => {
    const message = event.data || {};
    if (typeof message.type !== "string" || !message.type.startsWith("vx-whiteboard:")) return;
    if (overlayIndex !== null && isDirectChild(event.source, overlayIframe)) {
      handleOverlayMessage(event, message);
      return;
    }
    handleInlineMessage(event, message);
  });

  // Best-effort autosave flush - a page unload mid-debounce should not lose
  // the last few seconds of edits. Not guaranteed to complete (browsers do
  // not wait for async work in this handler), but the frame's own 800ms
  // debounce already keeps the window small.
  window.addEventListener("beforeunload", () => {
    for (const [index, record] of inlineFrames) {
      if (record.ready && !record.suspended) {
        post(record, { type: "vx-whiteboard:flush", flushId: "wb-unload-" + index });
      }
    }
    if (overlayIndex !== null && overlayReady) {
      postOverlay({ type: "vx-whiteboard:flush", flushId: "wb-unload-overlay" });
    }
  });

  document.addEventListener("DOMContentLoaded", replaceMermaidContainers);
  if (document.readyState !== "loading") replaceMermaidContainers();
})();
