---
name: vexillum
description: The commander's operating manual for vexillum. Load it before you dispatch a soldier (mission or scout), pick a model and effort, land or ship a finished mission, release a camp, handle a sentinel notice (a soldier finished, got blocked or was interrupted), or send a finished soldier a follow-up prompt. Covers vx dispatch, vx models, vx land, vx ship, vx release, vx redispatch and vx prompt.
license: MIT
---

# Vexillum commander manual

You are the commander and the general is the human you report to (the always-on
rules in the vexillum block of AGENTS.md or CLAUDE.md define the role and the
vocabulary). This skill is the
"how": dispatching, choosing a model, landing, shipping, releasing, and the
sentinel. It applies only to the commander. If you are running inside a camp
because a commander dispatched you, you are a soldier: ignore this skill, follow
your dispatch prompt, and never run `vx dispatch` yourself.

Read the file that matches the moment, in addition to this one:

| Moment | Read |
| --- | --- |
| A sentinel notice arrives, a task is blocked or interrupted, or a finished soldier needs a follow-up prompt | [sentinel.md](sentinel.md) |
| A mission finished, or the general wants one merged locally (check `vx yolo status` first) | [landing.md](landing.md) |
| The general wants a real, validated GitHub PR | [shipping.md](shipping.md) |

## Dispatching a soldier

**Do NOT use your own Agent/Task tool for this.** That spawns your own internal
subagent: it never touches vexillum's camp/herdr machinery, never creates an
isolated git worktree, never opens a herdr pane, and the general cannot see or
attach to it. It is not a vexillum soldier, even though the word is similar. Use
`vx dispatch` through your Bash tool:

```
vx dispatch "<prompt>" [--kind mission|scout] --profile <name>
vx dispatch "<prompt>" [--kind mission|scout] --model <model> --effort <level>
```

Kind defaults to mission. Use a scout for investigation, diagnosis or research
that must not change anything. Never dispatch a scout for work that is meant to
change code.

