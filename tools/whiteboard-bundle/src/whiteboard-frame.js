// The page inside every whiteboard iframe. It mounts Excalidraw on one board
// and talks to its parent, the embed (whiteboard-embed.js), with the vxb1.
// messages documented there. It never reaches the server itself: saving and
// Queue feedback are requests to the embed, which answers whether they worked.
import "@excalidraw/excalidraw/index.css";
import "./whiteboard-frame.css";

import { CaptureUpdateAction, Excalidraw, MainMenu } from "@excalidraw/excalidraw";
import React from "react";
import { createRoot } from "react-dom/client";

import { askOpeningChoice, confirmLinkDialog, createStatus, make, showFailure } from "./frame-dom.js";
import {
  convertDiagram,
  fitScene,
  getSceneVersion,
  loadFontsFor,
  normalizeElements,
  pickAppState,
  referencedFiles,
  renderPreview,
} from "./frame-scene.js";
import {
  MEASURE_GEN,
  OPEN_ASK,
  OPEN_CONVERT,
  OPEN_REOPEN,
  buildRecord,
  cloneScene,
  decideOpening,
  isImageBoard,
  isLive,
  readRecord,
  recordNeedsMeasuring,
  safeLinkTarget,
  scrubThemeFields,
  summarizeEdits,
} from "./whiteboard-core.js";

const PREFIX = "vxb1.";

// Long enough to coalesce a drag or a burst of typing into one write, short
// enough that closing the tab loses about a second of work (the embed also
// asks for a final state on the way out).
const SAVE_DELAY_MS = 1000;
// How long to wait for the embed to say a Queue feedback request worked.
const QUEUE_WAIT_MS = 60000;
// The canvas stays hidden until fonts have loaded and nodes are sized, but
// never for longer than this: after it the board shows and resizes in place.
const REVEAL_CAP_MS = 5000;
const REMARK_MAX = 1000;
const HELLO_RETRY_MS = 1500;

const query = new URLSearchParams(window.location.search);
const SLOT = /^\d{1,3}$/.test(query.get("slot") || "") ? Number(query.get("slot")) : -1;

// A fresh channel token per frame load: 128 random bits.
function mintToken() {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}
const TOKEN = mintToken();

function post(kind, payload) {
  window.parent.postMessage(Object.assign({ type: PREFIX + kind, token: TOKEN, slot: SLOT }, payload || {}), "*");
}

// ---------------------------------------------------------------- state

const state = {
  started: false,
  mode: "inline",
  palette: query.get("palette") === "light" ? "light" : "dark",
  total: 1,
  text: "",
  recordDigest: "",
  api: null,
  initial: null,
  fresh: false,
  needsMeasure: false,
  pristine: null,
  ready: false,
  frozen: false,
  broken: false,
  dirty: false,
  failedSave: false,
  version: 0,
  seq: 0,
  pendingSaves: new Set(),
  saveTimer: null,
  submitting: false,
  restoreFocus: false,
};

// ------------------------------------------------------------- page shell

const status = createStatus();
const title = make("span", { class: "vxb-name" });
const badge = make("span", { class: "vxb-badge", text: "Image diagram", title: "This diagram type could not be turned into shapes, so it is an image you can draw on." });
badge.hidden = true;
const remark = make("input", {
  type: "text",
  class: "vxb-remark",
  maxlength: String(REMARK_MAX),
  placeholder: "Remark for the agent (optional)",
  "aria-label": "Remark for the agent (optional)",
});
const queueButton = make("button", { type: "button", class: "vxb-btn vxb-btn-primary", text: "Queue feedback" });
const expandButton = make("button", { type: "button", class: "vxb-btn", text: "Fullscreen" });
const banner = make("p", { class: "vxb-banner", role: "note" });
banner.hidden = true;
const boardHost = make("div", { class: "vxb-board", "data-pending": "" });
const overlayHost = make("div", { class: "vxb-overlay-host" });
const main = make("main", { class: "vxb-main" }, [boardHost, overlayHost, status.root]);
const header = make("header", { class: "vxb-bar" }, [
  make("div", { class: "vxb-heading" }, [title, badge]),
  make("div", { class: "vxb-tools" }, [remark, queueButton, expandButton]),
]);

function applyPalette(palette) {
  state.palette = palette;
  document.documentElement.setAttribute("data-fr-theme", palette);
  document.body.setAttribute("data-vxb-palette", palette);
}

function applyMode(mode) {
  state.mode = mode;
  document.body.setAttribute("data-vxb-mode", mode);
  expandButton.hidden = mode === "fullscreen";
}

