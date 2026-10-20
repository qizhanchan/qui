package chart

import (
	"fmt"
	"strings"

	"github.com/qizhanchan/qui"
)

// Accessibility hooks for Chart. The root exposes a compact chart summary;
// AccessibleChildren publishes each visible series and its data-space extent
// so selectors and agents can inspect the data without inferring pixels.

func (c *Chart) Role() string { return qui.RoleChart }

func (c *Chart) AccessibleName() string {
	if c == nil {
		return ""
	}
	return c.Title
}

func (c *Chart) AccessibleValue() string {
	if c == nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if c.SubTitle != "" {
		parts = append(parts, c.SubTitle)
	}
	visible := 0
	for _, series := range c.Series {
		if series != nil && series.Visible() {
			visible++
		}
	}
	if visible > 0 {
		parts = append(parts, fmt.Sprintf("%d series", visible))
	}
	return strings.Join(parts, "; ")
}

// AccessibleChildren exposes one virtual list item per visible series. The
// value is the data-space extent rather than the rendered pixel geometry,
// which keeps the summary meaningful across zoom, themes and chart sizes.
func (c *Chart) AccessibleChildren() []qui.AXChild {
	if c == nil || len(c.Series) == 0 {
		return nil
	}
	out := make([]qui.AXChild, 0, len(c.Series))
	for i, series := range c.Series {
		if series == nil || !series.Visible() {
			continue
		}
		name := series.Name()
		if name == "" {
			name = fmt.Sprintf("Series %d", i+1)
		}
		bounds, _, _ := c.seriesAxesAt(i)
		if bounds.IsEmpty() {
			bounds = c.Bounds()
		}
		out = append(out, qui.AXChild{
			Role:   qui.RoleListitem,
			Name:   name,
			Value:  describeSeriesBounds(series.DataBounds()),
			Bounds: bounds,
		})
	}
	return out
}

func describeSeriesBounds(bounds SeriesBounds) string {
	if !bounds.HasData {
		return "no data"
	}
	return fmt.Sprintf("x %g..%g, y %g..%g", bounds.Xmin, bounds.Xmax, bounds.Ymin, bounds.Ymax)
}
