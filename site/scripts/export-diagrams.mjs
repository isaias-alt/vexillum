// Exports every Excalidraw scene in diagrams/*.excalidraw to two SVGs under
// public/diagrams/: <name>.light.svg and <name>.dark.svg, using Excalidraw's own
// exportToSvg. The docs pages show them with components/Diagram.tsx, so the
// page never ships Excalidraw. Run with `pnpm diagrams`.
//
// Excalidraw is a browser library: the script bundles it with esbuild and runs
// the exporter in headless Google Chrome (playwright-core, no browser download;
// point CHROME_PATH at another Chromium-based binary if Chrome is not installed).
// The fonts a scene uses are served from the package itself and inlined into
// each SVG, so the images render the same everywhere, offline.
//
// Scenes are authored with the light theme's site tokens (app/globals.css).
// The dark variant is the same scene with each color swapped for its dark token.

import { build } from "esbuild";
import { chromium } from "playwright-core";
import { mkdir, readdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const SITE = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const SRC = path.join(SITE, "diagrams");
const OUT = path.join(SITE, "public", "diagrams");
const EXCALIDRAW_DIST = path.join(
  SITE,
  "node_modules/@excalidraw/excalidraw/dist/prod",
);

// Light token -> dark token, by hex. Every color a scene uses must be a key;
// the export fails on one that is not, so a stray color cannot slip into either
// image unchecked.
const DARK = {
  "#1a1d22": "#e9eaec", // --fr-text
  "#5a5e66": "#a0a3a9", // --fr-text-secondary
  "#1f4e79": "#6fa1cb", // --fr-accent
  "#9c7a3f": "#c9a15a", // --fr-bronze
  "#ffffff": "#1c1f24", // --fr-surface
  "#dfe7ef": "#222a33", // --fr-accent-soft over --fr-bg
  "#e6e0d4": "#2e2a23", // --fr-bronze-soft over --fr-bg
  transparent: "transparent",
};
const LIGHT = Object.fromEntries(Object.keys(DARK).map((k) => [k, k]));

// Same origin for the blank page and the assets Excalidraw fetches (fonts),
// answered from node_modules by the route handler below.
const ORIGIN = "http://excalidraw.local";

async function bundleExcalidraw() {
  const out = await build({
    stdin: {
      contents:
        'import * as lib from "@excalidraw/excalidraw"; window.ExcalidrawLib = lib;',
      resolveDir: SITE,
    },
    bundle: true,
    write: false,
    format: "iife",
    platform: "browser",
    loader: { ".css": "empty" },
    define: { "process.env.NODE_ENV": '"production"' },
    logLevel: "error",
  });
  return out.outputFiles[0].text;
}

function recolor(elements, palette) {
  return elements.map((el) => {
    const next = { ...el };
    for (const key of ["strokeColor", "backgroundColor"]) {
      const light = el[key]?.toLowerCase();
      if (light === undefined) continue;
      if (!(light in palette)) {
        throw new Error(`${el.id}: ${key} ${el[key]} is not a site token (see DARK in scripts/export-diagrams.mjs)`);
      }
      next[key] = palette[light];
    }
    return next;
  });
}

async function main() {
  const names = (await readdir(SRC)).filter((f) => f.endsWith(".excalidraw"));
  if (names.length === 0) throw new Error(`no .excalidraw scenes in ${SRC}`);
  await mkdir(OUT, { recursive: true });

  const bundle = await bundleExcalidraw();
  const browser = await chromium.launch(
    process.env.CHROME_PATH
      ? { executablePath: process.env.CHROME_PATH }
      : { channel: "chrome" },
  );
  try {
    const page = await browser.newPage();
    await page.route(`${ORIGIN}/**`, async (route) => {
      const { pathname } = new URL(route.request().url());
      if (pathname === "/") {
        return route.fulfill({ contentType: "text/html", body: "<!doctype html>" });
      }
      const file = path.join(EXCALIDRAW_DIST, decodeURIComponent(pathname));
      if (!file.startsWith(EXCALIDRAW_DIST + path.sep)) return route.abort();
      try {
        return route.fulfill({ body: await readFile(file) });
      } catch {
        return route.fulfill({ status: 404 });
      }
    });
    page.on("pageerror", (err) => {
      throw err;
    });
    await page.addInitScript(`window.EXCALIDRAW_ASSET_PATH = "${ORIGIN}/";`);
    await page.goto(`${ORIGIN}/`);
    await page.addScriptTag({ content: bundle });

    for (const name of names) {
      const scene = JSON.parse(await readFile(path.join(SRC, name), "utf8"));
      const base = name.replace(/\.excalidraw$/, "");
      for (const [theme, palette] of [
        ["light", LIGHT],
        ["dark", DARK],
      ]) {
        const elements = recolor(scene.elements, palette);
        const svg = await page.evaluate(
          async ({ elements, files }) => {
            const { exportToSvg } = window.ExcalidrawLib;
            const el = await exportToSvg({
              elements,
              files,
              appState: { exportBackground: false },
              exportPadding: 12,
            });
            return el.outerHTML;
          },
          { elements, files: scene.files ?? {} },
        );
        const file = path.join(OUT, `${base}.${theme}.svg`);
        await writeFile(file, svg + "\n");
        console.log(`${path.relative(SITE, file)} (${(svg.length / 1024).toFixed(0)} KB)`);
      }
    }
  } finally {
    await browser.close();
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
