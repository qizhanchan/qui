package htmlcss

import "github.com/qizhanchan/qui"

// svgImage renders a vector source (a parsed SVG document) at its layout
// bounds via qui.DrawVector, which rasterizes at physical-pixel size
// (HiDPI-aware). It backs <img src="*.svg"> in the engine. A non-zero
// tint collapses the icon to a monochrome silhouette in that color — the
// common case for icon-set glyphs, which are single-color and
// should adopt the surrounding text color.
type svgImage struct {
	qui.BaseWidget
	src  qui.VectorSource
	w, h float32
	tint qui.Color
}

func newSVGImage(src qui.VectorSource, w, h float32, tint qui.Color) *svgImage {
	s := &svgImage{src: src, w: w, h: h, tint: tint}
	s.BaseWidget = qui.NewBaseWidget()
	s.SetSelf(s)
	return s
}

func (s *svgImage) Measure(available qui.Size) qui.Size {
	return qui.Size{W: s.w, H: s.h}
}

func (s *svgImage) Draw(canvas qui.Canvas) {
	qui.DrawVector(canvas, s.src, s.Bounds(), s.tint)
}

func (s *svgImage) HitTest(p qui.Point) qui.Widget {
	if s.Bounds().Contains(p) {
		return s
	}
	return nil
}

// Role reports the image role for the accessibility tree.
func (s *svgImage) Role() string { return qui.RoleImage }
