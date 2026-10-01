# Third-party notices

vexillum's Go binary vendors third-party browser software into
`internal/forum/assets/whiteboard/` for the whiteboard feature of
`vexillum forum` (see that package's doc comment). It is built by
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

## Adapted integration code

`tools/whiteboard-bundle/src/whiteboard-core.js`,
`tools/whiteboard-bundle/src/whiteboard-frame.js`,
`tools/whiteboard-bundle/src/whiteboard-frame.css`, and
`tools/whiteboard-bundle/src/whiteboard-embed.js` are adapted from
[`upstream`](https://github.com/upstream) at
**v0.1.80** (commit `a2a199c`), MIT licensed:

```
MIT License

Copyright (c) 2026 the upstream author

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

Also adapted from the same forum-tool release (same MIT license and
copyright notice as above):

- `internal/forum/assets/chrome/forum-sdk.js`: the prompt-context helpers
  (selector, element text, queue-key derivation) and the annotation mode
  (hover outline, click-to-annotate, text selection capture, the mode
  shortcut) of `src/artifact-sdk.js`; the note card is reimplemented in
  `forum-chrome.js`.
  The `queuePrompt`/`sendQueuedPrompts` option model and the poll/session
  semantics (`feedback`, `ended`, `browser_disconnected`, user-ended sessions
  needing `--reopen`) follow `src/server.js` and `src/session-store.js`,
  reimplemented in Go.
- `internal/forum/assets/chrome/forum-layout.js`: the passive layout audit
  (the severe-failure classifiers and the audit passes) of
  `src/artifact-sdk.js`, and `internal/forum/layout.go`: the warning
  lifecycle of `src/layout-warnings.js` (a warning is cleared only by a newer
  load plus a complete pass), reimplemented in Go.
- `internal/forum/attachments.go` and `internal/forum/hub.go`: the attachment
  size, count and disk limits of `src/attachment-store.js`, the transcript cap
  of `src/chat-messages.js` and the SSE live feed of `src/server.js`.
- `skills/forum/playbooks/*.md` (plan, comparison, input, diagram, table):
  rewritten for `window.forum` and the `vexillum forum` commands from
  `src/playbooks.js`.

Each adapted file's own header comment describes what changed from
upstream. Treat this code as vexillum's own - not an opaque vendor blob -
per `tools/whiteboard-bundle/README.md`: forum-tool ships a high release
cadence and keeps patching real bugs in this exact conversion path, so
diff against a newer forum-tool tag when something in the conversion looks
wrong before assuming it's a vexillum-specific bug.

## Adapted into `internal/tribunal/` (review and fix prompts)

The adversarial review step of `vexillum ship` (`internal/tribunal/review.go`
and `internal/tribunal/fix.go`) adapts prompt text, rules and the findings
vocabulary (severity error/warning/info, action ask-user/auto-fix/no-op,
`reviewed_paths` coverage record, fix-round provenance clause, simplification
pass, invariant-at-every-sibling-site fixer rules) from
[review-tool](https://github.com/upstream)
(`internal/pipeline/steps/review.go`, `internal/types/findings.go`), MIT
licensed, Copyright (c) 2026 the upstream author. No code is vendored or linked; the
full MIT license text above applies to the adapted portions.
