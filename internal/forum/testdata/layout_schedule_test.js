// Run by layout_dom_test.go: when forum-layout.js re-audits. Needs no layout
// engine: the audit is counted by how often it starts (each run asks the
// document for its fonts first). Prints "ok" or throws.
const fs = require("fs");
const assert = require("assert");
const vm = require("vm");

const listeners = {};
let reads = 0; // fontsReady reads document.fonts.ready twice per audit
const posted = [];
const parent = { postMessage: (m) => posted.push(m) };
const win = {
  parent,
  innerWidth: 1000,
  innerHeight: 700,
  addEventListener: (type, fn) => (listeners[type] ||= []).push(fn),
  setTimeout,
  clearTimeout,
  forum: { __dom: { selectorOf: () => "x" } },
  getComputedStyle: () => ({ overflowX: "visible", overflowY: "visible" }),
};
const doc = {
  documentElement: { scrollWidth: 1000, clientWidth: 1000 },
  body: null,
  readyState: "complete",
  currentScript: { src: "http://127.0.0.1/forum-assets/forum-layout.js?av=v7" },
  fonts: {
    get ready() {
      reads += 1;
      return Promise.resolve();
    },
  },
};
win.document = doc;
vm.runInNewContext(fs.readFileSync(process.argv[2], "utf8"), { window: win, document: doc, URL, Promise, Math, Number, Object, Array, Set, Map, String, JSON, Boolean, Element: function () {}, getComputedStyle: win.getComputedStyle, console });

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

(async () => {
  assert.deepStrictEqual(Object.keys(listeners).sort(), ["load", "resize"], "only load and resize re-audit: animationend/transitionend bubble from every hover and fade");
  await sleep(1500); // the first audit settles
  const audits = () => reads / 2;
  const base = audits();
  assert.ok(base >= 1, "the first audit ran");
  assert.ok(posted.length >= 1 && posted[0].type === "forum:layout", "and reported");
  assert.strictEqual(posted[0].artifact_version, "v7", "every pass carries the version of the document that ran it");

  // A window being dragged: one resize event every 100 ms for a second.
  for (let i = 0; i < 10; i += 1) {
    for (const fn of listeners.resize) fn();
    await sleep(100);
  }
  await sleep(1200);
  assert.ok(audits() - base <= 1, "a resize drag re-audits once, after it stops; got " + (audits() - base) + " audits");
  assert.ok(audits() - base >= 1, "and does re-audit when it stops");
  console.log("ok");
  process.exit(0);
})().catch((err) => {
  console.error(err);
  process.exit(1);
});
