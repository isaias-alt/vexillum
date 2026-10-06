// The esbuild options for the whiteboard frame bundle, shared by build.js
// (which writes the bundle) and generate-notices.js (which reads the metafile
// of the very same build to list what is inlined). Keeping them in one place
// means the notices cannot describe a different bundle than the one shipped.
// Dev-only, like everything in this directory (see README.md).

// Excalidraw loads canvas fonts on demand from <asset path>/fonts/<family>.
// Every family ships except Xiaolai: its CJK glyphs weigh about 12 MB, and
// without them those characters simply fall back to the system font.
export const FONT_FAMILIES = [
  "Assistant",
  "Cascadia",
  "ComicShanns",
  "Excalifont",
  "Liberation",
  "Lilita",
  "Nunito",
  "Virgil",
];

export function frameBuildOptions(outdir) {
  return {
    entryPoints: { whiteboard: "src/whiteboard-frame.js" },
    outdir,
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
  };
}
