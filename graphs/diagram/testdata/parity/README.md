# draw.io parity fixtures

Each JSON fixture contains a fixed viewport and an inline draw.io XML document.
`diagramcompare` renders that one document through both paths:

1. native `graphs/diagram` via `RenderImage` and `qui.NewImageCanvas`;
2. draw.io via the local `viewer-static.min.js` in headless Chrome.

ImageMagick is used twice:

- `compare -metric AE -fuzz 1%` is the near-pixel alignment gate. At most 4%
  of pixels may differ after ignoring 1% anti-alias noise.
- `compare -metric RMSE` is reported alongside it to catch color/opacity drift.

Each case writes `native.png`, `browser.png`, `diff.png` and a `compare.png`
montage (native / draw.io / diff).

Current fixture measurements at 640x360 with `fit=true`:

| Fixture | Changed pixels | RMSE |
|---|---:|---:|
| 01-shapes | 1.10% | 0.0641306 |
| 02-routes | 3.21% | 0.144363 |
| 03-arrows | 3.53% | 0.151938 |
| 04-flowchart | 0.80% | 0.0578921 |
| 05-uml | 0.90% | 0.0592156 |
| 06-er | 1.42% | 0.0780441 |
| 07-architecture | 0.42% | 0.041738 |
| 08-labels | 1.69% | 0.0888672 |
| 09-groups | 0.75% | 0.05397 |
| 10-self-parallel | 2.71% | 0.0920341 |
| 11-layers | 1.53% | 0.100467 |
| 12-dense-graph | 1.53% | 0.075874 |

Run all cases:

```bash
go run ./graphs/diagram/cmd/diagramcompare -all -out /tmp/qui-diagram-parity
```

The output directory contains `native.png`, `browser.png` and `diff.png` for
each case. Override `-viewer`, `-chrome`, `-compare` or the Arial font flags
for non-default local installations.
