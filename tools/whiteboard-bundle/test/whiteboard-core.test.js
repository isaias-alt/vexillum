// Run with `node --test test/` from tools/whiteboard-bundle (internal/forum's
// TestWhiteboardCore_NodeSuite runs it too, when node is installed).
import assert from "node:assert/strict";
import { test } from "node:test";

import {
  chooseStartMode,
  convertTwice,
  fitLabelsToNodes,
  fixSkeletonLabels,
  isImageOnly,
  labelWithRealBreaks,
  persistenceFields,
  remeasureText,
  repeatedIds,
  safeLinkTarget,
  sizeNodeSkeletons,
  savedSceneWasEdited,
  summarizeEdits,
  TEXT_METRICS_VERSION,
  withoutThemeFields,
} from "../src/whiteboard-core.js";

const box = (id, x, y, extra = {}) => ({ id, type: "rectangle", x, y, width: 100, height: 40, ...extra });
const label = (id, containerId, text, extra = {}) => ({
  id,
  type: "text",
  containerId,
  text,
  x: 0,
  y: 0,
  width: 40,
  height: 20,
  ...extra,
});

test("theme and canvas colour never survive in app state", () => {
  assert.deepEqual(withoutThemeFields({ theme: "dark", viewBackgroundColor: "#000", zoom: { value: 2 } }), {
    zoom: { value: 2 },
  });
  for (const junk of [null, undefined, "x", [], 3]) assert.deepEqual(withoutThemeFields(junk), {});
});

test("mermaid line breaks become real newlines", () => {
  assert.equal(labelWithRealBreaks("a<br>b<BR/>c<br />d\\ne"), "a\nb\nc\nd\ne");
  assert.equal(labelWithRealBreaks(7), 7);
  const [fixed] = fixSkeletonLabels([{ type: "rectangle", label: { text: "x<br>y", fontSize: 16 } }]);
  assert.equal(fixed.label.text, "x\ny");
  assert.equal(fixed.label.fontSize, 16);
  const untouched = { type: "rectangle", label: { text: "plain" } };
  assert.equal(fixSkeletonLabels([untouched])[0], untouched);
});

test("a node grows around its measured label and the label stays centred", () => {
  const node = box("n", 100, 100);
  const text = label("t", "n", "wide label", { x: 130, y: 110 });
  const [grownNode, grownText] = fitLabelsToNodes([node, text], { measure: () => ({ width: 160, height: 60 }) });
  assert.equal(grownText.width, 160);
  assert.equal(grownText.height, 60);
  assert.ok(grownNode.width >= 176 && grownNode.height >= 76, "padding around the text");
  // growth is symmetric: the node's centre did not move
  assert.equal(grownNode.x + grownNode.width / 2, 150);
  assert.equal(grownNode.y + grownNode.height / 2, 120);
  assert.equal(grownText.x + grownText.width / 2, grownNode.x + grownNode.width / 2);
  assert.equal(grownText.y + grownText.height / 2, grownNode.y + grownNode.height / 2);
});

test("labels of arrows are left where they are", () => {
  const arrow = { id: "a", type: "arrow", x: 0, y: 0, width: 50, height: 10 };
  const text = label("t", "a", "yes", { x: 20, y: 5 });
  const out = fitLabelsToNodes([arrow, text], { measure: () => ({ width: 90, height: 30 }) });
  assert.equal(out[0], arrow);
  // the text box itself may grow, but around its own centre
  assert.equal(out[1].x + out[1].width / 2, 40);
  assert.equal(out[1].y + out[1].height / 2, 15);
});

test("fitting without a measurer only recentres", () => {
  const out = fitLabelsToNodes([box("n", 0, 0), label("t", "n", "x", { x: 5, y: 5 })]);
  assert.equal(out[1].x, 30);
  assert.equal(out[1].y, 10);
});

test("saved text boxes are only ever grown", () => {
  const small = label("t", null, "hello", { containerId: undefined });
  const big = label("u", null, "hi", { width: 500, height: 200 });
  const fixed = label("v", null, "pinned", { autoResize: false });
  const { elements, changed } = remeasureText([small, big, fixed], () => ({ width: 120, height: 30 }));
  assert.equal(changed, 1);
  assert.equal(elements[0].width, 120);
  assert.equal(elements[1], big);
  assert.equal(elements[2], fixed);
});

test("only plain web and mail links are allowed out", () => {
  assert.equal(safeLinkTarget(" https://example.com/a?b=c "), "https://example.com/a?b=c");
  assert.equal(safeLinkTarget("HTTP://example.com"), "HTTP://example.com");
  assert.equal(safeLinkTarget("mailto:a@b.co"), "mailto:a@b.co");
  for (const bad of ["javascript:alert(1)", "data:text/html,x", "file:///etc/passwd", "/relative", "vbscript:x", "", null, "mailto:", "mailto:a b"]) {
    assert.equal(safeLinkTarget(bad), "", String(bad));
  }
});

