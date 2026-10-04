# Shipping through vexillum's own tribunal pipeline

`vx land` merges locally: no PR, no review pipeline. It is the right default for
most missions. If the general instead wants a real, validated GitHub PR for a
finished mission, offer `vx ship <task-id>`. Ask first, same as landing, but
treat this one as more consequential: it produces a real PR outside the machine.
You never decide on your own that a mission is "ready to ship" the way you might
decide it is ready to land; the pipeline decides, deterministically.

It runs vexillum's tribunal (lint, tests, an adversarial review of the diff by a
fresh session that reads the diff itself, a docs check) against the mission's
camp, synchronously, and only pushes and opens the pull request once every step
has passed.

Requires the `gh` binary installed (`vx doctor` reports whether it is). `vx ship`
refuses up front and says so if it is missing. There is no other setup: the
pipeline runs entirely inside vexillum, with no external tool to install or
configure.

## The general's later instructions count as intent

The review judges every component the change introduced against the mission's
intent: the dispatch prompt followed by the instructions the general gave
afterward. What you send with `vx prompt` and the answers you give with
`vx decide` (not `--dismiss`) are recorded on the task as amendments, with a
timestamp and the command they came from, and `vx ship` lists them after the
prompt, in order, labeled as given afterward. So a restyle, a removed shortcut
or an added retry the general asked for through those commands is not reported
as "not required"; whatever neither the prompt nor an amendment required still
is. The dispatch prompt itself is never rewritten.

Only those two commands, run by you, write amendments, and they live in the
task's state file, never in a file inside the camp, so a soldier cannot widen
its own mandate. Each is capped at 2000 characters and a task keeps its 20 most
recent. Scope changes given by hand in the soldier's pane are not recorded: when
the general widens the mission, relay it through `vx prompt` or `vx decide`.

## When a step fails

`vx ship` reports which step failed and why, and nothing is pushed. A review that
blocks prints its findings:

- error or warning findings block, including ask-user ones, which need the
  general's decision. Bring those to the general, do not decide for them.
- info findings never block and end up in the PR body, under Tribunal notes.

Plain `vx ship` only rejects. `vx ship <task-id> --fix` (optionally
`--max-rounds <n>`) is opt-in: it sends the auto-fix findings to a fixer in the
camp and reviews again, never touching ask-user ones. Use it only if the general
asks for it.

## The PR's title and body

The PR lives on a public repo, and its text needs no manual step: the tribunal
writes it. The review session, which reads the whole diff and the commits with no
author context, also returns a conventional-commit style `pr_title` (about 72
characters) and a short factual `pr_description` (what changed and why, 3 to 8
lines). `vx ship` uses them as the title and the What section. They are written
from the diff, never from the dispatch prompt, which `vx ship` never copies into
the PR.

If the reviewer omits them or returns invalid ones (a title that is not
conventional-commit style or not one line, a description with headings, or any
text that fails the sanitizer), `vx ship` says so and falls back, per field, to
text derived from the branch: the subject of the only commit, or of the newest
`feat`, `fix` or `docs` commit, or of the first commit, and the commit subjects as
a bullet list. The body also carries the diff stat and top-level areas touched,
Verification (the tribunal steps that passed, review rounds and fix rounds),
Tribunal notes (the info findings) and a one-line footer naming the mission.

Overrides are optional, for when the proposed text is not what you want:

- `--title "<text>"` replaces the title.
- `--body "<text>"` or `--body-file <path>` (`-` reads stdin) replaces the What
  section. The diff stat, Verification, Tribunal notes and footer stay.

Everything that comes from the camp, the reviewer or your own `--body` is
sanitized first: lines with an absolute home path, a localhost port or something
that looks like a secret are dropped, as is any line quoting the mission prompt.
`vx ship` says how many lines it dropped. An unsafe `--title` is refused before
the tribunal runs. Shipping a mission whose branch already has an open PR (see
below) reuses that PR and refreshes its title and description from the new
review, or from `--title` and `--body`, so a text you edited by hand on GitHub is
replaced.

## After the PR opens

Once every step passes and the PR opens, `vx ship` itself reports the PR's URL.
There is no separate pipeline to track afterward, since it already ran to
completion before `vx ship` returned. The mission is now `shipped`: pushed, with
a real PR open, but not landed locally.

