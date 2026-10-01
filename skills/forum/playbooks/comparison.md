<!--
Adapted from upstream (MIT License, Copyright (c) 2026 the upstream author),
src/playbooks.js at v0.1.80 (commit a2a199c), rewritten for vexillum forum
(window.forum and the `vexillum forum` commands). See THIRD-PARTY-NOTICES.md at
the vexillum repo root.
-->

# Playbook: comparison

Use when: show options, tradeoffs, and current vs target behavior.

## Choose

- Use before and after when the same system is changing over time.
- Use option cards when the user must choose between mutually exclusive directions.
- Use a scorecard only when the criteria are explicit and comparable.

## Structure

- Name the decision at the top of the artifact.
- Show the concrete behavior or artifact shape for each side, not just abstract pros and cons.
- End with a recommendation only when the evidence supports one.

## Design rules

- Keep corresponding details aligned so differences are visible without hunting.
- Use visual hierarchy to separate primary tradeoffs from secondary notes.
- Make the cost of each option as visible as the benefit.

## Pitfalls

- Do not make every option look equally recommended if one is clearly preferred.
- Do not compare vague summaries when concrete examples are available.
- Do not bury assumptions that would change the recommendation.

## With forum

- If the goal is selection, give each option a native radio in one `<form data-forum-question="...">` plus a field for the rationale, and queue the chosen option with its rationale once, on submit (input playbook).
- A re-submission replaces the previous unsent answer thanks to the question key.
- The browser audits the page for layout failures (text clipped by a fixed-width `overflow: hidden` box, controls pushed past the viewport, text covered by an opaque sibling, a page that scrolls sideways) and shows them to the user, who may send them to you as a `layout-warnings` prompt. Avoid them up front: let text wrap, keep fixed widths off containers that clip, and check a narrow width.

## Styles (forum-artifact.css)

Use forum's classes, not Tailwind or daisyUI from a CDN. Option cards in a grid, the recommended one accented, each with badges for its cost and benefit:

The forum styles are injected only while the artifact has no `<style>`, stylesheet `<link>` or CSS-framework CDN of its own; for tweaks use `style="..."` attributes, or add `<meta name="forum-style" content="on">` to keep the forum look next to your own `<style>` (SKILL.md, "When the forum styles apply").

```html
<section class="fr-grid">
  <article class="fr-card">
    <h3>A. Rewrite in Go</h3>
    <p>One binary, no runtime.</p>
    <span class="fr-badge fr-badge--success">simple to ship</span>
    <span class="fr-badge fr-badge--danger">3 weeks</span>
  </article>
  <article class="fr-card fr-card--accent">
    <h3>B. Keep the wrapper <span class="fr-badge fr-badge--accent">recommended</span></h3>
    <p>Ship now, revisit later.</p>
    <span class="fr-badge fr-badge--bronze">keeps a Node dependency</span>
  </article>
</section>
```

For before/after of the same thing, use two `fr-card`s with matching headings, and a `fr-table-wrap` table when the criteria are explicit.
