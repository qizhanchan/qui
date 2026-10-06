package widgets

import . "github.com/qizhanchan/qui"

// Native HTML form-control baseline.
//
// qui's default (zero-config) widget constructors — NewInput, NewSelect,
// NewButton, NewTextArea, NewCheckBox, NewRadioButton — render to match a
// raw HTML <input>/<select>/<button>/<textarea>/checkbox/radio under the
// deterministic UA baseline defined in widgets/scripts/web/native.html.
// These are the shared metrics both sides agree on; the alignment is
// verified pixel-for-pixel (ImageMagick AE) by
// widgets/scripts/compare-html.sh.
//
// A designed look is layered on top by the app — either through these
// widgets' own color fields or, preferably, as CSS on the htmlcss
// layer. Nothing here bakes in a design system.
var (
	// htmlControlBorder is the UA form-control border color, #767676.
	htmlControlBorder = Color{R: 0x76 / 255.0, G: 0x76 / 255.0, B: 0x76 / 255.0, A: 1}
	// htmlControlText is the default control text color (black).
	htmlControlText = Color{R: 0, G: 0, B: 0, A: 1}
	// htmlControlBg is the default control background (white).
	htmlControlBg = Color{R: 1, G: 1, B: 1, A: 1}
	// htmlButtonBg is the UA <button> face color, #efefef.
	htmlButtonBg = Color{R: 0xef / 255.0, G: 0xef / 255.0, B: 0xef / 255.0, A: 1}
	// htmlAccent is the checked/focus accent, #4d8cd8 (a neutral blue,
	// matching the reference's styled checkbox/radio fill).
	htmlAccent = Color{R: 0x4d / 255.0, G: 0x8c / 255.0, B: 0xd8 / 255.0, A: 1}
	// htmlDisabledText / htmlDisabledBorder for the :disabled look.
	htmlDisabledText   = Color{R: 0.55, G: 0.55, B: 0.55, A: 1}
	htmlDisabledBorder = Color{R: 0.80, G: 0.80, B: 0.80, A: 1}
)

const (
	// htmlControlRadius is the UA form-control corner radius (2 px).
	htmlControlRadius float32 = 2
	// htmlControlFontSize is the baseline control font size.
	htmlControlFontSize float32 = 14
	// htmlControlHeight is the box height of a text-like control
	// (input/select/button) at the baseline font, matching native.html.
	htmlControlHeight float32 = 22
	// htmlInputWidth is the default <input>/<textarea> box width.
	htmlInputWidth float32 = 180
	// htmlControlPadX / htmlControlPadY are the text inset from the box
	// edge (border 1 + UA padding), so text sits where the browser draws it.
	htmlControlPadX float32 = 3
	htmlControlPadY float32 = 2
	// htmlCheckSize is the <input type=checkbox|radio> box edge (13 px).
	htmlCheckSize float32 = 13
)

// Theme tracking for the native baseline.
//
// The zero-config constructors copy the HTML baseline colors above into
// their state styles and color fields. Under the default light theme those
// ARE the intended pixels (the compare-html.sh baseline), but after
// SetTheme a control built with them would keep its light look. themed
// maps each baseline constant to the theme token it stands for, at draw
// time, whenever the active theme is not the default one. Colors an app set
// itself don't match a baseline constant and pass through untouched.

var (
	baselineGen   = ^uint64(0)
	baselineState bool
)

// themeIsBaseline reports whether the active theme is the default light
// theme (cached per ThemeGeneration).
func themeIsBaseline() bool {
	if g := ThemeGeneration(); g != baselineGen {
		baselineGen = g
		baselineState = *CurrentTheme() == LightTheme
	}
	return baselineState
}

// Literal baseline grays some constructors use besides the named ones.
var (
	baselineHoverBg    = Color{R: 0.90, G: 0.90, B: 0.90, A: 1}
	baselinePressedBg  = Color{R: 0.82, G: 0.82, B: 0.82, A: 1}
	baselineDisabledBg = Color{R: 0.94, G: 0.94, B: 0.94, A: 1}
	baselineBlue       = Color{R: 0.30, G: 0.55, B: 0.85, A: 1}
	baselineTrack      = Color{R: 0.88, G: 0.88, B: 0.88, A: 1}
	baselineOffBorder  = Color{R: 0.60, G: 0.60, B: 0.60, A: 1}
	baselineTextGray   = Color{R: 0.35, G: 0.35, B: 0.35, A: 1}
	// MenuBar's bar surface and caption color.
	baselineBarBg   = Color{R: 0.95, G: 0.95, B: 0.95, A: 1}
	baselineBarText = Color{R: 0.10, G: 0.10, B: 0.10, A: 1}
)

// themed maps a baseline color to its theme token (see above).
func themed(c Color) Color {
	if c.A == 0 || themeIsBaseline() {
		return c
	}
	th := CurrentTheme()
	switch c {
	case htmlControlBorder:
		return th.BorderStrong
	case htmlControlText:
		return th.Text
	case htmlControlBg:
		return th.SurfaceOverlay
	case htmlButtonBg:
		return th.SurfaceStrong
	case htmlAccent, baselineBlue:
		return th.Accent
	case htmlDisabledText:
		return LerpColor(th.Surface, th.Text, 0.45)
	case htmlDisabledBorder:
		return th.Border
	case baselineHoverBg:
		return LerpColor(th.SurfaceStrong, th.Text, th.HoverOpacity)
	case baselinePressedBg:
		return LerpColor(th.SurfaceStrong, th.Text, th.PressedOpacity)
	case baselineDisabledBg, baselineTrack:
		return th.SurfaceRaised
	case baselineOffBorder, baselineTextGray:
		return th.TextMuted
	case baselineBarBg:
		return th.SurfaceRaised
	case baselineBarText:
		return th.Text
	}
	return c
}

// themedStyle returns s with its colors passed through themed. The result
// is a copy; s is not modified.
func themedStyle(s *Style) *Style {
	if themeIsBaseline() {
		return s
	}
	out := *s
	out.Background = themed(s.Background)
	out.Foreground = themed(s.Foreground)
	out.Border = themed(s.Border)
	return &out
}
