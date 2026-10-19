// vexillum's ask_user tool for Pi soldiers. Embedded in the vx binary and written
// to ~/.vexillum/projects/<key>/pi/ at dispatch, then loaded with `pi -e`; it is
// never added to the camp. Protocol, in the --vx-ask-dir directory:
//   question.json  written here (atomically) before the soldier is shown as blocked
//   answer.json    written by `vx decide`; an answer typed in the pane works too
// Both carry the same random id, so a stale file is never taken for the open question.
import { randomBytes } from "node:crypto";
import { mkdirSync, readFileSync, renameSync, rmSync, watch, writeFileSync } from "node:fs";
import { join } from "node:path";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";

const FLAG = "vx-ask-dir";
const OTHER = "Type my own answer";
const MAX_QUESTION = 4000;
const MAX_OPTIONS = 12;
const MAX_OPTION = 200;
const POLL_MS = 400;

type Details = { id?: string; answer: string | null; option?: number };

const clip = (s: string, n: number): string => (s.length > n ? `${s.slice(0, n - 3)}...` : s);

// Write-then-rename, so vx never reads a half-written file.
function writeAtomic(path: string, data: unknown): void {
  const tmp = `${path}.${randomBytes(4).toString("hex")}.tmp`;
  writeFileSync(tmp, `${JSON.stringify(data)}\n`, { mode: 0o600 });
  renameSync(tmp, path);
}

function readAnswer(dir: string, id: string): { answer: string; option?: number } | undefined {
  try {
    const a = JSON.parse(readFileSync(join(dir, "answer.json"), "utf8"));
    if (a?.version === 1 && a.id === id && typeof a.answer === "string" && a.answer !== "") {
      return { answer: a.answer, option: Number.isInteger(a.option) && a.option > 0 ? a.option : undefined };
    }
  } catch {
    // Absent or being replaced: look again on the next poll.
  }
  return undefined;
}

function clear(dir: string | undefined): void {
  if (!dir) return;
  for (const f of ["question.json", "answer.json"]) rmSync(join(dir, f), { force: true });
}

export default function (pi: ExtensionAPI) {
  pi.registerFlag(FLAG, { description: "Directory vexillum reads questions from and writes answers to", type: "string" });

  // A previous session in the same task may have died with a question open.
  pi.on("session_start", () => {
    const dir = pi.getFlag(FLAG);
    if (typeof dir === "string" && dir) clear(dir);
  });
  pi.on("session_shutdown", () => {
    const dir = pi.getFlag(FLAG);
    if (typeof dir === "string" && dir) clear(dir);
  });

  pi.registerTool({
    name: "ask_user",
    label: "Ask the general",
    description:
      "Ask the general (the human who dispatched you) a question you cannot continue without, and wait for the answer. " +
      "Offer the likely answers as options; the general can always type their own.",
    promptSnippet: "ask_user: ask the general a blocking question with options and wait for the answer",
    promptGuidelines: ["Use ask_user only for a decision you truly cannot make yourself, never for a status update."],
    parameters: Type.Object({
      question: Type.String({ description: "The question, self-contained" }),
      options: Type.Optional(Type.Array(Type.String(), { description: "Likely answers, most recommended first" })),
    }),
    executionMode: "sequential",

    async execute(_id, params, signal, _onUpdate, ctx) {
      if (ctx.mode !== "tui") {
        return {
          content: [{ type: "text", text: "ask_user is not available here (no interactive session). End your turn with a line `needs-decision: <question>` instead." }],
          details: { answer: null } as Details,
        };
      }

      const question = clip(params.question, MAX_QUESTION);
      const options = (params.options ?? []).slice(0, MAX_OPTIONS).map((o) => clip(o, MAX_OPTION));
      const dir = pi.getFlag(FLAG);
      const askDir = typeof dir === "string" && dir ? dir : undefined;
      const id = randomBytes(16).toString("hex");

      const dialog = new AbortController();
      const abort = () => dialog.abort();
      signal?.addEventListener("abort", abort, { once: true });

      let remote: { answer: string; option?: number } | undefined;
      let timer: ReturnType<typeof setInterval> | undefined;
      let watcher: ReturnType<typeof watch> | undefined;
      let blocked = false;
      try {
        if (askDir) {
          mkdirSync(askDir, { recursive: true, mode: 0o700 });
          rmSync(join(askDir, "answer.json"), { force: true });
          writeAtomic(join(askDir, "question.json"), { version: 1, id, question, options, asked_at: new Date().toISOString() });
          const check = () => {
            remote ??= readAnswer(askDir, id);
            if (remote) dialog.abort();
          };
          timer = setInterval(check, POLL_MS);
          try {
            watcher = watch(askDir, check);
          } catch {
            // Polling alone is enough.
          }
        }

        // The stock herdr integration reports "blocked" only for this event.
        pi.events.emit("herdr:blocked", { active: true, label: `ask_user: ${clip(question, 60)}` });
        blocked = true;

        const opts = { signal: dialog.signal };
        let picked = options.length > 0 ? await ctx.ui.select(question, [...options, OTHER], opts) : OTHER;
        if (picked === OTHER && !remote) picked = await ctx.ui.input(question, "Your answer", opts);

        const given = remote ? remote.answer : picked;
        if (!given || (!remote && signal?.aborted)) {
          return { content: [{ type: "text", text: "The general did not answer. Continue without it, or end your turn with `needs-decision:`." }], details: { id, answer: null } as Details };
        }
        const option = remote ? remote.option : options.indexOf(given) + 1 || undefined;
        const text = option ? `The general chose option ${option}: ${given}` : `The general answered: ${given}`;
        return { content: [{ type: "text", text }], details: { id, answer: given, option } as Details };
      } finally {
        if (timer) clearInterval(timer);
        watcher?.close();
        signal?.removeEventListener("abort", abort);
        // Cleared only now, so vx sees the question as open until it is consumed.
        clear(askDir);
        if (blocked) pi.events.emit("herdr:blocked", { active: false });
      }
    },
  });
}
