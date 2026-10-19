# OpenCode soldier verification checklist

Phase 3 of "vexillum for Pi and OpenCode": soldiers on OpenCode, in both
generations that share the `opencode` command: V1 (1.x, from 1.18.29) and V2
(2.x). This page says what was checked and against what, then lists the steps
for someone with a working OpenCode to confirm the rest on a real machine.

## What was checked, and what was not

- **V2 (2.0.32), run live.** The launch recipe (agent file, `opencode.json`,
  `--auto`), a scout and a mission end to end, `vx prompt`, `vx decide` on a
  question form (by option number and by typed text) and on a permission
  prompt (Allow once, Reject), landing and striking, redispatch with the files
  re-provisioned, the collision failure, the doctor lines, and the one-shot
  command used by `vx ship`. All of it ran in a throwaway directory with a
  throwaway `HOME` and XDG directories, on the free zero-retention Zen models
  only, with trivial prompts. The captured `opencode run --format json` output
  of a success and of an error is in `internal/soldier/testdata/`.
- **V1 (1.x): never run.** No V1 was available. The launch comes from the
  v1.18.29 and v1.19.3 sources and docs (the TUI takes `--model`, `--agent`,
  `--auto` and `--prompt`, and has no variant option; an agent's `variant`
  applies only together with its own `model`). Tests cover it against a fake
  `opencode` that reports 1.x. **Every V1 step below is unverified.**
- **Also unverified, on either generation:** what `OPENCODE_DISABLE_AUTOUPDATE=1`
  actually prevents (the variable is present in the TUI process; no update was
  available to observe), the "what's new" dialog after an upgrade, how several
  soldiers behave on the shared V2 background service when it restarts, what
  "Always allow" persists (vexillum never chooses it), and a V1 variant set
  through `OPENCODE_CONFIG_CONTENT` together with `--model`.
