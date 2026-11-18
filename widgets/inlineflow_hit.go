package widgets

import . "github.com/qizhanchan/qui"

// Float exclusions are holes in this anonymous inline formatting box. Its
// full-width bounds must not intercept a float's text/controls behind it.
func (b *InlineBox) HitTest(p Point) Widget {
	origin := b.Bounds()
	for _, ex := range b.floatEx {
		r := ex.Rect
		r.X += origin.X
		r.Y += origin.Y
		if r.Contains(p) {
			return nil
		}
	}
	return b.Container.HitTest(p)
}
