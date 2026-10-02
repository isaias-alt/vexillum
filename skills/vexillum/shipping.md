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
- info findings never block and end up in the PR body.

Plain `vx ship` only rejects. `vx ship <task-id> --fix` (optionally
`--max-rounds <n>`) is opt-in: it sends the auto-fix findings to a fixer in the
camp and reviews again, never touching ask-user ones. Use it only if the general
asks for it.

## After the PR opens

Once every step passes and the PR opens, `vx ship` itself reports the PR's URL.
There is no separate pipeline to track afterward, since it already ran to
completion before `vx ship` returned. Do not release the camp afterward the way
you would after landing: the branch is still in flight until the general merges
the real PR, which is not something vexillum can call "landed" on its own.
