package qui

import (
	"image"
	"math"
)

// Shader is a per-pixel color generator. When a Paint has a non-nil
// Shader, the rasterizer samples ColorAt(x, y) at each pixel's center
// in LOGICAL coordinates (the same space rects and paths are quoted
// in), then applies coverage as usual. When Shader is nil the
// rasterizer takes the solid-Color fast path — zero overhead vs. the
// pre-shader implementation.
//
// The returned color is STRAIGHT RGBA. The rasterizer premultiplies at
// the blend boundary the same way it does for solid colors.
//
// Skia's SkShader is the model — subclasses cover linear / radial
// gradients, image samplers, procedural patterns, and composition.
// qui v1 ships LinearGradient and RadialGradient; ImageShader and
// ComposeShader join when a concrete caller needs them.
type Shader interface {
	ColorAt(x, y float32) Color
}

// GradientStop is one anchor of a gradient — a position on [0, 1]
// (with 0 meaning "at start", 1 meaning "at end") paired with the
// color the gradient hits there. Callers are expected to supply
// stops sorted by Offset ascending; the samplers linearly
// interpolate between adjacent stops.
type GradientStop struct {
	Offset float32
	Color  Color
}

// LinearGradient interpolates colors along the vector from Start to
// End. Points on the perpendicular line through Start receive the
// first stop's color; points on the perpendicular through End receive
// the last stop's color. Anything past the endpoints saturates to the
// nearest stop (clamp spread — Skia calls this SkTileMode::kClamp).
//
// Stops must be sorted by Offset. Zero stops → transparent black
// everywhere; one stop → uniform color of that stop.
type LinearGradient struct {
	Start, End Point
	Stops      []GradientStop
}

// ColorAt samples the gradient at logical (x, y). See Shader docs.
func (g LinearGradient) ColorAt(x, y float32) Color {
	if len(g.Stops) == 0 {
		return Color{}
	}
	if len(g.Stops) == 1 {
		return g.Stops[0].Color
	}
	dx := g.End.X - g.Start.X
	dy := g.End.Y - g.Start.Y
	lenSq := dx*dx + dy*dy
	if lenSq <= 0 {
		return g.Stops[0].Color
	}
	// Project (P - Start) onto (End - Start), normalized by |End-Start|²
	// so t=0 at Start, t=1 at End.
	t := ((x-g.Start.X)*dx + (y-g.Start.Y)*dy) / lenSq
	return sampleGradientStops(g.Stops, t)
}

// RadialGradient interpolates colors from Center outward along a
// disk of radius Radius. Points at the center receive the first
// stop's color; points at distance == Radius receive the last stop's;
// beyond Radius saturates to the last stop (clamp spread).
type RadialGradient struct {
	Center Point
	Radius float32
	Stops  []GradientStop
}

// ColorAt samples the gradient at logical (x, y). See Shader docs.
func (g RadialGradient) ColorAt(x, y float32) Color {
	if len(g.Stops) == 0 {
		return Color{}
	}
	if len(g.Stops) == 1 || g.Radius <= 0 {
		return g.Stops[0].Color
	}
	dx := x - g.Center.X
	dy := y - g.Center.Y
	dist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	t := dist / g.Radius
	return sampleGradientStops(g.Stops, t)
}

// ConicGradient sweeps its stops around Center by angle, starting at Angle
// (radians, clockwise from 12 o'clock — CSS `conic-gradient(from …)`), one full
// turn mapping [0,1] over the stops.
type ConicGradient struct {
	Center Point
	Angle  float32
	Stops  []GradientStop
}

// ColorAt samples the conic gradient at logical (x, y).
func (g ConicGradient) ColorAt(x, y float32) Color {
	if len(g.Stops) == 0 {
		return Color{}
	}
	if len(g.Stops) == 1 {
		return g.Stops[0].Color
	}
	// Angle from center: 0 at 12 o'clock, increasing clockwise.
	a := float32(math.Atan2(float64(x-g.Center.X), float64(g.Center.Y-y))) - g.Angle
	twoPi := float32(2 * math.Pi)
	a = float32(math.Mod(float64(a), float64(twoPi)))
	if a < 0 {
		a += twoPi
	}
	return sampleGradientStops(g.Stops, a/twoPi)
}

