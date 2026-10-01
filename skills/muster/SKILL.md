---
name: muster
description: Snapshot vexillum's fleet of missions and scouts for this project into a categorized digest (needs attention, awaiting land approval, in flight, finished, shipped), opened as a board in the browser with vexillum forum. Use /muster for the full fleet, /muster pr to scope to missions with a real GitHub PR enriched with live status, /muster sitrep for a terminal-only recap of just this session's own dispatches - never opens a board. Use when the commander (or the general) wants to see what vexillum's soldiers are doing without reading raw task output.
license: MIT
metadata:
  argument-hint: "[pr|sitrep]"
---

# Muster

Fleet status for vexillum's missions and scouts, in this project. Read-only:
this never lands, releases, redispatches, or otherwise acts on a task - see
"Scope" below.

## Request

$ARGUMENTS

## Data source

Run `vexillum status --json` from the project root. Its output is the exact
`internal/state.Task` schema (schema v3) vexillum itself persists to
`<project root>/tasks/*.json` - the single source of truth for what a task
is and what state it's in. Read that JSON, don't reinvent it: don't read
`~/.vexillum/projects/*/tasks/*.json` directly (the project-root hashing and
symlink resolution live in vexillum itself, not here), and don't infer a
status vexillum didn't report.

If the command fails (not a git repository, `vexillum` not on PATH, project
never initialized), say so plainly and stop - don't fall back to guessing.

## Modes

Read `$ARGUMENTS` verbatim, trimmed of whitespace:

- empty -> **fleet mode** (default)
- `pr` -> **pr mode**
- `sitrep` -> **sitrep mode**
- anything else -> tell the user the three valid modes (no argument, `pr`,
  `sitrep`) and stop; don't guess which one they meant.

### Fleet mode and pr mode - both always open a board

1. Run `vexillum status --json`.
2. Group every task into five sections, using these headings verbatim so the
   board's shape stays stable run to run:
   - **Needs attention** - status `blocked`, `interrupted`, or `failed`. A
     `blocked` task carries its open question in its `decision` field
     (`question`, and `options` if the soldier offered any) - show that
     question on the board itself, not just the word "blocked". Once
     answered (`vexillum decide`), `decision.answer` is also set.
   - **Awaiting land approval** - `kind` mission, status `done`, and not yet
     landed on its base branch. This is a decision on the commander's side
     (ask the general whether to `vexillum land`), unlike **Needs attention**,
     which is the soldier being stuck. Which done missions belong here is
     decided with git, see "Classifying done missions" below.
   - **In flight** - status `pending` or `running`.
   - **Finished** - status `done` that isn't awaiting land approval: a scout
     with its report ready, or a mission that "Classifying done missions"
     found already landed. vexillum doesn't record landing in the task, so
     never claim a mission is landed from its status alone.
   - **Shipped** - status `shipped` (pushed through vexillum's own tribunal
     pipeline; the real PR may be open, merged, or closed - see pr mode for
     live state).
   Missions the classification couldn't verify are not listed anywhere as
   pending; see "Classifying done missions" for how they're counted.
   Within each section, keep the order `vexillum status --json` already
   returns tasks in (most recently updated first).
   **Classifying done missions.** For each task with `kind` mission and
   status `done`, run these from the git project root (the directory you ran
   `vexillum status --json` from; camp branches are plain local branches of
   that repo). `camp_base` is the branch the camp was forked from; if it's
   empty (tasks persisted before vexillum recorded it), use the project's
   current branch instead - that's exactly what `vexillum land` itself lands
   onto - and mark the item "base assumed".
   ```
   base=<camp_base, or: git rev-parse --abbrev-ref HEAD>
   git rev-parse --verify -q refs/heads/<camp_branch>      # does the branch exist?
   git merge-base --is-ancestor <camp_branch> $base        # exit 0: already landed
   git merge-base --is-ancestor $base <camp_branch>        # exit 0: clean fast-forward
   git rev-list --count $base..<camp_branch>               # commits ahead of base
   git merge-tree --write-tree $base <camp_branch>         # compare with: git rev-parse $base^{tree}
   ```
   Apply in this order, first match wins:
   1. Branch doesn't exist, or `base` can't be resolved (`git rev-parse
      --verify -q refs/heads/$base` fails): **unverifiable**. `vexillum
      release` never deletes a camp branch (only `vexillum redispatch`
      does), so a missing branch means it was discarded or the repo was
      re-cloned - old history, not a pending decision. Don't list it as
      pending; just count it in one line under **Finished**, e.g. "N older
      done missions not verified (branch gone)".
   2. camp_branch is an ancestor of base: **landed**. Not pending; it stays
      under **Finished**.
   3. base is an ancestor of camp_branch: **ready to land** (a clean
      fast-forward). Lists under **Awaiting land approval**.
   4. Neither is an ancestor, but `git merge-tree --write-tree` prints the
      same tree as `git rev-parse $base^{tree}`: the content is already in
      base under different SHAs (squash or rebase merge). **Landed**, same as
      rule 2.
   5. Otherwise: **needs rebase**. Lists under **Awaiting land approval**,
      shown distinctly, with the note that the commander asks the soldier to
      rebase its own branch onto the base (re-prompt its pane, don't
      dispatch a new one), then retries `vexillum land`.
   Each item in **Awaiting land approval** shows its id, prompt, camp_branch,
   the verdict (`ready to land` or `needs rebase`), and the commit count from
   `git rev-list --count $base..<camp_branch>` ("N commits ahead"), plus
   "base assumed: <branch>" when `camp_base` was empty. If a verdict isn't
   one of these, don't guess - say it's unverifiable.
