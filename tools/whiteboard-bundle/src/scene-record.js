// The stored record, the opening decision and the link filter: the parts of
// the whiteboard's contract with the server that need no DOM.
import { hasEdits } from "./scene-diff.js";
import { isLive } from "./scene-fit.js";

// Format of the stored record. Anything else is treated as absent: the record
// is a local autosave cache, so there is no migration.
export const RECORD_FORMAT = 2;

// Which text-measurement scheme computed the node sizes in a record. Raise it
// whenever sizing changes so older records are measured again on open.
export const MEASURE_GEN = 1;

const THEME_FIELDS = ["theme", "viewBackgroundColor"];

// JSON round trip: plain data in, plain data out, nothing shared.
export function cloneScene(scene) {
  return scene === undefined ? undefined : JSON.parse(JSON.stringify(scene));
}

// A copy of the scene without the fields that depend on the active theme.
// The server strips the same two fields on its side.
export function scrubThemeFields(scene) {
  const copy = cloneScene(scene);
  if (copy && copy.appState && typeof copy.appState === "object") {
    for (const field of THEME_FIELDS) delete copy.appState[field];
  }
  return copy;
}

const liveOnly = (elements) => (elements || []).filter(isLive);

// The record to store from the current scene, the as-converted reference
// elements, the digest of the diagram text and the measurement scheme.
export function buildRecord({ scene, referenceElements, digest, measureGen = MEASURE_GEN }) {
  const current = scrubThemeFields({
    elements: liveOnly(scene && scene.elements),
    appState: (scene && scene.appState) || {},
    files: (scene && scene.files) || {},
  });
  return {
    format: RECORD_FORMAT,
    digest: String(digest ?? ""),
    measure_gen: measureGen,
    current,
    pristine: Array.isArray(referenceElements) ? { elements: liveOnly(cloneScene(referenceElements)) } : null,
  };
}

// Validates a record that came from the server. Returns it, or null when it
// is missing, malformed or of another format.
export function readRecord(raw) {
  if (!raw || typeof raw !== "object" || raw.format !== RECORD_FORMAT) return null;
  if (typeof raw.digest !== "string") return null;
  if (!raw.current || typeof raw.current !== "object" || !Array.isArray(raw.current.elements)) return null;
  return {
    format: RECORD_FORMAT,
    digest: raw.digest,
    measure_gen: Number.isInteger(raw.measure_gen) ? raw.measure_gen : 0,
    saved_at: raw.saved_at,
    current: raw.current,
    pristine: raw.pristine && Array.isArray(raw.pristine.elements) ? { elements: raw.pristine.elements } : null,
  };
}

export const recordHasScene = (record) => !!record && liveOnly(record.current && record.current.elements).length > 0;

export const recordNeedsMeasuring = (record, generation = MEASURE_GEN) =>
  !!record && (record.measure_gen ?? 0) < generation;

export const OPEN_CONVERT = "convert";
export const OPEN_REOPEN = "reopen";
export const OPEN_ASK = "ask";

// What happens when a board opens. Exactly one of:
//   convert - build the scene from the diagram text
//   reopen  - open the stored scene
//   ask     - the reviewer chooses between the two
export function decideOpening({ record, digest }) {
  if (!recordHasScene(record)) return OPEN_CONVERT;
  if (record.digest === digest) return OPEN_REOPEN;
  const reference = record.pristine ? record.pristine.elements : null;
  return hasEdits(reference, record.current.elements) ? OPEN_ASK : OPEN_CONVERT;
}

// The normalized URL of a link target, only for http, https and mailto;
// null for anything else (other schemes, relative or scheme-less input).
export function safeLinkTarget(raw) {
  if (typeof raw !== "string") return null;
  let url;
  try {
    url = new URL(raw.trim());
  } catch {
    return null;
  }
  return url.protocol === "http:" || url.protocol === "https:" || url.protocol === "mailto:" ? url.href : null;
}
