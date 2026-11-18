package htmlcss

import (
	"strings"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/svg"
)

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
	// untinted keeps the document's authored colors (a web page's SVG
	// image), instead of painting it in the text color like an icon.
	untinted    bool
	fit         qui.ImageFit
	posX, posY  lenPct
	hasPosition bool
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
	tint := s.tint
	if s.untinted {
		tint = qui.Color{}
	}
	b := s.Bounds()
	dst := b
	if doc, ok := s.src.(*svg.Document); ok && s.fit != qui.ImageStretch {
		iw, ih := float32(doc.Width), float32(doc.Height)
		if iw <= 0 || ih <= 0 {
			iw, ih = doc.ViewBox.W, doc.ViewBox.H
		}
		if iw > 0 && ih > 0 {
			scale := b.W / iw
			if sy := b.H / ih; (s.fit == qui.ImageCover && sy > scale) || (s.fit == qui.ImageContain && sy < scale) {
				scale = sy
			}
			dst.W, dst.H = iw*scale, ih*scale
			x, y := lenPct{pct: .5}, lenPct{pct: .5}
			if s.hasPosition {
				x, y = s.posX, s.posY
			}
			dst.X += x.resolve(b.W - dst.W)
			dst.Y += y.resolve(b.H - dst.H)
		}
	}
	depth := canvas.Save()
	canvas.ClipRect(b)
	qui.DrawVector(canvas, s.src, dst, tint)
	canvas.RestoreTo(depth)
}

func (s *svgImage) HitTest(p qui.Point) qui.Widget {
	if s.Bounds().Contains(p) {
		return s
	}
	return nil
}

// Role reports the image role for the accessibility tree.
func (s *svgImage) Role() string { return qui.RoleImage }

func (s *svgImage) setObjectStyle(cs *ComputedStyle) {
	s.fit = objectFit(cs)
	s.posX, s.posY, s.hasPosition = parsePosition(strings.Fields(cs.raw["object-position"]), cs.FontSize)
}
