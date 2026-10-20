package chart

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
func drawGrid(canvas Canvas, plot Rect, x, y Axis, theme *Theme, override Color) {
	gridColor := theme.TextMuted
	gridColor.A *= 0.35 // subdued; ticks shouldn't compete with data
	if override.A > 0 {
		gridColor = override
	}
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
func drawAxes(canvas Canvas, plot Rect, x, y Axis, theme *Theme, family string, axisColor, labelColor, titleColor Color) {
	drawXAxis(canvas, plot, x, theme, family, axisColor, labelColor, titleColor, false)
	drawYAxis(canvas, plot, y, theme, family, axisColor, labelColor, titleColor, false)
}

func drawXAxis(canvas Canvas, plot Rect, x Axis, theme *Theme, family string, axisColor, labelColor, titleColor Color, top bool) {
	if x == nil {
		return
	}
	if axisColor.A == 0 {
		axisColor = theme.TextMuted
	}
	if labelColor.A == 0 {
		labelColor = theme.TextMuted
	}
	if titleColor.A == 0 {
		titleColor = theme.Text
	}
	tickLabelFont := Font{Family: family, Size: theme.FontSmall}
	if tickLabelFont.Size <= 0 {
		tickLabelFont.Size = 12
	}
	tickLen := float32(5)

	axisY := plot.Y + plot.H
	tickSign := float32(1)
	if top {
		axisY = plot.Y
		tickSign = -1
	}
	// X axis baseline.
	canvas.DrawLine(
		Point{X: plot.X, Y: axisY},
		Point{X: plot.X + plot.W, Y: axisY},
		axisColor, 1,
	)
	// X ticks + labels outside the plot.
	for _, t := range x.TickPositions() {
		px := plot.X + x.ValueToPixel(t.Value, plot.W)
		canvas.DrawLine(
			Point{X: px, Y: axisY},
			Point{X: px, Y: axisY + tickSign*tickLen},
			axisColor, 1,
		)
		labelW, _ := TextMetrics(t.Label, tickLabelFont)
		if labelW <= 0 {
			labelW = float32(len(t.Label)) * tickLabelFont.Size * 0.55
		}
		labelY := axisY + tickLen
		if top {
			labelY = axisY - tickLen - tickLabelFont.Size - 2
		}
		canvas.DrawText(t.Label,
			Rect{X: px - labelW/2, Y: labelY, W: labelW, H: tickLabelFont.Size + 2},
			labelColor, tickLabelFont,
		)
	}
	if x.Title() != "" {
		titleFont := Font{Family: family, Size: theme.FontSmall}
		titleW, _ := TextMetrics(x.Title(), titleFont)
		if titleW <= 0 {
			titleW = float32(len(x.Title())) * titleFont.Size * 0.55
		}
		titleY := plot.Y + plot.H - 10
		if top {
			titleY = plot.Y - tickLabelFont.Size - 12
		}
		canvas.DrawText(x.Title(),
			Rect{X: plot.X + plot.W + 15, Y: titleY, W: titleW, H: titleFont.Size + 2},
			titleColor, titleFont,
		)
	}
}

func drawYAxis(canvas Canvas, plot Rect, y Axis, theme *Theme, family string, axisColor, labelColor, titleColor Color, right bool) {
	if y == nil {
		return
	}
	if axisColor.A == 0 {
		axisColor = theme.TextMuted
	}
	if labelColor.A == 0 {
		labelColor = theme.TextMuted
	}
	if titleColor.A == 0 {
		titleColor = theme.Text
	}
	tickLabelFont := Font{Family: family, Size: theme.FontSmall}
	if tickLabelFont.Size <= 0 {
		tickLabelFont.Size = 12
	}
	tickLen := float32(5)
	axisX := plot.X
	tickSign := float32(-1)
	if right {
		axisX = plot.X + plot.W
		tickSign = 1
	}
	canvas.DrawLine(
		Point{X: axisX, Y: plot.Y},
		Point{X: axisX, Y: plot.Y + plot.H},
		axisColor, 1,
	)

	// Y ticks + labels outside the plot.
	for _, t := range y.TickPositions() {
		py := plot.Y + y.ValueToPixel(t.Value, plot.H)
		canvas.DrawLine(
			Point{X: axisX, Y: py},
			Point{X: axisX + tickSign*tickLen, Y: py},
			axisColor, 1,
		)
		labelW, _ := TextMetrics(t.Label, tickLabelFont)
		if labelW <= 0 {
			labelW = float32(len(t.Label)) * tickLabelFont.Size * 0.55
		}
		labelX := plot.X - tickLen - labelW - 3
		if right {
			labelX = axisX + tickLen + 3
		}
		canvas.DrawText(t.Label,
			Rect{X: labelX, Y: py - tickLabelFont.Size/2 - 3, W: labelW, H: tickLabelFont.Size + 2},
			labelColor, tickLabelFont,
		)
	}
	// ECharts defaults a value-axis name to the end position: horizontal,
	// just inside the top of the axis.
	if y.Title() != "" {
		titleFont := Font{Family: family, Size: theme.FontSmall}
		titleW, _ := TextMetrics(y.Title(), titleFont)
		if titleW <= 0 {
			titleW = float32(len(y.Title())) * titleFont.Size * 0.55
		}
		titleX := plot.X - titleW/2
		if right {
			titleX = plot.X + plot.W - titleW/2
		}
		canvas.DrawText(y.Title(),
			Rect{X: titleX, Y: plot.Y - 35, W: titleW, H: titleFont.Size + 2},
			titleColor, titleFont,
		)
	}
}