test("an image-only scene is the converter's fallback", () => {
  assert.equal(isImageOnly([{ type: "image" }, { type: "image", isDeleted: true }]), true);
  assert.equal(isImageOnly([{ type: "image" }, { type: "arrow" }]), false);
  assert.equal(isImageOnly([]), false);
  assert.equal(isImageOnly([{ type: "image", isDeleted: true }]), false);
});

test("repeated ids are reported once each", () => {
  assert.deepEqual(repeatedIds([{ id: "a" }, { id: "b" }, { id: "a" }, { id: "a" }, {}, { id: "b" }]).sort(), ["a", "b"]);
  assert.deepEqual(repeatedIds([{ id: "a" }]), []);
});

test("conversion runs twice, with the fonts requested in between", async () => {
  const order = [];
  const out = await convertTwice(["skeleton"], {
    materialize: (input) => {
      order.push(`materialize:${input[0]}`);
      return [`el${order.length}`];
    },
    preload: async (draft) => {
      order.push(`preload:${draft[0]}`);
    },
    refine: (input) => input.map((item) => `${item}+`),
  });
  assert.deepEqual(order, ["materialize:skeleton", "preload:el1", "materialize:skeleton+"]);
  assert.deepEqual(out, ["el3"]);
});

test("node skeletons are sized to their measured labels before conversion", () => {
  const measure = () => ({ width: 60, height: 20 });
  const rect = { type: "rectangle", id: "r", x: 100, y: 100, width: 50, height: 30, label: { text: "x" } };
  const diamond = { ...rect, type: "diamond", id: "d" };
  const [grownRect, grownDiamond] = sizeNodeSkeletons([rect, diamond], measure);
  assert.equal(grownRect.width, 76);
  assert.equal(grownRect.height, 36);
  assert.equal(grownRect.x + grownRect.width / 2, 125, "centre kept");
  assert.equal(grownDiamond.width, 152); // twice the label plus padding
  assert.equal(grownDiamond.height, 72);
  // already large enough, unlabelled, or not a node: untouched
  const roomy = { ...rect, width: 400, height: 400 };
  assert.equal(sizeNodeSkeletons([roomy], measure)[0], roomy);
  const bare = { type: "rectangle", x: 0, y: 0, width: 5, height: 5 };
  const arrow = { type: "arrow", x: 0, y: 0, label: { text: "yes" } };
  const unsized = { type: "rectangle", label: { text: "x" } };
  assert.deepEqual(sizeNodeSkeletons([bare, arrow, unsized], measure), [bare, arrow, unsized]);
});

test("arrows follow the nodes they are attached to when those grow", () => {
  const measure = () => ({ width: 84, height: 20 });
  const a = { type: "rectangle", id: "A", x: 0, y: 0, width: 100, height: 40, label: { text: "A" } };
  const b = { type: "rectangle", id: "B", x: 200, y: 0, width: 100, height: 40, label: { text: "B" } };
  // the arrow runs from A's right edge (100, 20) to B's left edge (200, 20)
  const arrow = { type: "arrow", id: "A_B", x: 100, y: 20, start: { id: "A" }, end: { id: "B" }, points: [[0, 0], [50, 0], [100, 0]] };
  const loose = { type: "arrow", id: "free", x: 0, y: 90, points: [[0, 0], [40, 0]] };
  const out = sizeNodeSkeletons([a, b, arrow, loose], measure);
  // both boxes are now 100 wide (84 + 16): nothing grew, so nothing moved
  assert.equal(out[2], arrow);

  const wide = () => ({ width: 184, height: 20 });
  const [ga, gb, garrow, gloose] = sizeNodeSkeletons([a, b, arrow, loose], wide);
  assert.equal(ga.width, 200);
  assert.equal(ga.x, -50);
  assert.equal(gb.x, 150);
  // tips stay on the outlines: A's right edge is now at 150, B's left edge at 150
  const tip = (el) => [el.x + el.points.at(-1)[0], el.y + el.points.at(-1)[1]];
  assert.deepEqual([garrow.x, garrow.y], [150, 20]);
  assert.deepEqual(tip(garrow), [150, 20]);
  assert.deepEqual(garrow.points[0], [0, 0]);
  assert.equal(gloose, loose);
});

test("the persisted fields carry the hash, metrics version, scene and baseline", () => {
  const fields = persistenceFields({ sceneSourceHash: "h1", textMetricsVersion: 1.9, baselineElements: [{ id: "a" }] }, { elements: [] });
  assert.deepEqual(fields, { sourceHash: "h1", textMetricsVersion: 1, scene: { elements: [] }, baseline: { elements: [{ id: "a" }] } });
  assert.equal(persistenceFields({}, undefined).scene, null);
  assert.equal(persistenceFields({}, null).textMetricsVersion, 0);
  assert.equal(TEXT_METRICS_VERSION, 1);
});

