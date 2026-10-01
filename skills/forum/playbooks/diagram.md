<!--
Adapted from upstream (MIT License, Copyright (c) 2026 the upstream author),
src/playbooks.js at v0.1.80 (commit a2a199c), rewritten for vexillum forum
(window.forum and the `vexillum forum` commands). See THIRD-PARTY-NOTICES.md at
the vexillum repo root.
-->

# Playbook: diagram

Use when: explain relationships, flows, state, architecture, and concepts with illustrations.

## Choose

- Default to hand-authored inline SVG: it gives proportion, emphasis, spatial metaphor and annotation-ready structure that generated layouts cannot.
- Use Mermaid only when the user asks for an editable whiteboard: a Mermaid diagram in a `<div class="mermaid">` container becomes an editable Excalidraw whiteboard in the forum browser (see the whiteboard section of SKILL.md for how its feedback reaches you).
- For large systems, draw a small overview illustration and put detail in module cards below it, instead of one dense auto-laid graph.

## Structure

- Assume the reader knows nothing about the system: explain from zero.
- Prefer one concept per diagram: a sequence of simple single-concept illustrations over one dense figure; layer understanding step by step.
- Lead with the question the diagram answers, not with the implementation detail that produced it.
- Keep the first visual to the core relationship, then put dense evidence or file references below it.
- For complex systems, separate topology from detail so the overview stays readable.

## Design rules

- Size with `viewBox` plus `width: 100%`; never fixed pixel dimensions, and keep every element inside the viewBox.
- Color through `currentColor` and the page's CSS custom properties so figures follow the artifact's light and dark themes.
- Give every meaningful node, edge and region a stable `id` and a `<title>`.
- Keep labels to a few words and put prose beside the figure in HTML: SVG text does not wrap, so short labels are also the overflow discipline.
- Keep figures self-contained: no external images, fonts or scripts.
- Check the artifact in light, dark and a narrow viewport before handing it over.
- When the user asked for a whiteboard, initialize Mermaid theme-aware (read `data-fr-theme` on `<html>`, which follows the user's dark/light switch) instead of hardcoding one theme.

## Pitfalls

- Do not cram every file or function into one figure when a layered explanation is clearer.
- Do not hand-build boxes-and-arrows from div/flexbox: inline SVG owns figures, HTML owns the prose around them.
- Do not reach for Mermaid to save authoring effort: it surrenders position, size and emphasis to the engine.
- Do not present unverified architecture claims as facts; cite the files or commands that support them.

## With forum

- Make modules, edges and captions easy to discuss: when a relationship is uncertain, label it as a question and add a small decision form so the user can resolve it (input playbook).
- A whiteboard's edits arrive as a `tag: whiteboard` prompt with a bounded summary and two file paths: read the summary, then update the Mermaid source in the artifact.

## Styles (forum-artifact.css)

Draw the figure with forum's SVG classes instead of hard-coded colors, so it follows the dark and light themes, and wrap it in `fr-figure`. No Tailwind, daisyUI or external CSS from a CDN.

The forum styles are injected only while the artifact has no `<style>`, stylesheet `<link>` or CSS-framework CDN of its own; for tweaks use `style="..."` attributes, or add `<meta name="forum-style" content="on">` to keep the forum look next to your own `<style>` (SKILL.md, "When the forum styles apply").

```html
<figure class="fr-figure">
  <svg viewBox="0 0 360 80" width="100%" role="img" aria-labelledby="t1">
    <title id="t1">The commander dispatches a soldier</title>
    <defs><marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path class="fr-arrow" d="M0 0L10 5L0 10z"/></marker></defs>
    <rect id="commander" class="fr-node fr-node--accent" x="10" y="20" width="110" height="40" rx="6"/>
    <text class="fr-label" x="65" y="45" text-anchor="middle">commander</text>
    <path class="fr-edge" d="M120 40H220" marker-end="url(#arrow)"/>
    <rect id="soldier" class="fr-node" x="230" y="20" width="110" height="40" rx="6"/>
    <text class="fr-label" x="285" y="45" text-anchor="middle">soldier</text>
  </svg>
  <figcaption>Dispatch flow</figcaption>
</figure>
```

Node variants: `fr-node--accent`, `--success`, `--danger`, `--bronze`; secondary text: `fr-label fr-label--muted`.
