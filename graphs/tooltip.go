package graphs

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

// drawTooltipInline paints a rounded-rect tooltip at state.anchor using
// the theme's fonts and colors. Kept in Chart package so the tooltip
// styling matches chart theming without a separate theme plumbing.
func drawTooltipInline(canvas Canvas, state tooltipState, plot Rect, theme *Theme) {
	if !state.active || state.text == "" {
		return
	}
	font := Font{Size: theme.FontSmall}
	if font.Size <= 0 {
		font.Size = 12
	}
	w, h := TextMetrics(state.text, font)
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
	canvas.DrawText(state.text,
		Rect{X: box.X + pad, Y: box.Y + pad, W: w, H: h},
		theme.Text, font)
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
