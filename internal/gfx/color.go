package gfx

// Color is a simple RGBA color definition.
type Color struct {
	R, G, B, A float32

	// themeDefault is provenance, not paint: the color is a framework
	// default standing in for a theme token (see ThemeDefault). Renderers
	// ignore it. It travels with the value through Style copies, and a
	// color an app or stylesheet writes, whether a literal, a parsed CSS
	// color or a theme token, never carries it.
	themeDefault bool
}

// ThemeDefault returns c marked as a framework-seeded default: the light
// baseline value of a slot that should follow the active theme. A widget
// constructor seeds its color slots with marked colors and maps them to
// theme tokens at draw time. Anything later written into the slot is
// unmarked and paints exactly as written, even when its RGBA happens to
// equal the default. The theme mapping never decides by value which colors
// are its own.
func ThemeDefault(c Color) Color {
	c.themeDefault = true
	return c
}

// IsThemeDefault reports whether c was marked by ThemeDefault.
func (c Color) IsThemeDefault() bool { return c.themeDefault }

var (
	ColorTransparent = Color{R: 0, G: 0, B: 0, A: 0}
	ColorWhite       = Color{R: 1, G: 1, B: 1, A: 1}
	ColorBlack       = Color{R: 0, G: 0, B: 0, A: 1}
	ColorGray        = Color{R: 0.5, G: 0.5, B: 0.5, A: 1}
	ColorBlue        = Color{R: 0.2, G: 0.4, B: 0.9, A: 1}
)

// AdjustBrightness returns c with R/G/B shifted by delta, clamped.
// Alpha is preserved.
func AdjustBrightness(c Color, delta float32) Color {
	return Color{
		R: clampColor(c.R + delta),
		G: clampColor(c.G + delta),
		B: clampColor(c.B + delta),
		A: c.A,
	}
}

// LerpColor linearly interpolates between a and b at t ∈ [0, 1].
// Straight RGB interpolation — fine for close-hue transitions (hover
// tints); for wide hue sweeps a proper HSL/OKLAB lerp is better but
// out of scope.
func LerpColor(a, b Color, t float32) Color {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	return Color{
		R: a.R + (b.R-a.R)*t,
		G: a.G + (b.G-a.G)*t,
		B: a.B + (b.B-a.B)*t,
		A: a.A + (b.A-a.A)*t,
	}
}

func clampColor(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