A shipped soldier waits on purpose. `vx ship` does not strike its camp: the
soldier's pane and worktree stay open until the general merges the PR, so if CI,
a reviewer or the general asks for changes, the same soldier still has the
context and the branch mounted. It is not stuck or stranded. It shows as
`shipped` in `vx status` and under Shipped on the muster board, so never
strike it, redispatch it or report it as a problem just because it is idle.

To ask it for a fix, use `vx prompt <task-id> "<text>"`: it accepts a `shipped`
task like any finished one, delivers the text to the soldier's pane and records it
as an amendment. The task goes `running` while the soldier works, and when it
settles it is `done` again, not `shipped`, because the new commits are not on the
PR yet. You get one notice for that settle. Then run `vx ship <task-id>` again:
the full tribunal runs, the commits are pushed to the same branch, and because a
PR for the branch already exists `vx ship` reuses it (it never opens a second
one) and refreshes its title and description. If that PR is already merged or was
closed, `vx ship` refuses before running anything: a merged one needs `git pull`,
`vx strike` and a new mission for the follow-up.

If the soldier rebased or amended the branch (for example after you asked it to
catch up with the base), the next push is non-fast-forward, and `vx ship` handles
it by itself, safely. After each successful push it records the tip it pushed on
the task. When origin rejects a push as non-fast-forward it reads origin's tip for
the branch: if that is exactly the recorded tip, the rewrite is its own, so it
retries once with `git push --force-with-lease=<branch>:<recorded sha>` (the
mission branch only, never the base branch or any other ref) and prints one line,
"rewrote the PR branch ... from <old> to <new>". It then refreshes the PR and
prints its URL as usual. Do not force-push the branch yourself.

When the tip is anything else, or no push is recorded (a task shipped before this
was tracked), `vx ship` never forces. It fails saying what is on origin and how to
decide, so bring that to the general: they can run `git fetch origin <branch>` in
the camp and inspect it, or authorize one manual
`git push --force-with-lease=<branch>:<origin sha> origin <branch>`, after which
`vx ship <task-id>` is run again. Never take that decision for them.

To finish it, merge the PR (the general on GitHub, or `vx land <task-id>`, which
merges the real PR once it is open, not a draft, mergeable and green). Then, in
the same turn:

```
git pull
vx strike <task-id>
```

Run `git pull` in the project's own checkout, on the base branch. `vx strike`
never fetches: it compares the camp against the local base branch only, so right
after a remote merge and before the pull it refuses, and the refusal says to
merge the pull request and run `git pull` on the base branch, then retry. It
leaves the camp and pane untouched. After the pull it passes, even for a squash
or rebase merge, because it also accepts a camp whose content the base already
contains. It still refuses a camp with uncommitted changes.

For a shipped task only, `vx strike` also asks `gh` about the PR, best effort
(`gh` installed and logged in, otherwise it silently relies on the local checks).
When the PR is `MERGED` it trusts that over the content check, which can refuse
forever after a remote merge when later commits touched the same lines, provided
the PR's merge commit is on the local base branch. If it is not yet, the refusal
says to `git pull`. A camp holding commits the merged PR never carried is still
refused. A task that is not shipped never triggers a network call.

After a successful strike it also deletes the task's local branch
`vexillum/<task-id>` with `git branch -d` semantics (never `-D`) when the branch
is an ancestor of the base, or when the PR is `MERGED` on a pulled base. A branch
checked out in another worktree is never deleted. Otherwise it keeps the branch
and prints one line saying why and what the general can do. It also runs
`git worktree prune` and prints what it pruned.

## When strike still refuses: `--discard`

If the check cannot pass and the general confirmed that the camp's work is
already on the base or is abandoned, `vx strike <task-id> --discard` strikes the
camp anyway. It prints each unlanded commit (hash and subject) and each
uncommitted change it threw away, and resets the worktree so the slot can be
reused. **Never use `--discard` without the general's approval**: ask first, say
what would be lost, and run it only after an explicit yes. A refusal that says to
merge and pull is answered by merging and pulling, not by `--discard`.
