// Everything that needs Excalidraw or the Mermaid converter, to build and
// size a scene and to turn it into stored or exported form.
import {
  FONT_FAMILY,
  convertToExcalidrawElements,
  exportToBlob,
  getSceneVersion,
  restoreElements,
} from "@excalidraw/excalidraw";
import { parseMermaidToExcalidraw } from "@excalidraw/mermaid-to-excalidraw";

import {
  breakTagsToNewlines,
  findRepeatedIds,
  fitNodesToLabels,
  isLive,
  uniquifyIds,
} from "./whiteboard-core.js";

export { getSceneVersion };

// The converter lays diagrams out for text of this size.
const DIAGRAM_FONT_PX = 20;

// What of the editor state is worth storing: the viewport. Everything else
// (selection, open menus, tool) is session noise.
const STORED_APP_STATE = ["scrollX", "scrollY", "zoom", "gridSize"];

export function pickAppState(appState) {
  const kept = {};
  for (const key of STORED_APP_STATE) if (appState && appState[key] !== undefined) kept[key] = appState[key];
  return kept;
}

// Only the binary files that a live image element still points at.
export function referencedFiles(elements, files) {
  const used = {};
  for (const element of elements) {
    if (isLive(element) && element.type === "image" && element.fileId && files && files[element.fileId]) {
      used[element.fileId] = files[element.fileId];
    }
  }
  return used;
}

// Conversion

// Mermaid text -> {elements, files}. Throws on text the converter rejects.
export async function convertDiagram(text) {
  const parsed = await parseMermaidToExcalidraw(text, { themeVariables: { fontSize: `${DIAGRAM_FONT_PX}px` } });
  const skeletons = uniquifyIds(breakTagsToNewlines(parsed.elements));
  // Ids are normally unique by now. Only when some id still repeats is
  // Excalidraw asked to issue fresh ones (it keeps bindings consistent).
  const regenerateIds = findRepeatedIds(skeletons).length > 0;
  const elements = convertToExcalidrawElements(skeletons, { regenerateIds });
  return { elements, files: parsed.files || {} };
}

// Stored elements may predate fields Excalidraw has since added.
export function normalizeElements(elements) {
  return restoreElements(elements, null, { repairBindings: true });
}

// Measuring

const FAMILY_NAMES = new Map(Object.entries(FONT_FAMILY).map(([name, id]) => [id, name]));
const FALLBACKS = "Xiaolai, sans-serif, Segoe UI Emoji";

function fontShorthand(fontSize, fontFamily) {
  const name = FAMILY_NAMES.get(fontFamily) || "Excalifont";
  return `${fontSize}px "${name}", ${FALLBACKS}`;
}

let canvas = null;

// Measure text exactly as the editor lays it out: only explicit line breaks,
// real font, the element's own line height.
export function measureLabel({ text, fontSize, fontFamily, lineHeight }) {
  canvas = canvas || document.createElement("canvas");
  const context = canvas.getContext("2d");
  context.font = fontShorthand(fontSize, fontFamily);
  const lines = String(text).split("\n");
  const width = Math.max(...lines.map((line) => context.measureText(line).width));
  return { width: Math.ceil(width), height: lines.length * fontSize * (lineHeight || 1.25) };
}

// Waits until every font the elements use has loaded (or failed), so that
// measuring afterwards sees the real glyphs rather than a fallback.
export async function loadFontsFor(elements) {
  if (!document.fonts || typeof document.fonts.load !== "function") return;
  const wanted = new Map();
  for (const element of elements) {
    if (isLive(element) && element.type === "text") {
      wanted.set(fontShorthand(element.fontSize, element.fontFamily), element.text || "x");
    }
  }
  await Promise.all(
    [...wanted].map(([font, sample]) => document.fonts.load(font, sample).catch(() => [])),
  );
}

// Size the nodes of a scene to their labels with the fonts as they are now.
export function fitScene(elements) {
  return fitNodesToLabels(elements, measureLabel);
}

// Export

// PNG for the agent: opaque white background whatever the theme, so the
// picture is neutral. Resolves a data URL, or "" when there is nothing to draw.
export async function renderPreview(elements, appState, files) {
  const live = elements.filter(isLive);
  if (live.length === 0) return "";
  const blob = await exportToBlob({
    elements: live,
    appState: { ...appState, exportBackground: true, viewBackgroundColor: "#ffffff", exportWithDarkMode: false, exportScale: 2 },
    files,
    mimeType: "image/png",
    exportPadding: 16,
  });
  return readAsDataUrl(blob);
}

// Reads a blob into a data URL, rejecting if the read fails.
function readAsDataUrl(blob) {
  const reader = new FileReader();
  const finished = new Promise((resolve, reject) => {
    reader.addEventListener("loadend", () => {
      if (reader.error) reject(reader.error);
      else resolve(typeof reader.result === "string" ? reader.result : "");
    });
  });
  reader.readAsDataURL(blob);
  return finished;
}
