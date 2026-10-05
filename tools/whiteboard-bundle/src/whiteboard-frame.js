/* global document, window, location, FileReader */

// The whiteboard frame: the page that runs inside every whiteboard iframe and
// mounts Excalidraw on one diagram's Mermaid source. build.js bundles it, with
// Excalidraw, the Mermaid converter (and its pinned mermaid) and React, into
// whiteboard.js, so nothing here touches the network.
//
// Two placements share this file. "inline" is the embedded board that
// whiteboard-embed.js puts where a `.mermaid` block was; "overlay" is the same
// board shown fullscreen over the page. Both are sandboxed
// (allow-scripts allow-popups, no allow-same-origin) and their embedder is
// window.parent: the artifact page. The frame never talks to the server. It
// only exchanges postMessage records with the embedder, which relays them
// through the forum chrome; every record carries the channel id this frame
// invented when it announced itself.
//
// Frame -> embedder:  ready, save, queueFeedback, maximize, teardownReady,
//                     teardownFailed, flushComplete   (prefix "vx-whiteboard:")
// Embedder -> frame:  init, theme, sourceChanged, prepareTeardown, flush,
//                     saveResult, queueResult

import { parseMermaidToExcalidraw } from "@excalidraw/mermaid-to-excalidraw";
import {
  convertToExcalidrawElements,
  Excalidraw,
  exportToBlob,
  exportToCanvas,
  FONT_FAMILY,
  restore,
} from "@excalidraw/excalidraw";
import React from "react";
import { createRoot } from "react-dom/client";
import "@excalidraw/excalidraw/index.css";
import "./whiteboard-frame.css";

import {
  chooseStartMode,
  convertTwice,
  fitLabelsToNodes,
  fixSkeletonLabels,
  isImageOnly,
  persistenceFields,
  plainCopy,
  remeasureText,
  repeatedIds,
  safeLinkTarget,
  sizeNodeSkeletons,
  summarizeEdits,
  TEXT_METRICS_VERSION,
  withoutThemeFields,
} from "./whiteboard-core.js";

const AUTOSAVE_DELAY_MS = 800;
const STATUS_VISIBLE_MS = 4000;
const MSG = (name) => `vx-whiteboard:${name}`;

const IMAGE_ONLY_NOTICE =
  "This diagram type is not natively editable, so it is shown as an image - draw, annotate, and add shapes on top.";

// Everything that changes while a board lives, in one place.
const board = {
  placement: "overlay", // "inline" | "overlay"
  index: 0,
  diagramId: "",
  channel: "",
  theme: "dark",
  source: "",
  sourceHash: "", // hash of the diagram as the page has it now
  sceneSourceHash: "", // hash of the source the scene on screen came from
  baselineElements: [],
  textMetricsVersion: TEXT_METRICS_VERSION,
  imageOnly: false,
  api: null, // Excalidraw's imperative API, once mounted
  autosaveTimer: 0,
  closingFlush: "", // flush id of a teardown in progress
  pendingFlushes: new Set(),
  queueing: false,
  setLocked: null, // React setters, registered by <Editor>
  setTheme: null,
};

// ------------------------------------------------------------------ plumbing

function describeError(error) {
  return error instanceof Error ? error.message : String(error);
}

function randomChannel() {
  if (window.crypto && window.crypto.randomUUID) return window.crypto.randomUUID();
  return `wb-${Math.random().toString(36).slice(2)}-${Date.now().toString(36)}`;
}

function tell(type, fields = {}) {
  window.parent.postMessage({ ...fields, type: MSG(type), diagramIndex: board.index, channelId: board.channel }, "*");
}

function make(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  Object.assign(node, attrs);
  node.append(...children);
  return node;
}

const byId = (id) => document.getElementById(id);

// The forum tokens choose their dark or light values from <html data-fr-theme>,
// so setting it there restyles the whole frame, Excalidraw's own panels
// included. The canvas itself follows Excalidraw's `theme` prop (see <Editor>).
function adoptTheme(theme) {
  board.theme = theme === "light" ? "light" : "dark";
  document.documentElement.setAttribute("data-fr-theme", board.theme);
  if (document.body) document.body.dataset.vexillumWhiteboardTheme = board.theme;
  if (board.setTheme) board.setTheme(board.theme);
}

