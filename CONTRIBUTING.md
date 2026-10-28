# Contributing to vexillum

Contributions are welcome: bug reports, feature ideas, documentation fixes and
code. The full guide is the Contributing page of the docs site (source in
`site/content/docs/en/guides/contributing.mdx`). This file states the same
rules in short.

## Issues

- **Bug**: use the bug report template. Include what happened, what you
  expected, steps to reproduce, and the output of `vx --version` and `vx doctor`.
- **Feature**: use the feature request template and describe the problem
  first, then your proposal and the alternatives you considered.

## Pull requests

- Branch from `canary`, the default branch. Keep each branch small and focused.
- Add or update tests.
- Title the PR and write commits as conventional commits (`feat:`, `fix:`,
  `docs:`, `chore:` or `test:`). They keep the history readable.
- Run the checks below before pushing.

## Documentation rule

A PR that changes commands, behavior, configuration or skills must also update
the docs in both languages (`site/content/docs/en` and `site/content/docs/es`,
same pages, same menu position) and regenerate the reference with
`go run ./tools/docgen`. Never edit generated pages by hand.

## Scope

vexillum runs on Claude Code, Pi and OpenCode. Support for another agent is
discussed first in an issue, not proposed as a PR. A few architecture decisions
are settled (see `AGENTS.md`) and are not reopened in a PR.

## Getting set up

```sh
git clone https://github.com/isaias-alt/vexillum.git
cd vexillum
go build -o vx ./cmd/vx
```

