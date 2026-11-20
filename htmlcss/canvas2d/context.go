package canvas2d

import (
	"image"
	"math"

	"github.com/qizhanchan/qui"
)

// MaxPixels bounds a canvas bitmap (Chrome's limit is 268M pixels; a
// smaller one keeps a hostile page from exhausting memory). A larger
// canvas gets no bitmap: drawing does nothing, like a failed allocation.
const MaxPixels = 64 << 20

// Context is a CanvasRenderingContext2D over a persistent bitmap.
// It is not safe for concurrent use: the page drives it from one thread.
type Context struct {
	w, h  int
	bmp   *image.RGBA // premultiplied
	st    state
	stack []state
	path  pathData // the current default path, device space
	gen   uint64

	scratch *image.RGBA // slow-path source layer
	change  func()
}

type state struct {
	m           qui.Matrix
	fill        Style
	stroke      Style
	alpha       float64
	op          string
	lineWidth   float64
	cap         string
	join        string
	miter       float64
	dash        []float64
	dashOffset  float64
	shadowBlur  float64
	shadowX     float64
	shadowY     float64
	shadowColor qui.Color
	font        qui.Font
	fontStr     string
	align       string
	baseline    string
	direction   string
	smoothing   bool
	smoothQual  string
	letterSp    string
	wordSp      string
	filter      string
	clip        *clipEntry // innermost clip (a chain)
}

// clipEntry is one clip() in device space; the effective clip is the
// intersection of the chain.
type clipEntry struct {
	parent  *clipEntry
	path    *qui.Path // device space
	rect    *qui.Rect // set when the clip is an axis-aligned rectangle
	evenOdd bool
	mask    []uint8 // cached coverage of the whole chain (slow path)
}

func defaultState() state {
	f := qui.Font{Size: 10, Weight: qui.FontWeightNormal}
	if fam := genericFamily("sans-serif"); fam != "" {
		f.Family = fam
	}
	return state{
		m:           qui.IdentityMatrix(),
		fill:        Color{C: qui.Color{A: 1}},
		stroke:      Color{C: qui.Color{A: 1}},
		alpha:       1,
		op:          "source-over",
		lineWidth:   1,
		cap:         "butt",
		join:        "miter",
		miter:       10,
		shadowColor: qui.Color{},
		font:        f,
		fontStr:     "10px sans-serif",
		align:       "start",
		baseline:    "alphabetic",
		direction:   "inherit",
		smoothing:   true,
		smoothQual:  "low",
		letterSp:    "0px",
		wordSp:      "0px",
		filter:      "none",
	}
}

// genericFamily maps a generic family through htmlcss's settings.
var genericFamily = func(string) string { return "" }

// New returns a context with a w×h transparent bitmap.
func New(w, h int) *Context {
	c := &Context{st: defaultState()}
	c.Resize(w, h)
	return c
}

// OnChange registers fn, called after every change to the bitmap (to
// invalidate the element showing it).
func (c *Context) OnChange(fn func()) { c.change = fn }

func (c *Context) changed() {
	c.gen++
	if c.change != nil {
		c.change()
	}
}

// Width and Height are the bitmap size.
func (c *Context) Width() int  { return c.w }
func (c *Context) Height() int { return c.h }

// Generation counts bitmap changes.
func (c *Context) Generation() uint64 { return c.gen }

// Image is the bitmap (premultiplied RGBA; nil when empty or too large).
// The caller must not keep it across drawing.
func (c *Context) Image() *image.RGBA { return c.bmp }

// Resize is setting canvas.width / height: the bitmap is cleared and the
// context state reset.
func (c *Context) Resize(w, h int) {
	w, h = max(w, 0), max(h, 0)
	c.w, c.h = w, h
	c.bmp, c.scratch = nil, nil
	if w > 0 && h > 0 && w*h <= MaxPixels {
		c.bmp = image.NewRGBA(image.Rect(0, 0, w, h))
		qui.MarkImageVolatile(c.bmp) // drawn into after it is shown
	}
	c.st = defaultState()
	c.stack = nil
	c.path.reset()
	c.changed()
}

