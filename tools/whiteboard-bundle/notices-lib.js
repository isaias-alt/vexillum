// Pure helpers for generate-notices.js: license classification, copyright
// extraction, SPDX expression splitting, package-root lookup and the woff2
// name-table reader. No file system access except in readWoff2Names, so the
// unit tests (test/notices-lib.test.js) need no node_modules.
// Dev-only, like everything in this directory (see README.md).

import { readFileSync } from "node:fs";
import { brotliDecompressSync } from "node:zlib";

// The squashed form of a license text: lower case, one space between words.
export function squash(text) {
  return text.toLowerCase().replace(/\s+/g, " ").trim();
}

// Every license a text contains, as SPDX identifiers, sorted. Most files hold
// one license; some hold several (d3-geo ships ISC plus the MIT license of the
// GeographicLib code it embeds). The match is on a distinctive phrase of each
// license, not on a title, because many files carry no title at all.
export function licenseIds(text) {
  const t = squash(text);
  const ids = [];
  if (t.includes("apache license") && t.includes("version 2.0")) ids.push("Apache-2.0");
  if (t.includes("mozilla public license") && t.includes("version 2.0")) ids.push("MPL-2.0");
  if (t.includes("permission is hereby granted, free of charge")) ids.push("MIT");
  if (t.includes("this is free and unencumbered software released into the public domain")) ids.push("Unlicense");
  if (t.includes("cc0 1.0 universal") || t.includes("creative commons legal code")) ids.push("CC0-1.0");
  if (t.includes("altered source versions must be plainly marked")) ids.push("Zlib");
  if (t.includes("redistribution and use in source and binary forms")) {
    ids.push(t.includes("neither the name") ? "BSD-3-Clause" : "BSD-2-Clause");
  }
  if (t.includes("permission to use, copy, modify, and/or distribute this software for any purpose with or without fee")) {
    ids.push(
      t.includes("provided that the above copyright notice and this permission notice appear in all copies")
        ? "ISC"
        : "0BSD",
    );
  }
  return ids.sort();
}

// The one license of a text, or null when it holds none or several.
export function classifyLicense(text) {
  const ids = licenseIds(text);
  return ids.length === 1 ? ids[0] : null;
}

// The license identifiers named by an SPDX expression such as
// "(MPL-2.0 OR Apache-2.0)" or "MIT AND Zlib", in order of appearance.
export function spdxIds(expression) {
  return expression
    .replace(/[()]/g, " ")
    .split(/\s+(?:AND|OR|WITH)\s+|\s+/i)
    .map((s) => s.trim())
    .filter((s) => s && !/^(AND|OR|WITH)$/i.test(s));
}

// What package.json declares as the license, as one SPDX string, or null.
// The legacy {type} object and the legacy `licenses` array are understood.
export function declaredLicense(pkg) {
  const one = (l) => (typeof l === "string" ? l : l && typeof l.type === "string" ? l.type : null);
  const direct = one(pkg.license);
  if (direct) return direct;
  if (Array.isArray(pkg.licenses) && pkg.licenses.length > 0) {
    const ids = pkg.licenses.map(one).filter(Boolean);
    if (ids.length > 0) return ids.join(" OR ");
  }
  return null;
}

// A copyright line is one that starts with "Copyright", "(c)" or the (c)
// sign, once a comment marker is stripped. The boilerplate that mentions a
// copyright notice or holder in the middle of a sentence, and the unfilled
// template of the Apache appendix, are not copyright lines.
const COPYRIGHT_START = /^(?:copyright\b|\(c\)|©)/i;
const COPYRIGHT_NOT =
  /copyright (?:notice|holders?|owner|and license|license|and related rights)\b|\[(?:yyyy|year|name[^\]]*|fullname|owner[^\]]*)\]|<year>|<copyright holder>|\{year\}/i;

