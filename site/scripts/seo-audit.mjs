#!/usr/bin/env node
// SEO audit of the production build. Run `pnpm build` first, then
// `pnpm seo:audit`: it serves the build with `next start`, fetches the real
// HTML of every URL in sitemap.xml and fails (exit 1) on anything that would
// hurt indexing or sharing. `--url <origin>` audits an already running server
// (a deployment, say) instead of starting one.
import { spawn } from "node:child_process";
import { readFileSync } from "node:fs";
import { setTimeout as sleep } from "node:timers/promises";

const SITE_URL = readFileSync(new URL("../lib/site.ts", import.meta.url), "utf8").match(
  /SITE_URL = "([^"]+)"/,
)[1];
const LANGS = ["en", "es"];
const SUFFIX = " - vexillum";
const MAX_DESCRIPTION = 160;

const argUrl = process.argv.includes("--url")
  ? process.argv[process.argv.indexOf("--url") + 1]
  : null;
const PORT = process.env.SEO_AUDIT_PORT ?? "4317";
const BASE = (argUrl ?? `http://localhost:${PORT}`).replace(/\/$/, "");

const failures = [];
const fail = (url, msg) => failures.push(`${url}: ${msg}`);

/** Absolute site URL -> the URL actually fetched from the server under audit. */
const local = (url) => (url.startsWith(SITE_URL) ? BASE + url.slice(SITE_URL.length) : url);
const langOf = (url) => (new URL(url).pathname.startsWith("/es") ? "es" : "en");

async function get(url, init) {
  const res = await fetch(local(url), { redirect: "manual", ...init });
  return res;
}

// ---- HTML extraction (the markup Next emits is regular enough for regexes) ----

const decode = (s) =>
  s
    .replace(/&quot;/g, '"')
    .replace(/&#x27;|&#39;/g, "'")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&amp;/g, "&");

function attrs(tag) {
  const out = {};
  for (const m of tag.matchAll(/([a-zA-Z:-]+)="([^"]*)"/g)) out[m[1].toLowerCase()] = decode(m[2]);
  return out;
}

function parse(html) {
  const tags = (name) => [...html.matchAll(new RegExp(`<${name}\\b[^>]*>`, "g"))].map((m) => attrs(m[0]));
  const metas = tags("meta");
  const links = tags("link");
  const meta = (key) =>
    metas.find((m) => m.name === key || m.property === key)?.content;
  const all = (key) =>
    metas.filter((m) => m.name === key || m.property === key).map((m) => m.content);
  const title = html.match(/<title>([^<]*)<\/title>/)?.[1];
  const body = html.slice(html.indexOf("<body"));
  return {
    lang: html.match(/<html[^>]*\blang="([^"]+)"/)?.[1],
    title: title === undefined ? undefined : decode(title),
    description: meta("description"),
    robots: meta("robots"),
    canonical: links.find((l) => l.rel === "canonical")?.href,
    alternates: Object.fromEntries(
      links.filter((l) => l.rel === "alternate" && l.hreflang).map((l) => [l.hreflang, l.href]),
    ),
    meta,
    all,
    h: [...body.matchAll(/<h([1-6])\b/g)].map((m) => Number(m[1])),
    imgsWithoutAlt: tags("img").filter((i) => i.alt === undefined).length,
    jsonld: [...html.matchAll(/<script type="application\/ld\+json">([\s\S]*?)<\/script>/g)].map(
      (m) => m[1],
    ),
  };
}

// ---- sitemap ----

function parseSitemap(xml) {
  const entries = new Map();
  for (const block of xml.matchAll(/<url>([\s\S]*?)<\/url>/g)) {
    const loc = block[1].match(/<loc>([^<]+)<\/loc>/)[1];
    const alternates = {};
    for (const l of block[1].matchAll(/<xhtml:link rel="alternate" hreflang="([^"]+)" href="([^"]+)"/g)) {
      alternates[l[1]] = l[2];
    }
    entries.set(loc, alternates);
  }
  return entries;
}

