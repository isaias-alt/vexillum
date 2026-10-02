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

## Forum wakes

The same Stop hook also carries forum feedback. When the user sends feedback
from a review panel opened in this project, a small background listener (not a
soldier, no model) stores it in the project's inbox and rings a forum wake. If
your turn is about to end you are told, in the same hook message and with no
soldier involved:

```
forum session <file>: 2 new messages (ended: false). Run vx forum inbox.
```

That is not a soldier status and needs no `vx status`. Follow the `forum` skill:
run `vx forum inbox`, act on the prompts, answer with `vx forum reply <file>
--reply "<what you did>"` (it does not block), then confirm what you handled
with `vx forum inbox --ack <uid>...`. You never poll for it. The wake is a
doorbell, delivered once: the inbox is the truth, so after a restart or a long
absence run `vx forum inbox` to see anything unread. `(ended: true)` means the
user pressed Send & End: the prompts are final feedback, apply them. A forum
wake for messages you already confirmed is dropped, like a stale soldier wake.

## Statuses

- **done**: the soldier finished. For a mission, report what it did and ask
  whether to land it (see [landing.md](landing.md)). For a scout, report its
  findings, then release the camp.
- **blocked**: the soldier asked a genuine question. It sits in the task's
  `decision` field. Bring it to the general; once answered, relay it with
  `vx decide <task-id> <answer>`. Report a blocked task plainly, no flavor.
  If the block is a false positive (the soldier never asked anything: the
  question reads like the template line `<a one-line summary ...>`, or the pane
  shows the soldier finished), clear it with `vx decide <task-id> --dismiss`
  instead. That sends the soldier nothing; the task becomes what its pane says
  (running, done, or interrupted), and it is refused if the pane is really
  blocked. `vx prompt` and `vx ship` refuse a blocked task and name this flag.
- **interrupted**: the sentinel lost the soldier itself, not a failure at its
  task: its herdr pane disappeared (closed by hand, or herdr restarted) while it
  was working. Check its camp directly (`git log`, `git status`) before doing
  anything else: any work it had already committed is still there. If that work
  matters, land it normally first (`vx land <task-id>`).

## What a wake is, and what you are not told

A wake is a one-shot note that a task changed status, delivered once. Before
one reaches you the sentinel drops it if it is no longer news: the task's
status has since changed (you answered, re-prompted or shipped it), its camp
was released, or its mission was landed. A burst of notices for old, finished
tasks after a long absence should not happen; if you want the full picture
after being away, run `vx status` rather than relying on wakes.

A `needs-decision:` line only blocks a task when the soldier wrote it in its
final turn, after the end of the dispatched prompt, with real text. The
template line from the prompt, or any angle-bracket placeholder, never counts,
and a question you already answered or dismissed does not block the task again.

## Sending a soldier a follow-up

To give a soldier that already finished more to do (a fix after review, an
extra step), use `vx prompt <task-id> "<text>"`. It sets the task back to
`running` before delivering the text and makes sure a sentinel is watching, so
the soldier's next finish is a transition the sentinel records and wakes you
for. Only a `done` or `unconfirmed` task with its camp still in place can be
prompted (a `blocked` one is answered with `vx decide`, an `interrupted` one
needs `vx redispatch`). It waits a few seconds for a trivial prompt to settle,
otherwise leaves the task running, like a fresh dispatch.

What you send with `vx prompt` (and the answer text you send with `vx decide`,
not `--dismiss`) also counts as the general's intent for the tribunal: once
delivered, it is recorded on the task as an amendment, with a timestamp and the
command it came from. `vx ship` hands the review the dispatch prompt followed by
these amendments, labeled as instructions the general gave afterward, so a
component you asked for after dispatch is not reported as "not required". The
dispatch prompt is never rewritten. Only these two commands, run by you, write
amendments, and they live in the task's state file, never in a file inside the
camp, so a soldier cannot widen its own mandate. Each is capped at 2000
characters and a task keeps its 20 most recent. So when the general widens the
scope, relay it through `vx prompt` or `vx decide` rather than by hand in the
pane, or the review will not know about it.

Do not prompt the soldier's pane by hand with `herdr agent prompt`: the task
would stay `done` and a second finish would never notify you. The sentinel does
reopen a settled task it later sees working again, but only on a poll, so a
short follow-up that finishes between two polls could still slip past.
`vx prompt` is the supported path.

## Stray `vx sentinel await` processes

The Stop hook runs `vx sentinel await` on every turn end. Each one records
itself in `~/.vexillum/sentinel-awaiters/`, exits by itself when the hook that
launched it is gone, and a newer one from the same session replaces the
previous turn's. Starting `vx sentinel` (or any new `await`) also stops
orphans left by a dead session, only ever after verifying the pid is a
`vx sentinel await`. If you still see leftovers, restarting the sentinel
(`vx sentinel`, after stopping the old one) reaps them; never `pkill` by name,
that would also kill the main sentinel and other sessions' hooks.

## Redispatch is destructive

`vx redispatch <task-id>` relaunches the mission from its original prompt in a
fresh camp. It is re-dispatch, not resumption. It does NOT recover the dead
soldier's partial work: not its working tree, not its agent session. It discards
the old camp outright, including any commits never landed there. Because that is
destructive, tell the general what you found in the old camp and ask before
running it. Never redispatch on your own judgment just because a task went
interrupted. If the dead soldier had a browser open (chrome-devtools-tool),
redispatch also stops that orphaned browser process on its own.
