# Pi soldier verification checklist

Phase 1 of "vexillum for Pi and OpenCode": soldiers on Pi. The code and its
tests were written without access to a real Pi model, so every behavior below
that depends on Pi itself (its TUI, its trust dialog, its thinking levels, how
herdr sees it) was derived from the docs and code that ship in Pi 1.3.0 and
from running Pi without a model, and tested against a fake `pi` on `PATH`.
This checklist is for someone with a working Pi to confirm those behaviors on
a real machine.

Work through the sections in order. Each step says what to run, what to look
at, and what a pass looks like. Write down the result of every step in the
table at the end, and report any step that does not pass with the output the
step names.

## 0. What you need

- macOS or Linux, herdr 0.9.x, git, and Claude Code (the commander is still
  Claude Code, and `vx doctor` still requires it).
- Pi 1.3.x, logged in to a provider (`/login` inside `pi`, or an API key in
  the environment). Check with `pi --version`, which prints `1.3.` followed
  by a patch number.
- A vexillum binary built from this branch. From the repository root:
  `go build -o /tmp/vx-pi ./cmd/vx`, then use `/tmp/vx-pi` wherever this page
  says `vx`.
- A model you can afford to run a few small tasks on. Pick one from
  `pi --list-models` and write it down as `<provider>/<id>`. Do not use a free
  tier for this check unless the repository below is disposable, because the
  soldier sends file contents to the provider.
- Everything below runs inside a herdr pane, in a throwaway repository:

  ```sh
  mkdir -p ~/pi-check && cd ~/pi-check
  git init -q -b main
  printf '# pi check\n' > README.md
  git add -A && git -c user.name=t -c user.email=t@example.com commit -qm init
  ```

Pi keeps its own state under `~/.pi/agent`. Nothing here touches it except the
steps that say so, and step 8 asks you to compare `~/.pi/agent/trust.json`
before and after.

## 1. Install the herdr integration and run doctor

```sh
herdr integration install pi
herdr integration status | grep '^pi:'
vx init --yes
vx doctor
```

Pass:

- `herdr integration status` prints a `pi:` line that is not `not installed`
  and not `outdated`.
- `vx doctor` prints `[ok] Pi`, `[ok] Pi version - 1.3.<patch>` and
  `[ok] herdr integration for pi - ...`, and ends with `Environment ready`
  (other unrelated warnings are fine).

Then check the other direction:

```sh
herdr integration uninstall pi
vx doctor | grep 'integration for pi'
```

Pass: `[warn] herdr integration for pi - not installed, so herdr reads a Pi
soldier's state off the screen; run 'herdr integration install pi'`, and
doctor still exits 0. Run `herdr integration install pi` again before going
on.

Finally, with Pi off the path and no pi profile, a Claude-only setup must show
no Pi line:

```sh
PATH=$(echo "$PATH" | tr ':' '\n' | grep -v "$(dirname "$(command -v pi)")" | paste -sd: -) vx doctor | grep -ci 'pi'
```

Pass: the count is `0` (this works when `pi` lives in a directory of its own
on `PATH`; if it shares `/usr/local/bin` with other tools, skip this check).

## 2. Add Pi profiles

Replace `<provider>/<id>` with the model you picked. Write
`.vexillum/models.json`:

```json
{
  "profiles": {
    "pi-scout": {
      "model": "<provider>/<id>",
      "effort": "low",
      "when": "Scout on Pi.",
      "harness": "pi"
    },
    "pi-mission": {
      "model": "<provider>/<id>",
      "effort": "medium",
      "when": "Mission on Pi.",
      "harness": "pi"
    }
  }
}
```

```sh
git add -A && git -c user.name=t -c user.email=t@example.com commit -qm "vexillum scaffold and pi profiles"
vx models
```

Pass: `vx models` lists `pi-scout  (<provider>/<id>, low, harness pi)` and
`pi-mission  (<provider>/<id>, medium, harness pi)`. The commit matters:
`vx land` refuses a dirty checkout.

Validation, which needs no model and creates nothing:

```sh
vx dispatch "x" --profile pi-scout --model sonnet
vx dispatch "x" --profile pi-scout --effort extreme
```

Pass: the first prints an error containing `must be provider/id`, the second
one containing `unknown effort "extreme" for harness pi (expected one of off,
minimal, low, medium, high, xhigh, max`. Neither creates a tab or a task
(`vx status` is unchanged).

## 3. A scout on Pi

```sh
vx dispatch "Find out which package manager this repository would use and why. Keep it to three lines." --kind scout --profile pi-scout
```

Look at the new herdr tab and answer each of these:

