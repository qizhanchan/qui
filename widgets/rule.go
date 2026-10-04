package widgets

import . "github.com/qizhanchan/qui"

// Rule is the thematic-break element — the qui equivalent of HTML's <hr>.
//
// It draws a single horizontal divider line spanning its content width.
// Thickness comes from Style().BorderSize (default 1); color from
// Style().Border, falling back to the theme's Border token when unset.
// It is a block-level, full-width, fixed-height leaf.
type Rule struct {
	BaseWidget
}

// NewRule constructs a default 1px horizontal rule.
func NewRule() *Rule {
	r := &Rule{BaseWidget: NewBaseWidget()}
	r.Style().BorderSize = 1
	return r
}

func (r *Rule) thickness() float32 {
	if t := r.Style().BorderSize; t > 0 {
		return t
	}
	return 1
}

func (r *Rule) Measure(available Size) Size {
	w := available.W
	if w <= 0 {
		w = 0
	}
	return Size{W: w, H: r.thickness()}
}

func (r *Rule) Draw(canvas Canvas) {
	b := r.Bounds()
	color := r.Style().Border
	if color.A == 0 {
		color = CurrentTheme().Border
	}
	t := r.thickness()
	// Center the line vertically within the widget's height so callers
	// that give the rule extra vertical room get a centered divider.
	y := b.Y + (b.H-t)/2
	canvas.FillRect(Rect{X: b.X, Y: y, W: b.W, H: t}, color)
}

func (r *Rule) HitTest(p Point) Widget {
	if r.Bounds().Contains(p) {
		return r
	}
	return nil
}
