// Run by chrome_dom_test.go: the conversation panel follows the reviewer's own
// queue or send to the bottom (where the queued list sits under the
// transcript), keeps a reviewer who is already near the bottom there when
// something else arrives, and never moves one who scrolled up to read.
const assert = require("assert");
const { makeEnv } = require("./chrome_harness.js");

const boot = { key: "0123456789abcdef", token: "t", file: "/tmp/x/plan.html", name: "plan.html", artifact_src: "/a/x/plan.html" };
const at = new Date().toISOString();
const agent = (round) => ({ id: "m" + round, role: "agent", text: "reply " + round, round, at });
const transcript = (n) => Array.from({ length: n }, (_, i) => agent(i + 1));
const prompt = (uid) => ({ uid, prompt: "queued " + uid, tag: "message" });
const snap = (version, messages, queued = []) => ({ version, key: boot.key, file: boot.file, status: "open", listening: false, pending: 0, queued, transcript: messages, round: messages.length, answered_through: messages.length, artifact_version: "v1" });
const VIEWPORT = 500;

(async () => {
  const tab = makeEnv(process.argv[2], boot);
  // The fake has no layout: the scroll area is as tall as what the script put in
  // it (one 100px block per round and per queued prompt).
  const scroll = tab.get("scroll");
  Object.defineProperty(scroll, "scrollHeight", { get: () => 400 + 100 * (tab.get("log").children.length + tab.get("queuedList").children.length) });
  scroll.clientHeight = VIEWPORT;
  const bottom = () => scroll.scrollHeight - VIEWPORT;
  const deliver = async (snapshot) => {
    tab.push(snapshot);
    await tab.tick();
  };

  let version = 1;
  let messages = transcript(12);
  await deliver(snap(version++, messages));

  // Near the bottom: an agent reply and then a queue change elsewhere both stay in view.
  scroll.scrollTop = bottom();
  messages = transcript(13);
  await deliver(snap(version++, messages));
  assert.strictEqual(scroll.scrollTop, bottom(), "an agent reply follows a reviewer who is at the bottom");
  await deliver(snap(version++, messages, [prompt("a")]));
  assert.strictEqual(scroll.scrollTop, bottom(), "the queued list stays in view for a reviewer at the bottom");

  // Scrolled up to read: neither an agent reply nor a queue change moves them.
  scroll.scrollTop = 120;
  messages = transcript(14);
  await deliver(snap(version++, messages, [prompt("a")]));
  await deliver(snap(version++, messages, [prompt("a"), prompt("b")]));
  assert.strictEqual(scroll.scrollTop, 120, "a reviewer who scrolled up is not yanked down");

  // Their own queue follows, even from up there, once the new queue arrives.
  tab.get("input").value = "one more";
  tab.get("queueBtn").click();
  await tab.tick();
  assert.ok(tab.calls.some((c) => c.url.endsWith("/queue") && c.init.method === "POST"), "the message was queued");
  assert.strictEqual(scroll.scrollTop, 120, "nothing moves until the queue has changed");
  await deliver(snap(version++, messages, [prompt("a"), prompt("b"), prompt("c")]));
  assert.strictEqual(scroll.scrollTop, bottom(), "queuing a message brings the queued list into view");

  // The follow is spent: the next arrival leaves a reviewer who scrolled up alone.
  scroll.scrollTop = 120;
  await deliver(snap(version++, transcript(15), [prompt("a"), prompt("b"), prompt("c")]));
  assert.strictEqual(scroll.scrollTop, 120, "following was one-shot");

  // Sending: the queue empties into a new round, which comes into view.
  tab.get("sendBtn").click();
  tab.get("composer").listeners.submit[0].fn({ preventDefault() {} });
  await tab.tick();
  await deliver(snap(version++, transcript(16)));
  assert.strictEqual(scroll.scrollTop, bottom(), "sending brings the newest round into view");

  console.log("ok");
  process.exit(0);
})().catch((error) => {
  console.error(error);
  process.exit(1);
});