function refreshControls() {
  const idle = state.ready && !state.frozen;
  queueButton.disabled = !idle || state.submitting;
  expandButton.disabled = !idle;
  queueButton.textContent = state.submitting ? "Queueing..." : "Queue feedback";
  queueButton.setAttribute("aria-busy", state.submitting ? "true" : "false");
}

document.body.append(header, banner, main);
applyPalette(state.palette);
applyMode("inline");
title.textContent = "Whiteboard";
refreshControls();

// --------------------------------------------------------------- React side

class Guard extends React.Component {
  constructor(props) {
    super(props);
    this.state = { failed: false };
  }

  static getDerivedStateFromError() {
    return { failed: true };
  }

  componentDidCatch(error) {
    this.props.onError(error);
  }

  render() {
    return this.state.failed ? null : this.props.children;
  }
}

let root = null;

function boardProps() {
  return {
    excalidrawAPI: onApi,
    initialData: state.initial,
    theme: state.palette,
    viewModeEnabled: state.frozen,
    handleKeyboardGlobally: false,
    autoFocus: false,
    onChange,
    onLinkOpen,
    UIOptions: {
      canvasActions: {
        loadScene: false,
        saveToActiveFile: false,
        toggleTheme: false,
        changeViewBackgroundColor: false,
        export: false,
        saveAsImage: false,
      },
    },
  };
}

function renderBoard() {
  if (!state.initial || state.broken) return;
  if (!root) root = createRoot(boardHost);
  root.render(
    React.createElement(
      Guard,
      { onError: (error) => fail("The whiteboard could not be drawn", error) },
      React.createElement(
        Excalidraw,
        boardProps(),
        React.createElement(MainMenu, null, React.createElement(MainMenu.DefaultItems.ClearCanvas), React.createElement(MainMenu.DefaultItems.Help)),
      ),
    ),
  );
}

function fail(headline, error) {
  state.broken = true;
  state.ready = false;
  clearTimeout(state.saveTimer);
  boardHost.hidden = true;
  showFailure(overlayHost, headline, error, state.text);
  status.clear();
  refreshControls();
}

// ------------------------------------------------------------ saving

function liveElements() {
  return state.api.getSceneElementsIncludingDeleted().filter(isLive);
}

function currentRecord() {
  const elements = liveElements();
  return buildRecord({
    scene: { elements, appState: pickAppState(state.api.getAppState()), files: referencedFiles(elements, state.api.getFiles()) },
    referenceElements: state.pristine,
    digest: state.recordDigest,
    measureGen: MEASURE_GEN,
  });
}

const unsaved = () => state.dirty || state.failedSave || state.pendingSaves.size > 0;

function scheduleSave() {
  clearTimeout(state.saveTimer);
  state.saveTimer = setTimeout(flushSave, SAVE_DELAY_MS);
}

function markDirty() {
  state.dirty = true;
  if (!state.frozen) scheduleSave();
}

function flushSave() {
  clearTimeout(state.saveTimer);
  if (!state.ready || state.frozen || state.broken) return;
  state.seq += 1;
  state.pendingSaves.add(state.seq);
  state.dirty = false;
  post("save", { seq: state.seq, record: currentRecord() });
}

function onSaved(message) {
  state.pendingSaves.delete(message.seq);
  if (message.ok) {
    if (state.failedSave && state.pendingSaves.size === 0) {
      state.failedSave = false;
      status.say("Saved.", { tone: "ok" });
    }
    return;
  }
  state.failedSave = true;
  status.say(`Your latest changes could not be saved (${message.error || "unknown error"}). They stay on this board and are saved again with your next change.`, {
    tone: "error",
    actions: [{ label: "Retry", run: flushSave }],
  });
}

function onChange(elements) {
  if (!state.ready) return;
  const version = getSceneVersion(elements);
  if (version === state.version) return;
  state.version = version;
  if (!state.frozen) markDirty();
}

// ------------------------------------------------------- scene preparation

function refit() {
  if (!state.api) return false;
  const all = state.api.getSceneElementsIncludingDeleted();
  const fitted = fitScene(all);
  const changed = fitted.some((element, index) => element !== all[index]);
  if (changed) state.api.updateScene({ elements: fitted, captureUpdate: CaptureUpdateAction.NEVER });
  if (state.pristine) state.pristine = fitScene(state.pristine);
  return changed;
}

const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

