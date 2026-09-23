# Contributing to vexillum

vexillum is a Go CLI, built in incremental layers (see `AGENTS.md`). This
document is for anyone sending a PR or filing an issue against the binary
itself - if you're looking for how the product AGENTS.md that vexillum
scaffolds into *your* project behaves, that's a different document (the one
`vexillum init` writes into your own repo).

## Before you start

For anything beyond a small fix, open an issue first. A few things are
settled architecture decisions (language, packaging model, single harness in
v1, herdr as the primary session backend, no second commander) and won't be
relitigated in a PR - if you're not sure whether something falls into that
bucket, ask in the issue before writing code.

## Getting set up

```sh
git clone https://github.com/isaias-alt/vexillum.git
cd vexillum
go build -o vexillum ./cmd/vexillum
```

No other runtime dependency is required to build or test the binary itself.
(Exercising `dispatch`/`sentinel` end-to-end additionally needs Claude Code
and herdr installed - `vexillum doctor` tells you what's missing.)

## Before opening a PR

CI runs exactly this, so it's worth matching locally:

```sh
go build ./...
go vet ./...
gofmt -l .        # must print nothing
go test ./... -race
```

## Code conventions

- Explicit `if err != nil`; never swallow an error. Wrap with
  `fmt.Errorf("...: %w", err)` when the extra context helps.
- No panics in normal flow - panic only for a truly unrecoverable state.
- Code, comments, and CLI-facing messages are in English.
- State written to disk is atomic (write to a temp file, then rename).
- Tests use the standard library `testing` package only.

## Scope

Keep PRs focused on one thing. If your change touches a settled architecture
decision from `AGENTS.md`, expect it to need a discussion in an issue before
a PR is the right next step.
