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
the tribunal runs. Re-shipping a mission whose PR is already open leaves the
PR's text alone, so fix a PR by hand with `gh pr edit` if it needs it.

## After the PR opens

Once every step passes and the PR opens, `vx ship` itself reports the PR's URL.
There is no separate pipeline to track afterward, since it already ran to
completion before `vx ship` returned. Do not release the camp afterward the way
you would after landing: the branch is still in flight until the general merges
the real PR, which is not something vexillum can call "landed" on its own.
