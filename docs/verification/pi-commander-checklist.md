# Pi commander verification checklist

Phase 4 of "vexillum for Pi and OpenCode": the commander, the agent you talk
to, running in Pi. The code was written without access to a real Pi model.
This checklist is for someone with a working Pi and a model to confirm on a
real machine what cannot be confirmed without one.

## What is already shown, and what is not

Shown by the repository's own tests, against the installed Pi 1.3.0 in a
pseudo terminal with a scripted model standing in for a provider
(`TestExtensionInRealPi` in `internal/piwake`), with the real `vx init` and the
real `vx sentinel await` in a temporary `HOME`:

- `vx init --commander pi` writes `.pi/extensions/vexillum-wake.ts` and
  `.pi/settings.json`; Pi asks "Trust project folder?" and does not load the
  extension until it is answered; what Pi saves in `trust.json` is what
  `vx doctor` reads.
- The extension starts `vx sentinel await` as a direct child of the Pi process
  and registers it under `~/.vexillum/sentinel-awaiters/`.
- A wake reaches an idle model as a user message, with no key pressed; a wake in
  the middle of a turn is held until the turn ends; text the general has typed
  and not sent is still in the editor afterwards and goes out as its own
  message; quitting ends the await.
- In a soldier's camp (a worktree under `~/.vexillum`, Pi started with
  `--approve` as soldiers are), the extension starts nothing and a wake for the
  project is not delivered there.

Also covered, against a fake Pi API under node (`TestExtensionUnderNode`): a
wake Pi cannot take is kept and retried, never dropped; wakes held when the
session is replaced go to the next session; an await that returns at once is
retried a few times and then left alone; one that ran for a while is restarted
at once; a failing await is retried with a growing pause and reported once.

**Not shown, and what this checklist is for:**

- That a real model obeys the wake text, loads the `vexillum` skill when it
  should and keeps to the commander's rule of never doing a soldier's work. A
  scripted model proves the text arrives, not what a model does with it.
- That `vx dispatch`, run from the commander's own shell tool, sees the herdr
  pane variables (`HERDR_WORKSPACE_ID`, `HERDR_PANE_ID`).
- Behavior of weaker or free models as commanders (tool-calling reliability).
- The 55 minute await cycle over a long session, herdr restarts, and `/new`,
  `/resume`, `/fork` or `/reload` while a wake is held.
- Pi with a `defaultProjectTrust` other than the default `ask`.
- Two commanders (Claude Code and Pi) in one project.

Write the result of every step in the table at the end and report any step
that does not pass with the output the step names.

## 0. What you need

- macOS or Linux, herdr 0.9.x, git.
- Pi 1.3.x logged in to a provider (`pi --version` prints `1.3.` and a patch
  number), and `herdr integration install pi` done.
- A model you can afford to run several small turns on, ideally the one you
  would really use as a commander, and, for step 9, a weaker one. Pick them
  from `pi --list-models`. The commander sends file contents to the provider,
  so use a throwaway repository.
- A vexillum binary built from this branch, in a directory of its own put first
  on `PATH`, so that both you and the extension find it as plain `vx`:
  `go build -o /tmp/vx-pc/vx ./cmd/vx && export PATH=/tmp/vx-pc:$PATH`. Start
  `pi` from a shell that has that `PATH`.
- Everything below runs inside a herdr pane, in a throwaway repository:

  ```sh
  mkdir -p ~/pi-commander-check && cd ~/pi-commander-check
  git init -q -b main
  printf '# pi commander check\n' > README.md
  git add -A && git -c user.name=t -c user.email=t@example.com commit -qm init
  ```

Note `~/.pi/agent/trust.json` and `~/.pi/agent/settings.json` before you
start (`cat` them, or write that they are absent): steps 1 and 12 compare them.

## 1. Init, and Pi's trust question

```sh
vx init --yes --commander pi
git status --short
vx doctor | grep -E 'Pi|sentinel'
```

Pass:

- `init` says it installed `.pi/extensions/vexillum-wake.ts`, added
  `../.claude/skills` to `"skills"` of `.pi/settings.json`, and prints
  `Pi will ask "Trust project folder?" the first time you run it here`.
  There is no `CLAUDE.md` import and no `.claude/settings.json`.
- `git status` shows `.pi/` among the new files.
- `doctor` prints `[ok] Pi wake extension - installed, current`,
  `[ok] Pi skills reference`, `[warn] Pi project trust - not trusted yet: Pi
  will ask "Trust project folder?" once ...` and `[ok] Pi wake finds vx`.
  It exits 0.

Commit them: `git add -A && git -c user.name=t -c user.email=t@example.com commit -qm "vexillum for pi"`.

Then run `pi` in the folder. Pass: Pi shows "Trust project folder?" before
anything else. Choose **Trust**. Pi starts and lists the skills `vexillum`,
`forum` and `muster` (`/skill:` completes them). `vx doctor` now prints
`[ok] Pi project trust - trusted (<folder> in ~/.pi/agent/trust.json)`, and
`~/.pi/agent/trust.json` has an entry for the folder.

Also: `ls ~/.vexillum/sentinel-awaiters/` shows one record whose `owner_pid` is
Pi's pid (`pgrep -x pi`, or `ps -o ppid= -p <await pid>`).

## 2. The commander follows the vexillum block (real model)

In that Pi session, with the real model, say:

> Dispatch a scout to find out which package manager this repository would use
> and why, three lines at most.

Pass, all of:

1. The model loads the `vexillum` skill first (you see it read the skill's
   `SKILL.md`, or call it by name) and does not start investigating itself.
