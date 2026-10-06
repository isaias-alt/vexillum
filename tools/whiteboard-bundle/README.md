# whiteboard-bundle (dev-only)

Builds `internal/forum/assets/whiteboard/{whiteboard.js.gz,whiteboard.css,fonts/}` and copies `src/whiteboard-embed.js` to `internal/forum/assets/whiteboard-embed.js`,
the browser bundle that `vexillum forum`'s whiteboard feature go:embeds.

**Never runs in `go build`, `go install`, or the brew/curl install path.**
This directory has its own `package.json`; `node_modules/` and `dist/` are
gitignored and never committed. Node exists only in a maintainer's shell
when regenerating the vendor - never in the `vexillum` binary or in a user's
install of it. See `AGENTS.md` at the repo root: "distribución: binario por
brew/curl... no introduzcas dependencias de runtime (Node, Python) en el
camino de instalación."

## Regenerating the vendor

```sh
cd tools/whiteboard-bundle
npm ci            # installs exactly what package-lock.json pins
npm run build
```

This writes the compiled output straight into
`internal/forum/assets/whiteboard/`. Review the diff and commit it like any
other vendor update.

## Regenerating the third-party notices

`THIRD-PARTY-NOTICES.md` at the repo root is generated, never edited by hand.
Re-run it whenever the bundle, `package-lock.json` or the vendored fonts change
(it is part of the same vendor update as `npm run build`):

```sh
cd tools/whiteboard-bundle
npm ci
npm run notices          # writes THIRD-PARTY-NOTICES.md and bundled-packages.json
npm run notices:check    # regenerates in memory, exits non-zero if either file is stale
```

`generate-notices.js` repeats the esbuild build of `build.js` (same options,
shared through `bundle-options.js`, nothing written) and lists every npm package
that contributes bytes to the output with its version, license, copyright lines
and license text, read from the installed package. The font section is read
from the name tables of the vendored woff2 files, and the Go section from
`go list -deps ./cmd/vx`. The output order is sorted, so reruns are
byte-identical. It exits non-zero, writing nothing, when a bundled package has
no license information, ships a license file it cannot classify, or declares a
license its own file contradicts. The hand-written prose and the curated facts
(headline packages, per-family font licenses, copyright fallbacks) live in
`notices-prose.js`; each curated font claim is re-checked against the font file
on every run.

`bundled-packages.json` records which lockfile entries the bundle inlines (the
lockfile itself cannot say: they are all devDependencies) and the sha256 of the
lockfile it was generated from. `go test ./internal/thirdparty` reads it, the
lockfile and the notices (no node, no network) and fails when the lockfile moved
without a regeneration or a bundled package has no entry in the notices. Like
`build.js`, none of this runs at vexillum's own build or install time.

## What is in here, and what is vendored

Everything under `src/`, `build.js`, `bundle-options.js` and the notices generator
(`generate-notices.js`, `notices-lib.js`, `notices-prose.js`) is vexillum's own code:

- `src/whiteboard-core.js` - the DOM-free surface the frame and the tests
  import; it re-exports three modules:
  - `scene-fit.js` - what a reviewer sees right after conversion: line-break
    tags, unique ids, node sizing from the measured label (growing around the
    node's centre, inscribed rectangle for ellipses and diamonds), attached
    arrows following grown nodes, image-board detection. Text measurement is
    injected, so none of it needs a browser.
  - `scene-diff.js` - edit detection and the edit summary the agent receives
    (one change list serves both, so they cannot disagree).
  - `scene-record.js` - the stored record (`format` 2), the opening decision
    (convert, reopen or ask) and the link filter.
- `src/frame-scene.js` - the parts of the frame that need Excalidraw or the
  Mermaid converter: conversion, restoring, font-aware measuring, the PNG
  export.
- `src/frame-dom.js` - plain-DOM pieces of the frame page: the status line, the
  link confirmation dialog, the opening choice and the failure panel.
- `src/whiteboard-frame.js` - the page inside each whiteboard iframe: mounts
  Excalidraw on one board and speaks the `vxb1.` postMessage protocol with the
  embedder (the message list is documented at the top of the embed).
- `src/whiteboard-embed.js` - injected into artifacts that contain `.mermaid`
  blocks; replaces them with frames, owns the lock cover, fullscreen overlay,
  persistence (through the artifact SDK's internal bridge, never the network)
  and the live theme switch. `build.js` copies it to
  `internal/forum/assets/whiteboard-embed.js`; the two must stay identical.
- `src/whiteboard-frame.css` - the frame's styling, all on the forum `--fr-*`
  tokens.

Tests: `node --test "test/*.test.js"` here runs the core and frame-DOM suites;
`go test ./internal/forum/` runs them too, plus the embed against a fake DOM
(`internal/forum/testdata/whiteboard_embed_dom_test.js`) and the real-Chrome
whiteboard tests (skipped when no Chrome is found).

What is vendored is the set of npm packages the bundle inlines, used through
their public APIs and pinned exactly: `mermaid@11.12.1`,
`@excalidraw/mermaid-to-excalidraw@2.2.2`, `@excalidraw/excalidraw@0.18.1`,
`react@18.3.1`, `react-dom@18.3.1`. Keep them pinned, not ranged - a converter or
Excalidraw upgrade should be a deliberate, tested bump, not a silent
`npm install` drift. See `THIRD-PARTY-NOTICES.md` at the repo root for their
licenses (generated; see above).
