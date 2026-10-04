package graphs

// CategoryAxis is a bucketed axis: each category is a string label
// that maps to an integer index. Used by BarSeries and any other
// series that lays out data in discrete columns / rows.
//
// ValueToPixel(idx, axisLen) returns the CENTER of the category's
// bucket in pixel units. Callers interested in bucket edges can
// reconstruct them from axisLen / len(Categories).
type CategoryAxis struct {
	Categories []string
	TitleText  string
	// GroupPadding is the fraction of each category slot reserved as
	// inter-group gap in grouped bar charts. 0..1 — default 0 means
	// adjacent groups touch.
	GroupPadding float32
}

// compile-time check
var _ Axis = (*CategoryAxis)(nil)

// Range on a category axis returns [0, N) so pan/zoom math still works
// (though category axes typically don't pan).
func (a *CategoryAxis) Range() (float32, float32) {
	return 0, float32(len(a.Categories))
}

// SetRange is a no-op — categories are fixed. Provided for Axis
// interface compliance.
func (a *CategoryAxis) SetRange(float32, float32) {}

func (a *CategoryAxis) Inverted() bool      { return false }
func (a *CategoryAxis) IsCategorical() bool { return true }
func (a *CategoryAxis) Title() string       { return a.TitleText }
func (a *CategoryAxis) SetTitle(s string)   { a.TitleText = s }

// TickPositions emits one major tick per category centered in its
// bucket. Value is the category index (cast to float).
func (a *CategoryAxis) TickPositions() []AxisTick {
	if len(a.Categories) == 0 {
		return nil
	}
	ticks := make([]AxisTick, len(a.Categories))
	for i, label := range a.Categories {
		ticks[i] = AxisTick{Value: float32(i), Label: label, Major: true}
	}
	return ticks
}

// ValueToPixel returns the center of the i'th bucket. Out-of-range
// indices still produce a valid projection (useful for extrapolation
// during a zoom animation, though v1 doesn't use that).
func (a *CategoryAxis) ValueToPixel(v float32, axisLen float32) float32 {
	n := len(a.Categories)
	if n == 0 || axisLen <= 0 {
		return 0
	}
	slot := axisLen / float32(n)
	return slot*v + slot*0.5
}

func (a *CategoryAxis) PixelToValue(p float32, axisLen float32) float32 {
	n := len(a.Categories)
	if n == 0 || axisLen <= 0 {
		return 0
	}
	slot := axisLen / float32(n)
	if slot == 0 {
		return 0
	}
	return (p - slot*0.5) / slot
}

func (a *CategoryAxis) FormatLabel(v float32) string {
	i := int(v + 0.5)
	if i < 0 || i >= len(a.Categories) {
		return ""
	}
	return a.Categories[i]
}

// BucketWidth is a convenience: the pixel width of one category slot.
// Used by BarSeries to compute group/stack widths.
func (a *CategoryAxis) BucketWidth(axisLen float32) float32 {
	n := len(a.Categories)
	if n == 0 || axisLen <= 0 {
		return 0
	}
	return axisLen / float32(n)
}
