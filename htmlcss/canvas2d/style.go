package canvas2d

import (
	"fmt"
	"image"
	"math"
	"sort"
	"strconv"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
)

// Style is a fillStyle / strokeStyle value: a Color, a *Gradient or a
// *Pattern.
type Style interface{ isStyle() }

// Color is a solid CSS color.
type Color struct{ C qui.Color }

func (Color) isStyle() {}

// String serializes as a canvas does: #rrggbb when opaque, else rgba().
func (c Color) String() string { return SerializeColor(c.C) }

// SerializeColor is the canvas serialization of a color.
func SerializeColor(c qui.Color) string {
	r, g, b := to8(c.R), to8(c.G), to8(c.B)
	if c.A >= 1 {
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	}
	a := strconv.FormatFloat(math.Round(float64(c.A)*1000)/1000, 'f', -1, 64)
	if c.A <= 0 {
		a = "0"
	}
	return fmt.Sprintf("rgba(%d, %d, %d, %s)", r, g, b, a)
}

func to8(v float32) uint8 {
	return uint8(math.Round(float64(max(0, min(1, v)) * 255)))
}

// ParseColor parses a CSS color for a style setter.
func ParseColor(s string) (qui.Color, bool) { return htmlcss.ParseColor(s) }

// GradientKind is linear, radial or conic.
type GradientKind uint8

const (
	Linear GradientKind = iota
	Radial
	Conic
)

// Gradient is a CanvasGradient.
type Gradient struct {
	Kind           GradientKind
	X0, Y0, R0     float64
	X1, Y1, R1     float64
	Angle          float64 // conic start angle
	stops          []gradStop
}

type gradStop struct {
	off float64
	c   qui.Color
}

func (*Gradient) isStyle() {}

// NewLinearGradient is createLinearGradient.
func NewLinearGradient(x0, y0, x1, y1 float64) *Gradient {
	return &Gradient{Kind: Linear, X0: x0, Y0: y0, X1: x1, Y1: y1}
}

// NewRadialGradient is createRadialGradient; ok is false for a negative
// radius (IndexSizeError).
func NewRadialGradient(x0, y0, r0, x1, y1, r1 float64) (*Gradient, bool) {
	if r0 < 0 || r1 < 0 {
		return nil, false
	}
	return &Gradient{Kind: Radial, X0: x0, Y0: y0, R0: r0, X1: x1, Y1: y1, R1: r1}, true
}

// NewConicGradient is createConicGradient.
func NewConicGradient(angle, x, y float64) *Gradient {
	return &Gradient{Kind: Conic, Angle: angle, X0: x, Y0: y}
}

// AddColorStop is CanvasGradient.addColorStop; ok is false when offset is
// outside [0, 1] (IndexSizeError).
func (g *Gradient) AddColorStop(offset float64, c qui.Color) bool {
	if !(offset >= 0 && offset <= 1) {
		return false
	}
	// Stable: equal offsets keep insertion order (a hard edge).
	i := sort.Search(len(g.stops), func(i int) bool { return g.stops[i].off > offset })
	g.stops = append(g.stops, gradStop{})
	copy(g.stops[i+1:], g.stops[i:])
	g.stops[i] = gradStop{offset, c}
	return true
}

// shader returns a qui shader sampling the gradient in user space, or nil
// when it paints nothing (no stops, degenerate geometry).
func (g *Gradient) shader() (qui.Shader, bool) {
	if len(g.stops) == 0 {
		return nil, false
	}
	switch g.Kind {
	case Linear:
		if g.X0 == g.X1 && g.Y0 == g.Y1 {
			return nil, false
		}
	case Radial:
		if g.X0 == g.X1 && g.Y0 == g.Y1 && g.R0 == g.R1 {
			return nil, false
		}
	}
	return gradShader{g: g, stops: append([]gradStop(nil), g.stops...)}, true
}

type gradShader struct {
	g     *Gradient
	stops []gradStop
}

func (s gradShader) ColorAt(xf, yf float32) qui.Color {
	x, y := float64(xf), float64(yf)
	g := s.g
	var t float64
	switch g.Kind {
	case Linear:
		dx, dy := g.X1-g.X0, g.Y1-g.Y0
		t = ((x-g.X0)*dx + (y-g.Y0)*dy) / (dx*dx + dy*dy)
	case Conic:
		a := math.Atan2(y-g.Y0, x-g.X0) - g.Angle
		t = math.Mod(a/(2*math.Pi), 1)
		if t < 0 {
			t++
		}
	case Radial:
		var ok bool
		t, ok = radialT(g, x, y)
		if !ok {
			return qui.Color{}
		}
	}
	return s.at(t)
}