// Reset is ctx.reset(): clear the bitmap, the path and the state stack.
func (c *Context) Reset() {
	if c.bmp != nil {
		clear(c.bmp.Pix)
	}
	c.st = defaultState()
	c.stack = nil
	c.path.reset()
	c.changed()
}

// ---- state ----

func (c *Context) Save() {
	if len(c.stack) >= 1024 {
		return
	}
	s := c.st
	s.dash = append([]float64(nil), c.st.dash...)
	c.stack = append(c.stack, s)
}

func (c *Context) Restore() {
	if n := len(c.stack); n > 0 {
		c.st = c.stack[n-1]
		c.stack = c.stack[:n-1]
	}
}

// IsContextLost is always false.
func (c *Context) IsContextLost() bool { return false }

// ---- transforms ----

func (c *Context) concat(n qui.Matrix) { c.st.m = c.st.m.Concat(n) }

func (c *Context) Scale(x, y float64) {
	if finite(x, y) {
		c.concat(qui.ScaleMatrix(float32(x), float32(y)))
	}
}

func (c *Context) Rotate(a float64) {
	if finite(a) {
		s, co := math.Sincos(a)
		c.concat(qui.Matrix{A: float32(co), B: float32(-s), C: float32(s), D: float32(co)})
	}
}

func (c *Context) Translate(x, y float64) {
	if finite(x, y) {
		c.concat(qui.TranslateMatrix(float32(x), float32(y)))
	}
}

// matrixOf maps canvas transform(a, b, c, d, e, f) to qui's layout.
func matrixOf(a, b, cc, d, e, f float64) qui.Matrix {
	return qui.Matrix{A: float32(a), C: float32(b), B: float32(cc), D: float32(d), TX: float32(e), TY: float32(f)}
}

func (c *Context) Transform(a, b, cc, d, e, f float64) {
	if finite(a, b, cc, d, e, f) {
		c.concat(matrixOf(a, b, cc, d, e, f))
	}
}

func (c *Context) SetTransform(a, b, cc, d, e, f float64) {
	if finite(a, b, cc, d, e, f) {
		c.st.m = matrixOf(a, b, cc, d, e, f)
	}
}

func (c *Context) ResetTransform() { c.st.m = qui.IdentityMatrix() }

// GetTransform returns a, b, c, d, e, f.
func (c *Context) GetTransform() [6]float64 {
	m := c.st.m
	return [6]float64{float64(m.A), float64(m.C), float64(m.B), float64(m.D), float64(m.TX), float64(m.TY)}
}

// ---- simple attributes ----

func (c *Context) GlobalAlpha() float64 { return c.st.alpha }
func (c *Context) SetGlobalAlpha(a float64) {
	if finite(a) && a >= 0 && a <= 1 {
		c.st.alpha = a
	}
}

func (c *Context) CompositeOp() string { return c.st.op }
func (c *Context) SetCompositeOp(op string) {
	if compositeOps[op] {
		c.st.op = op
	}
}

func (c *Context) FillStyle() Style      { return c.st.fill }
func (c *Context) StrokeStyle() Style    { return c.st.stroke }
func (c *Context) SetFillStyle(s Style)  { c.st.fill = s }
func (c *Context) SetStrokeStyle(s Style) { c.st.stroke = s }

func (c *Context) LineWidth() float64 { return c.st.lineWidth }
func (c *Context) SetLineWidth(v float64) {
	if finite(v) && v > 0 {
		c.st.lineWidth = v
	}
}

func (c *Context) LineCap() string { return c.st.cap }
func (c *Context) SetLineCap(v string) {
	if v == "butt" || v == "round" || v == "square" {
		c.st.cap = v
	}
}

