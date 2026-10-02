<!--
Adapted from upstream (MIT License, Copyright (c) 2026 the upstream author),
src/playbooks.js at v0.1.80 (commit a2a199c), rewritten for vx forum
(window.forum and the `vx forum` commands). See THIRD-PARTY-NOTICES.md at
the vexillum repo root.
-->

# Playbook: input

Use when: you need to collect user input on decisions, choices, preferences, triage, scope, or other structured feedback from within the artifact. **Open this playbook whenever the artifact contains a question for the user.**

## Choose

- Use controls for decisions the user can make faster visually than by writing a prompt.
- Use an opt-in tracked batch when you must preserve completeness across a multi-item decision (findings to fixes, constraints to implementation, recommendations to follow-up work).
- Use plain messages (the conversation panel) when the artifact only needs open-ended feedback.

## Structure

- Make each decision surface visible: what is chosen, what each option means, what happens next.
- For a tracked batch, give every candidate item a short, stable, visible ID and let the user select or disposition items with native controls.
- Keep reversible selection state local in the artifact until the user explicitly submits that question.
- Pair each question with a Submit or "Queue answer" control that sends exactly one prompt for the final answer.
- Show "selected" separately from "queued" so the user trusts what will be sent.

## Design rules

- Native controls (radios, checkboxes, text inputs, selects, textareas, buttons, labels, `<details>`, contenteditable) work exactly as authored: forum installs no click, change or submit handler of its own, so build option UIs from them. This holds with the chrome's **Annotate** switch On too (its default): a plain click on a control, or on a label, always acts on it. Only Alt/Option+click annotates a control, which the user may do to comment on a specific option; clicks on the surrounding text annotate it as usual.
- For reversible choices, never call `window.forum.queuePrompt()` from radio `change` or option `click` handlers; those only update local selected state.
- Use a per-question form submit (or an explicit Queue answer button) to read the current values and call `window.forum.queuePrompt()` exactly once for the final answer.
- Put `data-forum-question="<id>"` on the question's `<form>` (or pass `queueKey` and set the same value as `data-forum-queue-key` on the form). A re-submission before sending replaces the prior unsent answer for that key, and forum disables the form's submit button while its answer is queued and enables it again when the message is removed from the queue or sent. Do not build that state yourself; for anything custom use `window.forum.onQueueChange(keys => ...)` or `window.forum.isQueued(key)`.
- Because the button is disabled while queued, say so in the UI ("Queued - remove it from the queue to change it") instead of leaving the user guessing; style it with `form[data-forum-queued]`. Once sent, forum itself shows a "Sent in round N" / "Answered in round N" badge on the form (any markup; the user can hide it with the Marks switch), and the form carries `data-forum-sent="sent|answered"` and `data-forum-round` for your own styling (`window.forum.sentStatus(key)` for custom UI). Do not build that indicator yourself.
- Pass `tag`, `text`, `selector`, `target`, `data`, `queueKey` or `element` when they help you understand exactly what the user chose.
- For a tracked batch, queue the final selected set once, with a concise, bounded `data.items` array (stable ID, short label, requested disposition), and tell yourself in the prompt to account for every submitted ID before reporting completion.
- Call `window.forum.sendQueuedPrompts()` only when a control should immediately send committed feedback instead of waiting for the user to press Send to Agent.
- Make queued prompts specific enough to act on without a follow-up question. Keep controls accessible and readable on mobile.

## Images

- The user can attach screenshots to their own messages (paste, drop, **Attach image**); your artifact cannot, and `window.forum.queuePrompt` has no image option. When the answer you need is "show me", ask for it in the artifact's text ("paste a screenshot of the broken state into the conversation") instead of building an upload control.
- Images arrive in `vx forum inbox` (or `poll`) as local `attachments[].path`: open them before acting on the message.

## Pitfalls

- Do not queue one prompt per radio change, checkbox toggle, dropdown change or choice-button click while the user can still change their mind.
- Do not create controls whose queued prompt is unclear or too vague to execute.
- Do not hide the difference between selected locally and queued for the agent.
- Do not require interaction for content the user only needs to read.

