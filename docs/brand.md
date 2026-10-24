# AXLR Bots identity

<p align="center"><picture><source media="(prefers-color-scheme: dark)" srcset="assets/brand/axlr-bots-emblem-stacked-dark.svg"><img src="assets/brand/axlr-bots-emblem-stacked-light.svg" alt="AXLR Bots" width="300"></picture></p>

The official mark is the stacked emblem: two lines, AXLR over BOTS, in the Underpass pixel Emblem style of the KMP, MADE and AXLR emblems (2-pixel strokes on a 16-row grid). AXLR is set in its palette, one colour per letter, copper, gold, brown and sand; BOTS takes the same palette reversed. The A of AXLR is an invader bot and the O of BOTS a one-eye bot. There are no Spectrum bars in any AXLR Bots mark. Decided with the author on 24 October 2026.

The background is transparent, so each emblem comes in a light and a dark version, like the KMP, MADE and AXLR emblems. Both keep copper `#B85C38` and gold `#D97B2D`; each swaps the one tone that would fade into its page for the house ink of the other emblems. Every colour keeps at least 3:1 against GitHub's page (WCAG non-text contrast); `emblem.py` checks it and prints these ratios:

| Slot | Light, on `#ffffff` | Dark, on `#0d1117` |
|---|---|---|
| copper (A, S) | `#B85C38` 4.54:1 | `#B85C38` 4.17:1 |
| gold (X, T) | `#D97B2D` 3.08:1 | `#D97B2D` 6.15:1 |
| brown (L, O) | `#6B4A2E` 7.94:1 | cream `#F1E6D2` 15.31:1 |
| sand (R, B) | ink `#1B1411` 18.19:1 | `#C9B08C` 9.08:1 |

The icon is copper alone, which passes on both, so it has one version.

| Mark | Files | Size | Use |
|---|---|---|---|
| Emblem, stacked (official) | `assets/brand/axlr-bots-emblem-stacked-{light,dark}.{svg,png}` | 400 × 272 | the README's head, the organisation profile, cards |
| Emblem, one line | `assets/brand/axlr-bots-emblem-{light,dark}.{svg,png}` | 848 × 128 | narrow headers where the name must read in one line |
| Icon | `assets/brand/axlr-bots-icon.{svg,png}` | 112 × 128 | the invader alone, for avatars and favicons |

Preserve each asset's aspect ratio. Where the page follows the reader's theme, pair the two versions in a `<picture>` with `<source media="(prefers-color-scheme: dark)">`, as this page and the README do. For text-only contexts, write **AXLR Bots**.

## Source and regeneration

`assets/brand/emblem.py` is the one source: the grid, the font, the two bots and the palette. It checks the contrast table above and writes the ten files beside itself (SVG always, PNG through Pillow):

```bash
python3 docs/assets/brand/emblem.py
```

Change the script, not the exports.
