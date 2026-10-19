package graphs

import (
	"math"
	"strconv"
)

// Extent is a one-dimensional data domain. Valid is false when there is
// no meaningful range (for example, an empty data set).
type Extent struct {
	Min, Max float64
	Valid    bool
}

// ExtentOf returns the finite min/max extent of values. NaN and infinite
// values are ignored.
func ExtentOf(values []float64) Extent {
	var e Extent
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		if !e.Valid {
			e = Extent{Min: v, Max: v, Valid: true}
			continue
		}
		if v < e.Min {
			e.Min = v
		}
		if v > e.Max {
			e.Max = v
		}
	}
	return e
}

// Tick is one labelled position on a scale.
type Tick struct {
	Value float64
	Label string
	Major bool
}

// Scale maps values from a data domain into normalized [0, 1] positions
// and back. Pixel mapping deliberately belongs to the chart layer so the
// same scale can drive drawing, hit testing and accessibility.
type Scale interface {
	Extent() Extent
	SetExtent(Extent)
	Normalize(value float64) float64
	Invert(position float64) float64
	Ticks(target int) []Tick
}

// LinearScale is a continuous linear scale.
type LinearScale struct {
	Min, Max float64
	// Formatter overrides the default tick formatter.
	Formatter func(float64) string
}

var _ Scale = (*LinearScale)(nil)

// NewLinearScale constructs a linear scale over [min, max].
func NewLinearScale(min, max float64) *LinearScale {
	return &LinearScale{Min: min, Max: max}
}

func (s *LinearScale) Extent() Extent {
	return Extent{Min: s.Min, Max: s.Max, Valid: s.Max > s.Min}
}

func (s *LinearScale) SetExtent(e Extent) {
	if !e.Valid || e.Max <= e.Min {
		s.Min, s.Max = 0, 0
		return
	}
	s.Min, s.Max = e.Min, e.Max
}

// Normalize maps value to [0, 1]. Degenerate scales map everything to 0.
func (s *LinearScale) Normalize(value float64) float64 {
	span := s.Max - s.Min
	if span <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return (value - s.Min) / span
}

// Invert maps a normalized position back to a data value.
func (s *LinearScale) Invert(position float64) float64 {
	return s.Min + position*(s.Max-s.Min)
}

func (s *LinearScale) Ticks(target int) []Tick {
	ticks := NiceLinearTicks(s.Min, s.Max, target)
	if s.Formatter != nil {
		for i := range ticks {
			ticks[i].Label = s.Formatter(ticks[i].Value)
		}
	}
	return ticks
}

// NiceLinearTicks returns ticks snapped to 1/2/5 * 10^n steps across the
// visible [min, max] window. The endpoints themselves do not have to be
// tick locations.
func NiceLinearTicks(min, max float64, target int) []Tick {
	if math.IsNaN(min) || math.IsNaN(max) || math.IsInf(min, 0) || math.IsInf(max, 0) {
		return nil
	}
	if max < min {
		min, max = max, min
	}
	if target < 2 {
		target = 5
	}
	if max == min {
		return []Tick{{Value: min, Label: formatTick(min), Major: true}}
	}
	rough := (max - min) / float64(target-1)
	power := math.Pow(10, math.Floor(math.Log10(rough)))
	frac := rough / power
	nice := 10.0
	switch {
	case frac < 1.5:
		nice = 1
	case frac < 3:
		nice = 2
	case frac < 7:
		nice = 5
	}
	step := nice * power
	if step <= 0 || math.IsInf(step, 0) || math.IsNaN(step) {
		return nil
	}
	start := math.Ceil(min/step-1e-12) * step
	ticks := make([]Tick, 0, target+2)
	for v := start; v <= max+step*1e-9; v += step {
		// Snap values infinitesimally below a rounded multiple to that
		// multiple so labels don't show -0 or long tails.
		if math.Abs(v) < step*1e-10 {
			v = 0
		}
		ticks = append(ticks, Tick{Value: v, Label: formatTick(v), Major: true})
	}
	return ticks
}

func formatTick(v float64) string {
	return strconv.FormatFloat(v, 'g', 6, 64)
}

// CategoryScale maps discrete indices into fixed equal-width bands.
// Normalize returns the center of the indexed band in [0, 1].
type CategoryScale struct {
	Categories []string
}

var _ Scale = (*CategoryScale)(nil)

func (s *CategoryScale) Extent() Extent {
	n := len(s.Categories)
	return Extent{Min: 0, Max: float64(n), Valid: n > 0}
}

func (s *CategoryScale) SetExtent(Extent) {
	// Category domains are defined by Categories, not by a numeric extent.
}

func (s *CategoryScale) Normalize(value float64) float64 {
	n := len(s.Categories)
	if n == 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return (value + 0.5) / float64(n)
}

func (s *CategoryScale) Invert(position float64) float64 {
	n := len(s.Categories)
	if n == 0 {
		return 0
	}
	return position*float64(n) - 0.5
}

func (s *CategoryScale) Ticks(int) []Tick {
	ticks := make([]Tick, len(s.Categories))
	for i, label := range s.Categories {
		ticks[i] = Tick{Value: float64(i), Label: label, Major: true}
	}
	return ticks
}

// Band returns the normalized [start, end) bounds of category index i.
func (s *CategoryScale) Band(index int) (start, end float64) {
	n := len(s.Categories)
	if n == 0 || index < 0 || index >= n {
		return 0, 0
	}
	start = float64(index) / float64(n)
	return start, start + 1/float64(n)
}

// BandWidth returns the normalized width of one category band.
func (s *CategoryScale) BandWidth() float64 {
	n := len(s.Categories)
	if n == 0 {
		return 0
	}
	return 1 / float64(n)
}
