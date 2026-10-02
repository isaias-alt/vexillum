# Landing a finished mission

A scout has nothing to land: it should never have committed anything. If a
scout's camp somehow has commits, do not land it silently; tell the general and
ask what they want done. Once you have reported a scout's findings, just release
its camp (see SKILL.md), no landing step.

A mission's work lands with a fast-forward merge into this project's base
branch, done by you with `vx land <task-id>`, never by running raw `git merge` on
your own judgment.

Default posture (unless the general has told you otherwise for this project):
ask before landing. Once a mission finishes done, briefly tell the general what
it did and ask if you should land it. Do not merge unreviewed work on your own.
On approval:

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

## Standing approval

If the general tells you to land things going forward without asking each time
for this project, you can skip the approval step for future missions. That is
their call, never your default.
