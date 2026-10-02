// Run by chrome_dom_test.go: the round counter in the top bar, the transcript
// grouped by round with each user message's status, and the per-question
// "sent in round N" report posted to the artifact.
const assert = require("assert");
const { makeEnv } = require("./chrome_harness.js");

const boot = { key: "0123456789abcdef", token: "t", file: "/tmp/x/plan.html", name: "plan.html", artifact_src: "/a/x/plan.html" };
const at = "2026-01-01T12:00:00Z";
const msg = (id, role, round, extra = {}) => ({ id, role, text: id, at, round, ...extra });
const snap = (version, extra = {}) => ({ version, key: boot.key, file: boot.file, status: "open", listening: false, pending: 0, queued: [], transcript: [], artifact_version: "v1", round: 0, answered_through: 0, ...extra });
const text = (node) => (node.textContent || "") + (node.children || []).map(text).join("");
const classes = (node, cls) => ((node.className || "").split(" ").includes(cls) ? [node] : []).concat(...(node.children || []).map((c) => classes(c, cls)));

(async () => {
  const frameMessages = [];
  const tab = makeEnv(process.argv[2], boot);
  tab.get("artifact").contentWindow.postMessage = (m) => frameMessages.push(m);

  tab.push(snap(1));
  await tab.tick();
  assert.strictEqual(tab.get("roundChip").hidden, true, "no round before the first send");

  tab.push(snap(2, {
    round: 3,
    answered_through: 2,
    transcript: [
      msg("a0", "agent", 0),
      msg("u1", "user", 1, { queue_key: "question:q1" }),
      msg("r1", "agent", 1),
      msg("u2", "user", 2),
      msg("u3", "user", 3, { queue_key: "q2" }),
    ],
    queued: [{ uid: "p1", prompt: "later", tag: "message" }],
  }));
  await tab.tick();
  assert.strictEqual(tab.get("roundChip").hidden, false);
  assert.strictEqual(tab.get("roundNum").textContent, "3");
  assert.strictEqual(tab.get("roundChip").dataset.state, "open", "a round is still unanswered");
  assert.match(tab.get("roundChip").title, /2 of 3 rounds answered/);

  const log = tab.get("log");
  const groups = log.children;
  assert.deepStrictEqual(groups.map((g) => g.dataset.round), ["0", "1", "2", "3"], "messages are grouped by round, in order");
  assert.deepStrictEqual(groups.map((g) => g.dataset.answered), [undefined, "true", "true", "false"]);
  assert.ok(!classes(groups[0], "round-sep").length, "round 0 has no header");
  assert.match(text(classes(groups[1], "round-sep")[0]), /^Round 1answered$/);
  assert.match(text(classes(groups[3], "round-sep")[0]), /^Round 3waiting for the agent$/);

  const status = (group) => classes(group, "msg-status").map((n) => n.textContent + ":" + (n.dataset.state || ""));
  assert.deepStrictEqual(status(groups[1]), ["answered:answered", "answers round 1:"], "the user message is answered and the reply is tied to its round");
  assert.deepStrictEqual(status(groups[3]), ["sent:sent"], "an unanswered round is just sent");
  assert.strictEqual(tab.get("queuedRound").textContent, "for round 4", "queued messages say which round they will start");
  assert.ok(classes(tab.get("queuedList"), "queued-state").some((n) => n.textContent === "queued"), "a queued message is marked queued");

  // The artifact is told, per queue key, which round it was sent in and whether it is answered.
  const reported = frameMessages.filter((m) => m.type === "forum:rounds").pop();
  assert.deepStrictEqual(JSON.parse(JSON.stringify(reported.rounds)), { "question:q1": { round: 1, state: "answered" }, q2: { round: 3, state: "sent" } });

  // Answering the last round turns its messages answered and the chip done.
  tab.push(snap(3, { round: 3, answered_through: 3, transcript: [msg("u3", "user", 3, { queue_key: "q2" }), msg("r3", "agent", 3)] }));
  await tab.tick();
  assert.strictEqual(tab.get("roundChip").dataset.state, "done");
  assert.deepStrictEqual(JSON.parse(JSON.stringify(frameMessages.filter((m) => m.type === "forum:rounds").pop().rounds)), { q2: { round: 3, state: "answered" } });
  console.log("ok");
  process.exit(0);
})().catch((error) => {
  console.error(error);
  process.exit(1);
});
