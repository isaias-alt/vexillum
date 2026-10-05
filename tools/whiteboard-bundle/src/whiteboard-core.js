// Pure helpers behind the whiteboard frame: data in, data out, no DOM and no
// Excalidraw import, so the node test-suite under ../test can run them as they
// are. whiteboard-frame.js imports them through esbuild.
//
// Vocabulary: a "scene" is Excalidraw's { elements, appState, files }; a
// "skeleton" is the loose element description the Mermaid converter emits
// before Excalidraw turns it into real elements; a "saved record" is what the
// Go store keeps per diagram: { source_hash, text_metrics_version, scene,
// baseline: { elements } }. The baseline is the scene as it was converted, so
// later edits can be described as a difference against it.

export const TEXT_METRICS_VERSION = 1;
export const SUMMARY_LINE_LIMIT = 40;
export const SUMMARY_LINE_WIDTH = 200;

// Distances (scene units) below this are treated as noise, not as an edit.
const GEOMETRY_TOLERANCE = 2;
const ANGLE_TOLERANCE = 0.01;

const TEXT_PADDING = 16;
const NODE_SHAPES = new Set(["rectangle", "ellipse", "diamond"]);

const isRecord = (value) => value !== null && typeof value === "object" && !Array.isArray(value);
const asNumber = (value) => (Number.isFinite(Number(value)) ? Number(value) : 0);
const listOf = (value) => (Array.isArray(value) ? value : []);

function clip(text, limit) {
  const value = String(text);
  return value.length > limit ? `${value.slice(0, limit - 3)}...` : value;
}

// ---------------------------------------------------------------- app state

// Theme and canvas colour always come from the live frame, never from a saved
// scene: persisting them would make an old scene fight the current theme.
export function withoutThemeFields(appState) {
  if (!isRecord(appState)) return {};
  const { theme: _theme, viewBackgroundColor: _background, ...rest } = appState;
  return rest;
}

// ------------------------------------------------------------ label cleanup

// Mermaid writes a line break inside a label as <br>, <br/> or a literal
// backslash-n. Excalidraw wants a real newline in the text, otherwise
// "a<br>b" is drawn as the single word "a<br>b".
export function labelWithRealBreaks(text) {
  if (typeof text !== "string") return text;
  return text.replace(/<br\s*\/?\s*>/gi, "\n").replace(/\\n/g, "\n");
}

// Rewrites every text-bearing field of the converter's skeletons. Objects are
// copied only when something actually changes.
export function fixSkeletonLabels(skeletons) {
  return listOf(skeletons).map((skeleton) => {
    if (!isRecord(skeleton)) return skeleton;
    let out = skeleton;
    const set = (patch) => {
      out = { ...out, ...patch };
    };
    for (const field of ["text", "originalText"]) {
      const fixed = labelWithRealBreaks(skeleton[field]);
      if (fixed !== skeleton[field]) set({ [field]: fixed });
    }
    if (isRecord(skeleton.label)) {
      const fixed = labelWithRealBreaks(skeleton.label.text);
      if (fixed !== skeleton.label.text) set({ label: { ...skeleton.label, text: fixed } });
    }
    return out;
  });
}

function growAroundCenter(element, minWidth, minHeight) {
  const width = asNumber(element.width);
  const height = asNumber(element.height);
  if (width >= minWidth && height >= minHeight) return element;
  const nextWidth = Math.max(width, minWidth);
  const nextHeight = Math.max(height, minHeight);
  return {
    ...element,
    width: nextWidth,
    height: nextHeight,
    x: asNumber(element.x) - (nextWidth - width) / 2,
    y: asNumber(element.y) - (nextHeight - height) / 2,
  };
}

