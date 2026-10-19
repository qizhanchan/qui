// Command graphs demonstrates the native, declarative qui/graphs/chart
// surface: line, area, bar, scatter and pie series driven by one Option.
package main

import (
	"log"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/graphs/chart"
)

func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	window, err := app.NewWindow("qui/graphs demo", 1024, 720)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	// --- Top: line + area combo -----------------------------------------
	showSymbols := true
	topChart := chart.New(chart.Option{
		Title: &chart.TitleOption{
			Text:    "Weekly Revenue",
			SubText: "Line = actuals, Area = forecast",
		},
		Legend:  &chart.LegendOption{Position: "bottom"},
		Tooltip: &chart.TooltipOption{Trigger: "axis"},
		Palette: []chart.CSSColor{
			"#4285f4", "#f28c42", "#4cbf66", "#f2b333",
		},
		EnableSelect: true,
		Series: []chart.SeriesOption{
			{
				Type: chart.SeriesArea,
				Name: "Forecast",
				Data: []chart.DataPoint{
					chart.XYPoint(1, 1100), chart.XYPoint(2, 1400),
					chart.XYPoint(3, 1000), chart.XYPoint(4, 1700),
					chart.XYPoint(5, 2000), chart.XYPoint(6, 1700),
					chart.XYPoint(7, 1500),
				},
				FillOpacity: 0.2,
			},
			{
				Type: chart.SeriesLine,
				Name: "Revenue",
				Data: []chart.DataPoint{
					chart.XYPoint(1, 1200), chart.XYPoint(2, 1350),
					chart.XYPoint(3, 900), chart.XYPoint(4, 1800),
					chart.XYPoint(5, 2100), chart.XYPoint(6, 1600),
					chart.XYPoint(7, 1400),
				},
				Smooth:     true,
				ShowSymbol: &showSymbols,
			},
		},
	})
	topChart.OnSelect = func(s chart.Series, i int) {
		log.Printf("selected %s[%d]", s.Name(), i)
	}

	// --- Bottom-left: bar chart (grouped) -------------------------------
	barChart := chart.New(chart.Option{
		Title:   &chart.TitleOption{Text: "Orders by Day"},
		Legend:  &chart.LegendOption{Position: "bottom"},
		Tooltip: &chart.TooltipOption{Trigger: "axis"},
		XAxis: chart.AxisOption{
			Type:         chart.AxisCategory,
			Data:         []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"},
			GroupPadding: 0.2,
		},
		YAxis: chart.AxisOption{Type: chart.AxisValue},
		Series: []chart.SeriesOption{
			{
				Type: chart.SeriesBar,
				Name: "Online",
				Data: chart.Numbers(45, 52, 38, 61, 72, 55, 48),
			},
			{
				Type: chart.SeriesBar,
				Name: "Retail",
				Data: chart.Numbers(22, 28, 20, 35, 40, 62, 58),
			},
		},
	})

	// --- Bottom-center: scatter ----------------------------------------
	scatterChart := chart.New(chart.Option{
		Title: &chart.TitleOption{Text: "Latency vs Throughput"},
		XAxis: chart.AxisOption{Type: chart.AxisValue},
		YAxis: chart.AxisOption{Type: chart.AxisValue},
		Series: []chart.SeriesOption{{
			Type: chart.SeriesScatter,
			Name: "req/s",
			Data: scatterData(),
		}},
	})

	// --- Bottom-right: pie ---------------------------------------------
	pieChart := chart.New(chart.Option{
		Title: &chart.TitleOption{Text: "Channel Split"},
		Series: []chart.SeriesOption{{
			Type: chart.SeriesPie,
			Name: "channels",
			Data: []chart.DataPoint{
				chart.NamedPoint("Mobile", 55),
				chart.NamedPoint("Web", 30),
				chart.NamedPoint("API", 10),
				chart.NamedPoint("Kiosk", 5),
			},
		}},
	})

	// Layout: top row holds the combo chart; bottom row is the three
	// smaller charts side-by-side.
	topChart.SetFlex(1)
	barChart.SetFlex(1)
	scatterChart.SetFlex(1)
	pieChart.SetFlex(1)

	bottomRow := qui.NewContainer(qui.FlexLayout{Direction: qui.Horizontal, Gap: 12},
		barChart, scatterChart, pieChart,
	)
	bottomRow.SetFlex(1)

	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical, Gap: 12},
		topChart, bottomRow,
	)
	root.Style().Padding = qui.Insets{Top: 12, Right: 12, Bottom: 12, Left: 12}
	root.Style().Background = qui.Color{R: 0.95, G: 0.95, B: 0.97, A: 1}

	window.SetRoot(root)
	app.Run()
}

// scatterData returns 40 synthetic (latency, rps) pairs. Kept deterministic
// so screenshots are reproducible across runs.
func scatterData() []chart.DataPoint {
	pts := make([]chart.DataPoint, 0, 40)
	for i := 0; i < 40; i++ {
		x := float64(i) * 3.5
		// Two overlapping clusters.
		y := float64(80) + float64(i%8)*7 + float64((i*17)%13)
		pts = append(pts, chart.XYPoint(x, y))
	}
	return pts
}