let statusTimer = 0;
function notify(text, { sticky = false } = {}) {
  const line = byId("wbStatus");
  if (!line) return;
  line.textContent = text;
  line.hidden = !text;
  window.clearTimeout(statusTimer);
  if (text && !sticky) statusTimer = window.setTimeout(() => (line.hidden = true), STATUS_VISIBLE_MS);
}

function setBanner(id, text) {
  const banner = byId(id);
  if (!banner) return;
  banner.textContent = text;
  banner.hidden = !text;
}

// ---------------------------------------------------------------------- shell

function buildShell() {
  const header = make("header", { id: "wbHeader" });
  const note = make("input", {
    id: "wbNote",
    placeholder: "Optional note about these edits...",
    autocomplete: "off",
  });
  const queue = make("button", { id: "wbQueue", type: "button", textContent: "Queue feedback" });
  // In the overlay the embedder floats its own Close button over the right end
  // of this header (it must still work if this frame fails to boot), and the
  // stylesheet leaves room for it. Inline boards get a Fullscreen action instead.
  header.append(make("div", { id: "wbTitle", textContent: "Whiteboard" }), note, queue);
  if (board.placement === "inline") {
    const fullscreen = make("button", {
      id: "wbFullscreen",
      type: "button",
      textContent: "Fullscreen",
      title: "Open this whiteboard full screen",
    });
    fullscreen.onclick = () => tell("maximize");
    header.append(fullscreen);
  }

  const shell = make(
    "div",
    { id: "wbShell" },
    header,
    make("div", { id: "wbFallbackBanner", className: "wb-banner", hidden: true }),
    make("div", { id: "wbStaleBanner", className: "wb-banner wb-banner-warn", hidden: true }),
    make("div", { id: "wbStatus", className: "wb-status", hidden: true }),
    make("div", { id: "wbEditor" }),
    buildLinkDialog(),
  );
  document.body.dataset.vexillumWhiteboardMode = board.placement;
  document.body.append(shell);

  queue.onclick = () => sendFeedback().catch((error) => notify(`Queue failed: ${describeError(error)}`));
  note.addEventListener("keydown", (event) => {
    if (event.key !== "Enter" || event.isComposing) return;
    event.preventDefault();
    queue.click();
  });
}

// ---------------------------------------------------------------- link dialog

let focusBeforeDialog = null;

function buildLinkDialog() {
  const cancel = make("button", { id: "wbLinkConfirmCancel", type: "button", textContent: "Cancel" });
  const open = make("button", { id: "wbLinkConfirmOpen", type: "button", textContent: "Open link" });
  const dialog = make(
    "div",
    { id: "wbLinkConfirm", className: "wb-link-confirm", hidden: true },
    make(
      "div",
      { className: "wb-link-confirm-card" },
      make("div", { className: "wb-link-confirm-title", textContent: "Open external link?" }),
      make("p", { className: "wb-link-confirm-copy", textContent: "This link came from the diagram." }),
      make("p", { id: "wbLinkConfirmUrl", className: "wb-link-confirm-url" }),
      make("div", { className: "wb-link-confirm-actions" }, cancel, open),
    ),
  );
  dialog.setAttribute("role", "dialog");
  dialog.setAttribute("aria-modal", "true");
  dialog.setAttribute("aria-label", "Open external link");

  cancel.onclick = closeLinkDialog;
  open.onclick = () => {
    const url = dialog.dataset.url || "";
    if (url) window.open(url, "_blank", "noopener,noreferrer");
    closeLinkDialog();
  };
  // Focus stays inside the two buttons while the dialog is up.
  dialog.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      event.preventDefault();
      closeLinkDialog();
    } else if (event.key === "Tab") {
      const order = [cancel, open];
      const at = order.indexOf(document.activeElement) + (event.shiftKey ? -1 : 1);
      if (at >= 0 && at < order.length) return;
      event.preventDefault();
      order[event.shiftKey ? order.length - 1 : 0].focus();
    }
  });
  return dialog;
}

