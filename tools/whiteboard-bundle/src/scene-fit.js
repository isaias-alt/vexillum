// Pure helpers that shape a freshly converted scene before a reviewer sees it:
// line breaks, unique ids, node sizing and keeping attached arrows attached.
// No DOM and no Excalidraw import, so every rule here runs under node:test.
//
// Text measurement is injected: callers pass measure({text, fontSize,
// fontFamily, lineHeight}) -> {width, height} for the text laid out with only
// its explicit line breaks (no wrapping). In the browser that is a canvas
// measurement with the real font; in tests it is a fake.

// Horizontal and vertical breathing room between a label and the edge of the
// usable area inside its node, in scene pixels.
export const LABEL_PAD_X = 16;
export const LABEL_PAD_Y = 12;

const SHAPE_TYPES = new Set(["rectangle", "ellipse", "diamond"]);
const LINEAR_TYPES = new Set(["arrow", "line"]);

// Differences below this are float noise, not changes (keeps refits idempotent
// so a measure pass never keeps re-dirtying the scene).
const EPSILON = 0.01;

const BREAK_TAG = /<\s*br\s*\/?\s*>/gi;

export const isLive = (element) => !!element && element.isDeleted !== true;

function turnBreakTags(text) {
  return typeof text === "string" ? text.replace(BREAK_TAG, "\n") : text;
}

// Mermaid writes line breaks in labels as <br> tags. Returns copies of the
// converter's element skeletons with those tags turned into real newlines in
// the shape text and the labels of shapes and arrows.
export function breakTagsToNewlines(skeletons) {
  return (skeletons || []).map((item) => {
    if (!item || typeof item !== "object") return item;
    const copy = { ...item };
    if ("text" in copy) copy.text = turnBreakTags(copy.text);
    if ("originalText" in copy) copy.originalText = turnBreakTags(copy.originalText);
    if (copy.label && typeof copy.label === "object") {
      copy.label = { ...copy.label, text: turnBreakTags(copy.label.text) };
    }
    return copy;
  });
}

// Ids that occur on more than one live element, sorted.
export function findRepeatedIds(elements) {
  const seen = new Set();
  const repeated = new Set();
  for (const element of elements || []) {
    if (!isLive(element) || typeof element.id !== "string") continue;
    if (seen.has(element.id)) repeated.add(element.id);
    seen.add(element.id);
  }
  return [...repeated].sort();
}

// Gives every live element after the first holder of an id a fresh unique id
// (`<id>~2`, `<id>~3`, ...). The converter reuses an id for parallel edges;
// run this on its skeletons, before they become scene elements, so nothing
// else holds a reference to the repeated id yet.
export function uniquifyIds(elements) {
  const taken = new Set((elements || []).filter((e) => e && typeof e.id === "string").map((e) => e.id));
  const seen = new Set();
  return (elements || []).map((element) => {
    if (!isLive(element) || typeof element.id !== "string") return element;
    if (!seen.has(element.id)) {
      seen.add(element.id);
      return element;
    }
    let n = 2;
    while (taken.has(`${element.id}~${n}`)) n += 1;
    const id = `${element.id}~${n}`;
    taken.add(id);
    seen.add(id);
    return { ...element, id };
  });
}

// The largest axis-aligned rectangle that fits inside a shape, as a fraction
// of the shape's own width and height: all of a rectangle, 1/sqrt(2) of an
// ellipse's axes (corners on the curve) and half of a diamond's axes (corners
// on the edge midpoints).
const INSCRIBED_FRACTION = {
  rectangle: 1,
  ellipse: Math.SQRT1_2,
  diamond: 0.5,
};

// Outer size a shape needs so that its inscribed rectangle is (width x height).
export function outerSizeFor(type, width, height) {
  const fraction = INSCRIBED_FRACTION[type] ?? 1;
  return { width: width / fraction, height: height / fraction };
}

export function inscribedSizeOf(type, width, height) {
  const fraction = INSCRIBED_FRACTION[type] ?? 1;
  return { width: width * fraction, height: height * fraction };
}

const same = (a, b) => Math.abs(a - b) < EPSILON;

// Excalidraw redraws an element only when its version moves, so every real
// change goes through here.
function touched(element, changes) {
  const version = (element.version || 0) + 1;
  return {
    ...element,
    ...changes,
    version,
    versionNonce: (Math.imul(version, 2654435761) ^ 0x9e3779b9) >>> 0,
    updated: Date.now(),
  };
}

function absolutePoints(element) {
  return (element.points || []).map(([px, py]) => [element.x + px, element.y + py]);
}