2. It runs `vx dispatch ... --kind scout`, and a new herdr tab with a soldier
   appears. This is also the check that the tool shell of a Pi commander has
   the herdr environment: if dispatch fails with a message about
   `HERDR_WORKSPACE_ID`, report it with `env | grep HERDR` from the same Pi
   shell tool (ask the model to run it).
3. It tells you it dispatched and stops; it does not poll.

## 3. The wake, with nothing typed

Do not touch the keyboard while the scout works. Pass: when the scout finishes,
Pi starts a turn by itself, the user message it shows begins
`A vexillum soldier's status changed:`, and the model checks the soldier
(`vx status`, reads the report) and reports to you. Write down how long after
the scout finished this took (the await polls every 5 seconds). If the model
ignores the message, report the model and what it did.

## 4. An unsent draft survives a wake

Dispatch another scout (`vx dispatch "list the files in this repository" --kind
scout`). While it works, type `my draft` in Pi's editor and do not press Enter.
Pass: when the wake arrives and the model answers, `my draft` is still in the
editor, intact, and sending it afterwards reaches the model as exactly
`my draft`.

## 5. A wake during a turn

Ask the commander for something that takes it a minute (for example "read every
file in this repository and summarize each one in a paragraph"), and, in a
second pane, dispatch a scout from the same project that finishes quickly. Pass:
the wake does not interrupt the turn; it appears as the next user message when
the turn ends, and the model then deals with it.

## 6. The commander does not do a soldier's work

Say: "Fix the typo in README.md." Pass: the model dispatches a mission to do it
(`vx dispatch ... --kind mission`) instead of editing the file itself. Report
the model if it edits the file, since that is the block not being obeyed.
Strike the mission afterwards (`vx strike <task-id>`).

## 7. Nothing happens in a soldier's pane

Dispatch a scout on a Pi profile (see `docs/verification/pi-soldier-checklist.md`
section 2 for adding one). Pass: in the soldier's tab Pi starts without the
trust dialog (it is launched with `--approve`) and without any notice from the
wake; `ls ~/.vexillum/sentinel-awaiters/` still lists only the commander's await
(`ps ax | grep 'sentinel await'` shows one process per commander Pi); and the
soldier is not given messages about other soldiers.

## 8. The await cycle over a long session

Leave the commander Pi open and idle for about 60 minutes (a coffee, not a
test of patience). Pass: `~/.vexillum/sentinel-awaiters/` still has exactly one
record after the hour, with a different `pid` and a later `started_at` than
before: the 55 minute timeout ended the await and the extension started the
next. Then dispatch a scout and confirm the wake of step 3 still arrives.

## 9. A weaker model as commander

Repeat steps 2, 3 and 6 with the weaker model (`/model` in Pi). Write down what
it does: whether it loads the skill, dispatches, reads the wake message and
acts on it, or does the work itself. This is not a pass/fail step; it tells the
general which models are usable as commanders.

## 10. Replacing the session while a wake is pending

Dispatch a scout, then immediately type `/new` in Pi. Pass: the new session, not
the old one, receives the wake when the scout finishes (the old session is gone
and nothing is lost). Repeat with `/reload`. Report if a wake arrives twice, or
never.

## 11. Upgrade and edits

```sh
printf '\n// my change\n' >> .pi/extensions/vexillum-wake.ts
vx doctor | grep 'Pi wake extension'
vx upgrade
vx upgrade --force
ls .vexillum/backups/pi/
```

Pass: doctor says `edited by hand`; `vx upgrade` keeps the file and says it is
edited; `vx upgrade --force` replaces it, saves the edited copy as
`.vexillum/backups/pi/vexillum-wake.ts` and says so; doctor then says
`current`. Then, to see the stale case, restore an older copy if you have one
(`git log -p .pi/extensions/vexillum-wake.ts` after a later vexillum release)
and expect `stale` and a plain `vx upgrade` refreshing it.

## 12. Other `defaultProjectTrust` values

Only if you are willing to edit `~/.pi/agent/settings.json` (copy it first).
Remove the folder's entry from `~/.pi/agent/trust.json`, set
`"defaultProjectTrust": "never"`, and run `vx doctor`: pass is a warning that
the setting is `never` and Pi skips the extension. Run `pi` there: no dialog,
and no await in `~/.vexillum/sentinel-awaiters/`. Then set it to `"always"`:
doctor reports `[ok]`, and Pi loads the extension without asking. Restore your
settings and `trust.json` afterwards.

## 13. Two commanders in one project

`vx init --yes --commander claude --commander pi`, then run Claude Code in one
pane and Pi in another, in the same project. Dispatch a scout from either.
Report which commander is woken: a wake is delivered once, to whichever await
drains it first, so only one of them is expected to be, and the other has to be
told by hand. This step records the behavior; it is not expected to pass.

## 14. Results

| Step | Pass / fail / skipped | Notes (versions, model, anything odd) |
| --- | --- | --- |
| 1 init, trust dialog, doctor lines | | |
| 2 skill loaded, dispatch from the Pi shell tool | | |
| 3 wake with no input | | |
| 4 unsent draft kept | | |
| 5 wake during a turn | | |
| 6 commander does not do the soldier's work | | |
| 7 nothing in a soldier's pane | | |
| 8 await cycle after an hour | | |
| 9 weaker model | | |
| 10 `/new` and `/reload` with a wake pending | | |
| 11 upgrade, edit, `--force` | | |
| 12 `defaultProjectTrust` never and always | | |
| 13 two commanders | | |
