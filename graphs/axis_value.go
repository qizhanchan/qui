package graphs

import "strconv"

// ValueAxis is a linear continuous axis. Ranges can be set explicitly
// (Min/Max) or left as zero values, in which case the Chart auto-fits
// from series bounds. Format overrides the default strconv label
// formatter; use it for units, percentage, etc.
type ValueAxis struct {
	Min, Max  float32
	TickCount int // suggested number of ticks; default 5
	Format    func(float32) string
	TitleText string
	// InvertedAxis flips ValueToPixel / PixelToValue so larger values
	// map to smaller pixel offsets — what Y axes want by default so
	// "up on screen" = "higher value".
	InvertedAxis bool
}

// compile-time check
var _ Axis = (*ValueAxis)(nil)

func (a *ValueAxis) Range() (float32, float32) { return a.Min, a.Max }

func (a *ValueAxis) SetRange(min, max float32) {
	if max < min {
		min, max = max, min
	}
	a.Min = min
	a.Max = max
}

func (a *ValueAxis) Inverted() bool      { return a.InvertedAxis }
func (a *ValueAxis) IsCategorical() bool { return false }
func (a *ValueAxis) Title() string       { return a.TitleText }
func (a *ValueAxis) SetTitle(s string)   { a.TitleText = s }

// ValueToPixel maps a data value to a pixel offset in [0..axisLen].
// For inverted axes (Y), larger data values produce smaller pixel
// offsets so they appear higher on screen.
func (a *ValueAxis) ValueToPixel(v float32, axisLen float32) float32 {
	span := a.Max - a.Min
	if span == 0 || axisLen <= 0 {
		return 0
	}
	t := (v - a.Min) / span
	if a.InvertedAxis {
		t = 1 - t
	}
	return t * axisLen
}

func (a *ValueAxis) PixelToValue(p float32, axisLen float32) float32 {
	if axisLen <= 0 {
		return a.Min
	}
	span := a.Max - a.Min
	t := p / axisLen
	if a.InvertedAxis {
		t = 1 - t
	}
	return a.Min + t*span
}

// TickPositions produces nice-numbered ticks across [Min..Max]. The
// underlying niceNumber() helper snaps outward — we then filter to the
// visible window so ticks snap to round values but never paint outside
// the range.
func (a *ValueAxis) TickPositions() []AxisTick {
	tc := a.TickCount
	if tc <= 0 {
		tc = 5
	}
	step, niceMin, niceMax := niceNumber(a.Min, a.Max, tc)
	var ticks []AxisTick
	// Clamp the iteration window to the visible range so a snapped
	// niceMin below a.Min doesn't bleed beyond the plot.
	startV := niceMin
	for startV < a.Min {
		startV += step
	}
	for v := startV; v <= niceMax+step*0.001; v += step {
		if v > a.Max+step*0.001 {
			break
		}
		ticks = append(ticks, AxisTick{
			Value: v,
			Label: a.FormatLabel(v),
			Major: true,
		})
	}
	return ticks
}

func (a *ValueAxis) FormatLabel(v float32) string {
	if a.Format != nil {
		return a.Format(v)
	}
	// Default: %g drops trailing zeros; good enough for most axes.
	return strconv.FormatFloat(float64(v), 'g', 4, 32)
}