No other runtime dependency is required to build or test the binary itself.
(Exercising `dispatch`/`sentinel` end-to-end additionally needs Claude Code
and herdr installed - `vx doctor` tells you what's missing.)

## Before opening a PR

CI runs exactly this, so it's worth matching locally:

```sh
go build ./...
go vet ./...
gofmt -l .        # must print nothing
go test ./... -race
```

## Testing the forum end to end

The forum has three kinds of test, from cheapest to most real:

- DOM tests (`chrome_dom_test.go`, `sdk_dom_test.go`, the scripts in
  `internal/forum/testdata`) run the chrome and SDK scripts under Node.
- Real-Chrome tests (`internal/forum/*_chrome_test.go`) start headless Chrome on
  a throwaway profile and load a real session page. Set `VX_TEST_CHROME` to pick
  the browser; the tests skip when there is none.
- `cdp_chrome_test.go` does what a user does: it clicks inside the artifact.

Why the last one exists: the artifact runs in an iframe with an opaque origin
(`sandbox` without `allow-same-origin`), so nothing in the chrome page can reach
into it, and neither can a tool that works on the page's DOM. The claude-in-chrome
tool cannot see inside it (`find` and `read_page` stop at the frame) and a
coordinate `left_click` on a button in the artifact does not register (checked
against a local session: the button's own click handler never ran). That is why
the other real-Chrome tests have the artifact report on itself.

The Chrome DevTools Protocol can click there, and it is reliable. Input events
sent with `Input.dispatchMouseEvent` are dispatched by the browser at viewport
coordinates and routed to whichever frame is under the point, same-origin or
not, so the click reaches the artifact as a trusted event. Chrome runs the
sandboxed frame in its own process, which CDP lists as an `iframe` target:
attaching to it gives a session that runs scripts inside the artifact, so a
selector can be turned into coordinates (the helper falls back to
`Page.createIsolatedWorld` if the frame ever shares the page's process). In a
test:

```go
browser := startCDPChrome(t, chrome, env.ts.URL+"/session/"+key)
browser.click("#artifact", "#pick") // a real move, press and release
```

`click` waits until the element has stopped moving before it clicks, because
the frame starts loading before the chrome around it has laid out. The helper
speaks the WebSocket protocol itself so the tests stay standard library only.

### Keep E2E runs in their own directory

An E2E run must never use another project's `.vexillum/forum/`: it holds that
project's live artifacts and a run that clears or rewrites it loses them. This
applies to a soldier doing a manual run as much as to a test:

- Tests build their own state: `newEnv` gives each forum test a temporary home
  and artifact directory, and every Chrome gets a temporary `--user-data-dir`.
  The `internal/cli` suite also runs with a temporary `HOME`. Do not set `HOME`
  for a Chrome process, it renders a blank page.
- For a manual run, create a scratch project under a temporary directory (run
  `vx init` there, write artifacts to its own `.vexillum/forum/`) and use a
  temporary `HOME` for `vx`. Never point `vx forum` at a sibling project, and
  never `rm` anything under a `.vexillum/forum/` you did not create yourself.

## Benchmarking

Two layers, both measure only; neither changes how vx behaves. Run them on a
quiet machine (close builds and other agents) and compare numbers taken on the
same machine.

### Go benchmarks (`testing.B`, standard library only)

The hot in-process paths each have benchmarks next to their package:
`internal/atomicfile` (the atomic write, with and without fsync),
`internal/state` (save, load, the locked update, listing 10 to 1000 tasks),
`internal/project`, `internal/models` (defaults and layered files),
`internal/pending`, `internal/sentinel` (one polling pass over many projects and
tasks, with an in-memory herdr), `internal/camp` (one git process, the camp
cycle), `internal/cli` (`status`, `models`, `pending list` without the process)
and `internal/buildinfo`. They are skipped by a plain `go test`.

```sh
go test ./... -run '^$' -bench . -benchmem
go test ./internal/state -run '^$' -bench List -benchmem -count 10
go test ./internal/state -run '^$' -bench 'List/tasks=1000' -cpuprofile cpu.out
go tool pprof -top cpu.out
```

A benchmark that creates its own state uses `b.TempDir()` and never touches
`~/.vexillum`. The camp benchmarks run real `git`, so they show the price of a
git process, which the in-memory ones cannot.

### End-to-end wall time (`bench/`)

`bench/run.sh` builds `vx` once and times real invocations of it, using only
the Go toolchain and a shell. For every scenario it reports the first run, then
min, median and p95 of N warm runs (default 30 after 3 warm-up runs), the CPU
time the process used, and how many subprocesses a run started.

```sh
bench/run.sh                        # every scenario, results as markdown on stdout
bench/run.sh -n 50 -json out.json   # more runs, and the full report as JSON
bench/run.sh -only 'doctor|models'  # scenarios whose name matches a regexp
bench/run.sh -slow                  # also the scenario that waits out the probe's 15s bound
bench/run.sh -real-herdr            # also read-only calls to your real herdr server
bench/run.sh -h                     # every flag
```

What it measures:

- **Startup**: an empty Go program (the cost of starting any process here),
  `vx --version` and `vx --help`. Their difference is vx's own start-up cost,
  and `GODEBUG=inittrace=1` splits out the time spent in package `init`.
- **Cheap commands**: `models`, `status` (0, 50 and 500 tasks, text and JSON),
  `pending list`, `yolo`, `sentinel drain` and `await`, `forum inbox`, `forum
  stop`, and `doctor`.
- **Forum without a browser**: `forum <file> --no-open` cold (it starts the
  server and the listener) and warm, and `forum end`.
- **Soldier lifecycle**: `dispatch` (a new camp, and a reused one), `strike` and
  `land`, against a fake herdr and a fake claude (`bench/fakes`). They answer the
  calls dispatch makes with the real JSON shape, so no model, terminal or herdr
  server is needed and what is left is vexillum's work plus its git and herdr
  calls. `dispatch (soldier still working)` has the fake herdr behave like a
  soldier that is at work when the prompt goes in: it answers `working` at once
  to a call that asks to stop at `working` (`--until working`), as herdr does,
  and sleeps for the whole `--timeout` to any other. With `-slow`, `dispatch
  (soldier state never reported)` has it answer none of the states, so the probe
  waits out its upper bound (15s). The fake's `VXBENCH_PROMPT_MODE` and
  `VXBENCH_AGENT_STATUS` (see `bench/fakes/herdr`) also let a test kill a
  dispatch mid-prompt and show a sentinel an idle soldier.
- **External tools** vx shells out to (`git`, `herdr --version`, `pi --version`,
  `opencode --version`, `gh --version`, doctor's Stop hook probe), for scale.

Subprocesses are counted by a traced run: the tools `git`, `herdr`, `gh`,
`claude`, `pi`, `opencode` and `ps` are put first in `PATH` as symlinks to
`bench/cmd/spawnlog`, which runs the real tool and logs the call and how long the
tool took (the harness keeps only the calls whose parent is the vx process under
test, and the median of three traced runs). It cannot see a program started by
absolute path (doctor's Stop hook probe runs `/usr/bin/env`, which has its own
scenario above), a vx that re-executes itself (the auto-started sentinel; the
harness starts one first, the steady state of a real machine), or what a traced
tool starts in turn. On Linux, `strace -f -e trace=execve -c` gives the full
count if you need it; macOS `dtruss` needs root.

Every scenario runs in a world of its own under one temporary directory: a
temporary `HOME`, a scratch git repository with `vx init` applied, and, where a
scenario needs one, its own sentinel and forum server. All of it is stopped and
deleted at the end (`-keep` leaves it for inspection, with the path printed).
Nothing under your real `HOME` or any other project's `.vexillum/` is read or
written, the same rule as for the forum E2E runs above. The only exception is
`-real-herdr`, which makes read-only `herdr agent list` and `agent get` calls
with your real `HOME` so herdr can find its socket.

Reading the numbers: the median and min are stable, the p95 and max are not on
a busy laptop. The `first` column of the first scenarios includes the operating
system scanning a freshly built binary the first time it runs (several seconds
on macOS), which is why warm-up runs exist. The traced durations are what each
tool took, but the shim adds a process start on top, so do not add them up to
the untraced wall time. To add a scenario, append it to `scenarios` in
`bench/cmd/vxbench/scenarios.go`.

## Installing your own build

Maintainers can install a build of the current checkout as their local `vx-dev`
with `scripts/install-local.sh`. It refuses a dirty checkout or a branch other
than `canary` (install only after landing with green tests), checks that the
fresh binary's `vx --version` reports HEAD, keeps the old binary as `vx-dev.prev`
next to it for a one-step rollback, and replaces `vx-dev` atomically. The stable
`vx` comes from Homebrew, so the dev build never takes its name. A dev build
prints its identity, for example `vx dev (a1b2c3d, 2026-10-05, dirty)`.

```sh
scripts/install-local.sh            # installs to ~/.local/bin
VX_INSTALL_DIR=/tmp/vx-bin scripts/install-local.sh
scripts/install-local.sh --force    # skip the clean and branch guards
```

`VX_INSTALL_DIR` sets the destination, `VX_INSTALL_BASE` the base branch and
`VX_INSTALL_FORCE=1` is the same as `--force`.

## Releases

Release notes live only on [GitHub Releases](https://github.com/isaias-alt/vexillum/releases).
goreleaser publishes the install header and a compare link, with no commit
list; when a release deserves it, the maintainer writes a short highlights
paragraph by hand on the GitHub release page. There is no `CHANGELOG.md` and no
changelog page on the site.

## Third-party notices

`THIRD-PARTY-NOTICES.md` is generated and never edited by hand. After changing
the whiteboard bundle, its `package-lock.json` or the vendored fonts, run
`cd tools/whiteboard-bundle && npm ci && npm run notices` and commit the result
(see `tools/whiteboard-bundle/README.md`). `go test ./internal/thirdparty`
fails when the notices are stale.

## Docs checks

Changes to `site/`, `tools/docgen`, `skills/`, `internal/cli` or the docs
tooling run the `Docs` workflow (`.github/workflows/docs.yml`). To run each
step locally:

```sh
go test ./...                       # includes the docgen golden test
go run ./tools/docgen               # regenerate the reference pages if it fails

cd site
pnpm install --frozen-lockfile
pnpm lint
pnpm build
pnpm seo:audit                      # after pnpm build
pnpm links:check                    # after pnpm build, needs lychee
cd ..

lychee --config .lychee.toml --offline --exclude-path site --exclude-path .github/ISSUE_TEMPLATE '**/*.md'
vale site/content/docs/en README.md CONTRIBUTING.md
```

Run `pnpm lint`, `pnpm build` and the audits one after another, never at the
same time. `pnpm links:check` is strict about internal links and fragments and
only warns about external ones (config in `.lychee.toml`, ignore list in
`.lycheeignore`). Vale is advisory, English only (config in `.vale.ini`, rules
in `.vale/styles/Vexillum`); its findings never fail CI. Install `lychee` and
`vale` with `brew install lychee vale`. How to set up Vercel previews for pull
requests is in `.github/docs-ci-notes.md`.

## Code conventions

- Explicit `if err != nil`; never swallow an error. Wrap with
  `fmt.Errorf("...: %w", err)` when the extra context helps.
- No panics in normal flow - panic only for a truly unrecoverable state.
- Code, comments, and CLI-facing messages are in English.
- State written to disk is atomic (write to a temp file, then rename).
- Tests use the standard library `testing` package only.
