# vexillum

CLI orchestrator for code agents, written in Go. You talk to a commander
(Claude Code), and it dispatches soldiers in parallel, each isolated in its
own git worktree (camp), returning a PR (mission) or a report (scout). A
sentinel process watches dispatched soldiers and wakes the commander when
something needs attention.

See `AGENTS.md` for how vexillum itself is built.

## Install

macOS / Linux, via Homebrew:

```sh
brew install isaias-alt/tap/vexillum
```

macOS / Linux, via curl:

```sh
curl -fsSL https://raw.githubusercontent.com/isaias-alt/vexillum/main/scripts/install.sh | bash
```

Both install a prebuilt binary for your platform (amd64/arm64). The curl
script verifies the release checksum before installing.

## Usage

```sh
vexillum init        # prepare the current project to be orchestrated by vexillum
vexillum doctor      # check the health of the vexillum environment
vexillum dispatch    # dispatch a soldier (mission or scout) into an isolated camp
vexillum redispatch  # re-dispatch an interrupted task from its original prompt
vexillum status      # report the current project's fleet of tasks (read-only)
vexillum land        # land a finished mission's work into the base branch
vexillum ship        # push a finished mission through the no-mistakes gate for a real PR
vexillum release     # release a soldier's camp back to the pool
vexillum sentinel    # watch dispatched soldiers and record status changes
vexillum upgrade     # refresh an already-initialized project's scaffold
```

Run `vexillum --help` for the full command list, or `vexillum <command> -h`
for a specific command.

## Contributing

See `CONTRIBUTING.md` for how to build, test, and submit a PR. Found a bug or
have a feature request? Open an issue using the templates in
`.github/ISSUE_TEMPLATE/`.

## License

[MIT](LICENSE)
