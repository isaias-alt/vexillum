# vx benchmark

`bench/run.sh` builds `vx` once and times real invocations of it, in throwaway
worlds, with nothing but the Go toolchain and a shell. It tells you how long
each command takes, how many subprocesses it starts, and, against a baseline,
whether anything got slower than noise can explain. It measures only: it never
changes how `vx` behaves.

```sh
bench/run.sh                                  # every scenario, about 3 minutes
bench/run.sh -compare bench/baseline.json     # ... and flag what got slower than the baseline
bench/run.sh -only 'doctor|dispatch'          # scenarios whose name matches a regexp
bench/run.sh -n 50 -slow                      # more runs, plus the scenarios that wait out a 15 s timeout
bench/run.sh -md before-after.md -compare bench/baseline.json   # also a Markdown report
bench/run.sh -h                               # every flag
```

## When to run it

- **Before and after a change that could affect speed**: anything in dispatch,
  land, strike, the sentinel, doctor, the state files or process start-up
  (new imports change the start-up cost of every command). `-compare` is the
  after.
- **When something feels slow**, to see which command and how many
  subprocesses it starts, instead of guessing.
- **Not on every commit.** A run takes minutes and is only meaningful on a
  quiet machine. There is no CI job for it (a decision for later).

Close builds, browsers and other agents first. The header shows the machine's
load average at the start and the end of the run and says so when it is busy.

## Reading the numbers

Times are wall-clock milliseconds, from before the fork to after the wait, so
they include process start. Each scenario runs 3 warm-up times (not counted),
then 30 timed times.

| column | meaning |
|---|---|
| `first` | the first run, before warm-up has settled. On macOS the first run of a freshly built binary is slow because the system scans it (hundreds of ms for the empty program) |
| `min` | the fastest run: the one the machine disturbed least. The most stable number |
| `median` | the typical run. The number to quote |
| `p95`, `max` | the slow tail. They move with whatever else the machine is doing; look, but do not compare them |
| `cpu` | median user+sys time of the process and the children it waited for. Below the wall time when the command waits on something |
| `spawns` | subprocesses one traced run started (`git`, `herdr`, `gh`, `claude`, `pi`, `opencode`, `ps`). Exact, not noisy |

**Spawn count is the cost model.** A bare `git rev-parse` is 6 to 8 ms and
`vx` itself starts in about 4 ms above an empty Go program, so a command's time
is mostly the number of processes it starts. `-v` prints, per command, the
calls that took the longest; the Markdown report (`-md`) lists them all.

### What is noise

Timings on a laptop in use vary by about 2x from run to run, and noise only ever
adds time. That is why the numbers to trust are the **min** and the **median**,
and why nothing here is judged on p95 or max.

What to expect, measured on the machine the baseline comes from (Apple M1 Pro,
about 90 percent idle):

- **Quiet machine**: two full runs back to back agreed within 2 percent on the
  min and within 6 percent on the median of every `vx` scenario, including the
  ones that take 8 ms, where half a millisecond is 6 percent. That is why the
  20 percent threshold is a safe margin there.
- **Ambient load** (a few background daemons, load average about 2.6 on 8
  cores): the min was still within 5 percent of a quiet run's, but the median
  differed by up to 12 percent. A baseline taken under load makes later quiet
  runs look faster, which is why the baseline is captured on a quiet machine.
- **A busy laptop** (builds, browsers, other agents): the same command varies by
  about 2x from run to run, and min and median disagree. `-compare` calls those
  `noisy` rather than guess.

The scenarios that are not about `vx` at all (`floor: empty Go program`, `git
...`) are there to tell you how the machine is doing. If they moved against the
baseline, the machine moved.

## Comparing against a baseline

`bench/baseline.json` is a report taken on a quiet machine and committed. It
carries the host (label, CPU, cores, OS, architecture, Go version), the `vx`
version and commit, the date, and the number of runs behind every scenario.

```sh
bench/run.sh -compare bench/baseline.json
```

runs everything and prints, per scenario, the baseline and current median, the
change in median and in min, the subprocess count (`7→9` when it changed) and a
verdict. The last line says how it went; the exit status is 1 if anything
regressed, 0 otherwise.

A scenario is **SLOWER** only when all of these hold:

1. its **min and its median both rose by more than 20 percent** (`-threshold`),
   or by more than the baseline's own spread (median over min) when that is
   larger, so a scenario that was jittery in the baseline needs a bigger change
   to count;
2. its median rose by **at least 1 ms** (`-abs-floor`): a process that takes 8
   ms wobbles by a millisecond for no reason;
