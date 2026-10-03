import assert from "node:assert/strict";
import { test } from "node:test";
import { settled, stubFetch } from "./search-index-stub.mjs";

// One module instance per file: its status is process-wide state, and so is
// Fumadocs' cache of the loading promise. The failing case lives in
// search-index-retry.test.mjs (its own process).
const requests = stubFetch(() => true);
const index = await import("../lib/search-index.ts");

test("starts idle and the first client is generation 0", () => {
  assert.equal(index.getSearchIndexStatus(), "idle");
  assert.equal(index.createSearchClient("en").deps.at(-1), 0);
});

test("preload goes idle, loading, ready and fetches the index once", async () => {
  index.preloadSearchIndex("en");
  assert.equal(index.getSearchIndexStatus(), "loading");
  assert.equal(await settled(index.getSearchIndexStatus), "ready");
  assert.deepEqual(requests, ["/api/search"]);
});

test("later preloads and opens do nothing once ready", async () => {
  index.preloadSearchIndex("en");
  index.ensureSearchIndex("en");
  assert.equal(index.getSearchIndexStatus(), "ready");
  assert.equal(requests.length, 1);
  assert.equal(index.createSearchClient("en").deps.at(-1), 0);
});
