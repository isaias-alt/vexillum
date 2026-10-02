<!-- BEGIN:nextjs-agent-rules -->

# This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` (resolved from this file's directory; in monorepos the `next` package may not be visible from the repo root) before writing any code. Heed deprecation notices.

This block is written and re-added by `next dev` — verify at `node_modules/next/dist/server/lib/generate-agent-files.js`. Removing it from a diff only re-creates the uncommitted change; committing it with your work keeps the tree clean.

<!-- END:nextjs-agent-rules -->

## Docs checks

CI (`.github/workflows/docs.yml`) runs `go test ./...` (docgen golden test),
`pnpm install --frozen-lockfile`, `pnpm lint`, `pnpm build`, `pnpm seo:audit`
and the link checks, in that order and never concurrently. Run the same from
this directory before finishing a change:

```sh
pnpm install --frozen-lockfile && pnpm lint && pnpm build && pnpm seo:audit
pnpm links:check   # after pnpm build; needs lychee (brew install lychee)
```

`pnpm links:check` serves the build, crawls every page in the sitemap, fails on
any broken internal link or fragment and only warns on external ones. Vale
(`vale site/content/docs/en` from the repo root) is advisory and English only.
Details and the repo-level commands are in `CONTRIBUTING.md`; Vercel preview
setup is in `.github/docs-ci-notes.md`.
