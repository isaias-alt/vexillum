# vendor-fonts (dev-only)

Fonts that replace the copy `@excalidraw/excalidraw` ships. `build.js` copies
`vendor-fonts/<family>/` over `internal/forum/assets/whiteboard/fonts/<family>/`
after staging the package's fonts, and `generate-notices.js` compares the
vendored files against this directory instead of the package for these families.
Like everything here, none of it runs at vexillum's build or install time.

## Liberation/LiberationSans-Regular.woff2

The `@excalidraw/excalidraw` 0.18.1 package ships Liberation Sans 1.05
(Ascender Corporation, 2007), whose terms are not established by anything in the
file. This file is Liberation Sans 2.1.5, licensed under the SIL OFL 1.1 (the
font's own name table says so; the release `LICENSE` file carries the text).

- Source: the official release,
  <https://github.com/liberationfonts/liberation-fonts/releases/tag/2.1.5>,
  asset `liberation-fonts-ttf-2.1.5.tar.gz`
  (<https://github.com/liberationfonts/liberation-fonts/files/7261482/liberation-fonts-ttf-2.1.5.tar.gz>),
  sha256 `7191c669bf38899f73a2094ed00f7b800553364f90e2637010a69c0e268f25d0`.
- Input: `LiberationSans-Regular.ttf` from that archive,
  sha256 `76d04c18ea243f426b7de1f3ad208e927008f961dc5945e5aad352d0dfde8ee8`.
- Conversion: a lossless container change only (TTF to WOFF2), no subsetting and
  no change to glyphs, tables or the name table, so the Reserved Font Name
  "Liberation" is not touched:

  ```sh
  python3 -m venv venv && venv/bin/pip install fonttools brotli   # fonttools 4.60.2
  venv/bin/python -c "
  from fontTools.ttLib import TTFont
  f = TTFont('LiberationSans-Regular.ttf'); f.flavor = 'woff2'
  f.save('LiberationSans-Regular.woff2')"
  ```
