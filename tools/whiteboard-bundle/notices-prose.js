// The hand-written parts of THIRD-PARTY-NOTICES.md and the curated facts the
// generator cannot read from a package: which packages are the headline ones,
// where a copyright line has to come from when the package ships none, and what
// each vendored font family is licensed under. Everything that can be read from
// the bundle (names, versions, copyright lines, license texts) is generated in
// generate-notices.js; every curated fact here is checked against the real
// files there, so a stale entry fails the generator instead of lying quietly.
// Dev-only, like everything in this directory (see README.md).

export const REGENERATE = "cd tools/whiteboard-bundle && npm run notices";

// The packages vexillum chose and pins itself, in the order the summary table
// lists them. react and react-dom share one row.
export const HEADLINE = [
  { names: ["@excalidraw/excalidraw"], note: "" },
  { names: ["@excalidraw/mermaid-to-excalidraw"], note: "" },
  { names: ["mermaid"], note: " (exact, bundled for the converter)" },
  { names: ["react", "react-dom"], note: "" },
  { names: ["dompurify"], note: "" },
];

// The license vexillum elects where a package offers a choice. DOMPurify is
// "MPL-2.0 OR Apache-2.0": either one is enough, and the Apache text is
// already carried for other packages.
export const ELECTED = {
  dompurify: "Apache-2.0",
};

// Copyright lines for packages whose own files state none. A fixed `lines`
// entry is a claim that cannot be checked from the installed package and is
// flagged as such in the notices; a `file` + `pattern` entry is read from a
// file of the package (a banner comment) and therefore first-hand.
export const COPYRIGHT_FALLBACKS = {
  "@excalidraw/excalidraw": {
    lines: ["Copyright (c) 2020 Excalidraw"],
    unverified:
      "carried over from the previous notice; the npm package ships no LICENSE file, so the line cannot be checked offline",
  },
  dompurify: {
    file: "dist/purify.min.js",
    pattern: /\(c\) Cure53 and other contributors/,
    prefix: "",
  },
};

// License texts that live in a package but not in a LICENSE file: the Zlib
// license of pako is the header comment of its lib/zlib sources.
export const EXTRA_LICENSE_SOURCES = {
  pako: [{ id: "Zlib", file: "lib/zlib/zstream.js", comment: "//" }],
};

// What each vendored font family is licensed under, and the evidence the
// generator must still find in the font files for the claim to stand.
//   license  SPDX id, or "unverified" when no first-hand evidence settles it
//   holds    test on the font's name table that must be true (else the
//            generator fails and this entry needs a human look)
//   basis    one line for the notices saying where the claim comes from
const names13or14 = (n) => `${n[13] ?? ""} ${n[14] ?? ""}`;
export const FONT_LICENSES = {
  Assistant: {
    license: "OFL-1.1",
    holds: (n) => /Open Font License/.test(n[13] ?? "") && /Version 1\.1/.test(n[13] ?? ""),
    basis: "the font's name table (license description, name id 13) says SIL Open Font License 1.1",
  },
  Cascadia: {
    license: "OFL-1.1, with Microsoft's additional terms (text below)",
    holds: (n) => /based on the SIL Open Font license/i.test(n[13] ?? ""),
    basis:
      "the font's own license text (name id 13) is Microsoft's text based on the SIL OFL, with extra terms; it is reproduced in full below",
  },
  ComicShanns: {
    license: "MIT",
    holds: (n) => /^MIT License/.test(n[0] ?? "") && /Permission is hereby granted/.test(n[0] ?? ""),
    basis: "the font's name table (copyright, name id 0) carries the full MIT License text and five copyright lines",
  },
  Excalifont: {
    license: "licence unverified",
    holds: (n) => n[13] === undefined && n[14] === undefined && /All rights reserved/.test(n[0] ?? ""),
    basis:
      "licence unverified: the font file carries only a copyright line (\"All rights reserved\") and no licence text or URL; the npm package that ships it (@excalidraw/excalidraw) declares MIT for itself in package.json and ships no LICENSE file, and its README says nothing about the fonts. The Excalidraw repository was not consulted (no network)",
  },
  Liberation: {
    license: "licence unverified",
    holds: (n) => /license agreement under which you accepted/.test(n[13] ?? "") && /^Version 1\.0/.test(n[5] ?? ""),
    basis:
      "licence unverified: this is Liberation Sans 1.05 (Ascender Corporation, 2007). The font file says only that use \"is subject to the license agreement under which you accepted the Liberation font software\" and points to http://www.ascendercorp.com/liberation.html. Nothing in the file calls it the SIL OFL, and no licence text is in the file or in the npm package, so the actual terms are not established here and must not be assumed to be the OFL",
  },
  Lilita: {
    license: "OFL-1.1",
    holds: (n) => /OFL/.test(names13or14(n)),
    basis:
      "the font's name table (license URL, name id 14) points to http://scripts.sil.org/OFL; the file names the OFL by URL only and carries no text or version, so the OFL 1.1 text below (the version the Virgil file carries) is the one printed",
  },
  Nunito: {
    license: "OFL-1.1",
    holds: (n) => /OFL/.test(names13or14(n)),
    basis:
      "the font's name table (license URL, name id 14) points to https://scripts.sil.org/OFL; the file names the OFL by URL only and carries no text or version, so the OFL 1.1 text below (the version the Virgil file carries) is the one printed",
  },
  Virgil: {
    license: "OFL-1.1",
    holds: (n) => /SIL Open Font License, Version 1\.1/.test(n[13] ?? "") && /SIL OPEN FONT LICENSE Version 1\.1/.test(n[13] ?? ""),
    basis: "the font's name table (name id 13) states SIL Open Font License 1.1 and carries its full text",
  },
};

