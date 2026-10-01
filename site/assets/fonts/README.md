# Fonts for the Open Graph images

Satori (behind `next/og`) reads TrueType only, and the site's own fonts come
through `next/font/google` as woff2, so the same families are kept here as
`.ttf`. They are read at build time by `lib/og.tsx`; nothing is fetched and
nothing here is served to visitors.

| File | Family | Source | License |
| --- | --- | --- | --- |
| `Spectral-SemiBold.ttf` | Spectral 600 | github.com/google/fonts `ofl/spectral` | SIL OFL 1.1, `OFL-Spectral.txt` |
| `JetBrainsMono-Regular.ttf`, `JetBrainsMono-Medium.ttf` | JetBrains Mono 400, 500 | github.com/JetBrains/JetBrainsMono `fonts/ttf` | SIL OFL 1.1, `OFL-JetBrainsMono.txt` |
