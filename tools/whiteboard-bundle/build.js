// Dev-only build script. Produces the static browser bundle that
// internal/forum/assets/whiteboard/ commits and go:embeds - see README.md.
// Adapted from upstream's scripts/build.js (MIT, see
// THIRD-PARTY-NOTICES.md at the repo root), trimmed to only the whiteboard
// entry point vexillum needs.
import { cp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { gzipSync } from "node:zlib";

import * as esbuild from "esbuild";

const outDir = "dist";
const finalDir = "../../internal/forum/assets/whiteboard";

await rm(outDir, { recursive: true, force: true });
await mkdir(outDir, { recursive: true });

// Whiteboard frame: a self-contained browser bundle (Excalidraw + the
// Mermaid converter + its exactly-pinned mermaid + React) served from
// /whiteboard-assets/ by an embedded frame for every rendered Mermaid
// diagram in a `.mermaid` container. Everything is vendored so the
// whiteboard works fully offline once the vexillum binary is built.
await esbuild.build({
  entryPoints: { whiteboard: "src/whiteboard-frame.js" },
  outdir: outDir,
  bundle: true,
  minify: true,
  format: "iife",
  platform: "browser",
  conditions: ["production"],
  loader: { ".woff2": "file", ".woff": "file", ".ttf": "file" },
  define: {
    "process.env.NODE_ENV": '"production"',
    "process.env.IS_PREACT": '"false"',
  },
});

// Excalidraw lazily fetches canvas fonts from `EXCALIDRAW_ASSET_PATH/fonts/`.
// Vendor every family except Xiaolai (12 MB of CJK glyphs; those fall back
// to the system font when missing locally).
const fontFamilies = ["Assistant", "Cascadia", "ComicShanns", "Excalifont", "Liberation", "Lilita", "Nunito", "Virgil"];
await mkdir(`${outDir}/fonts`, { recursive: true });
for (const family of fontFamilies) {
  await cp(`node_modules/@excalidraw/excalidraw/dist/prod/fonts/${family}`, `${outDir}/fonts/${family}`, {
    recursive: true,
  });
}

// Commit the gzip-precompressed JS, not the ~7.3 MB raw output: the Go
// handler serves it with Content-Encoding: gzip directly (compress/gzip
// negotiation, no re-gzip per request), cutting the binary's embedded cost
// to roughly the compressed size instead of the raw one.
const js = await readFile(`${outDir}/whiteboard.js`);
await writeFile(`${outDir}/whiteboard.js.gz`, gzipSync(js, { level: 9 }));

await mkdir(finalDir, { recursive: true });
await rm(`${finalDir}/fonts`, { recursive: true, force: true });
await cp(`${outDir}/whiteboard.js.gz`, `${finalDir}/whiteboard.js.gz`);
await cp(`${outDir}/whiteboard.css`, `${finalDir}/whiteboard.css`);
await cp(`${outDir}/fonts`, `${finalDir}/fonts`, { recursive: true });

console.log(`Whiteboard bundle written to ${finalDir}`);
