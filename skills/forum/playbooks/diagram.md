# Diagram: show how something works

Use it to explain relationships, flows, state machines, architecture and
concepts. The goal is that the reader understands the mechanism after looking
at the picture, and only needs the text for the reasons behind it.

## Pick the tool

- **Hand-written inline SVG is the default.** You control position, size,
  emphasis and grouping, so the layout itself says something: the important
  box is big, the dependency points the way it really goes, the boundary is a
  visible region.
- **Mermaid is opt-in.** Only when the user asks for an editable whiteboard.
  Write `<div class="mermaid">...</div>`; in the forum browser it becomes an
  editable whiteboard the user can redraw, and their edits come back as a
  `tag: whiteboard` prompt (see "Whiteboards" in SKILL.md). Do not pick it just
  because it is quicker to author: it hands the layout to an engine.
- **Not div-and-flexbox boxes with arrows.** Figures are SVG; HTML carries the
  prose around them.

## Compose the explanation

- **Start from the question.** Title each figure with what it answers ("How
  does a prompt get from the page to the agent?"), not with the name of the
  component.
- **Assume no prior knowledge.** Introduce the pieces before the connections
  between them.
- **One idea per figure.** Several small figures that build on each other beat
  a single dense one. For a large system: a small overview of the main parts,
  then a card per part with its own detail below.
- **Overview first, evidence second.** Put the core relationship at the top and
  push file paths, edge cases and caveats under it.
- **Separate topology from detail.** What is connected to what is one picture;
  what flows through each connection is another.
- **Claims need support.** Do not draw an architecture you have not checked in
  the code; cite the file or command next to the figure.

## Drawing it

- Size with `viewBox` and `width="100%"`, never fixed pixel dimensions, and
  keep every shape and label inside the viewBox so nothing is cropped.
- Colors come from forum's classes (`fr-node`, `fr-edge`, `fr-arrow`,
  `fr-label`, and their variants) or from `currentColor` and `--fr-*` custom
  properties, never raw hex. That keeps the figure correct in dark and light.
- Give every meaningful node, edge and region a stable `id` and a `<title>`;
  that is what lets the user comment on exactly one part and lets screen
  readers navigate the figure. Add `role="img"` and an `aria-labelledby` title
  to the `<svg>`.
- Labels stay to a few words. SVG text does not wrap, so brevity is also your
  protection against overflow. Longer explanations go in HTML beside or under
  the figure.
- Emphasize by size, weight and the accent variants (`fr-node--accent`,
  `--success`, `--danger`, `--bronze`), and use `fr-label--muted` for
  secondary text. One accent per figure usually reads better than five.
- Self-contained: no external images, fonts or scripts.
- If you use Mermaid, initialize it from the page theme: read
  `data-fr-theme` on `<html>` (it follows the user's dark/light switch)
  instead of hardcoding a theme.

## Example

```html
<figure class="fr-figure">
  <svg viewBox="0 0 480 120" width="100%" role="img" aria-labelledby="flow-title">
    <title id="flow-title">A prompt travels from the page to the agent</title>
    <defs>
      <marker id="head" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto">
        <path class="fr-arrow" d="M0 0L10 5L0 10z"/>
      </marker>
    </defs>
    <g id="page"><title>The artifact page</title>
      <rect class="fr-node" x="10" y="35" width="120" height="50" rx="6"/>
      <text class="fr-label" x="70" y="65" text-anchor="middle">page</text></g>
    <path id="queue-edge" class="fr-edge" d="M130 60H180" marker-end="url(#head)"><title>queuePrompt</title></path>
    <g id="queue"><title>The user's queue</title>
      <rect class="fr-node fr-node--accent" x="180" y="35" width="120" height="50" rx="6"/>
      <text class="fr-label" x="240" y="65" text-anchor="middle">queue</text></g>
    <path id="send-edge" class="fr-edge" d="M300 60H350" marker-end="url(#head)"><title>Send to Agent</title></path>
    <g id="inbox"><title>The project inbox</title>
      <rect class="fr-node" x="350" y="35" width="120" height="50" rx="6"/>
      <text class="fr-label" x="410" y="65" text-anchor="middle">inbox</text></g>
    <text class="fr-label fr-label--muted" x="240" y="108" text-anchor="middle">the user decides when to send</text>
  </svg>
  <figcaption>Nothing leaves the queue until the user presses Send to Agent.</figcaption>
</figure>
```

## With forum

- When part of the picture is uncertain, label it as a question and attach a
  small decision form (input playbook) so the user resolves it in place.
- A whiteboard's edits arrive as a `tag: whiteboard` prompt with a bounded edit
  summary plus the paths of a scene file and a PNG preview. Read the summary,
  open the files only if you need more, then change the **Mermaid source** in
  the artifact; never try to write the scene back.
- Keep stable ids on nodes: the user's annotation records the element's
  selector, so a figure that keeps its ids keeps its comments attached.

## Styling and layout checks

Use forum's classes, not a CDN framework. The `fr-*` figure classes live in
`forum-artifact.css`, which forum injects only while the artifact has no
`<style>`, no stylesheet `<link>` and no CSS framework of its own. For a
tweak use `style="..."`; if a `<style>` block is unavoidable, add
`<meta name="forum-style" content="on">` so the forum look stays underneath
(SKILL.md, "When the forum styles apply").

The browser audits layout (clipped text, unreachable controls, covered text,
sideways scroll) and files findings in the user's Layout issues tray; they
reach you only if the user queues them. Before handing over, view the figure in
dark, in light and at phone width, and confirm no label is cut off.

## Mistakes to avoid

- One figure containing every file and function.
- Boxes-and-arrows built out of HTML elements.
- Using Mermaid to save effort when nobody asked for a whiteboard.
- Hard-coded colors that vanish in one of the themes.
- Architecture claims you did not verify.
