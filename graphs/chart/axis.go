package chart

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
