# Landing a finished mission

A scout has nothing to land: it should never have committed anything. If a
scout's camp somehow has commits, do not land it silently; tell the general and
ask what they want done. Once you have reported a scout's findings, just release
its camp (see SKILL.md), no landing step.

A mission's work lands with a fast-forward merge into this project's base
branch, done by you with `vx land <task-id>`, never by running raw `git merge` on
your own judgment.

Default posture: ask before landing, unless yolo mode is on (see "Yolo mode"
below). Once a mission finishes done, briefly tell the general what it did and
ask if you should land it. Do not merge unreviewed work on your own. On
approval:

```
vx land <task-id>
```

Verify the base branch actually moved (`git log`) before reporting the mission as
landed, then release its camp in the same turn.

It refuses and leaves everything untouched if this project's own checkout is
dirty, or if the mission's branch has diverged from the base (not a clean
fast-forward). It never forces or rebases anything. If it refuses, tell the
general instead of retrying blindly.

## Divergence between sibling missions

Divergence is expected when you land several sibling missions one at a time:
landing the first moves the base, so every other mission dispatched from that
same starting point stops being a clean fast-forward. Nothing went wrong. If the
general wants it landed too, ask the soldier itself (re-prompt its still-open
pane, do not dispatch a fresh one) to rebase its branch onto the updated base,
then retry `vx land`. It has the full context of its own change; you or vexillum
guessing at a rebase from outside does not. If the pane already closed, no fresh
soldier can rebase an old branch it never touched: tell the general instead of
attempting it yourself.

## A dirty checkout

A dirty checkout is often just vexillum's own scaffold (the managed block in
AGENTS.md, `.claude/skills/`, `.vexillum/`) never having been committed: `vx init`
writes those files but never commits them. If you notice uncommitted scaffold
files (or anything else untracked) before you ever dispatch a soldier, commit
them yourself with the general's ok early, instead of hitting the refusal later
while a mission is waiting to land.

## Yolo mode

Yolo is a per-project setting the general turns on with `vx yolo on` (and off
with `vx yolo off`). It is off by default, and you never turn it on yourself or
infer it from a single exchange: that is the general's call. Check it with:

```
vx yolo status
```

It prints `on` or `off`. Only a literal `on` counts: `off`, an error or any
other output means ask first, as usual. Run it again for each mission, since the
general may have changed it.

When it is `on` and a mission finished done and you have verified it (you read
what it did, and the work matches the request), do not ask: run `vx land
<task-id>`, verify the base branch actually moved (`git log`), report the
mission as landed and release its camp in the same turn. The approval question
is the only thing yolo skips; every other rule here still holds:

- Verify the base moved before reporting it landed, as always.
- Never use `vx release --discard`. Yolo does not approve discarding anything.
- Never land a scout. A scout has nothing to land, with or without yolo.
- Yolo never covers `vx ship`. Opening a real pull request stays an explicit
  decision of the general, so ask before shipping even when yolo is on.
- Yolo does not cover pushing anything, or any other outward-facing action.
- A refused land (dirty checkout) or a diverged branch is not yolo's to work
  around: tell the general, as in the sections above. Do not retry blindly,
  rebase or commit on your own to get past it. The setting lives in
  `.vexillum/yolo.json`, which is committed like `.vexillum/models.json`:
  if the general just turned yolo on and the land is refused for a dirty
  checkout, that file may be the uncommitted change, so tell the general.
- A mission that finished blocked, failed or interrupted is not "done and
  verified": report it as usual and do not land it.

Without yolo on, the general can still tell you in plain words to land one
specific mission: that is a one-off approval for that mission, nothing more.
