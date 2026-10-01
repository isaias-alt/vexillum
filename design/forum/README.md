# forum design system

`Vexillum design system.zip` is the source of truth for the look of
`vexillum forum`: direction B, "Marmol y Lapislazuli". It is delivered by the
general and never edited here. The shipped CSS is a copy, adapted by hand.

## What goes where

| In the zip | Shipped as | Notes |
|---|---|---|
| `forum-tokens.css` | `internal/forum/assets/chrome/forum-tokens.css` | Token names and values are copied verbatim. Only the theme wiring differs, by decision of the general: **dark is the default** (the zip's default is light) and does not follow `prefers-color-scheme`; the zip's light values are the alternative theme, under `:root[data-fr-theme="light"]`. |
| `Forum Design System.dc.html` | `internal/forum/assets/chrome/forum.css` | The spec (surfaces 1-6: artifact frame, conversation panel, composer, annotation mode, whiteboard frame, toasts). Not copied: its component rules are re-expressed over the chrome's own selectors in `forum.css`, using only `--fr-*` tokens. |
| (not in the zip) `site/app/icon.svg` | `internal/forum/assets/favicon.svg` | Verbatim copy of the site's vexillum icon, embedded and served at `/favicon.svg`; linked from the chrome and injected into artifacts that declare no icon of their own. It carries its own dark rounded background, so it reads on light and dark tabs. |
| `assets/vexillum-mark.svg` and the other `.dc.html` files | not shipped | Brand and landing material; unrelated to the forum chrome. |

The source lives here in `design/forum/`; the delivered CSS lives under
`internal/forum/assets/` and is embedded in the binary. No fonts, no CDN:
system stacks only.

## Theme

The chrome starts dark. The top bar has two switches built from one component
(`.switch`: `role="switch"`, state in `aria-checked`, an icon, a text label and a
visible On/Off). **Light theme** turns the light tokens on. The choice is saved in `localStorage`
(`forum-theme`) and applied by `forum-theme.js`, a blocking script in `<head>`
that runs before the stylesheets paint, so there is no flash. With no saved
choice, or with storage blocked, the chrome is dark. The artifact canvas stays
white in both themes: artifacts are authored for it.

**Annotate** is the other switch. It starts On in every new page load and the
user's choice is remembered in `localStorage` (`forum-annotate`, handled by
`forum-prefs.js`, same try/catch fallback as the theme). Because On makes clicks
on the artifact's own controls annotate instead of act, the bar always shows
its state as text, Ctrl/Cmd+I toggles it, and Alt/Option+click acts on a
control normally.

Buttons never wrap their label (`white-space: nowrap`); when the panel is too
narrow it is the hint text or the button row that wraps, not a label.

## Updating the identity

1. Unzip the new design system to a scratch directory.
2. Diff its `forum-tokens.css` against `internal/forum/assets/chrome/forum-tokens.css`
   and carry the token changes over, keeping the theme wiring described above (dark in the base `:root`, light under `data-fr-theme`).
3. Compare the surfaces in the spec against `forum.css` and adjust components.
   `forum.css` must stay free of raw colors (a test enforces it): add or change
   a token instead.

## Rules from the spec

- `--fr-selection` (tyrian) marks annotations and selections only. It is never
  a status color: connected, sent, error and the like use success, accent and
  danger.
- `--fr-accent` (lapis) is the primary action and the "listening" state;
  `--fr-bronze` is the tertiary accent.