function centerInside(container, text) {
  const spare = (outer, inner) => asNumber(outer) - asNumber(inner);
  const horizontal = text.textAlign || "center";
  const vertical = text.verticalAlign || "middle";
  const x =
    horizontal === "left"
      ? asNumber(container.x)
      : asNumber(container.x) + spare(container.width, text.width) / (horizontal === "right" ? 1 : 2);
  const y =
    vertical === "top"
      ? asNumber(container.y)
      : asNumber(container.y) + spare(container.height, text.height) / (vertical === "bottom" ? 1 : 2);
  return x === asNumber(text.x) && y === asNumber(text.y) ? text : { ...text, x, y };
}

// After conversion (and once the fonts are in), makes node labels fit: a text
// box is never smaller than its measured size, the node box around it never
// smaller than the text plus padding, and the label sits centred in its node.
// Only node shapes are touched; the labels of arrows float on their own and
// resizing those containers would drag the arrow.
export function fitLabelsToNodes(elements, { measure } = {}) {
  const list = listOf(elements);
  const current = new Map();
  for (const element of list) if (element && element.id) current.set(element.id, element);

  const texts = list.filter((element) => element && element.id && element.type === "text" && !element.isDeleted);
  const bound = texts.filter((text) => text.containerId);

  if (measure) {
    for (const text of texts) {
      const size = measure(text);
      current.set(text.id, growAroundCenter(text, asNumber(size && size.width), asNumber(size && size.height)));
    }
  }

  for (const text of bound) {
    const node = current.get(text.containerId);
    if (!node || !NODE_SHAPES.has(node.type)) continue;
    const label = current.get(text.id);
    current.set(
      node.id,
      growAroundCenter(node, asNumber(label.width) + TEXT_PADDING, asNumber(label.height) + TEXT_PADDING),
    );
  }

  for (const text of bound) {
    const node = current.get(text.containerId);
    if (node && NODE_SHAPES.has(node.type)) current.set(text.id, centerInside(node, current.get(text.id)));
  }

  return list.map((element) => (element && element.id && current.has(element.id) ? current.get(element.id) : element));
}

// How much bigger than its label a node must be, per shape, for the label to
// sit inside without wrapping: a diamond's usable area is half its width and
// height, an ellipse's roughly 70% of it.
const LABEL_ROOM = { rectangle: 1, ellipse: 1.45, diamond: 2 };

// Sizes node skeletons to their measured labels BEFORE Excalidraw builds
// elements from them, then drags the ends of every arrow that touches a resized
// node along with it. Both halves matter: the converter keeps the arrow paths it
// was handed, so a box that grew without them would leave arrow tips floating
// inside or beside it.
export function sizeNodeSkeletons(skeletons, measure) {
  const list = listOf(skeletons);
  const resized = new Map(); // node id -> { before, after }

  const sized = list.map((skeleton) => {
    const room = isRecord(skeleton) ? LABEL_ROOM[skeleton.type] : undefined;
    if (!room || !isRecord(skeleton.label) || typeof skeleton.label.text !== "string") return skeleton;
    if (!Number.isFinite(skeleton.width) || !Number.isFinite(skeleton.height)) return skeleton;
    const size = measure({ ...skeleton.label });
    const grown = growAroundCenter(
      skeleton,
      (asNumber(size && size.width) + TEXT_PADDING) * room,
      (asNumber(size && size.height) + TEXT_PADDING) * room,
    );
    if (grown !== skeleton && skeleton.id !== undefined) resized.set(skeleton.id, { before: skeleton, after: grown });
    return grown;
  });
  if (resized.size === 0) return sized;
  return sized.map((skeleton) => followResizedNodes(skeleton, resized));
}

// Scaling a shape about its centre maps its old outline onto the new one, so an
// arrow tip keeps its place on the outline when its coordinates inside the
// shape are scaled the same way.
function movedWithNode(point, { before, after }) {
  const cx = asNumber(before.x) + asNumber(before.width) / 2;
  const cy = asNumber(before.y) + asNumber(before.height) / 2;
  const halfW = asNumber(before.width) / 2 || 1;
  const halfH = asNumber(before.height) / 2 || 1;
  return [
    cx + ((point[0] - cx) / halfW) * (asNumber(after.width) / 2),
    cy + ((point[1] - cy) / halfH) * (asNumber(after.height) / 2),
  ];
}