async function settleFonts() {
  // The editor registers its fonts once it has drawn text; give it a moment,
  // then wait for every face it asked for. (A timer, not an animation frame:
  // a board scrolled out of view gets no frames until it is seen.)
  await pause(80);
  await loadFontsFor(state.api.getSceneElementsIncludingDeleted());
  if (document.fonts && document.fonts.ready) await document.fonts.ready;
}

function reveal() {
  boardHost.removeAttribute("data-pending");
  status.clear();
}

async function onApi(api) {
  if (state.api === api) return;
  state.api = api;
  const cap = setTimeout(reveal, REVEAL_CAP_MS);
  try {
    await settleFonts();
    const changed = refit() || state.fresh || state.needsMeasure;
    if (state.fresh) {
      state.pristine = cloneScene(state.api.getSceneElementsIncludingDeleted().filter(isLive));
      // Centres the diagram, zooming out only when it would not fit.
      state.api.scrollToContent(undefined, { fitToContent: true, viewportZoomFactor: 0.9, animate: false });
      await pause(60); // the editor applies the new viewport on its next update
    }
    state.version = getSceneVersion(state.api.getSceneElements());
    state.ready = true;
    refreshControls();
    clearTimeout(cap);
    reveal();
    if (changed) {
      state.dirty = true;
      flushSave();
    }
    if (state.restoreFocus) expandButton.focus();
    if (document.fonts && typeof document.fonts.addEventListener === "function") {
      document.fonts.addEventListener("loadingdone", () => {
        if (state.ready && !state.broken && refit()) markDirty();
      });
    }
  } catch (error) {
    clearTimeout(cap);
    fail("The whiteboard could not be prepared", error);
  }
}

async function begin(message) {
  state.started = true;
  state.text = String(message.text || "");
  state.total = Number.isInteger(message.total) && message.total > 0 ? message.total : 1;
  state.restoreFocus = !!message.restoreFocus;
  applyPalette(message.palette === "light" ? "light" : "dark");
  applyMode(message.mode === "fullscreen" ? "fullscreen" : "inline");
  title.textContent = `Diagram ${SLOT + 1} of ${state.total}`;
  status.say("Preparing the diagram...", { tone: "info", sticky: true });

  const record = readRecord(message.record);
  let outcome = decideOpening({ record, digest: message.digest });
  let kept = false;
  if (outcome === OPEN_ASK) {
    status.clear();
    const choice = await askOpeningChoice(overlayHost);
    outcome = choice === "keep" ? OPEN_REOPEN : OPEN_CONVERT;
    kept = choice === "keep";
    status.say("Preparing the diagram...", { tone: "info", sticky: true });
  }

  let elements;
  let files;
  let appState = {};
  try {
    if (outcome === OPEN_REOPEN) {
      elements = normalizeElements(record.current.elements);
      files = record.current.files || {};
      appState = pickAppState(record.current.appState);
      state.pristine = record.pristine ? cloneScene(record.pristine.elements) : null;
      state.fresh = false;
      state.needsMeasure = recordNeedsMeasuring(record);
      state.recordDigest = record.digest;
    } else {
      const converted = await convertDiagram(state.text);
      elements = converted.elements;
      files = converted.files;
      state.fresh = true;
      state.needsMeasure = true;
      state.recordDigest = message.digest;
    }
  } catch (error) {
    fail("This diagram could not be converted", error);
    return;
  }
  if (kept) {
    banner.textContent = "Showing your saved scene, which predates the current diagram text.";
    banner.hidden = false;
  }
  badge.hidden = !isImageBoard(elements);
  state.initial = { elements, files, appState: { ...appState, viewBackgroundColor: "transparent" } };
  renderBoard();
}

// ----------------------------------------------------------- queue feedback

const waiting = new Map(); // reqId -> {resolve, timer}
let reqCounter = 0;

function askEmbedToQueue(payload) {
  return new Promise((resolve) => {
    const reqId = ++reqCounter;
    const timer = setTimeout(() => {
      waiting.delete(reqId);
      resolve({ ok: false, error: "no answer from the page" });
    }, QUEUE_WAIT_MS);
    waiting.set(reqId, { resolve, timer });
    post("submit", Object.assign({ reqId }, payload));
  });
}

function onQueued(message) {
  const entry = waiting.get(message.reqId);
  if (!entry) return;
  waiting.delete(message.reqId);
  clearTimeout(entry.timer);
  entry.resolve({ ok: !!message.ok, error: message.error });
}

