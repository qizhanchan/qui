package graphs

import (
	. "github.com/qizhanchan/qui"
)

// PlotGutters is the amount of space reserved outside the plot
// rectangle for axes, tick labels, and the axis title. Derived from
// the theme font sizes + a tick-label character budget.
type PlotGutters struct {
	Left, Right, Top, Bottom float32
}

// defaultGutters returns sensible fixed gutters for v1. We assume:
//   - left gutter holds Y axis ticks (6 chars × ~7px + tick length)
//   - bottom gutter holds X axis ticks (one line of label font)
//   - right + top are small pads so rightmost X label and top marker
//     can extend slightly without overlapping the chart boundary.
func defaultGutters(theme *Theme) PlotGutters {
	labelSize := theme.FontSmall
	if labelSize <= 0 {
		labelSize = 12
	}
	charWidth := labelSize * 0.55
	return PlotGutters{
		Left:   charWidth*6 + 12, // 6 chars + tick marks + padding
		Right:  charWidth * 2,
		Top:    labelSize,
		Bottom: labelSize*2 + 6, // one line tick label + padding
	}
}

// drawGrid paints faint horizontal + vertical lines at each major tick.
// Skips category axes because their "ticks" are bucket centers and
// gridlines at those positions visually collide with bar series.
func drawGrid(canvas Canvas, plot Rect, x, y Axis, theme *Theme) {
	gridColor := theme.TextMuted
	gridColor.A *= 0.35 // subdued; ticks shouldn't compete with data
	if !x.IsCategorical() {
		for _, t := range x.TickPositions() {
			px := plot.X + x.ValueToPixel(t.Value, plot.W)
			canvas.DrawLine(
				Point{X: px, Y: plot.Y},
				Point{X: px, Y: plot.Y + plot.H},
				gridColor, 1,
			)
		}
	}
	if !y.IsCategorical() {
		for _, t := range y.TickPositions() {
			py := plot.Y + y.ValueToPixel(t.Value, plot.H)
			canvas.DrawLine(
				Point{X: plot.X, Y: py},
				Point{X: plot.X + plot.W, Y: py},
				gridColor, 1,
			)
		}
	}
}

// drawAxes paints the two axis lines, tick marks, and tick labels
// around plot. Inspired by matplotlib / Qt Graphs axis rendering.
func drawAxes(canvas Canvas, plot Rect, x, y Axis, theme *Theme) {
	axisColor := theme.TextMuted
	tickLabelFont := Font{Size: theme.FontSmall}
	if tickLabelFont.Size <= 0 {
		tickLabelFont.Size = 12
	}
	tickLen := float32(5)

	// X axis baseline (bottom edge of plot).
	canvas.DrawLine(
		Point{X: plot.X, Y: plot.Y + plot.H},
		Point{X: plot.X + plot.W, Y: plot.Y + plot.H},
		axisColor, 1,
	)
	// Y axis baseline (left edge of plot).
	canvas.DrawLine(
		Point{X: plot.X, Y: plot.Y},
		Point{X: plot.X, Y: plot.Y + plot.H},
		axisColor, 1,
	)

	// X ticks + labels below the plot.
	for _, t := range x.TickPositions() {
		px := plot.X + x.ValueToPixel(t.Value, plot.W)
		canvas.DrawLine(
			Point{X: px, Y: plot.Y + plot.H},
			Point{X: px, Y: plot.Y + plot.H + tickLen},
			axisColor, 1,
		)
		labelW := float32(len(t.Label)) * tickLabelFont.Size * 0.55
		canvas.DrawText(t.Label,
			Rect{X: px - labelW/2, Y: plot.Y + plot.H + tickLen + 2, W: labelW, H: tickLabelFont.Size + 2},
			theme.TextMuted, tickLabelFont,
		)
	}

	// Y ticks + labels to the left of the plot.
	for _, t := range y.TickPositions() {
		py := plot.Y + y.ValueToPixel(t.Value, plot.H)
		canvas.DrawLine(
			Point{X: plot.X - tickLen, Y: py},
			Point{X: plot.X, Y: py},
			axisColor, 1,
		)
		labelW := float32(len(t.Label))*tickLabelFont.Size*0.55 + 4
		canvas.DrawText(t.Label,
			Rect{X: plot.X - tickLen - labelW - 2, Y: py - tickLabelFont.Size/2, W: labelW, H: tickLabelFont.Size + 2},
			theme.TextMuted, tickLabelFont,
		)
	}

	// X-axis title, centered below the tick labels.
	if x.Title() != "" {
		titleFont := Font{Size: theme.FontSmall}
		canvas.DrawText(x.Title(),
			Rect{X: plot.X, Y: plot.Y + plot.H + tickLen + tickLabelFont.Size + 6, W: plot.W, H: titleFont.Size + 2},
			theme.Text, titleFont,
		)
	}
	// Y-axis title (not rotated in v1 — sits above the Y axis).
	if y.Title() != "" {
		titleFont := Font{Size: theme.FontSmall}
		titleW := float32(len(y.Title()))*titleFont.Size*0.55 + 4
		canvas.DrawText(y.Title(),
			Rect{X: plot.X - titleW, Y: plot.Y - titleFont.Size - 2, W: titleW, H: titleFont.Size + 2},
			theme.Text, titleFont,
		)
	}
}
