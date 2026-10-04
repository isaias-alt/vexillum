# Input: decisions, answers and triage

Read this before writing any artifact that asks the user something. It covers
how to turn a question into controls, how an answer travels back to you, and
how to account for it afterwards.

## Pick the mechanism

| The user has to... | Build |
|---|---|
| pick one option, or a few | A form with radios or checkboxes, one submit |
| rule on a list of items (accept, defer, drop) | A **tracked batch**: one row per item, one submit |
| explain something in their own words | Nothing: the conversation panel is already a free-text channel. Add a textarea only when it belongs to a specific choice |
| read something | No control at all. Never make reading depend on clicking |

If a question can be answered faster by clicking than by typing, put the
controls in the page. If the user would just type a sentence anyway, ask the
question in prose and let them reply in the panel.

## The rule that matters most: queue once, on submit

`window.forum.queuePrompt(text, opts)` puts a message in the user's queue. The
user can still change their mind while they pick, so a prompt is created only
when they commit:

- Radios, checkboxes and selects only change what is selected on the page.
- A form `submit` (or an explicit "Queue answer" button) reads the final
  values and calls `queuePrompt` **exactly once**.
- Never call `queuePrompt` from `change` or `click`; you would flood the
  queue with answers the user has already abandoned.

Native controls (inputs, selects, textareas, buttons, labels, `<summary>`,
`contenteditable`) work as authored with the **Annotate** switch On, which is
the default: a plain click acts on the control. Only Alt/Option+click turns a
click into a comment on it, which is how a user can question one specific
option. You never need to tell them to turn annotation off.

## What forum does for a decision form

Mark the form with `data-forum-question="<id>"` and forum takes care of the
life cycle, with no code from you:

1. The prompt's `queueKey` becomes `question:<id>`, so a second submission
   before sending **replaces** the first instead of stacking.
2. While that answer is queued the form's submit buttons are disabled
   (`data-forum-queued="true"` is set on the form). They come back when the
   user removes the message from the queue or sends it. Buttons you disabled
   yourself are left alone.
3. After the user sends it, the form gets `data-forum-sent="sent"` and
   `data-forum-round="<n>"`; once you answer that round it turns to
   `"answered"`. forum also paints a "Sent in round N" / "Answered in round N"
   badge on the form by itself. Do not draw your own.

Things you should still do:

