import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { atLeastMinVersion, escapeMDX, generate, normalizeBody, renderPage } from "./changelog.mjs";

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

test("normalizeBody keeps only the grouped sections without hashes", () => {
  const out = normalizeBody(
    "## Changelog\n### Features\n* 37e003d feat(forum): accept {braces} and <tags> in `<code>`\n### Others\n* abc1234 wip\n### Bug fixes\n* d61739f1a49dc26675d687b0fd4d6a4d7c14c8c1 fix: a bug\n\n**Full changelog**: https://x\n",
  );
  assert.ok(out.startsWith("### Features"));
  assert.ok(!out.includes("37e003d") && !out.includes("wip") && !out.includes("Full changelog"));
  assert.ok(out.includes("\\{braces\\} and \\<tags> in `<code>`"));
  assert.ok(out.indexOf("### Features") < out.indexOf("### Bug fixes"));
  assert.equal(normalizeBody("### Others\n* abc1234 x"), "");
});

test("renderPage marks the page as generated and lists releases", () => {
  const page = renderPage("en", [release]);
  assert.ok(page.includes("Do not edit by hand"));
  assert.ok(page.includes("## [v0.2.0](https://github.com/isaias-alt/vexillum/releases/tag/v0.2.0)"));
  assert.ok(page.includes("Released 2026-10-02"));
  assert.ok(renderPage("es", [release]).includes("Publicado 2026-10-02"));
});

test("renderPage says that nothing was published when there are no releases", () => {
  const en = renderPage("en", []);
  assert.ok(en.includes("No release has been published yet."));
  assert.ok(!en.includes("##"));
  assert.ok(renderPage("es", []).includes("Todavía no se publicó ningún release."));
});

test("generate writes both locales and never throws on a failing fetch", async () => {
  const dir = mkdtempSync(join(tmpdir(), "changelog-"));
  const failing = async () => {
    throw new Error("offline");
  };
  await generate(dir, failing, quiet);
  assert.ok(readFileSync(join(dir, "en", "changelog.mdx"), "utf8").includes("No release has been published yet."));
  assert.ok(readFileSync(join(dir, "es", "changelog.mdx"), "utf8").includes("Todavía no se publicó"));
});

test("generate keeps an earlier page when GitHub is unreachable", async () => {
  const dir = mkdtempSync(join(tmpdir(), "changelog-"));
  mkdirSync(join(dir, "en"));
  writeFileSync(join(dir, "en", "changelog.mdx"), "earlier");
  await generate(dir, async () => ({ ok: false, status: 503 }), quiet);
  assert.equal(readFileSync(join(dir, "en", "changelog.mdx"), "utf8"), "earlier");
  assert.ok(readFileSync(join(dir, "es", "changelog.mdx"), "utf8").includes("Todavía no se publicó"));
});

test("generate renders non-draft releases newest first", async () => {
  const dir = mkdtempSync(join(tmpdir(), "changelog-"));
  const older = { ...release, tag_name: "v0.2.1", published_at: "2026-10-01T00:00:00Z", body: "" };
  const draft = { ...release, tag_name: "v9.9.9", draft: true };
  const ok = async () => ({ ok: true, json: async () => [older, draft, release] });
  await generate(dir, ok, quiet);
  const page = readFileSync(join(dir, "en", "changelog.mdx"), "utf8");
  assert.ok(page.indexOf("v0.2.0") < page.indexOf("v0.2.1]"));
  assert.ok(!page.includes("v9.9.9"));
});

test("generate excludes pre-releases and caps to the latest 10", async () => {
  const dir = mkdtempSync(join(tmpdir(), "changelog-"));
  const stable = Array.from({ length: 12 }, (_, i) => ({
    ...release,
    tag_name: `v1.0.${i}`,
    published_at: `2026-10-${String(i + 1).padStart(2, "0")}T00:00:00Z`,
  }));
  const pre = [
    { ...release, tag_name: "v2.0.0-rc.1", prerelease: true },
    { ...release, tag_name: "v2.0.0-canary.3" },
  ];
  await generate(dir, async () => ({ ok: true, json: async () => [...pre, ...stable] }), quiet);
  const page = readFileSync(join(dir, "en", "changelog.mdx"), "utf8");
  assert.ok(!page.includes("v2.0.0"));
  assert.equal(page.match(/^## \[/gm).length, 10);
  assert.ok(page.includes("v1.0.11") && !page.includes("v1.0.1]"));
});

test("generate shows the no-release line when GitHub returns nothing", async () => {
  const dir = mkdtempSync(join(tmpdir(), "changelog-"));
  await generate(dir, async () => ({ ok: true, json: async () => [] }), quiet);
  assert.ok(readFileSync(join(dir, "en", "changelog.mdx"), "utf8").includes("No release has been published yet."));
  await generate(dir, async () => ({ ok: false, status: 403 }), quiet);
  assert.ok(readFileSync(join(dir, "es", "changelog.mdx"), "utf8").includes("Todavía no se publicó"));
});

test("releases below the minimum version are treated as no releases", async () => {
  assert.ok(!atLeastMinVersion("v0.1.1") && !atLeastMinVersion("v0.1.9") && !atLeastMinVersion("nightly"));
  assert.ok(atLeastMinVersion("v0.2.0") && atLeastMinVersion("v0.10.0") && atLeastMinVersion("v1.0.0"));
  const dir = mkdtempSync(join(tmpdir(), "changelog-"));
  const early = ["v0.1.0", "v0.1.1"].map((tag_name) => ({ ...release, tag_name }));
  await generate(dir, async () => ({ ok: true, json: async () => early }), quiet);
  const page = readFileSync(join(dir, "en", "changelog.mdx"), "utf8");
  assert.ok(page.includes("No release has been published yet.") && !page.includes("v0.1"));
});
