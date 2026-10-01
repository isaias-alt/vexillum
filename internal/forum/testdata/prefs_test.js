// Run by theme_test.go: forum-prefs.js against a fake window. Prints "ok" or throws.
const fs = require("fs");
const assert = require("assert");
const vm = require("vm");
const source = fs.readFileSync(process.argv[2], "utf8");

function load(storage) {
  const win = {};
  Object.defineProperty(win, "localStorage", { get: () => { if (storage === null) throw new Error("blocked"); return storage; } });
  vm.runInNewContext(source, { window: win });
  return win.forumPrefs;
}
const memory = (initial) => {
  const data = new Map(Object.entries(initial || {}));
  return { getItem: (k) => (data.has(k) ? data.get(k) : null), setItem: (k, v) => data.set(k, String(v)), data };
};

// Annotation mode starts On in a new page load.
assert.strictEqual(load(memory()).annotate(), true);
// The user's choice is remembered either way.
const store = memory();
let prefs = load(store);
prefs.setAnnotate(false);
assert.strictEqual(store.data.get("forum-annotate"), "off");
assert.strictEqual(load(store).annotate(), false, "a saved Off survives a reload");
prefs.setAnnotate(true);
assert.strictEqual(load(store).annotate(), true);
// Unknown values fall back to On.
assert.strictEqual(load(memory({ "forum-annotate": "banana" })).annotate(), true);
// Blocked storage: still On, and setting does not throw.
prefs = load(null);
assert.strictEqual(prefs.annotate(), true);
assert.strictEqual(prefs.setAnnotate(false), false);
console.log("ok");