// sampleGradientStops interpolates the color at parametric position t
// through the sorted stops. Clamp semantics: t outside [Stops[0].Offset,
// Stops[last].Offset] snaps to the nearest endpoint's color.
//
// Uses linear interpolation in straight (non-premultiplied) RGB space.
// That's what CSS gradients specify and what Skia's default gradient
// sampler does — perceptually a hair off vs. gamma-correct blending
// but consistent with every browser and design tool.
func sampleGradientStops(stops []GradientStop, t float32) Color {
	if t <= stops[0].Offset {
		return stops[0].Color
	}
	last := stops[len(stops)-1]
	if t >= last.Offset {
		return last.Color
	}
	// Binary search would be faster for many-stop gradients; linear is
	// fine for typical 2-4 stop cases and keeps the code compact.
	for i := 1; i < len(stops); i++ {
		hi := stops[i]
		if t <= hi.Offset {
			lo := stops[i-1]
			span := hi.Offset - lo.Offset
			if span <= 0 {
				return hi.Color
			}
			u := (t - lo.Offset) / span
			return LerpColor(lo.Color, hi.Color, u)
		}
	}
	return last.Color
}

// solidShader wraps a Color as a trivial Shader. Never wired into
// Paint (nil Shader is the fast path), but useful in tests and for
// callers that want a uniform Shader alongside a real one.
type solidShader struct{ c Color }

func (s solidShader) ColorAt(_, _ float32) Color { return s.c }

// SolidShader returns a Shader that always returns c. Handy for
// callers that need a Shader-typed value.
func SolidShader(c Color) Shader { return solidShader{c: c} }

// ImageFit selects how an image maps onto a box (CSS background-size /
// object-fit).
type ImageFit uint8

const (
	ImageStretch ImageFit = iota // fill the box (100% 100%), aspect ignored
	ImageCover                   // scale to cover, cropping overflow (center)
	ImageContain                 // scale to fit inside, letterboxing (center)
)

// imageShader samples an image onto Bounds per Fit. Pixels outside the mapped
// region (contain letterbox) return transparent; edges clamp otherwise.
type imageShader struct {
	img    image.Image
	bounds Rect
	fit    ImageFit
}

func (s imageShader) ColorAt(x, y float32) Color {
	b := s.img.Bounds()
	iw, ih := float32(b.Dx()), float32(b.Dy())
	if b.Empty() || s.bounds.W <= 0 || s.bounds.H <= 0 || iw <= 0 || ih <= 0 {
		return Color{}
	}
	nx := (x - s.bounds.X) / s.bounds.W // 0..1 across the box
	ny := (y - s.bounds.Y) / s.bounds.H
	var u, v float32 // 0..1 across the image
	switch s.fit {
	case ImageCover:
		sc := s.bounds.W / iw
		if h := s.bounds.H / ih; h > sc {
			sc = h
		}
		dispW, dispH := iw*sc, ih*sc
		u = (nx*s.bounds.W + (dispW-s.bounds.W)/2) / dispW
		v = (ny*s.bounds.H + (dispH-s.bounds.H)/2) / dispH
	case ImageContain:
		sc := s.bounds.W / iw
		if h := s.bounds.H / ih; h < sc {
			sc = h
		}
		dispW, dispH := iw*sc, ih*sc
		px := nx*s.bounds.W - (s.bounds.W-dispW)/2
		py := ny*s.bounds.H - (s.bounds.H-dispH)/2
		if px < 0 || py < 0 || px >= dispW || py >= dispH {
			return Color{} // letterbox → transparent
		}
		u, v = px/dispW, py/dispH
	default:
		u, v = nx, ny
	}
	px := b.Min.X + int(u*iw)
	py := b.Min.Y + int(v*ih)
	if px < b.Min.X {
		px = b.Min.X
	}
	if px >= b.Max.X {
		px = b.Max.X - 1
	}
	if py < b.Min.Y {
		py = b.Min.Y
	}
	if py >= b.Max.Y {
		py = b.Max.Y - 1
	}
	r, g, bl, a := s.img.At(px, py).RGBA()
	return Color{R: float32(r) / 65535, G: float32(g) / 65535, B: float32(bl) / 65535, A: float32(a) / 65535}
}

// ImageShader returns a Shader that samples img onto bounds with the given fit.
func ImageShader(img image.Image, bounds Rect, fit ImageFit) Shader {
	return imageShader{img: img, bounds: bounds, fit: fit}
}