function closeLinkDialog() {
  byId("wbLinkConfirm").hidden = true;
  const target = focusBeforeDialog;
  focusBeforeDialog = null;
  if (target && typeof target.focus === "function") target.focus();
}

function askToOpenLink(url) {
  const dialog = byId("wbLinkConfirm");
  focusBeforeDialog = document.activeElement;
  dialog.dataset.url = url;
  byId("wbLinkConfirmUrl").textContent = url;
  dialog.hidden = false;
  byId("wbLinkConfirmCancel").focus();
}

// Excalidraw would navigate by itself; the frame intercepts so that only
// http(s) and mailto links, and only after a confirmation, ever open.
function onSceneLink(element, event) {
  event.preventDefault();
  const url = safeLinkTarget(element && element.link);
  if (url) askToOpenLink(url);
  else notify("Blocked a link with an unsupported or unsafe scheme.");
}

// --------------------------------------------------------------------- editor

// Inline boards start locked (Excalidraw view mode) under a click-catcher, so a
// page full of boards scrolls like a page instead of every wheel turn zooming a
// canvas; the first click hands that one board over for editing. The theme is
// state here so a live switch re-renders the canvas.
function Editor({ initialScene }) {
  const [locked, setLocked] = React.useState(board.placement === "inline");
  const [theme, setTheme] = React.useState(board.theme);

  React.useEffect(() => {
    board.setLocked = setLocked;
    board.setTheme = setTheme;
    setTheme(board.theme); // a switch that arrived while React was mounting
    return () => {
      board.setLocked = null;
      board.setTheme = null;
    };
  }, []);

  const cover = locked
    ? React.createElement(
        "div",
        {
          className: "wb-activate",
          role: "button",
          tabIndex: 0,
          onClick: () => setLocked(false),
          onKeyDown: (event) => {
            if (event.key === "Enter" || event.key === " ") setLocked(false);
          },
        },
        React.createElement("span", { className: "wb-activate-label" }, "Click to edit"),
      )
    : null;

  return React.createElement(
    "div",
    { style: { position: "relative", width: "100%", height: "100%" } },
    React.createElement(Excalidraw, {
      // Transparent so the frame's own background shows; exports paint white.
      initialData: {
        elements: initialScene.elements,
        appState: { ...initialScene.appState, viewBackgroundColor: "transparent" },
        files: initialScene.files || undefined,
        scrollToContent: true,
      },
      theme,
      viewModeEnabled: locked,
      onChange: queueAutosave,
      onLinkOpen: onSceneLink,
      excalidrawAPI: (api) => {
        board.api = api;
        // The inline frame is much smaller than the scene's natural size; fit
        // the whole diagram so it does not open zoomed into one corner.
        window.setTimeout(() => {
          try {
            api.scrollToContent(api.getSceneElements(), { fitToContent: true });
          } catch {
            // Purely cosmetic.
          }
        }, 0);
      },
      UIOptions: { canvasActions: { loadScene: false, saveToActiveFile: false, toggleTheme: false } },
    }),
    cover,
  );
}

function mountEditor(initialScene) {
  createRoot(byId("wbEditor")).render(React.createElement(Editor, { initialScene }));
}

// ----------------------------------------------------------- fonts and metrics

const fontNames = Object.fromEntries(Object.entries(FONT_FAMILY).map(([name, id]) => [id, name]));
const probe = document.createElement("canvas").getContext("2d");

function fontShorthand(element) {
  const name = fontNames[element.fontFamily ?? FONT_FAMILY.Excalifont] || "sans-serif";
  const stack = name === "Excalifont" ? [name, "Xiaolai", "Segoe UI Emoji"] : [name, "Segoe UI Emoji"];
  return `${Number(element.fontSize) || 20}px ${stack.map((family) => JSON.stringify(family)).join(", ")}`;
}

