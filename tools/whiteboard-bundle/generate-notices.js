// Maintainer-only generator for THIRD-PARTY-NOTICES.md. Run it with
// `npm run notices` from this directory (see README.md) after any change to the
// bundle, the lockfile or the vendored fonts; nothing here runs during
// `go build` or on a user's machine.
//
// It repeats the esbuild build of build.js (same options, nothing written) to
// get the metafile, takes every npm package that contributes bytes to the
// output, and reads each one's name, version, license identifier, copyright
// lines and license text from the installed package. It fails, with the list
// of offenders, when a bundled package has no license information, a license
// file it cannot classify, or a declared license that its own file contradicts,
// so a new dependency cannot slip in unnoticed. The font section is read from
// the vendored woff2 files' name tables. Output order is fully sorted, so two
// runs on the same tree give the same bytes.
//
// Writes, relative to the vexillum repo:
//   THIRD-PARTY-NOTICES.md                     the notices
//   tools/whiteboard-bundle/bundled-packages.json
//       the bundled package list and the sha256 of package-lock.json it was
//       generated from; the Go test (internal/thirdparty) reads it
//
// `node generate-notices.js --check` regenerates in memory and exits non-zero
// when the committed files differ.

import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { existsSync, readdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import * as esbuild from "esbuild";

import { FONT_FAMILIES, frameBuildOptions } from "./bundle-options.js";
import {
  classifyLicense,
  copyrightLines,
  licenseIds,
  declaredLicense,
  licenseBody,
  packageRoot,
  printableLicense,
  readWoff2Names,
  spdxIds,
} from "./notices-lib.js";
import {
  COPYRIGHT_FALLBACKS,
  ELECTED,
  EMBEDDED_INVENTORY,
  EXTRA_LICENSE_SOURCES,
  FONT_DISPLAY,
  FONT_LICENSES,
  GO_STD_NOTE,
  GO_NO_MODULES,
  GO_LICENSE_TEXT,
  HEADLINE,
  ICONS,
  INTRO,
  REGENERATE,
  SLOT_SECTION,
  XIAOLAI_NOTE,
} from "./notices-prose.js";

const HERE = dirname(fileURLToPath(import.meta.url));
const REPO = resolve(HERE, "../..");
const NOTICES = join(REPO, "THIRD-PARTY-NOTICES.md");
const MANIFEST = join(HERE, "bundled-packages.json");
const LOCKFILE = join(HERE, "package-lock.json");
const FONTS_DIR = join(REPO, "internal/forum/assets/whiteboard/fonts");
const FONT_SOURCE = join(HERE, "node_modules/@excalidraw/excalidraw/dist/prod/fonts");

// Problems are collected and reported together so one run shows them all.
const problems = [];
const fail = (message) => problems.push(message);

const byText = (a, b) => (a < b ? -1 : a > b ? 1 : 0);
const cell = (text) => text.replace(/\|/g, "\\|").replace(/\s+/g, " ").trim();
const fence = (text) => `\`\`\`\n${text.replace(/\r\n/g, "\n").replace(/[ \t]+$/gm, "").trimEnd()}\n\`\`\``;

// ---------------------------------------------------------------- npm bundle

async function bundledRoots() {
  const result = await esbuild.build({
    ...frameBuildOptions("dist-notices"),
    write: false,
    metafile: true,
  });
  const bytes = new Map();
  for (const output of Object.values(result.metafile.outputs)) {
    for (const [input, info] of Object.entries(output.inputs)) {
      bytes.set(input, (bytes.get(input) ?? 0) + info.bytesInOutput);
    }
  }
  const roots = new Set();
  for (const [input, n] of bytes) {
    const root = packageRoot(input);
    if (root && n > 0) roots.add(root);
  }
  return [...roots].sort(byText);
}

function licenseFileNames(dir) {
  return readdirSync(dir)
    .filter((f) => /^(licen[sc]e|copying|unlicense)/i.test(f))
    .filter((f) => !/\.(js|mjs|cjs|ts|json|map)$/i.test(f))
    .filter((f) => statSync(join(dir, f)).isFile())
    .sort(byText);
}

function noticeFileNames(dir) {
  return readdirSync(dir)
    .filter((f) => /^notice/i.test(f) && statSync(join(dir, f)).isFile())
    .sort(byText);
}

// Everything the notices need to know about one installed package copy.
function describePackage(root) {
  const dir = join(HERE, root);
  const pkg = JSON.parse(readFileSync(join(dir, "package.json"), "utf8"));
  const label = `${pkg.name}@${pkg.version}`;
  const files = licenseFileNames(dir).map((f) => {
    const text = readFileSync(join(dir, f), "utf8");
    const ids = licenseIds(text);
    if (ids.length === 0) fail(`${label}: license file ${f} is not a license vexillum can classify`);
    return { name: f, text, ids };
  });
  const notices = noticeFileNames(dir).map((f) => ({ name: f, text: readFileSync(join(dir, f), "utf8") }));

  const extra = [];
  for (const source of EXTRA_LICENSE_SOURCES[pkg.name] ?? []) {
    const raw = readFileSync(join(dir, source.file), "utf8");
    // The first run of comment lines of the file is the license header.
    const lines = [];
    for (const line of raw.split("\n")) {
      if (line.startsWith(source.comment)) lines.push(line.slice(source.comment.length).replace(/^ /, ""));
      else if (lines.length > 0) break;
    }
    const text = lines.join("\n").trim();
    if (classifyLicense(text) !== source.id) fail(`${label}: ${source.file} header is not a ${source.id} license`);
    extra.push({ name: source.file, text, ids: [source.id] });
  }

  const declared = declaredLicense(pkg);
  const detected = [...new Set([...files, ...extra].flatMap((f) => f.ids))].sort(byText);
  let expression = declared;
  let detectedOnly = false;
  let extended = "";
  if (!declared) {
    if (detected.length === 0) {
      fail(`${label}: no license information (no license field, no usable license file)`);
      expression = "UNKNOWN";
    } else {
      expression = detected.join(" AND ");
      detectedOnly = true;
    }
  } else {
    const named = spdxIds(declared);
    for (const file of [...files, ...extra]) {
      const missing = file.ids.filter((id) => !named.includes(id));
      if (missing.length === 0) continue;
      if (file.ids.length === 1) {
        fail(`${label}: declares ${declared} but its license file ${file.name} reads ${missing[0]}`);
      } else {
        // A combined file: the declared license plus the licenses of code the
        // package embeds. Both apply, so both are listed.
        for (const id of missing) if (!extended.includes(id)) extended += `${extended ? " AND " : ""}${id}`;
      }
    }
    if (extended) expression = `${declared} AND ${extended}`;
  }

  // Copyright lines: the package's own license and notice files first, then a
  // curated fallback, then the package.json author. Only the first two are
  // "the package's own LICENSE text"; the others are marked in the output.
  let lines = [];
  for (const f of [...files, ...extra, ...notices]) {
    for (const l of copyrightLines(f.text)) if (!lines.includes(l)) lines.push(l);
  }
  let source = "license";
  let unverified = "";
  if (lines.length === 0) {
    const fallback = COPYRIGHT_FALLBACKS[pkg.name];
    const author = typeof pkg.author === "string" ? pkg.author : pkg.author?.name;
    if (fallback?.file) {
      const text = readFileSync(join(dir, fallback.file), "utf8");
      const m = text.match(fallback.pattern);
      if (m) {
        lines = [`${fallback.prefix ?? ""}${m[0]}`];
        source = "banner";
      } else {
        fail(`${label}: no copyright text matching ${fallback.pattern} in ${fallback.file}`);
      }
    } else if (fallback?.lines) {
      lines = fallback.lines;
      source = "curated";
      unverified = fallback.unverified;
    } else if (author) {
      lines = [author.replace(/\s*<[^>]*>/, "").replace(/\s*\([^)]*\)\s*$/, "").trim()];
      source = "author";
    } else {
      source = "none";
    }
  }
  return { root, pkg, label, name: pkg.name, version: pkg.version, expression, detectedOnly, extended, files, extra, notices, lines, source, unverified };
}

