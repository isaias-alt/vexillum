---
name: forum
description: Open an HTML artifact (plan, comparison, diagram, table, decision form) in the browser for the user to review, collect their feedback through a poll loop, and answer in the browser's conversation panel. Use when a response will be clearer as a visual page than as prose, or when you need structured decisions from the user. Driven by the `vexillum forum` commands.
license: MIT
metadata:
  argument-hint: "<what the artifact should show>"
---

# Forum

`vexillum forum` serves an HTML file you wrote to the user's browser and
carries their feedback back to you. The user chats, queues messages, fills in
the decision forms you built, and presses **Send to Agent**; you receive it
with `vexillum forum poll`. The loop and the idea are inspired by
[forum-tool](https://github.com/upstream) (MIT, see
THIRD-PARTY-NOTICES.md); forum is built into the vexillum binary and needs no
Node, no `npx`, no network.

> The review chrome has its own fixed look (dark by default, with a light
> theme the user can switch to from the top bar) and your artifact sits on a
> white canvas inside it. Never design your artifacts to match the chrome:
> your artifact has its own look.

## Request

$ARGUMENTS

If the request above is non-empty, build that artifact. If it is empty, infer
what to visualize from the conversation.

## The flow

1. **Write the artifact** as one HTML file, by default under `.forum/` in the
   current directory (add it to `.gitignore` if the project does not already
   ignore it). Open the matching playbook first (below). Assets (images, CSS,
   scripts) go next to the HTML file and are referenced by **relative** paths -
   never start a path with `/`. Only files in the artifact's own directory are
   served, and dot-files never are.
2. **Open it**: `vexillum forum .forum/plan.html`. It opens (or resumes) the
   session, opens the browser, **returns immediately**, and prints the session
   URL and the next step. A background server (one per user, 127.0.0.1 only)
   keeps running and stops itself when nothing is connected. `--no-open` skips
   opening the browser.
3. **Tell the user** the review is open and what you need from them.
4. **Poll in a loop**: `vexillum forum poll .forum/plan.html`. It blocks until
   there is feedback. Never kill it, never background it with `&`/`nohup`, and
   do not tell the user it is being watched unless a poll is actually running
   in your harness's tracked foreground/background-job facility.
   After every response, act on it and poll again, until the session ends.
5. **Answer in the browser** while you keep waiting:
   `vexillum forum poll .forum/plan.html --reply "Done: switched to the Pro plan"`
   shows your markdown message in the conversation panel, then waits again.
   Use `--reply-file <path>` (or `-` for stdin) for long or multi-line replies.
6. **Edit the artifact** when feedback asks for changes. The browser reloads
   it by itself when the file changes.
7. **End** when you are done: `vexillum forum end .forum/plan.html`. The user
   can also end it from the browser (**Send & End** delivers their final
   feedback once, then ends).

Sessions are identified by the file's **absolute path**; the same file always
resumes the same session, including its queue and transcript.

## Commands

| Command | What it does |
|---|---|
| `vexillum forum <file> [--no-open] [--reopen] [--port n]` | Open or resume the session, return at once. |
| `vexillum forum poll <file> [--reply <text> \| --reply-file <path\|->] [--timeout <dur>]` | Wait for feedback (consuming it). `--timeout 10m` returns `status: timeout` if nothing arrives. |
| `vexillum forum end <file>` | End the session as the agent. A plain `forum <file>` reopens it later. |
| `vexillum forum stop` | Shut the background server down. |

If a command's first argument is literally `poll`, `end`, `stop` or `serve`,
it is the subcommand; to open a file with such a name, write `./poll`.

## `forum <file>` output

```
session: <key>
file: <absolute path>
status: open
url: http://127.0.0.1:<port>/session/<key>
pending_prompts: <n>
next_step: <what to do now>
```