## Patterns

Single choice, submitted once:

```html
<form data-forum-question="plan" onsubmit="event.preventDefault();
  const choice = new FormData(event.currentTarget).get('plan');
  if (choice) window.forum.queuePrompt('Use the ' + choice + ' plan', {
    tag: 'choice', text: 'Plan: ' + choice, element: event.currentTarget,
    data: { question: 'plan', answer: choice } });">
  <label><input type="radio" name="plan" value="Starter"> Starter</label>
  <label><input type="radio" name="plan" value="Pro"> Pro</label>
  <button type="submit">Queue this answer</button>
</form>
```

Tracked batch, the final selected set queued once:

```html
<form data-forum-question="tracked-review" onsubmit="event.preventDefault();
  const selected = [...event.currentTarget.querySelectorAll('input[name=items]:checked')]
    .map((i) => ({ id: i.value, label: i.dataset.label, disposition: i.dataset.disposition }));
  if (selected.length) window.forum.queuePrompt(
    'Act on every selected item and return an item-by-item receipt. Account for every submitted ID before reporting completion.',
    { tag: 'tracked-batch', text: 'Apply ' + selected.length + ' selected review items',
      element: event.currentTarget, data: { items: selected } });">
  <label><input type="checkbox" name="items" value="R-03" data-label="Preserve rollback behavior" data-disposition="must-address"> R-03 - Preserve rollback behavior</label>
  <label><input type="checkbox" name="items" value="R-08" data-label="Reuse the existing error surface" data-disposition="must-address"> R-08 - Reuse the existing error surface</label>
  <button type="submit">Queue selected items</button>
</form>
```

The receipt you give for a tracked batch must assign every submitted ID exactly one outcome: addressed with concrete evidence, deferred with a reason, or rejected with a reason. Before declaring completion, compare the submitted ID set with the receipt's ID set and surface every missing ID. Deliver the receipt with `vx forum reply <file> --reply-file -` (if you are polling by hand instead: `vx forum poll <file> --reply-file -`, or with several sessions `vx forum poll --all --reply-to <file> --reply-file -`).

A custom (non-native) choice UI should make its option elements update local state, then use a separate Queue answer button to queue the final value.

Use `window.forum.queuePrompt` for user intent, not for analytics or UI-only state changes. End every input path with an obvious way for the user to send the feedback to you.

## Styles (forum-artifact.css)

Style decision forms with forum's classes, not Tailwind or daisyUI from a CDN. `fr-choice` turns a radio or checkbox into a selectable row that highlights when checked, and a `submit` button is the primary action. The same form as above, styled:

The forum styles are injected only while the artifact has no `<style>`, stylesheet `<link>` or CSS-framework CDN of its own; for tweaks use `style="..."` attributes, or add `<meta name="forum-style" content="on">` to keep the forum look next to your own `<style>` (SKILL.md, "When the forum styles apply").

```html
<form class="fr-form" data-forum-question="plan" onsubmit="event.preventDefault();
  const choice = new FormData(event.currentTarget).get('plan');
  if (choice) window.forum.queuePrompt('Use the ' + choice + ' plan', {
    tag: 'choice', text: 'Plan: ' + choice, element: event.currentTarget,
    data: { question: 'plan', answer: choice } });">
  <fieldset class="fr-choices" style="border:0;padding:0">
    <legend>Which plan?</legend>
    <label class="fr-choice"><input type="radio" name="plan" value="Starter">
      <span>Starter <small class="fr-muted">For small teams</small></span></label>
    <label class="fr-choice"><input type="radio" name="plan" value="Pro">
      <span>Pro <small class="fr-muted">Includes the audit log</small></span></label>
  </fieldset>
  <div class="fr-field"><label for="why">Why? (optional)</label><textarea id="why" rows="2"></textarea></div>
  <div class="fr-actions"><button type="submit">Queue this answer</button></div>
</form>
```
