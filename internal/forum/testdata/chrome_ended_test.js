// Run by chrome_dom_test.go: the session-ended dialog. Prints "ok" or throws.
const assert = require("assert");
const { makeEnv } = require("./chrome_harness.js");

const boot = { key: "0123456789abcdef", token: "t", file: "/tmp/x/plan.html", name: "plan.html", artifact_src: "/a/x/plan.html" };
const open = { version: 1, key: boot.key, file: boot.file, status: "open", listening: true, pending: 0, queued: [], transcript: [], artifact_version: "v1" };
const ended = (by) => ({ ...open, version: 2, status: "ended", ended_by: by });

(async () => {
  const env = makeEnv(process.argv[2], boot);
  const backdrop = env.get("endedBackdrop");
  const dialog = env.get("endedDialog");

  env.push(open);
  await env.tick();
  assert.strictEqual(env.app.inert, false, "nothing is blocked while the session is open");
  assert.strictEqual(env.app.inert, false, "an open session leaves the chrome interactive");

  // The user pressed Send & End (or the agent ended it): the dialog appears.
  env.push(ended("user"));
  await env.tick();
  assert.strictEqual(backdrop.hidden, false, "the dialog is shown once the state is ended");
  assert.strictEqual(env.app.inert, true, "the chrome and the artifact are inert behind it");
  assert.strictEqual(env.get("endedPath").textContent, boot.file, "it shows the artifact's absolute path");
  assert.match(env.get("endedDesc").textContent, /You ended this session/);
  assert.strictEqual(env.doc.activeElement, dialog, "focus moves into the dialog");

  // There is no way out: Escape is swallowed, nothing listens for clicks on
  // the backdrop or the dialog, and Tab stays inside.
  const esc = env.key({ key: "Escape" });
  assert.ok(esc.defaultPrevented && esc.stopped, "Escape is swallowed before any other handler");
  assert.strictEqual((backdrop.listeners.click || []).length, 0, "clicking outside does nothing");
  assert.strictEqual((dialog.listeners.click || []).length, 0);
  assert.strictEqual(backdrop.hidden, false);
  for (let i = 0; i < 4; i += 1) {
    const tab = env.key({ key: "Tab", shiftKey: i % 2 === 1 });
    assert.ok(tab.defaultPrevented, "Tab is trapped");
    assert.ok(env.doc.activeElement === dialog || env.doc.activeElement === env.get("endedCopy"), "focus never leaves the dialog");
  }

  // The agent reopening the session (or a later snapshot saying "open")
  // brings the chrome back.
  env.push({ ...open, version: 3 });
  await env.tick();
  assert.strictEqual(backdrop.hidden, true, "a reopened session removes the dialog");
  assert.strictEqual(env.app.inert, false);

  // An agent-ended session says so.
  env.push(ended("agent"));
  await env.tick();
  assert.match(env.get("endedDesc").textContent, /Your agent ended this session/);
  assert.strictEqual(backdrop.hidden, false);

  console.log("ok");
  process.exit(0);
})().catch((err) => {
  console.error(err);
  process.exit(1);
});