test("what counts as a reviewer edit", () => {
  const baseline = [box("a", 0, 0), label("t", "a", "Start", { x: 10, y: 10 }), box("b", 200, 0)];
  const saved = (elements, withBaseline = true) => ({ scene: { elements }, baseline: withBaseline ? { elements: baseline } : undefined });

  assert.equal(savedSceneWasEdited(saved(structuredClone(baseline))), false);
  // style churn and sub-tolerance jitter are not edits
  const noisy = structuredClone(baseline);
  noisy[0].strokeColor = "#ff0000";
  noisy[0].version = 9;
  noisy[0].x = 1;
  assert.equal(savedSceneWasEdited(saved(noisy)), false);

  const moved = structuredClone(baseline);
  moved[2].x = 260;
  assert.equal(savedSceneWasEdited(saved(moved)), true);

  const renamed = structuredClone(baseline);
  renamed[1].text = "Begin";
  assert.equal(savedSceneWasEdited(saved(renamed)), true);

  assert.equal(savedSceneWasEdited(saved([...baseline, box("c", 0, 99)])), true);
  assert.equal(savedSceneWasEdited(saved(baseline.slice(0, 2))), true);
  // deleted-in-place counts as gone
  const gone = structuredClone(baseline);
  gone[2].isDeleted = true;
  assert.equal(savedSceneWasEdited(saved(gone)), true);

  const rewired = [{ id: "e", type: "arrow", x: 0, y: 0, width: 5, height: 5, startBinding: { elementId: "a" }, points: [[0, 0], [5, 5]] }];
  const rewiredAfter = [{ ...rewired[0], endBinding: { elementId: "b" } }];
  assert.equal(savedSceneWasEdited({ scene: { elements: rewiredAfter }, baseline: { elements: rewired } }), true);
  const bent = [{ ...rewired[0], points: [[0, 0], [5, 40]] }];
  assert.equal(savedSceneWasEdited({ scene: { elements: bent }, baseline: { elements: rewired } }), true);

  // without a baseline any saved scene is assumed to carry edits
  assert.equal(savedSceneWasEdited(saved(baseline, false)), true);
  assert.equal(savedSceneWasEdited({}), false);
});

test("opening a board: convert, restore or ask", () => {
  const base = [box("a", 0, 0)];
  const record = (hash, elements) => ({ source_hash: hash, scene: { elements }, baseline: { elements: base } });

  assert.equal(chooseStartMode(null, "h"), "convert");
  assert.equal(chooseStartMode({ source_hash: "h" }, "h"), "convert"); // no scene
  assert.equal(chooseStartMode(record("h", base), "h"), "restore");
  assert.equal(chooseStartMode(record("old", base), "new"), "convert"); // untouched autosave
  assert.equal(chooseStartMode(record("old", [box("a", 90, 0)]), "new"), "ask");
});

test("an edit summary names what changed, bounded and without bound-label noise", () => {
  const baseline = [box("a", 0, 0), label("ta", "a", "Start"), box("b", 200, 0), label("tb", "b", "Done"), { id: "gone", type: "ellipse", x: 0, y: 300, width: 10, height: 10 }];
  const edited = structuredClone(baseline).filter((el) => el.id !== "gone");
  edited[0].x = 40; // moved by 40
  edited[3].text = "Finished"; // relabel via the bound text
  edited.push(box("c", 0, 500), label("tc", "c", "Cache"));
  edited.push({ id: "link", type: "arrow", x: 0, y: 0, width: 5, height: 5, startBinding: { elementId: "a" }, endBinding: { elementId: "c" } });
  edited.push({ id: "scribble", type: "freedraw", x: 12.4, y: 7.6, width: 1, height: 1 });

  const summary = summarizeEdits(baseline, edited);
  const text = summary.lines.join("\n");
  assert.deepEqual(summary.stats, { added: 2, removed: 1, moved: 1, relabeled: 1, drawn: 1 });
  assert.equal(summary.totalChanges, 6);
  assert.match(text, /Added rectangle "Cache" \(c\)/);
  assert.match(text, /Added arrow \(link\) from rectangle "Start" \(a\) to rectangle "Cache" \(c\)/);
  assert.match(text, /Removed ellipse \(gone\)/);
  assert.match(text, /Moved by \(40, 0\): rectangle "Start" \(a\)/);
  assert.match(text, /Relabeled rectangle \(b\): "Done" -> "Finished"/);
  assert.match(text, /Drew a freehand mark near \(12, 8\)/);
  assert.ok(!/text "Cache"/.test(text), "the label of a new shape is reported through the shape");
});

test("an unchanged scene says so, and long summaries are cut", () => {
  const baseline = [box("a", 0, 0)];
  const same = summarizeEdits(baseline, structuredClone(baseline));
  assert.equal(same.totalChanges, 0);
  assert.match(same.lines[0], /No element changes detected/);

  const many = Array.from({ length: 60 }, (_, i) => box(`n${i}`, i * 500, 0));
  const long = summarizeEdits([], many, { maxLines: 5 });
  assert.equal(long.lines.length, 6);
  assert.equal(long.lines[5], "...and 55 more changes");
  assert.equal(summarizeEdits([], many.slice(0, 2), { maxLines: 1 }).lines[1], "...and 1 more change");

  const wordy = summarizeEdits([], [box("x".repeat(400), 0, 0)]);
  assert.ok(wordy.lines[0].length <= 200);
});
