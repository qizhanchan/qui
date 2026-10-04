package svg

import (
	"image"
	"image/color"
	"math"

	"github.com/qizhanchan/qui"
	"github.com/srwiley/rasterx"
	"golang.org/x/image/math/fixed"
)

// rasterizeDocument is the inner rasterization pass shared by the
// public Rasterize entry. It produces a *image.RGBA with the given
// pixel dimensions; tinting / cache lookup happen one level up.
//
// preserveAspectRatio is implicit xMidYMid meet: scale = min(w/vw,
// h/vh), then center-pad. This matches what every browser does with
// the default attribute and is what svg.go's predecessor did via
// SvgIcon.SetTarget.
func rasterizeDocument(d *Document, width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	if d == nil {
		return img
	}

	vb := d.effectiveViewBox()
	if vb.W <= 0 || vb.H <= 0 {
		return img
	}

	sx := float32(width) / vb.W
	sy := float32(height) / vb.H
	s := sx
	if sy < s {
		s = sy
	}

	// xMidYMid meet: center the scaled viewBox inside the target image.
	tx := (float32(width) - vb.W*s) / 2
	ty := (float32(height) - vb.H*s) / 2

	// Initial matrix: translate(-vb.X, -vb.Y) → scale(s) → translate(tx, ty).
	// Composed left-to-right per SVG transform semantics.
	m := Translate(-vb.X, -vb.Y).Then(Scale(s, s)).Then(Translate(tx, ty))

	scanner := rasterx.NewScannerGV(width, height, img, img.Bounds())
	dasher := rasterx.NewDasher(width, height, scanner)

	// Root group's own style + transform participate in the cascade
	// before walking children. <svg> is conceptually <g transform=...>.
	root := d.Root
	rootMatrix := applyElementMatrix(m.toMatrix2D(), root.Transform)
	rootStyle := resolveStyle(defaultResolvedStyle(), root.Style)
	rootOpacity := 1.0
	if root.Style.Opacity.Set {
		rootOpacity = float64(clampUnit(root.Style.Opacity.V))
	}

	for _, child := range root.Children {
		drawElement(child, dasher, rootMatrix, rootStyle, rootOpacity)
	}

	return img
}

// effectiveViewBox returns the document's coordinate system: explicit
// viewBox when both W and H are positive; otherwise the intrinsic
// Width×Height; otherwise a sensible 1×1 fallback so we don't divide
// by zero.
func (d *Document) effectiveViewBox() ViewBox {
	if d.ViewBox.W > 0 && d.ViewBox.H > 0 {
		return d.ViewBox
	}
	if d.Width > 0 && d.Height > 0 {
		return ViewBox{X: 0, Y: 0, W: d.Width, H: d.Height}
	}
	return ViewBox{X: 0, Y: 0, W: 1, H: 1}
}

// drawShape is the per-element entry point used by every leaf shape's
// draw method. It handles the fill pass and the stroke pass, threading
// the resolved style and final composite opacity through. addPath is
// the closure that issues the shape's geometry into an Adder — the
// same closure is invoked twice (once for fill, once for stroke) so
// it must not depend on outer mutable state.
func drawShape(d *rasterx.Dasher, m rasterx.Matrix2D, s resolvedStyle, opacity float64, addPath func(rasterx.Adder)) {
	if s.Fill.Kind == PaintSolid && s.FillOpacity > 0 && s.Fill.Color.A > 0 {
		fillPass(d, m, s, opacity, addPath)
	}
	if s.Stroke.Kind == PaintSolid && s.StrokeOpacity > 0 && s.Stroke.Color.A > 0 && s.StrokeWidth > 0 {
		strokePass(d, m, s, opacity, addPath)
	}
}

func fillPass(d *rasterx.Dasher, m rasterx.Matrix2D, s resolvedStyle, opacity float64, addPath func(rasterx.Adder)) {
	d.Clear()
	f := &d.Filler
	f.SetWinding(s.FillRule != FillRuleEvenOdd)
	ad := newMatrixAdder(f, m)
	addPath(ad)
	f.SetColor(rasterx.ApplyOpacity(toNRGBA(s.Fill.Color), float64(s.FillOpacity)*opacity))
	f.Draw()
	// rasterx leaves winding sticky on the scanner; reset to default
	// nonzero so the next fill on a different element starts clean.
	f.SetWinding(true)
}

func strokePass(d *rasterx.Dasher, m rasterx.Matrix2D, s resolvedStyle, opacity float64, addPath func(rasterx.Adder)) {
	d.Clear()
	// Stroke width should scale with the user-space transform. The
	// simplest correct approximation is to multiply by the average of
	// the matrix's two scale factors (|column0| and |column1| lengths).
	// For uniform scale + rotation this is exact; for non-uniform it's
	// the same heuristic browsers fall back on for vector-effect=
	// "non-scaling-stroke" off.
	scale := matrixScale(m)
	width := fixed.Int26_6(float64(s.StrokeWidth) * scale * 64)
	if width < 1 {
		// At < 1/64 of a logical pixel rasterx silently draws nothing;
		// floor to one fixed-point unit so very thin strokes still show.
		width = 1
	}
	miter := fixed.Int26_6(s.MiterLimit * 64)
	capL := capFunc(s.LineCap)
	join := joinMode(s.LineJoin)
	var dashes []float64
	if len(s.Dash) > 0 {
		dashes = make([]float64, len(s.Dash))
		for i, v := range s.Dash {
			dashes[i] = float64(v) * scale
		}
	}
	d.SetStroke(width, miter, capL, capL, rasterx.RoundGap, join, dashes, float64(s.DashOffset)*scale)

	ad := newMatrixAdder(d, m)
	addPath(ad)
	d.SetColor(rasterx.ApplyOpacity(toNRGBA(s.Stroke.Color), float64(s.StrokeOpacity)*opacity))
	d.Draw()
}

