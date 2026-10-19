// vexillum's wake for a Pi commander. Embedded in the vx binary and written to
// .pi/extensions/vexillum-wake.ts by `vx init` (kept current by `vx upgrade`);
// it is committed with the project, so every checkout has it, soldier camps
// included, where it does nothing.
//
// The sentinel records a wake when a soldier finishes or forum feedback
// arrives; `vx sentinel await` blocks until one is pending, prints it on stderr
// and exits 2. This extension keeps one await running as a child of Pi and, when
// it exits 2, hands the text to the model as a user message (pi.sendUserMessage),
// the way the Claude Code Stop hook wakes a Claude commander. It never types
// into the editor, so whatever the general has written but not sent stays put.
//
// Things it takes care of:
//   - a wake the await already consumed is never dropped: if Pi cannot take the
//     message right now it is kept and retried, and handed to the next runtime
//     when the session is replaced (/new, /resume, /fork, /reload);
//   - a busy Pi gets the message as a follow-up, delivered when the turn ends;
//   - an await that returns at once with exit 0 (a soldier camp, a folder that is
//     not a vexillum project, no herdr pane) is not respawned in a loop: it is
//     retried a couple of times with a growing pause and then left alone until
//     the next session start;
//   - the await gives up quietly after 55 minutes with exit 0; that is a normal
//     end and it is started again at once.
import { spawn, type ChildProcess } from "node:child_process";
import { realpathSync } from "node:fs";
import { homedir } from "node:os";
import { join, sep } from "node:path";
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";

// The shell script vexillum's Claude Code Stop hook runs: it finds vx on PATH or
// in its usual install directories and execs `vx sentinel await`, or prints why
// it could not and exits 0. vx fills this in when it writes the file.
const AWAIT_SCRIPT = "__VX_AWAIT_SCRIPT__";

const SOLDIER_FLAG = "--vx-ask-dir";
const MAX_STDERR = 64 * 1024;
const MAX_PENDING = 50;
const HANDOFF = Symbol.for("vexillum.pi-wake.handoff");

type Tuning = {
  fastMs: number; // an await that ends sooner than this "returned at once"
  quietLimit: number; // consecutive fast exit 0 before leaving it alone
  quietRetryMs: number[]; // pause before the next try after a fast exit 0
  failRetryMs: number[]; // pause after a failed await (other exit code, no sh), last one repeats
  deliverRetryMs: number; // pause between tries to hand a wake to Pi
};

const TUNING: Tuning = {
  fastMs: 10_000,
  quietLimit: 3,
  quietRetryMs: [5_000, 20_000],
  failRetryMs: [5_000, 15_000, 60_000, 300_000],
  deliverRetryMs: 1_000,
};

// A test hook: a JSON object overriding the numbers above so the tests do not
// have to wait them out. Nothing sets it outside the tests.
try {
  Object.assign(TUNING, JSON.parse(process.env.VEXILLUM_WAKE_TUNING ?? "{}"));
} catch {
  // Keep the defaults.
}

const real = (p: string): string => {
  try {
    return realpathSync(p);
  } catch {
    return p;
  }
};

// Camps live under ~/.vexillum; a Pi soldier is also launched with --vx-ask-dir.
// Neither is a commander, and `vx sentinel await` would exit 0 there anyway.
function notCommander(ctx: ExtensionContext): string | undefined {
  if (ctx.mode !== "tui") return "not an interactive session";
  if (!process.env.HERDR_WORKSPACE_ID) return "not inside a herdr pane";
  if (process.argv.includes(SOLDIER_FLAG)) return "a soldier";
  const home = real(join(homedir(), ".vexillum"));
  const cwd = real(ctx.cwd);
  if (cwd === home || cwd.startsWith(home + sep)) return "a camp";
  return undefined;
}

