# OpenCode commander verification checklist

Phase 4 of "vexillum for Pi and OpenCode": the commander running in OpenCode
2.x, woken by the sentinel from outside through OpenCode's HTTP API. This page
says what was checked and against what, then lists the steps for someone with a
working OpenCode and a real model to confirm the rest on a real machine.

## What was checked, and what was not

- **Delivery, run live on OpenCode 2.0.32.** In a throwaway directory with a
  throwaway `HOME` and `XDG_*` directories, its own service port (49778), a real
  herdr pane running the OpenCode TUI, a vexillum binary built from this branch
  and a real `vx sentinel` started with that environment. See "The live check of
  this branch" below for exactly what ran and what it showed.
- **Not checked: any model answering a wake.** During the live check every
  model request returned `403 OpenCode's free tier can only be used from within
  OpenCode` (see below), so no model read a wake, loaded the vexillum skill or
  ran a command. The check proves the wake reaches the session as a user turn,
  that the editor draft survives, and the hold/retry/fallback behavior. It
  proves nothing about what a model does with the wake.
- **Not checked: `HERDR_PANE_ID` inside a model-run bash tool.** The commander
  registers itself from `HERDR_PANE_ID` when it runs `vx dispatch` or
  `vx forum`. The live check registered it by running `vx forum` with the
  variable set to the pane id by hand.
- **Not checked: OpenCode 1.x.** `opencode api` does not exist there. The
  sentinel is expected to fall back to `herdr agent prompt` when the commander
  is idle, and `vx doctor` says so. Unit tests cover the fallback with fakes.
- **Not checked: a 2.x later than 2.0.32.** The two routes used,
  `GET /api/info` and `POST /api/session/<id>/prompt`, come from the generated
  client of the OpenCode v2.0.32 sources and were exercised on 2.0.32 only.
- **Not checked: OpenCode's fallback to `CLAUDE.md`** when there is no
  `AGENTS.md` (documented by OpenCode, not exercised), and the
  `OPENCODE_DISABLE_CLAUDE_CODE*` switches (documented, not exercised).
- **Not checked: long runs.** herdr restarting, the OpenCode service
  restarting while a wake is held, a sentinel running for hours, two commanders
  of different harnesses in one project.

Work through the sections in order. Each step says what to run, what to look
at and what a pass looks like. Write the result of every step in the table at
the end, and report any step that does not pass with the output the step
names.

## The live check of this branch

Environment: macOS, herdr 0.9.5, OpenCode 2.0.32, a vexillum binary built from
this branch, all under a throwaway `HOME` and `XDG_CONFIG_HOME`,
`XDG_DATA_HOME`, `XDG_STATE_HOME` and `XDG_CACHE_HOME`, with the OpenCode
service on port 49778 (`opencode service set port 49778`) and herdr's
OpenCode plugin files copied into the throwaway config so herdr could read the
state. The project was a throwaway git repository with `vx init --yes
--commander opencode`, an `opencode.json` with `"model":
"opencode/big-pickle"` and `"permission": {"*": "deny"}`, and nothing else.
The commander was `herdr agent start <name> --kind opencode --pane <pane>` in a
tab created with those environment variables (and closed afterwards). Wakes
were hand-made files (a forum wake and its inbox entry), delivered by a real
`vx sentinel`.