`pending_prompts` counts prompts the user already sent that your next poll
delivers. If the user **ended the session from the browser**, the command exits
1 with `status: user_ended` and does not reopen it: reopening something the
user deliberately closed is not your call. Pass `--reopen` only when the user
asks for further review, or when something important needs their visual
attention.

## `poll` output

Stable, line-oriented text (exit code 0 for every status below; non-zero only
for real errors, printed to stderr):

```
session: <key>
file: <absolute path>
status: feedback | ended | browser_disconnected | timeout
ended_by: user | agent            (only when the session ended)
prompts[<n>]:
  - uid: <prompt id>
    tag: <tag>
    prompt: <text>
    selector: <css selector>      (omitted when empty)
    text: <element text or label> (omitted when empty)
    target: <compact JSON>        (omitted when empty)
    attachments[<n>]:             (omitted when the user attached no images)
      - path: <absolute local path of an image>
        type: image/png | image/jpeg | image/gif | image/webp
        bytes: <size>
next_step: <what to do now>
```

`tag`, `selector` and `text` are how an **annotation** arrives; see
"Annotations" below. `attachments` is how **images** arrive; see "Images".

- A multi-line value is written as `key: |` followed by its lines indented two
  spaces deeper than the key. Everything else is `key: value` on one line.
- `prompts[0]:` means no prompts. Prompts arrive in the order the user queued
  them. Feedback is **consumed** by the poll that returns it - read the whole
  response; it is not delivered twice.
- `status: feedback` - act on the prompts, then poll again (usually with
  `--reply`).
- `status: ended` - the session ended (`ended_by` says who). Prompts, if any,
  are the **final** feedback (Send & End): apply them. **Stop polling** and do
  not reopen the session uninvited.
- `status: browser_disconnected` - the review window has been gone past a grace
  period (about 30 seconds) but the session is still resumable and nothing is
  lost. Ask the user whether to reopen it (`vexillum forum <file>`) or end it;
  do neither on your own, and do not tight-loop on this status.
- `status: timeout` - only with `--timeout`; poll again.
- Common `tag` values: `message` (typed in the composer), `feedback` (default
  for `queuePrompt`), `whiteboard` (see below), `layout-warnings` (layout
  issues the user chose to send, see "Layout issues"), `text` (an annotated
  text selection), an HTML tag name such as `button` or `p` (an annotated
  element), plus whatever tag your artifact passes.

If the server went away (stopped, crashed, idle), `poll` restarts it and keeps
waiting; everything queued is on disk.

## What the user sees

A conversation panel next to your artifact: their messages (plain text), your
replies (markdown, sanitized), a composer, a removable list of **queued**
messages, **Send to Agent** and **Send & End**, and an indicator of whether
your agent is listening. When no poll is running the panel tells the user:
"Your agent is not listening. Ask it to poll for updates." - so keep a poll
running whenever the session is open.

**Several tabs.** The same session can be open in more than one browser tab or
window; every tab shows the same queue, transcript, listening state and end
state, live, and any of them can send. You need do nothing: there is still one
session and one poll. A tab whose connection drops reconnects by itself, and
nothing the user queued or sent is ever lost, because it is all on disk.

**The transcript is bounded.** The conversation panel keeps the newest 500
messages and at most 5 MB of transcript per session; older ones scroll off for
good (and their images are freed once nothing references them). This never
costs you feedback: a prompt the user sent is delivered by `poll` from its own
outbox no matter what the transcript has evicted, and a prompt still waiting in
the queue is untouched. If you reply with very large markdown, expect it to
push older messages out sooner; prefer `--reply` summaries over dumps.

## When the session ends

When the session ends (the user pressed **Send & End**, or you ran
`vexillum forum end`) the browser shows a **Session ended** dialog that cannot
be dismissed: it says who ended it, shows the artifact's absolute path with a
**Copy path** button, and tells the user they can close the tab. Everything
behind it is inert, so nothing typed afterwards can reach you. What you do:

