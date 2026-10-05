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

- `src/whiteboard-core.js` - pure logic (edit summaries, the convert/restore/ask
  decision, label and node sizing, link filtering). No DOM, no Excalidraw.
- `src/whiteboard-frame.js` - the page inside each whiteboard iframe: mounts
  Excalidraw on one diagram and speaks the `vx-whiteboard:*` postMessage
  protocol with the embedder.
- `src/whiteboard-embed.js` - injected into artifacts that contain `.mermaid`
  blocks; replaces them with frames, runs fullscreen and teardown, follows the
  forum's live theme switch. `build.js` copies it to
  `internal/forum/assets/whiteboard-embed.js`.
- `src/whiteboard-frame.css` - the frame's styling, all on the forum `--fr-*`
  tokens.

Tests: `node --test "test/*.test.js"` here runs the core suite; `go test
./internal/forum/` runs it too, plus the embed against a fake DOM
(`internal/forum/testdata/whiteboard_embed_dom_test.js`) and the real-Chrome
whiteboard tests.

What is vendored is the set of npm packages the bundle inlines, used through
their public APIs and pinned exactly: `mermaid@11.12.1`,
`@excalidraw/mermaid-to-excalidraw@2.2.2`, `@excalidraw/excalidraw@0.18.1`,
`react@18.3.1`, `react-dom@18.3.1`. Keep them pinned, not ranged - a converter or
Excalidraw upgrade should be a deliberate, tested bump, not a silent
`npm install` drift. See `THIRD-PARTY-NOTICES.md` at the repo root for their
licenses.