| Step | What ran | What it showed |
| --- | --- | --- |
| Init | `vx init --yes --commander opencode` in a repository with no rules file | `AGENTS.md` created with the block, no `CLAUDE.md`, no `.claude/settings.json`, skills in `.claude/skills/` |
| Registration | `HERDR_PANE_ID=<pane> vx forum <file> --no-open` | `commanders.json` got an `opencode` entry for the pane; the forum listener started ("listener: running") |
| Wake before any message | a forum wake written while the pane had `agent_session` empty | the sentinel logged once that the pane has no session yet; the wake file stayed; nothing was dropped |
| Delivery | the first message sent in the TUI, creating the session | on the next pass the sentinel logged "woke the opencode commander ... through the OpenCode API"; the wake file was gone; `opencode api GET /api/session/<id>/message` listed the wake as a `user` message right after the first one, and the TUI showed it as a user turn |
| Draft preserved | `DRAFTTEXT typed by the general, not sent` typed with no Enter, then a second wake written | the second wake was delivered as its own `user` message; the editor still held `DRAFTTEXT typed by the general, not sent` |
| Fallback | a sentinel started with a `PATH` that has `herdr` but no `opencode`, a fresh TUI that had no session, `DRAFT2 typed, unsent ` typed in its editor, and a wake | after about 30 seconds the sentinel logged "woke ... through herdr agent prompt"; the message that reached the session read `DRAFT2 typed, unsent Forum feedback is waiting for you:...`, which is the documented glue defect of the fallback |
| Doctor | `vx doctor` in the project, with the commander live | `[ok] OpenCode commander registered - pane ..., session ses_...`, `[ok] OpenCode wake API - the service (OpenCode 2.0.32) answers GET /api/info ...`, `[ok] OpenCode reads the vexillum block - AGENTS.md holds it` |
| Model | `opencode run -m opencode/<free model> --auto "Reply with only the word ok."` for four free models, and the TUI | every request failed: `403 OpenCode's free tier can only be used from within OpenCode`, with the service started from my shell and again with a clean environment; the session recorded each wake followed by a failed assistant turn |

Also worth knowing:

- herdr reported the pane `blocked` after the provider error and kept doing so,
  so the fallback (which needs `idle` or `done`) was never taken there; the API
  route does not look at the status.
- **Any `opencode` command starts the background service of the `HOME` it runs
  under**, even `opencode models`, `opencode api` and `opencode service start`.
  Run every command of a check under the throwaway environment, and finish with
  `opencode service stop` under it.

## 0. What you need

- macOS or Linux, herdr 0.9.x, git, OpenCode 2.x (`opencode --version` prints
  `opencode v2.`...) and a vexillum binary built from this branch
  (`go build -o /tmp/vx-oc ./cmd/vx`, used wherever this page says `vx`).
- A **model that works**. The free Zen models refused every request during the
  live check; connect a provider you can use (`opencode` then `/connect`), and
  use trivial prompts only. Note the model in the results.
- herdr's OpenCode integration: `herdr integration install opencode`, then
  `herdr integration status | grep '^opencode:'` should say current.
- Everything below in a throwaway repository, ideally with a throwaway `HOME`,
  `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME` and `XDG_CACHE_HOME`
  (the sentinel and the commander must share them), a service port of its own
  and herdr's OpenCode plugin files copied into the throwaway config:

  ```sh
  mkdir -p ~/ocmd-check && cd ~/ocmd-check
  git init -q -b main
  printf '# ocmd check\n' > README.md
  git add -A && git -c user.name=t -c user.email=t@example.com commit -qm init
  ```

## 1. Init and doctor

```sh
vx init --yes --commander opencode
git status --short
vx doctor
```

Pass: `AGENTS.md` holds the vexillum block, there is no `CLAUDE.md` and no
`.claude/settings.json`, `.claude/skills/{vexillum,forum,muster}` exist, and
`.vexillum/config.json` says `"commander": ["opencode"]`. Doctor requires
`opencode` and is ready without `claude`. `OpenCode commander registered`
says nobody has registered yet (an optional miss, not a failure).

## 2. Registration from the model's own shell

Start the commander in a herdr pane (`herdr agent start oc --kind opencode
--pane <pane>` after `herdr tab create`, or open OpenCode in a pane herdr
already tracks as an `opencode` agent), in the project directory. Ask the
model to run `echo $HERDR_PANE_ID` and tell you the output, then to run a
command that registers a commander, such as `vx forum <html-file> --no-open` or
`vx dispatch ... --kind scout`.

Pass: its bash tool printed a pane id (this is the unverified claim), and
`projects/<repo>-<hash>/commanders.json` under `~/.vexillum` has an `opencode`
entry with that pane. `vx doctor` now says `OpenCode commander registered -
pane <pane>, session ses_...`.

## 3. The wake lands as a user turn

With a sentinel running (`vx dispatch` started one; check `pgrep -f 'vx
sentinel$'` and that it is the binary you built), dispatch a scout with a
trivial prompt, or write a wake by hand (see below). Pass: when it finishes,
within a few seconds the commander's session gets a user message beginning
`A vexillum soldier's status changed:`, the sentinel log says it woke the
commander through the OpenCode API, and the model reacts (it checks on the
soldier and reports). To see the message without the TUI:

```sh
herdr agent get <pane>                 # agent_session.value is the session id
opencode api GET /api/session/<session-id>/message
```

