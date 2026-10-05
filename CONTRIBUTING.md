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
- Title the PR and write commits as conventional commits (`feat:`, `fix:` or
  `docs:`). `chore:` and `test:` are left out of the release notes.
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