function measureText(element) {
  const fallback = { width: Number(element.width) || 0, height: Number(element.height) || 0 };
  if (!probe) return fallback;
  probe.font = fontShorthand(element);
  const lines = String(element.text || "")
    .replace(/\r\n?/g, "\n")
    .replace(/\t/g, "        ")
    .split("\n");
  return {
    width: Math.max(...lines.map((line) => probe.measureText(line || " ").width)),
    height: lines.length * (Number(element.fontSize) || 20) * (Number(element.lineHeight) || 1.25),
  };
}

// Excalidraw loads the fonts a scene uses lazily. Drawing a one-pixel export
// makes it request exactly those, and document.fonts.load makes sure the
// browser really has them before anything is measured.
async function warmFonts(elements, files) {
  const texts = elements.filter((element) => element.type === "text" && !element.isDeleted);
  if (texts.length === 0) return;
  await exportToCanvas({ elements, appState: { exportBackground: false }, files: files || null, maxWidthOrHeight: 1 });
  await Promise.all(texts.map((element) => document.fonts.load(fontShorthand(element), String(element.text || ""))));
  await document.fonts.ready;
}

// ----------------------------------------------------------------- conversion

async function convertMermaid(source) {
  const parsed = await parseMermaidToExcalidraw(source, { themeVariables: { fontSize: "16px" } });
  const files = parsed.files || {};
  const skeletons = fixSkeletonLabels(parsed.elements);

  // Keeping the converter's ids lets the edit summary name real nodes. Parallel
  // edges reuse an id though, and Excalidraw needs unique ones, so in that case
  // identity is given up for a valid scene.
  const materialize = (input) => {
    const kept = convertToExcalidrawElements(input, { regenerateIds: false });
    return repeatedIds(kept).length === 0 ? kept : convertToExcalidrawElements(input, { regenerateIds: true });
  };
  const elements = await convertTwice(skeletons, {
    materialize,
    preload: (draft) => warmFonts(draft, files),
    refine: (prepared) => sizeNodeSkeletons(prepared, measureText),
  });
  return { elements: fitLabelsToNodes(elements, { measure: measureText }), files };
}

// ---------------------------------------------------------------- scene state

function settle(elements, appState, files) {
  // restore() is Excalidraw's defensive loader: it fills in missing fields and
  // repairs bindings, so a stale or hand-edited record cannot crash the editor.
  return restore(
    { elements: Array.isArray(elements) ? elements : [], appState: withoutThemeFields(appState), files: files || {} },
    null,
    null,
    { repairBindings: true },
  );
}

const FRESH_VIEW = { viewBackgroundColor: "#ffffff" };

function snapshot() {
  if (!board.api) return null;
  const view = board.api.getAppState();
  return {
    elements: board.api.getSceneElements().map(plainCopy),
    appState: withoutThemeFields({ scrollX: view.scrollX, scrollY: view.scrollY, zoom: view.zoom }),
    files: board.api.getFiles() || {},
  };
}

// A saved record is compared against the page's current source through the
// same normalisation the editor will apply to it.
function settledRecord(saved) {
  const scene = settle(saved.scene && saved.scene.elements, saved.scene && saved.scene.appState, saved.scene && saved.scene.files);
  const hasBaseline = saved.baseline && Array.isArray(saved.baseline.elements);
  const baseline = hasBaseline ? settle(saved.baseline.elements, FRESH_VIEW, saved.scene && saved.scene.files) : null;
  return {
    ...saved,
    scene: { ...saved.scene, elements: scene.elements },
    baseline: baseline ? { ...saved.baseline, elements: baseline.elements } : null,
  };
}

function showBoard({ elements, appState, files, baseline, sceneSourceHash }) {
  board.baselineElements = baseline;
  board.sceneSourceHash = sceneSourceHash;
  board.textMetricsVersion = TEXT_METRICS_VERSION;
  board.imageOnly = isImageOnly(elements);
  if (board.imageOnly) setBanner("wbFallbackBanner", IMAGE_ONLY_NOTICE);
  mountEditor({ elements, appState, files });
}

async function openFromConversion() {
  const converted = await convertMermaid(board.source);
  const fresh = settle(converted.elements, FRESH_VIEW, converted.files);
  showBoard({
    elements: fresh.elements,
    appState: FRESH_VIEW,
    files: fresh.files || converted.files,
    baseline: plainCopy(fresh.elements),
    sceneSourceHash: board.sourceHash,
  });
  // A bare conversion is autosaved too, so that reopening the same source
  // restores it; chooseStartMode does not mistake that save for reviewer edits.
  queueAutosave();
}