// radialT solves the two-circle gradient for the largest ω with r(ω) ≥ 0
// whose circle passes through (x, y).
func radialT(g *Gradient, x, y float64) (float64, bool) {
	cdx, cdy, dr := g.X1-g.X0, g.Y1-g.Y0, g.R1-g.R0
	pdx, pdy := x-g.X0, y-g.Y0
	a := cdx*cdx + cdy*cdy - dr*dr
	b := pdx*cdx + pdy*cdy + g.R0*dr
	c := pdx*pdx + pdy*pdy - g.R0*g.R0
	if math.Abs(a) < 1e-12 {
		if b == 0 {
			return 0, false
		}
		w := c / (2 * b)
		if g.R0+w*dr < 0 {
			return 0, false
		}
		return w, true
	}
	disc := b*b - a*c
	if disc < 0 {
		return 0, false
	}
	sq := math.Sqrt(disc)
	w1, w2 := (b+sq)/a, (b-sq)/a
	if w1 < w2 {
		w1, w2 = w2, w1
	}
	if g.R0+w1*dr >= 0 {
		return w1, true
	}
	if g.R0+w2*dr >= 0 {
		return w2, true
	}
	return 0, false
}

// at samples the stops (pad spread).
func (s gradShader) at(t float64) qui.Color {
	st := s.stops
	if t <= st[0].off {
		return st[0].c
	}
	last := st[len(st)-1]
	if t >= last.off {
		return last.c
	}
	for i := 1; i < len(st); i++ {
		if t < st[i].off {
			a, b := st[i-1], st[i]
			f := float32((t - a.off) / (b.off - a.off))
			// Chrome interpolates canvas gradients unpremultiplied.
			pa, pb := a.c, b.c
			return qui.Color{
				R: pa.R + (pb.R-pa.R)*f, G: pa.G + (pb.G-pa.G)*f,
				B: pa.B + (pb.B-pa.B)*f, A: pa.A + (pb.A-pa.A)*f,
			}
		}
	}
	return last.c
}

// Pattern is a CanvasPattern.
type Pattern struct {
	img        *image.RGBA // snapshot, premultiplied
	rx, ry     bool        // repeat along x / y
	m          qui.Matrix  // pattern space → user space
	smooth     bool
}

func (*Pattern) isStyle() {}

// NewPattern is createPattern; ok is false for an unknown repetition
// (SyntaxError). The image is copied.
func NewPattern(img image.Image, repetition string) (*Pattern, bool) {
	p := &Pattern{m: qui.IdentityMatrix(), smooth: true}
	switch repetition {
	case "", "repeat":
		p.rx, p.ry = true, true
	case "repeat-x":
		p.rx = true
	case "repeat-y":
		p.ry = true
	case "no-repeat":
	default:
		return nil, false
	}
	p.img = toRGBA(img)
	return p, true
}

// SetTransform is CanvasPattern.setTransform.
func (p *Pattern) SetTransform(a, b, c, d, e, f float64) {
	p.m = qui.Matrix{A: float32(a), C: float32(b), B: float32(c), D: float32(d), TX: float32(e), TY: float32(f)}
}

func (p *Pattern) shader() (qui.Shader, bool) {
	if p.img == nil || p.img.Rect.Empty() {
		return nil, false
	}
	inv, ok := p.m.Invert()
	if !ok {
		return nil, false
	}
	return patShader{p: p, inv: inv}, true
}

type patShader struct {
	p   *Pattern
	inv qui.Matrix
}

func (s patShader) ColorAt(x, y float32) qui.Color {
	q := s.inv.TransformPoint(qui.Point{X: x, Y: y})
	img := s.p.img
	w, h := img.Rect.Dx(), img.Rect.Dy()
	ix, iy := int(math.Floor(float64(q.X))), int(math.Floor(float64(q.Y)))
	if s.p.rx {
		ix = ((ix % w) + w) % w
	} else if ix < 0 || ix >= w {
		return qui.Color{}
	}
	if s.p.ry {
		iy = ((iy % h) + h) % h
	} else if iy < 0 || iy >= h {
		return qui.Color{}
	}
	o := img.PixOffset(img.Rect.Min.X+ix, img.Rect.Min.Y+iy)
	px := img.Pix[o : o+4 : o+4]
	a := float32(px[3]) / 255
	if a == 0 {
		return qui.Color{}
	}
	return qui.Color{R: float32(px[0]) / 255 / a, G: float32(px[1]) / 255 / a, B: float32(px[2]) / 255 / a, A: a}
}

// toRGBA copies img into a premultiplied RGBA at the origin.
func toRGBA(img image.Image) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	if src, ok := img.(*image.RGBA); ok {
		for y := 0; y < b.Dy(); y++ {
			copy(out.Pix[y*out.Stride:y*out.Stride+4*b.Dx()], src.Pix[src.PixOffset(b.Min.X, b.Min.Y+y):])
		}
		return out
	}
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bb, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			o := out.PixOffset(x, y)
			out.Pix[o], out.Pix[o+1], out.Pix[o+2], out.Pix[o+3] = uint8(r>>8), uint8(g>>8), uint8(bb>>8), uint8(a>>8)
		}
	}
	return out
}
