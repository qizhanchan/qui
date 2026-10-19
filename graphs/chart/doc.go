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
// Option currently covers title, legend, item/axis tooltips, axes and grid
// gutters, palettes, continuous visual-map colors, line/area/bar/
// pictorial-bar/scatter/effect-scatter/pie/candlestick/radar/funnel/gauge/
// heatmap/boxplot series,
// and graph/tree/treemap/sunburst/sankey/parallel/theme-river layouts.
// MergeOption supplies ECharts-style incremental updates while SetOption
// replaces. Multi-grid/secondary axes, dataset transforms, dataZoom,
// rendered visualMap legends, animation and large-data progressive rendering
// are the next layers of the same native pipeline. Map/GEO charts are out of
// scope.
//
// Pixel-parity fixtures under testdata/parity compare this renderer with
// ECharts in offscreen Chrome Canvas using ImageMagick compare. Run the
// chartcompare command documented in testdata/parity/README.md.
package chart
