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
