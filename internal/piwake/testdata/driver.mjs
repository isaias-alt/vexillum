// Drives the embedded wake extension with a stand-in for Pi's extension API
// (no model, no Pi) and a fake `vx` on PATH whose behavior each test scripts.
// Run by TestExtensionUnderNode with node; exits non-zero on the first failed
// assertion. The timings come in through VEXILLUM_WAKE_TUNING (see the test).
import assert from "node:assert/strict";
import { chmodSync, mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const [extPath, base] = process.argv.slice(2);
const T = JSON.parse(process.env.VEXILLUM_WAKE_TUNING);
const mod = await import(extPath);

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const until = async (cond, what, ms = 5000) => {
  for (let waited = 0; waited < ms; waited += 10) {
    if (cond()) return;
    await sleep(10);
  }
  throw new Error(`timed out waiting for ${what}`);
};

// The fake vx: runs plan.<n> (n = how many times it has been started, from 1)
// under sh, or blocks like a real await when there is no such plan. It records
// who started it and where.
const bin = join(base, "bin");
const state = join(base, "fakevx");
mkdirSync(bin, { recursive: true });
mkdirSync(state, { recursive: true });
writeFileSync(
  join(bin, "vx"),
  `#!/bin/sh
d="${state}"
n=$(cat "$d/count" 2>/dev/null || echo 0)
n=$((n+1))
echo "$PPID $$ $(pwd)" >> "$d/starts"
echo $n > "$d/count"
echo "$@" > "$d/args.$n"
[ -f "$d/plan.$n" ] && . "$d/plan.$n"
exec sleep 30
`,
);
chmodSync(join(bin, "vx"), 0o755);
process.env.PATH = `${bin}:${process.env.PATH}`;
process.env.HERDR_WORKSPACE_ID = "w-test";
const home = join(base, "home");
mkdirSync(home, { recursive: true });
process.env.HOME = home;
const cwd = join(base, "project");
mkdirSync(cwd, { recursive: true });

const count = () => Number(readFileSync(join(state, "count"), "utf8").trim() || 0);
const starts = () => readFileSync(join(state, "starts"), "utf8").trim().split("\n").map((l) => l.split(" "));
const plan = (n, body) => writeFileSync(join(state, `plan.${n}`), body);
const reset = () => {
  for (const f of readdirSync(state)) rmSync(join(state, f));
  for (const f of ["count", "starts"]) writeFileSync(join(state, f), "");
};

// A fresh extension runtime against a mock pi. idle and failures are scripted.
function load({ idle = () => true, sendFails = () => false, mode = "tui", where = cwd } = {}) {
  const handlers = {};
  const sent = [];
  const notices = [];
  const pi = {
    on: (name, fn) => (handlers[name] = fn),
    sendUserMessage(text, opts) {
      if (sendFails()) throw new Error("the agent is busy");
      sent.push({ text, opts });
    },
  };
  mod.default(pi);
  const ctx = { mode, cwd: where, isIdle: () => idle(), ui: { notify: (m, t) => notices.push([m, t]) } };
  return {
    sent,
    notices,
    start: () => handlers.session_start({ type: "session_start", reason: "startup" }, ctx),
    shutdown: (reason = "quit") => handlers.session_shutdown({ type: "session_shutdown", reason }),
  };
}

// 1. A wake while Pi is idle: delivered as a plain user message, the await is
// started again at once, and it is a direct child of this process (so its
// owner tracking sees Pi go away).
{
  reset();
  plan(1, `echo "A vexillum soldier's status changed" >&2\nexit 2\n`);
  const x = load();
  x.start();
  await until(() => x.sent.length === 1, "the idle wake");
  assert.deepEqual(x.sent[0], { text: "A vexillum soldier's status changed", opts: undefined });
  await until(() => count() >= 2, "the await to start again");
  const [ppid, , dir] = starts()[0];
  assert.equal(Number(ppid), process.pid, "the await must be a direct child of the Pi process");
  assert.equal(dir.replace(/^\/private/, ""), cwd.replace(/^\/private/, ""), "the await runs in the project folder");
  assert.equal(readFileSync(join(state, "args.1"), "utf8").trim(), "sentinel await");
  x.shutdown();
  assert.equal(x.sent.length, 1);
}

// 2. A wake while Pi is mid-turn goes in as a follow-up.
{
  reset();
  plan(1, `echo busy-wake >&2\nexit 2\n`);
  const x = load({ idle: () => false });
  x.start();
  await until(() => x.sent.length === 1, "the busy wake");
  assert.deepEqual(x.sent[0], { text: "busy-wake", opts: { deliverAs: "followUp" } });
  x.shutdown();
}

// 3. Pi cannot take the message now (it throws): the wake is kept and retried,
// never dropped, and wakes that pile up meanwhile go in together, in order.
{
  reset();
  let failing = true;
  plan(1, `echo first >&2\nexit 2\n`);
  plan(2, `echo second >&2\nexit 2\n`);
  const x = load({ sendFails: () => failing });
  x.start();
  await until(() => count() >= 3, "both wakes to have been consumed");
  await sleep(3 * T.deliverRetryMs);
  assert.equal(x.sent.length, 0, "nothing can be delivered while Pi refuses");
  failing = false;
  await until(() => x.sent.length === 1, "the held wakes");
  assert.equal(x.sent[0].text, "first\n\nsecond");
  await sleep(3 * T.deliverRetryMs);
  assert.equal(x.sent.length, 1, "delivered exactly once");
  x.shutdown();
}

// 4. The session is replaced while a wake is held: the next runtime gets it. On
// quit nothing is handed over.
{
  reset();
  plan(1, `echo held >&2\nexit 2\n`);
  const a = load({ sendFails: () => true });
  a.start();
  await until(() => count() >= 2, "the wake to be consumed");
  a.shutdown("new");
  const b = load();
  b.start();
  await until(() => b.sent.length === 1, "the handed over wake");
  assert.equal(b.sent[0].text, "held");
  b.shutdown();

  reset();
  plan(1, `echo lost-on-quit >&2\nexit 2\n`);
  const c = load({ sendFails: () => true });
  c.start();
  await until(() => count() >= 2, "the wake to be consumed");
  c.shutdown("quit");
  const d = load();
  d.start();
  await sleep(3 * T.deliverRetryMs);
  assert.equal(d.sent.length, 0, "a quit hands nothing over");
  d.shutdown();
}

// 5. An await that comes back at once with exit 0 (a camp, no project) is not
// respawned in a loop: it is tried quietLimit times with a growing pause, then
// left alone, and nothing is said.
{
  reset();
  for (const n of [1, 2, 3, 4, 5]) plan(n, `exit 0\n`);
  const x = load();
  x.start();
  const t0 = Date.now();
  await until(() => count() >= T.quietLimit, "the tries");
  const took = Date.now() - t0;
  assert.ok(took >= T.quietRetryMs[0], `the second try waited for the pause (took ${took}ms)`);
  await sleep(T.quietRetryMs[1] + 300);
  assert.equal(count(), T.quietLimit, "gave up after the limit");
  assert.deepEqual(x.notices, []);
  x.shutdown();
}

// 6. An await that ran for a while and ended with exit 0 (the 55 minute timeout,
// or superseded) is started again at once, without the pause.
{
  reset();
  plan(1, `sleep ${(T.fastMs + 200) / 1000}\nexit 0\n`);
  plan(2, `exit 0\n`);
  const x = load();
  x.start();
  await until(() => count() >= 1, "the first await");
  const t0 = Date.now();
  await until(() => count() >= 2, "the second await");
  const gap = Date.now() - t0 - (T.fastMs + 200);
  assert.ok(gap < T.quietRetryMs[0], `restarted at once, not after the pause (gap ${gap}ms)`);
  x.shutdown();
}

// 7. Exit 0 at once with something on stderr (vx could not be found): the
// general is told once, and it is not retried.
{
  reset();
  plan(1, `echo "vx: not found, so the wake cannot work" >&2\nexit 0\n`);
  const x = load();
  x.start();
  await until(() => x.notices.length === 1, "the notice");
  assert.match(x.notices[0][0], /vx: not found/);
  assert.equal(x.notices[0][1], "warning");
  await sleep(T.quietRetryMs[0] + 200);
  assert.equal(count(), 1);
  x.shutdown();
}

// 8. A failing await (another exit code) is retried with a pause, and the
// general is told once.
{
  reset();
  plan(1, `echo boom >&2\nexit 1\n`);
  plan(2, `exit 1\n`);
  plan(3, `echo after-failures >&2\nexit 2\n`);
  const x = load();
  x.start();
  await until(() => x.sent.length === 1, "the wake after the failures");
  assert.equal(x.sent[0].text, "after-failures");
  assert.equal(x.notices.length, 1, "told once");
  assert.match(x.notices[0][0], /code 1: boom/);
  x.shutdown();
}

// 9. Where it must do nothing: a soldier's camp, a Pi started with the soldier
// flag, outside a herdr pane, and a session that is not the interactive one.
{
  const noops = [
    ["a camp", { where: join(home, ".vexillum", "projects", "p-1", "camps", "1", "p") }],
    ["a soldier", {}, () => process.argv.push("--vx-ask-dir", "/x"), () => process.argv.splice(-2)],
    ["no herdr pane", {}, () => delete process.env.HERDR_WORKSPACE_ID, () => (process.env.HERDR_WORKSPACE_ID = "w-test")],
    ["print mode", { mode: "print" }],
  ];
  for (const [what, opts, before = () => {}, after = () => {}] of noops) {
    reset();
    before();
    const x = load(opts);
    x.start();
    await sleep(300);
    assert.equal(count(), 0, `${what}: no await may be started`);
    assert.deepEqual(x.notices, [], `${what}: nothing is said`);
    x.shutdown();
    after();
  }
}

// 10. Shutting down ends the await: it does not outlive the session.
{
  reset();
  const x = load();
  x.start();
  await until(() => count() === 1, "the await");
  const pid = Number(starts()[0][1]);
  process.kill(pid, 0); // alive
  x.shutdown();
  await until(() => {
    try {
      process.kill(pid, 0);
      return false;
    } catch {
      return true;
    }
  }, "the await to end");
  await sleep(100);
  assert.equal(count(), 1, "and it is not started again");
}

console.log("ok");
process.exit(0);
