# Third-party notices

vexillum's Go binary vendors third-party browser software into
`internal/forum/assets/whiteboard/` for the whiteboard feature of
`vx forum` (see that package's doc comment). It is built by
`tools/whiteboard-bundle/build.js` (dev-only, never run at vexillum's own
build or install time - see that directory's README) and go:embedded into
the binary. Each component remains under its own license; the notices
below satisfy their attribution requirements.

## Bundled into `whiteboard.js.gz` and `whiteboard.css`

| Package                                              | License | Copyright                                         |
| ----------------------------------------------------- | ------- | -------------------------------------------------- |
| `@excalidraw/excalidraw` 0.18.1                       | MIT     | Copyright (c) 2020 Excalidraw                      |
| `@excalidraw/mermaid-to-excalidraw` 2.2.2             | MIT     | Copyright (c) 2023 Excalidraw                      |
| `mermaid` 11.12.1 (exact, bundled for the converter)  | MIT     | Copyright (c) 2014 - 2022 Knut Sveidqvist          |
| `react`, `react-dom` 18.3.1                           | MIT     | Copyright (c) Meta Platforms, Inc. and affiliates  |
| `dompurify` 3.4.16                                    | MPL-2.0 OR Apache-2.0 | Copyright (c) Cure53 and other contributors |

Other transitive dependencies of the packages above are bundled too (for example `d3` under ISC, `katex`, `cytoscape`, `dayjs`, `lodash-es`, `marked`, `roughjs` and `uuid` under MIT); their exact versions and licenses are in `tools/whiteboard-bundle/package-lock.json`.

The full MIT license text applies to each of the packages above:

```
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
```

## Fonts vendored into `internal/forum/assets/whiteboard/fonts/` (from `@excalidraw/excalidraw`)

| Family          | License                                      |
| --------------- | --------------------------------------------- |
| Excalifont      | MIT (created for Excalidraw)                  |
| Virgil          | MIT (created for Excalidraw by Ellinor Rapp)  |
| Nunito          | SIL Open Font License 1.1                     |
| Assistant       | SIL Open Font License 1.1                     |
| Cascadia Code   | SIL Open Font License 1.1                     |
| Comic Shanns    | MIT                                           |
| Liberation Sans | SIL Open Font License 1.1                     |
| Lilita One      | SIL Open Font License 1.1                     |

The Xiaolai family (CJK glyphs, ~12 MB) is intentionally not vendored;
Excalidraw falls back to its CDN or the system font for those glyphs.

## Inspired by `internal/slot/` (AGENTS.md marker block repair)

`internal/slot/slot.go` (`Repair`) is inspired by the markdown-section
injection and orphan-marker stripping (`InjectMarkdownSection`,
`stripOrphanMarkers`) of
[gentle-ai](https://github.com/Gentleman-Programming/gentle-ai), MIT
licensed. No code is vendored or linked; the approach (recover from orphan
markers and duplicate blocks by dropping the stray ones) is reimplemented in
Go with different semantics: it only runs after explicit confirmation, keeps
the first well-formed pair, and never touches text outside the markers. The
8-hex-char content hash in the BEGIN marker follows the convention used by
Beads.