- Say what the disabled button means ("Queued: remove it from the queue to
  change it") and style it off `form[data-forum-queued]`. Keep "selected" and
  "queued" visibly different, so the user knows what will be sent.
- With a custom key, pass the same string as `queueKey` and as
  `data-forum-queue-key` on the form. Any key that is not the bare question id
  is only matched back to its form while the page that queued it stays open.
- For bespoke UI, `window.forum.onQueueChange((keys) => ...)` reports the keys
  waiting in the queue, `window.forum.isQueued(key)` checks one, and
  `window.forum.sentStatus(key)` returns `{ round, state }` or `null`.
- Give every prompt enough context to act on without asking back: pass `tag`
  (what kind of answer), `text` (a human label), `selector` or `element` (what
  it is about), `target` (any JSON locator) and `data` (any JSON; it reaches
  you printed after `Context data:`).

## Pattern 1: a single choice with a reason

This is the whole thing, already styled with forum's classes. The form is a
stack, the fieldset is a column of selectable rows (the checked one lights
up), and a submit button is the primary action.

```html
<form class="fr-form" data-forum-question="cache-layer">
  <fieldset class="fr-choices" style="border:0;padding:0">
    <legend style="padding:0">Where should the cache live?</legend>
    <label class="fr-choice">
      <input type="radio" name="cache" value="memory" required>
      <span>In memory <small class="fr-muted">fastest, lost on restart</small></span>
    </label>
    <label class="fr-choice">
      <input type="radio" name="cache" value="disk">
      <span>On disk <small class="fr-muted">survives restarts, one extra read</small></span>
    </label>
  </fieldset>
  <div class="fr-field">
    <label for="cache-why">Anything I should know? (optional)</label>
    <textarea id="cache-why" name="why" rows="2"></textarea>
  </div>
  <div class="fr-actions"><button type="submit">Queue this answer</button></div>
</form>
<script>
  document.querySelector('[data-forum-question="cache-layer"]')
    .addEventListener('submit', (event) => {
      event.preventDefault();
      const form = event.currentTarget;
      const data = new FormData(form);
      const choice = data.get('cache');
      if (!choice) return;
      const why = String(data.get('why') || '').trim();
      window.forum.queuePrompt(
        'Put the cache ' + (choice === 'disk' ? 'on disk' : 'in memory') +
          (why ? '. Reason: ' + why : ''),
        { tag: 'choice', text: 'Cache: ' + choice, element: form,
          data: { question: 'cache-layer', answer: choice, reason: why } });
    });
</script>
```

Inline `onsubmit="..."` on the form works the same; use whichever reads better.
Check the choice before you queue, so an empty submission queues nothing.

## Pattern 2: a tracked batch

Use it when completeness matters: every finding must end up fixed, deferred or
rejected, and nothing may be silently dropped. The user marks a disposition per
item and submits **one** prompt carrying the full set.

```html
<form class="fr-form" data-forum-question="review-triage">
  <div class="fr-table-wrap">
    <table>
      <thead><tr><th>ID</th><th>Finding</th><th>Do</th></tr></thead>
      <tbody>
        <tr data-item="F-1" data-label="Unbounded retry loop">
          <td style="white-space:nowrap">F-1</td><td>Unbounded retry loop</td>
          <td><select name="F-1" aria-label="Disposition for F-1">
            <option value="">Leave out</option><option value="fix">Fix</option>
            <option value="defer">Defer</option><option value="reject">Reject</option>
          </select></td>
        </tr>
        <tr data-item="F-2" data-label="Stale help text">
          <td style="white-space:nowrap">F-2</td><td>Stale help text</td>
          <td><select name="F-2" aria-label="Disposition for F-2">
            <option value="">Leave out</option><option value="fix">Fix</option>
            <option value="defer">Defer</option><option value="reject">Reject</option>
          </select></td>
        </tr>
      </tbody>
    </table>
  </div>
  <div class="fr-actions"><button type="submit">Queue triage</button></div>
</form>
<script>
  document.querySelector('[data-forum-question="review-triage"]')
    .addEventListener('submit', (event) => {
      event.preventDefault();
      const form = event.currentTarget;
      const items = [...form.querySelectorAll('tr[data-item]')]
        .map((row) => ({ id: row.dataset.item, label: row.dataset.label,
                         disposition: row.querySelector('select').value }))
        .filter((item) => item.disposition);
      if (!items.length) return;
      window.forum.queuePrompt(
        'Apply this triage. Return a receipt that gives every submitted ID ' +
          'exactly one outcome before you call the work done.',
        { tag: 'tracked-batch', text: items.length + ' triaged items',
          element: form, data: { items } });
    });
</script>
```

Rules for a batch:

- Every item has a short, stable, **visible** ID (`F-1`), so the user, the
  prompt and your receipt all talk about the same thing.
- `data.items` stays small and bounded: id, a short label, the disposition.
  Put long evidence on the page, not in the prompt.
- Your receipt (sent with `vx forum reply <file> --reply-file -`, or the
  `poll` forms if you are polling by hand) gives each submitted ID exactly one
  outcome: **addressed** with concrete evidence (a file, a commit, a command),
  **deferred** with a reason, or **rejected** with a reason. Before you say
  "done", compare the set of submitted IDs with the set in the receipt and
  report any ID that is missing.

## Sending

Queued messages leave when the user presses **Send to Agent** in the panel, so
close every decision path with a plain sentence telling them so. Call
`window.forum.sendQueuedPrompts()` only from a control whose entire meaning is
"submit this to the agent now" (a "Send my answers" button at the end of a
long form); it flushes the whole queue, not just your form.

## Images

The artifact cannot take images: `queuePrompt` has no attachment option, and
an upload control would go nowhere. When "show me" is the real answer, ask for
it in text ("paste a screenshot of the broken state into the conversation").
The user attaches it to a panel message and it reaches you as a local path in
`attachments` (see `vx forum inbox`). Open the file before acting.

## Custom controls

If a native control cannot do the job (a star rating, a drag-to-rank list),
let its elements update local state only, and keep a separate "Queue answer"
button that reads that state and queues once. Use `queuePrompt` for what the
user means, never for analytics or to mirror hover and focus state.

## Styling

Use forum's classes (`fr-form`, `fr-field`, `fr-choices`, `fr-choice`,
`fr-actions`, `fr-table-wrap`), never a CDN framework. They come from
`forum-artifact.css`, which forum injects only while the artifact has no
`<style>`, no stylesheet `<link>` and no CSS framework of its own. Small tweaks
go in `style="..."` attributes. If you really need a `<style>` block, add
`<meta name="forum-style" content="on">` so the forum look stays underneath it
(see "When the forum styles apply" in SKILL.md).

The browser also audits the page for severe layout faults and keeps them in
the user's Layout issues tray; it never wakes you with them. Prevent them:
let text wrap, keep fixed widths off anything that clips, and look at the form
at a narrow width and in both themes before you hand it over.

## Mistakes to avoid

- A prompt per radio change, checkbox toggle or select change.
- Vague prompts ("Option B") that you cannot execute without asking again.
- Hiding the difference between "selected here" and "queued for the agent".
- Questions that were already answered, left in the page. After a decision,
  update the artifact to show the outcome and remove the question.
- Requiring a click for content that only needs to be read.
