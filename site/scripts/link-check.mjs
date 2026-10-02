#!/usr/bin/env node
// Link check of the production build. Run `pnpm build` first, then
// `pnpm links:check`: it serves the build with `next start`, takes every URL
// in sitemap.xml and runs lychee over the rendered pages, in two passes:
//
//   1. internal links and fragments (everything on the local server):
//      strict, any broken link fails the script (exit 1);
//   2. external links: advisory, a failure is printed (and surfaced as a
//      GitHub Actions warning) but the exit code stays 0, because a third
//      party being down is not a defect of this change.
//
// Needs the `lychee` binary on PATH. Config: .lychee.toml, ignore list:
// .lycheeignore (both at the repository root).
import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";
import { fileURLToPath } from "node:url";

const SITE_URL = readFileSync(new URL("../lib/site.ts", import.meta.url), "utf8").match(
  /SITE_URL = "([^"]+)"/,
)[1];
const REPO_ROOT = fileURLToPath(new URL("../../", import.meta.url));
const PORT = process.env.LINK_CHECK_PORT ?? "4318";
const BASE = `http://localhost:${PORT}`;

const ghWarning = (msg) => {
  if (process.env.GITHUB_ACTIONS) console.log(`::warning title=External links::${msg}`);
};

function lychee(args) {
  const res = spawnSync("lychee", ["--config", ".lychee.toml", ...args], {
    cwd: REPO_ROOT,
    stdio: "inherit",
  });
  if (res.error) {
    console.error(`could not run lychee (is it installed?): ${res.error.message}`);
    process.exit(1);
  }
  return res.status;
}

async function waitFor(url, tries = 60) {
  for (let i = 0; i < tries; i++) {
    try {
      if ((await fetch(url)).status === 200) return;
    } catch {
      // not up yet
    }
    await sleep(1000);
  }
  throw new Error(`${url} did not come up (did you run \`pnpm build\`?)`);
}

const server = spawn("pnpm", ["exec", "next", "start", "-p", PORT], {
  cwd: fileURLToPath(new URL("..", import.meta.url)),
  stdio: ["ignore", "ignore", "inherit"],
});
const dir = mkdtempSync(join(tmpdir(), "vx-links-"));
let code = 0;

try {
  await waitFor(`${BASE}/sitemap.xml`);
  const xml = await (await fetch(`${BASE}/sitemap.xml`)).text();
  const urls = [...xml.matchAll(/<loc>([^<]+)<\/loc>/g)]
    .map((m) => m[1])
    .filter((u) => u.startsWith(SITE_URL))
    .map((u) => BASE + u.slice(SITE_URL.length));
  if (urls.length === 0) throw new Error("sitemap.xml lists no page of this site");
  const list = join(dir, "urls.txt");
  writeFileSync(list, urls.join("\n") + "\n");
  console.log(`checking links on ${urls.length} pages`);

  // Pass 1: `--include` wins over `--exclude`, so excluding everything and
  // including the local origin leaves exactly the internal links.
  console.log("\n== internal links (strict) ==");
  if (lychee(["--exclude", ".", "--include", `^${BASE}`, "--files-from", list]) !== 0) code = 1;

  // Pass 2: everything except the local server; .lycheeignore drops the rest.
  console.log("\n== external links (advisory) ==");
  const ext = lychee(["--exclude", `^${BASE}`, "--files-from", list]);
  if (ext !== 0) {
    ghWarning("some external links failed; see the log of the link check step");
    console.log("external link failures do not fail the check");
  }
} catch (err) {
  console.error(err instanceof Error ? err.message : err);
  code = 1;
} finally {
  server.kill("SIGTERM");
  rmSync(dir, { recursive: true, force: true });
}
process.exit(code);