func (c *Context) LineJoin() string { return c.st.join }
func (c *Context) SetLineJoin(v string) {
	if v == "miter" || v == "round" || v == "bevel" {
		c.st.join = v
	}
}

func (c *Context) MiterLimit() float64 { return c.st.miter }
func (c *Context) SetMiterLimit(v float64) {
	if finite(v) && v > 0 {
		c.st.miter = v
	}
}

// SetLineDash is setLineDash; odd-length lists repeat. Invalid values
// (negative, non-finite) ignore the call.
func (c *Context) SetLineDash(d []float64) {
	for _, v := range d {
		if !finite(v) || v < 0 {
			return
		}
	}
	if len(d)%2 == 1 {
		d = append(append([]float64(nil), d...), d...)
	}
	c.st.dash = append([]float64(nil), d...)
}

func (c *Context) LineDash() []float64 { return append([]float64(nil), c.st.dash...) }
func (c *Context) LineDashOffset() float64 { return c.st.dashOffset }
func (c *Context) SetLineDashOffset(v float64) {
	if finite(v) {
		c.st.dashOffset = v
	}
}

func (c *Context) ShadowBlur() float64 { return c.st.shadowBlur }
func (c *Context) SetShadowBlur(v float64) {
	if finite(v) && v >= 0 {
		c.st.shadowBlur = v
	}
}
func (c *Context) ShadowOffsetX() float64 { return c.st.shadowX }
func (c *Context) ShadowOffsetY() float64 { return c.st.shadowY }
func (c *Context) SetShadowOffsetX(v float64) {
	if finite(v) {
		c.st.shadowX = v
	}
}
func (c *Context) SetShadowOffsetY(v float64) {
	if finite(v) {
		c.st.shadowY = v
	}
}
func (c *Context) ShadowColor() qui.Color     { return c.st.shadowColor }
func (c *Context) SetShadowColor(v qui.Color) { c.st.shadowColor = v }

func (c *Context) ImageSmoothing() bool     { return c.st.smoothing }
func (c *Context) SetImageSmoothing(v bool) { c.st.smoothing = v }
func (c *Context) ImageSmoothingQuality() string { return c.st.smoothQual }
func (c *Context) SetImageSmoothingQuality(v string) {
	if v == "low" || v == "medium" || v == "high" {
		c.st.smoothQual = v
	}
}

// Filter is stored and reported; it is not applied.
func (c *Context) Filter() string     { return c.st.filter }
func (c *Context) SetFilter(v string) { c.st.filter = v }

func (c *Context) LetterSpacing() string { return c.st.letterSp }
func (c *Context) WordSpacing() string   { return c.st.wordSp }
func (c *Context) SetLetterSpacing(v string) {
	if px, ok := spacingPx(v); ok {
		c.st.letterSp = v
		c.st.font.LetterSpacing = px
	}
}
func (c *Context) SetWordSpacing(v string) {
	if px, ok := spacingPx(v); ok {
		c.st.wordSp = v
		c.st.font.WordSpacing = px
	}
}

// ---- the current path ----

func (c *Context) BeginPath() { c.path.reset() }
func (c *Context) ClosePath() { c.path.closePath() }
func (c *Context) MoveTo(x, y float64) { c.path.moveTo(c.st.m, x, y) }
func (c *Context) LineTo(x, y float64) { c.path.lineTo(c.st.m, x, y) }
func (c *Context) QuadraticCurveTo(cx, cy, x, y float64) {
	c.path.quadTo(c.st.m, cx, cy, x, y)
}
func (c *Context) BezierCurveTo(c1x, c1y, c2x, c2y, x, y float64) {
	c.path.cubicTo(c.st.m, c1x, c1y, c2x, c2y, x, y)
}
func (c *Context) Rect(x, y, w, h float64) { c.path.rect(c.st.m, x, y, w, h) }

