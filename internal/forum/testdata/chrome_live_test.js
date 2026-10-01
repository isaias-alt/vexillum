// Run by chrome_dom_test.go: the chrome's live feed (SSE) - it applies every
// snapshot, shows the connection banner when the stream fails or drops and
// reconnects by itself, and two tabs of one session end up showing the same.
const assert = require("assert");
const { makeEnv } = require("./chrome_harness.js");

const boot = { key: "0123456789abcdef", token: "t", file: "/tmp/x/plan.html", name: "plan.html", artifact_src: "/a/x/plan.html" };
const snap = (version, extra = {}) => ({ version, key: boot.key, file: boot.file, status: "open", listening: false, pending: 0, queued: [], transcript: [], artifact_version: "v1", ...extra });
const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

(async () => {
  // A first connection that fails: the banner says so, then it recovers.
  const tab = makeEnv(process.argv[2], boot, { failures: 1 });
  await tab.tick();
  assert.strictEqual(tab.get("connBanner").hidden, false, "a failed connection shows the reconnecting banner");
  await wait(1100);
  assert.strictEqual(tab.connections(), 2, "it reconnected on its own");
  tab.push(snap(1, { listening: true }));
  await tab.tick();
  assert.strictEqual(tab.get("connBanner").hidden, true, "a snapshot clears the banner");
  assert.strictEqual(tab.get("presenceText").textContent, "Agent listening");

  // Heartbeats are not events; the stream dropping is.
  tab.ping();
  await tab.tick();
  assert.strictEqual(tab.get("connBanner").hidden, true);
  tab.drop();
  await tab.tick();
  assert.strictEqual(tab.get("connBanner").hidden, false, "a dropped stream shows the banner");
  await wait(1100);
  assert.strictEqual(tab.connections(), 3, "and reconnects");
  tab.push(snap(2, { queued: [{ uid: "pr_1", prompt: "hi", tag: "message", queued_at: "2026-01-01T00:00:00Z" }] }));
  await tab.tick();
  assert.strictEqual(tab.get("connBanner").hidden, true);
  assert.strictEqual(tab.get("queuedCount").textContent, "(1)");

  // Two tabs of the same session reflect the same queue, listening state and end.
  const other = makeEnv(process.argv[2], boot);
  for (const t of [tab, other]) {
    t.push(snap(3, { listening: false, queued: [{ uid: "pr_1", prompt: "hi", tag: "message", queued_at: "2026-01-01T00:00:00Z" }, { uid: "pr_2", prompt: "two", tag: "message", queued_at: "2026-01-01T00:00:01Z" }] }));
  }
  await tab.tick();
  for (const t of [tab, other]) {
    assert.strictEqual(t.get("queuedCount").textContent, "(2)", "both tabs show the queue");
    assert.strictEqual(t.get("presenceText").textContent, "Agent not listening");
  }
  for (const t of [tab, other]) t.push(snap(4, { status: "ended", ended_by: "user" }));
  await tab.tick();
  for (const t of [tab, other]) {
    assert.strictEqual(t.get("sessionBadge").textContent, "Ended", "both tabs see the session end");
    assert.strictEqual(t.get("endedBackdrop").hidden, false, "and both show the ended dialog");
  }
  console.log("ok");
  process.exit(0);
})().catch((err) => {
  console.error(err);
  process.exit(1);
});
