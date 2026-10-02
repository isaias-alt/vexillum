# vexillum site

Marketing landing and documentation for vexillum. Next.js 16 with
[Fumadocs](https://fumadocs.dev) (versions pinned in `package.json`), Tailwind 4,
English and Spanish. Governed by `AGENTS.md` here; not part of the Go binary.

## Run

```sh
pnpm install
pnpm dev        # http://localhost:3000
pnpm build      # production build, includes the static search index
pnpm lint
pnpm typecheck
pnpm seo:audit  # after pnpm build: serves the build and audits every page's SEO
```

## Layout

| Path | What |
| --- | --- |
| `app/[lang]/(home)` | the landing, `/` and `/es` |
| `app/[lang]/docs` | the docs, `/docs` and `/es/docs` (Fumadocs notebook layout, re-themed in `app/globals.css`) |
| `content/docs/en`, `content/docs/es` | the pages, one tree per locale; `meta.json` orders the sidebar |
| `content/docs/en/reference/cli` | **generated**, see below |
| `lib/strings.ts` | landing and nav strings per locale |
| `app/api/search/route.ts` | static search index, queried in the browser per locale |
| `app/llms.txt`, `app/llms-full.txt`, `app/llms.mdx` | `llms.txt`, `llms-full.txt` and `/docs/<page>.md` (`/es/...` for Spanish) |
| `lib/seo.ts`, `lib/docs-seo.ts` | metadata (title, canonical, hreflang, Open Graph, Twitter) and JSON-LD for every page |
| `app/sitemap.ts`, `app/robots.txt` | sitemap with hreflang alternates, robots.txt |
| `app/og/[lang]/[...slug]` | the 1200x630 social images, prerendered at build with `next/og` (`lib/og.tsx`, fonts in `assets/fonts`) |
| `scripts/seo-audit.mjs` | `pnpm seo:audit`: fails on duplicate or missing titles and descriptions, broken canonical or hreflang, bad JSON-LD, missing images |
| `proxy.ts` | locale routing: English has no prefix, Spanish lives under `/es` |

## The CLI reference is generated

`content/docs/en/reference/cli/*` and `content/docs/es/reference/cli/index.mdx`
come from the command registry in `internal/cli`. Never edit them by hand; from
the repository root run `go run ./tools/docgen` (`go test ./...` fails when they
drift). The reference is English only, because a command's usage is the binary's
own output: Spanish pages for a command fall back to the English page, and the
generated Spanish index links to them under `/es/docs`.

## Writing docs

- English first, then the Spanish translation with the same file name and
  structure. Domain and technical terms (commander, soldier, mission, scout,
  camp, sentinel, worktree, hook, flag) stay in English in Spanish.
- A page missing from `es/` is served from `en/` until it is translated.
- Facts come from the code and `vx <command> -h`; check them before writing.
