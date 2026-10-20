package chart

import (
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
)

func TestChartAccessibilitySummarizesVisibleSeries(t *testing.T) {
	c := New(Option{
		Title:     &TitleOption{Text: "Revenue", SubText: "weekly"},
		XAxis:     AxisOption{Type: AxisCategory, Data: []string{"A", "B"}},
		YAxis:     AxisOption{Type: AxisValue},
		Animation: boolPointer(false),
		Series: []SeriesOption{
			{Type: SeriesLine, Name: "actual", Data: Numbers(2, 5)},
			{Type: SeriesBar, Name: "target", Data: Numbers(3, 4)},
		},
	})
	c.Layout(qui.Rect{W: 500, H: 300})
	if c.Role() != qui.RoleChart || c.AccessibleName() != "Revenue" {
		t.Fatalf("chart accessibility identity = %q/%q", c.Role(), c.AccessibleName())
	}
	if value := c.AccessibleValue(); value != "weekly; 2 series" {
		t.Fatalf("accessible value = %q", value)
	}
	children := c.AccessibleChildren()
	if len(children) != 2 {
		t.Fatalf("accessible children = %d, want 2", len(children))
	}
	if children[0].Name != "actual" || children[1].Name != "target" {
		t.Fatalf("series names = %q/%q", children[0].Name, children[1].Name)
	}
	if !strings.Contains(children[0].Value, "x 0..1") || !strings.Contains(children[0].Value, "y 2..5") {
		t.Fatalf("series value = %q", children[0].Value)
	}
	if children[0].Bounds.IsEmpty() || !children[0].Bounds.Contains(qui.Point{X: children[0].Bounds.X + 1, Y: children[0].Bounds.Y + 1}) {
		t.Fatalf("series bounds = %+v", children[0].Bounds)
	}

	c.Series[1].SetVisible(false)
	if value := c.AccessibleValue(); value != "weekly; 1 series" || len(c.AccessibleChildren()) != 1 {
		t.Fatalf("hidden series leaked into accessibility: %q %+v", value, c.AccessibleChildren())
	}
}
