package chart

import (
	"strconv"
	"strings"

	. "github.com/qizhanchan/qui"
)

// tooltipState carries the inline-tooltip rendering info. When active
// is false, Chart.Draw skips the tooltip pass.
type tooltipState struct {
	active  bool
	text    string
	anchor  Point
	padding float32
}

// DefaultTooltipText renders the default "name: (x, y)" string for a
// series hit. Charts that need richer content assign a TooltipBuilder.
func DefaultTooltipText(s Series, index int) string {
	switch v := s.(type) {
	case *LineSeries:
		if index >= 0 && index < len(v.Points) {
			return s.Name() + ": " + formatPoint(v.Points[index])
		}
	case *ScatterSeries:
		if index >= 0 && index < len(v.Points) {
			return s.Name() + ": " + formatPoint(v.Points[index])
		}
	case *AreaSeries:
		if index >= 0 && index < len(v.Points) {
			return s.Name() + ": " + formatPoint(v.Points[index])
		}
	case *BarSeries:
		if index >= 0 && index < len(v.Values) {
			return s.Name() + ": " + formatFloat(v.Values[index])
		}
	case *PieSeries:
		if index >= 0 && index < len(v.Slices) {
			return v.Slices[index].Label + ": " + formatFloat(v.Slices[index].Value)
		}
	}
	return s.Name()
}

// TooltipEntry is one series value in an axis-trigger tooltip.
type TooltipEntry struct {
	Name  string
	Value float64
	Color Color
}

// DefaultAxisTooltipText aggregates all cartesian series at one data index.
func DefaultAxisTooltipText(c *Chart, index int) string {
	if c == nil || index < 0 {
		return ""
	}
	label := ""
	if ca, ok := c.XAxis.(*CategoryAxis); ok && index < len(ca.Categories) {
		label = ca.Categories[index]
	}
	lines := make([]string, 0, len(c.Series)+1)
	if label != "" {
		lines = append(lines, label)
	}
	for _, series := range c.Series {
		if !series.Visible() {
			continue
		}
		if value, ok := seriesValueAt(series, index); ok {
			lines = append(lines, series.Name()+": "+formatFloat(value))
		}
	}
	return strings.Join(lines, "\n")
}

func seriesValueAt(series Series, index int) (float32, bool) {
	if index < 0 {
		return 0, false
	}
	switch s := series.(type) {
	case *LineSeries:
		if index < len(s.Points) {
			return s.Points[index].Y, true
		}
	case *AreaSeries:
		if index < len(s.Points) {
			return s.Points[index].Y, true
		}
	case *BarSeries:
		if index < len(s.Values) {
			return s.Values[index], true
		}
	case *ScatterSeries:
		if index < len(s.Points) {
			return s.Points[index].Y, true
		}
	}
	return 0, false
}

// drawTooltipInline paints a rounded-rect tooltip at state.anchor using
// the theme's fonts and colors. Kept in Chart package so the tooltip
// styling matches chart theming without a separate theme plumbing.
func drawTooltipInline(canvas Canvas, state tooltipState, plot Rect, theme *Theme, family string) {
	if !state.active || state.text == "" {
		return
	}
	font := Font{Family: family, Size: theme.FontSmall}
	if font.Size <= 0 {
		font.Size = 12
	}
	lines := strings.Split(state.text, "\n")
	lineH := font.Size + 2
	var w float32
	for _, line := range lines {
		lineW, _ := TextMetrics(line, font)
		if lineW > w {
			w = lineW
		}
	}
	h := float32(len(lines)) * lineH
	pad := state.padding
	if pad <= 0 {
		pad = 6
	}
	box := Rect{
		X: state.anchor.X + 10,
		Y: state.anchor.Y - h - pad*2 - 4,
		W: w + pad*2,
		H: h + pad*2,
	}
	// Keep tooltip inside the plot rect when possible — flip horizontally
	// if it would run off the right edge.
	if box.X+box.W > plot.X+plot.W {
		box.X = state.anchor.X - box.W - 10
	}
	if box.Y < plot.Y {
		box.Y = state.anchor.Y + 12
	}
	bg := theme.Surface
	bg.A = 0.95
	canvas.FillRoundedRect(box, theme.RadiusSmall, bg)
	canvas.StrokeRect(box, theme.TextMuted, 1)
	for i, line := range lines {
		canvas.DrawText(line,
			Rect{X: box.X + pad, Y: box.Y + pad + float32(i)*lineH, W: w, H: lineH},
			theme.Text, font)
	}
}

func formatPoint(p Point2D) string {
	return "(" + formatFloat(p.X) + ", " + formatFloat(p.Y) + ")"
}

// formatFloat renders a float32 compactly, dropping trailing zeros.
func formatFloat(v float32) string {
	s := strconv.FormatFloat(float64(v), 'f', 2, 32)
	if !strings.Contains(s, ".") {
		return s
	}
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}
