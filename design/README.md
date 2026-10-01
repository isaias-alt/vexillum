# Vexillum design system

This directory is the unzipped export of the Vexillum design system (forum,
docs, landing and brand), delivered by the general. It is the source of truth
and is never edited by hand: a new export is unzipped over it. Reference
screenshots (`uploads/`) are not kept. The shipped CSS is a copy, adapted by
hand, as described below.

| File | What it defines |
|---|---|
| `Vexillum Brand & Design System.dc.html` | brand: colors, type, mark |
| `Vexillum Landing.dc.html` | the landing page (`site/`) |
| `Vexillum Docs.dc.html` | the documentation site (`site/`) |
| `Forum Design System.dc.html`, `Forum App.dc.html`, `forum-tokens.css` | `vexillum forum`: direction B, "Marmol y Lapislazuli" |
| `assets/vexillum-mark.svg` | the vexillum mark |

## forum

## What goes where

| In the export | Shipped as | Notes |
|---|---|---|
| `forum-tokens.css` | `internal/forum/assets/chrome/forum-tokens.css` | Token names and values are copied verbatim. Only the theme wiring differs, by decision of the general: **dark is the default** (the zip's default is light) and does not follow `prefers-color-scheme`; the zip's light values are the alternative theme, under `:root[data-fr-theme="light"]`. |
| `Forum Design System.dc.html` | `internal/forum/assets/chrome/forum.css` | The spec (surfaces 1-6: artifact frame, conversation panel, composer, annotation mode, whiteboard frame, toasts). Not copied: its component rules are re-expressed over the chrome's own selectors in `forum.css`, using only `--fr-*` tokens. |
| (not in the zip) `site/app/icon.svg` | `internal/forum/assets/favicon.svg` | Verbatim copy of the site's vexillum icon, embedded and served at `/favicon.svg`; linked from the chrome and injected into artifacts that declare no icon of their own. It carries its own dark rounded background, so it reads on light and dark tabs. |
| (derived from the tokens and the spec's component vocabulary) | `internal/forum/assets/chrome/forum-artifact.css` | The look of an artifact's *content*: typography, links, code, tables, cards, badges, buttons, decision-form controls, callouts, SVG figures. Built only from `--fr-*` tokens; the zip defines no content components, so this extends the spec's buttons, code and status colors rather than inventing a separate style. |
| `assets/vexillum-mark.svg` and the other `.dc.html` files | not shipped by forum | Brand, landing and docs material; unrelated to the forum chrome. |

The source lives here in `design/`; the delivered CSS lives under
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
`forum-prefs.js`, same try/catch fallback as the theme). The bar always shows
its state as text and Ctrl/Cmd+I toggles it. Native controls (radios,
checkboxes, inputs, selects, buttons, labels, summaries, links inside forms)
always act normally, even with Annotate On, so decision forms work;
Alt/Option+click annotates a control instead, and everything else annotates on
a plain click.

Buttons never wrap their label (`white-space: nowrap`); when the panel is too
narrow it is the hint text or the button row that wraps, not a label.

## Tray, attachments and the ended dialog

The design system's reserved components are used as named, re-expressed over
the chrome's own selectors in `forum.css` with `--fr-*` tokens only:

- **Layout issues**: a top-bar button (the `.switch` pill with a count) opens a
  tray of `.fr-notice-item`s in a `.fr-notice-tray`, one per issue (checkbox,
  `.fr-notice-item-icon`, title, explanation, selector, viewport, Dismiss). Open
  issues use the danger pair, a queued one the accent pair, a resolved one the
  quiet border. The tray is detection-only: nothing in it reaches the agent
  until the user queues the selection.
- **Attachments**: `.fr-attachments-tray` and `.fr-attachment-chip` under the
  composer, thumbnails (`.thumb`) in the queue and the transcript.
- **Session ended**: a modal `.fr-dialog` with no way out.
- **Disabled** is one look for every button variant (`.btn`, `.btn-primary`,
  `.btn-danger`): sunken surface, muted text, one opacity. The textarea and
  the attach link use the same opacity, so a finished session greys the whole
  composer equally.

## Artifact content

`vexillum forum` links `forum-tokens.css` and `forum-artifact.css` ahead of an
artifact's own markup, so agents get the identity with no setup and write no
Tailwind or daisyUI. Safety rules, each covered by a test:

- Everything in `forum-artifact.css` is inside one low-priority cascade layer
  (`@layer forum-artifact`), so any style an artifact writes itself wins.
- Bare elements get skin only (color, type, borders, radius). Layout (page
  width and padding, grids) is opt-in through classes (`fr-page`, `fr-grid`,
  ...), so a self-styled layout is never moved.
- The stylesheet is injected only into artifacts with no styling of their own:
  no `<style>` block, no `<link rel="stylesheet">`, no CSS-framework CDN
  (Tailwind, daisyUI, Bootstrap and the like count as styling). Inline
  `style=""` attributes do not count. Three branches, each tested:
  automatic (that rule), `<meta name="forum-style" content="on">` (force it,
  the artifact's own CSS still wins in the layer) and `content="none"` (never;
  it wins over `on`). `window.forum` is injected in every case.
- The tyrian selection color is never used in content.
- The theme follows the chrome's switch: the iframe is loaded with
  `?theme=dark|light`, the server renders `<html data-fr-theme>` (no flash), and
  the chrome posts `forum:theme` to the SDK when the user flips the switch.
- Every `fr-*` class named in `skills/forum/` must exist in the stylesheet.

## Updating the identity

1. Unzip the new export over this directory (delete `uploads/`).
2. Diff `design/forum-tokens.css` (from git) against `internal/forum/assets/chrome/forum-tokens.css`
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

## site (landing and docs)

`site/` is built from `Vexillum Landing.dc.html` and `Vexillum Docs.dc.html`,
which use the same `forum-tokens.css` identity (lapis on marble, bronze as the
tertiary accent).

| In the export | Shipped as | Notes |
|---|---|---|
| `forum-tokens.css` (the `--fr-*` tokens) | `site/app/globals.css` | Names and values copied verbatim; only the theme wiring differs: **dark is the default** and is the `data-theme="dark"` attribute set by an inline script, light is the base `:root`. The tokens are mapped onto Tailwind colors and onto Fumadocs' `--color-fd-*` so its components use the brand. |
| `Vexillum Landing.dc.html` | `site/app/[lang]/(home)/page.tsx` and `site/components/` | The "minute with vexillum" steps use the real `vexillum` commands (the export's `vx` is not a binary). |
| `Vexillum Docs.dc.html` | Fumadocs notebook layout, re-themed in `site/app/globals.css` | Top bar, 244px sidebar with small-caps groups, content, "on this page". Fumadocs UI was kept (search dialog, mobile drawer, language switch) and re-themed rather than rebuilt on its headless core. |
| `assets/vexillum-mark.svg` | `site/components/Logo.tsx`, `site/app/icon.svg` | currentColor mark. |

Fonts are Spectral and JetBrains Mono through `next/font`, self-hosted at build
time: no request to Google Fonts at runtime.

### Domain

The `.dc.html` exports still show `vexillum.lucasco.dev` in the install command.
They are kept untouched as the design system's export; the real site is served
from `https://vx.lucasco.dev` (see `site/lib/site.ts`).
