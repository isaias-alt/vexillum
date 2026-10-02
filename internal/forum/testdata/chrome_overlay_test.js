// Run by chrome_dom_test.go: the blocking "agent is working" overlay. It comes
// from the server's awaiting_since (so any tab shows it), blocks everything
// behind it, cannot be escaped before the grace period, offers Stop waiting
// after it, and clears by itself on any snapshot that no longer says awaiting.
const assert = require("assert");
const { makeEnv } = require("./chrome_harness.js");

const boot = { key: "0123456789abcdef", token: "t", file: "/tmp/x/plan.html", name: "plan.html", artifact_src: "/a/x/plan.html" };
const ago = (ms) => new Date(Date.now() - ms).toISOString();
const snap = (version, extra = {}) => ({ version, key: boot.key, file: boot.file, status: "open", listening: false, pending: 0, queued: [], transcript: [], artifact_version: "v1", round: 1, answered_through: 0, ...extra });

(async () => {
  const tab = makeEnv(process.argv[2], boot);
  const backdrop = tab.get("workingBackdrop");
  backdrop.hidden = true; // the markup starts it hidden
  tab.get("endedBackdrop").hidden = true;
  const shown = () => !backdrop.hidden;

  tab.push(snap(1));
  await tab.tick();
  assert.ok(!shown(), "nothing awaited: no overlay");

  tab.push(snap(2, { awaiting_since: ago(2000) }));
  await tab.tick();
  assert.ok(shown(), "awaiting_since shows the overlay");
  assert.strictEqual(tab.app.inert, true, "the artifact and conversation are inert behind it");
  assert.strictEqual(tab.get("workingStop").hidden, true, "no way out before the grace period");
  assert.match(tab.get("workingElapsed").textContent, /^Working for [0-9]+s$/);
  assert.strictEqual(tab.doc.activeElement, tab.get("workingDialog"), "focus moves into the dialog");

  const escape = tab.key({ key: "Escape" });
  assert.ok(escape.defaultPrevented && shown(), "Escape does not dismiss it before the grace period");

  // Past the grace period: Stop waiting appears, Escape and the button both work, and the server is told.
  const stale = ago(31000);
  tab.push(snap(3, { awaiting_since: stale }));
  await tab.tick();
  assert.strictEqual(tab.get("workingStop").hidden, false);
  tab.get("workingStop").click();
  await tab.tick();
  assert.ok(!shown() && tab.app.inert === false, "Stop waiting gives the surface back");
  assert.ok(tab.calls.some((c) => c.url.endsWith("/stop-waiting") && c.init.method === "POST"), "the server is told");

  // The same awaiting_since does not bring it back; a new send does.
  tab.push(snap(4, { awaiting_since: stale }));
  await tab.tick();
  assert.ok(!shown(), "a wait the user gave up on stays dismissed in this tab");
  tab.push(snap(5, { round: 2, awaiting_since: ago(500) }));
  await tab.tick();
  assert.ok(shown(), "the next round blocks again");

  // Escape past the grace period is a way out too.
  tab.push(snap(6, { round: 2, awaiting_since: ago(31000) }));
  await tab.tick();
  tab.key({ key: "Escape" });
  await tab.tick();
  assert.ok(!shown(), "Escape after the grace period stops waiting");

  // Any answer clears it: a snapshot without awaiting_since (reply, poll, reload, or a reconnect to an idle server).
  tab.push(snap(7, { round: 3, awaiting_since: ago(1000) }));
  await tab.tick();
  assert.ok(shown());
  tab.push(snap(8, { round: 3, answered_through: 3 }));
  await tab.tick();
  assert.ok(!shown() && tab.app.inert === false, "clears when the server says nothing is awaited");

  // The ended dialog wins over the overlay and keeps the surface inert.
  tab.push(snap(9, { round: 4, awaiting_since: ago(1000) }));
  await tab.tick();
  tab.push(snap(10, { round: 4, status: "ended", ended_by: "user", awaiting_since: ago(1000) }));
  await tab.tick();
  assert.ok(!shown() && tab.app.inert === true && !tab.get("endedBackdrop").hidden, "an ended session shows only the ended dialog");
  console.log("ok");
  process.exit(0);
})().catch((error) => {
  console.error(error);
  process.exit(1);
});
