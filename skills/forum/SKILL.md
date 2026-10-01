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
next_step: <what to do now>
```

`tag`, `selector` and `text` are how an **annotation** arrives; see
"Annotations" below.

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
  for `queuePrompt`), `whiteboard` (see below), `text` (an annotated text
  selection), an HTML tag name such as `button` or `p` (an annotated element),
  plus whatever tag your artifact passes.

If the server went away (stopped, crashed, idle), `poll` restarts it and keeps
waiting; everything queued is on disk.

## What the user sees

A conversation panel next to your artifact: their messages (plain text), your
replies (markdown, sanitized), a composer, a removable list of **queued**
messages, **Send to Agent** and **Send & End**, and an indicator of whether
your agent is listening. When no poll is running the panel tells the user:
"Your agent is not listening. Ask it to poll for updates." - so keep a poll
running whenever the session is open.

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

When annotation mode is Off, or the click has Alt/Option held, the SDK intercepts nothing: native controls (radios,
checkboxes, inputs, selects, textareas, buttons, forms, `<details>`, links)
behave exactly as authored. With annotation mode On (the default) a plain click
annotates the control instead of firing it (see "Annotations"). `window.forum` only exists when the page is opened
through `vexillum forum`; a copy opened from disk has no `window.forum`, so
guard calls or tell the user how to open it.

## Annotations

Every artifact can be annotated by the user with no work on your side:

- **Annotate** (a switch in the top bar, showing On or Off; Ctrl/Cmd+I toggles
  it): it is **On by default** in every new page load, and the user's choice is
  remembered. While On, hover outlines an element, a click selects it and opens
  a note card; Enter queues the note, Esc cancels. Every element is
  annotatable, controls included: a click on a radio, checkbox, button or form
  annotates it without toggling or submitting it. **Holding Alt/Option while
  clicking** acts on the control normally and does not annotate. Because of
  this, when you ask the user to fill in a decision form, tell them they can
  switch Annotate Off or Alt/Option+click the controls.
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
Reply with `--reply` as usual: say what you changed and where. Annotations
never carry images or layout diagnostics.

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

## Design

forum injects no design system: artifacts stay portable, so they render the
same when opened directly. Decide the look in this order: (1) what the user
asked for; (2) otherwise the design system of the project the artifact is
about (theme config, CSS variables, component library, existing pages); (3)
only if both yield nothing, deliberate hand-written CSS. Give the page its own
background and text colors (and a dark variant via `prefers-color-scheme`), so
text is never invisible on the chrome's surface. No horizontal overflow at any
width. Say in your handoff which source you used.