// Arc returns false for a negative radius (IndexSizeError).
func (c *Context) Arc(x, y, r, a0, a1 float64, ccw bool) bool {
	return c.path.ellipse(c.st.m, x, y, r, r, 0, a0, a1, ccw)
}

func (c *Context) Ellipse(x, y, rx, ry, rot, a0, a1 float64, ccw bool) bool {
	return c.path.ellipse(c.st.m, x, y, rx, ry, rot, a0, a1, ccw)
}

func (c *Context) ArcTo(x1, y1, x2, y2, r float64) bool {
	var cur qui.Point
	if c.path.has {
		inv, ok := c.st.m.Invert()
		if !ok {
			return true
		}
		cur = inv.TransformPoint(c.path.cur)
	}
	return c.path.arcTo(c.st.m, cur, x1, y1, x2, y2, r)
}

func (c *Context) RoundRect(x, y, w, h float64, radii []Radius) bool {
	return c.path.roundRect(c.st.m, x, y, w, h, radii)
}

// Path2D is a reusable path in its own coordinate space.
type Path2D struct{ d pathData }

func NewPath2D() *Path2D { return &Path2D{} }

func (p *Path2D) Clone() *Path2D { return &Path2D{d: p.d.clone()} }

var ident = qui.IdentityMatrix()

func (p *Path2D) MoveTo(x, y float64) { p.d.moveTo(ident, x, y) }
func (p *Path2D) LineTo(x, y float64) { p.d.lineTo(ident, x, y) }
func (p *Path2D) ClosePath()          { p.d.closePath() }
func (p *Path2D) QuadraticCurveTo(cx, cy, x, y float64) {
	p.d.quadTo(ident, cx, cy, x, y)
}
func (p *Path2D) BezierCurveTo(c1x, c1y, c2x, c2y, x, y float64) {
	p.d.cubicTo(ident, c1x, c1y, c2x, c2y, x, y)
}
func (p *Path2D) Rect(x, y, w, h float64) { p.d.rect(ident, x, y, w, h) }
func (p *Path2D) Arc(x, y, r, a0, a1 float64, ccw bool) bool {
	return p.d.ellipse(ident, x, y, r, r, 0, a0, a1, ccw)
}
func (p *Path2D) Ellipse(x, y, rx, ry, rot, a0, a1 float64, ccw bool) bool {
	return p.d.ellipse(ident, x, y, rx, ry, rot, a0, a1, ccw)
}
func (p *Path2D) ArcTo(x1, y1, x2, y2, r float64) bool {
	return p.d.arcTo(ident, p.d.cur, x1, y1, x2, y2, r)
}
func (p *Path2D) RoundRect(x, y, w, h float64, radii []Radius) bool {
	return p.d.roundRect(ident, x, y, w, h, radii)
}

// AddPath is Path2D.addPath(path, transform).
func (p *Path2D) AddPath(src *Path2D, a, b, cc, d, e, f float64) {
	if src == nil || !finite(a, b, cc, d, e, f) {
		return
	}
	p.d.appendPath(&src.d, matrixOf(a, b, cc, d, e, f))
}

// devicePath is the path to draw (nil = the current path) in device space.
func (c *Context) devicePath(p *Path2D) *pathData {
	if p == nil {
		return &c.path
	}
	var d pathData
	d.appendPath(&p.d, c.st.m)
	return &d
}

// ---- drawing ----

// FillRule values for fill / clip / isPointInPath.
const (
	NonZero = false
	EvenOdd = true
)

// Fill fills p (nil = the current path).
func (c *Context) Fill(p *Path2D, evenOdd bool) {
	c.drawPath(c.devicePath(p), c.st.fill, false, evenOdd)
}

// Stroke strokes p (nil = the current path).
func (c *Context) Stroke(p *Path2D) {
	c.drawPath(c.devicePath(p), c.st.stroke, true, false)
}

