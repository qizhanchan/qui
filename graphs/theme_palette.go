package graphs

import (
	"math"

	. "github.com/qizhanchan/qui"
)

// DefaultPalette is the fallback series-color palette used when no
// theme-derived palette is available or when the active theme is too
// desaturated to spin a hue wheel off. The ordering is chosen to keep
// adjacent series visually distinct — it cycles through hue families
// before repeating.
var DefaultPalette = []Color{
	{R: 0.26, G: 0.52, B: 0.96, A: 1}, // blue
	{R: 0.96, G: 0.53, B: 0.26, A: 1}, // orange
	{R: 0.30, G: 0.75, B: 0.40, A: 1}, // green
	{R: 0.95, G: 0.70, B: 0.20, A: 1}, // amber
	{R: 0.74, G: 0.38, B: 0.87, A: 1}, // violet
	{R: 0.30, G: 0.80, B: 0.80, A: 1}, // teal
	{R: 0.90, G: 0.30, B: 0.50, A: 1}, // magenta
	{R: 0.60, G: 0.60, B: 0.65, A: 1}, // muted gray
}

// PaletteFor derives a palette from the active theme's primary color,
// rotating the hue at even 45° intervals while preserving saturation
// and lightness. Falls back to DefaultPalette if theme.Accent is
// effectively grayscale (low saturation) — hue rotation off a gray
// seed just produces more gray.
func PaletteFor(theme *Theme) []Color {
	if theme == nil {
		return DefaultPalette
	}
	h, s, l := rgbToHSL(theme.Accent)
	if s < 0.1 {
		return DefaultPalette
	}
	const count = 8
	out := make([]Color, count)
	for i := 0; i < count; i++ {
		hue := math.Mod(h+float64(i)*(360.0/count), 360.0)
		if hue < 0 {
			hue += 360
		}
		r, g, b := hslToRGB(hue, s, l)
		out[i] = Color{R: r, G: g, B: b, A: theme.Accent.A}
	}
	return out
}

// rgbToHSL converts a [0..1]-channel RGB color to HSL with hue in degrees.
func rgbToHSL(c Color) (h float64, s, l float64) {
	r := float64(c.R)
	g := float64(c.G)
	b := float64(c.B)
	maxC := math.Max(r, math.Max(g, b))
	minC := math.Min(r, math.Min(g, b))
	l = (maxC + minC) / 2
	if maxC == minC {
		return 0, 0, l
	}
	d := maxC - minC
	if l > 0.5 {
		s = d / (2 - maxC - minC)
	} else {
		s = d / (maxC + minC)
	}
	switch maxC {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	return h, s, l
}

// hslToRGB inverts rgbToHSL. Returned channels are [0..1] float32.
func hslToRGB(h, s, l float64) (r, g, b float32) {
	if s == 0 {
		v := float32(l)
		return v, v, v
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	hh := h / 360
	r = float32(hueToRGB(p, q, hh+1.0/3))
	g = float32(hueToRGB(p, q, hh))
	b = float32(hueToRGB(p, q, hh-1.0/3))
	return r, g, b
}

func hueToRGB(p, q, t float64) float64 {
	if t < 0 {
		t += 1
	}
	if t > 1 {
		t -= 1
	}
	switch {
	case t < 1.0/6:
		return p + (q-p)*6*t
	case t < 1.0/2:
		return q
	case t < 2.0/3:
		return p + (q-p)*(2.0/3-t)*6
	default:
		return p
	}
}