// "(c)" alone also starts the list items of the Apache and MPL texts, so that
// form must be followed by a year; "Copyright ..." needs none.
export function isCopyrightLine(line) {
  const t = line.replace(/^[\s*#/]+/, "").trim();
  if (!COPYRIGHT_START.test(t) || COPYRIGHT_NOT.test(t)) return false;
  return /^copyright\b/i.test(t) || /^(?:\(c\)|\u00a9)\s*\d{4}/i.test(t);
}

export function copyrightLines(text) {
  const out = [];
  for (const line of text.split(/\r?\n/)) {
    if (!isCopyrightLine(line)) continue;
    const t = line.replace(/^[\s*#/]+/, "").trim().replace(/\s+/g, " ");
    if (!out.includes(t)) out.push(t);
  }
  return out;
}

// The text of a license file with its copyright and title lines taken out and
// its whitespace squashed: two files that differ only in who holds the
// copyright, or in the title, have the same body.
export function licenseBody(text) {
  const kept = text
    .split(/\r?\n/)
    .filter((l) => !isCopyrightLine(l))
    .join("\n");
  return squash(kept)
    .replace(/^\(?the (?:mit|isc|bsd[\w -]*|zlib) licen[sc]e(?: \([^)]*\))?\)?:? ?/, "")
    .replace(/^(?:mit|isc|bsd[\w -]*|zlib|0bsd) licen[sc]e:? ?/, "")
    .replace(/[‘’]/g, "'")
    .replace(/[“”]/g, '"');
}

// The text to print for a license file: its own text with the copyright
// lines left out (they are listed per package) and blank runs squeezed.
export function printableLicense(text) {
  const kept = text
    .replace(/\r\n/g, "\n")
    .split("\n")
    .filter((l) => !isCopyrightLine(l))
    .join("\n");
  return kept.replace(/\n{3,}/g, "\n\n").replace(/^\s*\n/, "").trimEnd();
}

// The package root of a path under node_modules, honouring nested copies and
// scoped names: "node_modules/a/node_modules/@s/b/x.js" -> "node_modules/a/node_modules/@s/b".
export function packageRoot(path) {
  const marker = "node_modules/";
  const i = path.lastIndexOf(marker);
  if (i < 0) return null;
  const rest = path.slice(i + marker.length).split("/");
  const name = rest[0].startsWith("@") ? `${rest[0]}/${rest[1]}` : rest[0];
  return path.slice(0, i + marker.length) + name;
}

function readBase128(buf, cursor) {
  let value = 0;
  for (let i = 0; i < 5; i++) {
    const byte = buf[cursor.at++];
    value = (value * 128) + (byte & 0x7f);
    if ((byte & 0x80) === 0) return value;
  }
  throw new Error("woff2: bad UIntBase128");
}

const WOFF2_KNOWN_TAGS = [
  "cmap", "head", "hhea", "hmtx", "maxp", "name", "OS/2", "post", "cvt ", "fpgm", "glyf", "loca", "prep",
  "CFF ", "VORG", "EBDT", "EBLC", "gasp", "hdmx", "kern", "LTSH", "PCLT", "VDMX", "vhea", "vmtx", "BASE",
  "GDEF", "GPOS", "GSUB", "EBSC", "JSTF", "MATH", "CBDT", "CBLC", "COLR", "CPAL", "SVG ", "sbix", "acnt",
  "avar", "bdat", "bloc", "bsln", "cvar", "fdsc", "feat", "fmtx", "fvar", "gvar", "hsty", "just", "lcar",
  "mort", "morx", "opbd", "prop", "trak", "Zapf", "Silf", "Glat", "Gloc", "Feat", "Sill",
];

// The name table of a woff2 font as {nameID: string}. The name table is never
// transformed by woff2, so after the single Brotli stream is inflated it sits
// at the offset the table directory implies. The Windows English record wins
// over any other platform record for the same name id.
export function readWoff2Names(file) {
  const buf = readFileSync(file);
  if (buf.toString("latin1", 0, 4) !== "wOF2") throw new Error(`${file}: not a woff2 file`);
  const numTables = buf.readUInt16BE(12);
  const compressedSize = buf.readUInt32BE(20);
  const cursor = { at: 48 };
  const tables = [];
  for (let i = 0; i < numTables; i++) {
    const flags = buf[cursor.at++];
    const index = flags & 0x3f;
    let tag;
    if (index === 63) {
      tag = buf.toString("latin1", cursor.at, cursor.at + 4);
      cursor.at += 4;
    } else {
      tag = WOFF2_KNOWN_TAGS[index];
    }
    const version = (flags >> 6) & 3;
    const origLength = readBase128(buf, cursor);
    const transformed = tag === "glyf" || tag === "loca" ? version === 0 : version !== 0;
    tables.push({ tag, length: transformed ? readBase128(buf, cursor) : origLength });
  }
  const data = brotliDecompressSync(buf.subarray(cursor.at, cursor.at + compressedSize));
  let offset = 0;
  for (const t of tables) {
    t.offset = offset;
    offset += t.length;
  }
  const nameTable = tables.find((t) => t.tag === "name");
  if (!nameTable) throw new Error(`${file}: no name table`);
  const d = data.subarray(nameTable.offset, nameTable.offset + nameTable.length);
  const count = d.readUInt16BE(2);
  const stringOffset = d.readUInt16BE(4);
  const names = {};
  const windowsEnglish = new Set();
  for (let i = 0; i < count; i++) {
    const r = 6 + i * 12;
    const platform = d.readUInt16BE(r);
    const language = d.readUInt16BE(r + 4);
    const id = d.readUInt16BE(r + 6);
    const length = d.readUInt16BE(r + 8);
    const start = stringOffset + d.readUInt16BE(r + 10);
    const raw = d.subarray(start, start + length);
    const text = platform === 3 || platform === 0
      ? Buffer.from(raw).swap16().toString("utf16le")
      : raw.toString("latin1");
    const preferred = platform === 3 && language === 0x409;
    if (preferred || !(id in names)) {
      if (preferred || !windowsEnglish.has(id)) names[id] = text;
      if (preferred) windowsEnglish.add(id);
    }
  }
  return names;
}
