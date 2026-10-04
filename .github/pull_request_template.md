## What this does

## Why

Link the issue this addresses, if there is one.

## How to verify

## Checklist

- [ ] The title is a conventional commit (`feat:`, `fix:` or `docs:`)
- [ ] Tests added or updated
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .` (empty output), and `go test ./... -race` pass locally
- [ ] If this touches the core (commands, behavior, config or skills): docs updated in English and Spanish, and the reference regenerated
- [ ] If this touches `site/`: `pnpm lint`, `pnpm build` and `pnpm seo:audit` pass
- [ ] Code, comments, and CLI-facing messages are in English
- [ ] No settled architecture decision from `AGENTS.md` is being silently reversed
