// Drives the embedded OSC 7501 plugin with a stand-in for OpenCode's TUI plugin
// API (no OpenCode, no model, no terminal). Run by TestPluginUnderNode with
// node; exits non-zero on the first failed assertion.
//
// The plugin writes to /dev/tty and falls back to stdout. Both paths are
// replaced here (fs.openSync, fs.writeSync, process.stdout.write on the fs
// module object the plugin imports), so a test run from a terminal never
// prints the sequences there.
import assert from "node:assert/strict";
import fs from "node:fs";

const [pluginPath] = process.argv.slice(2);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const osc = (body) => `\x1b]7501;${body}\x07`;

// Everything the plugin writes, in order, whichever way it got out.
let out = [];
const realOpen = fs.openSync;
const realWrite = fs.writeSync;
let ttyOK = false;
fs.openSync = (path, ...rest) => {
  if (path === "/dev/tty") {
    if (!ttyOK) throw Object.assign(new Error("ENXIO: no such device or address"), { code: "ENXIO" });
    return 9999;
  }
  return realOpen(path, ...rest);
};
fs.writeSync = (fd, data, ...rest) => (fd === 9999 ? void out.push(String(data)) : realWrite(fd, data, ...rest));
const realStdout = process.stdout.write.bind(process.stdout);
process.stdout.write = (data) => void out.push(String(data));

// A stand-in for api: sessions maps id -> {status, permissions, forms, parentID}.
function stubApi() {
  const sessions = new Map();
  const listeners = [];
  let route = { type: "home" };
  const get = (id) => sessions.get(id) ?? { status: "idle", permissions: [], forms: [] };
  return {
    sessions,
    listeners,
    goto: (id) => (route = id ? { type: "session", sessionID: id } : { type: "home" }),
    emit: (type, data) => listeners.forEach((fn) => fn({ details: { type, data } })),
    api: {
      ui: { router: { current: () => route } },
      data: {
        listen: (fn) => (listeners.push(fn), () => listeners.splice(listeners.indexOf(fn), 1)),
        session: {
          status: (id) => get(id).status,
          permission: { list: (id) => get(id).permissions },
          form: { list: (id) => get(id).forms },
          family: (id) => [...sessions].filter(([, s]) => s.parentID === id).map(([k]) => k),
          get: (id) => sessions.get(id),
        },
      },
    },
  };
}

let n = 0;
async function load(env, tty = false) {
  ttyOK = tty;
  out = [];
  for (const k of ["HC_TERMINAL_ID", "VX_OSC_LOG"]) delete process.env[k];
  Object.assign(process.env, env);
  const stub = stubApi();
  // A fresh module instance each time: the plugin caches its tty descriptor.
  const mod = await import(`${pluginPath}?run=${++n}`);
  assert.equal(mod.default.id, "vx.osc7501");
  const dispose = mod.default.setup(stub.api);
  return { stub, dispose };
}
const sent = () => out.join("");

// 1. Not an hc terminal: silent, and there is nothing to dispose.
{
  const { stub, dispose } = await load({});
  stub.sessions.set("ses_a", { status: "running", permissions: [], forms: [] });
  stub.goto("ses_a");
  stub.emit("session.execution.started", { sessionID: "ses_a" });
  await sleep(600);
  assert.equal(dispose, undefined);
  assert.equal(sent(), "", "wrote something outside an hc terminal");
}

