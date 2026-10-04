// Maintainer-only build for the whiteboard browser bundle that vexillum
// commits under internal/forum/assets/ and go:embeds. Run it with
// `npm run build` from this directory (see README.md); nothing here runs
// during `go build` or on a user's machine.
//
// Outputs, relative to the vexillum repo:
//   internal/forum/assets/whiteboard/whiteboard.js.gz   the frame app, pre-gzipped
//   internal/forum/assets/whiteboard/whiteboard.css     Excalidraw's CSS + the frame CSS
//   internal/forum/assets/whiteboard/fonts/             the canvas fonts Excalidraw fetches lazily
//   internal/forum/assets/whiteboard-embed.js           the script injected into artifacts

import { cp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { gzipSync } from "node:zlib";

import * as esbuild from "esbuild";

const SCRATCH = "dist";
const ASSETS = "../../internal/forum/assets";
const TARGET = `${ASSETS}/whiteboard`;

// Excalidraw loads canvas fonts on demand from <asset path>/fonts/<family>.
// Every family ships except Xiaolai: its CJK glyphs weigh about 12 MB, and
// without them those characters simply fall back to the system font.
const FONT_FAMILIES = [
  "Assistant",
  "Cascadia",
  "ComicShanns",
  "Excalifont",
  "Liberation",
  "Lilita",
  "Nunito",
  "Virgil",
];
const FONT_SOURCE = "node_modules/@excalidraw/excalidraw/dist/prod/fonts";

// One self-contained script: Excalidraw, the Mermaid converter with its pinned
// mermaid, and React are all inlined, so the whiteboard works with no network
// once the vexillum binary exists.
async function bundleFrame() {
  await esbuild.build({
    entryPoints: { whiteboard: "src/whiteboard-frame.js" },
    outdir: SCRATCH,
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
}

async function stageFonts() {
  const fontsDir = `${SCRATCH}/fonts`;
  await mkdir(fontsDir, { recursive: true });
  for (const family of FONT_FAMILIES) {
    await cp(`${FONT_SOURCE}/${family}`, `${fontsDir}/${family}`, { recursive: true });
  }
}

// The Go handler serves the script with Content-Encoding: gzip as stored, so
// the repo and the embedded binary carry only the compressed form (the raw
// output is several megabytes) and nothing is compressed per request.
async function compressScript() {
  const raw = await readFile(`${SCRATCH}/whiteboard.js`);
  await writeFile(`${SCRATCH}/whiteboard.js.gz`, gzipSync(raw, { level: 9 }));
}

async function publish() {
  await mkdir(TARGET, { recursive: true });
  await rm(`${TARGET}/fonts`, { recursive: true, force: true });
  await cp(`${SCRATCH}/whiteboard.js.gz`, `${TARGET}/whiteboard.js.gz`);
  await cp(`${SCRATCH}/whiteboard.css`, `${TARGET}/whiteboard.css`);
  await cp(`${SCRATCH}/fonts`, `${TARGET}/fonts`, { recursive: true });
  // The embed script needs no bundling. It is edited under src/ next to the
  // rest of the whiteboard code and copied out here, so the committed copy
  // cannot drift from its source.
  await cp("src/whiteboard-embed.js", `${ASSETS}/whiteboard-embed.js`);
}

await rm(SCRATCH, { recursive: true, force: true });
await mkdir(SCRATCH, { recursive: true });

await bundleFrame();
await stageFonts();
await compressScript();
await publish();

console.log(`Whiteboard bundle written to ${TARGET}`);
