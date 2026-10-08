# Mona Sans variable font

Unmodified official GitHub font, stored locally. No CDN or font network request
is needed at runtime. Only its local filename was shortened to `mona-sans.woff2`;
the internal font names and tables are unchanged. This is the official family,
not a claim that the file is byte-identical to the font served by githubuniverse.com.

- Repository: https://github.com/github/mona-sans
- Release tag: `v2.0.27`
- Pinned commit: `0f7dc66ddd766605eb0e75c3f47bf9d1dd38ceca`
- Upstream path: `fonts/webfonts/variable/MonaSansVF[wdth,wght,opsz,ital].woff2`
- [Pinned download](https://raw.githubusercontent.com/github/mona-sans/0f7dc66ddd766605eb0e75c3f47bf9d1dd38ceca/fonts/webfonts/variable/MonaSansVF%5Bwdth,wght,opsz,ital%5D.woff2)
- Downloaded: 2026-10-07
- Size: **532,968 bytes**
- SHA256: `fd40288d051171b51e3d01f36790604470dbb4d4fc5b36ee5a8119f4f4c6b3e1`
- Git blob SHA1, independently matched to the pinned repository tree:
  `f12b5e3d6853567a21a721b229a7ae9160f674a7`

The original upstream `LICENSE` and `OFL.txt` are preserved in
`MonaSans-LICENSE.txt` and `MonaSans-OFL.txt`. Both contain the full SIL Open Font
License 1.1 and their respective upstream copyright notices. Include them in
redistributed binary-package notices; Vite does not copy these texts merely
because the WOFF2 is imported by CSS. The font's internal copyright corresponds
to `OFL.txt` (2022 Mona Sans Project Authors, Reserved Font Name "Mona").

## Actual font tables

Inspected directly with FontTools 4.66.1 and Brotli 1.2.0; no font conversion,
subsetting, renaming of internal names, or editing was performed.

- Family / typographic family: `Mona Sans VF`
- Subfamily: `Regular`
- PostScript name: `MonaSansVF-Regular`
- Version string: `Version 2.027;Glyphs 3.4.1 (3436)`

| Axis | Minimum | Font-file default | Maximum |
| --- | ---: | ---: | ---: |
| `wdth` (width) | 75 | 100 | 125 |
| `wght` (weight) | 200 | 200 | 900 |
| `opsz` (optical size) | 0 | 0 | 100 |
| `ital` (italic) | 0 | 0 | 1 |

The metadata defaults above are the values in this exact file, not suggested
body-text CSS. Set an explicit body weight (for example 400). A local
`@font-face` may use the CSS family alias `Mona Sans` with `font-weight: 200 900`
and `font-stretch: 75% 125%`; the internal family remains `Mona Sans VF`.

## Cyrillic limitation

The best Unicode cmap contains 568 code points, including all 95 printable
ASCII characters. It contains **zero characters in U+0400–U+052F** and **0/66
Russian uppercase/lowercase letters including Ё/ё**. Do not claim that Russian
UI text is rendered in Mona Sans.

For a stack such as `"Mona Sans", "Segoe UI Variable", "Segoe UI", system-ui,
sans-serif`, browsers use Mona Sans for supported characters and a later local
font for missing Cyrillic glyphs. On Windows that will normally be Segoe UI
Variable or Segoe UI, depending on installation; `system-ui` covers other hosts.
Mixed Latin/Cyrillic lines can therefore have visibly different letterforms or
metrics. No additional fallback font has been downloaded or bundled.

## Repeat the download

From the repository root:

```sh
curl --fail --location \
  'https://raw.githubusercontent.com/github/mona-sans/0f7dc66ddd766605eb0e75c3f47bf9d1dd38ceca/fonts/webfonts/variable/MonaSansVF%5Bwdth,wght,opsz,ital%5D.woff2' \
  --output desktop/ui/assets/fonts/mona-sans.woff2
(cd desktop/ui/assets/fonts && shasum -a 256 -c SHA256SUMS)
```

This task adds assets and provenance only; it does not change CSS, UI, or build
scripts. Exact asset metadata is also recorded in `provenance.json`.
