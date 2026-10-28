# The sentinel and what to do when it speaks

`vx dispatch` auto-starts a sentinel in the background if none is running, so you
normally do not think about it. It polls every dispatched soldier's live status
and, when one settles, surfaces that to you as a sentinel notice: it keeps your
turn from quietly ending and tells you what changed. Keep doing other things (or
talk to the general) while a soldier works; you will be interrupted with the
update the next time you would otherwise stop. How the notice reaches you depends
on the harness you run in, and the notice you act on is the same: in Claude Code
an async Stop hook that `vx init` registers in `.claude/settings.json`; in Pi an
extension that `vx init` writes to `.pi/extensions/`; in OpenCode the sentinel
itself, which posts the notice into your session.

If you ever need to start one yourself (the auto-start failed; `vx dispatch`
warns on stderr if so), run `vx sentinel` in the background. It refuses a second
one ("a sentinel is already running", naming its pid). That is fine, do not start
another.

If your turn is about to end and you are told a soldier's status changed, that is
the sentinel. Go check on it (report to the general, or land/strike as
appropriate) before actually stopping. Verify before you report a soldier as
finished: look at `git log` and `git status` in its camp rather than trusting the
status word alone. And never judge whether a soldier is alive or waiting from a
`herdr pane read`: the viewport can be stale (see `unconfirmed` below).

## Forum wakes

The same sentinel notice also carries forum feedback. When the user sends feedback
from a review panel opened in this project, a small background listener (not a
soldier, no model) stores it in the project's inbox and rings a forum wake. If
your turn is about to end you are told, in the same notice and with no soldier
involved:

```
forum session <file>: 2 new messages (ended: false). Run vx forum inbox.
```

That is not a soldier status and needs no `vx status`. Follow the `forum` skill:
run `vx forum inbox`, act on the prompts (update the artifact whenever the
feedback changes or decides something), answer with `vx forum reply <file>
--reply "<what changed in the artifact>"` (it does not block), then confirm what you handled
with `vx forum inbox --ack <uid>...`. You never poll for it. The wake is a
doorbell, delivered once: the inbox is the truth, so after a restart or a long
absence run `vx forum inbox` to see anything unread. `(ended: true)` means the
user pressed Send & End: the prompts are final feedback, apply them. A forum
wake for messages you already confirmed is dropped, like a stale soldier wake.

## Statuses

- **done**: the soldier finished. For a mission, report what it did and ask
  whether to land it, unless `vx yolo status` prints `on`: then land it without
  asking (see [landing.md](landing.md), which also says what yolo never
  covers). For a scout, report its findings, then strike the camp.
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
  matters, land it normally first (`vx land <task-id>`). One more cause: a
  soldier that went idle without ever being sent its prompt (the `vx dispatch`
  that launched it died between pasting and submitting it, leaving an unsent
  paste in its pane) is marked interrupted too, and its note says so. Nothing
  ran, so `vx redispatch <task-id>` relaunches it from the original prompt. An
  `unconfirmed` task is different: it was prompted, its pane is open, and
  `vx redispatch` refuses it (carry it on with `vx prompt`).

- **unconfirmed**: the soldier's turn went idle and nothing proves it finished (a
  scout with no report, a mission with no new commit) and it has no valid pause
  declared. It was prompted and its pane is open. The task's `output` carries the
  pane read taken when it was marked and a note saying why, including whether a
  pause file existed earlier and is now gone. Never trust a `herdr pane read`
  for liveness here: the viewport can be stale and show an old "still running"
  screen for a soldier that stopped long ago (it cost 41 minutes once). Verify
  with the facts instead:
  1. `ls ~/.vexillum/projects/<key>/pauses/<agent>.md` (the note in the task's
     output gives the full path): does it exist, and is it fresh (written in the
     last 30 minutes)? A pause file is the only thing that makes
     an idle soldier "waiting".
  2. `herdr pane list`: the pane's `agent_status` (`idle` or `working`).
  3. The task's `output` from `vx status`, plus `git log` / `git status` in the
     camp (or the report path for a scout).

  No fresh pause file, an idle pane and no commit or report means the soldier
  stopped without delivering (typically it woke from a pause, deleted its pause
  file and ended its turn). It is not waiting on anything: re-prompt it with
  `vx prompt <task-id> "<finish the deliverable>"`. Do not explain it away to the
  general ("it is waiting in the background") and do not wait. If the checks
  show it is genuinely working (`agent_status` is `working`), leave it; the
  sentinel reopens it when it works again.