- On `status: ended` from `poll`: apply the final prompts (if any), then
  **stop polling**. Do not reopen the session on your own, and do not run
  `vexillum forum <file> --reopen` unless the user asks for further review.
- When the user asks you to review again, reopen it (`vexillum forum <file>
  --reopen`); the dialog goes away in their open tab by itself and the queue and
  transcript are still there.
- If you ended it yourself, say so in your final message so the user is not
  surprised by the dialog, and give them the path to the artifact.

## Images

The user can **attach images** to a message: paste a screenshot into the
composer, drop image files onto the panel, or press **Attach image** (PNG,
JPEG, GIF or WebP; up to 10 MB each and 4 per message, 256 MB per session).
They are shown as thumbnails in the queue and the conversation and stored
under `~/.vexillum/forums/<key>/attachments/`.

They reach you in the `poll` output as **absolute local file paths** in the
prompt's `attachments`:

```
  - uid: pr_...
    tag: message
    prompt: The header looks wrong, see the screenshot
    attachments[1]:
      - path: /Users/me/.vexillum/forums/<key>/attachments/at_3f9c....png
        type: image/png
        bytes: 48211
```

Read each image from its `path` with your file-reading tool (it can open
images) **before acting**; do not guess from the text alone. A message that is
only images arrives with the prompt `(see the attached image)`. The file stays
on disk while the session exists and a prompt or the transcript refers to it;
copy it elsewhere if you need it longer. An artifact cannot attach images
itself: `queuePrompt` has no image option, and annotations carry none.

## Layout issues

While the artifact is open, the browser passively audits it for **severe,
provable layout failures**: text cut off by its container, a control the user
cannot reach, text almost entirely covered by another element, content that
makes the page scroll sideways. Hidden elements, deliberate ellipsis or
line-clamp truncation, intentional scrollers, masked and screen-reader-only
content and anything mid-animation are ignored, and a finding must show up in
two samples a moment apart. What it finds goes to a **Layout issues** tray in
the top bar (with a count) and **nowhere else**:

- It **never wakes you** and **never appears in `poll`**. Do not go looking for
  layout problems the user has not sent you, and never edit the artifact to
  chase one on your own initiative.
- Only if the user opens the tray, selects issues and presses **Queue selected
  fixes** (then Send to Agent) do they arrive, as an ordinary prompt:

```
  - uid: pr_...
    tag: layout-warnings
    prompt: |
      Fix these 2 layout issues the browser detected in this artifact:
      1. [42eb3759445106dc] Text cut off by its container - Rendered text crosses its container's right edge by 550px and is hidden. Target: div#bad-clip. Viewport: Desktop (1106px). Status: Open.
      2. ...
      Apply every listed fix in one pass before saving ...
    text: Layout issues: 2 selected
    target: {"type":"layout-warnings","warnings":[{"id":"...","rule":"clipped-text","selector":"div#bad-clip","axis":"horizontal","overflow_px":549.9,"viewport_class":"desktop","viewport_width":1106}]}
```

Apply every listed fix in **one** edit, then `--reply` saying what changed. A
queued issue is a request, not a resolved issue: it is marked resolved only
after the browser reloads the edited artifact and a complete audit no longer
finds it, and it comes back as "Still present" if your fix did not work (the
user may queue it again). Issues are per viewport width class (mobile up to 640
px, compact up to 1024 px, desktop above): fixing the desktop layout does not
clear a phone-width issue. The audit only sees what the user's window shows, so
check dark and light and a narrow width yourself.

## Browser API inside the artifact: `window.forum`

The artifact runs sandboxed; `window.forum` (injected before your scripts) is
the whole API. There is no `window.forum`.

