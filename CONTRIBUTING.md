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

vexillum supports Claude Code only, for now. Support for another agent is
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

## Installing your own build

Maintainers can install a build of the current checkout as their local `vx`
with `scripts/install-local.sh`. It refuses a dirty checkout or a branch other
than `canary` (install only after landing with green tests), checks that the
fresh binary's `vx --version` reports HEAD, keeps the old binary as `vx.prev`
next to it for a one-step rollback, and replaces `vx` atomically. A dev build
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