| Observe | Pass |
| --- | --- |
| The tab's process | `pi` started with `--approve --tui-mode regular --model <provider>/<id> --thinking low`. Run `ps ax -o args | grep -- '--tui-mode regular'` and read the line. |
| No dialog | There is no "Trust project folder?" dialog, and the prompt reaches the editor without anyone pressing a key. |
| Scrollback | The pane is not on the alternate screen: scroll the pane's own scrollback up and earlier output (the startup header) is still there. In the default fullscreen mode it would not be. |
| The prompt | The pane shows the dispatched prompt, ending with the instructions to write a report at a path under `~/.vexillum/projects/<id>/reports/` and the `needs-decision:` convention. It does not mention AskUserQuestion. |
| Rules | The Pi startup header lists `AGENTS.md` under `[Context]`. |
| herdr state | While Pi works, `herdr agent get <agent-name>` shows `working`; when it finishes, `idle`. Take `<agent-name>` from `vx status --json` (`herdr_agent_name`). Run the `get` a few times during the run. |
| vx status | `vx status` shows the task `running` while Pi works and `done` after (the sentinel polls every 5 seconds, so allow a few seconds). |
| The report | The report file exists at the path named in the prompt, and `vx status --json` shows `"harness": "pi"`, the model and `"effort": "low"`. |

Then:

```sh
vx strike <task-id>
```

Pass: the strike succeeds (the report exists) and the tab closes. Dispatch a
second scout and run `vx strike` on it before it writes a report: that must
be refused with a message about the missing report.

## 4. A mission on Pi, land and strike

```sh
vx dispatch "Create hello.txt containing the single word hi, and commit it with the message 'add hello'." --kind mission --profile pi-mission
```

Pass:

- The tab behaves as in step 3 (same flags, no dialog, `working` then `idle`).
- When it settles, `vx status` shows `done`, and in the camp (`camp_path` in
  `vx status --json`) `git log --oneline` shows `add hello` ahead of `main`.
- `vx land <task-id>` prints `landed:` and `struck:`, `hello.txt` exists in
  `~/pi-check`, `git log -1 --format=%s` there is `add hello`, and the tab is
  closed.

## 5. A blocked question

```sh
vx dispatch "Before you do anything else, you need to know which color to use. Ask me, using the needs-decision line the way you were told. When I answer, write the answer into color.txt and commit it." --kind mission --profile pi-mission
```

Pi has no question tool, so this goes through the `needs-decision:` line. Pass:

- The soldier ends its turn with a line `needs-decision: <its question>`.
- Within a few sentinel polls (it polls every 5 seconds) `vx status` shows
  the task `blocked`, and
  `vx status --json` has a `decision` whose `question` is the text after
  `needs-decision:` and whose `kind` is `prose`.
- `vx decide <task-id> "blue"` prints `answered task ... now running` (or
  `done` if it was quick). In the pane, the word `blue` appears as typed text
  in the editor and is submitted, and the soldier carries on and writes `blue`
  into `color.txt`.
- No key press is involved: a numeric answer is typed as the text itself.
  Repeat with `vx decide` on a second blocked task using the answer `1` and
  check the pane receives the text `1`, not a menu choice.
- After the answer, `vx status --json` shows the decision with the answer and
  an `amendments` entry, and the task does not block again on the old line.

Also run `vx decide <task-id> --dismiss` on a blocked task of your own making
(dispatch a scout, wait for `blocked`, dismiss): pass if the task becomes
`running` or `done` and `vx status --json` shows the decision `dismissed`.

## 6. Thinking levels and clamping

1. Find a model in `pi --list-models` that does not offer `xhigh` or `max`
   (the table has a thinking column; try a smaller or older model, or one
   whose levels you can see in `/thinking` inside `pi`). Make a profile
   `pi-clamp` with that model and `"effort": "max"`.
2. `vx dispatch "Say hello and stop." --kind scout --profile pi-clamp`.

Pass: validation accepts `max` (it is a Pi level), the soldier starts without
an error, the process line from `ps` shows `--thinking max`, and Pi's footer
or editor border shows a lower level than `max` (Pi clamped it to what the
model supports). Report which model you used and what level the footer
showed.

Then a model that does support the level: the footer shows exactly the level
you asked for.

## 7. A model id that ends in `:free`

If you have access to an OpenRouter-style free model, make a profile with a
model such as `openrouter/<vendor>/<model>:free` and `"effort": "low"`, then
dispatch a one-line scout with it.

Pass: the process line shows `--model openrouter/<vendor>/<model>:free
--thinking low` (the thinking level is not folded into the model id), Pi's
footer shows that model with its `:free` suffix, and the soldier works. A
failure to find the model would read `No models matching` or an invalid
thinking level warning in the pane: report the exact text.

Skip this step if you have no such model, and say so in the table.

## 8. Project trust

This step settles whether `--approve` is doing its job. Do it in the
`~/pi-check` repository.

1. Record the trust file: `cat ~/.pi/agent/trust.json 2>/dev/null; echo`. Keep
   the output.