A wake by hand, for a forum wake: write
`forum-inbox/<session>/<uid>.json` and `wakes/forum/<session>.json` in the
project's directory under `~/.vexillum/projects/` (the shapes are in
`internal/inbox` and `internal/sentinel/forumwake.go`).

## 4. The draft survives

Type a message in the commander's editor and do **not** press Enter. Trigger a
wake as in section 3. Pass: the wake appears in the transcript as its own user
turn and the editor still holds what you typed, untouched.

## 5. A wake before the first message

In a fresh OpenCode pane in the project (nothing sent yet), register it as in
section 2 (with the variable set by hand if you only want this step) and
trigger a wake. Pass: the sentinel logs that the pane has no OpenCode session
yet, the wake file stays on disk, and once you send the first message the wake
is delivered on the next pass. (If the pane stays idle with no session for 30
seconds the sentinel types the wake with `herdr agent prompt`: that is the
fallback, section 7.)

## 6. A busy commander

While the commander is working on a long answer, trigger a wake. Pass: the
wake is delivered at once but waits behind the running turn (`"delivery":
"queue"`), and the model sees it right after, in order.

## 7. The fallback

Start the sentinel with a `PATH` that has `herdr` and `git` but no `opencode`
(stop yours first: one sentinel runs per machine, and verify the pid is yours
before `kill`). With the commander idle and an unsent draft in its editor,
trigger a wake. Pass: the sentinel logs the API problem once, and after 30
seconds types the wake with `herdr agent prompt`, and the draft is glued to it
(the documented cost). With the commander `working` or `blocked` the sentinel
types nothing; it waits.

## 8. Wrong environment

Start the sentinel with a different `HOME` than the commander's OpenCode. Pass:
the wake is not delivered and not dropped, and the log names the problem with
the hint about sharing `HOME` and the XDG directories. `vx doctor` run from
the commander's shell says the service answers; run from the other `HOME` it
says the service is not running (and never starts one: check with
`opencode service status`).

## 9. Forum feedback

From the commander, `vx forum <html-file>`, then send feedback from the
browser. Pass: the sentinel delivers `Forum feedback is waiting for you:` to
the session, the model runs `vx forum inbox`, answers with `vx forum reply`
and confirms with `--ack`, and a second wake for the same, already confirmed,
message is not delivered.

## 10. The block is not hidden

In a throwaway project: put the block in `CLAUDE.md` (`vx init --yes` with no
`AGENTS.md`), then add an `AGENTS.md` with other text. Pass: `vx doctor` warns
`OpenCode reads the vexillum block`, and `vx init --commander opencode` warns
and changes nothing. Ask the OpenCode commander to quote the first heading of
its rules: with both files it should be the one in `AGENTS.md`. Then delete
the block from `CLAUDE.md`, run `vx init --commander opencode`, and the block
is in `AGENTS.md` and the commander quotes `## Vexillum commander`.

## 11. Service and herdr restarts

With a wake held (no session, or a stopped service), run `opencode service
restart`, and, separately, restart herdr (pane ids change). Pass: the wake is
delivered after the service is back, and after herdr's restart the
commander is found again by directory (`vx doctor` shows "found again by
directory") or, when it is not, the wake stays queued. Report what you see.

## 12. OpenCode 1.x

With a 1.x OpenCode as the commander: `vx doctor` shows `OpenCode wake API`
as a warning that names the V1 limit. Trigger a wake: it is delivered by
`herdr agent prompt` after 30 seconds, with the draft caveat, when the
commander is idle. Report anything else.

## Results

| Step | Pass / fail / not run | Notes (OpenCode version, model, output of a failing step) |
| --- | --- | --- |
| 1 Init and doctor | | |
| 2 Registration from the model's shell | | |
| 3 Wake lands as a user turn | | |
| 4 Draft survives | | |
| 5 Wake before the first message | | |
| 6 Busy commander | | |
| 7 Fallback | | |
| 8 Wrong environment | | |
| 9 Forum feedback | | |
| 10 Block not hidden | | |
| 11 Service and herdr restarts | | |
| 12 OpenCode 1.x | | |

When you are done, stop what you started: your sentinel (by pid, after
checking it is the binary you built), `vx forum stop`, `opencode service stop`
under the throwaway environment, and close the herdr tabs you created.