// matrixScale returns the average linear scaling implied by m.
// For uniform scale this is the scale factor; for the typical
// "translate + scale + small rotation" cases this is within a few
// percent of the right answer.
func matrixScale(m rasterx.Matrix2D) float64 {
	xLen := length2(m.A, m.B)
	yLen := length2(m.C, m.D)
	return (xLen + yLen) / 2
}

func length2(a, b float64) float64 { return math.Sqrt(a*a + b*b) }

// toNRGBA converts a qui.Color (float32, premultiplied-agnostic) into
// the straight-alpha NRGBA form rasterx expects.
func toNRGBA(c qui.Color) color.NRGBA {
	return color.NRGBA{
		R: byteFrom(c.R),
		G: byteFrom(c.G),
		B: byteFrom(c.B),
		A: byteFrom(c.A),
	}
}

func byteFrom(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(v*255 + 0.5)
}

func capFunc(c LineCap) rasterx.CapFunc {
	switch c {
	case LineCapRound:
		return rasterx.RoundCap
	case LineCapSquare:
		return rasterx.SquareCap
	default:
		return rasterx.ButtCap
	}
}

func joinMode(j LineJoin) rasterx.JoinMode {
	switch j {
	case LineJoinRound:
		return rasterx.Round
	case LineJoinBevel:
		return rasterx.Bevel
	default:
		return rasterx.Miter
	}
}

// --- matrix adapter Adder -----------------------------------------

// matrixAdder wraps an underlying rasterx.Adder and transforms every
// point it receives through m before forwarding. This is how we get
// per-element transforms to compose with the document-level scale:
// the path geometry stays in user coordinates and the matrix bakes
// in transforms + the viewBox→pixel mapping.
type matrixAdder struct {
	dst rasterx.Adder
	m   rasterx.Matrix2D
}

func newMatrixAdder(dst rasterx.Adder, m rasterx.Matrix2D) *matrixAdder {
	return &matrixAdder{dst: dst, m: m}
}

func (a *matrixAdder) Start(p fixed.Point26_6) { a.dst.Start(a.m.TFixed(p)) }
func (a *matrixAdder) Line(p fixed.Point26_6)  { a.dst.Line(a.m.TFixed(p)) }
func (a *matrixAdder) QuadBezier(b, c fixed.Point26_6) {
	a.dst.QuadBezier(a.m.TFixed(b), a.m.TFixed(c))
}
func (a *matrixAdder) CubeBezier(b, c, d fixed.Point26_6) {
	a.dst.CubeBezier(a.m.TFixed(b), a.m.TFixed(c), a.m.TFixed(d))
}
func (a *matrixAdder) Stop(closeLoop bool) { a.dst.Stop(closeLoop) }

// --- tint / premultiply (carried over verbatim from the old svg.go) -

// applyTint rewrites every output pixel as (tint.RGB, tint.A * coverage)
// in premultiplied form. The input is the rasterizer's straight-alpha
// monochrome output; we collapse RGB to "covered or not" via the alpha
// channel since the SVG is single-color. This is the same routine the
// old svg.go shipped with — kept identical so existing icon palettes
// produce byte-identical bitmaps.
func applyTint(img *image.RGBA, tint color.RGBA) {
	pix := img.Pix
	tr16 := uint16(tint.R)
	tg16 := uint16(tint.G)
	tb16 := uint16(tint.B)
	ta16 := uint16(tint.A)
	for i := 0; i+3 < len(pix); i += 4 {
		a := pix[i+3]
		if a == 0 {
			pix[i+0] = 0
			pix[i+1] = 0
			pix[i+2] = 0
			continue
		}
		outA := (uint16(a) * ta16) / 255
		pix[i+3] = uint8(outA)
		pix[i+0] = uint8((tr16 * outA) / 255)
		pix[i+1] = uint8((tg16 * outA) / 255)
		pix[i+2] = uint8((tb16 * outA) / 255)
	}
}

// premultiplyInPlace turns straight RGBA into premultiplied RGBA.
// Used for the no-tint path so multi-color SVGs composite correctly
// when blitted through draw.Over.
func premultiplyInPlace(img *image.RGBA) {
	pix := img.Pix
	for i := 0; i+3 < len(pix); i += 4 {
		a := uint16(pix[i+3])
		if a == 0 || a == 255 {
			continue
		}
		pix[i+0] = uint8((uint16(pix[i+0]) * a) / 255)
		pix[i+1] = uint8((uint16(pix[i+1]) * a) / 255)
		pix[i+2] = uint8((uint16(pix[i+2]) * a) / 255)
	}
}

func tintToColor(t qui.Color) color.RGBA {
	return color.RGBA{
		R: byteFrom(t.R),
		G: byteFrom(t.G),
		B: byteFrom(t.B),
		A: byteFrom(t.A),
	}
}