function pngSize(buf) {
  if (buf.length < 24 || buf.toString("latin1", 1, 4) !== "PNG") return null;
  return { width: buf.readUInt32BE(16), height: buf.readUInt32BE(20) };
}

// ---- checks ----

function checkPage(url, p, sitemap) {
  const lang = langOf(url);
  if (p.lang !== lang) fail(url, `<html lang="${p.lang}"> should be "${lang}"`);

  const isLanding = new URL(url).pathname === "/" || new URL(url).pathname === "/es";
  const titleOk = isLanding ? p.title?.startsWith("vexillum - ") : p.title?.endsWith(SUFFIX);
  if (!p.title) fail(url, "missing <title>");
  else if (!titleOk) {
    fail(url, `title "${p.title}" breaks the pattern (landing "vexillum - ...", docs "... - vexillum")`);
  }
  if (!p.description) fail(url, "missing meta description");
  else if (p.description.length > MAX_DESCRIPTION) {
    fail(url, `description is ${p.description.length} chars (max ${MAX_DESCRIPTION})`);
  }
  if (p.robots && /noindex/i.test(p.robots)) fail(url, `robots meta "${p.robots}" blocks a sitemap page`);

  if (p.canonical !== url) fail(url, `canonical is ${p.canonical}, expected the page itself`);

  // hreflang: matches the sitemap, self-referencing, reciprocal, with an x-default.
  const expected = sitemap.get(url);
  if (JSON.stringify(sortKeys(p.alternates)) !== JSON.stringify(sortKeys(expected))) {
    fail(url, `hreflang ${JSON.stringify(p.alternates)} differs from the sitemap's ${JSON.stringify(expected)}`);
  }
  if (p.alternates[lang] !== url) fail(url, `hreflang ${lang} does not point at the page itself`);
  if (!p.alternates["x-default"]) fail(url, "missing hreflang x-default");

  // social
  const og = (k) => p.meta(`og:${k}`);
  if (og("title") !== p.title) fail(url, `og:title "${og("title")}" differs from the title`);
  if (og("description") !== p.description) fail(url, "og:description differs from the description");
  if (og("url") !== url) fail(url, `og:url is ${og("url")}`);
  if (og("site_name") !== "vexillum") fail(url, "og:site_name is not vexillum");
  if (!og("locale")) fail(url, "missing og:locale");
  const wantLocale = { en: "en_US", es: "es_ES" }[lang];
  if (og("locale") !== wantLocale) fail(url, `og:locale is ${og("locale")}, expected ${wantLocale}`);
  const altLocales = p.all("og:locale:alternate");
  const wantAlt = LANGS.filter((l) => l !== lang && p.alternates[l]).map(
    (l) => ({ en: "en_US", es: "es_ES" })[l],
  );
  if (JSON.stringify(altLocales) !== JSON.stringify(wantAlt)) {
    fail(url, `og:locale:alternate ${JSON.stringify(altLocales)} should be ${JSON.stringify(wantAlt)}`);
  }
  if (p.meta("twitter:card") !== "summary_large_image") fail(url, "twitter:card is not summary_large_image");
  if (p.meta("twitter:title") !== p.title) fail(url, "twitter:title differs from the title");
  if (p.meta("twitter:description") !== p.description) fail(url, "twitter:description differs from the description");
  const image = og("image");
  if (!image || !image.startsWith(`${SITE_URL}/og/`)) fail(url, `og:image is ${image}`);
  if (og("image:width") !== "1200" || og("image:height") !== "630") fail(url, "og:image size is not 1200x630");
  if (!og("image:alt")) fail(url, "missing og:image:alt");
  if (p.meta("twitter:image") !== image) fail(url, "twitter:image differs from og:image");
  if (!p.meta("twitter:image:alt")) fail(url, "missing twitter:image:alt");

  // structure: one h1, first heading is it, no skipped levels, alt on images
  const h1s = p.h.filter((n) => n === 1).length;
  if (h1s !== 1) fail(url, `${h1s} <h1> elements, expected exactly 1`);
  if (p.h[0] !== 1) fail(url, `first heading is <h${p.h[0]}>, expected <h1>`);
  for (let i = 1; i < p.h.length; i++) {
    if (p.h[i] > p.h[i - 1] + 1) {
      fail(url, `heading jumps from <h${p.h[i - 1]}> to <h${p.h[i]}>`);
      break;
    }
  }
  if (p.imgsWithoutAlt) fail(url, `${p.imgsWithoutAlt} <img> without alt`);

  // JSON-LD
  const types = [];
  for (const raw of p.jsonld) {
    try {
      const data = JSON.parse(raw);
      types.push(data["@type"]);
      if (data["@context"] !== "https://schema.org") fail(url, "JSON-LD without schema.org context");
      checkLd(url, data, sitemap);
    } catch (e) {
      fail(url, `JSON-LD does not parse: ${e.message}`);
    }
  }
  const wantTypes = isLanding ? ["SoftwareApplication"] : ["BreadcrumbList", "TechArticle"];
  for (const t of wantTypes) if (!types.includes(t)) fail(url, `missing ${t} JSON-LD`);
}

