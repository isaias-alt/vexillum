# Comparison: options, tradeoffs, before and after

Use it when the user has to weigh alternatives, or needs to see how something
differs between now and later. The page should let them see the difference at
a glance and, when a choice is needed, make it without leaving the page.

## Pick the form

| Situation | Form |
|---|---|
| One thing changing over time | **Before / after**: two cards with matching headings |
| Mutually exclusive directions | **Option cards** in a grid, one per option |
| Many options against explicit criteria | A **scorecard** table (`fr-table-wrap`), only if the criteria are real and comparable |
| One option vs. the status quo | Before / after, with the status quo as "before" |

Do not use a scorecard to look rigorous. Invented numeric scores hide the
reasoning; a short sentence per cell is more honest.

## Build it in this order

1. **State the decision** in the title or the first line: "Where should the
   cache live?", not "Cache options".
2. **Show each option concretely.** A real config snippet, a command, a
   screenshot, a table of before and after values: whatever the option would
   actually look like. "Simpler" and "more flexible" are conclusions, not
   evidence.
3. **Keep the options parallel.** Same headings in the same order inside every
   card, same units, same level of detail, so the eye can run across and see
   exactly where they differ.
4. **Give cost the same weight as benefit.** Each option gets what it costs
   (time, risk, lock-in, a new dependency) in the same visual treatment as what
   it gives.
5. **State your assumptions.** If the verdict flips when an assumption does
   ("assuming under 1000 users"), write it next to the verdict.
6. **Recommend only what the evidence supports.** When one option is clearly
   better, mark it (`fr-card--accent` plus a "recommended" badge) and say why in
   one line. When it is a close call, say so and do not dress one option up.

## Letting the user decide

Put a form under the cards and queue the choice once, on submit; the full
mechanics (and why never on click) are in the input playbook. Native radios in
a single form, plus an optional reason:

```html
<form class="fr-form" data-forum-question="cache-layer">
  <fieldset class="fr-choices" style="border:0;padding:0">
    <legend style="padding:0">Pick a direction</legend>
    <label class="fr-choice"><input type="radio" name="option" value="memory" required>
      <span>In memory</span></label>
    <label class="fr-choice"><input type="radio" name="option" value="disk">
      <span>On disk</span></label>
  </fieldset>
  <div class="fr-field">
    <label for="why">Reason (optional)</label>
    <textarea id="why" name="why" rows="2"></textarea>
  </div>
  <div class="fr-actions"><button type="submit">Queue my choice</button></div>
</form>
<script>
  document.querySelector('[data-forum-question="cache-layer"]')
    .addEventListener('submit', (event) => {
      event.preventDefault();
      const form = event.currentTarget;
      const data = new FormData(form);
      const option = data.get('option');
      if (!option) return;
      const why = String(data.get('why') || '').trim();
      window.forum.queuePrompt('Keep the cache ' + (option === 'disk' ? 'on disk' : 'in memory') +
          (why ? '. Reason: ' + why : ''),
        { tag: 'choice', text: 'Cache: ' + option, element: form,
          data: { question: 'cache-layer', answer: option, reason: why } });
    });
</script>
```

Submitting again before sending replaces the earlier answer. After the user
commits, update the page: keep the chosen option, fold the rest into a short
"considered and set aside" note, and remove the form.

If the comparison is only for understanding (no decision), skip the form and
let the user comment on the page, which they can do by selecting text or
clicking an element.

## Example

```html
<main class="fr-page fr-stack">
  <h1>Where should the cache live?</h1>
  <p class="fr-muted">Assuming one process and fewer than 10k entries.</p>
  <section class="fr-grid">
    <article class="fr-card" id="opt-memory">
      <h3>In memory</h3>
      <p><code>cache := map[string]Entry{}</code> guarded by a mutex.</p>
      <p><span class="fr-badge fr-badge--success">no extra I/O</span>
         <span class="fr-badge fr-badge--danger">cold start after every restart</span></p>
    </article>
    <article class="fr-card fr-card--accent" id="opt-disk">
      <h3>On disk <span class="fr-badge fr-badge--accent">recommended</span></h3>
      <p>One JSON file per key, written atomically.</p>
      <p><span class="fr-badge fr-badge--success">survives restarts</span>
         <span class="fr-badge fr-badge--bronze">one read per lookup</span></p>
    </article>
  </section>
</main>
```

## Styling and layout checks

Use forum's classes (`fr-card`, `fr-grid`, `fr-badge`, `fr-table-wrap`), not a
CDN framework. They are defined in `forum-artifact.css`, which forum injects
only while the artifact has no `<style>`, no stylesheet `<link>` and no CSS
framework of its own. Tweak with `style="..."` attributes, or add
`<meta name="forum-style" content="on">` when you need your own `<style>` block
on top (SKILL.md, "When the forum styles apply").

The browser audits layout (clipped text, unreachable controls, text hidden
under another element, sideways scroll) and files findings in the user's
Layout issues tray without waking you. Prevent them: let text wrap, avoid fixed
widths on anything that clips, and check dark, light and a phone-width view
yourself.

## Mistakes to avoid

- Making all options look equally good when you prefer one.
- Comparing summaries when you could show the real thing side by side.
- Leaving out the assumption that would change the recommendation.
- Cards whose sections differ in order or wording, which forces the reader to
  hunt for the matching part.
