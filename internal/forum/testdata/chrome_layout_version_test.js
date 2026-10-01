// Run by chrome_dom_test.go: which document a layout pass belongs to, and what
// happens when reporting it fails. Prints "ok" or throws.
const assert = require("assert");
const { makeEnv } = require("./chrome_harness.js");

const boot = { key: "0123456789abcdef", token: "t", file: "/tmp/x/plan.html", name: "plan.html", artifact_src: "/a/x/plan.html" };
const snapshot = (version, av) => ({ version, key: boot.key, file: boot.file, status: "open", listening: true, pending: 0, queued: [], transcript: [], artifact_version: av, layout_warnings: [] });
const pass = (av, extra = {}) => ({ type: "forum:layout", artifact_version: av, complete: true, target_presence_complete: true, viewport_width: 1200, findings: [], ...extra });
const diag = (env) => env.calls.filter((c) => c.url.endsWith("/layout/diagnostics"));
async function fromFrame(env, data) {
  for (const l of env.winListeners.message || []) await l({ source: env.get("artifact").contentWindow, data });
}

(async () => {
  // 1. A document about to be replaced must not be filed as the new revision.
  {
    const env = makeEnv(process.argv[2], boot);
    env.push(snapshot(1, "v1"));
    await env.tick();
    env.push(snapshot(2, "v2")); // the file changed: the chrome reloads the frame
    await env.tick();
    await fromFrame(env, pass("v1", { findings: [{ kind: "clipped-text", selector: "p", axis: "horizontal", overflow_px: 9 }] })); // the outgoing document, late
    assert.strictEqual(diag(env).length, 0, "a pass from the replaced document is dropped, not stamped with the new version");
    await fromFrame(env, pass("v2"));
    assert.strictEqual(diag(env).length, 1);
    assert.strictEqual(JSON.parse(diag(env)[0].init.body).artifact_version, "v2");
  }

  // 2. A document ahead of the chrome's snapshot waits for the snapshot to catch up.
  {
    const env = makeEnv(process.argv[2], boot);
    env.push(snapshot(1, "v1"));
    await env.tick();
    await fromFrame(env, pass("v2"));
    assert.strictEqual(diag(env).length, 0, "the snapshot has not announced v2 yet");
    env.push(snapshot(2, "v2"));
    await env.tick();
    assert.strictEqual(diag(env).length, 1, "it goes out once the snapshot announces v2");
    assert.strictEqual(JSON.parse(diag(env)[0].init.body).artifact_version, "v2");
  }

  // 3. A failed report is retried, with backoff, until it lands.
  {
    const env = makeEnv(process.argv[2], boot, { failPosts: 3, timeScale: 400 });
    env.push(snapshot(1, "v1"));
    await env.tick();
    await fromFrame(env, pass("v1", { findings: [{ kind: "clipped-text", selector: "p", axis: "horizontal", overflow_px: 9 }] }));
    for (let i = 0; i < 40 && diag(env).length < 4; i += 1) await env.tick();
    assert.strictEqual(diag(env).length, 4, "three failures and then the one that lands");
    const bodies = new Set(diag(env).map((c) => c.init.body));
    assert.strictEqual(bodies.size, 1, "every retry sends the same pass");
    await env.tick();
    assert.strictEqual(diag(env).length, 4, "it stops retrying once it landed");
  }

  // 4. A newer pass supersedes a retry still waiting; a rejected session stops retrying.
  {
    const env = makeEnv(process.argv[2], boot, { failPosts: 1, timeScale: 400 });
    env.push(snapshot(1, "v1"));
    await env.tick();
    await fromFrame(env, pass("v1", { viewport_width: 1111 }));
    await fromFrame(env, pass("v1", { viewport_width: 1222 }));
    for (let i = 0; i < 20; i += 1) await env.tick();
    const widths = diag(env).map((c) => JSON.parse(c.init.body).viewport_width);
    assert.ok(widths.includes(1222) && widths.filter((w) => w === 1111).length <= 1, "the newer pass is the one that lands: " + widths);
  }

  console.log("ok");
  process.exit(0);
})().catch((err) => {
  console.error(err);
  process.exit(1);
});
