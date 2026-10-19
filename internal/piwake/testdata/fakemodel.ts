// A scripted model for Pi, loaded with -e: it answers "ack <n>" to everything
// (slowly when the last user message says SLOW) and logs every call, so a test
// can read back what the model was sent and when. Nothing here touches a
// network. FAKE_LOG is the JSONL file; FAKE_SLOW_MS how long a SLOW turn takes.
import { createAssistantMessageEventStream } from "@earendil-works/pi-ai";
import { appendFileSync } from "node:fs";

let calls = 0;
const text = (m: any): string =>
  typeof m.content === "string" ? m.content : (m.content ?? []).map((c: any) => (c.type === "text" ? c.text : "")).join("");
const log = (o: object): void => appendFileSync(process.env.FAKE_LOG!, JSON.stringify({ ...o, t: Date.now() }) + "\n");

export default function (pi: any) {
  pi.registerProvider("fakevx", {
    baseUrl: "http://127.0.0.1:1",
    apiKey: "x",
    api: "fakevx-api",
    models: [{ id: "scripted", name: "Scripted", reasoning: false, input: ["text"], cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 }, contextWindow: 100000, maxTokens: 4000 }],
    streamSimple(model: any, context: any) {
      const call = ++calls;
      const users = context.messages.filter((m: any) => m.role === "user").map(text);
      log({ call, event: "start", users });
      const slow = users[users.length - 1]?.includes("SLOW");
      const out: any = {
        role: "assistant", content: [], api: model.api, provider: model.provider, model: model.id,
        usage: { input: 1, output: 1, cacheRead: 0, cacheWrite: 0, totalTokens: 2, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } },
        stopReason: "stop", timestamp: Date.now(),
      };
      const stream = createAssistantMessageEventStream();
      setTimeout(() => {
        const reply = `ack ${call}`;
        stream.push({ type: "start", partial: out });
        out.content.push({ type: "text", text: "" });
        stream.push({ type: "text_start", contentIndex: 0, partial: out });
        out.content[0].text = reply;
        stream.push({ type: "text_delta", contentIndex: 0, delta: reply, partial: out });
        stream.push({ type: "text_end", contentIndex: 0, content: reply, partial: out });
        stream.push({ type: "done", reason: "stop", message: out });
        stream.end();
        log({ call, event: "end" });
      }, slow ? Number(process.env.FAKE_SLOW_MS ?? 4000) : 0);
      return stream;
    },
  });
}
