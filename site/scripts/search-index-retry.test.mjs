import assert from "node:assert/strict";
import { test } from "node:test";
import { settled, stubFetch } from "./search-index-stub.mjs";

// The first request for the index fails, every later one succeeds. Fumadocs
// caches the rejected promise per URL, so the retry must use another URL.
const requests = stubFetch((_url, attempt) => attempt > 1);
const index = await import("../lib/search-index.ts");

test("a failed preload ends in error and stays quiet", async () => {
  index.preloadSearchIndex("en");
  assert.equal(index.getSearchIndexStatus(), "loading");
  assert.equal(await settled(index.getSearchIndexStatus), "error");
  assert.deepEqual(requests, ["/api/search"]);
  assert.equal(index.createSearchClient("en").deps.at(-1), 0);
});

test("focus and idle preloads do not retry a failed load", async () => {
  index.preloadSearchIndex("en");
  index.preloadSearchIndex("en");
  await new Promise((resolve) => setTimeout(resolve, 20));
  assert.equal(index.getSearchIndexStatus(), "error");
  assert.equal(requests.length, 1);
  assert.equal(index.createSearchClient("en").deps.at(-1), 0);
});

test("the next real open retries on a fresh URL with a bumped generation", async () => {
  index.ensureSearchIndex("en");
  assert.equal(index.getSearchIndexStatus(), "loading");
  assert.equal(index.createSearchClient("en").deps.at(-1), 1);
  assert.equal(await settled(index.getSearchIndexStatus), "ready");
  assert.deepEqual(requests, ["/api/search", "/api/search?r=1"]);
});

test("once ready, opening again does not fetch or bump the generation", () => {
  index.ensureSearchIndex("en");
  assert.equal(index.getSearchIndexStatus(), "ready");
  assert.equal(requests.length, 2);
  assert.equal(index.createSearchClient("en").deps.at(-1), 1);
});
