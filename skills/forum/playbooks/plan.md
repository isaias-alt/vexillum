<!--
Adapted from upstream (MIT License, Copyright (c) 2026 the upstream author),
src/playbooks.js at v0.1.80 (commit a2a199c), rewritten for vexillum forum
(window.forum and the `vexillum forum` commands). See THIRD-PARTY-NOTICES.md at
the vexillum repo root.
-->

# Playbook: plan

Use when: explain a product or technical plan before implementation.

## Choose

- Use this when the user needs to inspect a feature approach before implementation begins, or explicitly asked for a PRD, technical design, implementation plan or proposal.
- Use the comparison or diagram playbook alone when the plan is only a single small design choice.

## Structure

- Start with the goal, the current state, and the desired behavior.
- Then describe the proposed approach, focusing on high level decisions.
- End with the risks you see and the open questions you have, and follow the comparison playbook to give the user options to choose from.

## Design rules

- Verify each claim against the codebase before presenting it as fact; cite files or commands.
- When discussing frontend experiences, prefer visually mocking the experience under the product's own design system over describing it in text.
- The plan must be self-contained enough that another developer can read it and implement the proposal.

## Pitfalls

- Do not leave resolved open questions in the artifact. After a decision, update the content to reflect it and remove the question.
- Do not focus only on the ambiguous decisions and omit the actual proposal.
- Do not omit failure modes, migration concerns, or backwards compatibility questions.

## With forum

- Make the plan and its uncertainties easy to react to: the user can write messages in the panel and answer your open questions through decision forms (see the input playbook), then press Send to Agent.
- After each round of feedback, edit the artifact (the browser reloads it) and `--reply` with what changed.
- The browser audits the page for layout failures (text clipped by a fixed-width `overflow: hidden` box, controls pushed past the viewport, text covered by an opaque sibling, a page that scrolls sideways) and shows them to the user, who may send them to you as a `layout-warnings` prompt. Avoid them up front: let text wrap, keep fixed widths off containers that clip, and check a narrow width.

## Styles (forum-artifact.css)

Build the plan from forum's own classes (details in SKILL.md, "Design: use forum's own styles"); no Tailwind or daisyUI from a CDN. A minimal plan skeleton: a page container, a status badge, a callout for the decision needed, and cards for the approach and risks.

The forum styles are injected only while the artifact has no `<style>`, stylesheet `<link>` or CSS-framework CDN of its own; for tweaks use `style="..."` attributes, or add `<meta name="forum-style" content="on">` to keep the forum look next to your own `<style>` (SKILL.md, "When the forum styles apply").

```html
<main class="fr-page fr-stack">
  <h1>Plan: bounded prompt queue <span class="fr-badge fr-badge--accent">proposal</span></h1>
  <div class="fr-callout fr-callout--info"><strong>Goal.</strong> Never lose a queued prompt, never grow without bound.</div>
  <section class="fr-grid">
    <article class="fr-card"><h3>Approach</h3><p>Cap at 200, persist atomically.</p></article>
    <article class="fr-card fr-card--accent"><h3>Risks</h3><p>A full queue must tell the user, not drop silently.</p></article>
  </section>
  <div class="fr-callout fr-callout--bronze"><strong>Open question.</strong> Cap per session or global?</div>
</main>
```
