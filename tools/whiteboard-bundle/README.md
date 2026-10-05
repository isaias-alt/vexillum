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

## What is in here, and what is vendored

Everything under `src/` and `build.js` is vexillum's own code:

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
licenses.
