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

<!-- docgen:commands:start -->
| Command | Description |
| --- | --- |
| `vexillum init` | Prepare the current project to be orchestrated by vexillum |
| `vexillum upgrade` | Refresh an already-initialized project's vexillum scaffold |
| `vexillum doctor` | Report on the health of the vexillum environment |
| `vexillum dispatch` | Dispatch a soldier (mission or scout) into an isolated camp |
| `vexillum redispatch` | Re-dispatch an interrupted task from its original prompt |
| `vexillum decide` | Answer a blocked task's open question so it can continue |
| `vexillum status` | Report the current project's fleet of tasks |
| `vexillum land` | Land a finished mission's work into this project's base branch |
| `vexillum ship` | Ship a finished mission through vexillum's own tribunal pipeline, opening a real pull request |
| `vexillum release` | Release a soldier's camp back to the pool once its work has landed |
| `vexillum sentinel` | Watch dispatched soldiers and record status changes |
| `vexillum forum` | Open a local HTML artifact for visual review and collect the user's feedback |
| `vexillum banner` | Publish an HTML artifact to a public URL, or update one already published |
<!-- docgen:commands:end -->

Run `vexillum --help` for the full command list, or `vexillum <command> -h`
for a specific command.

## Contributing

See `CONTRIBUTING.md` for how to build, test, and submit a PR. Found a bug or
have a feature request? Open an issue using the templates in
`.github/ISSUE_TEMPLATE/`.

### Regenerating the command reference

The command list is defined once, in the registry in `internal/cli/commands.go`
(name, one-line summary, full usage). `vexillum --help`, the table above, and
the per-command pages under `site/content/docs/en/reference/cli/` are all derived
from it - do not edit the generated parts by hand. After adding or changing a
command, regenerate and commit the result:

```sh
go run ./tools/docgen
```

`go test ./...` fails with that same instruction if the committed files drift
from the registry. `tools/docgen` is a dev-only tool: it is never part of the
`vexillum` binary or its installation.

## License

[MIT](LICENSE)
