package main

import (
	"math"

	"github.com/qizhanchan/qui/graphs/chart"
)

var galleryPalette = []chart.CSSColor{
	"#5470c6", "#91cc75", "#fac858", "#ee6666",
	"#73c0de", "#3ba272", "#fc8452", "#9a60b4",
}

func baseOption(title, subtitle string) chart.Option {
	return chart.Option{
		Title:           &chart.TitleOption{Text: title, SubText: subtitle, Left: 10, Top: 10},
		TextStyle:       &chart.TextStyleOption{Color: "#263238"},
		BackgroundColor: "#ffffff",
		Legend:          &chart.LegendOption{Position: "bottom"},
		Tooltip:         &chart.TooltipOption{Trigger: "axis"},
		Grid:            &chart.GridOption{Left: 64, Right: 30, Top: 70, Bottom: 68},
		Palette:         galleryPalette,
	}
}

func galleryEntries() []galleryEntry {
	return []galleryEntry{
		{
			category: "Cartesian", name: "Line + Area",
			description: "Smooth line and translucent area share one category axis. Hover shows the aggregated axis tooltip; clicking a point fires selection.",
			features:    "hover, axis tooltip, selection, animation, legend",
			build:       lineAreaDemo,
		},
		{
			category: "Cartesian", name: "Bars",
			description: "Grouped positive and negative bars with shared zero baseline, rounded marks and a stacked-friendly model.",
			features:    "grouped bars, negative values, axis tooltip, legend",
			build:       barsDemo,
		},
		{
			category: "Data", name: "Dataset + Transform",
			description: "Rows are supplied once through dataset, mapped with dimensions/encode, then filtered and sorted by declarative transforms.",
			features:    "dataset, encode, filter transform, sort transform",
			build:       datasetDemo,
		},
		{
			category: "Layout", name: "Multi Grid",
			description: "Two independent grids with their own X/Y axes. Each series is routed by gridIndex, xAxisIndex and yAxisIndex.",
			features:    "multiple grids, independent axes, series routing",
			build:       multiGridDemo,
		},
		{
			category: "Interaction", name: "DataZoom",
			description: "A 240-point series with a draggable range slider and inside navigation. The axis crosshair follows axis-trigger tooltips.",
			features:    "slider brush, wheel zoom, drag pan, axisPointer",
			build:       dataZoomDemo,
		},
		{
			category: "Visual", name: "Heatmap",
			description: "Category-by-category values mapped through a continuous visualMap ramp, with a rendered gradient legend.",
			features:    "heatmap, visualMap colors, gradient legend, cell hit test",
			build:       heatmapDemo,
		},
		{
			category: "Radial", name: "Pie",
			description: "Donut slices with outside labels. labelLayout.hideOverlap keeps dense label sets readable.",
			features:    "donut radius, leader lines, hideOverlap, slice hit test",
			build:       pieDemo,
		},
		{
			category: "Large data", name: "Sampled Scatter",
			description: "Five thousand scatter points rendered through LTTB sampling. Raw data remains available for bounds and tooltip hit testing.",
			features:    "LTTB sampling, large threshold, scatter, tooltip",
			build:       sampledScatterDemo,
		},
		{
			category: "Cartesian", name: "Pictorial + Effect",
			description: "Repeated pictorial symbols and animated-looking ripples share a category axis while using different mark styles.",
			features:    "pictorialBar, effectScatter, symbol repeat, clipping",
			build:       pictorialEffectDemo,
		},
		{
			category: "Financial", name: "Candlestick + Boxplot",
			description: "Two analytical marks on separate grids: OHLC candles above and five-number boxplots below.",
			features:    "candlestick, boxplot, multiple grids, multi-series routing",
			build:       financialDemo,
		},
		{
			category: "Relational", name: "Radar",
			description: "Two profiles normalized across five radar indicators, with filled areas and per-series legend colors.",
			features:    "radar indicators, filled polygons, legend",
			build:       radarDemo,
		},
		{
			category: "Radial", name: "Gauge",
			description: "A 270-degree gauge with major/minor ticks, numeric labels, tapered pointer and center detail.",
			features:    "gauge geometry, ticks, pointer, center labels",
			build:       gaugeDemo,
		},
		{
			category: "Radial", name: "Funnel",
			description: "Sorted funnel stages with outside labels and leader lines. Item colors cycle through the chart palette.",
			features:    "funnel sort, labels, leader lines, item colors",
			build:       funnelDemo,
		},
		{
			category: "Relational", name: "Graph",
			description: "Circular network layout with endpoint names or indexes and palette-colored nodes.",
			features:    "graph layout, links, node colors, endpoint mapping",
			build:       graphDemo,
		},
		{
			category: "Hierarchy", name: "Tree",
			description: "Nested data lowered into a horizontal tree with orthogonal links and labels at every node.",
			features:    "tree hierarchy, nested data, link routing, labels",
			build:       treeDemo,
		},
		{
			category: "Hierarchy", name: "Treemap",
			description: "Squarified rectangles sized by value, with palette coloring and contained labels.",
			features:    "squarified layout, nested values, cell hit test",
			build:       treemapDemo,
		},
		{
			category: "Hierarchy", name: "Sunburst",
			description: "Nested hierarchy laid out as concentric rings with angular weights and sector hit testing.",
			features:    "sunburst rings, hierarchy, sector hit test",
			build:       sunburstDemo,
		},
		{
			category: "Flow", name: "Sankey",
			description: "Weighted links across depth columns with collision-resolved nodes and cubic flow ribbons.",
			features:    "sankey layout, weighted links, ribbons, labels",
			build:       sankeyDemo,
		},
		{
			category: "Relational", name: "Parallel",
			description: "Multi-dimensional profiles connected across four parallel axes, each with independent nice-number scaling.",
			features:    "parallel axes, dimensions, profile hit test",
			build:       parallelDemo,
		},
		{
			category: "Flow", name: "Theme River",
			description: "A stacked streamgraph with a symmetric baseline and per-layer colors across time.",
			features:    "themeRiver, streamgraph baseline, layers, labels",
			build:       themeRiverDemo,
		},
	}
}