function checkLd(url, data, sitemap) {
  if (data["@type"] === "BreadcrumbList") {
    const items = data.itemListElement;
    items.forEach((it, i) => {
      if (it.position !== i + 1) fail(url, "breadcrumb positions are not 1..n");
      if (!it.name) fail(url, "breadcrumb item without a name");
      if (!sitemap.has(it.item)) fail(url, `breadcrumb ${it.item} is not a sitemap URL`);
    });
    if (items.at(-1)?.item !== url) fail(url, "last breadcrumb is not the page itself");
  }
  if (data["@type"] === "TechArticle") {
    if (!data.headline || !data.description) fail(url, "TechArticle without headline or description");
    if (data.url !== url) fail(url, `TechArticle url is ${data.url}`);
    if (data.inLanguage !== langOf(url)) fail(url, `TechArticle inLanguage is ${data.inLanguage}`);
  }
  if (data["@type"] === "SoftwareApplication") {
    for (const k of ["name", "description", "url", "applicationCategory", "operatingSystem", "license"]) {
      if (!data[k]) fail(url, `SoftwareApplication without ${k}`);
    }
  }
}

const sortKeys = (o) => Object.fromEntries(Object.entries(o ?? {}).sort(([a], [b]) => a.localeCompare(b)));

// ---- run ----

