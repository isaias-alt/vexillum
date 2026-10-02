<!--
Adapted from upstream (MIT License, Copyright (c) 2026 the upstream author),
src/playbooks.js at v0.1.80 (commit a2a199c), rewritten for vx forum
(window.forum and the `vx forum` commands). See THIRD-PARTY-NOTICES.md at
the vexillum repo root.
-->

# Playbook: table

Use when: turn dense records into scan-friendly review surfaces.

## Choose

- Use a table when rows share the same fields and the user needs to compare evidence quickly.
- Use cards when each record has a different shape or needs a long explanation.
- Use summaries above the table when counts, risk levels, or statuses change how the table should be read.

## Structure

- Start with a short summary of what the rows prove or require.
- Group columns by the decision they support: identity, evidence, status, action.
- Keep raw details available, but make the primary status visible without reading every cell.

## Design rules

- Use semantic table markup when the data is tabular.
- Protect long paths, code symbols, URLs and prose from overflowing on narrow screens (wrap or contain them deliberately).
- Use restrained color for status and severity so the table stays readable when printed or skimmed, and never use color as the only status signal.

## Pitfalls

- Do not paste a terminal table into HTML and call it done.
- Do not hide the important conclusion below a large undifferentiated grid.

## With forum

- If a row implies a follow-up change, give it an action control that queues a specific prompt: `window.forum.queuePrompt('Rename foo to bar in src/a.go', { tag: 'row-action', text: 'foo', selector: '#row-12', target: { row: 12 } })`. `target` and `selector` tell you exactly which row the prompt is about.
- When one action covers several rows and completeness matters, use the tracked batch pattern from the input playbook so the user submits one explicit set of IDs and you account for each of them.
- The browser audits the page for layout failures (text clipped by a fixed-width `overflow: hidden` box, controls pushed past the viewport, text covered by an opaque sibling, a page that scrolls sideways) and shows them to the user, who may send them to you as a `layout-warnings` prompt. Avoid them up front: let text wrap, keep fixed widths off containers that clip, and check a narrow width.

## Styles (forum-artifact.css)

Use the forum classes, not Tailwind or daisyUI from a CDN. Wrap the table so a wide one scrolls instead of overflowing, and show status as a text badge (never color alone):

The forum styles are injected only while the artifact has no `<style>`, stylesheet `<link>` or CSS-framework CDN of its own; for tweaks use `style="..."` attributes, or add `<meta name="forum-style" content="on">` to keep the forum look next to your own `<style>` (SKILL.md, "When the forum styles apply").

```html
<div class="fr-table-wrap">
  <table>
    <thead><tr><th>File</th><th>Finding</th><th>Status</th></tr></thead>
    <tbody>
      <tr id="row-12"><td><code>internal/forum/hub.go</code></td><td>Queue is not bounded</td>
          <td><span class="fr-badge fr-badge--danger">blocker</span></td></tr>
      <tr><td><code>cmd/vexillum/main.go</code></td><td>Help text is stale</td>
          <td><span class="fr-badge fr-badge--bronze">minor</span></td></tr>
    </tbody>
  </table>
</div>
```

Cards for records with different shapes: `<article class="fr-card"><h3>...</h3><p>...</p><span class="fr-badge">tag</span></article>` inside a `<section class="fr-grid">`.