// Rebuilds a linear element from absolute points, with its first point at the
// element origin as Excalidraw expects.
function linearFromAbsolute(element, absolute) {
  const [ox, oy] = absolute[0];
  const points = absolute.map(([px, py]) => [px - ox, py - oy]);
  const xs = points.map((p) => p[0]);
  const ys = points.map((p) => p[1]);
  return {
    x: ox,
    y: oy,
    points,
    width: Math.max(...xs) - Math.min(...xs),
    height: Math.max(...ys) - Math.min(...ys),
  };
}

// Makes every shape big enough for its label and centres the label in it.
//
//  - Sizes grow only, around the shape's own centre, so nothing drifts.
//  - For an ellipse or a diamond the label must fit the inscribed rectangle,
//    not the bounding box (see INSCRIBED_FRACTION).
//  - The label is centred on the shape's centre whatever its textAlign and
//    verticalAlign say: those only matter inside a text box wider than its
//    text, and the box here is exactly the measured text.
//  - Arrows that were attached to a grown shape have the attached end scaled
//    with the shape, so it still touches the outline; unattached arrows and
//    free ends are untouched.
//
// Returns a new array; elements that did not change keep their identity.
export function fitNodesToLabels(elements, measure) {
  const list = elements || [];
  const byId = new Map();
  for (const element of list) if (isLive(element)) byId.set(element.id, element);

  const labelOf = new Map(); // container id -> text element
  for (const element of list) {
    if (isLive(element) && element.type === "text" && element.containerId && byId.has(element.containerId)) {
      labelOf.set(element.containerId, element);
    }
  }

  const replacements = new Map();
  const grown = new Map(); // shape id -> {cx, cy, sx, sy}

  for (const [containerId, label] of labelOf) {
    const container = byId.get(containerId);
    if (!SHAPE_TYPES.has(container.type)) continue;
    const raw = label.originalText ?? label.text ?? "";
    const natural = measure({
      text: raw,
      fontSize: label.fontSize,
      fontFamily: label.fontFamily,
      lineHeight: label.lineHeight,
    });
    const need = outerSizeFor(container.type, natural.width + 2 * LABEL_PAD_X, natural.height + 2 * LABEL_PAD_Y);
    const width = Math.max(container.width, Math.ceil(need.width));
    const height = Math.max(container.height, Math.ceil(need.height));
    const cx = container.x + container.width / 2;
    const cy = container.y + container.height / 2;

    if (!same(width, container.width) || !same(height, container.height)) {
      replacements.set(
        containerId,
        touched(container, { x: cx - width / 2, y: cy - height / 2, width, height }),
      );
      grown.set(containerId, { cx, cy, sx: width / container.width, sy: height / container.height });
    }
    const textBox = { x: cx - natural.width / 2, y: cy - natural.height / 2, width: natural.width, height: natural.height };
    const unchanged =
      same(label.x, textBox.x) && same(label.y, textBox.y) && same(label.width, textBox.width) && same(label.height, textBox.height) && label.text === raw;
    if (!unchanged) replacements.set(label.id, touched(label, { ...textBox, text: raw }));
  }

  if (grown.size > 0) {
    for (const element of list) {
      if (!isLive(element) || !LINEAR_TYPES.has(element.type) || !element.points || element.points.length < 2) continue;
      const start = element.startBinding && grown.get(element.startBinding.elementId);
      const end = element.endBinding && grown.get(element.endBinding.elementId);
      if (!start && !end) continue;
      const absolute = absolutePoints(element);
      const scale = ([px, py], g) => [g.cx + (px - g.cx) * g.sx, g.cy + (py - g.cy) * g.sy];
      if (start) absolute[0] = scale(absolute[0], start);
      if (end) absolute[absolute.length - 1] = scale(absolute[absolute.length - 1], end);
      const next = touched(element, linearFromAbsolute(element, absolute));
      replacements.set(element.id, next);

      // A label riding on a two-point arrow follows the arrow's midpoint.
      const arrowLabel = [...labelOf.values()].find((l) => l.containerId === element.id);
      if (arrowLabel && absolute.length === 2) {
        const mx = (absolute[0][0] + absolute[1][0]) / 2;
        const my = (absolute[0][1] + absolute[1][1]) / 2;
        const current = replacements.get(arrowLabel.id) || arrowLabel;
        replacements.set(
          arrowLabel.id,
          touched(current, { x: mx - current.width / 2, y: my - current.height / 2 }),
        );
      }
    }
  }

  if (replacements.size === 0) return list.slice();
  return list.map((element) => (element && replacements.has(element.id) ? replacements.get(element.id) : element));
}

// Whether a converted scene holds nothing but images (no editable shapes):
// the converter's fallback for diagram types it cannot turn into shapes.
export function isImageBoard(elements) {
  const live = (elements || []).filter(isLive);
  return live.length > 0 && live.every((element) => element.type === "image");
}
