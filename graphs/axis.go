package graphs

// Axis maps data values to pixel positions along a single dimension
// and produces tick positions + labels for the axis painter.
//
// axisLen is supplied per call (not stored) because the same axis
// instance is reused across measure/layout/draw and the pixel length
// only exists at draw time (after the plot area is known).
type Axis interface {
	// Range returns the currently-visible data range.
	Range() (min, max float32)
	// SetRange clamps or snaps to a new range; used by pan / zoom.
	SetRange(min, max float32)
	// TickPositions returns the visible ticks in data-value order.
	TickPositions() []AxisTick
	// ValueToPixel maps a data value to a pixel offset in [0..axisLen].
	ValueToPixel(v float32, axisLen float32) float32
	// PixelToValue inverts ValueToPixel; zoom/pan use it to keep the
	// cursor-anchored value fixed during scroll-zoom.
	PixelToValue(p float32, axisLen float32) float32
	// FormatLabel renders a tick value as a display string.
	FormatLabel(v float32) string
	// IsCategorical distinguishes bucketed axes (string categories) from
	// continuous ones. Bar series check this to lay out groups.
	IsCategorical() bool
	// Title returns the axis title shown next to the ticks.
	Title() string
	SetTitle(string)
	// Inverted reports whether the pixel axis grows in the opposite
	// direction to the value axis (Y axes typically do: larger value
	// paints higher on screen, i.e. smaller pixel).
	Inverted() bool
}

// AxisTick is one rendered tick mark.
type AxisTick struct {
	Value float32
	Label string
	Major bool
}

// niceNumber rounds a numeric span to a "nice" increment suitable for
// axis ticks. Step falls on 1 / 2 / 5 × 10^k so labels stay readable.
// Returns the step and the clamped (min, max) snapped outward to the
// nearest multiples of step.
func niceNumber(min, max float32, targetTicks int) (step, niceMin, niceMax float32) {
	if targetTicks < 2 {
		targetTicks = 5
	}
	if max <= min {
		// Degenerate range — synthesize a 1-unit window around min.
		return 1, min - 0.5, min + 0.5
	}
	span := float64(max - min)
	rough := span / float64(targetTicks-1)
	// Order of magnitude of the step.
	pow := float64ExpBase10(rough)
	frac := rough / pow
	var nice float64
	switch {
	case frac < 1.5:
		nice = 1
	case frac < 3:
		nice = 2
	case frac < 7:
		nice = 5
	default:
		nice = 10
	}
	stepF := nice * pow
	niceMinF := float64(floorDiv(float64(min), stepF)) * stepF
	niceMaxF := float64(ceilDiv(float64(max), stepF)) * stepF
	return float32(stepF), float32(niceMinF), float32(niceMaxF)
}

func float64ExpBase10(v float64) float64 {
	if v <= 0 {
		return 1
	}
	p := 1.0
	for v >= 10 {
		v /= 10
		p *= 10
	}
	for v < 1 {
		v *= 10
		p /= 10
	}
	return p
}

func floorDiv(x, step float64) float64 {
	q := x / step
	if q >= 0 {
		// Cast to int64 floors toward zero — that's correct for positive q.
		return float64(int64(q))
	}
	// Negative: int64 cast also floors toward zero, so we need to step
	// further down by 1 when there's a fractional remainder.
	i := int64(q)
	if float64(i) != q {
		i -= 1
	}
	return float64(i)
}

func ceilDiv(x, step float64) float64 {
	q := x / step
	if q <= 0 {
		return float64(int64(q))
	}
	i := int64(q)
	if float64(i) != q {
		i += 1
	}
	return float64(i)
}
