// What did the reviewer change? Compares the scene as first converted (the
// reference) with the scene as it is now, and answers two questions from one
// list of changes so they can never disagree: "is it edited?" and "what do I
// tell the agent?". Deleted elements are invisible to everything here.
import { isLive } from "./scene-fit.js";

// Position and size tolerance, in scene pixels.
//
// Restoring a stored scene and measuring its text again moves coordinates and
// sizes by rounding noise: sub-pixel to about one pixel. A deliberate drag
// starts well beyond that (Excalidraw only treats a pointer move as a drag
// after about 10px). Two pixels sits above the noise with a comfortable
// margin and far below anything a person does on purpose. A difference must
// exceed the tolerance to count; exactly equal to it does not.
export const POSITION_TOLERANCE = 2;

// Rotation noise is orders of magnitude smaller than a deliberate turn;
// 0.02 rad is about one degree.
export const ANGLE_TOLERANCE = 0.02;

// Summary limits chosen for an agent reading a prompt: enough lines to cover a
// substantial rework, short enough to scan. The server enforces its own,
// larger ceiling (50 lines x 300 characters).
export const EDIT_SUMMARY_LINE_LIMIT = 40;
export const EDIT_SUMMARY_LINE_WIDTH = 160;

const LINEAR = new Set(["arrow", "line"]);
const SHAPES = new Set(["rectangle", "ellipse", "diamond"]);

const far = (a, b) => Math.abs((a || 0) - (b || 0)) > POSITION_TOLERANCE;
const round = (n) => Math.round(n || 0);
const oneLine = (s) => String(s ?? "").replace(/\s*\n\s*/g, " / ").replace(/\s+/g, " ").trim();
const clip = (s, max) => (s.length > max ? s.slice(0, Math.max(0, max - 3)) + "..." : s);
const textOf = (el) => oneLine(el.originalText ?? el.text ?? "");

function indexScene(elements) {
  const live = (elements || []).filter(isLive);
  const byId = new Map(live.map((el) => [el.id, el]));
  const labels = new Map(); // container id -> text element riding on it
  for (const el of live) {
    if (el.type === "text" && el.containerId && byId.has(el.containerId)) labels.set(el.containerId, el);
  }
  const isBound = (el) => el.type === "text" && !!el.containerId && byId.has(el.containerId);
  const labelText = (id) => (labels.has(id) ? textOf(labels.get(id)) : "");
  return { live, byId, labels, isBound, labelText };
}

const endId = (binding) => (binding && binding.elementId) || null;

function pathEnds(el) {
  const pts = el.points || [];
  if (pts.length < 2) return null;
  const first = pts[0];
  const last = pts[pts.length - 1];
  return { from: [el.x + first[0], el.y + first[1]], to: [el.x + last[0], el.y + last[1]] };
}

function pointsDiffer(a, b) {
  const pa = a.points || [];
  const pb = b.points || [];
  if (pa.length !== pb.length) return true;
  return pa.some((p, i) => far(p[0], pb[i][0]) || far(p[1], pb[i][1]));
}

// Rank of each change kind in the summary: structural edits first, so a
// truncated summary loses the least important lines (pure layout) first.
const RANK = {
  removed: 0, added: 1, relabeled: 2, reconnected: 3, arrowheads: 4, linked: 5, replaced: 5,
  resized: 6, rotated: 7, reshaped: 8, moved: 9,
};