async function queueFeedback() {
  if (state.submitting || !state.ready || state.frozen) return;
  state.submitting = true;
  refreshControls();
  status.say("Queueing feedback...", { tone: "info", sticky: true });
  try {
    const elements = liveElements();
    const appState = state.api.getAppState();
    const files = referencedFiles(elements, state.api.getFiles());
    let png = "";
    try {
      png = await renderPreview(elements, appState, files);
    } catch (_) {
      png = ""; // the preview is optional; the scene still goes
    }
    const result = await askEmbedToQueue({
      current: scrubThemeFields({ elements, appState: pickAppState(appState), files }),
      png,
      editLines: summarizeEdits(state.pristine, elements),
      remark: remark.value.slice(0, REMARK_MAX),
    });
    if (result.ok) {
      remark.value = "";
      status.say("Feedback queued for the agent.", { tone: "ok" });
    } else {
      status.say(`Feedback was not queued: ${result.error || "unknown error"}.`, { tone: "error", actions: [{ label: "Try again", run: queueFeedback }] });
    }
  } catch (error) {
    status.say(`Feedback was not queued: ${(error && error.message) || error}.`, { tone: "error", actions: [{ label: "Try again", run: queueFeedback }] });
  } finally {
    state.submitting = false;
    refreshControls();
  }
}

queueButton.addEventListener("click", queueFeedback);
expandButton.addEventListener("click", () => {
  if (state.ready && !state.frozen) post("expand");
});

// ------------------------------------------------------------- Escape key

// In fullscreen, Escape is also the way back (the embed owns the overlay and
// handles it when focus is outside this frame). Inside the frame Escape first
// belongs to the editor: it cancels a selection, a text edit, a menu or a
// dialog. Only a press with nothing of that to cancel asks to leave.
function nothingToCancel() {
  if (!state.api || document.querySelector(".vxb-scrim, .vxb-choice")) return false;
  const app = state.api.getAppState();
  const open = app.openMenu || app.openPopup || app.openDialog || app.contextMenu || app.editingTextElement || app.newElement || app.editingLinearElement;
  return !open && Object.keys(app.selectedElementIds || {}).length === 0 && !app.selectedLinearElement;
}

// Captured, because the editor stops the key before it bubbles; the state it
// is judged on is therefore the one from before the editor handles the key.
window.addEventListener(
  "keydown",
  (event) => {
    if (event.key !== "Escape" || state.mode !== "fullscreen" || event.isComposing) return;
    if (nothingToCancel()) post("leave");
  },
  true,
);

// -------------------------------------------------------------------- links

async function onLinkOpen(element, event) {
  event.preventDefault();
  const url = safeLinkTarget(element && element.link);
  if (!url) {
    const shown = String((element && element.link) || "").slice(0, 80);
    status.say(`Blocked a link that is not http, https or mailto: ${shown}`, { tone: "error" });
    return;
  }
  if (await confirmLinkDialog(url)) window.open(url, "_blank", "noopener,noreferrer");
}

// ---------------------------------------------------- freeze and final state

function setFrozen(on) {
  if (state.frozen === on) return;
  state.frozen = on;
  if (on) clearTimeout(state.saveTimer);
  refreshControls();
  renderBoard();
  if (!on && state.dirty) scheduleSave();
}

function answerSnapshot(message) {
  if (message.freeze) setFrozen(true);
  const live = state.ready && !state.broken;
  post("final", { reqId: message.reqId, record: live ? currentRecord() : null, unsaved: live ? unsaved() : false });
}

// ---------------------------------------------------------------- messages

window.addEventListener("message", (event) => {
  if (event.source !== window.parent) return;
  const message = event.data;
  if (!message || typeof message.type !== "string" || message.type.indexOf(PREFIX) !== 0) return;
  if (message.token !== TOKEN || message.slot !== SLOT) return;
  switch (message.type.slice(PREFIX.length)) {
    case "start":
      if (!state.started) begin(message);
      break;
    case "saved":
      onSaved(message);
      break;
    case "queued":
      onQueued(message);
      break;
    case "palette":
      applyPalette(message.palette === "light" ? "light" : "dark");
      renderBoard();
      break;
    case "snap":
      answerSnapshot(message);
      break;
    case "thaw":
      setFrozen(false);
      break;
    default:
      break;
  }
});

if (SLOT < 0) {
  boardHost.hidden = true;
  showFailure(overlayHost, "This whiteboard address is not valid", new Error("The frame was opened without a board number."), "");
} else {
  post("hello");
  const retry = setInterval(() => {
    if (state.started) clearInterval(retry);
    else post("hello");
  }, HELLO_RETRY_MS);
}
