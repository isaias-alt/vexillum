---
name: muster
description: Snapshot vexillum's fleet of missions and scouts for this project into a categorized digest (needs attention, in flight, finished, shipped), opened as an interactive board. Use /muster for the full fleet, /muster pr to scope to missions with a real GitHub PR enriched with live status, /muster sitrep for a terminal-only recap of just this session's own dispatches - never opens a board. Use when the commander (or the general) wants to see what vexillum's soldiers are doing without reading raw task output.
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
2. Group every task into four sections, using these headings verbatim so the
   board's shape stays stable run to run:
   - **Needs attention** - status `blocked`, `interrupted`, or `failed`.
   - **In flight** - status `pending` or `running`.
   - **Finished** - status `done` (a mission ready to land, or a scout with
     its report ready - vexillum doesn't record whether a `done` mission has
     actually been landed yet, so don't claim it has or hasn't).
   - **Shipped** - status `shipped` (pushed through the no-mistakes gate; the
     real PR may be open, merged, or closed - see pr mode for live state).
   Within each section, keep the order `vexillum status --json` already
   returns tasks in (most recently updated first).
3. **pr mode only** - narrow to missions with a real PR. The only status
   vexillum ever pushes a real branch through the no-mistakes gate for is
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
4. Build the digest as an HTML board (grouped by the sections above; each
   task shows at least its id, kind, status or live PR state, prompt, and
   camp branch). Before writing it, get current guidance from the `lavish`
   skill the way its own file says to - don't assume this file's own idea of
   lavish's workflow is still accurate:
   ```
   npx -y lavish-axi --help
   npx -y lavish-axi design
   ```
   Then render the HTML and open it with `npx -y lavish-axi <file>`. Both
   fleet mode and pr mode always reach this step - there's no "board-less"
   variant of either.

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
4. Never invoke `lavish` in this mode and never open a board - sitrep is
   meant to be a lightweight, terminal-only recap (mirrors firstmate's own
   `ahoy`), not a rendered artifact.

## Scope

This is Phase A: a read-only board. Do not add a click-to-act path (e.g.
routing a board click to `vexillum land <task-id>`) on your own judgment even
if it looks easy - that was left as an explicit open question for the
general to decide, not something to resolve unilaterally from inside this
skill.