// Every difference between the reference and the current scene that matters
// to a person: content, identity, geometry, connectivity. Colours, strokes,
// fonts, roughness, seeds, versions, ordering keys and timestamps are not
// compared.
function collect(reference, current) {
  const before = indexScene(reference);
  const after = indexScene(current);
  const changes = [];
  const push = (kind, detail) => changes.push({ kind, rank: RANK[kind], order: changes.length, ...detail });

  const name = (scene, el) => scene.labelText(el.id) || (el.type === "text" ? textOf(el) : "");
  const endName = (scene, id) => {
    if (!id) return null;
    const el = scene.byId.get(id);
    if (!el) return `[gone ${id}]`;
    return { label: scene.labelText(id) || (el.type === "text" ? textOf(el) : ""), type: el.type, id };
  };

  for (const old of before.live) {
    if (before.isBound(old)) continue;
    const now = after.byId.get(old.id);
    if (!now || after.isBound(now) !== before.isBound(old)) {
      if (!now) push("removed", { el: old, label: name(before, old) });
      continue;
    }
    if (now.type !== old.type) {
      push("replaced", { el: now, from: old.type, label: name(after, now) });
      continue;
    }
    const label = name(after, now);

    if (now.type === "text") {
      const a = textOf(old);
      const b = textOf(now);
      if (a !== b) push("relabeled", { el: now, from: a, to: b, label: "" });
    } else if (name(before, old) !== name(after, now)) {
      push("relabeled", { el: now, from: name(before, old), to: name(after, now), label: "" });
    }

    if (LINEAR.has(now.type)) {
      const sA = endId(old.startBinding), sB = endId(now.startBinding);
      const eA = endId(old.endBinding), eB = endId(now.endBinding);
      if (sA !== sB || eA !== eB) {
        push("reconnected", {
          el: now,
          start: sA !== sB ? { from: endName(before, sA), to: endName(after, sB) } : null,
          end: eA !== eB ? { from: endName(before, eA), to: endName(after, eB) } : null,
        });
      }
      if ((old.startArrowhead || null) !== (now.startArrowhead || null) || (old.endArrowhead || null) !== (now.endArrowhead || null)) {
        push("arrowheads", { el: now, from: old, to: now });
      }
      // An arrow attached at both ends rides along with its shapes: moving a
      // shape is already reported, the arrow's own geometry is not news.
      const freeEnd = !endId(now.startBinding) || !endId(now.endBinding);
      if (freeEnd && (far(old.x, now.x) || far(old.y, now.y) || pointsDiffer(old, now))) {
        push("reshaped", { el: now, ends: pathEnds(now), label });
      }
    } else if (now.type === "image") {
      if (old.fileId !== now.fileId) push("replaced", { el: now, from: "image", label });
      else geometry(old, now, label);
    } else {
      geometry(old, now, label);
    }

    if ((old.link || "") !== (now.link || "")) push("linked", { el: now, from: old.link || "", to: now.link || "", label });
    if (Math.abs((old.angle || 0) - (now.angle || 0)) > ANGLE_TOLERANCE) push("rotated", { el: now, label });
  }

  function geometry(old, now, label) {
    const sizeFree = now.type !== "text"; // a text box's size is derived from its text
    const resized = sizeFree && (far(old.width, now.width) || far(old.height, now.height));
    if (resized) {
      push("resized", { el: now, label, fromSize: [round(old.width), round(old.height)] });
    } else if (far(old.x, now.x) || far(old.y, now.y)) {
      push("moved", { el: now, label, delta: [round(now.x - old.x), round(now.y - old.y)] });
    }
  }

  for (const now of after.live) {
    if (after.isBound(now) || before.byId.has(now.id)) continue;
    push("added", { el: now, label: name(after, now) });
  }
  // A label whose shape survived but whose own text element was removed is
  // already covered by the relabel comparison above (its text became empty).
  changes.sort((a, b) => a.rank - b.rank || a.order - b.order);
  return { changes, before, after };
}

export function compareScenes(reference, current) {
  return collect(reference, current).changes;
}

// Whether the stored scene differs from its reference in a way that matters.
// With no reference scene at all, it counts as edited exactly when the scene
// holds something.
export function hasEdits(referenceElements, currentElements) {
  if (!Array.isArray(referenceElements)) return (currentElements || []).some(isLive);
  return compareScenes(referenceElements, currentElements).length > 0;
}

const KIND = { rectangle: "rectangle", ellipse: "ellipse", diamond: "diamond", arrow: "arrow", line: "line", text: "text", freedraw: "drawing", image: "image", frame: "frame" };
const kindOf = (el) => KIND[el.type] || el.type;
const quoted = (label) => (label ? ` "${clip(label, 40)}"` : "");
const tag = (el) => ` [id ${el.id}]`;
const pt = (p) => `(${round(p[0])},${round(p[1])})`;
const signed = (n) => (n >= 0 ? `+${n}` : `${n}`);

function endText(end) {
  if (!end) return "a free end";
  if (typeof end === "string") return end;
  return end.label ? `"${clip(end.label, 30)}"` : `${KIND[end.type] || end.type} [id ${end.id}]`;
}