- `window.forum.queuePrompt(text, opts)` - puts one prompt in the user's
  queue. Nothing reaches you until the user presses **Send to Agent**.
  Returns a Promise of the queued prompt (rejects if the session ended).
  Options:
  - `tag` - short label you will see in `poll` output (default `feedback`).
  - `text` - label or selection the prompt is about.
  - `selector` - CSS selector of the element it is about.
  - `target` - any JSON describing the target (a table cell, a row id).
  - `data` - any JSON; appended to the prompt as `Context data:` followed by
    pretty JSON.
  - `queueKey` - a later **unsent** prompt with the same key replaces the
    earlier one instead of piling up. It is derived automatically for radios,
    checkboxes, inputs, selects and textareas (per form/fieldset), and from a
    `data-forum-question="<id>"` attribute on a wrapper; pass `queueKey`
    explicitly to override (`""` disables replacement).
  - `element` - a DOM element to derive `selector`, `text` and `queueKey`
    from (default: the focused element).
- `window.forum.sendQueuedPrompts()` - sends everything queued right away,
  instead of waiting for the user to press Send to Agent. Use it only for a
  control whose whole purpose is "submit this to the agent now".

Native controls (radios, checkboxes, inputs, selects, textareas, buttons,
labels, `<summary>`, links inside forms) **always act exactly as authored**, in
every mode: a plain click on them never annotates and is never intercepted, so
decision forms need no special treatment. Only Alt/Option+click annotates a
control (see "Annotations"). `window.forum` only exists when the page is opened
through `vexillum forum`; a copy opened from disk has no `window.forum`, so
guard calls or tell the user how to open it.

## Annotations

Every artifact can be annotated by the user with no work on your side:

- **Annotate** (a switch in the top bar, showing On or Off; Ctrl/Cmd+I toggles
  it): it is **On by default** in every new page load, and the user's choice is
  remembered. While On, hover outlines an element, a click selects it and opens
  a note card; Enter queues the note, Esc cancels. A plain click annotates
  **content** (text, headings, cards, images, links in prose). The artifact's
  **controls always act normally** with a plain click: radios, checkboxes,
  inputs, selects, textareas, buttons, labels (a click on a label activates its
  control), `<summary>` and links inside a form, so a decision form works with
  Annotate On and you need not warn the user. To annotate a control instead,
  **hold Alt/Option while clicking it**: that annotates it without toggling,
  typing or submitting. The hover outline skips controls unless Alt/Option is
  held.
- **Selecting text** (in or out of annotation mode) offers an
  **Annotate selection** action that opens the same card.

Each annotation joins the user's queue (removable there) and reaches you with
the next poll, like any prompt. The note is `prompt`; what it is about is in
the other fields:

```
  - uid: pr_...
    tag: button                         # the element's tag, or `text` for a selection
    prompt: Make this button larger     # the user's note
    selector: button[data-testid="save"]  # unique CSS selector of the element
                                          # (for a selection: the element that contains it)
    text: Save changes                  # the element's text (<= 240 chars) or the selected text (<= 500)
```

A long selection also carries `target: {"type":"text-range","selector":...,"text":...}`
with up to 2000 characters of it. The selector prefers a unique `id`, then
`data-testid`/`data-forum-question`/`name`, then a `:nth-of-type` path; use it
to find the element in your source (`querySelector` semantics), and the
`text` to find it by content when the markup is generated. Stable
`id`/`data-testid` attributes in your artifact make annotations land exactly.
Reply with `--reply` as usual: say what you changed and where. An annotation
carries only the note and what it is about; images are attached to a composer
message (see "Images") and layout issues arrive only when the user queues them
(see "Layout issues").

## Decision forms

Build choices from native controls and call `window.forum.queuePrompt` **once,
on submit** - never from `change` or `click` handlers of radios, checkboxes or
selects, because the user may still change their mind. A submit button then
queues the final answer; the `queueKey` makes a re-submission replace the
previous one. Full patterns (single choice, tracked batch, free text) are in
`playbooks/input.md`.

