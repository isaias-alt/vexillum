import assert from "node:assert/strict";
import { test } from "node:test";
import { indexView, resultItems } from "../lib/search-state.ts";

test("indexView shows the spinner while the index is idle or loading", () => {
  assert.deepEqual(indexView("idle"), { loading: true, failed: false });
  assert.deepEqual(indexView("loading"), { loading: true, failed: false });
});

test("indexView only reports a failure for a failed index load", () => {
  assert.deepEqual(indexView("error"), { loading: false, failed: true });
  assert.deepEqual(indexView("ready"), { loading: false, failed: false });
});

test("a retry replaces the failure: error then loading shows the spinner", () => {
  assert.equal(indexView("error").failed, true);
  assert.equal(indexView("loading").failed, false);
  assert.equal(indexView("loading").loading, true);
});

test("resultItems shows nothing for an empty query", () => {
  assert.equal(resultItems("empty", { pending: false, failed: false }), null);
  assert.equal(resultItems(undefined, { pending: false, failed: false }), null);
});

test("resultItems keeps an empty list (no results) only once settled", () => {
  const settled = { pending: false, failed: false };
  assert.deepEqual(resultItems([], settled), []);
  assert.equal(resultItems([], { pending: true, failed: false }), null);
  assert.equal(resultItems([], { pending: false, failed: true }), null);
});

test("resultItems keeps non-empty results while a new query is pending", () => {
  const data = [{ id: "a" }];
  assert.equal(resultItems(data, { pending: true, failed: false }), data);
});