func (c *Context) FillRect(x, y, w, h float64) {
	if !finite(x, y, w, h) || w == 0 || h == 0 {
		return
	}
	var d pathData
	d.rect(c.st.m, x, y, w, h)
	c.drawPath(&d, c.st.fill, false, false)
}

func (c *Context) StrokeRect(x, y, w, h float64) {
	if !finite(x, y, w, h) {
		return
	}
	var d pathData
	switch {
	case w == 0 && h == 0:
		return
	case w == 0 || h == 0:
		d.moveTo(c.st.m, x, y)
		d.lineTo(c.st.m, x+w, y+h)
	default:
		d.rect(c.st.m, x, y, w, h)
	}
	c.drawPath(&d, c.st.stroke, true, false)
}

// ClearRect makes the rect transparent (transform and clip apply).
func (c *Context) ClearRect(x, y, w, h float64) {
	if c.bmp == nil || !finite(x, y, w, h) || w == 0 || h == 0 {
		return
	}
	if c.st.clip == nil && c.st.m.IsAxisAligned() {
		// Pixel-aligned rect, no clip: zero the pixels directly.
		r := c.st.m.TransformRect(qui.Rect{X: float32(min(x, x+w)), Y: float32(min(y, y+h)), W: float32(math.Abs(w)), H: float32(math.Abs(h))})
		x0, y0 := math.Round(float64(r.X)), math.Round(float64(r.Y))
		x1, y1 := math.Round(float64(r.X+r.W)), math.Round(float64(r.Y+r.H))
		if math.Abs(x0-float64(r.X)) < 1e-3 && math.Abs(y0-float64(r.Y)) < 1e-3 && math.Abs(x1-float64(r.X+r.W)) < 1e-3 && math.Abs(y1-float64(r.Y+r.H)) < 1e-3 {
			clearRect(c.bmp, image.Rect(int(x0), int(y0), int(x1), int(y1)).Intersect(c.bmp.Rect))
			c.changed()
			return
		}
	}
	var d pathData
	d.rect(c.st.m, x, y, w, h)
	// A clear is "copy" of transparent black restricted to the rect and
	// the clip: destination-out of an opaque fill does exactly that.
	saved := c.st
	c.st.op, c.st.alpha = "destination-out", 1
	c.st.shadowColor = qui.Color{}
	c.drawPath(&d, Color{C: qui.Color{A: 1}}, false, false)
	c.st = saved
}

// Clip intersects the clip with p (nil = the current path).
func (c *Context) Clip(p *Path2D, evenOdd bool) {
	d := c.devicePath(p)
	e := &clipEntry{parent: c.st.clip, evenOdd: evenOdd, path: d.gfxPath(ident)}
	if r, ok := axisRect(d); ok {
		e.rect = &r
	}
	c.st.clip = e
}

// axisRect reports a path that is exactly one axis-aligned rectangle.
func axisRect(d *pathData) (qui.Rect, bool) {
	var pts []qui.Point
	closed := false
	for _, s := range d.segs {
		switch s.op {
		case segMove:
			if len(pts) > 0 && closed {
				continue // rect()'s trailing moveTo
			}
			if len(pts) > 0 {
				return qui.Rect{}, false
			}
			pts = append(pts, s.p[0])
		case segLine:
			if closed {
				return qui.Rect{}, false
			}
			pts = append(pts, s.p[0])
		case segClose:
			closed = true
		default:
			return qui.Rect{}, false
		}
	}
	if len(pts) == 5 && pts[4] == pts[0] {
		pts = pts[:4]
	}
	if len(pts) != 4 {
		return qui.Rect{}, false
	}
	minX, minY := min(pts[0].X, pts[2].X), min(pts[0].Y, pts[2].Y)
	maxX, maxY := max(pts[0].X, pts[2].X), max(pts[0].Y, pts[2].Y)
	for _, p := range pts {
		if (p.X != minX && p.X != maxX) || (p.Y != minY && p.Y != maxY) {
			return qui.Rect{}, false
		}
	}
	if !((pts[0].X == pts[1].X) != (pts[0].Y == pts[1].Y)) {
		return qui.Rect{}, false
	}
	return qui.Rect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}, true
}