// ------------------------------------------------------------ license texts

// Per SPDX id, the distinct bodies seen across all packages, most common
// first. The first one is printed as the license text; any other is printed as
// a variant so no wording of a bundled license is left out.
function licensePool(packages) {
  const pool = new Map();
  for (const p of packages) {
    for (const f of [...p.files, ...p.extra]) {
      if (f.ids.length !== 1) continue;
      const id = f.ids[0];
      const bodies = pool.get(id) ?? new Map();
      const key = licenseBody(f.text);
      const entry = bodies.get(key) ?? { text: printableLicense(f.text), packages: new Set() };
      entry.packages.add(p.label);
      bodies.set(key, entry);
      pool.set(id, bodies);
    }
  }
  const out = new Map();
  for (const [id, bodies] of pool) {
    const variants = [...bodies.values()]
      .map((v) => ({ text: v.text, packages: [...v.packages].sort(byText) }))
      .sort((a, b) => b.packages.length - a.packages.length || byText(a.packages[0], b.packages[0]));
    out.set(id, variants);
  }
  return out;
}

const LICENSE_ORDER = ["MIT", "ISC", "BSD-2-Clause", "BSD-3-Clause", "Apache-2.0", "MPL-2.0", "Zlib", "0BSD", "CC0-1.0", "Unlicense"];

