// The whiteboard embed. The forum server injects this script into an artifact
// that contains at least one `.mermaid` block (see internal/forum/server_pages.go);
// build.js copies it to internal/forum/assets/whiteboard-embed.js, which is the
// copy the binary serves.
//
// It runs inside the artifact page, which is itself a sandboxed iframe under the
// forum chrome. It replaces every `.mermaid` block, in document order, with a
// sandboxed iframe on /whiteboard-frame (the editable Excalidraw view of that
// block's Mermaid source), and it is the only party that talks to the server:
// every request goes through window.forum.__rpc, which asks the chrome to make
// it with the session credentials. The frames themselves are given nothing but
// postMessage.
//
// Each board the embed manages is a "placement": an iframe, the channel id its
// frame announced, and a little state. Inline boards live in the page; at most
// one fullscreen placement exists at a time, drawn over everything by the
// overlay. Both kinds are driven by the same message handling.
//
//   frame -> embed:  ready, save, queueFeedback, maximize, teardownReady,
//                    teardownFailed, flushComplete        ("vx-whiteboard:" prefix)
//   embed -> frame:  init, theme, prepareTeardown, flush, saveResult, queueResult

(function () {
  "use strict";

  const PREFIX = "vx-whiteboard:";
  const OVERLAY_ID = "vxWhiteboardOverlay";
  const CLOSE_LABEL = "× Close";
  const CLOSE_ANYWAY_LABEL = "× Close anyway";
  const TEARDOWN_WAIT_MS = 1500;
  const START_WAIT_MS = 20000;
  const SUMMARY_LINES = 50;
  const SUMMARY_LINE_CHARS = 300;

  // ------------------------------------------------------------------- theme

  // The design system's own values, used when an artifact carries no --fr-*
  // tokens of its own (artifacts bring their styles; the embed must still look
  // right in one that does not).
  const PALETTE = {
    dark: {
      bg: "#15171A",
      surface: "#1C1F24",
      text: "#E9EAEC",
      "text-secondary": "#A0A3A9",
      "border-strong": "#3A3F47",
      danger: "#E08268",
      scrim: "rgba(0,0,0,0.6)",
    },
    light: {
      bg: "#F2F1EC",
      surface: "#FFFFFF",
      text: "#1A1D22",
      "text-secondary": "#5A5E66",
      "border-strong": "#C2C0B8",
      danger: "#A8341F",
      scrim: "rgba(26,29,34,0.45)",
    },
  };

  // The chrome's choice wins: the SDK keeps <html data-fr-theme> in step with
  // the switch in the chrome. The OS preference only decides for a page that
  // has no forum theme at all.
  function currentTheme() {
    const forced = document.documentElement.getAttribute("data-fr-theme");
    if (forced === "dark" || forced === "light") return forced;
    const dark = window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches;
    return dark ? "dark" : "light";
  }

  const token = (name) => `var(--fr-${name}, ${PALETTE[currentTheme()][name]})`;

  // Styling is registered as a function from theme to style values, applied
  // now and again on every theme switch.
  const painters = [];
  function skin(node, styles) {
    const paint = () => Object.assign(node.style, styles());
    paint();
    painters.push(paint);
  }

  // ------------------------------------------------------------ server calls

  function ask(op, payload) {
    if (!window.forum || typeof window.forum.__rpc !== "function") {
      return Promise.reject(new Error("window.forum is missing: the whiteboard needs the forum chrome"));
    }
    return window.forum.__rpc(op, payload);
  }

  const describeError = (error) => String((error && error.message) || error);

  // One request serves every board's source lookup.
  let sourcesRequest = null;
  const sources = new Map();
  async function sourceOf(index) {
    if (!sources.size) {
      sourcesRequest = sourcesRequest || ask("whiteboard.sources");
      try {
        const reply = await sourcesRequest;
        for (const item of Array.isArray(reply.sources) ? reply.sources : []) sources.set(item.index, item);
      } finally {
        sourcesRequest = null;
      }
    }
    const entry = sources.get(index);
    if (!entry) throw new Error("this diagram's Mermaid source was not found on the page");
    return entry;
  }

  async function savedSceneOf(index) {
    try {
      return (await ask("whiteboard.load", { index })).whiteboard || null;
    } catch {
      return null;
    }
  }

  function persist(placement, message) {
    return ask("whiteboard.save", {
      index: placement.index,
      body: {
        source_hash: String(message.sourceHash || ""),
        text_metrics_version: Number(message.textMetricsVersion) || 0,
        scene: message.scene || null,
        baseline: message.baseline || null,
      },
    });
  }

  function boundedLines(lines) {
    return (Array.isArray(lines) ? lines : [])
      .filter((line) => typeof line === "string")
      .slice(0, SUMMARY_LINES)
      .map((line) => line.slice(0, SUMMARY_LINE_CHARS));
  }

  // -------------------------------------------------------------- placements

  /** @type {Array<ReturnType<typeof newPlacement>>} */
  const boards = []; // inline placements, indexed by diagram
  let fullscreen = null; // the open fullscreen placement, if any
  let overlayHost = null; // { root, iframe, close }, built on first use

  function newPlacement(kind, index, iframe) {
    return {
      kind, // "inline" | "overlay"
      index,
      iframe,
      channel: "", // bound on the frame's first "ready"
      ready: false, // the init message has been delivered
      suspended: false, // an inline board hidden behind the overlay
      closing: null, // a teardown in flight
      dead: false,
      statusNode: null,
      statusError: false,
      startTimer: 0,
    };
  }

  const frameUrl = (index) => `/whiteboard-frame?diagramIndex=${encodeURIComponent(String(index))}&theme=${currentTheme()}`;

  function send(placement, type, fields) {
    const target = placement.iframe.contentWindow;
    if (!target || !placement.channel || placement.dead) return;
    target.postMessage({ ...fields, type: PREFIX + type, channelId: placement.channel }, "*");
  }

  // An empty box looks the same as a broken one, so every inline board has a
  // status line laid over it: "Loading", or why the board did not start.
  function say(placement, text, isError) {
    const node = placement.statusNode;
    if (!node) return;
    window.clearTimeout(placement.startTimer);
    placement.statusError = Boolean(isError);
    node.textContent = text || "";
    node.style.display = text ? "flex" : "none";
    node.style.color = statusColor(placement);
  }

  const statusColor = (placement) => (placement.statusError ? token("danger") : token("text-secondary"));

  function awaitReady(placement) {
    say(placement, "Loading whiteboard...", false);
    placement.startTimer = window.setTimeout(() => {
      if (placement.ready) return;
      say(
        placement,
        "The whiteboard did not start: its frame never reported ready. Reload the page; if it persists, run `vx forum stop` and open the session again so the server restarts with the current build.",
        true,
      );
    }, START_WAIT_MS);
  }

  function buildInline(index) {
    const iframe = document.createElement("iframe");
    iframe.src = frameUrl(index);
    iframe.sandbox = "allow-scripts allow-popups";
    iframe.title = `Whiteboard · diagram ${index + 1}`;
    skin(iframe, () => ({
      width: "100%",
      height: "480px",
      border: `1px solid ${token("border-strong")}`,
      borderRadius: "var(--fr-radius-lg, 12px)",
      background: token("bg"),
      colorScheme: currentTheme(),
    }));

    const placement = newPlacement("inline", index, iframe);
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
    placement.statusNode = status;
    skin(status, () => ({ color: statusColor(placement) }));

    const wrapper = document.createElement("div");
    wrapper.style.position = "relative";
    wrapper.append(iframe, status);
    return { placement, wrapper };
  }

  let mounted = false;
  function mountBoards() {
    if (mounted) return;
    mounted = true;
    document.querySelectorAll(".mermaid").forEach((container, index) => {
      const { placement, wrapper } = buildInline(index);
      container.replaceWith(wrapper);
      boards[index] = placement;
      awaitReady(placement);
    });
  }

  // Once a frame has announced itself, hand it the diagram: source, its hash,
  // whatever scene was saved, and the theme as it is right now.
  async function handshake(placement) {
    try {
      const entry = await sourceOf(placement.index);
      const saved = await savedSceneOf(placement.index);
      if (placement.dead) return;
      send(placement, "init", {
        mode: placement.kind,
        diagramIndex: placement.index,
        diagramId: "",
        source: entry.source,
        sourceHash: entry.hash,
        saved,
        theme: currentTheme(),
      });
      placement.ready = true;
      if (placement.kind === "inline") say(placement, "", false);
    } catch (error) {
      if (placement.kind === "inline") say(placement, `Could not start the whiteboard: ${describeError(error)}`, true);
      else showOverlayFailure(describeError(error));
    }
  }

  // ----------------------------------------------------- saving and feedback

  function onSave(placement, message) {
    const flushId = String(message.flushId || "");
    persist(placement, message).then(
      () => flushId && send(placement, "saveResult", { flushId, ok: true }),
      (error) => flushId && send(placement, "saveResult", { flushId, ok: false, error: describeError(error) }),
    );
  }

  async function onQueueFeedback(placement, message) {
    try {
      await persist(placement, message);
      await ask("whiteboard.feedback", {
        index: placement.index,
        body: {
          scene: message.scene || null,
          pngDataUrl: String(message.pngDataUrl || ""),
          summaryLines: boundedLines(message.summaryLines),
        },
      });
      send(placement, "queueResult", { ok: true });
    } catch (error) {
      send(placement, "queueResult", { ok: false, error: describeError(error) });
    }
  }

  // ---------------------------------------------------------------- teardown

  // Asks a frame to lock itself and save one last time. Resolves "saved" when
  // it confirms, "failed" when its save was refused, "timeout" when it said
  // nothing in time.
  let teardownCount = 0;
  function requestTeardown(placement) {
    if (placement.closing) return placement.closing.promise;
    const flushId = `wb-teardown-${++teardownCount}`;
    const closing = { flushId };
    closing.promise = new Promise((resolve) => {
      closing.resolve = resolve;
    });
    closing.timer = window.setTimeout(() => settleTeardown(placement, flushId, "timeout"), TEARDOWN_WAIT_MS);
    placement.closing = closing;
    send(placement, "prepareTeardown", { flushId });
    return closing.promise;
  }

  function settleTeardown(placement, flushId, outcome) {
    const closing = placement.closing;
    if (!closing || closing.flushId !== String(flushId || "")) return;
    window.clearTimeout(closing.timer);
    placement.closing = null;
    closing.resolve(outcome);
  }

  // -------------------------------------------------------------- the overlay

  function buildOverlay() {
    const root = document.createElement("div");
    root.id = OVERLAY_ID;
    skin(root, () => ({
      position: "fixed",
      inset: "0",
      zIndex: "2147483000",
      background: token("scrim"),
      display: root.style.display || "none",
    }));

    const iframe = document.createElement("iframe");
    iframe.sandbox = "allow-scripts allow-popups";
    iframe.title = "Whiteboard (fullscreen)";
    skin(iframe, () => ({
      position: "absolute",
      inset: "0",
      width: "100%",
      height: "100%",
      border: "0",
      background: token("bg"),
      colorScheme: currentTheme(),
    }));

    // Looks like a forum secondary button, on the tokens when present.
    const close = document.createElement("button");
    close.type = "button";
    close.textContent = CLOSE_LABEL;
    skin(close, () => ({
      position: "absolute",
      top: "8px",
      right: "10px",
      zIndex: "1",
      padding: "4px 10px",
      font: "500 13px/1.3 var(--fr-font-sans, system-ui, sans-serif)",
      cursor: "pointer",
      color: token("text"),
      background: token("surface"),
      border: `1px solid ${token("border-strong")}`,
      borderRadius: "var(--fr-radius-sm, 4px)",
    }));
    close.onclick = closeFullscreen;

    root.append(iframe, close);
    document.body.append(root);
    return { root, iframe, close };
  }

  function overlayShell() {
    overlayHost = overlayHost || buildOverlay();
    return overlayHost;
  }

  function showOverlayFailure(text) {
    const host = overlayShell();
    host.root.style.display = "block";
    host.iframe.srcdoc =
      `<p style="font-family:sans-serif;padding:16px;color:${token("danger")};">Could not open the whiteboard: ` +
      `${String(text || "unknown error").replace(/&/g, "&amp;").replace(/</g, "&lt;")}</p>`;
  }

  function openFullscreen(origin) {
    if (fullscreen || origin.kind !== "inline" || !origin.ready || origin.suspended) return;
    requestTeardown(origin).then((outcome) => {
      if (outcome !== "saved" || fullscreen) return;
      const host = overlayShell();
      origin.suspended = true;
      origin.iframe.style.pointerEvents = "none";
      fullscreen = newPlacement("overlay", origin.index, host.iframe);
      fullscreen.origin = origin;
      host.close.textContent = CLOSE_LABEL;
      host.root.style.display = "block";
      host.iframe.removeAttribute("srcdoc");
      host.iframe.src = frameUrl(origin.index);
    });
  }

  function closeFullscreen() {
    const placement = fullscreen;
    if (!placement) return;
    // A frame that never started has nothing to save; a frame that would not
    // answer last time may be closed on the second press.
    if (!placement.ready || placement.forceClose) {
      finishFullscreen(placement);
      return;
    }
    requestTeardown(placement).then((outcome) => {
      if (outcome === "saved") {
        finishFullscreen(placement);
      } else if (outcome === "timeout" && fullscreen === placement) {
        placement.forceClose = true;
        overlayHost.close.textContent = CLOSE_ANYWAY_LABEL;
      }
    });
  }

  function finishFullscreen(placement) {
    placement.dead = true;
    fullscreen = null;
    overlayHost.root.style.display = "none";
    overlayHost.iframe.removeAttribute("srcdoc");
    overlayHost.iframe.src = "about:blank";

    // Two live in-memory scenes are not reconciled: the inline board simply
    // loads again, from whatever the overlay just saved.
    const origin = placement.origin;
    origin.suspended = false;
    origin.iframe.style.pointerEvents = "";
    origin.ready = false;
    origin.channel = "";
    awaitReady(origin);
    origin.iframe.src = frameUrl(origin.index);
  }

  // ----------------------------------------------------------------- routing

  function placementFor(source) {
    if (!source) return null;
    if (fullscreen && fullscreen.iframe.contentWindow === source) return fullscreen;
    return boards.find((placement) => placement.iframe.contentWindow === source) || null;
  }

  function route(placement, type, message) {
    if (Number(message.diagramIndex) !== placement.index) return;
    if (type === "ready") {
      const channel = String(message.channelId || "");
      if (placement.channel || !channel) return;
      placement.channel = channel;
      handshake(placement);
      return;
    }
    if (!placement.channel || message.channelId !== placement.channel) return;

    const live = !placement.suspended;
    if (type === "save" && live) onSave(placement, message);
    else if (type === "queueFeedback" && live) onQueueFeedback(placement, message);
    else if (type === "maximize") openFullscreen(placement);
    else if (type === "teardownReady") settleTeardown(placement, message.flushId, "saved");
    else if (type === "teardownFailed") settleTeardown(placement, message.flushId, "failed");
  }

  window.addEventListener("message", (event) => {
    const message = event.data || {};
    if (typeof message.type !== "string" || !message.type.startsWith(PREFIX)) return;
    const placement = placementFor(event.source);
    if (placement) route(placement, message.type.slice(PREFIX.length), message);
  });

  // Best effort only: a page that is going away may not wait for the round trip
  // to the server, but the frame's own autosave debounce keeps the window small.
  window.addEventListener("beforeunload", () => {
    for (const placement of boards) {
      if (placement && placement.ready && !placement.suspended) {
        send(placement, "flush", { flushId: `wb-unload-${placement.index}` });
      }
    }
    if (fullscreen && fullscreen.ready) send(fullscreen, "flush", { flushId: "wb-unload-overlay" });
  });

  // ------------------------------------------------------------- live theming

  // Repaint what the embed draws itself and tell every running frame, so the
  // boards follow the chrome's theme switch without a reload.
  let paintedTheme = currentTheme();
  function followTheme() {
    const theme = currentTheme();
    if (theme === paintedTheme) return;
    paintedTheme = theme;
    painters.forEach((paint) => paint());
    for (const placement of [...boards, fullscreen]) {
      if (placement && placement.ready) send(placement, "theme", { theme });
    }
  }

  if (typeof MutationObserver === "function") {
    new MutationObserver(followTheme).observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-fr-theme"],
    });
  }
  const colorScheme = window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)");
  if (colorScheme && typeof colorScheme.addEventListener === "function") {
    colorScheme.addEventListener("change", followTheme);
  }

  document.addEventListener("DOMContentLoaded", mountBoards);
  if (document.readyState !== "loading") mountBoards();
})();