// IsPointInPath tests (x, y) in canvas pixels against p's interior.
func (c *Context) IsPointInPath(p *Path2D, x, y float64, evenOdd bool) bool {
	if !finite(x, y) {
		return false
	}
	return containsPoint(c.devicePath(p).flatten(ident, 0.1), qui.Point{X: float32(x), Y: float32(y)}, evenOdd)
}

// IsPointInStroke tests (x, y) against p's stroke (line width, without
// joins and caps).
func (c *Context) IsPointInStroke(p *Path2D, x, y float64) bool {
	if !finite(x, y) {
		return false
	}
	inv, ok := c.st.m.Invert()
	if !ok {
		return false
	}
	user := c.devicePath(p).flatten(inv, 0.1)
	q := inv.TransformPoint(qui.Point{X: float32(x), Y: float32(y)})
	return nearStroke(user, q, float32(c.st.lineWidth/2))
}

// paintFor builds the qui paint of a style; ok is false when it paints
// nothing.
func (c *Context) paintFor(s Style, stroke bool) (qui.Paint, bool) {
	p := qui.Paint{AntiAlias: true, Alpha: 1}
	switch v := s.(type) {
	case Color:
		if v.C.A <= 0 {
			return p, false
		}
		p.Color = v.C
	case *Gradient:
		sh, ok := v.shader()
		if !ok {
			return p, false
		}
		p.Color = qui.Color{A: 1}
		p.Shader = sh
	case *Pattern:
		sh, ok := v.shader()
		if !ok {
			return p, false
		}
		p.Color = qui.Color{A: 1}
		p.Shader = sh
	default:
		return p, false
	}
	if stroke {
		p.Style = qui.PaintStroke
		p.StrokeWidth = float32(c.st.lineWidth)
		p.MiterLimit = float32(c.st.miter)
		switch c.st.cap {
		case "round":
			p.Cap = qui.CapRound
		case "square":
			p.Cap = qui.CapSquare
		default:
			p.Cap = qui.CapButt
		}
		switch c.st.join {
		case "round":
			p.Join = qui.JoinRound
		case "bevel":
			p.Join = qui.JoinBevel
		default:
			p.Join = qui.JoinMiter
		}
	}
	return p, true
}

// drawPath draws a device-space path with style.
func (c *Context) drawPath(d *pathData, s Style, stroke, evenOdd bool) {
	if c.bmp == nil || len(d.segs) == 0 {
		return
	}
	inv, ok := c.st.m.Invert()
	if !ok {
		return
	}
	var user *qui.Path
	if stroke && len(c.st.dash) > 0 {
		user = dashed(d.flatten(inv, 0.1), c.st.dash, c.st.dashOffset)
	}
	if user == nil {
		user = d.gfxPath(inv)
	}
	c.drawUser(user, qui.IdentityMatrix(), d.gfxPath(ident).Bounds(), s, stroke, evenOdd)
}

// drawUser draws a user-space path (mapped by extra first) with style.
// devBounds is its device-space extent before stroking.
func (c *Context) drawUser(user *qui.Path, extra qui.Matrix, devBounds qui.Rect, s Style, stroke, evenOdd bool) {
	if c.bmp == nil || user.IsEmpty() {
		return
	}
	paint, ok := c.paintFor(s, stroke)
	if !ok {
		return
	}
	if evenOdd {
		paint.FillRule = qui.FillEvenOdd
	}
	bounds := devBounds
	if stroke {
		grow := float32(c.st.lineWidth) * c.st.m.AvgScale() * float32(max(c.st.miter, 1)) / 2
		bounds = qui.Rect{X: bounds.X - grow, Y: bounds.Y - grow, W: bounds.W + 2*grow, H: bounds.H + 2*grow}
	}
	m := c.st.m
	c.render(bounds, func(cv qui.Canvas, alpha float32) {
		pp := withAlpha(paint, alpha)
		if stroke && pp.Shader != nil {
			// Strokes take only solid colors: rasterize the stroke's
			// coverage, then color it from the shader.
			if img, at := shadeStroke(user, pp, m.Concat(extra), bounds, c.w, c.h); img != nil {
				cv.DrawImage(img, at)
			}
			return
		}
		cv.Concat(m)
		cv.Concat(extra)
		cv.DrawShape(qui.ShapePath{Path: user}, pp)
	})
}