export const FONT_DISPLAY = {
  Assistant: "Assistant",
  Cascadia: "Cascadia Code",
  ComicShanns: "Comic Shanns",
  Excalifont: "Excalifont",
  Liberation: "Liberation Sans",
  Lilita: "Lilita One",
  Nunito: "Nunito",
  Virgil: "Virgil",
};

export const XIAOLAI_NOTE = `The Xiaolai family (CJK glyphs, ~12 MB) is intentionally not vendored;
Excalidraw falls back to its CDN or the system font for those glyphs.`;

export const INTRO = `vexillum is one Go binary. Besides its own code it carries the third-party
software and assets listed in this file: the browser bundle and the fonts of
the whiteboard feature of \`vx forum\` (vendored under
\`internal/forum/assets/whiteboard/\`, see that package's doc comment), a few
icon shapes in the forum chrome, and the Go modules linked into the binary.
Each component remains under its own license; the notices below satisfy their
attribution requirements.

The bundle is built by \`tools/whiteboard-bundle/build.js\` (dev-only, never run
at vexillum's own build or install time, see that directory's README) and
go:embedded into the binary. This file is generated from the same esbuild
build, the packages' own LICENSE files and the font files' name tables, so it
lists exactly what is bundled.`;

export const EMBEDDED_INVENTORY = `| Embedded in the binary | Origin | Where it is noticed |
| --- | --- | --- |
| \`internal/forum/assets/whiteboard/whiteboard.js.gz\`, \`whiteboard.css\` | npm packages bundled by esbuild | [npm packages](#npm-packages-bundled-into-the-whiteboard) |
| \`internal/forum/assets/whiteboard/fonts/\` | fonts shipped by \`@excalidraw/excalidraw\` | [Fonts](#fonts) |
| \`internal/forum/assets/chrome/chrome.html\` (inline SVG icons) | Feather icon geometry | [Icons](#icons-in-the-forum-chrome) |
| \`internal/forum/assets/chrome/*\`, \`whiteboard-embed.js\` | vexillum's own code | none needed |
| \`internal/forum/assets/favicon.svg\`, \`favicon.ico\` | vexillum's own mark | none needed |
| \`internal/commander/core.*.md\`, \`internal/models/default-models.json\`, \`skills/\` | vexillum's own text and data | none needed |
| Go modules and the Go standard library | linked into the binary | [Go](#go-modules-and-the-go-standard-library) |`;

export const ICONS = `The inline SVG icons in \`internal/forum/assets/chrome/chrome.html\` (warning
triangle, tag, image, moon, sun) are small stroke glyphs whose geometry matches
the Feather icon set (\`alert-triangle\`, \`tag\`, \`image\`, \`moon\`, \`sun\`) with
coordinates rounded. Feather is MIT licensed; Lucide, its maintained fork, is
ISC licensed and carries the Feather MIT notice below in its own LICENSE
(\`lucide-react\` 1.47.0 reproduces it). This attribution rests on comparing
the path data with the published icons, not on a file shipped in the binary,
so treat the match as identified, not certified. No Feather or Lucide code is
linked.

The Feather notice, as reproduced by Lucide:

\`\`\`
Copyright (c) 2013-present Cole Bemis

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
\`\`\``;

export const GO_STD_NOTE = `The Go standard library is compiled into every Go binary, including the
subsets of \`golang.org/x/crypto\`, \`golang.org/x/net\` and \`golang.org/x/text\`
that it vendors. It is licensed by The Go Authors under the same BSD-3-Clause
text as the modules above (the toolchain used to generate this file ships no
separate LICENSE file, so that equality is stated by the Go project, not
checked here).`;

export const SLOT_SECTION = `## Inspired by \`internal/slot/\` (AGENTS.md marker block repair)

\`internal/slot/slot.go\` (\`Repair\`) is inspired by the markdown-section
injection and orphan-marker stripping (\`InjectMarkdownSection\`,
\`stripOrphanMarkers\`) of
[gentle-ai](https://github.com/Gentleman-Programming/gentle-ai), MIT
licensed. No code is vendored or linked; the approach (recover from orphan
markers and duplicate blocks by dropping the stray ones) is reimplemented in
Go with different semantics: it only runs after explicit confirmation, keeps
the first well-formed pair, and never touches text outside the markers. The
8-hex-char content hash in the BEGIN marker follows the convention used by
Beads.`;
