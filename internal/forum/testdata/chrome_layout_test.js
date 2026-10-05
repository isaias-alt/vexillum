// Run by chrome_dom_test.go: the Layout issues tray and the bridge that carries
// the artifact's passive audit to the server. Prints "ok" or throws.
const assert = require("assert");
const { makeEnv } = require("./chrome_harness.js");

const boot = { key: "0123456789abcdef", token: "t", file: "/tmp/x/plan.html", name: "plan.html", artifact_src: "/a/x/plan.html" };
const warning = (id, over = {}) => ({
  id, status_label: "", title: "Text cut off by its container", explanation: "Rendered text crosses its container's right edge by 40px and is hidden.",
  selector: "div#card > p", viewport_label: "Desktop", viewport_width: 1200,
  active: true, selectable: true, outstanding: false, ...over,
});
const snapshot = (over = {}) => ({ version: 1, key: boot.key, file: boot.file, status: "open", listening: true, pending: 0, queued: [], transcript: [], artifact_version: "v1", layout_warnings: [], ...over });

async function fromFrame(env, data) {
  for (const l of env.winListeners.message || []) await l({ source: env.get("artifact").contentWindow, data });
}
const posts = (env, suffix) => env.calls.filter((c) => c.url.endsWith(suffix));

(async () => {
  const env = makeEnv(process.argv[2], boot);
  const tray = env.get("layoutTray");
  const list = env.get("layoutList");
  const count = env.get("layoutCount");
  tray.hidden = true; // the markup ships it closed
  const pass = (extra = {}) => ({ type: "forum:layout", artifact_version: "v1", complete: true, target_presence_complete: true, viewport_width: 1200, findings: [], ...extra });

  // A pass that arrives before the first snapshot waits for the artifact version.
  await fromFrame(env, pass({ findings: [{ kind: "clipped-text", selector: "p", axis: "horizontal", overflow_px: 12 }] }));
  assert.strictEqual(posts(env, "/layout/diagnostics").length, 0, "nothing is sent before the chrome knows which artifact version it shows");

  env.push(snapshot({ layout_warnings: [warning("a1"), warning("b2", { title: "Control cut off by its container", selector: "button#go" })] }));
  await env.tick();
  const sent = posts(env, "/layout/diagnostics");
  assert.strictEqual(sent.length, 1, "the held pass is sent once the version is known");
  const body = JSON.parse(sent[0].init.body);
  assert.strictEqual(body.artifact_version, "v1", "the pass keeps the version of the document that produced it");
  assert.strictEqual(body.complete, true);
  assert.deepStrictEqual(body.findings, [{ kind: "clipped-text", selector: "p", axis: "horizontal", overflow_px: 12 }]);
  assert.strictEqual(sent[0].init.headers["X-Forum-Token"], "t", "passes carry the session token like every other request");

  // A pass with no version, or one the audit stamped for a document the chrome
  // no longer shows, is never filed against the current one.
  await fromFrame(env, pass({ artifact_version: undefined }));
  await fromFrame(env, pass({ artifact_version: "v0-old" }));
  assert.strictEqual(posts(env, "/layout/diagnostics").length, 1, "unversioned and old-document passes are not sent");

  // The tray reflects the snapshot: a count, one item per issue, closed by default.
  assert.strictEqual(count.textContent, "2");
  assert.strictEqual(count.hidden, false);
  assert.strictEqual(list.children.length, 2);
  assert.strictEqual(tray.hidden, true, "the tray stays closed until the user opens it");
  assert.strictEqual(env.get("layoutEmpty").hidden, true);

  // Messages from anywhere but the artifact frame are ignored, and garbage is bounded.
  for (const l of env.winListeners.message || []) await l({ source: {}, data: pass() });
  await fromFrame(env, pass({ viewport_width: Infinity, findings: [{ kind: "x".repeat(500), selector: "s".repeat(900), axis: "diagonal", overflow_px: NaN }, null, 7] }));
  const hostile = JSON.parse(posts(env, "/layout/diagnostics")[1].init.body);
  assert.strictEqual(posts(env, "/layout/diagnostics").length, 2, "only the frame's own message counted");
  assert.strictEqual(hostile.viewport_width, 0);
  assert.strictEqual(hostile.findings.length, 1);
  assert.ok(hostile.findings[0].kind.length <= 64 && hostile.findings[0].selector.length <= 300);
  assert.strictEqual(hostile.findings[0].axis, "horizontal");
  assert.strictEqual(hostile.findings[0].overflow_px, 0);

  // A plain issue has no status label; the title is just the title.
  const titleOf = (item) => item.children.find((c) => c.className === "layout-item").children[0];
  assert.strictEqual(titleOf(list.children[0]).children.length, 0, "an empty status_label shows no label");

  // Open the tray, select one issue, queue it.
  env.get("layoutBtn").click();
  assert.strictEqual(tray.hidden, false);
  assert.strictEqual(env.get("layoutBtn").attrs["aria-expanded"], "true");
  assert.strictEqual(env.get("layoutQueue").disabled, true, "nothing selected, nothing to queue");
  const first = list.children[0];
  const checkbox = first.children[0];
  assert.strictEqual(checkbox.type, "checkbox");
  checkbox.checked = true;
  for (const l of checkbox.listeners.change || []) l.fn({ target: checkbox });
  assert.strictEqual(env.get("layoutQueue").disabled, false);
  assert.match(env.get("layoutQueue").textContent, /\(1\)/);
  env.get("layoutQueue").click();
  await env.tick();
  const queued = posts(env, "/layout/queue");
  assert.strictEqual(queued.length, 1);
  assert.deepStrictEqual(JSON.parse(queued[0].init.body), { ids: ["a1"] });
  assert.strictEqual(tray.hidden, true, "the tray closes once the fixes are queued");
  assert.strictEqual(posts(env, "/send").length, 0, "queueing never sends: that stays the user's separate step");

  // Escape closes the tray first; select-all picks every selectable issue.
  env.get("layoutBtn").click();
  assert.strictEqual(tray.hidden, false);
  env.key({ key: "Escape" });
  assert.strictEqual(tray.hidden, true);
  env.get("layoutBtn").click();
  env.get("layoutSelectAll").click();
  assert.match(env.get("layoutQueue").textContent, /\(2\)/);
  env.get("layoutSelectAll").click();
  assert.strictEqual(env.get("layoutQueue").disabled, true, "select all toggles back to none");

  // Queued and closed issues cannot be selected; closed ones do not count.
  env.push(snapshot({ version: 2, layout_warnings: [warning("a1", { status_label: "Waiting for a fix", selectable: false, outstanding: true }), warning("c3", { status_label: "Resolved", active: false, selectable: false })] }));
  await env.tick();
  assert.strictEqual(count.textContent, "1", "only unresolved issues are counted");
  assert.strictEqual(titleOf(list.children[0]).children[0].textContent, "Waiting for a fix", "a non-empty status_label is shown next to the title");
  assert.strictEqual(titleOf(list.children[1]).children[0].textContent, "Resolved");
  assert.strictEqual(list.children.length, 2);
  assert.ok(list.children.every((item) => item.children[0].type !== "checkbox"), "queued and resolved issues have no checkbox");

  // No issues: the badge is gone and the tray says so.
  env.push(snapshot({ version: 3 }));
  await env.tick();
  assert.strictEqual(count.hidden, true);
  assert.strictEqual(env.get("layoutEmpty").hidden, false);

  // An ended session closes the tray, disables the button and stops reporting.
  env.get("layoutBtn").click();
  const before = posts(env, "/layout/diagnostics").length;
  env.push(snapshot({ version: 4, status: "ended", ended_by: "user" }));
  await env.tick();
  assert.strictEqual(tray.hidden, true);
  assert.strictEqual(env.get("layoutBtn").disabled, true);
  await fromFrame(env, pass());
  assert.strictEqual(posts(env, "/layout/diagnostics").length, before, "an ended session ignores passes");

  console.log("ok");
  process.exit(0);
})().catch((err) => {
  console.error(err);
  process.exit(1);
});