async function openFromRecord(saved) {
  const view = withoutThemeFields(saved.scene && saved.scene.appState);
  const restored = settle(saved.scene && saved.scene.elements, view, saved.scene && saved.scene.files);
  const files = restored.files || (saved.scene && saved.scene.files) || {};
  let elements = restored.elements;
  const hasBaseline = saved.baseline && Array.isArray(saved.baseline.elements);
  let baseline = plainCopy(hasBaseline ? saved.baseline.elements : restored.elements);

  const outdated = (Number(saved.text_metrics_version) || 0) < TEXT_METRICS_VERSION;
  if (outdated) {
    await warmFonts(elements, files);
    elements = remeasureText(elements, measureText).elements;
    baseline = remeasureText(baseline, measureText).elements;
  }
  showBoard({
    elements,
    appState: { ...FRESH_VIEW, ...view },
    files,
    baseline,
    sceneSourceHash: saved.source_hash || board.sourceHash,
  });
  if (outdated) queueAutosave();
}

// The scene on disk holds edits but came from an older version of the diagram.
// Never merge silently: the reviewer picks between starting over from the new
// diagram and carrying on with the old scene.
function askAboutStaleScene() {
  const banner = byId("wbStaleBanner");
  const restart = make("button", { type: "button", textContent: "Re-convert (discard saved edits)" });
  const keep = make("button", { type: "button", textContent: "Keep editing saved scene" });
  banner.textContent = "This diagram changed since these whiteboard edits were saved. ";
  banner.append(restart, keep);
  banner.hidden = false;
  return new Promise((resolve) => {
    restart.onclick = () => {
      banner.hidden = true;
      resolve("reconvert");
    };
    keep.onclick = () => {
      banner.textContent =
        "Editing a scene converted from an older version of this diagram. Re-open the whiteboard to convert the latest diagram.";
      resolve("keep");
    };
  });
}

async function start(init) {
  board.source = String(init.source || "");
  board.sourceHash = String(init.sourceHash || "");
  board.diagramId = String(init.diagramId || "");
  byId("wbTitle").textContent = `Whiteboard · diagram ${board.index + 1}`;

  const saved = init.saved && typeof init.saved === "object" && init.saved.scene ? init.saved : null;
  try {
    const mode = chooseStartMode(saved && settledRecord(saved), board.sourceHash);
    if (mode === "restore" || (mode === "ask" && (await askAboutStaleScene()) === "keep")) {
      await openFromRecord(saved);
    } else {
      await openFromConversion();
    }
  } catch (error) {
    notify(`Could not open this diagram as a whiteboard: ${describeError(error)}`, { sticky: true });
  }
}

function noteSourceChange(message) {
  board.source = String(message.source || "");
  board.sourceHash = String(message.sourceHash || "");
  setBanner(
    "wbStaleBanner",
    board.sourceHash === board.sceneSourceHash
      ? ""
      : "The underlying diagram changed while you were editing. Your edits are kept; close and re-open the whiteboard to convert the latest diagram.",
  );
}

// ---------------------------------------------------------------- persistence

function sendSave(flushId = "") {
  const scene = snapshot();
  if (!scene) return false;
  tell("save", { ...persistenceFields(board, scene), ...(flushId ? { flushId } : {}) });
  return true;
}

function queueAutosave() {
  if (board.closingFlush) return;
  window.clearTimeout(board.autosaveTimer);
  board.autosaveTimer = window.setTimeout(() => sendSave(), AUTOSAVE_DELAY_MS);
}

// The embedder is about to hide or replace this board: lock it, save once more
// and report back so nothing typed in the last moments is lost.
function beginClose(message) {
  const flushId = String(message.flushId || "");
  if (!flushId) return;
  board.closingFlush = flushId;
  window.clearTimeout(board.autosaveTimer);
  if (board.setLocked) board.setLocked(true);
  if (!sendSave(flushId)) {
    board.closingFlush = "";
    tell("teardownReady", { flushId });
  }
}