3. the **host has not drifted**: the empty-program scenario is not itself more
   than 20 percent slower than in the baseline. If it is, regressions are
   demoted to `noisy` and the summary says by how much the host moved;
4. it **is still there after running again**. A scenario that looks slower, or
   whose min and median disagree, is run again up to twice (`-recheck`), and the
   attempt with the lowest median stands, since noise only adds time. A real
   regression survives every attempt.

| verdict | meaning |
|---|---|
| `same` | within the threshold, or too small to tell |
| `faster` | min and median both fell by more than the limit |
| `SLOWER` | a regression, per the rules above |
| `noisy` | min and median disagree, or the host drifted: not enough to call. Run again on a quieter machine |
| `info` | shown and compared but never judged: it measures the machine or a polling interval, not something `vx` can regress |
| `new` | not in the baseline |

Color follows the verdict (green better, red worse, yellow doubtful) and only
appears on a terminal with `NO_COLOR` unset; `-color always|never` overrides.

To compare two reports you already have, without running anything:

```sh
bench/run.sh -compare old.json -against new.json
```

and add `-md report.md` to either form for a Markdown report with the two sides
by side (min and median before and after, the percent change, the verdict).

### Guards

A guard is a hard limit, checked on every run whatever the baseline says. It
catches what a percentage cannot, like a baseline that was already bad.

- `vx dispatch (soldier still working)` must have a median under 5 s. Dispatch
  used to wait out a 15 s probe for a soldier that was working; since `f18f213`
  it returns as soon as herdr reports the soldier working (about 70 ms). A run
  of 3 that already breaks the guard stops there instead of taking minutes.
- The wake latency phases have ceilings (below).

A failed guard fails the run.

## Scenarios

| group | what it times |
|---|---|
| Startup | an empty Go program (the cost of starting any process), `vx --version`, `vx --help`. Their difference is `vx`'s own start-up cost |
| Cheap commands | `models`, `status` (0, 50, 500 tasks, text and JSON), `pending list`, `yolo`, `sentinel drain` and `await`, `forum inbox`, `forum stop` |
| doctor | with fake `pi` and `opencode` on a PATH that holds nothing else of the machine's: once with tools that answer at once, once with tools that cost what they cost on a laptop (165 and 62 ms). doctor runs its checks side by side, so the slow variant should take about as long as the slowest tool, not the sum |
| Forum | `forum <file> --no-open` cold (starts the server and listener) and warm, `forum end` |
| Soldier lifecycle | `dispatch` (new camp, reused camp), `strike`, `land`, against a fake herdr and claude; and `dispatch (soldier still working)`, the guarded one. With `-slow`, `dispatch (state never reported)` has the fake answer none of the states dispatch's probe stops at, so it waits out the probe's 15 s upper bound: shown, never judged |
| Wake latency | see below |
| External tools | `git rev-parse`, `git status`, doctor's Stop hook probe, for scale |
| Real tools | `-real-tools`: this machine's `pi`, `opencode`, `gh`, `herdr` and a doctor that runs them. **Machine specific**: these are the numbers that made doctor slow on one laptop and say nothing about another, so they are never in the baseline and never judged. `-real-herdr` adds read-only calls to your real herdr server |

### Wake latency

The path that gets a finished soldier's result in front of the commander, with
the real sentinel and the real `vx sentinel await` (what the Stop hook runs),
and only herdr faked. Per run: a mission is dispatched and gets a commit, a
`vx sentinel await` starts, the fake herdr starts saying `idle`, and the clock
runs until the await exits with the wake.

| phase | what it is | |
|---|---|---|
| `wake: sentinel poll` | the soldier finishes until the sentinel's next pass asks herdr and sees `idle` | uniform over 0 to 5 s: the sentinel polls every 5 s |
| `wake: state write` | from seeing `idle` until the wake file is written | about 1.05 s: the sentinel re-reads the pane a second after the soldier stops so the final report has finished painting, then saves the task |
| `wake: await poll` | from the wake file until the await's next poll finds it | uniform over 0 to 5 s: the await polls every 5 s |
| `wake: total` | the sum | between about 1 and 11 s, median about 6 s |

The two polls wait for a timer, so they are spread evenly over their interval
whatever `vx` does (the moment the soldier finishes is spread evenly too, so
the medians do not depend on how the runs lined up): they are shown and
bounded by a guard (10 s each, 30 s total), not compared. **The state write is
the part `vx` can make slower**, and it is compared. The scenario makes 10
timed runs, not 30, because each takes several seconds.

### Fakes

