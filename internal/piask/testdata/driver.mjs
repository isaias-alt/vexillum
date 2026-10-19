// Drives the embedded ask_user extension with a stand-in for Pi's extension API
// (no model, no Pi). Run by TestExtensionUnderNode with node; exits non-zero on
// the first failed assertion.
import assert from "node:assert/strict";
import { existsSync, mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const [extPath, base] = process.argv.slice(2);
const mod = await import(extPath);

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const until = async (cond, what) => {
  for (let i = 0; i < 100; i++) {
    if (cond()) return;
    await sleep(50);
  }
  throw new Error(`timed out waiting for ${what}`);
};
const put = (dir, name, obj) => {
  mkdirSync(dir, { recursive: true });
  const tmp = join(dir, `${name}.tmp`);
  writeFileSync(tmp, JSON.stringify(obj));
  renameSync(tmp, join(dir, name));
};

// A fresh extension runtime against a mock pi. ui.select/input are scripted.
function load(flag, ui) {
  const handlers = {};
  const events = [];
  let tool;
  const pi = {
    registerFlag() {},
    getFlag: () => flag,
    on: (name, fn) => (handlers[name] = fn),
    registerTool: (t) => (tool = t),
    events: { emit: (channel, data) => events.push([channel, data]) },
  };
  mod.default(pi);
  return { handlers, events, tool, ctx: { mode: "tui", ui } };
}

// A dialog that stays up until aborted, like Pi's own.
const waitingDialog = (log) => (title, optionsOrPlaceholder, opts) =>
  new Promise((resolve) => {
    log.push(title);
    opts.signal.addEventListener("abort", () => resolve(undefined), { once: true });
  });

// 1. vx answers while the dialog is up: it is closed, the answer is returned, files are cleared.
{
  const dir = join(base, "remote");
  const shown = [];
  const x = load(dir, { select: waitingDialog(shown), input: waitingDialog(shown) });
  const run = x.tool.execute("c1", { question: "Which db?", options: ["pg", "sqlite"] }, undefined, undefined, x.ctx);
  await until(() => existsSync(join(dir, "question.json")), "question.json");
  const q = JSON.parse(readFileSync(join(dir, "question.json"), "utf8"));
  assert.equal(q.version, 1);
  assert.match(q.id, /^[0-9a-f]{32}$/);
  assert.equal(q.question, "Which db?");
  assert.deepEqual(q.options, ["pg", "sqlite"]);
  assert.deepEqual(x.events.map((e) => e[1].active), [true], "blocked is reported while waiting");
  assert.equal(x.events[0][0], "herdr:blocked");

  put(dir, "answer.json", { version: 1, id: "f".repeat(32), answer: "WRONG" });
  await sleep(700);
  assert.equal(existsSync(join(dir, "question.json")), true, "an answer for another id must be ignored");

  put(dir, "answer.json", { version: 1, id: q.id, answer: "sqlite", option: 2 });
  const result = await run;
  assert.equal(result.content[0].text, "The general chose option 2: sqlite");
  assert.equal(result.details.answer, "sqlite");
  assert.equal(existsSync(join(dir, "question.json")), false);
  assert.equal(existsSync(join(dir, "answer.json")), false);
  assert.deepEqual(x.events.map((e) => e[1].active), [true, false], "blocked is cleared after");
}

// 2. Free text from vx, no option number.
{
  const dir = join(base, "text");
  const x = load(dir, { select: waitingDialog([]), input: waitingDialog([]) });
  const run = x.tool.execute("c", { question: "Name it" }, undefined, undefined, x.ctx);
  await until(() => existsSync(join(dir, "question.json")), "question.json");
  const q = JSON.parse(readFileSync(join(dir, "question.json"), "utf8"));
  assert.deepEqual(q.options, []);
  put(dir, "answer.json", { version: 1, id: q.id, answer: "Call it vexillum" });
  const result = await run;
  assert.equal(result.content[0].text, "The general answered: Call it vexillum");
}

// 3. Answered in the pane first: the option is returned and the question closes.
{
  const dir = join(base, "pane");
  const x = load(dir, { select: async () => "pg", input: async () => assert.fail("no input expected") });
  const result = await x.tool.execute("c", { question: "Which db?", options: ["pg", "sqlite"] }, undefined, undefined, x.ctx);
  assert.equal(result.content[0].text, "The general chose option 1: pg");
  assert.equal(existsSync(join(dir, "question.json")), false);
  assert.deepEqual(x.events.map((e) => e[1].active), [true, false]);
}

// 4. "Type my own answer" in the pane leads to the input dialog.
{
  const dir = join(base, "own");
  const x = load(dir, { select: async () => "Type my own answer", input: async () => "mine" });
  const result = await x.tool.execute("c", { question: "q", options: ["a"] }, undefined, undefined, x.ctx);
  assert.equal(result.content[0].text, "The general answered: mine");
}

// 5. Escape in the dialog: no answer, nothing left behind, blocked cleared.
{
  const dir = join(base, "esc");
  const x = load(dir, { select: async () => undefined, input: async () => undefined });
  const result = await x.tool.execute("c", { question: "q", options: ["a"] }, undefined, undefined, x.ctx);
  assert.match(result.content[0].text, /did not answer/);
  assert.equal(existsSync(join(dir, "question.json")), false);
  assert.deepEqual(x.events.map((e) => e[1].active), [true, false]);
}

// 6. The agent run is aborted while waiting.
{
  const dir = join(base, "abort");
  const x = load(dir, { select: waitingDialog([]), input: waitingDialog([]) });
  const ac = new AbortController();
  const run = x.tool.execute("c", { question: "q", options: ["a"] }, ac.signal, undefined, x.ctx);
  await until(() => existsSync(join(dir, "question.json")), "question.json");
  ac.abort();
  const result = await run;
  assert.match(result.content[0].text, /did not answer/);
  assert.equal(existsSync(join(dir, "question.json")), false);
}

// 7. A question and an answer left by a dead session are cleared at session start and shutdown.
{
  const dir = join(base, "stale");
  put(dir, "question.json", { version: 1, id: "a".repeat(32), question: "old" });
  put(dir, "answer.json", { version: 1, id: "a".repeat(32), answer: "old" });
  const x = load(dir, {});
  x.handlers.session_start();
  assert.equal(existsSync(join(dir, "question.json")), false);
  assert.equal(existsSync(join(dir, "answer.json")), false);
  put(dir, "question.json", { version: 1, id: "b".repeat(32), question: "open" });
  x.handlers.session_shutdown();
  assert.equal(existsSync(join(dir, "question.json")), false);
}

// 8. Without the flag it still asks in the pane and records nothing.
{
  const x = load(undefined, { select: async () => "a", input: async () => "x" });
  const result = await x.tool.execute("c", { question: "q", options: ["a"] }, undefined, undefined, x.ctx);
  assert.equal(result.content[0].text, "The general chose option 1: a");
}

// 9. Outside the TUI there is nobody to ask: it says how to ask instead.
{
  const x = load(join(base, "print"), {});
  x.ctx.mode = "print";
  const result = await x.tool.execute("c", { question: "q" }, undefined, undefined, x.ctx);
  assert.match(result.content[0].text, /needs-decision/);
  assert.equal(existsSync(join(base, "print")), false);
}

console.log("ok");
