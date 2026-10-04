// Command graphs demonstrates the qui/graphs v1 charting surface:
// Line, Area, Bar grouped/stacked, Scatter, Pie + Legend + Tooltip.
package main

import (
	"log"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/graphs"
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
	topChart := graphs.NewChart()
	topChart.Title = "Weekly Revenue"
	topChart.SubTitle = "Line = actuals, Area = forecast"
	revenue := graphs.NewLineSeries("Revenue", []graphs.Point2D{
		{X: 1, Y: 1200}, {X: 2, Y: 1350}, {X: 3, Y: 900},
		{X: 4, Y: 1800}, {X: 5, Y: 2100}, {X: 6, Y: 1600},
		{X: 7, Y: 1400},
	})
	revenue.ShowMarkers = true
	revenue.Smooth = true
	forecast := graphs.NewAreaSeries("Forecast", []graphs.Point2D{
		{X: 1, Y: 1100}, {X: 2, Y: 1400}, {X: 3, Y: 1000},
		{X: 4, Y: 1700}, {X: 5, Y: 2000}, {X: 6, Y: 1700},
		{X: 7, Y: 1500},
	})
	forecast.FillAlpha = 0.2
	topChart.AddSeries(forecast)
	topChart.AddSeries(revenue)
	topChart.SetLegend(graphs.NewLegend(graphs.LegendBottom))
	// topChart.EnableZoom = true
	// topChart.EnablePan = true
	// topChart.EnableSelect = true
	topChart.OnSelect = func(s graphs.Series, i int) {
		log.Printf("selected %s[%d]", s.Name(), i)
	}

	// --- Bottom-left: bar chart (grouped) -------------------------------
	barChart := graphs.NewChart()
	barChart.Title = "Orders by Day"
	barChart.XAxis = &graphs.CategoryAxis{
		Categories:   []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"},
		GroupPadding: 0.2,
	}
	online := graphs.NewBarSeries("Online", []float32{45, 52, 38, 61, 72, 55, 48})
	retail := graphs.NewBarSeries("Retail", []float32{22, 28, 20, 35, 40, 62, 58})
	barChart.AddSeries(online)
	barChart.AddSeries(retail)
	barChart.SetLegend(graphs.NewLegend(graphs.LegendBottom))

	// --- Bottom-center: scatter ----------------------------------------
	scatterChart := graphs.NewChart()
	scatterChart.Title = "Latency vs Throughput"
	scatter := graphs.NewScatterSeries("req/s", scatterData())
	scatterChart.AddSeries(scatter)

	// --- Bottom-right: pie ---------------------------------------------
	pieChart := graphs.NewChart()
	pieChart.Title = "Channel Split"
	pie := graphs.NewPieSeries("channels", []graphs.PieSlice{
		{Label: "Mobile", Value: 55},
		{Label: "Web", Value: 30},
		{Label: "API", Value: 10},
		{Label: "Kiosk", Value: 5},
	})
	pieChart.AddSeries(pie)

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
func scatterData() []graphs.Point2D {
	pts := make([]graphs.Point2D, 0, 40)
	for i := 0; i < 40; i++ {
		x := float32(i) * 3.5
		// Two overlapping clusters.
		y := float32(80) + float32(i%8)*7 + float32((i*17)%13)
		pts = append(pts, graphs.Point2D{X: x, Y: y})
	}
	return pts
}
