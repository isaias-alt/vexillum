# Table: dense records the user can scan and act on

Use it when the content is many records of the same kind (findings, files,
tasks, test results, options with criteria) and the user's job is to scan them,
spot what matters and maybe act on some rows.

## Table or something else

- **Table**: every row has the same fields and the reader compares across rows.
- **Cards** (`fr-card` in an `fr-grid`): records differ in shape, or each needs
  a paragraph of explanation. A table with a "Notes" column that holds
  paragraphs wants to be cards.
- **Stats above the table** (`fr-stat`): when a count or a ratio changes how
  the rows should be read ("3 blockers out of 41").

## Anatomy

1. **A verdict line first.** One or two sentences saying what the rows add up
   to and what, if anything, the user must do. A reader who stops here should
   still know the answer.
2. **Optional stat strip.** A few `fr-stat` tiles with the counts that matter.
3. **The table.** Columns follow the questions a reviewer asks, left to right:
   which record is it, what is the evidence, how bad or how done is it, what
   happens next. Put the status near the left, not in the last column of a
   wide table that scrolls out of view.
4. **Detail on demand.** Long output, stack traces and full diffs go in a
   `<details>` under the row or on a card, so the grid stays one line per row.

## Making the grid readable

- Use real `<table>`, `<thead>`, `<th scope>` markup. Wrap it in
  `fr-table-wrap` so a wide table scrolls inside its box rather than pushing
  the page sideways.
- Status is a word inside a badge (`blocker`, `ok`, `needs info`), never only
  a color. Reserve the loud variants (`fr-badge--danger`) for what really needs
  attention, so they still mean something.
- Long paths, symbols and URLs need a plan for narrow screens: allow them to
  wrap (`overflow-wrap: anywhere` in a `style` attribute) or let the wrapper
  scroll. Do not clip them.
- Sort the rows by the user's priority (worst first), not by whatever order
  your tool printed them in. Say how they are sorted.
- Keep short identifiers on one line (`white-space: nowrap` on the cell), or a
  narrow screen will break `F-1` at its hyphen.
- Give each row you may later refer to a stable `id` (`id="row-12"`), so the
  user's annotations and your own prompts point at the same element.

## Acting on rows with forum

A row that implies a change can carry its own button (see the example below
for the markup). Queue one specific prompt, and describe the row precisely so
you need nothing else to act:

```html
<script>
  document.querySelectorAll('button[data-row]').forEach((button) => {
    button.addEventListener('click', () => {
      const row = button.closest('tr');
      window.forum.queuePrompt(
        'Handle the unbounded retry in ' + row.dataset.path,
        { tag: 'row-action', text: row.dataset.path, selector: '#' + row.id,
          target: { row: Number(button.dataset.row), path: row.dataset.path } });
    });
  });
</script>
```

An action button is a deliberate click on an unambiguous "queue this" control,
so queueing on click is fine here. That is different from the rule in the
input playbook: never queue from a control the user is still adjusting.

If one decision covers several rows and none may be forgotten, use the
**tracked batch** from the input playbook: a checkbox or select per row, a
single submit, and a receipt from you that accounts for every submitted ID.

## Example

```html
<main class="fr-page fr-stack">
  <h1>Retry audit <span class="fr-badge fr-badge--accent">12 findings</span></h1>
  <p>Two retry loops can run forever; the rest are cosmetic. Fix F-1 and F-2
     before release.</p>
  <div class="fr-table-wrap">
    <table>
      <thead>
        <tr><th scope="col">ID</th><th scope="col">Status</th>
            <th scope="col">Where</th><th scope="col">Finding</th><th scope="col">Action</th></tr>
      </thead>
      <tbody>
        <tr id="f-1" data-path="internal/sync/push.go">
          <th scope="row" style="white-space:nowrap">F-1</th>
          <td><span class="fr-badge fr-badge--danger">blocker</span></td>
          <td><code>internal/sync/push.go</code></td>
          <td>Retry loop has no upper bound</td>
          <td><button type="button" class="fr-btn" data-row="1">Queue fix</button></td>
        </tr>
        <tr id="f-7" data-path="cmd/vx/help.go">
          <th scope="row" style="white-space:nowrap">F-7</th>
          <td><span class="fr-badge fr-badge--bronze">minor</span></td>
          <td><code>cmd/vx/help.go</code></td>
          <td>Help text mentions a removed flag</td>
          <td><button type="button" class="fr-btn" data-row="7">Queue fix</button></td>
        </tr>
      </tbody>
    </table>
  </div>
</main>
```

## Styling and layout checks

Use forum's own classes (`fr-table-wrap`, `fr-badge`, `fr-stat`, `fr-card`,
`fr-grid`), not a CDN framework. They come from `forum-artifact.css`, which is
injected only while the artifact brings no `<style>`, stylesheet `<link>` or
CSS-framework CDN. For one-off tweaks use `style="..."`; if you truly need a
`<style>` block, add `<meta name="forum-style" content="on">` to keep the forum
look under it (SKILL.md, "When the forum styles apply").

The browser audits layout in the background (clipped text, controls pushed
off-screen, text under another element, sideways scrolling) and lists what it
finds in the user's Layout issues tray. It never wakes you; you hear about it
only if the user queues the fixes. So prevent it yourself: look at the table in
dark and light, and at phone width, before you hand it over.

## Mistakes to avoid

- Pasting terminal output into a `<pre>` and calling it a table.
- A conclusion buried under a screen of undifferentiated rows.
- Status encoded only in color, or in a column nobody sees on a small screen.
- Fixed-width cells that clip a path instead of letting it wrap.
