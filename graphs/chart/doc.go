// Package chart is qui's native, ECharts-class charting solution.
//
// The declarative Option surface is intentionally close to the shape of
// an ECharts option while remaining ordinary Go: no JavaScript runtime,
// DOM, embedded browser or external process is involved. Option values
// are lowered onto qui widgets and rendered through qui.Canvas.
//
//	c := chart.New(chart.Option{
//		XAxis: chart.AxisOption{Type: chart.AxisCategory, Data: []string{"A", "B"}},
//		Series: []chart.SeriesOption{{
//			Type: chart.SeriesBar,
//			Name: "Values",
//			Data: chart.Numbers(3, 7),
//		}},
//	})
//
// Option covers title, legend, item/axis tooltips/crosshairs, one or more
// grids, multiple primary/secondary axes, grid gutters, palettes, continuous
// visual-map colors and legends, line/area/bar/pictorial-bar/scatter/
// effect-scatter/pie/candlestick/radar/funnel/gauge/heatmap/boxplot series,
// and graph/tree/treemap/sunburst/sankey/parallel/theme-river layouts.
// Dataset sources support dimensions/encode plus native and JSON-declarative
// filter/sort transforms.
// MergeOption supplies presence-aware incremental updates while SetOption
// replaces. DataZoom supports initial windows, a draggable slider and inside
// wheel/pan navigation. Declarative charts animate by default; pass
// animation:false for deterministic rendering. Large line/area/scatter data
// can use sampling/largeThreshold, and pie labelLayout.hideOverlap suppresses
// collisions. Progressive chunked rendering remains future work; Map/GEO
// charts are intentionally out of scope.
//
// Pixel-parity fixtures under testdata/parity compare this renderer with
// ECharts in offscreen Chrome Canvas using ImageMagick compare. Run the
// chartcompare command documented in testdata/parity/README.md.
package chart
