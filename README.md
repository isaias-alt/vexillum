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
curl -fsSL https://vx.lucasco.dev/install | bash
```

Both install a prebuilt binary for your platform (amd64/arm64). The curl
script verifies the release checksum before installing.

Both install the latest stable release. To try the latest canary build (no
stability promise) or an exact version, pass an option to the script:

```sh
curl -fsSL https://vx.lucasco.dev/install | bash -s -- --channel canary
curl -fsSL https://vx.lucasco.dev/install | bash -s -- --version v0.2.0-rc.1
```

## Usage

<!-- docgen:commands:start -->
| Command | Description |
| --- | --- |
| `vx init` | Prepare the current project to be orchestrated by vexillum |
| `vx upgrade` | Update vx to the latest release, then refresh the project's vexillum scaffold |
| `vx doctor` | Report on the health of the vexillum environment |
| `vx dispatch` | Dispatch a soldier (mission or scout) into an isolated camp |
| `vx models` | List the model and effort profiles a dispatch can use |
| `vx yolo` | Turn yolo mode on or off for this project, or print whether it is on |
| `vx redispatch` | Re-dispatch an interrupted task from its original prompt |
| `vx decide` | Answer a blocked task's open question so it can continue |
| `vx prompt` | Send a follow-up prompt to a soldier that already finished |
| `vx pending` | Record and clear the commander's own pending decisions |
| `vx status` | Report the troop: every mission and scout in the current project |
| `vx land` | Land a finished mission's work into this project's base branch |
| `vx ship` | Ship a finished mission through vexillum's own tribunal pipeline, opening a real pull request |
| `vx strike` | Strike a soldier's camp (dismantle it and return it to the pool) once its work has landed |
| `vx sentinel` | Watch dispatched soldiers and record status changes |
| `vx forum` | Open a local HTML artifact for visual review and collect the user's feedback |
<!-- docgen:commands:end -->

Run `vx --help` for the full command list, or `vx <command> -h`
for a specific command.

## Contributing

See `CONTRIBUTING.md` for how to build, test, and submit a PR. Found a bug or
have a feature request? Open an issue using the templates in
`.github/ISSUE_TEMPLATE/`.

### Regenerating the command reference

The command list is defined once, in the registry in `internal/cli/commands.go`
(name, one-line summary, full usage). `vx --help`, the table above, and
the per-command pages under `site/content/docs/en/reference/cli/` are all derived
from it - do not edit the generated parts by hand. After adding or changing a
command, regenerate and commit the result:

```sh
go run ./tools/docgen
```

`go test ./...` fails with that same instruction if the committed files drift
from the registry. `tools/docgen` is a dev-only tool: it is never part of the
`vx` binary or its installation.

## License

[MIT](LICENSE)
