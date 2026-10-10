# Docs CI notes

Companion to `.github/workflows/docs.yml`. It covers what the workflow cannot
do by itself: the Vercel setup that gives every pull request a preview of the
docs site.

## What the workflow checks

On pull requests and pushes to `canary` that touch the site, the docgen inputs,
the skills or the docs tooling, `docs.yml` runs, in order: `go test ./...`
(includes the docgen golden test), `pnpm install --frozen-lockfile`, `pnpm lint`,
`pnpm build`, `pnpm seo:audit`, then the link checks. A second job runs Vale
and only reports. How to run each one locally is in `CONTRIBUTING.md`.

## Vercel previews per pull request (manual, dashboard only)

Nothing here is automated from the repository: it needs the general's Vercel
account. Do it once.

1. Vercel dashboard, **Add New... > Project**, import the GitHub repository
   `isaias-alt/vexillum`. If the site project already exists, open it and go
   to **Settings** instead of importing again.
2. **Root Directory**: `site`. Leave "Include source files outside of the Root
   Directory in the Build Step" enabled (the default).
3. **Framework Preset**: Next.js (detected from `site/package.json`).
4. **Install Command** (override on): `pnpm install --frozen-lockfile`
5. **Build Command** (override on): `pnpm build`
6. **Output Directory**: leave the Next.js default (override off).
7. **Node.js Version** (Settings > General): 24.x, the version CI uses.
8. **Ignored Build Step** (Settings > Git): choose **Run my Bash script** and
   enter:

   ```sh
   git diff --quiet HEAD^ HEAD -- .
   ```

   The command runs inside `site/`. Exit code 0 means "no change here, skip
   the build"; exit code 1 builds. The generated CLI and skills pages are
   committed under `site/content/`, so a change to the Go code that matters for
   the docs always shows up as a diff in `site/`, and `docs.yml` (docgen golden
   test) is what enforces that those files are up to date.
9. **Settings > Git**: keep "Pull Request Comments" and the GitHub deployment
   status on, so each PR gets its preview URL and a commit status.
10. **Settings > Environment Variables**: the site needs none today. If one is
    added later, define it for the Preview environment too.
11. Production branch stays `canary`. Pushes to `canary` deploy to the production
    domain, every other branch and pull request gets a preview URL.

If a PR preview is wanted for a change outside `site/` (for example only
`skills/`), the ignored-build-step command will skip it by design. Run
`go run ./tools/docgen`, commit the regenerated pages, and the diff in `site/`
triggers the build.

Optional, after the first preview works: in **Settings > Branches** of the
GitHub repository, require the `Docs / Build, audit and link check` check on
`canary`. Do not require `Docs / Prose lint (advisory)`: it is meant to never
block.