// shadeStroke renders a shader-painted stroke as an image covering
// bounds (device space), returned with its device rect.
func shadeStroke(user *qui.Path, p qui.Paint, m qui.Matrix, bounds qui.Rect, w, h int) (*image.RGBA, qui.Rect) {
	area := image.Rect(
		int(math.Floor(float64(bounds.X))), int(math.Floor(float64(bounds.Y))),
		int(math.Ceil(float64(bounds.X+bounds.W))), int(math.Ceil(float64(bounds.Y+bounds.H))),
	).Intersect(image.Rect(0, 0, w, h))
	inv, ok := m.Invert()
	if area.Empty() || !ok {
		return nil, qui.Rect{}
	}
	cov := image.NewRGBA(image.Rect(0, 0, area.Dx(), area.Dy()))
	cv := qui.NewImageCanvas(cov)
	cv.Translate(float32(-area.Min.X), float32(-area.Min.Y))
	cv.Concat(m)
	solid := p
	solid.Shader = nil
	solid.Color = qui.Color{A: 1}
	cv.DrawShape(qui.ShapePath{Path: user}, solid)
	for y := 0; y < area.Dy(); y++ {
		for x := 0; x < area.Dx(); x++ {
			o := cov.PixOffset(x, y)
			a := float32(cov.Pix[o+3]) / 255
			if a == 0 {
				continue
			}
			q := inv.TransformPoint(qui.Point{X: float32(area.Min.X+x) + 0.5, Y: float32(area.Min.Y+y) + 0.5})
			c := p.Shader.ColorAt(q.X, q.Y)
			a *= max(0, min(1, c.A))
			cov.Pix[o] = uint8(clamp01(c.R)*a*255 + 0.5)
			cov.Pix[o+1] = uint8(clamp01(c.G)*a*255 + 0.5)
			cov.Pix[o+2] = uint8(clamp01(c.B)*a*255 + 0.5)
			cov.Pix[o+3] = uint8(a*255 + 0.5)
		}
	}
	return cov, qui.Rect{X: float32(area.Min.X), Y: float32(area.Min.Y), W: float32(area.Dx()), H: float32(area.Dy())}
}

// withAlpha folds global alpha into a paint's color or shader.
func withAlpha(p qui.Paint, alpha float32) qui.Paint {
	if alpha >= 1 {
		return p
	}
	p.Color.A *= alpha
	if p.Shader != nil {
		p.Shader = alphaShader{p.Shader, alpha}
	}
	return p
}

type alphaShader struct {
	s qui.Shader
	a float32
}

func (s alphaShader) ColorAt(x, y float32) qui.Color {
	c := s.s.ColorAt(x, y)
	c.A *= s.a
	return c
}

// shadowVisible is the spec's "shadows are drawn" test.
func (c *Context) shadowVisible() bool {
	return c.st.shadowColor.A > 0 && (c.st.shadowBlur > 0 || c.st.shadowX != 0 || c.st.shadowY != 0)
}

// needsMask reports a clip the fast path cannot express (even-odd).
func (c *Context) needsMask() bool {
	for e := c.st.clip; e != nil; e = e.parent {
		if e.evenOdd && e.rect == nil {
			return true
		}
	}
	return false
}