// 2. The whole life of a session, through the stdout fallback.
{
  const { stub, dispose } = await load({ HC_TERMINAL_ID: "t1" });
  assert.equal(sent(), osc("app=opencode:state=idle"), "boot reports idle at once");

  // Home route, no session yet: still idle, and not repeated.
  await sleep(600);
  assert.equal(sent(), osc("app=opencode:state=idle"), "an unchanged state is not repeated");

  // Opening a session that has not worked: idle.
  stub.sessions.set("ses_a", { status: "idle", permissions: [], forms: [] });
  stub.goto("ses_a");
  await sleep(400);
  assert.equal(sent(), osc("app=opencode:state=idle"));

  // Working.
  stub.sessions.get("ses_a").status = "running";
  stub.emit("session.execution.started", { sessionID: "ses_a" });
  assert.ok(sent().endsWith(osc("app=opencode:state=working")), `no working: ${JSON.stringify(sent())}`);

  // A permission request that is answered at once (--auto) never shows.
  stub.sessions.get("ses_a").permissions = [{ id: "p1" }];
  await sleep(300);
  stub.sessions.get("ses_a").permissions = [];
  await sleep(600);
  assert.ok(!sent().includes("blocked"), "a sub-second permission request was reported as blocked");

  // One that stays is reported, after the grace and not before.
  const before = sent();
  stub.sessions.get("ses_a").permissions = [{ id: "p2" }];
  stub.emit("permission.asked", { sessionID: "ses_a" });
  await sleep(300);
  assert.equal(sent(), before, "blocked was reported before the grace period");
  await sleep(700);
  assert.ok(sent().endsWith(osc("app=opencode:state=blocked:kind=permission")), `no blocked permission: ${JSON.stringify(sent())}`);

  // Answered: back to working, then a question form.
  stub.sessions.get("ses_a").permissions = [];
  stub.emit("permission.replied", { sessionID: "ses_a" });
  assert.ok(sent().endsWith(osc("app=opencode:state=working")));
  stub.sessions.get("ses_a").forms = [{ id: "f1" }];
  await sleep(1000);
  assert.ok(sent().endsWith(osc("app=opencode:state=blocked:kind=question")), `no blocked question: ${JSON.stringify(sent())}`);

  // Answered and finished: done.
  stub.sessions.get("ses_a").forms = [];
  stub.sessions.get("ses_a").status = "idle";
  stub.emit("form.replied", { sessionID: "ses_a" });
  assert.ok(sent().endsWith(osc("app=opencode:state=done")), `no done: ${JSON.stringify(sent())}`);

  // A failed execution is an error until the session works again.
  stub.sessions.get("ses_a").status = "running";
  stub.emit("session.execution.started", { sessionID: "ses_a" });
  stub.sessions.get("ses_a").status = "idle";
  stub.emit("session.execution.failed", { sessionID: "ses_a", error: { message: "boom boom" } });
  assert.ok(sent().endsWith(osc("app=opencode:state=error")), `no error: ${JSON.stringify(sent())}`);
  stub.sessions.get("ses_a").status = "running";
  stub.emit("session.execution.started", { sessionID: "ses_a" });
  assert.ok(sent().endsWith(osc("app=opencode:state=working")));
  stub.sessions.get("ses_a").status = "idle";
  stub.emit("session.execution.succeeded", { sessionID: "ses_a" });
  assert.ok(sent().endsWith(osc("app=opencode:state=done")));

  // Only app, state and kind are ever sent: msg= makes hc 0.1.16 drop the record.
  for (const m of sent().matchAll(/\x1b\]7501;([^\x07]*)\x07/g)) {
    assert.match(m[1], /^app=opencode:state=(idle|working|blocked|done|error)(:kind=(permission|question))?$/, `unexpected record ${m[1]}`);
  }
  dispose();
}

// 3. Another soldier's session, on the same OpenCode service, is not this pane's.
{
  const { stub, dispose } = await load({ HC_TERMINAL_ID: "t2" });
  stub.sessions.set("ses_mine", { status: "idle", permissions: [], forms: [] });
  stub.sessions.set("ses_other", { status: "running", permissions: [{ id: "p" }], forms: [] });
  stub.goto("ses_mine");
  await sleep(1000);
  stub.emit("session.execution.started", { sessionID: "ses_other" });
  stub.emit("session.execution.failed", { sessionID: "ses_other", error: { message: "x" } });
  await sleep(300);
  assert.equal(sent(), osc("app=opencode:state=idle"), `a foreign session leaked into this pane: ${JSON.stringify(sent())}`);
  dispose();
}

// 4. A child session counts for its parent (the family), a foreign child does not.
{
  const { stub, dispose } = await load({ HC_TERMINAL_ID: "t3" });
  stub.sessions.set("ses_parent", { status: "idle", permissions: [], forms: [] });
  stub.goto("ses_parent");
  await sleep(300);
  stub.emit("session.created", { sessionID: "ses_child", parentID: "ses_parent" });
  stub.sessions.set("ses_child", { status: "running", permissions: [], forms: [], parentID: "ses_parent" });
  stub.emit("session.execution.started", { sessionID: "ses_child" });
  assert.ok(sent().endsWith(osc("app=opencode:state=working")), `a child session was not counted: ${JSON.stringify(sent())}`);
  // Viewing the child reports the parent's family, not a new session.
  stub.goto("ses_child");
  await sleep(300);
  assert.ok(sent().endsWith(osc("app=opencode:state=working")));
  dispose();
}

// 5. With a controlling terminal the sequences go there and not to stdout.
{
  const { stub, dispose } = await load({ HC_TERMINAL_ID: "t4" }, true);
  assert.equal(sent(), osc("app=opencode:state=idle"));
  dispose();
}

// 6. The cleanup stops the poll: nothing is written after it.
{
  const { stub, dispose } = await load({ HC_TERMINAL_ID: "t5" });
  stub.sessions.set("ses_a", { status: "idle", permissions: [], forms: [] });
  stub.goto("ses_a");
  dispose();
  const after = sent();
  stub.sessions.get("ses_a").status = "running";
  await sleep(600);
  assert.equal(sent(), after, "the plugin kept reporting after its cleanup");
}

realStdout("ok\n");
process.exit(0);
