// Run by theme_test.go: exercises assets/chrome/forum-theme.js against a
// minimal fake window. Prints "ok" or throws.
const fs = require("fs");
const assert = require("assert");
const vm = require("vm");

const source = fs.readFileSync(process.argv[2], "utf8");

function load(storage) {
  const attrs = {};
  const win = {
    document: { documentElement: { getAttribute: (k) => (k in attrs ? attrs[k] : null), setAttribute: (k, v) => { attrs[k] = v; } } },
  };
  Object.defineProperty(win, "localStorage", { get: () => { if (storage === null) throw new Error("blocked"); return storage; } });
  vm.runInNewContext(source, { window: win });
  return { win, attrs };
}

function memory(initial) {
  const data = new Map(Object.entries(initial || {}));
  return { getItem: (k) => (data.has(k) ? data.get(k) : null), setItem: (k, v) => data.set(k, String(v)), data };
}

// Default: no saved choice means dark, whatever the OS prefers.
let { win, attrs } = load(memory());
assert.strictEqual(attrs["data-fr-theme"], "dark");
assert.strictEqual(win.forumTheme.current(), "dark");

// A saved light choice is applied at load time.
({ win, attrs } = load(memory({ "forum-theme": "light" })));
assert.strictEqual(attrs["data-fr-theme"], "light");

// Garbage in storage falls back to dark.
({ attrs } = load(memory({ "forum-theme": "purple" })));
assert.strictEqual(attrs["data-fr-theme"], "dark");

// The toggle flips and persists.
const store = memory();
({ win, attrs } = load(store));
assert.strictEqual(win.forumTheme.toggle(), "light");
assert.strictEqual(attrs["data-fr-theme"], "light");
assert.strictEqual(store.data.get("forum-theme"), "light");
assert.strictEqual(win.forumTheme.toggle(), "dark");
assert.strictEqual(store.data.get("forum-theme"), "dark");

// Storage that throws still works: dark by default, toggle applies for the page view.
({ win, attrs } = load(null));
assert.strictEqual(attrs["data-fr-theme"], "dark");
assert.strictEqual(win.forumTheme.toggle(), "light");
assert.strictEqual(attrs["data-fr-theme"], "light");

console.log("ok");
