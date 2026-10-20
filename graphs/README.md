# graphs

`graphs` is split into a stable primitive layer at the package root and
ready-to-use native solutions in subpackages.

```text
graphs/          scales and rendering-agnostic data/geometry primitives
graphs/chart/    native ECharts-class declarative charting
graphs/diagram/  native draw.io-class diagram rendering + editing
```

The root package never imports a solution subpackage. Both solutions may
use the root primitives, but chart-specific and editor-specific policy
stays in their respective packages.

## diagram

`graphs/diagram` is a native, retained diagram document and editor. It
supports draw.io-style pages, layers, groups, vertices, edges, ports,
waypoints, labels, styling, routing and headless PNG/SVG/PDF export. draw.io
XML is an interoperability and parity format; the runtime is pure Go with no
JavaScript bridge.

```go
d := diagram.NewEditor(diagram.NewDocument(1000, 640))
d.AddVertex("Process", diagram.ShapeRoundedRect,
    diagram.Rect{X: 80, Y: 80, W: 160, H: 60})
d.Connect(
    diagram.Endpoint{Cell: "v-1", Port: diagram.PortSouth},
    diagram.Endpoint{Cell: "v-2", Port: diagram.PortNorth},
    diagram.RouteOrthogonal, "next",
)
```

Run the interactive editor:

```bash
go run ./examples/diagram
```

Coverage:

- 12 built-in shapes: rectangle, rounded rectangle, ellipse, diamond,
  parallelogram, hexagon, cylinder, document, actor, cloud, note and text
- straight, orthogonal, rounded-orthogonal, entity-relation, curved and
  polyline routing, plus self-loops and parallel-edge fan-out
- triangle, vee, diamond, circle and rectangle arrowheads
- pages, layers, nested groups, custom waypoints and fixed endpoints
- node and edge labels with alignment, wrapping, multiline text and inline edit
- selection (single/multi/marquee), drag, resize handles, port-to-port
  connection, grid snapping, keyboard nudging and delete
- pan, wheel/pinch zoom, fit and cursor feedback
- snapshot-backed undo/redo, copy/paste and native JSON persistence
- draw.io XML import/export, including compressed diagram payloads
- AX children for every node/edge, selectable through the regular agent surface
- deterministic headless rendering through `RenderImage` and PNG/SVG/PDF write
  helpers

The package deliberately does not clone draw.io's full stencil catalog or
support JavaScript-defined shapes. Applications register the primitives they
need and keep business-specific metadata in their own model.

## chart

`graphs/chart` exposes an ECharts-shaped `Option` written as ordinary Go.
There is no JS runtime, embedded browser or external process.

Run the interactive gallery to browse all 20 chart families and the newer
dataset, multi-axis, dataZoom, visualMap, sampling and animation features:

```bash
go run ./examples/graphs
```

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
- linear and category axes, explicit ranges, multiple grids, secondary X/Y axes
- cartesian: line (straight/smooth), area, grouped/stacked/negative bar,
  pictorial bar, scatter, effect scatter, candlestick, heatmap and boxplot
- radial and relational: pie/donut, radar, gauge, funnel, graph/network,
  tree, treemap, sunburst, sankey, parallel coordinates and theme river
- pie/funnel slice legends, leader-line labels, radius percentages and donut support
- continuous visual-map color ramps (heatmap and reusable color interpolation)
- rendered continuous visualMap legend, axis-trigger crosshair and dataZoom
  slider / inside navigation
- CSS hex/rgb colors, per-chart palettes and per-data-item itemStyle colors
- dataset sources with dimensions/encode and native transform functions
- full replacement plus presence-aware `OptionPatch`/`SeriesPatch` updates
- item/axis tooltips with Go formatters
- native hover, selection, pan/zoom and SVG/PDF export through `qui.Canvas`
- declarative entrance/update animation with dirty-rect ticks and configurable easing
- LTTB/min/max/average sampling for large line/area/scatter data
- pie label collision suppression through `labelLayout.hideOverlap`

Map/GEO charts are deliberately out of scope. They require a geographic data
and projection stack rather than another native chart renderer.

Inside the reactive/html surface a chart is a normal hosted leaf, so CSS sizing
and reactive state updates work without a chart-specific bridge:

```go
h.Leaf("x-chart", "chart",
    func() *chart.Chart { return chart.New(option) },
    func(c *chart.Chart) reactive.Flags {
        c.MergeOption(chart.OptionPatch{Series: []chart.SeriesPatch{{
            Name: &seriesName,
            Data: &points,
        }}})
        return reactive.FlagNone
    },
).Class("chart")
```

Remaining hardening work:

1. Progressive chunked rendering and a spatial hit-test index for very large
   datasets beyond the current sampling path.
2. Broader JSON transform operators and dataset lifecycle benchmarks.
3. Point-level accessibility children and richer animation easing/label
   layout policies.

## Pixel parity with ECharts

`graphs/chart/cmd/chartcompare -all` renders every parity option through
`graphs/chart` and through ECharts in offscreen Chrome Canvas, then uses
ImageMagick `compare` for the regression metric. Current baseline cases live
in `graphs/chart/testdata/parity` and gate on normalized RMSE `0.04`. The
current suite covers 22 chart cases; see
[`graphs/chart/testdata/parity/README.md`](chart/testdata/parity/README.md)
for the measured baseline.

## Diagram parity with draw.io

`graphs/diagram/cmd/diagramcompare -all` decodes the same draw.io XML through
`graphs/diagram`, renders the native page through `qui.NewImageCanvas`, renders
the reference through draw.io's local `viewer-static.min.js` in headless
Chrome, and compares the PNGs with ImageMagick. The 12 fixtures cover every
built-in shape family, routing/arrow styles, flowchart/UML/ER/architecture
composition, labels, groups, layers, self/parallel edges and a dense graph.
The near-pixel gate is `compare -metric AE -fuzz 1%`: at most 4% of pixels
may differ. RMSE is still reported because it catches color/opacity drift that
a fuzz-tolerant AE count can miss. Each case writes `native.png`,
`browser.png`, `diff.png` and a `compare.png` three-panel montage. See
[`graphs/diagram/testdata/parity/README.md`](diagram/testdata/parity/README.md)
for the measured baseline. Semantic tests separately pin routing, editing,
serialization, accessibility and export behavior where pixel diffs are not a
meaningful assertion.

chart 模块来源于 https://github.com/apache/echarts, 我们深度参考它并实现来基本的 chart，希望可以让用户可以获得 echarts 的一部分基础能力
diagram 场景没有太多的共性，不同业务的需求差别很大，这里参考了 draw io 的实现