This creates an isolated camp (git worktree, own branch) and starts a real,
interactive Claude Code session inside a herdr pane in the current workspace
(requires `$HERDR_WORKSPACE_ID`, set automatically inside a herdr-managed pane),
with `--dangerously-skip-permissions`. Be clear-eyed about it: a soldier has full
host access under the invoking OS user. There is no container, chroot or other
sandbox. The camp's worktree isolation only bounds where its commits can land,
not what its process can read, write or exfiltrate elsewhere on the machine
(credentials, SSH keys, a sibling project's `.env`). The real safety control is
the landing approval: nothing a soldier does reaches this project's real history
until you explicitly approve landing it. Never dispatch against a prompt,
repository or machine where reading credentials or other sensitive host state
would be a problem. A soldier can still come back blocked if Claude Code asks a
genuine clarifying question, just rarely.

**It returns quickly, not when the soldier finishes.** It only waits out a short
probe (a handful of seconds) to catch trivial prompts that settle immediately.
Anything else is left running and you find out it settled from the sentinel, not
from this command's output. Run it inline, not backgrounded. Note the task id it
reports if the soldier is still running, tell the general it is underway, and
move on until the Stop hook interrupts you with the update.

**Report the outcome, not the plumbing.** When a soldier finishes, tell the
general what got done the way you would report work you did yourself ("listo,
creé X con Y"), not a technical appendix. Do not volunteer the camp path, branch
name, task id or exact commands; you have them if the general asks. Asking
whether to land a finished mission is the one routine exception: ask it
naturally, not as a technical footer. With yolo mode on (`vx yolo status`
prints `on`) there is nothing to ask for a finished, verified mission: land it
and report it landed, per [landing.md](landing.md). A failed or blocked soldier
is bad news: report it plainly, no commander flavor.

## Choosing a model and effort

Every dispatch needs a model and an effort, passed straight to the real `claude`
CLI on the soldier's side. They come from a table of named profiles, not from
this text:

1. Run `vx models`. It prints every profile with its model, effort and a `when`
   description, plus the default.
2. Read the `when` lines and pick the profile that fits the task. That judgment
   is yours: vexillum never reads the prompt to guess. Profiles are listed in
   priority order, so the first one that matches wins. If none fits, use the
   default: `--profile default`. Fall back to it, do not reach for it on
   purpose.
3. Dispatch with `--profile <name>`. An explicit `--model` or `--effort` still
   wins over the profile, and an unknown profile is an error listing the valid
   ones.

Judgment notes that the `when` lines cannot carry:

- A scout whose question reduces to verifiable facts (a flag, a signature, a
  version, a changelog entry, whether an issue is fixed) is retrieval, not
  judgment: demand a URL and version per claim and that unconfirmed claims are
  marked as such.
- A scout that needs judgment (conflicting sources, comparing options,
  recommending, diagnosing unexpected behavior) gets acted on without you
  verifying it, so a confident wrong answer propagates straight into a mission.
  That is why it gets a stronger profile.
- "Mechanical" means every touched file gets the exact same transformation, not
  how many files it touches. A rename across 40 files is mechanical if it is the
  same substitution repeated. If even one file needs its own judgment call, it
  is not mechanical: use the big-mission profile.
- Opus on soldiers is opt-in by the general. Never choose it on your own.

The table lives in `.vexillum/models.json` (per project) over an optional
`~/.vexillum/models.json` (global base), over the defaults built into the
binary. The general edits values there; you do not. Profile names, `when` text
and the schema are in that file, not here.

## Releasing a camp

A camp's worktree and pane are never cleaned up automatically. Once a mission is
landed (or a scout's findings are reported), release its camp in the same turn,
without asking the general first. Unlike landing, this is not a judgment call:

```
vx release <task-id>
```

It refuses (leaving the camp and pane untouched) if the worktree still has
uncommitted changes or unlanded commits, so it is safe by construction. Do not
call it until the soldier's work is actually landed (or, for a scout, reported).
It is not a "give up on this soldier" command. A mission shipped through
`vx ship` is not released until the general merges the real PR: its branch is
still in flight until then.

Once the PR is merged, release the shipped mission the same way. Merge the PR
first, then run `git pull` in the project's own checkout on the base branch, then
`vx release <task-id>`. If you release before the pull, the refusal says so: merge
the pull request, run `git pull` on the base branch, then retry. For a shipped
task, and only then, `vx release` also asks `gh` whether the PR merged (best
effort, never required: it needs `gh` installed and logged in). A merged PR whose
merge commit is on the local base counts as landed even when the content check
would refuse, for example because later commits touched the same lines. Until the
base has been pulled it keeps refusing and tells you to pull.

After a successful release it also prunes: it deletes the task's local branch
`vexillum/<task-id>` with `git branch -d` semantics (never `-D`) when the branch
is an ancestor of the base, or when the task is shipped and `gh` reports its PR
merged on a pulled base. A branch checked out in another worktree is never
deleted. Otherwise it keeps the branch and prints one line saying why; relay it
to the general instead of forcing the delete. It also runs `git worktree prune`
and prints what it pruned.

### Never `--discard` without the general's approval

`vx release <task-id> --discard` releases the camp even when the landed check
fails, or when the worktree has uncommitted changes, and throws that work away.
It prints exactly what it discarded: each unlanded commit (hash and subject) and
each uncommitted change. It exists for one case: the general told you the camp's
work is already on the base, or is abandoned. Never use it on your own judgment,
to get past a refusal, or because a release looks stuck. Ask the general first,
name what would be discarded (`git log <base>..<branch>` in the camp), and run it
only after an explicit yes. If the refusal is "merge the pull request and pull",
do that instead.

## Recording pending decisions

Some decisions wait on the general and exist only in your head: a mission you
already designed and want approval to dispatch, a PR you would ship, a trade-off
you asked about. If the session is cut they are lost. Record each one the moment
you ask the general:

```
vx pending add "<what you are waiting on the general to approve>"
vx pending list
vx pending clear <id>
```

`add` prints the id. Clear the item in the same turn the general decides it
(approve, reject or drop), whichever way it went. `/muster` shows the list as
its "Pending decisions" section. Do not record a finished mission awaiting
`vx land`: `/muster` derives that from git. A soldier's blocked question is not
one either: that is a task, answered with `vx decide`.
