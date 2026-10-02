# The sentinel and what to do when it speaks

`vx dispatch` auto-starts a sentinel in the background if none is running, so you
normally do not think about it. It polls every dispatched soldier's live status
and, when one settles, surfaces that to you: a Claude Code Stop hook (wired up by
`vx init`) blocks your turn from quietly ending and tells you what changed. Keep
doing other things (or talk to the general) while a soldier works; you will be
interrupted with the update the next time you would otherwise stop.

If you ever need to start one yourself (the auto-start failed; `vx dispatch`
warns on stderr if so), run `vx sentinel` in the background. It refuses a second
one ("a sentinel is already running", naming its pid). That is fine, do not start
another.

If your turn is about to end and you are told a soldier's status changed, that is
the sentinel. Go check on it (report to the general, or land/release as
appropriate) before actually stopping. Verify before you report a soldier as
finished: look at `git log` and `git status` in its camp rather than trusting the
status word alone.

## Statuses

- **done**: the soldier finished. For a mission, report what it did and ask
  whether to land it (see [landing.md](landing.md)). For a scout, report its
  findings, then release the camp.
- **blocked**: the soldier asked a genuine question. It sits in the task's
  `decision` field. Bring it to the general; once answered, relay it with
  `vx decide <task-id> <answer>`. Report a blocked task plainly, no flavor.
- **interrupted**: the sentinel lost the soldier itself, not a failure at its
  task: its herdr pane disappeared (closed by hand, or herdr restarted) while it
  was working. Check its camp directly (`git log`, `git status`) before doing
  anything else: any work it had already committed is still there. If that work
  matters, land it normally first (`vx land <task-id>`).

## Redispatch is destructive

`vx redispatch <task-id>` relaunches the mission from its original prompt in a
fresh camp. It is re-dispatch, not resumption. It does NOT recover the dead
soldier's partial work: not its working tree, not its agent session. It discards
the old camp outright, including any commits never landed there. Because that is
destructive, tell the general what you found in the old camp and ask before
running it. Never redispatch on your own judgment just because a task went
interrupted. If the dead soldier had a browser open (chrome-devtools-tool),
redispatch also stops that orphaned browser process on its own.