```html
<form data-forum-question="plan" onsubmit="event.preventDefault();
  const choice = new FormData(event.currentTarget).get('plan');
  if (choice) window.forum.queuePrompt('Use the ' + choice + ' plan',
    { tag: 'choice', text: 'Plan: ' + choice, element: event.currentTarget,
      data: { question: 'plan', answer: choice } });">
  <label><input type="radio" name="plan" value="Starter"> Starter</label>
  <label><input type="radio" name="plan" value="Pro"> Pro</label>
  <button type="submit">Queue this answer</button>
</form>
```

Show the user the difference between "selected" and "queued", and end every
decision path with an obvious way to send it (the panel's Send to Agent, or a
`sendQueuedPrompts` button).

## Whiteboards (Mermaid, opt-in)

Mermaid is **not** the diagram default (see `playbooks/diagram.md`). Only when
the user asks for an editable whiteboard, author the diagram as
`<div class="mermaid">...</div>`. In the browser it becomes an embedded,
editable Excalidraw whiteboard (click it to unlock editing, Fullscreen opens it
over the whole page); flowchart, sequence, class, ER and state diagrams become
editable shapes, other types embed as an image to draw on. Edits autosave
locally. **Queue feedback** writes a `.excalidraw` scene and a PNG preview to
`~/.vexillum/forums/<key>/whiteboards/` and queues a prompt with
`tag: whiteboard`, which `poll` delivers like any other:

```
  - uid: pr_...
    tag: whiteboard
    prompt: |
      Whiteboard feedback for diagram 1 of 2.
      Edit summary:
      - Added node "Cache"
      Scene (.excalidraw JSON): /.../whiteboards/0.excalidraw
      Preview (PNG): /.../whiteboards/0.png
      Read the summary first ...
    text: Whiteboard: diagram 1
    target: {"whiteboard":1,"scenePath":"...","previewPath":"..."}
```

Read the summary first, open the files only if you need more, then apply the
edits by **updating the Mermaid source in the artifact** - never try to write
the scene back.

## Playbooks

Open every playbook that matches before writing HTML (one artifact often
combines several, e.g. a plan with a comparison and a diagram):

| Playbook | Use when |
|---|---|
| `playbooks/plan.md` | Explain a product or technical plan before implementation. |
| `playbooks/comparison.md` | Show options, tradeoffs, current vs target behavior. |
| `playbooks/input.md` | Collect decisions, choices, preferences or triage from inside the artifact. **Required** whenever you need structured answers. |
| `playbooks/diagram.md` | Explain relationships, flows, state or architecture with illustrations. |
| `playbooks/table.md` | Turn dense records into scan-friendly review surfaces. |

## Design: use forum's own styles

forum gives every artifact its own identity (the vexillum design system), so
**do not pull Tailwind, daisyUI, Bootstrap or any CSS framework from a CDN**:
they bring a different look, need the network, and fight the theme. Write plain
semantic HTML and use the classes below. The stylesheet is injected for you
(`/forum-assets/forum-tokens.css` and `/forum-assets/forum-artifact.css`, linked
ahead of your markup), follows the user's dark/light switch (dark by default),
and needs no `<link>` from you. **It is applied only to an artifact that brings
no styling of its own** (see "When the forum styles apply" below).

- **Bare elements are already styled**: `h1`-`h6`, `p`, `a`, `ul`/`ol`,
  `blockquote`, `code`, `pre`, `table`, `button`, `input`, `select`,
  `textarea`, `fieldset`, `details`. Write them without classes first.
- **Components** (classes): `fr-card` (`fr-card--accent`), `fr-badge`
  (`--accent` `--success` `--danger` `--bronze`), `fr-callout` (`--info`
  `--success` `--danger` `--bronze`), `fr-btn` (`--primary` `--secondary`
  `--bronze` `--danger`), `fr-stat` (`fr-stat-value`, `fr-stat-label`),
  `fr-table-wrap` (wraps a `<table>` so wide tables scroll instead of
  overflowing), `fr-figure` (an SVG with a `figcaption`).
