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
//     levels deep (chrome -> artifact iframe -> whiteboard iframe) and it
//     validates senders via `source.parent === artifactFrame.contentWindow`.
//     vexillum forum serves the artifact page directly at `/` with this
//     script injected into it - there is no separate chrome/artifact split -
//     so whiteboard iframes are direct children of `window`, and the sender
//     check is `source.parent === window`.
//   - No `key`/session id in any URL or endpoint: this server only ever
//     serves the single artifact given to `vexillum forum <file>` for its
//     process lifetime, so every request already implicitly belongs to that
//     one forum - see internal/forum's package doc.
//   - No prompt queue / poll loop exists yet in vexillum forum (that is a
//     separate, not-yet-built feature - see the design scout report this
//     mission was dispatched from). "Queue feedback" here simply persists
//     the edited scene plus a `.excalidraw`/PNG snapshot to disk and reports
//     success in the frame's own status line; it does not enqueue anything
//     for an agent to read.
//   - No live-reload / chrome-restart flushing: vexillum forum does not
//     hot-reload the served artifact, so that upstream machinery (relevant
//     only to forum-tool's editor-in-the-loop workflow) is dropped. Closing
//     the fullscreen overlay reloads the inline iframe from disk instead, so
//     it picks up whatever the overlay just saved.
//
// Runs inside the artifact page (injected by internal/forum's server before
// </body> when the page contains at least one `.mermaid` container). Finds
// every `.mermaid` container, in document order, and replaces it with a
// sandboxed iframe pointing at /whiteboard-frame - the editable Excalidraw
// view of that diagram's Mermaid source. Owns every server round trip; the
// frames themselves have no server access (see whiteboard-frame.js).

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
    return window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  }

  async function fetchMermaidSources() {
    const response = await fetch("/api/mermaid-sources");
    if (!response.ok) throw new Error("could not read the page's Mermaid sources");
    const data = await response.json();
    return Array.isArray(data.sources) ? data.sources : [];
  }

  async function fetchSavedScene(index) {
    const response = await fetch("/api/whiteboard/" + index);
    if (!response.ok) return null;
    const data = await response.json();
    return data.whiteboard || null;
  }

  async function persistScene(index, message) {
    const response = await fetch("/api/whiteboard/" + index, {
      method: "PUT",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        source_hash: String(message.sourceHash || ""),
        text_metrics_version: Number(message.textMetricsVersion) || 0,
        scene: message.scene || null,
        baseline: message.baseline || null,
      }),
    });
    if (!response.ok) throw new Error("failed to save whiteboard scene");
  }

  async function publishFeedback(index, scene, pngDataUrl) {
    const response = await fetch("/api/whiteboard/" + index + "/feedback-files", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ scene: scene || null, pngDataUrl: String(pngDataUrl || "") }),
    });
    if (!response.ok) throw new Error("failed to write whiteboard feedback files");
    return response.json();
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
      container.replaceWith(iframe);
      inlineFrames.set(index, { iframe, channelId: "", ready: false, suspended: false });
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
      else post(target, message);
      return true;
    } catch (error) {
      if (mode === "overlay") showOverlayError(describeError(error));
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

  function summaryText(lines) {
    return (Array.isArray(lines) ? lines : [])
      .filter((line) => typeof line === "string")
      .slice(0, 50)
      .map((line) => line.slice(0, 300))
      .join("\n");
  }

  async function handleQueueFeedback(index, message, placement) {
    try {
      await persistScene(index, message);
      const files = await publishFeedback(index, message.scene, message.pngDataUrl);
      const result = { type: "vx-whiteboard:queueResult", ok: true };
      if (placement === "overlay") postOverlay(result);
      else post(inlineFrames.get(index), result);
      // eslint-disable-next-line no-console
      console.info(
        "[vexillum forum] whiteboard feedback saved: diagram " + (index + 1),
        "\n" + summaryText(message.summaryLines),
        "\nscene:",
        files.scene_path,
        files.preview_path ? "\npreview: " + files.preview_path : "",
      );
    } catch (error) {
      const result = { type: "vx-whiteboard:queueResult", ok: false, error: describeError(error) };
      if (placement === "overlay") postOverlay(result);
      else post(inlineFrames.get(index), result);
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
    overlayCloseButton.textContent = "× Close";
    Object.assign(overlayCloseButton.style, {
      position: "absolute",
      top: "10px",
      right: "10px",
      zIndex: "1",
      border: "0",
      borderRadius: "8px",
      padding: "8px 12px",
      fontWeight: "700",
      cursor: "pointer",
      background: "#f4c95d",
      color: "#17130a",
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
