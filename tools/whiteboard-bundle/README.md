# whiteboard-bundle (dev-only)

Builds `internal/review/assets/whiteboard/{whiteboard.js.gz,whiteboard.css,fonts/}`,
the browser bundle that `vexillum review`'s whiteboard feature go:embeds.

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
npm install
npm run build
```

This writes the compiled output straight into
`internal/review/assets/whiteboard/`. Review the diff and commit it like any
other vendor update.

## What's vendored, and from where

`src/whiteboard-core.js`, `src/whiteboard-frame.js`, and
`src/whiteboard-frame.css` are adapted from `upstream` at
**v0.1.80** (`src/whiteboard-core.js`, `src/whiteboard-frame.js`,
`src/whiteboard-frame.css` in that repo), MIT licensed. See
`THIRD-PARTY-NOTICES.md` at the vexillum repo root for full attribution, and
each file's header comment for what changed. Treat these as vexillum's own
code, not an opaque vendor blob: forum-tool ships a high release cadence and
keeps patching real bugs in this exact conversion path
(`mermaid-to-excalidraw#110` and friends) - diff against a newer forum-tool
tag when something in the conversion looks wrong before assuming it's a
vexillum-specific bug.

The pinned npm packages (`mermaid@11.12.1`,
`@excalidraw/mermaid-to-excalidraw@2.2.2`, `@excalidraw/excalidraw@0.18.1`,
`react@18.3.1`, `react-dom@18.3.1`) are the same exact versions forum-tool
vendors as of that release. Keep them pinned, not ranged - a converter or
Excalidraw upgrade should be a deliberate, tested vendor bump, not a silent
`npm install` drift.