function followResizedNodes(skeleton, resized) {
  if (!isRecord(skeleton) || skeleton.type !== "arrow" || !Array.isArray(skeleton.points) || skeleton.points.length < 2) {
    return skeleton;
  }
  const startNode = skeleton.start && resized.get(skeleton.start.id);
  const endNode = skeleton.end && resized.get(skeleton.end.id);
  if (!startNode && !endNode) return skeleton;

  const origin = [asNumber(skeleton.x), asNumber(skeleton.y)];
  const absolute = skeleton.points.map((point) => [origin[0] + asNumber(point[0]), origin[1] + asNumber(point[1])]);
  if (startNode) absolute[0] = movedWithNode(absolute[0], startNode);
  if (endNode) absolute[absolute.length - 1] = movedWithNode(absolute[absolute.length - 1], endNode);
  // Excalidraw wants the first point at (0, 0) relative to the arrow's x/y.
  const [x, y] = absolute[0];
  return { ...skeleton, x, y, points: absolute.map((point) => [point[0] - x, point[1] - y]) };
}

// Text boxes of scenes saved before the current metrics version may be too
// small for the glyphs now drawn; grow them to the measured size. Returns the
// new list and how many boxes changed.
export function remeasureText(elements, measure) {
  let changed = 0;
  const next = listOf(elements).map((element) => {
    if (!element || element.type !== "text" || element.isDeleted || element.autoResize === false) return element;
    const measured = measure(element);
    const width = Math.max(asNumber(element.width), asNumber(measured && measured.width));
    const height = Math.max(asNumber(element.height), asNumber(measured && measured.height));
    if (width === asNumber(element.width) && height === asNumber(element.height)) return element;
    changed += 1;
    return { ...element, width, height };
  });
  return { elements: next, changed };
}

// --------------------------------------------------------- conversion pieces

