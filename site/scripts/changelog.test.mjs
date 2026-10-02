import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { escapeMDX, generate, normalizeBody, renderPage } from "./changelog.mjs";

const quiet = { log() {}, warn() {} };

const release = {
  tag_name: "v0.2.0",
  html_url: "https://github.com/isaias-alt/vexillum/releases/tag/v0.2.0",
  published_at: "2026-10-02T10:00:00Z",
  draft: false,
  body: "## Changelog\n### Features\n* 37e003d feat(forum): accept {braces} and <tags> in `<code>`\n\n**Full changelog**: https://x/compare/v0.1.1...v0.2.0\n",
};

test("escapeMDX escapes braces and angle brackets outside code", () => {
  assert.equal(escapeMDX("a {b} <c> `{d}`"), "a \\{b\\} \\<c> `{d}`");
  assert.equal(escapeMDX("```\n{x}\n```\n{y}"), "```\n{x}\n```\n\\{y\\}");
});

test("normalizeBody drops the goreleaser title and links commit hashes", () => {
  const out = normalizeBody(release.body);
  assert.ok(!out.includes("## Changelog"));
  assert.ok(out.startsWith("### Features"));
  assert.ok(out.includes("[37e003d](https://github.com/isaias-alt/vexillum/commit/37e003d)"));
  assert.ok(out.includes("\\{braces\\} and \\<tags> in `<code>`"));
});

test("renderPage marks the page as generated and lists releases", () => {
  const page = renderPage("en", [release]);
  assert.ok(page.includes("Do not edit by hand"));
  assert.ok(page.includes("## [v0.2.0](https://github.com/isaias-alt/vexillum/releases/tag/v0.2.0)"));
  assert.ok(page.includes("Released 2026-10-02"));
  assert.ok(renderPage("es", [release]).includes("Publicado 2026-10-02"));
});

test("renderPage has a graceful page for no releases and for unavailable", () => {
  assert.ok(renderPage("en", []).includes("no releases yet"));
  assert.ok(renderPage("es", []).includes("Todavía no hay releases"));
  assert.ok(renderPage("en", [], "unavailable").includes("could not be loaded"));
});

test("generate writes both locales and never throws on a failing fetch", async () => {
  const dir = mkdtempSync(join(tmpdir(), "changelog-"));
  const failing = async () => {
    throw new Error("offline");
  };
  await generate(dir, failing, quiet);
  assert.ok(readFileSync(join(dir, "en", "changelog.mdx"), "utf8").includes("could not be loaded"));
  assert.ok(readFileSync(join(dir, "es", "changelog.mdx"), "utf8").includes("No se pudieron"));
});

test("generate keeps an earlier page when GitHub is unreachable", async () => {
  const dir = mkdtempSync(join(tmpdir(), "changelog-"));
  mkdirSync(join(dir, "en"));
  writeFileSync(join(dir, "en", "changelog.mdx"), "earlier");
  await generate(dir, async () => ({ ok: false, status: 503 }), quiet);
  assert.equal(readFileSync(join(dir, "en", "changelog.mdx"), "utf8"), "earlier");
  assert.ok(readFileSync(join(dir, "es", "changelog.mdx"), "utf8").includes("No se pudieron"));
});

test("generate renders non-draft releases newest first", async () => {
  const dir = mkdtempSync(join(tmpdir(), "changelog-"));
  const older = { ...release, tag_name: "v0.1.0", published_at: "2026-09-01T00:00:00Z", body: "" };
  const draft = { ...release, tag_name: "v9.9.9", draft: true };
  const ok = async () => ({ ok: true, json: async () => [older, draft, release] });
  await generate(dir, ok, quiet);
  const page = readFileSync(join(dir, "en", "changelog.mdx"), "utf8");
  assert.ok(page.indexOf("v0.2.0") < page.indexOf("v0.1.0"));
  assert.ok(!page.includes("v9.9.9"));
});

test("normalizeBody shortens a full commit hash but links the full one", () => {
  const sha = "d61739f1a49dc26675d687b0fd4d6a4d7c14c8c1";
  assert.equal(
    normalizeBody(`* ${sha} Add MIT license`),
    `* [d61739f](https://github.com/isaias-alt/vexillum/commit/${sha}) Add MIT license`,
  );
});
