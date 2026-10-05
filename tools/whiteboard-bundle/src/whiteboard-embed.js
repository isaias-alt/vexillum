// Whiteboard embed: injected by the forum server into every artifact that has
// at least one <div class="mermaid"> block. It puts one editable whiteboard
// (an iframe running the Excalidraw frame) in the place of each block, and
// owns everything that has to live in the artifact's own page: the lock cover,
// the fullscreen overlay, persistence, and the live theme.
//
// Plain script, no imports (build.js copies it as is). It never calls the
// network itself: every server operation goes through the artifact SDK's
// internal bridge (window.forum.__rpc), which the forum chrome answers.
//
// Vocabulary
//   board      one diagram, identified by its ordinal (document order)
//   placement  one iframe showing a board: the inline one, or the overlay's
//   record     what is stored per board (see src/scene-record.js)
//
// Messages with frames (every kind starts with "vxb1." and carries the frame's
// channel token and the board ordinal as `slot`)
//   frame -> embed
//     vxb1.hello    {token, slot}                     frame is alive
//     vxb1.save     {seq, record}                     debounced autosave
//     vxb1.submit   {reqId, current, png, editLines, remark}   Queue feedback
//     vxb1.expand                                      wants fullscreen
//     vxb1.leave                                       Escape pressed in the fullscreen frame
//     vxb1.final    {reqId, record, unsaved}          answer to vxb1.snap
//   embed -> frame
//     vxb1.start    {mode, text, digest, record, palette, total, restoreFocus}
//     vxb1.saved    {seq, ok, error}
//     vxb1.queued   {reqId, ok, error}
//     vxb1.palette  {palette}
//     vxb1.snap     {reqId, freeze}                   give me your final state
//     vxb1.thaw                                       unfreeze after a failure
(function () {
  "use strict";

  if (window.__vxbEmbed) return;
  window.__vxbEmbed = true;

  // ------------------------------------------------------------ constants

  const PREFIX = "vxb1.";
  const FRAME_PATH = "/whiteboard-frame";
  const TOKENS_HREF = "/forum-assets/forum-tokens.css";
  const OVERLAY_ID = "vxb-overlay";
  const SLOT_CLASS = "vxb-slot";

  // A cold start has to download and run a multi-megabyte bundle and fetch its
  // fonts: on a slow laptop over a busy disk that is well past ten seconds,
  // and only a frame that has not reported in after this long is called
  // broken.
  const STARTUP_WAIT_MS = 45000;
  // A live frame answers a final-state request in a few milliseconds; this is
  // the wait before one is declared unresponsive.
  const FINAL_WAIT_MS = 3000;
  // Server limits on what Queue feedback may carry (the server clips again).
  const MAX_EDIT_LINES = 50;
  const MAX_EDIT_LINE_CHARS = 300;
  const MAX_REMARK_CHARS = 1000;
  const PNG_PREFIX = "data:image/png;base64,";
  // Height of the frame's own header strip; the lock cover starts below it so
  // the header's buttons stay usable on a locked board. Mirrors the frame CSS.
  const HEADER_PX = 44;
  const STAGE_HEIGHT_PX = 520;

  // ----------------------------------------------------------------- state

  const boards = new Map(); // ordinal -> board
  let overlayBoard = null; // the board shown fullscreen, if any
  let diagramCount = 0;
  let requestCounter = 0;

  const clip = (value, max) => String(value === null || value === undefined ? "" : value).slice(0, max);
  const errorText = (error) => String((error && error.message) || error || "no details given");

  // ------------------------------------------------------------------ theme

  function readPalette() {
    const attr = document.documentElement.getAttribute("data-fr-theme");
    if (attr === "light" || attr === "dark") return attr;
    try {
      if (window.matchMedia && window.matchMedia("(prefers-color-scheme: light)").matches) return "light";
    } catch (_) {
      /* no matchMedia: dark, the forum default */
    }
    return "dark";
  }

  let palette = "dark";

  // The forum tokens switch on <html data-fr-theme>. When the attribute is
  // missing (an artifact opened outside the chrome) the OS preference decides
  // and the embed writes the attribute itself, so the tokens resolve.
  function ensureThemeAttribute() {
    const attr = document.documentElement.getAttribute("data-fr-theme");
    if (attr !== "light" && attr !== "dark") document.documentElement.setAttribute("data-fr-theme", palette);
  }

  function paintPalette() {
    const slots = [];
    for (const board of boards.values()) slots.push(board.slot);
    const overlay = document.getElementById(OVERLAY_ID);
    if (overlay) slots.push(overlay);
    for (const node of slots) {
      node.setAttribute("data-vxb-palette", palette);
      for (const frame of node.querySelectorAll("iframe")) frame.style.colorScheme = palette;
    }
  }

  function onThemeChange() {
    const next = readPalette();
    if (next === palette) return;
    palette = next;
    ensureThemeAttribute();
    paintPalette();
    for (const board of boards.values()) {
      for (const placement of [board.inline, board.full]) {
        if (placement && placement.token && placement.started) send(placement, "palette", { palette });
      }
    }
  }

  // ------------------------------------------------------------ DOM helpers

  function make(tag, props, children) {
    const node = document.createElement(tag);
    for (const [name, value] of Object.entries(props || {})) {
      if (name === "class") node.className = value;
      else if (name === "text") node.textContent = value;
      else node.setAttribute(name, value);
    }
    for (const child of children || []) node.appendChild(child);
    return node;
  }

  const STYLE = `
.${SLOT_CLASS} { position: relative; margin: 16px 0; border: 1px solid var(--fr-border-strong); border-radius: var(--fr-radius-md); background: var(--fr-bg); color: var(--fr-text); font: 13px/1.5 var(--fr-font-sans); overflow: hidden; }
.vxb-stage { position: relative; height: ${STAGE_HEIGHT_PX}px; background: var(--fr-bg); }
.vxb-frame { display: block; width: 100%; height: 100%; border: 0; background: var(--fr-bg); }
.vxb-cover { position: absolute; left: 0; right: 0; bottom: 0; top: ${HEADER_PX}px; display: flex; align-items: flex-end; justify-content: flex-start; flex-direction: column; gap: var(--fr-space-2); background: transparent; cursor: pointer; padding: var(--fr-space-4); }
.vxb-cover:hover { background: var(--fr-accent-soft); }
.vxb-cover[data-vxb-reason="away"] { top: 0; align-items: center; justify-content: center; background: var(--fr-scrim); cursor: default; }
.vxb-cover-text { color: var(--fr-text); background: var(--fr-surface); border: 1px solid var(--fr-border); border-radius: var(--fr-radius-sm); padding: var(--fr-space-1) var(--fr-space-3); }
.vxb-unlock, .vxb-back, .vxb-leave { font: inherit; font-weight: 600; color: var(--fr-accent-contrast); background: var(--fr-accent); border: 1px solid var(--fr-accent); border-radius: var(--fr-radius-sm); padding: 0 var(--fr-space-4); box-sizing: border-box; height: 28px; cursor: pointer; }
.vxb-unlock:hover, .vxb-back:hover { background: var(--fr-accent-hover); }
.vxb-leave { color: var(--fr-text); background: var(--fr-surface); border-color: var(--fr-danger); flex: none; white-space: nowrap; }
.vxb-unlock:focus-visible, .vxb-back:focus-visible, .vxb-leave:focus-visible { outline: 2px solid var(--fr-text); outline-offset: 2px; }
.vxb-note { margin: 0; padding: var(--fr-space-2) var(--fr-space-3); color: var(--fr-text-secondary); border-top: 1px solid var(--fr-border); background: var(--fr-surface); min-height: 1.5em; }
.vxb-note:empty { display: none; }
.vxb-note[data-vxb-tone="error"] { color: var(--fr-danger); }
.vxb-notice { padding: var(--fr-space-4); background: var(--fr-surface); color: var(--fr-text); }
.vxb-notice p { margin: 0 0 var(--fr-space-2); }
.vxb-notice pre { margin: var(--fr-space-2) 0 0; padding: var(--fr-space-3); overflow: auto; max-height: 320px; font: 12px/1.5 var(--fr-font-mono); color: var(--fr-text); background: var(--fr-surface-sunken); border: 1px solid var(--fr-border); border-radius: var(--fr-radius-sm); white-space: pre-wrap; }
#${OVERLAY_ID} { position: fixed; inset: 0; z-index: 2147483000; background: var(--fr-bg); color: var(--fr-text); font: 13px/1.5 var(--fr-font-sans); }
#${OVERLAY_ID} .vxb-frame { position: absolute; inset: 0; }
#${OVERLAY_ID} .vxb-way { position: absolute; top: var(--fr-space-2); right: var(--fr-space-3); z-index: 2; }
#${OVERLAY_ID} .vxb-overlay-note { position: absolute; left: 50%; bottom: 64px; transform: translateX(-50%); z-index: 2; display: flex; gap: var(--fr-space-3); align-items: center; max-width: min(90vw, 640px); padding: var(--fr-space-2) var(--fr-space-3); background: var(--fr-surface); color: var(--fr-text); border: 1px solid var(--fr-danger); border-radius: var(--fr-radius-md); box-shadow: var(--fr-shadow-md); }
#${OVERLAY_ID} .vxb-overlay-note[hidden] { display: none; }
@media (prefers-reduced-motion: no-preference) { .vxb-unlock { transition: background-color 120ms ease; } }
`;

  function installStyles() {
    const head = document.head || document.documentElement;
    const hasTokens = Array.from(document.querySelectorAll("link")).some((link) => (link.getAttribute("href") || "").indexOf(TOKENS_HREF) === 0);
    if (!hasTokens) head.appendChild(make("link", { rel: "stylesheet", href: TOKENS_HREF }));
    head.appendChild(make("style", { "data-vxb": "" }, [document.createTextNode(STYLE)]));
  }

  // ------------------------------------------------------------ bridge calls

  function rpc(op, payload) {
    const bridge = window.forum && window.forum.__rpc;
    if (typeof bridge !== "function") return Promise.reject(new Error("the forum chrome is not available"));
    return bridge(op, payload);
  }

  // ----------------------------------------------------------- frame traffic

  const validToken = (token) => typeof token === "string" && /^[0-9a-f]{32,}$/.test(token);

  function send(placement, kind, payload) {
    const target = placement.iframe && placement.iframe.contentWindow;
    if (!target || !placement.token) return;
    target.postMessage(Object.assign({ type: PREFIX + kind, token: placement.token, slot: placement.board.ordinal }, payload || {}), "*");
  }

  function allPlacements() {
    const list = [];
    for (const board of boards.values()) {
      if (board.inline) list.push(board.inline);
      if (board.full) list.push(board.full);
    }
    return list;
  }

  function validRecord(record) {
    return !!record && typeof record === "object" && record.format === 2 && typeof record.digest === "string" &&
      !!record.current && Array.isArray(record.current.elements);
  }

  // Writes for one board go out strictly in order, so an older autosave can
  // never land after a newer one.
  function persist(board, record) {
    const run = () => rpc("board.write", { ordinal: board.ordinal, body: record });
    board.writes = (board.writes || Promise.resolve()).then(run, run);
    const result = board.writes;
    board.writes = result.catch(() => {});
    return result;
  }

  // -------------------------------------------------------- final-state flow

  // Asks a placement's frame for its final state and persists it. Resolves
  // {ok: true, record} or {ok: false, reason, error}. At most one request per
  // placement is outstanding: a second one waits for the first to settle.
  // With freeze, the frame goes read-only and stops autosaving, and the embed
  // stops accepting its saves; a failure thaws it again.
  function requestFinal(placement, options) {
    const freeze = !!(options && options.freeze);
    const run = () => finalExchange(placement, freeze);
    placement.finalChain = (placement.finalChain || Promise.resolve()).then(run, run);
    return placement.finalChain;
  }

  function finalExchange(placement, freeze) {
    return new Promise((resolve) => {
      if (!placement.token || !placement.started) {
        // A frame that never started has no edits to lose.
        resolve({ ok: true, record: null });
        return;
      }
      const reqId = ++requestCounter;
      if (freeze) placement.frozen = true;
      let settled = false;
      const settle = (result) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        placement.outstanding = null;
        if (!result.ok && freeze) {
          placement.frozen = false;
          send(placement, "thaw");
        }
        resolve(result);
      };
      const timer = setTimeout(() => settle({ ok: false, reason: "timeout", error: "the whiteboard did not answer" }), FINAL_WAIT_MS);
      placement.outstanding = {
        id: reqId,
        answer(message) {
          if (message.record !== null && message.record !== undefined && !validRecord(message.record)) {
            settle({ ok: false, reason: "malformed", error: "the whiteboard sent an unreadable state" });
            return;
          }
          const record = message.record || null;
          if (!record || message.unsaved === false) {
            settle({ ok: true, record });
            return;
          }
          persist(placement.board, record).then(
            () => settle({ ok: true, record }),
            (error) => settle({ ok: false, reason: "persist", error: errorText(error) }),
          );
        },
      };
      send(placement, "snap", { reqId, freeze });
    });
  }

  // -------------------------------------------------------------- messages

  function placementOf(source) {
    if (!source) return null;
    for (const placement of allPlacements()) {
      if (placement.iframe && placement.iframe.contentWindow === source) return placement;
    }
    return null;
  }

  function onMessage(event) {
    const data = event.data;
    if (!data || typeof data !== "object" || typeof data.type !== "string" || data.type.indexOf(PREFIX) !== 0) return;
    const placement = placementOf(event.source);
    if (!placement || !validToken(data.token) || data.slot !== placement.board.ordinal) return;
    const kind = data.type.slice(PREFIX.length);

    if (kind === "hello") {
      if (placement.token) return; // the first announcement binds the token
      placement.token = data.token;
      onHello(placement);
      return;
    }
    if (data.token !== placement.token) return;

    switch (kind) {
      case "save":
        onSave(placement, data);
        break;
      case "submit":
        onSubmit(placement, data);
        break;
      case "expand":
        if (placement === placement.board.inline) openFullscreen(placement.board);
        break;
      case "leave":
        if (placement === placement.board.full) leaveFullscreen(false);
        break;
      case "final":
        if (placement.outstanding && placement.outstanding.id === data.reqId) placement.outstanding.answer(data);
        break;
      default:
        break;
    }
  }

  function onHello(placement) {
    clearTimeout(placement.startTimer);
    const board = placement.board;
    if (board.note.textContent === LOADING_TEXT) setNote(board, "");
    const begin = (record) => {
      if (placement.dead) return;
      placement.started = true;
      send(placement, "start", {
        mode: placement.mode,
        text: board.diagram.text,
        digest: board.diagram.digest,
        record,
        palette,
        total: diagramCount,
        restoreFocus: !!placement.restoreFocus,
      });
      placement.restoreFocus = false;
    };
    if (placement.mode === "fullscreen" && board.handoff !== undefined) {
      const record = board.handoff;
      board.handoff = undefined;
      begin(record);
      return;
    }
    rpc("board.read", { ordinal: board.ordinal }).then(
      (result) => begin(result && validRecord(result.record) ? result.record : null),
      () => begin(null),
    );
  }

  function onSave(placement, message) {
    if (placement.frozen || !placement.started) return;
    const seq = message.seq;
    if (!validRecord(message.record)) {
      send(placement, "saved", { seq, ok: false, error: "the whiteboard sent an unreadable record" });
      return;
    }
    persist(placement.board, message.record).then(
      () => send(placement, "saved", { seq, ok: true }),
      (error) => send(placement, "saved", { seq, ok: false, error: errorText(error) }),
    );
  }

  function onSubmit(placement, message) {
    const reqId = message.reqId;
    const reply = (ok, error) => send(placement, "queued", { reqId, ok, error: error || "" });
    if (placement.submitting) {
      reply(false, "a request is already in progress");
      return;
    }
    if (!message.current || typeof message.current !== "object") {
      reply(false, "the whiteboard sent no scene");
      return;
    }
    const body = {
      current: message.current,
      edit_lines: (Array.isArray(message.editLines) ? message.editLines : [])
        .filter((line) => typeof line === "string")
        .slice(0, MAX_EDIT_LINES)
        .map((line) => clip(line, MAX_EDIT_LINE_CHARS)),
      remark: clip(typeof message.remark === "string" ? message.remark : "", MAX_REMARK_CHARS),
    };
    if (typeof message.png === "string" && message.png.indexOf(PNG_PREFIX) === 0) body.png = message.png;
    placement.submitting = true;
    rpc("board.submit", { ordinal: placement.board.ordinal, body }).then(
      () => {
        placement.submitting = false;
        reply(true);
      },
      (error) => {
        placement.submitting = false;
        reply(false, errorText(error));
      },
    );
  }

  // ------------------------------------------------------------ placements

  function frameUrl(board) {
    return FRAME_PATH + "?slot=" + board.ordinal + "&palette=" + palette;
  }

  function frameTitle(board, suffix) {
    return "Whiteboard " + (board.ordinal + 1) + " of " + diagramCount + (suffix || "");
  }

  function newPlacement(board, mode) {
    const iframe = make("iframe", {
      class: "vxb-frame",
      title: frameTitle(board, mode === "fullscreen" ? " (fullscreen)" : ""),
      sandbox: "allow-scripts allow-popups allow-popups-to-escape-sandbox",
      src: frameUrl(board),
    });
    iframe.style.colorScheme = palette;
    const placement = {
      board, mode, iframe, token: null, started: false, frozen: false, dead: false,
      outstanding: null, finalChain: null, submitting: false, startTimer: null, restoreFocus: false,
    };
    placement.startTimer = setTimeout(() => onStartTimeout(placement), STARTUP_WAIT_MS);
    return placement;
  }

  function retirePlacement(placement) {
    if (!placement) return;
    placement.dead = true;
    clearTimeout(placement.startTimer);
    if (placement.iframe && placement.iframe.parentNode) placement.iframe.remove();
  }

  const LOADING_TEXT = "Loading whiteboard...";
  const STARTUP_HELP =
    "The whiteboard did not start. Reload this page; if it keeps happening, restart the forum server with `vx forum stop` and open the artifact again.";

  function onStartTimeout(placement) {
    if (placement.token) return;
    if (placement.mode === "fullscreen") {
      showOverlayNote(STARTUP_HELP, false);
    } else {
      retirePlacement(placement);
      showNotice(placement.board, STARTUP_HELP);
      setNote(placement.board, "");
    }
  }

  // ----------------------------------------------------------------- slots

  function showNotice(board, message) {
    board.stage.textContent = "";
    const children = [make("p", { role: "alert", text: message })];
    if (board.fallbackText) children.push(make("pre", { text: board.fallbackText }));
    board.stage.appendChild(make("div", { class: "vxb-notice" }, children));
    board.stage.style.height = "auto";
  }

  function setNote(board, message, tone) {
    board.note.textContent = message || "";
    if (tone) board.note.setAttribute("data-vxb-tone", tone);
    else board.note.removeAttribute("data-vxb-tone");
  }

  function lockCover(board, reason) {
    unlockCover(board);
    const cover = make("div", { class: "vxb-cover" });
    if (reason === "away") {
      cover.setAttribute("data-vxb-reason", "away");
      cover.appendChild(make("span", { class: "vxb-cover-text", text: "Open in fullscreen" }));
      board.cover = cover;
    } else {
      const button = make("button", {
        type: "button",
        class: "vxb-unlock",
        "aria-label": "Edit diagram " + (board.ordinal + 1) + " of " + diagramCount + ": unlock the whiteboard",
        text: "Edit diagram",
      });
      button.addEventListener("click", (event) => {
        event.stopPropagation();
        unlockBoard(board, true);
      });
      cover.addEventListener("click", () => unlockBoard(board, false));
      cover.appendChild(button);
      cover.appendChild(make("span", { class: "vxb-cover-text", text: "Locked so the page scrolls normally" }));
      board.cover = cover;
    }
    board.stage.appendChild(cover);
  }

  function unlockCover(board) {
    if (board.cover) board.cover.remove();
    board.cover = null;
  }

  function unlockBoard(board, focusFrame) {
    board.unlocked = true;
    unlockCover(board);
    const iframe = board.inline && board.inline.iframe;
    if (iframe) {
      iframe.removeAttribute("tabindex");
      if (focusFrame && typeof iframe.focus === "function") iframe.focus();
    }
  }

  function mountInline(board) {
    const placement = newPlacement(board, "inline");
    board.inline = placement;
    if (!board.unlocked) placement.iframe.setAttribute("tabindex", "-1");
    board.stage.insertBefore(placement.iframe, board.stage.firstChild);
    setNote(board, LOADING_TEXT);
    paintPalette();
  }

  function buildSlot(block, ordinal) {
    const stage = make("div", { class: "vxb-stage" });
    const note = make("p", { class: "vxb-note", role: "status", "aria-live": "polite" });
    const slot = make("div", { class: SLOT_CLASS, "data-vxb-palette": palette, "data-vxb-board": String(ordinal) }, [stage, note]);
    block.parentNode.insertBefore(slot, block);
    block.style.display = "none";
    return { stage, note, slot };
  }

  function mountBoard(block, ordinal, listing, failure) {
    const parts = buildSlot(block, ordinal);
    const board = Object.assign(parts, {
      ordinal, block, diagram: null, inline: null, full: null, cover: null, unlocked: false,
      phase: "idle", handoff: undefined, writes: null,
      fallbackText: (block.textContent || "").trim(),
    });
    boards.set(ordinal, board);
    if (failure) {
      showNotice(board, "The whiteboard could not start: " + failure + ". It needs the forum chrome around this page (open the artifact with `vx forum <file>`). The diagram text is shown below.");
      return;
    }
    const diagram = listing && listing.find((item) => item && item.ordinal === ordinal);
    if (!diagram || typeof diagram.text !== "string" || typeof diagram.digest !== "string") {
      showNotice(board, "This diagram's text can no longer be found in the artifact, so it cannot be opened as a whiteboard. It may have been edited or removed: reload the page to see the current version.");
      return;
    }
    board.diagram = diagram;
    mountInline(board);
    lockCover(board, "locked");
  }

  // ------------------------------------------------------------ fullscreen

  function showOverlayNote(message, offerLeave) {
    const overlay = document.getElementById(OVERLAY_ID);
    if (!overlay) return;
    const note = overlay.querySelector(".vxb-overlay-note");
    note.querySelector("span").textContent = message;
    note.querySelector(".vxb-leave").hidden = !offerLeave;
    note.hidden = false;
  }

  function setPageInert(on) {
    const body = document.body;
    if (on) {
      overlayBoard.inerted = [];
      for (const child of Array.from(body.children)) {
        if (child.id === OVERLAY_ID || child.hasAttribute("inert")) continue;
        child.setAttribute("inert", "");
        overlayBoard.inerted.push(child);
      }
      overlayBoard.priorOverflow = document.documentElement.style.overflow;
      document.documentElement.style.overflow = "hidden";
    } else if (overlayBoard) {
      for (const child of overlayBoard.inerted || []) child.removeAttribute("inert");
      document.documentElement.style.overflow = overlayBoard.priorOverflow || "";
    }
  }

  async function openFullscreen(board) {
    if (overlayBoard || board.phase !== "idle" || !board.inline) return;
    board.phase = "opening";
    setNote(board, "");
    const result = await requestFinal(board.inline, { freeze: true });
    if (!result.ok) {
      board.phase = "idle";
      setNote(board, "Could not open fullscreen: " + result.error + ". Nothing was changed, try again.", "error");
      return;
    }
    board.handoff = result.record;
    overlayBoard = board;
    board.phase = "fullscreen";
    lockCover(board, "away");
    board.inline.iframe.setAttribute("inert", "");

    const back = make("button", { type: "button", class: "vxb-back", text: "Back to page" });
    back.addEventListener("click", () => leaveFullscreen(false));
    const leave = make("button", { type: "button", class: "vxb-leave", text: "Leave without saving" });
    leave.hidden = true;
    leave.addEventListener("click", () => leaveFullscreen(true));
    const noteText = make("span", { text: "" });
    const note = make("div", { class: "vxb-overlay-note", role: "alert" }, [noteText, leave]);
    note.hidden = true;
    board.full = newPlacement(board, "fullscreen");
    const overlay = make("div", {
      id: OVERLAY_ID,
      role: "dialog",
      "aria-modal": "true",
      "aria-label": frameTitle(board, ", fullscreen"),
      "data-vxb-palette": palette,
    }, [make("div", { class: "vxb-way" }, [back]), board.full.iframe, note]);
    document.body.appendChild(overlay);
    setPageInert(true);
    if (typeof back.focus === "function") back.focus();
  }

  // Leaves fullscreen. The safe path first obtains the overlay frame's final
  // state and persists it; if that fails the overlay stays and offers a
  // separate "Leave without saving" button, which is the only forced exit.
  async function leaveFullscreen(force) {
    const board = overlayBoard;
    if (!board || board.phase !== "fullscreen") return;
    if (!force) {
      board.phase = "closing";
      const result = await requestFinal(board.full, { freeze: true });
      if (!result.ok) {
        board.phase = "fullscreen";
        showOverlayNote("Could not save the fullscreen edits (" + result.error + "). The board is still open. Try Back to page again, or leave without saving.", true);
        return;
      }
    }
    board.phase = "closing";
    retirePlacement(board.full);
    board.full = null;
    const overlay = document.getElementById(OVERLAY_ID);
    setPageInert(false);
    if (overlay) overlay.remove();
    overlayBoard = null;
    board.inerted = null;

    // The inline board starts over from what the fullscreen session saved.
    retirePlacement(board.inline);
    unlockCover(board);
    board.inline = null;
    board.unlocked = true;
    mountInline(board);
    board.inline.restoreFocus = true;
    board.phase = "idle";
    // The control that opened fullscreen lives inside the frame: focus the
    // frame now and ask it (restoreFocus) to put focus on that control.
    if (typeof board.inline.iframe.focus === "function") board.inline.iframe.focus();
  }

  // -------------------------------------------------------------- unload

  // Best effort: ask every live frame for its final state and let the embed
  // persist whatever comes back. Nothing waits on it.
  function flushAll() {
    for (const placement of allPlacements()) {
      if (placement.started && !placement.frozen && !placement.dead) {
        requestFinal(placement, { freeze: false }).catch(() => {});
      }
    }
  }

  // ------------------------------------------------------------------ start

  async function start() {
    const blocks = Array.from(document.querySelectorAll("div.mermaid"));
    if (blocks.length === 0) return;
    palette = readPalette();
    ensureThemeAttribute();
    installStyles();

    let listing = null;
    let failure = "";
    try {
      const result = await rpc("board.list", {});
      listing = result && Array.isArray(result.diagrams) ? result.diagrams : [];
    } catch (error) {
      failure = errorText(error);
    }
    diagramCount = listing ? listing.length : blocks.length;
    blocks.forEach((block, ordinal) => mountBoard(block, ordinal, listing, failure));
    paintPalette();
  }

  window.addEventListener("message", onMessage);
  window.addEventListener("pagehide", flushAll);
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "hidden") flushAll();
  });
  document.addEventListener(
    "keydown",
    (event) => {
      if (event.key === "Escape" && overlayBoard && !event.defaultPrevented) {
        event.preventDefault();
        leaveFullscreen(false);
      }
    },
    true,
  );
  if (typeof MutationObserver === "function") {
    new MutationObserver(onThemeChange).observe(document.documentElement, { attributes: true, attributeFilter: ["data-fr-theme"] });
  }
  if (window.matchMedia) {
    try {
      window.matchMedia("(prefers-color-scheme: light)").addEventListener("change", onThemeChange);
    } catch (_) {
      /* older engines: the attribute observer still covers the chrome's toggle */
    }
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", start);
  else start();
})();
