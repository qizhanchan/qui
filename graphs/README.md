# graphs

`graphs` is split into a stable primitive layer at the package root and
ready-to-use native solutions in subpackages.

```text
graphs/          scales and rendering-agnostic data/geometry primitives
graphs/chart/    native ECharts-class declarative charting
graphs/diagram/  native draw.io-class diagram editing (next)
```

The root package never imports a solution subpackage. Both solutions may
use the root primitives, but chart-specific and editor-specific policy
stays in their respective packages.

## chart

`graphs/chart` exposes an ECharts-shaped `Option` written as ordinary Go.
There is no JS runtime, embedded browser or external process.

```go
c := chart.New(chart.Option{
    Title: &chart.TitleOption{Text: "Orders"},
    XAxis: chart.AxisOption{
        Type: chart.AxisCategory,
        Data: []string{"Mon", "Tue", "Wed"},
    },
    YAxis: chart.AxisOption{Type: chart.AxisValue},
    Series: []chart.SeriesOption{{
        Type: chart.SeriesBar,
        Name: "Online",
        Data: chart.Numbers(12, 18, 15),
    }},
})
```

Current declarative coverage:

- title / subtitle, tooltip visibility, legend placement
- linear and category axes, explicit ranges, grid gutters
- cartesian: line (straight/smooth), area, grouped/stacked/negative bar,
  pictorial bar, scatter, effect scatter, candlestick, heatmap and boxplot
- radial and relational: pie/donut, radar, gauge, funnel, graph/network,
  tree, treemap, sunburst, sankey, parallel coordinates and theme river
- pie/funnel slice legends, leader-line labels, radius percentages and donut support
- continuous visual-map color ramps (heatmap and reusable color interpolation)
- CSS hex/rgb colors, per-chart palettes and per-data-item itemStyle colors
- full replacement plus name/index-based `MergeOption` updates
- item/axis tooltips with Go formatters
- native hover, selection, pan/zoom and SVG/PDF export through `qui.Canvas`

Map/GEO charts are deliberately out of scope. They require a geographic data
and projection stack rather than another native chart renderer.

Next layers:

1. Dataset/source/dimensions/encode and transform pipelines.
2. Multiple grids/axes and secondary Y axes.
3. dataZoom, axisPointer, rendered visualMap legends and frame animation.
4. Large-data sampling, progressive rendering and label collision.
5. The draw.io-class editor under `graphs/diagram`.

## Pixel parity with ECharts

`graphs/chart/cmd/chartcompare` renders the same ECharts option through
`graphs/chart` and through ECharts in offscreen Chrome Canvas, then uses
ImageMagick `compare` for the regression metric. Current baseline cases live
in `graphs/chart/testdata/parity` and gate on normalized RMSE `0.04`. The
current suite covers 20 chart cases; see
[`graphs/chart/testdata/parity/README.md`](chart/testdata/parity/README.md)
for the measured baseline.