func lineAreaDemo() *chart.Chart {
	symbols := false
	option := baseOption("Revenue Forecast", "smooth line + translucent area")
	option.Tooltip.Trigger = "axis"
	option.EnableSelect = true
	option.Series = []chart.SeriesOption{
		{Type: chart.SeriesArea, Name: "Forecast", FillOpacity: .22, Data: chart.Numbers(980, 1120, 1080, 1450, 1730, 1660, 1520, 1600)},
		{Type: chart.SeriesLine, Name: "Actual", Smooth: true, ShowSymbol: &symbols, Data: chart.Numbers(1020, 1180, 930, 1510, 1830, 1580, 1470, 1690)},
	}
	option.XAxis = chart.AxisOption{Type: chart.AxisCategory, Data: []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun", "Mon+"}}
	option.YAxis = chart.AxisOption{Type: chart.AxisValue}
	return chart.New(option)
}

func barsDemo() *chart.Chart {
	option := baseOption("Cash Flow", "grouped bars · positive and negative")
	option.Tooltip.Trigger = "axis"
	option.XAxis = chart.AxisOption{Type: chart.AxisCategory, Data: []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun"}}
	option.YAxis = chart.AxisOption{Type: chart.AxisValue}
	option.Series = []chart.SeriesOption{
		{Type: chart.SeriesBar, Name: "Revenue", Data: chart.Numbers(20, 32, 18, 44, 30, 38)},
		{Type: chart.SeriesBar, Name: "Cost", Data: chart.Numbers(-12, -18, -10, -24, -16, -20)},
	}
	return chart.New(option)
}

func datasetDemo() *chart.Chart {
	option := baseOption("Dataset Pipeline", "source → filter → sort → encode")
	option.Tooltip.Trigger = "axis"
	option.XAxis = chart.AxisOption{Type: chart.AxisValue}
	option.YAxis = chart.AxisOption{Type: chart.AxisValue}
	option.Dataset = chart.DatasetList{{
		Dimensions: []string{"day", "dayIndex", "online", "retail"},
		Source: []chart.DatasetRow{
			{Values: []any{"Mon", 0.0, 45.0, 22.0}},
			{Values: []any{"Tue", 1.0, 52.0, 28.0}},
			{Values: []any{"Wed", 2.0, 28.0, 20.0}},
			{Values: []any{"Thu", 3.0, 61.0, 35.0}},
			{Values: []any{"Fri", 4.0, 72.0, 40.0}},
			{Values: []any{"Sat", 5.0, 55.0, 62.0}},
			{Values: []any{"Sun", 6.0, 48.0, 58.0}},
		},
		Transforms: []chart.DatasetTransformSpec{
			{Type: "filter", Config: chart.DatasetTransformConfig{Dimension: "online", GTE: float64Ptr(40)}},
			{Type: "sort", Config: chart.DatasetTransformConfig{Dimension: "dayIndex", Order: "asc"}},
		},
	}}
	option.Series = []chart.SeriesOption{
		{Type: chart.SeriesLine, Name: "Online", Smooth: true, Encode: &chart.EncodeOption{X: "dayIndex", Y: "online"}},
		{Type: chart.SeriesBar, Name: "Retail", Encode: &chart.EncodeOption{X: "dayIndex", Y: "retail"}},
	}
	return chart.New(option)
}

func multiGridDemo() *chart.Chart {
	option := baseOption("Two Coordinate Systems", "gridIndex · xAxisIndex · yAxisIndex")
	option.Legend = nil
	option.Tooltip.Trigger = "axis"
	option.Grids = []chart.GridOption{
		{Left: 64, Right: 35, Top: 70, Bottom: 300},
		{Left: 64, Right: 35, Top: 335, Bottom: 65},
	}
	option.XAxes = []chart.AxisOption{
		{Type: chart.AxisCategory, Data: []string{"A", "B", "C", "D", "E"}},
		{Type: chart.AxisCategory, Data: []string{"A", "B", "C", "D", "E"}},
	}
	option.YAxes = []chart.AxisOption{
		{Type: chart.AxisValue},
		{Type: chart.AxisValue},
	}
	option.Series = []chart.SeriesOption{
		{Type: chart.SeriesLine, Name: "latency", GridIndex: 0, XAxisIndex: 0, YAxisIndex: 0, Smooth: true, Data: chart.Numbers(12, 18, 16, 24, 21)},
		{Type: chart.SeriesBar, Name: "volume", GridIndex: 1, XAxisIndex: 1, YAxisIndex: 1, Data: chart.Numbers(22, 34, 28, 41, 37)},
	}
	return chart.New(option)
}

func dataZoomDemo() *chart.Chart {
	points := make([]chart.DataPoint, 240)
	for i := range points {
		x := float64(i)
		y := 52 + 22*math.Sin(float64(i)*.11) + 9*math.Sin(float64(i)*.37)
		points[i] = chart.XYPoint(x, y)
	}
	option := baseOption("Zoomable Time Series", "drag the slider · wheel inside the plot")
	option.Tooltip.Trigger = "axis"
	option.Legend = nil
	option.AxisPointer = &chart.AxisPointerOption{Type: "line"}
	option.XAxis = chart.AxisOption{Type: chart.AxisValue, Name: "samples"}
	option.YAxis = chart.AxisOption{Type: chart.AxisValue}
	option.DataZoom = []chart.DataZoomOption{{Start: float64Ptr(18), End: float64Ptr(72)}}
	option.Series = []chart.SeriesOption{{Type: chart.SeriesLine, Name: "load", Smooth: true, Data: points}}
	return chart.New(option)
}

func heatmapDemo() *chart.Chart {
	option := baseOption("Weekly Activity", "continuous visualMap · gradient legend")
	option.XAxis = chart.AxisOption{Type: chart.AxisCategory, Data: []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}}
	option.YAxis = chart.AxisOption{Type: chart.AxisCategory, Data: []string{"00", "06", "12", "18"}}
	option.VisualMap = &chart.VisualMapOption{
		Min: float64Ptr(0), Max: float64Ptr(100),
		InRange: &chart.VisualMapInRange{Color: []chart.CSSColor{"#dbeafe", "#60a5fa", "#2563eb", "#7c3aed"}},
	}
	rows := make([]chart.DataPoint, 0, 28)
	for y := 0; y < 4; y++ {
		for x := 0; x < 7; x++ {
			value := float64((x*17 + y*23 + x*y*11) % 100)
			rows = append(rows, chart.DataPoint{Values: []float64{float64(x), float64(y), value}})
		}
	}
	option.Series = []chart.SeriesOption{{Type: chart.SeriesHeatmap, Name: "activity", Data: rows}}
	return chart.New(option)
}

func pieDemo() *chart.Chart {
	hideOverlap := true
	option := baseOption("Channel Mix", "outside labels · collision suppression")
	option.Grid = nil
	option.Legend = &chart.LegendOption{Position: "bottom"}
	option.Tooltip.Trigger = "item"
	option.Series = []chart.SeriesOption{{
		Type: chart.SeriesPie, Name: "channels", Radius: "65%",
		LabelLayout: &chart.LabelLayoutOption{HideOverlap: &hideOverlap},
		Data: []chart.DataPoint{
			chart.NamedPoint("Mobile", 31), chart.NamedPoint("Web", 24), chart.NamedPoint("API", 18),
			chart.NamedPoint("Partner", 11), chart.NamedPoint("Kiosk", 7), chart.NamedPoint("Other", 5),
			chart.NamedPoint("Legacy", 3), chart.NamedPoint("Internal", 1),
		},
	}}
	return chart.New(option)
}

func sampledScatterDemo() *chart.Chart {
	points := make([]chart.DataPoint, 5000)
	for i := range points {
		x := float64(i) * .02
		y := 50 + 24*math.Sin(x*.67) + 10*math.Sin(x*2.7) + float64(i%17)*.25
		points[i] = chart.XYPoint(x, y)
	}
	option := baseOption("Large Scatter", "5,000 points · LTTB sampling")
	option.Tooltip.Trigger = "item"
	option.XAxis = chart.AxisOption{Type: chart.AxisValue, Name: "x"}
	option.YAxis = chart.AxisOption{Type: chart.AxisValue, Name: "y"}
	option.Series = []chart.SeriesOption{{
		Type: chart.SeriesScatter, Name: "sample", SymbolSize: 4,
		Sampling: "lttb", LargeThreshold: 2000, Data: points,
	}}
	option.EnableSelect = true
	return chart.New(option)
}

func pictorialEffectDemo() *chart.Chart {
	option := baseOption("Pictorial + Effects", "symbol repeat and ripple marks")
	option.Tooltip.Trigger = "axis"
	option.XAxis = chart.AxisOption{Type: chart.AxisCategory, Data: []string{"A", "B", "C", "D", "E"}}
	option.YAxis = chart.AxisOption{Type: chart.AxisValue}
	option.Series = []chart.SeriesOption{
		{
			Type: chart.SeriesPictorialBar, Name: "units", Symbol: "roundRect",
			SymbolSize: 22, SymbolRepeat: boolPtr(true), SymbolMargin: 3, SymbolClip: boolPtr(true),
			Data: chart.Numbers(8, 12, 16, 10, 14),
		},
		{
			Type: chart.SeriesEffectScatter, Name: "events", SymbolSize: 8,
			Data: []chart.DataPoint{chart.XYPoint(0, 8), chart.XYPoint(1, 12), chart.XYPoint(2, 16), chart.XYPoint(3, 10), chart.XYPoint(4, 14)},
		},
	}
	return chart.New(option)
}

func financialDemo() *chart.Chart {
	candles := []chart.DataPoint{
		{Values: []float64{20, 34, 10, 38}}, {Values: []float64{40, 35, 30, 50}},
		{Values: []float64{31, 38, 28, 44}}, {Values: []float64{38, 15, 5, 42}},
		{Values: []float64{18, 28, 14, 32}}, {Values: []float64{28, 24, 16, 30}},
		{Values: []float64{24, 30, 20, 36}},
	}
	boxes := []chart.DataPoint{
		{Values: []float64{10, 25, 40, 60, 78}}, {Values: []float64{18, 32, 48, 68, 88}},
		{Values: []float64{5, 22, 38, 55, 72}}, {Values: []float64{22, 40, 58, 76, 95}},
		{Values: []float64{12, 30, 50, 64, 82}}, {Values: []float64{8, 24, 42, 59, 80}},
		{Values: []float64{20, 36, 54, 70, 90}},
	}
	option := baseOption("Market Analytics", "OHLC candles + distribution boxes")
	option.Legend = nil
	option.Grids = []chart.GridOption{
		{Left: 64, Right: 35, Top: 70, Bottom: 300},
		{Left: 64, Right: 35, Top: 335, Bottom: 62},
	}
	categories := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	option.XAxes = []chart.AxisOption{
		{Type: chart.AxisCategory, Data: categories},
		{Type: chart.AxisCategory, Data: categories},
	}
	option.YAxes = []chart.AxisOption{
		{Type: chart.AxisValue, Scale: boolPtr(true)},
		{Type: chart.AxisValue, Min: float64Ptr(0), Max: float64Ptr(100)},
	}
	option.Series = []chart.SeriesOption{
		{Type: chart.SeriesCandlestick, Name: "AAPL", GridIndex: 0, XAxisIndex: 0, YAxisIndex: 0, Data: candles},
		{Type: chart.SeriesBoxplot, Name: "Score", GridIndex: 1, XAxisIndex: 1, YAxisIndex: 1, Data: boxes},
	}
	return chart.New(option)
}

func radarDemo() *chart.Chart {
	option := baseOption("Team Profiles", "five normalized indicators")
	option.Tooltip.Trigger = "item"
	option.Radar = &chart.RadarOption{Indicators: []chart.RadarIndicator{
		{Name: "Sales", Max: 100}, {Name: "Support", Max: 100}, {Name: "Tech", Max: 100},
		{Name: "Market", Max: 100}, {Name: "Admin", Max: 100},
	}}
	option.Series = []chart.SeriesOption{
		{Type: chart.SeriesRadar, Name: "Team A", FillOpacity: .18, Data: []chart.DataPoint{{Values: []float64{80, 90, 70, 85, 75}}}},
		{Type: chart.SeriesRadar, Name: "Team B", FillOpacity: .12, Data: []chart.DataPoint{{Values: []float64{60, 75, 95, 55, 90}}}},
	}
	return chart.New(option)
}

func gaugeDemo() *chart.Chart {
	option := baseOption("System Load", "270° gauge · 72%")
	option.Legend = nil
	option.Tooltip.Trigger = "item"
	option.Series = []chart.SeriesOption{{
		Type: chart.SeriesGauge, Name: "Load", Data: []chart.DataPoint{{Name: "Load", Y: 72}},
	}}
	return chart.New(option)
}

func funnelDemo() *chart.Chart {
	option := baseOption("Sales Funnel", "stage conversion")
	option.Legend = nil
	option.Tooltip.Trigger = "item"
	option.Series = []chart.SeriesOption{{
		Type: chart.SeriesFunnel, Name: "Funnel", Data: []chart.DataPoint{
			chart.NamedPoint("Visitors", 100), chart.NamedPoint("Trials", 72),
			chart.NamedPoint("Qualified", 48), chart.NamedPoint("Proposal", 26), chart.NamedPoint("Won", 12),
		},
	}}
	return chart.New(option)
}

func graphDemo() *chart.Chart {
	option := baseOption("Service Network", "circular graph layout")
	option.Tooltip.Trigger = "item"
	option.Series = []chart.SeriesOption{{
		Type: chart.SeriesGraph, Name: "services", Layout: "circular",
		Data: []chart.DataPoint{
			{Name: "API"}, {Name: "Auth"}, {Name: "Users"}, {Name: "Orders"}, {Name: "Search"}, {Name: "Billing"}, {Name: "Events"}, {Name: "Storage"},
		},
		Links: []chart.GraphLinkOption{
			{Source: "API", Target: "Auth"}, {Source: "API", Target: "Users"}, {Source: "API", Target: "Orders"},
			{Source: "Users", Target: "Search"}, {Source: "Orders", Target: "Billing"}, {Source: "Orders", Target: "Events"},
			{Source: "Events", Target: "Storage"}, {Source: "Auth", Target: "Storage"}, {Source: "Billing", Target: "Events"},
		},
	}}
	return chart.New(option)
}

func treeDemo() *chart.Chart {
	option := baseOption("Product Hierarchy", "nested tree")
	option.Legend = nil
	option.Tooltip.Trigger = "item"
	option.Series = []chart.SeriesOption{{
		Type: chart.SeriesTree, Name: "tree", Data: []chart.DataPoint{{
			Name: "root", Children: []chart.DataPoint{
				{Name: "Hardware", Children: []chart.DataPoint{{Name: "Sensors"}, {Name: "Controllers"}, {Name: "Gateways"}}},
				{Name: "Software", Children: []chart.DataPoint{{Name: "Firmware"}, {Name: "Cloud"}, {Name: "Mobile"}}},
				{Name: "Services", Children: []chart.DataPoint{{Name: "Install"}, {Name: "Support"}}},
			},
		}},
	}}
	return chart.New(option)
}

func treemapDemo() *chart.Chart {
	option := baseOption("Storage Footprint", "squarified treemap")
	option.Legend = nil
	option.Tooltip.Trigger = "item"
	option.Series = []chart.SeriesOption{{
		Type: chart.SeriesTreemap, Name: "storage", Data: []chart.DataPoint{
			chart.NamedPoint("Images", 42), chart.NamedPoint("Video", 28), chart.NamedPoint("Logs", 16),
			chart.NamedPoint("Database", 9), chart.NamedPoint("Other", 5),
		},
	}}
	return chart.New(option)
}

func sunburstDemo() *chart.Chart {
	option := baseOption("Subscription Mix", "nested sunburst")
	option.Legend = nil
	option.Tooltip.Trigger = "item"
	option.Series = []chart.SeriesOption{{
		Type: chart.SeriesSunburst, Name: "plans", Data: []chart.DataPoint{{
			Name: "All", Children: []chart.DataPoint{
				{Name: "Free", Y: 45, Children: []chart.DataPoint{{Name: "US", Y: 25}, {Name: "EU", Y: 20}}},
				{Name: "Pro", Y: 32, Children: []chart.DataPoint{{Name: "US", Y: 18}, {Name: "EU", Y: 14}}},
				{Name: "Team", Y: 16, Children: []chart.DataPoint{{Name: "US", Y: 9}, {Name: "EU", Y: 7}}},
				{Name: "Enterprise", Y: 7},
			},
		}},
	}}
	return chart.New(option)
}

func sankeyDemo() *chart.Chart {
	option := baseOption("Request Flow", "weighted sankey")
	option.Tooltip.Trigger = "item"
	option.Series = []chart.SeriesOption{{
		Type: chart.SeriesSankey, Name: "flow",
		Data: []chart.DataPoint{{Name: "CDN"}, {Name: "Gateway"}, {Name: "Auth"}, {Name: "Search"}, {Name: "Orders"}, {Name: "Storage"}},
		Links: []chart.GraphLinkOption{
			{Source: "CDN", Target: "Gateway", Value: 70},
			{Source: "Gateway", Target: "Auth", Value: 24},
			{Source: "Gateway", Target: "Search", Value: 28},
			{Source: "Gateway", Target: "Orders", Value: 18},
			{Source: "Auth", Target: "Storage", Value: 20},
			{Source: "Search", Target: "Storage", Value: 25},
			{Source: "Orders", Target: "Storage", Value: 15},
		},
	}}
	return chart.New(option)
}

func parallelDemo() *chart.Chart {
	option := baseOption("Vehicle Profiles", "four parallel dimensions")
	option.Legend = nil
	option.Tooltip.Trigger = "item"
	option.ParallelAxis = []chart.AxisOption{
		{Name: "Price", Min: float64Ptr(0), Max: float64Ptr(100)},
		{Name: "Power", Min: float64Ptr(0), Max: float64Ptr(100)},
		{Name: "Speed", Min: float64Ptr(0), Max: float64Ptr(100)},
		{Name: "Range", Min: float64Ptr(0), Max: float64Ptr(100)},
	}
	option.Series = []chart.SeriesOption{{
		Type: chart.SeriesParallel, Name: "cars", Data: []chart.DataPoint{
			{Values: []float64{20, 35, 60, 45}}, {Values: []float64{50, 70, 40, 80}},
			{Values: []float64{80, 55, 75, 30}}, {Values: []float64{36, 64, 82, 58}},
		},
	}}
	return chart.New(option)
}

func themeRiverDemo() *chart.Chart {
	option := baseOption("Composition Over Time", "theme river")
	option.Legend = &chart.LegendOption{Position: "bottom"}
	option.Tooltip.Trigger = "item"
	option.SingleAxis = chart.AxisOption{Type: chart.AxisValue, Min: float64Ptr(0), Max: float64Ptr(6), SplitNumber: 6}
	data := make([]chart.DataPoint, 0, 28)
	layers := []struct {
		name string
		vals []float64
	}{
		{"API", []float64{10, 15, 20, 18, 25, 20, 15}},
		{"Web", []float64{20, 18, 25, 30, 24, 28, 32}},
		{"Mobile", []float64{15, 20, 18, 22, 20, 15, 18}},
		{"Partner", []float64{8, 12, 10, 15, 18, 22, 20}},
	}
	for _, layer := range layers {
		for i, value := range layer.vals {
			data = append(data, chart.DataPoint{Name: layer.name, Values: []float64{float64(i), value}})
		}
	}
	option.Series = []chart.SeriesOption{{Type: chart.SeriesThemeRiver, Name: "traffic", Data: data}}
	return chart.New(option)
}

func float64Ptr(v float64) *float64 { return &v }
func boolPtr(v bool) *bool          { return &v }