## What a wake is, and what you are not told

A wake is a one-shot note that a task changed status, delivered once. Before
one reaches you the sentinel drops it if it is no longer news: the task's
status has since changed (you answered, re-prompted or shipped it), its camp
was struck, or its mission was landed. A burst of notices for old, finished
tasks after a long absence should not happen; if you want the full picture
after being away, run `vx status` rather than relying on wakes.

A wake reaches the commander that dispatched the task, in the project that
commander lives in, which is not always the project you dispatched into: a
commander working in one repository can dispatch into another and still hears
its soldiers finish, because the task records your pane and the project your
Stop hook (or Pi extension, or OpenCode session) is read from. `vx dispatch`
warns when the two differ; `vx status`, `land`, `strike` and `prompt` still
run from the project the soldier was dispatched into. A wake is consumed only
once it is delivered: if the hook that was to carry it is gone it stays queued
for the next turn. A Claude Code await gives up after 55 minutes, so a commander idle for
longer hears nothing until its next turn (Pi restarts its await by itself,
OpenCode has none), and `vx doctor` warns about wakes
waiting in a project where nothing is listening. After a long absence, read
`vx status` before trusting that you were told about everything.

A `needs-decision:` line only blocks a task when the soldier wrote it in its
final turn, after the end of the dispatched prompt, with real text. The
template line from the prompt, or any angle-bracket placeholder, never counts,
and a question you already answered or dismissed does not block the task again.

## Sending a soldier a follow-up

To give a soldier that already finished more to do (a fix after review, an
extra step), use `vx prompt <task-id> "<text>"`. It sets the task back to
`running` before delivering the text and makes sure a sentinel is watching, so
the soldier's next finish is a transition the sentinel records and wakes you
for. Only a `done`, `shipped` or `unconfirmed` task with its camp still in place
can be prompted (a `blocked` one is answered with `vx decide`, an `interrupted`
one needs `vx redispatch`). Prompting a `shipped` task is how you ask for a fix
on an open PR: it goes `running` while the soldier works and settles as `done`,
not `shipped`, with one notice for that settle; then `vx ship <task-id>` pushes
the fix to the same PR. Like a fresh dispatch, it returns as soon as the soldier is
working (or has already settled) and leaves the task running.

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

The wake path of Claude Code and Pi runs `vx sentinel await` (the Stop hook does it
on every turn end, the Pi extension from inside Pi; OpenCode has none, the
sentinel delivers by itself). Each one records itself in `~/.vexillum/sentinel-awaiters/`,
exits by itself when the process that launched it is gone, and a newer one from
the same session replaces the previous turn's. Starting `vx sentinel` (or any new `await`) also stops
orphans left by a dead session, only ever after verifying the pid is a
`vx sentinel await`. If you still see leftovers, restarting the sentinel
(`vx sentinel`, after stopping the old one) reaps them; never `pkill` by name,
that would also kill the main sentinel and other sessions' awaits.

## Redispatch is destructive

`vx redispatch <task-id>` relaunches the mission from its original prompt in a
fresh camp. It is re-dispatch, not resumption. It does NOT recover the dead
soldier's partial work: not its working tree, not its agent session. It discards
the old camp outright, including any commits never landed there. Because that is
destructive, tell the general what you found in the old camp and ask before
running it. Never redispatch on your own judgment just because a task went
interrupted. Redispatch does not touch the general's browser: if the soldier
drove one (for example through a Chrome extension), tabs it opened stay there
until the general closes them.