// ------------------------------------------------------------------- fonts

function readFonts() {
  const families = readdirSync(FONTS_DIR).filter((f) => statSync(join(FONTS_DIR, f)).isDirectory()).sort(byText);
  if (families.join() !== [...FONT_FAMILIES].sort(byText).join()) {
    fail(`vendored font families (${families.join(", ")}) differ from FONT_FAMILIES in bundle-options.js`);
  }
  const out = [];
  for (const family of families) {
    const files = readdirSync(join(FONTS_DIR, family)).filter((f) => f.endsWith(".woff2")).sort(byText);
    const sourceDir = join(FONT_SOURCE, family);
    for (const f of files) {
      const source = join(sourceDir, f);
      if (existsSync(source) && !readFileSync(source).equals(readFileSync(join(FONTS_DIR, family, f)))) {
        fail(`font ${family}/${f} differs from the copy in @excalidraw/excalidraw (run npm run build)`);
      }
    }
    const tables = files.map((f) => readWoff2Names(join(FONTS_DIR, family, f)));
    const distinct = (id) => [...new Set(tables.map((n) => n[id]).filter((v) => v !== undefined))];
    const curated = FONT_LICENSES[family];
    if (!curated) {
      fail(`font family ${family} has no entry in FONT_LICENSES (notices-prose.js)`);
      continue;
    }
    for (const n of tables) {
      if (!curated.holds(n)) fail(`font family ${family}: the font file no longer backs the curated license claim (${curated.license})`);
    }
    out.push({
      family,
      display: FONT_DISPLAY[family] ?? family,
      files: files.length,
      curated,
      versions: distinct(5).map((v) => v.trim()),
      copyright: distinct(0),
      licenseText: distinct(13),
      licenseUrl: distinct(14),
      designers: distinct(9),
    });
  }
  return out;
}

// The OFL text, taken from the Virgil font file, which carries it verbatim.
function oflText(fonts) {
  const virgil = fonts.find((f) => f.family === "Virgil");
  const full = virgil?.licenseText[0] ?? "";
  const at = full.indexOf("SIL OPEN FONT LICENSE Version 1.1");
  const start = at < 0 ? -1 : full.lastIndexOf("-".repeat(59), at);
  if (start < 0) {
    fail("the Virgil font no longer carries the OFL text");
    return "";
  }
  return full.slice(start).trim();
}

function fontCopyrightCell(font) {
  const lines = font.copyright.flatMap((c) => {
    const found = copyrightLines(c);
    return found.length > 0 ? found : [c.trim().replace(/\s+/g, " ")];
  });
  return [...new Set(lines)].join("; ");
}

// --------------------------------------------------------------------- Go

function goModules() {
  let listing;
  try {
    listing = execFileSync("go", ["list", "-deps", "-f", "{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}", "./cmd/vx"], {
      cwd: REPO,
      encoding: "utf8",
    });
  } catch (err) {
    fail(`go list failed (the Go section needs the go tool and the module cache): ${err.message}`);
    return [];
  }
  const seen = new Map();
  for (const line of listing.split("\n").filter(Boolean)) {
    const [path, version, dir] = line.split(" ");
    if (seen.has(path)) continue;
    const licenseFile = ["LICENSE", "LICENSE.txt", "LICENSE.md", "COPYING"].map((f) => join(dir, f)).find(existsSync);
    if (!licenseFile) {
      fail(`Go module ${path}@${version}: no LICENSE file in the module cache`);
      continue;
    }
    const text = readFileSync(licenseFile, "utf8");
    const id = classifyLicense(text);
    if (!id) fail(`Go module ${path}@${version}: LICENSE is not a license vexillum can classify`);
    seen.set(path, { path, version, id, lines: copyrightLines(text), text });
  }
  return [...seen.values()].sort((a, b) => byText(a.path, b.path));
}

// ------------------------------------------------------------------ output

