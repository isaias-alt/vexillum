// Run by chrome_dom_test.go: the "Marks" switch next to Annotate and what the
// chrome reports to the artifact for the status badges - annotations by the
// selector they were sent with (forms are matched by queue key instead), the
// newest round per selector, answered or not, and whether to show them at all.
const assert = require("assert");
const { makeEnv } = require("./chrome_harness.js");

const boot = { key: "0123456789abcdef", token: "t", file: "/tmp/x/plan.html", name: "plan.html", artifact_src: "/a/x/plan.html" };
const at = "2026-01-01T12:00:00Z";
const msg = (id, round, extra = {}) => ({ id, role: "user", text: id, at, round, ...extra });
const snap = (version, extra = {}) => ({ version, key: boot.key, file: boot.file, status: "open", listening: false, pending: 0, queued: [], transcript: [], artifact_version: "v1", round: 0, answered_through: 0, ...extra });
const plain = (value) => JSON.parse(JSON.stringify(value));

(async () => {
  const sent = [];
  const prefs = { marks: true };
  const tab = makeEnv(process.argv[2], boot);
  tab.get("artifact").contentWindow.postMessage = (m) => sent.push(plain(m));
  const lastMarks = () => sent.filter((m) => m.type === "forum:marks").pop();

  tab.push(snap(1));
  await tab.tick();
  assert.deepStrictEqual(lastMarks(), { type: "forum:marks", visible: true, annotations: [] });
  assert.strictEqual(tab.get("marksSwitch").attrs["aria-checked"], "true");
  assert.strictEqual(tab.get("marksState").textContent, "On");

  tab.push(snap(2, {
    round: 2,
    answered_through: 1,
    transcript: [
      msg("a", 1, { selector: "#p1", text: "first note" }),
      msg("b", 2, { selector: "#p1", text: "second note" }),
      msg("c", 1, { selector: "#p2", text: "other" }),
      msg("form", 2, { selector: "form#f > button", queue_key: "question:q1" }), // a decision: shown on its form, not as a comment
      msg("none", 2), // no selector: nothing to attach to
      { id: "ag", role: "agent", text: "ok", at, round: 2, selector: "#p9" },
    ],
  }));
  await tab.tick();
  assert.deepStrictEqual(lastMarks().annotations, [
    { selector: "#p1", round: 2, state: "sent", count: 2, text: "second note" },
    { selector: "#p2", round: 1, state: "answered", count: 1, text: "other" },
  ]);

  // Answering round 2 turns the newest comment answered.
  tab.push(snap(3, { round: 2, answered_through: 2, transcript: [msg("a", 1, { selector: "#p1", text: "x" }), msg("b", 2, { selector: "#p1", text: "y" })] }));
  await tab.tick();
  assert.deepStrictEqual(lastMarks().annotations, [{ selector: "#p1", round: 2, state: "answered", count: 2, text: "y" }]);

  // Hide all: the artifact is told, the switch follows, and it comes back On.
  tab.get("marksSwitch").click();
  assert.strictEqual(lastMarks().visible, false);
  assert.strictEqual(tab.get("marksSwitch").attrs["aria-checked"], "false");
  assert.strictEqual(tab.get("marksState").textContent, "Off");
  tab.get("marksSwitch").click();
  assert.strictEqual(lastMarks().visible, true);

  // A (re)loaded artifact announces itself and is told everything again.
  const before = sent.filter((m) => m.type === "forum:marks").length;
  for (const l of tab.winListeners.message || []) l({ source: tab.get("artifact").contentWindow, data: { type: "forum:ready" } });
  assert.ok(sent.filter((m) => m.type === "forum:marks").length > before, "forum:ready makes the chrome resend the marks");
  console.log("ok");
  process.exit(0);
})().catch((error) => {
  console.error(error);
  process.exit(1);
});
