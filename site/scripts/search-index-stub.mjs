// Shared by the search-index tests: a stubbed fetch serving a real (empty)
// static search index in the shape Fumadocs' client loads, and a way to wait
// for the status to settle. zbsearch is Fumadocs' engine and not a direct
// dependency, so it is resolved from fumadocs-core.
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const fumadocsRequire = createRequire(require.resolve("fumadocs-core/package.json"));
const zbsearch = await import(fumadocsRequire.resolve("zbsearch"));

const db = zbsearch.create({ schema: { _: "string" } });
export const indexPayload = JSON.stringify({ type: "simple", ...zbsearch.save(db) });

/**
 * Replaces globalThis.fetch. `respond(url, attempt)` returns true for a good
 * response and false for a 500; `attempt` is the 1-based request count.
 */
export function stubFetch(respond) {
  const requests = [];
  globalThis.fetch = async (input) => {
    const url = String(input);
    requests.push(url);
    return respond(url, requests.length)
      ? new Response(indexPayload, { status: 200 })
      : new Response("boom", { status: 500 });
  };
  return requests;
}

export async function settled(getStatus) {
  for (let i = 0; i < 100; i++) {
    const status = getStatus();
    if (status === "ready" || status === "error") return status;
    await new Promise((resolve) => setTimeout(resolve, 5));
  }
  throw new Error(`status never settled: ${getStatus()}`);
}