// render runs draw against the bitmap. bounds is a conservative device
// rect of what draw touches. draw receives a canvas in device space with
// the clip applied, and the alpha to paint with.
func (c *Context) render(bounds qui.Rect, draw func(cv qui.Canvas, alpha float32)) {
	if c.st.op == "source-over" && !c.shadowVisible() && !c.needsMask() {
		cv := qui.NewImageCanvas(c.bmp)
		depth := cv.Save()
		c.applyClips(cv)
		draw(cv, float32(c.st.alpha))
		cv.RestoreTo(depth) // composites clip-path and alpha layers
		c.changed()
		return
	}
	// Slow path: draw alone into the scratch layer, then merge.
	full := image.Rect(0, 0, c.w, c.h)
	area := image.Rect(
		int(math.Floor(float64(bounds.X)))-1, int(math.Floor(float64(bounds.Y)))-1,
		int(math.Ceil(float64(bounds.X+bounds.W)))+1, int(math.Ceil(float64(bounds.Y+bounds.H)))+1,
	).Intersect(full)
	if c.scratch == nil {
		c.scratch = image.NewRGBA(full)
	}
	layer := c.scratch
	clearRect(layer, area)
	if !area.Empty() {
		cv := qui.NewImageCanvas(layer)
		depth := cv.Save()
		cv.ClipRect(qui.Rect{X: float32(area.Min.X), Y: float32(area.Min.Y), W: float32(area.Dx()), H: float32(area.Dy())})
		draw(cv, 1)
		cv.RestoreTo(depth)
	}
	mask := c.clipMask()
	alpha := float32(c.st.alpha)
	if c.shadowVisible() && !area.Empty() {
		sc := c.st.shadowColor
		if sh, sr := shadowLayer(layer, area, c.st.shadowBlur, c.st.shadowX, c.st.shadowY, sc.R, sc.G, sc.B, sc.A); sh != nil {
			region := sr
			if !boundedOp(c.st.op) {
				region = full
			}
			mergeLayer(c.bmp, sh, region, c.st.op, alpha, mask)
		}
	}
	region := area
	if !boundedOp(c.st.op) {
		region = full
	}
	mergeLayer(c.bmp, layer, region, c.st.op, alpha, mask)
	clearRect(layer, area)
	c.changed()
}

func clearRect(img *image.RGBA, r image.Rectangle) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		o := img.PixOffset(r.Min.X, y)
		clear(img.Pix[o : o+4*r.Dx()])
	}
}

// applyClips narrows cv to the clip chain (fast path: no even-odd paths).
func (c *Context) applyClips(cv qui.Canvas) {
	var chain []*clipEntry
	for e := c.st.clip; e != nil; e = e.parent {
		chain = append(chain, e)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		e := chain[i]
		if e.rect != nil {
			cv.ClipRect(*e.rect)
		} else {
			cv.ClipPath(e.path)
		}
	}
}

// clipMask is the clip chain's coverage (nil = no clip), cached on the
// innermost entry.
func (c *Context) clipMask() []uint8 {
	e := c.st.clip
	if e == nil {
		return nil
	}
	if e.mask != nil && len(e.mask) == c.w*c.h {
		return e.mask
	}
	var mask []uint8
	if e.parent != nil {
		saved := c.st.clip
		c.st.clip = e.parent
		parent := c.clipMask()
		c.st.clip = saved
		mask = append([]uint8(nil), parent...)
	} else {
		mask = make([]uint8, c.w*c.h)
		for i := range mask {
			mask[i] = 255
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
	cv := qui.NewImageCanvas(img)
	p := qui.Paint{Color: qui.Color{A: 1}, AntiAlias: true, Alpha: 1}
	if e.evenOdd {
		p.FillRule = qui.FillEvenOdd
	}
	cv.DrawShape(qui.ShapePath{Path: e.path}, p)
	for i := range mask {
		mask[i] = uint8(uint16(mask[i]) * uint16(img.Pix[4*i+3]) / 255)
	}
	e.mask = mask
	return mask
}