- **Decision forms**: `fr-form` (a stack), `fr-field` (label above control),
  `fr-choices` + `fr-choice` (a radio or checkbox as a selectable row; the
  checked one highlights itself), `fr-actions` (button row). A
  `<button type="submit">` is the primary action by default.
- **Layout helpers** (opt-in, never applied to the page by default):
  `fr-page` (centered 72rem column with padding), `fr-stack` (vertical gap;
  it is the only spacing between its children, so do not add margins to them,
  and a heading directly inside it sits close to the block it introduces),
  `fr-cluster` (wrapping row), `fr-grid` (responsive columns), `fr-muted`.
- **SVG diagrams**: `fr-node` (`--accent` `--success` `--danger` `--bronze`),
  `fr-edge`, `fr-arrow`, `fr-label` (`--muted`) instead of hard-coded colors.
- **Tokens**: for anything custom, use the custom properties (`--fr-bg`,
  `--fr-surface`, `--fr-surface-sunken`, `--fr-border`, `--fr-text`,
  `--fr-text-secondary`, `--fr-accent`, `--fr-success`, `--fr-danger`,
  `--fr-bronze`, `--fr-space-1..6`, `--fr-radius-sm|md|lg`, `--fr-font-sans`,
  `--fr-font-mono`, ...), never raw hex, so the theme switch keeps working.
  Do not use `--fr-selection` (tyrian): it belongs to annotations.

A page skeleton:

```html
<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Plan: rename the sync layer</title></head>
<body>
  <main class="fr-page fr-stack">
    <h1>Rename the sync layer <span class="fr-badge fr-badge--accent">proposal</span></h1>
    <p class="fr-muted">What changes, why, and what I need from you.</p>
    <div class="fr-callout fr-callout--info">One decision needed: see the form at the end.</div>
    <section class="fr-grid">
      <article class="fr-card"><h3>Before</h3><p>...</p></article>
      <article class="fr-card fr-card--accent"><h3>After</h3><p>...</p></article>
    </section>
  </main>
</body>
</html>
```

### When the forum styles apply

The stylesheet is injected **only if the artifact has none of its own**. Any of
these makes the artifact "self-styled", and forum leaves its look alone:

- a `<style>` block, anywhere in the document;
- a `<link rel="stylesheet">` (a local file, Google Fonts CSS, anything);
- a CSS framework loaded from a CDN: a `<script src>` or stylesheet link whose
  address names Tailwind, daisyUI, Bootstrap, Bulma, UnoCSS, Twind, Windi,
  Materialize, Semantic UI, Pico, water.css or mvp.css. A CDN framework counts
  as the artifact's own style (it sets the whole look), which is also why you
  must not use one: you would opt out of the identity by accident.

Inline `style="..."` attributes do **not** count, so for a one-off tweak on a
forum-styled page (a width, a margin) write `style="..."` and keep the rest.
Adding a `<style>` block "just for a small tweak" turns the forum styles off
for the whole page: if you want both, say so explicitly.

Override the default with a meta in `<head>` (`none` wins if both appear):

| Meta | Effect |
|---|---|
| *(none)* | automatic: forum styles only when the artifact brings no style of its own |
| `<meta name="forum-style" content="on">` | always inject, even with your own `<style>`/stylesheet. Your CSS still wins on any conflict: the forum sheet sits in a low-priority cascade layer, so a small `<style>` of tweaks can sit on top of the forum look |
| `<meta name="forum-style" content="none">` | never inject: the page is entirely yours (a mock of the user's product in its own design system, a styled slide deck) |

`window.forum` is injected in every case. Whatever you choose: no external CDN
dependencies for styling, check dark and light theme and a narrow width, no
horizontal overflow. Say in your handoff whether the page uses the forum styles
or its own, and why.
