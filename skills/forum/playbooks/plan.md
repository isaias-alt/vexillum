# Plan: a proposal the user can inspect before anything is built

Use it when the user should look at an approach before implementation starts,
or asked for a design doc, a PRD, an implementation plan or a proposal. If the
whole plan is one small choice, skip this and use the comparison or diagram
playbook on its own.

A plan page has one job: let someone who was not in the conversation see what
you propose, check that it is grounded, and tell you where you are wrong. It
is not a status report and it is not a brainstorm.

## Sections, in reading order

1. **Goal.** What this achieves and for whom, in a sentence or two. Name what
   is explicitly out of scope.
2. **Today.** How the relevant part of the system behaves now, with the files,
   commands or output that show it.
3. **Target.** How it should behave afterwards. Where a user-visible surface
   changes, show it (see below) instead of describing it.
4. **Approach.** The decisions that shape the work, at a high level: which
   modules change, in what order, what is reused, what is new. Give the reason
   for every decision that was not obvious.
5. **Risks and failure modes.** What can go wrong, how you would notice, and
   how it is undone. Include migration concerns and backwards compatibility,
   even when the answer is "none, because ...".
6. **Open questions.** Only the ones you cannot settle yourself, each with the
   options and your recommendation, ideally as a decision form (input
   playbook) so the user answers in one click.

Lead the proposal, not the uncertainty: the approach must be complete enough
that the open questions read as refinements, not as holes.

## Make it checkable

- **Verify before you assert.** Read the code or run the command before you
  state something as fact, and cite it (`internal/forum/hub.go`, the output of
  `go test ./...`). Mark what you inferred and did not check as an assumption.
- **Self-contained.** Another developer should be able to implement it from
  the page alone, without the chat history. Spell out names, paths and
  interfaces.
- **Show, don't narrate, for UI work.** When the plan changes what a person
  sees, render a mock of it in the product's own design system, not forum's.
  That page needs `<meta name="forum-style" content="none">` (or a section
  that carries its own styles) and its own CSS; read the product's tokens or
  component library first so the mock is faithful.
- **Reach for other playbooks inside the plan.** A flow or architecture
  becomes an inline SVG (diagram playbook); competing approaches become
  option cards (comparison playbook); a long inventory becomes a table.

## Working the review loop

- The user reads, annotates elements or text, writes in the panel and answers
  your decision forms; their feedback comes back when they press **Send to
  Agent**. A **Send & End** is their go-ahead to start building.
- After each round, edit the file (the browser reloads it) and then
  `vx forum reply <file> --reply "<what changed>"`, so the round is marked
  answered.
- **Keep the page current.** When a question is decided, rewrite the plan to
  reflect the answer and delete the question. A plan that still lists
  resolved questions makes the reader re-litigate them. Keep a short "Decided"
  list if the history helps.
- Keep stable `id`s on sections and on anything people are likely to
  comment on, so their notes still land on the right element after you
  rewrite the page.

## Example skeleton

```html
<main class="fr-page fr-stack">
  <h1>Plan: bound the prompt queue <span class="fr-badge fr-badge--accent">proposal</span></h1>

  <div class="fr-callout fr-callout--info" id="goal">
    <strong>Goal.</strong> A queued prompt is never lost and the queue never
    grows without limit. Out of scope: changing how prompts are delivered.
  </div>

  <section class="fr-stack" id="today">
    <h2>Today</h2>
    <p>The queue is an unbounded slice persisted on every change
       (<code>internal/forum/hub.go</code>).</p>
  </section>

  <section class="fr-grid" id="approach">
    <article class="fr-card"><h3>1. Cap</h3><p>Reject the 201st prompt with a visible error.</p></article>
    <article class="fr-card fr-card--accent"><h3>2. Persist</h3><p>Write to a temp file, then rename.</p></article>
  </section>

  <section class="fr-stack" id="risks">
    <h2>Risks</h2>
    <div class="fr-callout fr-callout--danger">A full queue must tell the user; a silent drop loses feedback.</div>
  </section>

  <div class="fr-callout fr-callout--bronze" id="q-cap">
    <strong>Open question.</strong> Cap per session or global?
  </div>
</main>
```

Put the decision form for an open question (input playbook) right under its
callout, so the question and the way to answer it sit together.

## Styling and layout checks

Unless the plan mocks a product UI, use forum's own classes (`fr-page`,
`fr-stack`, `fr-callout`, `fr-card`, `fr-grid`, `fr-badge`), never a CDN
framework. They come from `forum-artifact.css`, injected only while the
artifact has no `<style>`, no stylesheet `<link>` and no CSS framework of its
own. Tweak with `style="..."`; to add a `<style>` block and keep the forum look
beneath it, put `<meta name="forum-style" content="on">` in the head (SKILL.md,
"When the forum styles apply").

The browser quietly audits layout (clipped text, controls out of reach, text
covered by another element, sideways scroll) and lists problems in the user's
Layout issues tray; you hear about them only if the user queues the fixes.
Prevent them: let text wrap, keep fixed widths off clipping boxes, and view the
page in dark, light and at phone width before handing it over.

## Mistakes to avoid

- Questions that were answered and are still on the page.
- Only the contested choices, with the actual proposal missing.
- No failure modes, no migration or compatibility story.
- Claims about the code that you did not read.
- A plan that needs the chat to make sense.