// Only plain web and mail links may leave the whiteboard. Anything else
// (javascript:, data:, file:, relative paths, ...) can arrive through the
// untrusted Mermaid `click` directive and is refused with an empty string.
export function safeLinkTarget(url) {
  const value = String(url || "").trim();
  if (/^https?:\/\//i.test(value)) return value;
  if (/^mailto:\S+$/i.test(value)) return value;
  return "";
}

// True when the converter gave up on a diagram type and returned only
// pictures: the board is then something to draw on, not nodes to edit.
export function isImageOnly(elements) {
  const live = listOf(elements).filter((element) => element && !element.isDeleted);
  return live.length > 0 && live.every((element) => element.type === "image");
}

// Ids that appear more than once. Parallel edges make the converter reuse an
// id, which Excalidraw cannot represent.
export function repeatedIds(elements) {
  const seen = new Set();
  const repeated = new Set();
  for (const element of listOf(elements)) {
    const id = element && element.id ? String(element.id) : "";
    if (!id) continue;
    if (seen.has(id)) repeated.add(id);
    seen.add(id);
  }
  return [...repeated];
}

// Excalidraw measures text while it builds elements, but its fonts arrive
// asynchronously. The first pass tells us which glyphs are needed, `preload`
// fetches those fonts, and the second pass builds the elements again with the
// real metrics. The result of the first pass is never shown.
//
// `refine`, when given, rewrites the skeletons between the passes (see
// sizeNodeSkeletons): at that point the fonts are in, so text can be measured.
export async function convertTwice(skeletons, { materialize, preload, refine }) {
  await preload(materialize(skeletons));
  return materialize(refine ? refine(skeletons) : skeletons);
}

// -------------------------------------------------------------- persistence

// The body of a "save" message (and the scene half of "queueFeedback").
export function persistenceFields(session, scene) {
  return {
    sourceHash: String(session.sceneSourceHash || ""),
    textMetricsVersion: Math.max(0, Math.floor(asNumber(session.textMetricsVersion))),
    scene: scene === undefined ? null : scene,
    baseline: { elements: listOf(session.baselineElements) },
  };
}

const liveById = (elements) => {
  const map = new Map();
  for (const element of listOf(elements)) {
    if (isRecord(element) && element.id && !element.isDeleted) map.set(element.id, element);
  }
  return map;
};

const samePoints = (a, b) =>
  a.length === b.length &&
  a.every((point, i) => Math.abs(asNumber(point[0]) - asNumber(b[i][0])) <= GEOMETRY_TOLERANCE &&
    Math.abs(asNumber(point[1]) - asNumber(b[i][1])) <= GEOMETRY_TOLERANCE);

// Style (colours, strokes, fonts), version counters and timestamps are not
// edits: the converter and `restore()` rewrite them freely. What counts is
// identity, wording, placement, shape and wiring.
function contentChanged(was, now) {
  if (was.type !== now.type) return true;
  if (String(was.text || "") !== String(now.text || "")) return true;
  if ((was.containerId || null) !== (now.containerId || null)) return true;
  if ((was.link || null) !== (now.link || null)) return true;
  if ((was.fileId || null) !== (now.fileId || null)) return true;
  for (const key of ["x", "y", "width", "height"]) {
    if (Math.abs(asNumber(now[key]) - asNumber(was[key])) > GEOMETRY_TOLERANCE) return true;
  }
  if (Math.abs(asNumber(now.angle) - asNumber(was.angle)) > ANGLE_TOLERANCE) return true;
  for (const side of ["startBinding", "endBinding"]) {
    if ((was[side]?.elementId || null) !== (now[side]?.elementId || null)) return true;
  }
  if (Array.isArray(was.points) || Array.isArray(now.points)) {
    if (!samePoints(listOf(was.points), listOf(now.points))) return true;
  }
  return false;
}

// Did the reviewer change anything in this saved scene, compared with the
// scene it was converted into?
export function savedSceneWasEdited(saved) {
  const sceneElements = saved && saved.scene && saved.scene.elements;
  const baselineElements = saved && saved.baseline && saved.baseline.elements;
  if (!Array.isArray(baselineElements)) return Array.isArray(sceneElements);
  if (!Array.isArray(sceneElements)) return true;
  const was = liveById(baselineElements);
  const now = liveById(sceneElements);
  if (was.size !== now.size) return true;
  for (const [id, element] of now) {
    const original = was.get(id);
    if (!original || contentChanged(original, element)) return true;
  }
  return false;
}

// What to do when a board opens: "convert" the Mermaid source afresh,
// "restore" the saved scene, or "ask" the reviewer. Every conversion is
// autosaved, so a saved record alone proves nothing; it only matters when the
// source changed underneath AND the reviewer had really edited the scene.
export function chooseStartMode(saved, sourceHash) {
  if (!isRecord(saved) || !saved.scene) return "convert";
  if (String(saved.source_hash || "") === String(sourceHash || "")) return "restore";
  return savedSceneWasEdited(saved) ? "ask" : "convert";
}

// ------------------------------------------------------------- edit summary

function labelsByContainer(live) {
  const map = new Map();
  for (const element of live.values()) {
    if (element.type === "text" && element.containerId) map.set(element.containerId, element);
  }
  return map;
}

function visibleText(element, labels) {
  return String(element.text || (labels.get(element.id) || {}).text || "")
    .replace(/\s+/g, " ")
    .trim();
}

function nameOf(element, labels) {
  const text = visibleText(element, labels);
  const kind = String(element.type || "element");
  return text ? `${kind} "${clip(text, 60)}" (${element.id})` : `${kind} (${element.id})`;
}

function connectionOf(arrow, live, labels) {
  const from = arrow.startBinding?.elementId && live.get(arrow.startBinding.elementId);
  const to = arrow.endBinding?.elementId && live.get(arrow.endBinding.elementId);
  if (!from && !to) return "";
  const end = (element) => (element ? nameOf(element, labels) : "(unattached)");
  return ` from ${end(from)} to ${end(to)}`;
}

const upperFirst = (text) => text.charAt(0).toUpperCase() + text.slice(1);
const isBoundLabel = (element) => element.type === "text" && Boolean(element.containerId);

// Describes how `edited` differs from `baseline`: a bounded list of
// human-readable lines plus a tally. Text bound to a shape is reported through
// the shape, so renaming a node is one "relabeled" line instead of a moved
// text element.
export function summarizeEdits(baseline, edited, { maxLines = SUMMARY_LINE_LIMIT } = {}) {
  const before = liveById(baseline);
  const after = liveById(edited);
  const beforeLabels = labelsByContainer(before);
  const afterLabels = labelsByContainer(after);
  const tally = { added: 0, removed: 0, moved: 0, relabeled: 0, drawn: 0 };
  const lines = [];
  const record = (kind, line) => {
    tally[kind] += 1;
    lines.push(clip(line, SUMMARY_LINE_WIDTH));
  };

  for (const [id, element] of after) {
    if (before.has(id)) continue;
    // The label of a brand-new shape is described by the shape.
    if (isBoundLabel(element) && after.has(element.containerId) && !before.has(element.containerId)) continue;
    if (element.type === "freedraw") {
      record("drawn", `Drew a freehand mark near (${Math.round(element.x)}, ${Math.round(element.y)})`);
      continue;
    }
    const linked = element.type === "arrow" || element.type === "line" ? connectionOf(element, after, afterLabels) : "";
    record("added", `Added ${nameOf(element, afterLabels)}${linked}`);
  }

  for (const [id, element] of before) {
    if (after.has(id)) continue;
    if (isBoundLabel(element) && before.has(element.containerId)) continue;
    record("removed", `Removed ${nameOf(element, beforeLabels)}`);
  }

  for (const [id, element] of after) {
    const original = before.get(id);
    if (!original) continue;
    const bound = isBoundLabel(element);

    const wasText = visibleText(original, beforeLabels);
    const nowText = visibleText(element, afterLabels);
    if (wasText !== nowText && !bound) {
      record("relabeled", `Relabeled ${element.type} (${id}): "${clip(wasText, 50)}" -> "${clip(nowText, 50)}"`);
    }
    if (bound) continue;

    const dx = Math.round(asNumber(element.x) - asNumber(original.x));
    const dy = Math.round(asNumber(element.y) - asNumber(original.y));
    const dw = Math.round(asNumber(element.width) - asNumber(original.width));
    const dh = Math.round(asNumber(element.height) - asNumber(original.height));
    const shifted = Math.max(Math.abs(dx), Math.abs(dy)) > GEOMETRY_TOLERANCE;
    const resized = Math.max(Math.abs(dw), Math.abs(dh)) > GEOMETRY_TOLERANCE;
    if (!shifted && !resized) continue;
    const parts = [];
    if (shifted) parts.push(`moved by (${dx}, ${dy})`);
    if (resized) parts.push(`resized by (${dw}, ${dh})`);
    record("moved", `${upperFirst(parts.join(" and "))}: ${nameOf(element, afterLabels)}`);
  }

  const total = Object.values(tally).reduce((sum, count) => sum + count, 0);
  const kept = lines.slice(0, maxLines);
  const dropped = lines.length - kept.length;
  if (dropped > 0) kept.push(`...and ${dropped} more change${dropped === 1 ? "" : "s"}`);
  if (total === 0) kept.push("No element changes detected (view-only or style-only edits).");
  return { lines: kept, stats: tally, totalChanges: total };
}

// Deep copy through JSON: the form in which scenes are persisted and posted.
export const plainCopy = (value) => JSON.parse(JSON.stringify(value));