- **One stray session.** The first live soldier of the V2 spike ran under the
  real `HOME`, so it created one session in the maintainer's real OpenCode
  database, titled with the start of its prompt ("Create a file named
  hello.txt in the repository root..."). It sent only that trivial prompt to a
  free zero-retention model. It was not deleted.

Work through the sections in order. Each step says what to run, what to look
at, and what a pass looks like. Write the result of every step in the table at
the end, and report any step that does not pass with the output the step
names.

## 0. What you need

- macOS or Linux, herdr 0.9.x, git, and Claude Code (the commander is still
  Claude Code, and `vx doctor` still requires it).
- OpenCode. For V2 steps, 2.x (`opencode --version` prints `2.`...). For V1
  steps, 1.18.29 or later in the 1.x series, such as 1.19.3. Run `opencode`
  once and connect a provider; the free Zen models need no key.
- A vexillum binary built from this branch. From the repository root:
  `go build -o /tmp/vx-oc ./cmd/vx`, then use `/tmp/vx-oc` wherever this page
  says `vx`.
- A free **zero-retention** model, so the check sends nothing worth keeping to
  a provider that logs it: `opencode/glm-5.3-flash-free`,
  `opencode/deepseek-v4.1-flash-free`, `opencode/longcat-2.5-preview-free` or
  `opencode/space-bunny-free`. Confirm in `opencode models` that the one you
  pick is listed, and write it down as `<provider>/<model>`. Any other free
  model logs prompts. Use trivial prompts only.
- Everything below runs inside a herdr pane, in a throwaway repository:

  ```sh
  mkdir -p ~/oc-check && cd ~/oc-check
  git init -q -b main
  printf '# opencode check\n' > README.md
  git add -A && git -c user.name=t -c user.email=t@example.com commit -qm init
  ```

To keep your real OpenCode state out of the check, you can run everything
under a throwaway `HOME` and `XDG_CONFIG_HOME`, `XDG_DATA_HOME`,
`XDG_STATE_HOME` and `XDG_CACHE_HOME`, and give that OpenCode a different
service port in its config than the default, so it does not meet your own
running service.

## 1. Install the herdr integration and run doctor

```sh
herdr integration install opencode
herdr integration status | grep '^opencode:'
vx init --yes
vx doctor
```

Pass:

- `herdr integration status` prints an `opencode:` line that is not
  `not installed` and not `outdated`.
- `vx doctor` prints `[ok] OpenCode`, `[ok] OpenCode version - <version> (V1)`
  or `(V2)`, and `[ok] herdr integration for opencode - ...`, and ends with
  `Environment ready`.

The other directions:

```sh
herdr integration uninstall opencode
vx doctor | grep 'integration for opencode'
```

Pass: `[warn] herdr integration for opencode - not installed, so herdr reads an
OpenCode soldier's state off the screen; run 'herdr integration install
opencode'`, and doctor still exits 0. Reinstall it before going on.

A shadowing install, if you have both generations (V1 from one package
manager, V2 from another): put the 1.x directory first in `PATH` and run
`vx doctor`.

Pass: `[warn] OpenCode V1 shadowing V2 in PATH - ...` naming both paths and
versions. With the 2.x directory first, the line is gone.

With OpenCode off the path and no opencode profile, a setup without OpenCode
must show no OpenCode line.

## 2. Add OpenCode profiles and validation

Replace `<provider>/<model>` with the free model you picked. Write
`.vexillum/models.json`:

```json
{
  "profiles": {
    "oc-scout": {
      "model": "<provider>/<model>",
      "effort": "low",
      "when": "Scout on OpenCode.",
      "harness": "opencode"
    },
    "oc-mission": {
      "model": "<provider>/<model>",
      "effort": "medium",
      "when": "Mission on OpenCode.",
      "harness": "opencode"
    }
  }
}
```

```sh
git add -A && git -c user.name=t -c user.email=t@example.com commit -qm "vexillum scaffold and opencode profiles"
vx models
```

Pass: `vx models` lists both profiles with `harness opencode`. The commit
matters: `vx land` refuses a dirty checkout.

Validation, which needs no model and creates nothing:

```sh
vx dispatch "x" --profile oc-scout --model sonnet
vx dispatch "x" --profile oc-scout --effort 'not a label'
```

Pass: the first prints an error containing `must be provider/model`, the
second one containing `must be a plain label`. Neither creates a tab or a task
(`vx status` is unchanged).

## 3. V2: a scout

Use a 2.x `opencode` first on `PATH`.

```sh
vx dispatch "Find out which package manager this repository would use and why. Keep it to three lines." --kind scout --profile oc-scout
```

Look at the new herdr tab, and in the camp (`camp_path` in `vx status --json`):

| Observe | Pass |
| --- | --- |
| The tab's process | `opencode --auto`, with no `--model`. `ps ax -o args | grep 'opencode --auto'`. |
| Camp files | `.opencode/agents/vx-soldier.md` exists with `model: "<provider>/<model>"`, and `opencode.json` has `default_agent` and `model`. |
| Excluded | `git -C <camp> status --short` is empty, and `git -C <camp> rev-parse --git-path info/exclude` names a file that lists `/.opencode/` and `/opencode.json`. |
| Environment | `OPENCODE_DISABLE_AUTOUPDATE=1` is set for the opencode process (`ps eww -p <pid>` on macOS, `/proc/<pid>/environ` on Linux). |
| The model | The pane's footer shows the model you configured, not the default. |
| No dialog | No dialog stops the start, and the prompt reaches the editor without anyone pressing a key. |
| The prompt | The pane shows the dispatched prompt, ending with the instructions to write a report under `~/.vexillum/projects/<id>/reports/`. |
| herdr state | While OpenCode works, `herdr agent get <agent-name>` shows `working`; when it finishes, `idle`. |
| vx status | `running` while it works and `done` after (the sentinel polls every 5 seconds). |
| The report | The report file exists, and `vx status --json` shows `"harness": "opencode"`, `"harness_major": 2`, `"harness_version"`, the model and `"effort": "low"`. |

Then `vx strike <task-id>`. Pass: it succeeds and the tab closes.

A slow start: if the first dispatch after a cold start prints
`agent_not_ready` in herdr's own output but the soldier comes up anyway, that
is the polling working. Report it if `vx dispatch` gave up on a soldier that
was starting.

## 4. V2: a mission, land and strike

```sh
vx dispatch "Create hello.txt containing the single word hi, and commit it with the message 'add hello'." --kind mission --profile oc-mission
```

Pass:

- The tab behaves as in step 3.
- `vx status` shows `done`, and in the camp `git log --oneline` shows
  `add hello` ahead of `main`, and `git status` is clean (the provisioned files
  are not part of the commit).
- `vx land <task-id>` prints `landed:` and `struck:`, `hello.txt` exists in
  `~/oc-check`, `git log -1 --format=%s` there is `add hello`, and
  `~/oc-check/opencode.json` does **not** exist (the files were only in the
  camp). The tab is closed.

## 5. V2: collision with a project's own configuration

```sh
printf '{}\n' > ~/oc-check/opencode.json
git -C ~/oc-check add opencode.json
git -C ~/oc-check -c user.name=t -c user.email=t@example.com commit -qm "project opencode config"
vx dispatch "Say hello." --kind scout --profile oc-scout
```

Pass: dispatch prints `status=failed` and an error naming `opencode.json`,
"is tracked by the project" and "never overwrites or merges", exits non-zero,
and starts no tab. `vx status` shows the task `failed`, and `vx strike
<task-id>` removes it. The project's file is unchanged. Repeat with
`opencode.jsonc`, and with a tracked `.opencode/agents/vx-soldier.md`: same
result. A tracked `.opencode/commands/x.md` is **not** a collision.

Then `git -C ~/oc-check rm -q opencode.json && git -C ~/oc-check -c user.name=t -c user.email=t@example.com commit -qm "drop it"`.

## 6. V2: a blocked question form

```sh
vx dispatch "Before you do anything else, ask me which color to use with your question tool and wait for my answer. Then write the answer into color.txt and commit it." --kind mission --profile oc-mission
```

Whether a free model uses the question tool is up to the model; if it only
writes a `needs-decision:` line, that is the plain-text path of step 7.
Pass, when a form appears:

- `vx status` shows the task `blocked`, and `vx status --json` has a `decision`
  with the question and the option texts.
- `vx decide <task-id> 2` selects the second option: the pane shows the form
  submitted with option 2, and the soldier carries on.
- On a second blocked task, `vx decide <task-id> "teal"` (text that is not an
  option) goes into the "Type your own answer" field and is submitted; the
  soldier uses `teal`.
- `vx decide <task-id> 9` when there are fewer than nine options is refused
  with nothing typed.
- A non-ASCII answer is refused with nothing typed.
- After the answer, `vx status` returns to `running` or `done` within a few
  seconds, not stuck on `blocked`.

## 7. Blocked on plain text, dismiss, and a permission prompt

Plain text: when a soldier ends its turn with `needs-decision: <question>`,
`vx status` shows `blocked` and `vx decide <task-id> "blue"` types `blue` into
the editor and submits it. Pass: the soldier receives `blue`.

Dismiss: `vx decide <task-id> --dismiss` on a task that was marked blocked by
mistake (dispatch a scout, wait for `blocked` if it asks, dismiss). Pass: the
task becomes `running` or `done` and `vx status --json` shows the decision
`dismissed`.

Permission prompt. With `--auto` it is rare. To provoke one, add a rule with
`"ask"` for an action to a throwaway `opencode.json` in a different directory
that OpenCode loads globally (do not edit your real config), then dispatch a
task that needs that action. When the prompt appears:

- `vx status` shows the task `blocked`, with options `Allow once` and
  `Reject`.
- `vx decide <task-id> "allow once"` presses Enter and the action runs.
- On another task, `vx decide <task-id> reject` presses Esc and the soldier
  sees "The user declined this tool call".
- `vx decide <task-id> "always allow"` is **refused**, and nothing is typed.

Skip this part if you cannot provoke a prompt, and say so in the table.

## 8. V2: variants

1. Find a model and variant that exist: `opencode models --verbose` may show a
   model's variants. Make a profile with `"model": "<provider>/<model>#<variant>"`
   and dispatch a one-line scout.

   Pass: the agent file has `model: "<provider>/<model>#<variant>"`, the
   `opencode.json` `model` has **no** variant, and the pane footer shows the
   variant (for example `· high`).
2. Make a profile whose variant the model does **not** have, such as
   `<provider>/<model>#nope`.

   Pass, and this is OpenCode's behavior: the soldier starts and the footer
   shows **no** variant (the 2.x TUI drops an unknown variant silently).
   Report it if instead the start fails.
3. The one-shot is stricter. In the throwaway repository:

   ```sh
   opencode run --auto --format json --model "<provider>/<model>#nope" -- "say ping" < /dev/null; echo "exit=$?"
   ```

   Pass: exit 1 and a JSON line `{"type":"error",...}` on stdout whose message
   is `Variant unavailable for <provider>/<model>: nope`.
4. A profile with `"effort": "high"` and no `#variant` must run with **no**
   variant: effort is recorded, never passed.

## 9. V2: prompt, redispatch and the version guard

On a finished (`done`) task whose camp is still leased:

```sh
vx prompt <task-id> "Also append a second line 'bye' to hello.txt and commit it."
```

Pass: the text appears in the pane as typed input, the task goes back to
`running` and then `done`, and `vx status --json` has an `amendments` entry.

Redispatch:

1. Dispatch a new mission on `oc-mission`.
2. Close its herdr tab by hand while it is working.
3. Wait for `vx status` to show it `interrupted` (allow a few minutes).
4. `vx redispatch <task-id>`.

Pass: it prints `redispatching task ...`, a fresh tab appears, the camp has
`.opencode/agents/vx-soldier.md` and `opencode.json` again (re-provisioned),
`vx status --json` has `"harness": "opencode"`, `"harness_major": 2` and
`"redispatches": 1`. `ps` shows `opencode --auto` again.

Version guard: with a task `interrupted` on 2.x, put a 1.x `opencode` first in
`PATH` and run `vx redispatch <task-id>`. Pass: it refuses with `was dispatched
on opencode 2.x but opencode 1.x ... is installed now` **before** discarding
anything: the task is still `interrupted` and its camp is untouched.

## 10. The tribunal on an OpenCode mission (optional, needs `gh` and a remote)

Skip this step if the repository has no GitHub remote.

```sh
vx ship <task-id>
```

While the review runs, `ps ax -o args | grep 'opencode run'` shows the
reviewer: `opencode run --auto --format json --model <provider>/<model> --
<the review brief>`.

Pass:

- The reviewer is `opencode`, not `claude`, on the mission's own model, and its
  answer is parsed (findings or "no findings", not a parse error).
- A failure is surfaced: `opencode run --auto --format json --model
  bogus/none -- hi < /dev/null; echo "exit=$?"` exits non-zero, and `vx ship`
  treats such a run as a failed review step, naming the cause from the error
  line.
- The run does not hang waiting for input (its stdin is the null device).

## 11. V1 (unverified): every step

Do this section with a 1.x `opencode` (1.18.29 or later) first in `PATH`, and
a profile with a model you can use on that install. **None of this has been run.**
Report the exact text of anything that differs.

1. `opencode --version` prints `1.`... and `vx doctor` shows
   `[ok] OpenCode version - ... (V1)` and the integration line.
2. `vx dispatch "Find out which package manager this repository would use." --kind scout --profile oc-scout`.
   - `vx status --json` has `"harness_major": 1`.
   - The process is `opencode --auto --model <provider>/<model>`
     (`ps ax -o args`). **Check that the 1.x TUI accepts `--auto`**; if it
     exits with an error about the flag, report the text.
   - The camp has **no** `.opencode/agents/vx-soldier.md` and no
     `opencode.json` from vexillum.
   - The footer shows the model you configured.
   - No dialog stops the start (including a "what's new" dialog: if one
     appears, that is the case the guide's "run the TUI once after each
     upgrade" step covers; report what it looked like).
   - The report is written, the task goes `done`, and `vx strike` closes it.
3. A variant on V1. Make a profile `"model": "<provider>/<model>#<variant>"`
   for a variant the model has.
   - The pane's environment holds `OPENCODE_CONFIG_CONTENT` with
     `{"agent":{"build":{"model":"<provider>/<model>","variant":"<variant>"}}}`
     (`ps eww -p <pid>`).
   - **The footer shows the variant.** This is the least certain V1 point:
     whether an agent's variant is applied when `--model` also names the
     model. Report what the footer shows. If it shows no variant, V1 variants
     do not work through this recipe and need another mechanism.
4. A mission, `vx land` and `vx strike`, as in step 4. Pass: same results, and
   no vexillum file is left in the project.
5. A collision does **not** apply on V1 (vexillum writes nothing): with a
   tracked `opencode.json`, a scout dispatches normally and the project's file
   is untouched.
6. A blocked question. V1's question form and permission prompt were never
   observed, so `vx decide` refuses a screen it cannot classify and types
   nothing. Pass: when a V1 soldier is blocked on a screen, `vx decide` reports
   that it does not know the screen, nothing is typed into the pane, and you
   can answer by attaching to the pane. A `needs-decision:` line is answered
   as plain text. Report the screen (`herdr agent read <agent-name>`) so it can
   be taught.
7. `vx prompt` and `vx redispatch`: as in step 9. Redispatch relaunches on V1
   (`"harness_major": 1` is kept).
8. The tribunal, if you have a remote: `ps` shows `opencode run --auto
   --format json --model <provider>/<model> [--variant <variant>] -- ...`. Pass: the 1.x
   `run` accepts those flags, and its JSON output (`--format json`) parses.
   Report the output of `opencode run --auto --format json --model
   <provider>/<model> -- "say ping" < /dev/null` if the review fails to parse.
9. herdr's detection: while a V1 soldier works, `herdr agent get <agent-name>`
   shows `working`, then `idle` (the integration for opencode must be
   installed and current).

## 12. Things only time can show

Not steps to pass or fail, but to watch for during a real use, and to report:

- **Auto-update.** Run a soldier across an OpenCode release if you can. With
  `OPENCODE_DISABLE_AUTOUPDATE=1` the soldier should not exit or restart the
  shared service mid-task.
- **What's new.** After you upgrade OpenCode, dispatch before running the TUI
  once. Report whether the soldier stops on a dialog.
- **Several soldiers on V2.** Dispatch three scouts at once. They share one
  background service. Report whether any fails to start, and what happens to
  the others if the service is restarted (`opencode service stop`).
- **Always allow.** Whether "Always allow" saves a rule somewhere is not
  something vexillum tests: it never chooses it.

## 13. Results

| Step | Pass / fail / skipped | Notes (versions, model, anything odd) |
| --- | --- | --- |
| 1 doctor and integration | | |
| 2 profiles and validation | | |
| 3 V2 scout: process, files, exclude, env, states, report | | |
| 4 V2 mission: commit, land, strike | | |
| 5 V2 collision | | |
| 6 V2 question form: number, typed text, refusals | | |
| 7 plain text, dismiss, permission prompt | | |
| 8 V2 variants | | |
| 9 prompt, redispatch, version guard | | |
| 10 tribunal | | |
| 11 V1: every sub-step (1 to 9) | | |
| 12 auto-update, what's new, several soldiers | | |

When you report back, include `vx doctor`, `opencode --version`,
`herdr --version`, the OS, and for any failed step the exact command, what you
saw, and (for pane problems) the output of `herdr agent read <agent-name>`.