2. Negative control, to see the dialog this protects against. Make a project
   with a protected resource and open Pi in it by hand, without `--approve`:

   ```sh
   mkdir -p ~/pi-check/.pi && printf '{}\n' > ~/pi-check/.pi/settings.json
   git -C ~/pi-check add -A && git -C ~/pi-check -c user.name=t -c user.email=t@example.com commit -qm "pi project settings"
   cd ~/pi-check && pi
   ```

   Pass: Pi shows "Trust project folder?" with the choices Trust, Trust parent
   folder, Trust (this session only) and Do not trust. Choose
   **Trust (this session only)**, then quit Pi. Do not choose Trust or Trust
   parent folder, which save the decision.
3. Dispatch a scout on `pi-scout` in `~/pi-check` now that `.pi/settings.json`
   is committed (camps are worktrees of the project, so the camp has it too).

   Pass: the soldier's pane shows **no** trust dialog, the soldier reaches
   `working` and finishes, and `vx status` shows it `done`.
4. `cat ~/.pi/agent/trust.json 2>/dev/null; echo` again.

   Pass: identical to step 1. `--approve` trusts the camp for that one
   process and saves nothing.
5. Repeat step 3 after replacing `.pi/settings.json` with a project skill
   directory: `git rm -q .pi/settings.json`, then `mkdir -p .agents/skills/x`
   holding a `SKILL.md` (front matter between `---` lines with `name: x` and
   `description: x`), commit it, and dispatch again.

   Pass: again no dialog and no change to `trust.json`.

If any dispatch in this step stops at a dialog, read the pane with
`herdr agent read <agent-name>` and report the screen. vexillum will report
`agent_not_ready` for a startup block it does not recognize and will not press
any key.

## 9. Follow-up prompt and redispatch

On a finished (`done`) pi task whose camp is still leased:

```sh
vx prompt <task-id> "Also append a second line 'bye' to hello.txt and commit it."
```

Pass: the text appears in the pane as typed input, the task goes back to
`running` and then `done`, and `vx status --json` has an `amendments` entry
holding the prompt text.

Redispatch:

1. Dispatch a new mission on `pi-mission`.
2. Close its herdr tab by hand while it is working.
3. Wait for `vx status` to show it `interrupted`. The sentinel confirms a
   missing pane over a short window before it says so, so allow a few minutes.
4. `vx redispatch <task-id>`.

Pass: it prints `redispatching task ...`, a fresh tab appears, `ps` shows
`pi --approve --tui-mode regular --model ... --thinking ...` again with the
same model and level, `vx status --json` has `"harness": "pi"` and
`"redispatches": 1`, and the new camp is a fresh worktree. Redispatch must
look for `pi` on `PATH`, not `claude`: to confirm, run it once with a `PATH`
that has `herdr` and `pi` but not `claude` and check it does not complain
about claude.

## 10. The tribunal on a Pi mission (optional, needs `gh` and a remote)

Skip this step if the repository has no GitHub remote.

```sh
vx ship <task-id>
```

While the review runs, `ps ax -o args | grep 'pi -p'` shows the reviewer:
`pi -p --no-session --model <provider>/<id> --thinking medium -- <the review
brief>`.

Pass:

- The reviewer is `pi`, not `claude`, on the mission's own model, and its
  answer is parsed (the output shows findings or "no findings", not a parse
  error about invalid JSON).
- Provider failure is surfaced rather than swallowed: run
  `pi -p --no-session --model bogus/none -- hi; echo "exit=$?"` and report the
  exit status (vexillum treats a non-zero exit as a failed review step).

Note for the report: the reviewer here is not restricted to read-only tools.
The review brief tells it to fetch the diff with read-only `git` through a
shell, so a tool allowlist without a shell would blind it. The read-only
variant of the one-shot command (`--tools read,grep,find,ls --no-mcp`) exists
and is tested but is not used by the tribunal yet.

## 11. The commander is woken

With a Claude Code commander session open in the same herdr workspace and the
sentinel Stop hook installed (`vx doctor` shows `sentinel Stop hook` ok):
dispatch a quick scout on `pi-scout` and wait.

Pass: the commander receives the sentinel notice that a soldier finished,
exactly as for a Claude soldier. No Pi extension is involved: the sentinel
reads the status from herdr.

## 12. Results

| Step | Pass / fail / skipped | Notes (versions, model, anything odd) |
| --- | --- | --- |
| 1 doctor and integration | | |
| 2 profiles and validation | | |
| 3 scout: flags, no dialog, scrollback, states, report, strike | | |
| 4 mission: commit, land, strike | | |
| 5 blocked question and `vx decide` as text | | |
| 6 thinking clamp | | |
| 7 `:free` model id | | |
| 8 trust: no dialog, `trust.json` unchanged | | |
| 9 prompt and redispatch | | |
| 10 tribunal | | |
| 11 commander wake | | |

When you report back, include `vx doctor`, `pi --version`, `herdr --version`,
the OS, and for any failed step the exact command, what you saw, and (for pane
problems) the output of `herdr agent read <agent-name>`.