// Page unload or similar: save now, no UI change.
function flushNow(message) {
  const flushId = String(message.flushId || "");
  if (!flushId || board.pendingFlushes.has(flushId)) return;
  board.pendingFlushes.add(flushId);
  window.clearTimeout(board.autosaveTimer);
  if (!sendSave(flushId)) {
    board.pendingFlushes.delete(flushId);
    tell("flushComplete", { flushId, ok: true });
  }
}

function settleSave(message) {
  const flushId = String(message.flushId || "");
  if (!flushId) return;
  if (flushId === board.closingFlush) {
    board.closingFlush = "";
    if (message.ok) {
      tell("teardownReady", { flushId });
      return;
    }
    if (board.setLocked) board.setLocked(false);
    const error = String(message.error || "failed to save whiteboard scene");
    notify(`Could not save before closing: ${error}`, { sticky: true });
    tell("teardownFailed", { flushId, error });
  } else if (board.pendingFlushes.delete(flushId)) {
    tell("flushComplete", { flushId, ok: Boolean(message.ok) });
  }
}

// ------------------------------------------------------------------- feedback

function toDataUrl(blob) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(new Error("could not encode PNG preview"));
    reader.readAsDataURL(blob);
  });
}

function releaseQueueButton() {
  board.queueing = false;
  const button = byId("wbQueue");
  if (!button) return;
  button.disabled = false;
  button.textContent = "Queue feedback";
}

async function sendFeedback() {
  if (!board.api || board.queueing) return;
  board.queueing = true;
  const button = byId("wbQueue");
  button.disabled = true;
  button.textContent = "Queueing...";
  try {
    const scene = snapshot();
    const summary = summarizeEdits(board.baselineElements, scene.elements);
    const png = await exportToBlob({
      elements: board.api.getSceneElements(),
      appState: { exportBackground: true, viewBackgroundColor: "#ffffff" },
      files: board.api.getFiles() || null,
      mimeType: "image/png",
    });
    tell("queueFeedback", {
      ...persistenceFields(board, scene),
      diagramId: board.diagramId,
      imageFallback: board.imageOnly,
      note: byId("wbNote").value.trim(),
      summaryLines: summary.lines,
      stats: summary.stats,
      pngDataUrl: await toDataUrl(png),
    });
  } catch (error) {
    releaseQueueButton();
    throw error;
  }
}

function settleQueue(message) {
  releaseQueueButton();
  if (message.ok) {
    byId("wbNote").value = "";
    notify("Feedback saved.");
  } else {
    notify(`Queue failed: ${String(message.error || "unknown error")}`, { sticky: true });
  }
}

// ----------------------------------------------------------------------- boot

const handlers = {
  [MSG("theme")]: (message) => adoptTheme(message.theme),
  [MSG("sourceChanged")]: noteSourceChange,
  [MSG("prepareTeardown")]: beginClose,
  [MSG("flush")]: flushNow,
  [MSG("saveResult")]: settleSave,
  [MSG("queueResult")]: settleQueue,
};

function boot() {
  window.EXCALIDRAW_ASSET_PATH = `${location.origin}/whiteboard-assets/`;
  const params = new URL(location.href).searchParams;
  const index = Number(params.get("diagramIndex"));
  board.index = Number.isInteger(index) && index >= 0 && index <= 999 ? index : 0;
  board.diagramId = String(params.get("diagramId") || "");
  board.channel = randomChannel();
  // The embedder also passes its theme in the URL, so the first paint is right
  // before init arrives.
  adoptTheme(params.get("theme"));

  let started = false;
  window.addEventListener("message", (event) => {
    if (event.source !== window.parent) return;
    const message = event.data || {};
    if (message.channelId !== board.channel) return;
    if (message.type === MSG("init") && !started) {
      started = true;
      board.placement = message.mode === "inline" ? "inline" : "overlay";
      adoptTheme(message.theme);
      buildShell();
      start(message);
    } else if (started && handlers[message.type]) {
      handlers[message.type](message);
    }
  });
  tell("ready");
}

boot();