async function audit() {
  const sitemapRes = await get(`${SITE_URL}/sitemap.xml`);
  if (sitemapRes.status !== 200) throw new Error(`sitemap.xml answered ${sitemapRes.status}`);
  const sitemapXml = await sitemapRes.text();
  const sitemap = parseSitemap(sitemapXml);
  if (sitemap.size === 0) throw new Error("sitemap.xml lists no URLs");
  if (/<lastmod>/.test(sitemapXml)) fail("sitemap.xml", "lastmod present but no reliable source for it");

  // robots.txt
  const robots = await (await get(`${SITE_URL}/robots.txt`)).text();
  if (!robots.includes(`Sitemap: ${SITE_URL}/sitemap.xml`)) fail("robots.txt", "does not reference the sitemap");
  for (const path of ["/llms.txt", "/es/llms.txt"]) {
    if (!robots.includes(`${SITE_URL}${path}`)) fail("robots.txt", `does not mention ${path}`);
    const res = await get(`${SITE_URL}${path}`);
    if (res.status !== 200) fail(path, `answered ${res.status}`);
  }
  for (const path of ["/install", "/api/"]) {
    if (!robots.includes(`Disallow: ${path}`)) fail("robots.txt", `does not disallow ${path}`);
  }
  for (const url of sitemap.keys()) {
    const path = new URL(url).pathname;
    if (path === "/install" || path.startsWith("/api/") || path.startsWith("/llms")) {
      fail("sitemap.xml", `lists non-page ${path}`);
    }
  }

  // every sitemap page
  const pages = new Map();
  for (const url of sitemap.keys()) {
    const res = await get(url);
    if (res.status !== 200) {
      fail(url, `answered ${res.status}`);
      continue;
    }
    pages.set(url, parse(await res.text()));
  }
  for (const [url, p] of pages) checkPage(url, p, sitemap);

  // reciprocity: every alternate page points back with the same language
  for (const [url, p] of pages) {
    for (const [hl, target] of Object.entries(p.alternates)) {
      if (hl === "x-default") continue;
      const back = pages.get(target);
      if (!back) fail(url, `hreflang ${hl} target ${target} is not an audited page`);
      else if (back.alternates[langOf(url)] !== url) fail(url, `${target} does not link back`);
    }
  }

  // uniqueness, per language
  for (const field of ["title", "description"]) {
    const seen = new Map();
    for (const [url, p] of pages) {
      const key = `${langOf(url)}|${p[field]}`;
      if (seen.has(key)) fail(url, `${field} duplicates ${seen.get(key)}`);
      else seen.set(key, url);
    }
  }

  // pages with no translation: the /es URL serves the English page and must
  // canonicalise to it, with no es alternate and no listing in the sitemap
  let fallbacks = 0;
  for (const [url] of sitemap) {
    if (langOf(url) !== "en" || !new URL(url).pathname.startsWith("/docs")) continue;
    const esUrl = url.replace(SITE_URL, `${SITE_URL}/es`);
    if (sitemap.has(esUrl)) continue;
    fallbacks++;
    const res = await get(esUrl);
    if (res.status !== 200) {
      fail(esUrl, `answered ${res.status}`);
      continue;
    }
    const p = parse(await res.text());
    if (p.canonical !== url) fail(esUrl, `fallback canonical is ${p.canonical}, expected ${url}`);
    if (p.alternates.es) fail(esUrl, "fallback page claims an es alternate");
    if (p.robots && /noindex/i.test(p.robots)) fail(esUrl, "fallback page is noindex while canonicalised");
  }

  // social images: reachable, real PNGs, 1200x630
  const images = new Set([...pages.values()].map((p) => p.meta("og:image")).filter(Boolean));
  for (const image of images) {
    const res = await get(image);
    const buf = Buffer.from(await res.arrayBuffer());
    const size = pngSize(buf);
    if (res.status !== 200) fail(image, `answered ${res.status}`);
    else if (!res.headers.get("content-type")?.startsWith("image/png")) fail(image, "not served as image/png");
    else if (!size || size.width !== 1200 || size.height !== 630) fail(image, `is ${size?.width}x${size?.height}`);
    else if (buf.length > 600_000) fail(image, `is ${buf.length} bytes, over the 600 KB budget`);
  }

  // an unknown URL is a real 404
  const missing = await get(`${SITE_URL}/docs/does-not-exist`);
  if (missing.status !== 404) fail("/docs/does-not-exist", `answered ${missing.status}, expected 404`);

  console.log(
    `audited ${pages.size} pages, ${fallbacks} untranslated /es fallbacks, ${images.size} social images`,
  );
}

let server;
try {
  if (!argUrl) {
    server = spawn("pnpm", ["exec", "next", "start", "-p", PORT], {
      stdio: ["ignore", "ignore", "inherit"],
    });
    for (let i = 0; ; i++) {
      try {
        if ((await fetch(`${BASE}/robots.txt`)).ok) break;
      } catch {
        // not up yet
      }
      if (i > 60) throw new Error("next start did not come up (did you run `pnpm build`?)");
      await sleep(500);
    }
  }
  await audit();
} catch (e) {
  failures.push(`audit crashed: ${e.stack ?? e}`);
} finally {
  server?.kill();
}

if (failures.length) {
  console.error(`\n${failures.length} SEO problem(s):`);
  for (const f of failures.slice(0, 80)) console.error(` - ${f}`);
  if (failures.length > 80) console.error(` ... and ${failures.length - 80} more`);
  process.exit(1);
}
console.log("seo audit passed");