export default function (pi: ExtensionAPI) {
  let generation = 0;
  let ctx: ExtensionContext | undefined;
  let child: ChildProcess | undefined;
  let respawnTimer: ReturnType<typeof setTimeout> | undefined;
  let flushTimer: ReturnType<typeof setTimeout> | undefined;
  let pending: string[] = [];
  let reported = false;

  const stop = (): void => {
    generation++;
    if (respawnTimer) clearTimeout(respawnTimer);
    if (flushTimer) clearTimeout(flushTimer);
    respawnTimer = flushTimer = undefined;
    // SIGTERM makes the await end quietly with exit 0; it must not outlive
    // its session.
    child?.kill("SIGTERM");
    child = undefined;
    ctx = undefined;
  };

  // Tell the general once per session why the wake is not working; more would
  // only repeat it.
  const report = (message: string): void => {
    if (reported) return;
    reported = true;
    try {
      ctx?.ui.notify(`vexillum wake: ${message}`, "warning");
    } catch {
      // The context is gone; nothing to tell.
    }
  };

  const deliver = (text: string): boolean => {
    try {
      if (!ctx) return false;
      if (ctx.isIdle()) pi.sendUserMessage(text);
      else pi.sendUserMessage(text, { deliverAs: "followUp" });
      return true;
    } catch {
      return false;
    }
  };

  // Hand every held wake to Pi in one message; keep them all and try again
  // shortly when Pi cannot take it now.
  const flush = (): void => {
    if (flushTimer) clearTimeout(flushTimer);
    flushTimer = undefined;
    if (pending.length === 0) return;
    if (deliver(pending.join("\n\n"))) {
      pending = [];
      return;
    }
    const gen = generation;
    flushTimer = setTimeout(() => {
      if (gen === generation) flush();
    }, TUNING.deliverRetryMs);
  };

  const hold = (text: string): void => {
    pending.push(text);
    if (pending.length > MAX_PENDING) pending = pending.slice(-MAX_PENDING);
    flush();
  };

  const respawnIn = (gen: number, ms: number, quiet: number, fail: number): void => {
    respawnTimer = setTimeout(() => {
      if (gen === generation) run(gen, quiet, fail);
    }, ms);
  };

  const run = (gen: number, quiet: number, fail: number): void => {
    const startedAt = Date.now();
    let stderr = "";
    let ended = false;
    const end = (code: number | null, signal: NodeJS.Signals | null, why?: string): void => {
      if (ended) return;
      ended = true;
      if (gen !== generation) return;
      child = undefined;
      const ranMs = Date.now() - startedAt;
      const text = stderr.trim();

      if (code === 2) {
        if (text) hold(text);
        respawnIn(gen, 0, 0, 0);
      } else if (code === 0 && ranMs >= TUNING.fastMs) {
        // The 55 minute timeout, or a newer await took over: start again.
        respawnIn(gen, 0, 0, 0);
      } else if (code === 0) {
        // Back at once: a camp, a folder that is no project, vx not found.
        if (text) {
          report(text.split("\n")[0]);
          return;
        }
        quiet++;
        if (quiet >= TUNING.quietLimit) return;
        respawnIn(gen, TUNING.quietRetryMs[Math.min(quiet - 1, TUNING.quietRetryMs.length - 1)], quiet, fail);
      } else {
        report(why ?? `vx sentinel await ended with ${signal ?? `code ${code}`}${text ? `: ${text.split("\n")[0]}` : ""}`);
        const pause = TUNING.failRetryMs[Math.min(fail, TUNING.failRetryMs.length - 1)];
        respawnIn(gen, pause, quiet, fail + 1);
      }
    };

    try {
      child = spawn("sh", ["-c", AWAIT_SCRIPT], { stdio: ["ignore", "ignore", "pipe"], cwd: ctx?.cwd, env: process.env });
    } catch (e) {
      end(null, null, `could not start the sentinel await: ${(e as Error).message}`);
      return;
    }
    child.unref();
    child.stderr?.setEncoding("utf8");
    child.stderr?.on("data", (d: string) => {
      if (stderr.length < MAX_STDERR) stderr += d;
    });
    child.on("error", (e) => end(null, null, `could not start the sentinel await: ${e.message}`));
    child.on("close", (code, signal) => end(code, signal));
  };

  pi.on("session_start", (_event, c) => {
    stop();
    reported = false;
    if (notCommander(c)) return;
    ctx = c;
    // Wakes the previous runtime held when the session was replaced.
    const held = (globalThis as Record<symbol, unknown>)[HANDOFF];
    delete (globalThis as Record<symbol, unknown>)[HANDOFF];
    if (Array.isArray(held)) pending = held.filter((t): t is string => typeof t === "string");
    flush();
    run(generation, 0, 0);
  });

  pi.on("session_shutdown", (event) => {
    if (pending.length > 0 && event.reason !== "quit") {
      (globalThis as Record<symbol, unknown>)[HANDOFF] = pending;
    }
    pending = [];
    stop();
  });
}