function render(packages, fonts, goMods) {
  // One entry per name@version: a package present at several paths is one notice.
  const unique = new Map();
  for (const p of packages) if (!unique.has(p.label)) unique.set(p.label, p);
  const list = [...unique.values()].sort((a, b) => byText(a.name, b.name) || byText(a.version, b.version));
  const pool = licensePool(list);

  // Every id in use needs a text to print: the pool, or fail loudly.
  const used = new Set();
  for (const p of list) for (const id of spdxIds(p.expression)) used.add(id);
  used.delete("UNKNOWN");
  for (const id of used) {
    if (!pool.has(id)) fail(`license ${id} is in use but no package ships its text`);
  }
  for (const id of pool.keys()) if (!LICENSE_ORDER.includes(id)) fail(`license ${id} is not in LICENSE_ORDER (generate-notices.js)`);

  const out = [];
  const push = (s = "") => out.push(s);

  push(`<!-- AUTO-GENERATED by tools/whiteboard-bundle/generate-notices.js. DO NOT EDIT BY HAND.`);
  push(`     Regenerate with: ${REGENERATE}`);
  push(`     (add --check to verify without writing) -->`);
  push();
  push("# Third-party notices");
  push();
  push(`> This file is auto-generated, do not edit it by hand. Regenerate it with \`${REGENERATE}\`.`);
  push();
  push(INTRO);
  push();

  push("## Summary: the packages vexillum chose");
  push();
  push("| Package | License | Copyright |");
  push("| --- | --- | --- |");
  for (const row of HEADLINE) {
    const members = row.names.map((n) => list.find((p) => p.name === n)).filter(Boolean);
    if (members.length !== row.names.length) {
      fail(`headline package ${row.names.join(", ")} is not in the bundle`);
      continue;
    }
    const first = members[0];
    const versions = [...new Set(members.map((m) => m.version))].join(", ");
    const license = ELECTED[first.name]
      ? `${first.expression} (used under ${ELECTED[first.name]})`
      : first.expression;
    push(`| ${row.names.map((n) => `\`${n}\``).join(", ")} ${versions}${row.note} | ${cell(license)} | ${cell(copyrightCell(first))} |`);
  }
  push();
  push(
    `Every other bundled package is a transitive dependency of these and is listed in full below. ` +
      `DOMPurify is offered under "MPL-2.0 OR Apache-2.0"; vexillum uses it under the Apache License 2.0, ` +
      `whose text is included below (the MPL-2.0 text is included as well).`,
  );
  push();

  push("## What is embedded in the binary");
  push();
  push(EMBEDDED_INVENTORY);
  push();

  push("## npm packages bundled into the whiteboard");
  push();
  push(
    `\`whiteboard.js.gz\` and \`whiteboard.css\` inline ${list.length} npm packages (${packages.length} installed copies, ` +
      `because a few packages are present in more than one version path), taken from the esbuild metafile of the very build ` +
      `that produces them. Packages that esbuild tree-shook away entirely are not listed.`,
  );
  push();
  const counts = new Map();
  for (const p of list) counts.set(p.expression, (counts.get(p.expression) ?? 0) + 1);
  push("| License | Packages |");
  push("| --- | --- |");
  for (const [expression, n] of [...counts].sort((a, b) => b[1] - a[1] || byText(a[0], b[0]))) {
    push(`| ${cell(expression)} | ${n} |`);
  }
  push();
  push("| Package | Version | License | Copyright |");
  push("| --- | --- | --- | --- |");
  const marks = new Set();
  for (const p of list) {
    const license = p.detectedOnly ? `${p.expression} \\*` : p.expression;
    if (p.detectedOnly) marks.add("detected");
    push(`| \`${p.name}\` | ${p.version} | ${cell(license)} | ${cell(copyrightCell(p))} |`);
  }
  push();
  if (marks.has("detected")) {
    push(`\\* The package declares no license field; the identifier is read from its license file.`);
    push();
  }
  const authors = list.filter((p) => p.source === "author");
  if (authors.length > 0) {
    push(
      `† The package's own license files carry no copyright line (they are the unfilled standard text, or the package ships none); ` +
        `the holder shown is the package.json author, not a copyright line: ${authors.map((p) => `\`${p.name}\``).join(", ")}.`,
    );
    push();
  }
  const curated = list.filter((p) => p.source === "curated");
  const none = list.filter((p) => p.source === "none");
  if (curated.length > 0 || none.length > 0) {
    push("‡ Not established from the package itself (marked as unverified):");
    push();
    for (const p of curated) push(`- \`${p.name}\` ${p.version}: ${p.unverified}.`);
    if (none.length > 0) {
      const names = [...new Set(none.map((p) => p.name))];
      push(
        `- ${names.length} package name${names.length === 1 ? "" : "s"} (${names.map((n) => `\`${n}\``).join(", ")}): the npm package ships ` +
          `no license file and no copyright line, only a license field in package.json (${[...new Set(none.map((p) => p.expression))].join(", ")}); ` +
          `the copyright holder is not stated in anything bundled here.`,
      );
    }
    push();
  }
  const withNotice = list.filter((p) => p.notices.length > 0);
  if (withNotice.length > 0) {
    push("### NOTICE files");
    push();
    for (const p of withNotice) {
      for (const n of p.notices) {
        push(`\`${p.name}\` ${p.version}, \`${n.name}\`:`);
        push();
        push(fence(n.text));
        push();
      }
    }
  }

  push("## Fonts");
  push();
  push(
    "The fonts under `internal/forum/assets/whiteboard/fonts/` are copied unchanged from `@excalidraw/excalidraw` " +
      `${list.find((p) => p.name === "@excalidraw/excalidraw")?.version ?? ""} (\`dist/prod/fonts\`). ` +
      "Licence and copyright below are read from each font file's own name table; where the file does not settle the licence it says so.",
  );
  push();
  push("| Family | Files | Version | License | Copyright (from the font file) |");
  push("| --- | --- | --- | --- | --- |");
  for (const f of fonts) {
    push(`| ${f.display} | ${f.files} | ${cell(f.versions.join(", "))} | ${cell(f.curated.license)} | ${cell(fontCopyrightCell(f))} |`);
  }
  push();
  push("Basis for each licence:");
  push();
  for (const f of fonts) push(`- **${f.display}**: ${f.curated.basis}.`);
  push();
  push(XIAOLAI_NOTE);
  push();
  const ofl = oflText(fonts);
  push("### SIL Open Font License 1.1");
  push();
  push(
    "Applies to Assistant, Lilita One, Nunito and Virgil, each with the copyright line (and Reserved Font Name, where one is " +
      "given) shown in the table above. The text is the one the Virgil font file carries.",
  );
  push();
  push(fence(ofl));
  push();
  const cascadia = fonts.find((f) => f.family === "Cascadia");
  if (cascadia?.licenseText[0]) {
    push("### Cascadia Code license text");
    push();
    push("As embedded in the font file (name id 13); it is Microsoft's text based on the SIL OFL, with additional terms.");
    push();
    push(fence(cascadia.licenseText[0]));
    push();
  }
  push("### Comic Shanns");
  push();
  push("MIT License; the copyright lines are in the table above and the license text is the MIT text in the license section below.");
  push();

  push("## Icons in the forum chrome");
  push();
  push(ICONS);
  push();

  push("## Go modules and the Go standard library");
  push();
  if (goMods.length === 0) {
    push(GO_NO_MODULES);
    push();
  } else {
    push("Linked into the `vx` binary (from `go list -deps ./cmd/vx`):");
    push();
    push("| Module | Version | License | Copyright |");
    push("| --- | --- | --- | --- |");
    for (const m of goMods) push(`| \`${m.path}\` | ${m.version} | ${m.id ?? "unknown"} | ${cell(m.lines.join("; "))} |`);
    push();
    for (const m of goMods) {
      push(`License text of \`${m.path}\`:`);
      push();
      push(fence(m.text));
      push();
    }
  }
  push(GO_STD_NOTE);
  push();
  push(fence(GO_LICENSE_TEXT));
  push();

  push("## License texts for the npm packages");
  push();
  push(
    "One text per license; the copyright lines belong to the individual packages in the table above and are left out of the " +
      "texts. Where packages word the same license differently, each wording is printed with the packages that use it.",
  );
  push();
  for (const id of LICENSE_ORDER) {
    const variants = pool.get(id);
    if (!variants) continue;
    const total = variants.reduce((n, v) => n + v.packages.length, 0);
    push(`### ${id}`);
    push();
    push(`Used by ${total} package${total === 1 ? "" : "s"}${id === "Apache-2.0" ? ", including DOMPurify (elected, see the summary)" : ""}.`);
    push();
    variants.forEach((v, i) => {
      if (variants.length > 1) {
        const names = v.packages.length > 6 ? `${v.packages.slice(0, 6).join(", ")} and ${v.packages.length - 6} more` : v.packages.join(", ");
        push(i === 0 ? `Wording used by ${names}:` : `Variant wording used by ${names}:`);
        push();
      }
      push(fence(v.text));
      push();
    });
  }

  const combined = list.flatMap((p) => p.files.filter((f) => f.ids.length > 1).map((f) => ({ p, f })));
  if (combined.length > 0) {
    push("### Combined license files");
    push();
    push(
      "These packages ship one license file that holds more than one license (their own plus the license of code they embed). " +
        "The files are reproduced as shipped, with their copyright lines.",
    );
    push();
    for (const { p, f } of combined) {
      push(`\`${p.name}\` ${p.version}, \`${f.name}\` (${f.ids.join(", ")}):`);
      push();
      push(fence(f.text));
      push();
    }
  }

  push(SLOT_SECTION);
  push();
  return out.join("\n");
}