function describeChange(c, after) {
  const el = c.el;
  switch (c.kind) {
    case "removed":
      return `removed ${kindOf(el)}${quoted(c.label)}${tag(el)}`;
    case "added": {
      if (LINEAR.has(el.type)) {
        const ends = [el.startBinding, el.endBinding].map((b) => {
          const target = b && after.byId.get(b.elementId);
          return target ? endText({ label: after.labelText(target.id) || (target.type === "text" ? textOf(target) : ""), type: target.type, id: target.id }) : "a free end";
        });
        const note = c.label ? ` labeled "${clip(c.label, 40)}"` : "";
        return `added ${kindOf(el)} from ${ends[0]} to ${ends[1]}${note}${tag(el)}`;
      }
      const where = el.type === "freedraw" ? ` around ${pt([el.x, el.y])}` : ` at ${pt([el.x, el.y])}`;
      const size = SHAPES.has(el.type) ? ` size ${round(el.width)}x${round(el.height)}` : "";
      return `added ${kindOf(el)}${quoted(c.label || (el.type === "text" ? textOf(el) : ""))}${tag(el)}${where}${size}`;
    }
    case "relabeled":
      return `relabeled ${kindOf(el)}${tag(el)}: "${clip(c.from, 50)}" -> "${clip(c.to, 50)}"`;
    case "reconnected": {
      const parts = [];
      if (c.start) parts.push(`start ${endText(c.start.from)} -> ${endText(c.start.to)}`);
      if (c.end) parts.push(`end ${endText(c.end.from)} -> ${endText(c.end.to)}`);
      return `reconnected ${kindOf(el)}${tag(el)}: ${parts.join("; ")}`;
    }
    case "arrowheads":
      return `changed arrowheads of ${kindOf(el)}${tag(el)}: start=${c.to.startArrowhead || "none"} end=${c.to.endArrowhead || "none"} (was start=${c.from.startArrowhead || "none"} end=${c.from.endArrowhead || "none"})`;
    case "linked":
      return `changed link of ${kindOf(el)}${quoted(c.label)}${tag(el)}: "${clip(c.from, 60)}" -> "${clip(c.to, 60)}"`;
    case "replaced":
      return `replaced ${c.from} with ${kindOf(el)}${quoted(c.label)}${tag(el)}`;
    case "resized":
      return `resized ${kindOf(el)}${quoted(c.label)}${tag(el)}: ${c.fromSize[0]}x${c.fromSize[1]} -> ${round(el.width)}x${round(el.height)}, now at ${pt([el.x, el.y])}`;
    case "rotated":
      return `rotated ${kindOf(el)}${quoted(c.label)}${tag(el)} to ${Math.round((el.angle * 180) / Math.PI)} deg`;
    case "reshaped":
      return `reshaped ${kindOf(el)}${tag(el)}: now ${c.ends ? `${pt(c.ends.from)} -> ${pt(c.ends.to)}` : "different"}`;
    case "moved":
      return `moved ${kindOf(el)}${quoted(c.label)}${tag(el)} by (${signed(c.delta[0])},${signed(c.delta[1])})`;
    default:
      return `changed ${kindOf(el)}${tag(el)}`;
  }
}

// The lines an agent gets: one short line per change, most structural first.
// At most maxLines lines; when changes were left out, the last line says how
// many.
export function summarizeEdits(referenceElements, currentElements, limits = {}) {
  const maxLines = limits.maxLines ?? EDIT_SUMMARY_LINE_LIMIT;
  const maxChars = limits.maxLineChars ?? EDIT_SUMMARY_LINE_WIDTH;
  if (!Array.isArray(referenceElements)) {
    const n = (currentElements || []).filter(isLive).length;
    return [`no converted reference to compare with; the scene holds ${n} element${n === 1 ? "" : "s"}, see the scene file`];
  }
  const { changes, after } = collect(referenceElements, currentElements);
  if (changes.length === 0) {
    return ["no content changes: the diagram matches what was converted from the Mermaid text"];
  }
  const lines = changes.map((c) => clip(oneLine(describeChange(c, after)), maxChars));
  if (lines.length <= maxLines) return lines;
  const omitted = lines.length - (maxLines - 1);
  return [...lines.slice(0, maxLines - 1), clip(`... and ${omitted} more change${omitted === 1 ? "" : "s"} not listed (see the scene file)`, maxChars)];
}
