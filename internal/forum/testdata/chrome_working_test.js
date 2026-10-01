// Run by chrome_dom_test.go: the three things the panel can say about the
// agent - listening (a poll is open), working (it took the user's prompts and
// has not polled or replied since) and not listening - and that "working"
// goes back to the not-listening warning by itself when its window runs out.
const assert = require("assert");
const { makeEnv } = require("./chrome_harness.js");

const boot = { key: "0123456789abcdef", token: "t", file: "/tmp/x/plan.html", name: "plan.html", artifact_src: "/a/x/plan.html" };
const snap = (version, extra = {}) => ({ version, key: boot.key, file: boot.file, status: "open", listening: false, pending: 0, queued: [], transcript: [], artifact_version: "v1", ...extra });
const inMs = (ms) => new Date(Date.now() + ms).toISOString();
const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

(async () => {
  const tab = makeEnv(process.argv[2], boot);
  const say = () => ({ state: tab.get("presence").dataset.state, text: tab.get("presenceText").textContent, warn: tab.get("listenBanner").hidden, working: tab.get("workingBanner").hidden });

  tab.push(snap(1, { working_until: inMs(60000) }));
  await tab.tick();
  assert.deepStrictEqual(say(), { state: "working", text: "Agent working", warn: true, working: false }, "delivered and not polling: working, not 'not listening'");

  tab.push(snap(2, { listening: true, working_until: inMs(60000) }));
  await tab.tick();
  assert.deepStrictEqual(say(), { state: "listening", text: "Agent listening", warn: true, working: true }, "an open poll always wins");

  tab.push(snap(3));
  await tab.tick();
  assert.deepStrictEqual(say(), { state: "idle", text: "Agent not listening", warn: false, working: true }, "never delivered: the plain warning");

  tab.push(snap(4, { working_until: inMs(-1000) }));
  await tab.tick();
  assert.strictEqual(say().state, "idle", "a window already past does not show working");

  // The window runs out with no new snapshot: the panel falls back by itself.
  tab.push(snap(5, { working_until: inMs(300) }));
  await tab.tick();
  assert.strictEqual(say().state, "working");
  await wait(900);
  assert.deepStrictEqual(say(), { state: "idle", text: "Agent not listening", warn: false, working: true }, "after the window: not listening again");

  tab.push(snap(6, { status: "ended", ended_by: "user", working_until: inMs(60000) }));
  await tab.tick();
  assert.strictEqual(say().state, "ended", "an ended session never shows working");
  console.log("ok");
  process.exit(0);
})().catch((error) => {
  console.error(error);
  process.exit(1);
});