function repositoryUrl(pkg) {
  const raw = typeof pkg.repository === "string" ? pkg.repository : pkg.repository?.url;
  if (!raw) return "";
  return raw.replace(/^git\+/, "").replace(/^git:\/\//, "https://").replace(/^ssh:\/\/git@/, "https://").replace(/\.git$/, "");
}

// The copyright cell of the package table. ‡ and † point at the footnotes,
// which say how far the line can be trusted.
function copyrightCell(p) {
  if (p.source === "none") {
    const repo = repositoryUrl(p.pkg);
    return `‡ none stated in the package${repo ? ` (project: ${repo})` : ""}`;
  }
  const text = p.lines.join("; ");
  if (p.source === "author") return `† ${text}`;
  if (p.source === "curated") return `‡ ${text}`;
  if (p.source === "banner") return `${text} (package banner)`;
  return text;
}

// -------------------------------------------------------------------- main

async function main() {
  const check = process.argv.includes("--check");
  if (!existsSync(join(HERE, "node_modules"))) {
    throw new Error("node_modules is missing: run `npm ci` in tools/whiteboard-bundle first");
  }
  const roots = await bundledRoots();
  const packages = roots.map(describePackage);
  const fonts = readFonts();
  const goMods = goModules();
  const text = render(packages, fonts, goMods);

  const lock = JSON.parse(readFileSync(LOCKFILE, "utf8"));
  const manifest = {
    note: "Generated by generate-notices.js; do not edit by hand.",
    lockfileSha256: createHash("sha256").update(readFileSync(LOCKFILE)).digest("hex"),
    packages: packages
      .map((p) => ({ name: p.name, version: p.version, path: p.root }))
      .sort((a, b) => byText(a.path, b.path)),
  };
  for (const m of manifest.packages) {
    if (lock.packages[m.path]?.version !== m.version) fail(`${m.path}: installed ${m.version} but package-lock.json says ${lock.packages[m.path]?.version}`);
  }
  const manifestText = `${JSON.stringify(manifest, null, 2)}\n`;

  if (problems.length > 0) {
    console.error(`generate-notices: ${problems.length} problem${problems.length === 1 ? "" : "s"}, nothing written:`);
    for (const p of problems) console.error(`  - ${p}`);
    process.exit(1);
  }

  const warnings = packages.filter((p) => p.source === "author" || p.source === "none" || p.unverified);
  if (warnings.length > 0) {
    console.error("generate-notices: copyright line not established from the package's own license text for:");
    for (const p of [...new Map(warnings.map((w) => [w.label, w])).values()].sort((a, b) => byText(a.label, b.label))) {
      console.error(`  - ${p.label} (${p.unverified ? "unverified claim" : p.source === "author" ? "package.json author" : "none"})`);
    }
  }
  for (const f of fonts) if (f.curated.license === "licence unverified") console.error(`generate-notices: font ${f.display}: licence unverified`);

  if (check) {
    const stale = [];
    if (!existsSync(NOTICES) || readFileSync(NOTICES, "utf8") !== text) stale.push("THIRD-PARTY-NOTICES.md");
    if (!existsSync(MANIFEST) || readFileSync(MANIFEST, "utf8") !== manifestText) stale.push("tools/whiteboard-bundle/bundled-packages.json");
    if (stale.length > 0) {
      console.error(`generate-notices: stale, run \`${REGENERATE}\`: ${stale.join(", ")}`);
      process.exit(1);
    }
    console.log("generate-notices: up to date");
    return;
  }
  writeFileSync(NOTICES, text);
  writeFileSync(MANIFEST, manifestText);
  console.log(`generate-notices: wrote THIRD-PARTY-NOTICES.md (${new Set(packages.map((p) => p.label)).size} packages) and bundled-packages.json`);
}

await main();