`bench/fakes/` holds `herdr`, `claude`, `pi`, `opencode` and `gh`, shell
scripts that answer what `vx` asks with the real JSON shape and cost one shell
start. They are on PATH only inside the benchmark. See the comments at the top
of `herdr` for the environment variables that change what it does
(`VXBENCH_PROMPT_MODE`, `VXBENCH_AGENT_STATUS`, `VXBENCH_STATUS_FILE`,
`VXBENCH_SEEN_FILE`) and of `pi` and `opencode` for their start-up delay
(`VXBENCH_PI_DELAY`, `VXBENCH_OPENCODE_DELAY`).

The doctor scenarios use a *hermetic* PATH: a directory of links to the fakes
and to `git`, `ps`, `sh`, `env` and `sleep`, and nothing else. Without it a
machine that has `pi` or `opencode` installed (or two copies of `opencode`)
would time them, and the baseline would not travel.

## Updating the baseline

Update it when the numbers moved on purpose (an optimization landed, a command
now does more work) or when `vx` changed enough that the old baseline is not the
right thing to hold the next change to. Never to make a regression go away.

1. Commit your change first. The baseline records the commit, and a dirty tree
   is flagged in it.
2. Get a **quiet machine**: close builds, browsers and other agents, and wait
   until the load average (shown in the header) is well under half the cores.
3. Capture it with the default scenarios, nothing opt-in (no `-slow`, no
   `-real-tools`, no `-real-herdr`), so that `-compare` finds every row:

   ```sh
   bench/run.sh -label maintainer-laptop -json bench/baseline.json
   ```

4. Run `bench/run.sh -compare bench/baseline.json` right away. A quiet machine
   shows no `SLOWER` and no `noisy`; if it does, the capture was disturbed, do
   it again.
5. Commit `bench/baseline.json` on its own (`chore: refresh the vx benchmark
   baseline`) with the reason in the message.

A baseline from another machine is not comparable: `-compare` says so when the
CPU, core count, OS or architecture differ. Percentages against a different
machine mean nothing; take a baseline there, or compare two reports taken on the
same machine with `-against`.

## Go benchmarks

The hot in-process paths each have `testing.B` benchmarks next to their package:
`internal/atomicfile` (the atomic write, with and without fsync),
`internal/state` (save, load, the locked update, listing 10 to 1000 tasks),
`internal/project`, `internal/models`, `internal/pending`, `internal/sentinel`
(one polling pass over many projects and tasks, with an in-memory herdr),
`internal/camp` (one git process, the camp cycle), `internal/cli` (`status`,
`models`, `pending list` without the process) and `internal/buildinfo`. A plain
`go test` skips them.

```sh
go test ./... -run '^$' -bench . -benchmem
go test ./internal/state -run '^$' -bench List -benchmem -count 10
go test ./internal/state -run '^$' -bench 'List/tasks=1000' -cpuprofile cpu.out
go tool pprof -top cpu.out
```

They create their own state under `b.TempDir()` and never touch `~/.vexillum`.
The camp benchmarks run real `git`, so they show the price of a git process,
which the in-memory ones cannot. `benchstat` (not vendored) compares two
`-count 10` runs.

## How it is built, and safety

Every scenario runs in a world of its own under one temporary directory: a
temporary `HOME`, a scratch git repository with `vx init` applied, and, where a
scenario needs one, its own sentinel and forum server. All of it is stopped and
deleted at the end (`-keep` leaves it for inspection, with the path printed).
Nothing under your real `HOME` or any other project's `.vexillum/` is read or
written, the same rule as for the forum E2E runs. The only exception is
`-real-herdr`, which makes read-only `herdr agent list` and `agent get` calls
with your real `HOME` so herdr can find its socket.

Subprocesses are counted by a traced run: `git`, `herdr`, `gh`, `claude`, `pi`,
`opencode` and `ps` are put first in PATH as links to `bench/cmd/spawnlog`,
which runs the real tool and logs the call and how long the tool took (only the
calls whose parent is the `vx` under test count, and the median of three traced
runs is kept). It cannot see a program started by absolute path (doctor's Stop
hook probe runs `/usr/bin/env`, which has its own scenario), a `vx` that
re-executes itself (the auto-started sentinel; the harness starts one first, the
steady state of a real machine), or what a traced tool starts in turn. The
traced durations are what each tool took, but the shim adds a process start on
top, so do not add them up to the untraced wall time.

The code is `bench/cmd/vxbench/`: `scenarios.go` (the list; to add a scenario,
append it there), `world.go` (the throwaway worlds), `wake.go`, `compare.go`
(the verdict rules), `report.go` (the baseline file format, schema version 1),
`render_term.go` and `render_md.go`. `go test ./bench/...` covers the
statistics, the comparison, the file format and the output.