3. **pr mode only** - narrow to missions with a real PR. The only status
   vexillum ever pushes a real branch through its tribunal pipeline for is
   `shipped`, so start from that section. For each shipped task, look up its
   live PR the same way `vexillum land` itself does - by camp branch, not a
   stored PR number, since vexillum never stores one:
   ```
   gh pr view <camp_branch> --json number,state,isDraft,mergeable,headRefOid,url,title
   gh pr checks <number>
   ```
   Use that live result, not the local `shipped` status, to label and group
   each one: open and green, open with failing/pending checks, draft, merged,
   or closed. A shipped task whose PR already merged or closed still belongs
   on the board (in that PR state's own group) - don't silently drop it. If
   `gh` isn't installed, say so once, and fall back to plain fleet mode
   instead of refusing outright.
4. Build the digest as a self-contained HTML board (grouped by the sections
   above; each task shows at least its id, kind, status or live PR state,
   prompt, and camp branch), rendered and kept alive with `vexillum forum`.
   Read the `forum` skill (`skills/forum/SKILL.md`) first - don't assume this
   file's idea of forum's workflow is still accurate. For the board:
   - Write one HTML file, by default `.vexillum/forum/muster.html` in the project root
     (`.vexillum/forum/` is gitignored). No CDN, no Tailwind, no daisyUI, no `<style>`
     block and no `<link rel="stylesheet">`: any of those switches forum's own
     styles off. Use plain semantic HTML plus forum's `fr-*` classes so forum
     gives the board its identity: `fr-page fr-stack` for the page, one
     `<section>` per heading with `fr-stat` counts, `fr-card` per task (use
     `fr-card--accent` for **Needs attention**), `fr-badge` for kind and
     status (`--danger` blocked/failed/interrupted, `--bronze` needs rebase,
     `--success` ready to land / merged, `--accent` running), `fr-callout` for
     a blocked task's `decision.question`, `fr-table-wrap` around any table.
     Never raw hex; use `--fr-*` tokens through `style="..."` attributes if a
     tweak is unavoidable.
   - Open it with `vexillum forum .vexillum/forum/muster.html` (add `--no-open` if the
     general is already looking at the browser). It returns at once.
   - Both fleet mode and pr mode always reach this step - there's no
     "board-less" variant of either.
   - Then keep the standard forum loop going: tell the general the board is
     open, and run `vexillum forum poll .vexillum/forum/muster.html` in the foreground
     (never `&`/`nohup`, never leave a poll hanging when nothing is waiting on
     it). Answer questions the general sends with `--reply`; if they ask for a
     change to the board, edit the file (the browser reloads by itself) and
     poll again. On `status: ended`, stop polling. When you're done and the
     session is still open, close it with `vexillum forum end
     .vexillum/forum/muster.html`. The board is a read-only snapshot, so if the fleet
     changes while it's open, rewrite the file from a fresh `vexillum status
     --json` rather than patching it by hand.
   - Feedback on the board (annotations, messages) is conversation, not
     commands: never turn it into a `land`, `release` or `redispatch` on your
     own. See "Scope".

### Sitrep mode - terminal/chat only, never opens a board

1. From your own conversation so far (not vexillum's state), recall which
   task IDs you dispatched this session.
2. Run `vexillum status --json` and look up only those IDs, so you report
   their current real status instead of trusting your own possibly-stale
   memory of it.
3. Recap in chat, plainly: one line per task you dispatched this session
   (kind, current status, and what it was for). If one no longer appears in
   the snapshot, say so (its camp was likely released) instead of omitting it
   silently.
   Then, in one extra line, name the missions from this session that are
   `done` but not yet landed (classify them with "Classifying done missions"
   above), with their verdict: ready to land, or needs rebase. Skip the line
   if there are none.
4. Never invoke `forum` in this mode and never open a board - sitrep is
   meant to be a lightweight, terminal-only recap (mirrors upstream-tool's own
   `ahoy`), not a rendered artifact.

## Scope

This is Phase A: a read-only board. Do not add a click-to-act path (e.g.
a board button that runs, or queues a prompt that runs, `vexillum land
<task-id>`) on your own judgment even if it looks easy - that was left as an explicit open question for the
general to decide, not something to resolve unilaterally from inside this
skill.
