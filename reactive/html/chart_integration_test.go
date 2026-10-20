package html_test

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/graphs/chart"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
)

func TestChartRunsAsReactiveLeafAndUpdatesInPlace(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 520, H: 360})
	rt := h.Mount(win, `.wrap { width: 500px; height: 340px; } .chart { width: 480px; height: 260px; }`, func() h.Node {
		value, setValue := reactive.UseState(2)
		return h.Div(
			h.Button("increase").Class("inc").OnClick(func() { setValue(value + 1) }),
			h.Leaf("x-chart", "chart",
				func() *chart.Chart {
					return chart.New(chart.Option{
						Animation: boolPtr(false),
						XAxis:     chart.AxisOption{Type: chart.AxisCategory, Data: []string{"A", "B"}},
						Series:    []chart.SeriesOption{{Type: chart.SeriesBar, Name: "value", Data: chart.Numbers(float64(value))}},
					})
				},
				func(c *chart.Chart) reactive.Flags {
					c.MergeOption(chart.OptionPatch{Series: []chart.SeriesPatch{{
						Name: strPtr("value"),
						Data: &[]chart.DataPoint{chart.Number(float64(value))},
					}}})
					return reactive.FlagNone
				},
			).Class("chart"),
		).Class("wrap")
	})
	layoutAll(win, rt.Root())
	el := findClass(rt.Root(), "chart")
	if el == nil {
		t.Fatal("chart leaf not found")
	}
	c, ok := el.HostedWidget().(*chart.Chart)
	if !ok {
		t.Fatalf("hosted widget = %T", el.HostedWidget())
	}
	if got := c.Series[0].(*chart.BarSeries).Values; len(got) != 1 || got[0] != 2 {
		t.Fatalf("initial chart values = %v", got)
	}
	button := findClass(rt.Root(), "inc")
	x, y := center(button)
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, x, y, qui.MouseButtonLeft, 0))
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, x, y, qui.MouseButtonLeft, 0))
	layoutAll(win, rt.Root())
	if got := c.Series[0].(*chart.BarSeries).Values; len(got) != 1 || got[0] != 3 {
		t.Fatalf("updated chart values = %v", got)
	}
}

func boolPtr(value bool) *bool { return &value }

func strPtr(value string) *string { return &value }
