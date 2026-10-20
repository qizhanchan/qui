# Chart parity fixtures

These cases compare the native `graphs/chart` renderer against ECharts 6.1
running in offscreen Chrome Canvas. Both sides consume the same raw
ECharts option JSON.

Run:

```bash
go run ./graphs/chart/cmd/chartcompare \
  -case graphs/chart/testdata/parity/mixed-bar-line.json \
  -out /tmp/qui-chart-parity
```

Run the complete suite and write a machine-readable report:

```bash
go run ./graphs/chart/cmd/chartcompare \
  -all \
  -out /tmp/qui-chart-parity > /tmp/qui-chart-parity/report.json
```

For each case the command renders:

- `native.png` through `qui.NewImageCanvas`
- `browser.png` through ECharts in headless Chrome
- `diff.png` through `/opt/homebrew/bin/compare`

The default regression budget is normalized RMSE `0.04`. Set
`-max-rmse=0` to report without gating.

Current local baseline (macOS, Arial loaded as the shared font, ECharts
6.1.0, ImageMagick 7.1.2-34):

| Case | RMSE |
|---|---:|
| `mixed-bar-line.json` | 0.0376 |
| `line-smooth.json` | 0.0343 |
| `stacked-bar.json` | 0.0312 |
| `negative-bar.json` | 0.0399 |
| `multi-grid-axis.json` | 0.0353 |
| `scatter.json` | 0.0372 |
| `pie.json` | 0.0355 |
| `candlestick.json` | 0.0347 |
| `dataset-line.json` | 0.0377 |
| `radar.json` | 0.0345 |
| `funnel.json` | 0.0159 |
| `gauge.json` | 0.0390 |
| `heatmap.json` | 0.0325 |
| `boxplot.json` | 0.0314 |
| `graph.json` | 0.0157 |
| `tree.json` | 0.0381 |
| `treemap.json` | 0.0204 |
| `sunburst.json` | 0.0343 |
| `sankey.json` | 0.0259 |
| `parallel.json` | 0.0388 |
| `theme-river.json` | 0.0395 |
| `pictorial-bar.json` | 0.0399 |

The remaining difference is dominated by text rasterization, antialiasing
at mark boundaries, and occasional one-pixel coverage differences. Layout,
axis extents, stack grouping, bar slots, radial/hierarchical layouts,
squarified treemaps, sankey relaxation, parallel-axis geometry, streamgraph
baselines, palette, title/legend anchoring, and smooth-line control points
are part of this comparison. Map/GEO charts are intentionally excluded.
