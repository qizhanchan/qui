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
	htmlControlBorder = ThemeDefault(Color{R: 0x76 / 255.0, G: 0x76 / 255.0, B: 0x76 / 255.0, A: 1})
	// htmlControlText is the default control text color (black).
	htmlControlText = ThemeDefault(Color{R: 0, G: 0, B: 0, A: 1})
	// htmlControlBg is the default control background (white).
	htmlControlBg = ThemeDefault(Color{R: 1, G: 1, B: 1, A: 1})
	// htmlButtonBg is the UA <button> face color, #efefef.
	htmlButtonBg = ThemeDefault(Color{R: 0xef / 255.0, G: 0xef / 255.0, B: 0xef / 255.0, A: 1})
	// htmlAccent is the checked/focus accent, #2d75cc (a neutral blue,
	// matching the reference's styled checkbox/radio fill; white on it
	// passes WCAG AA).
	htmlAccent = ThemeDefault(Color{R: 0x2d / 255.0, G: 0x75 / 255.0, B: 0xcc / 255.0, A: 1})
	// htmlDisabledText / htmlDisabledBorder for the :disabled look.
	htmlDisabledText   = ThemeDefault(Color{R: 0.55, G: 0.55, B: 0.55, A: 1})
	htmlDisabledBorder = ThemeDefault(Color{R: 0.80, G: 0.80, B: 0.80, A: 1})
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
// The zero-config constructors seed their state styles and color fields
// with the baseline colors above. Under the default light theme those ARE
// the intended pixels (the compare-html.sh baseline), but after SetTheme a
// control built with them would keep its light look, so themed maps each
// one to the theme token it stands for at draw time.
//
// Which slots follow the theme is decided by provenance, never by value:
// every baseline color is marked with ThemeDefault, the mark travels with
// the value through Style copies, and anything an app or a stylesheet
// writes is unmarked. An authored `color: #000` therefore stays black on a
// dark theme even though it equals htmlControlText. Matching by RGBA alone
// would remap it to the theme's light text.

var (
	baselineGen   = ^uint64(0)
	baselineState bool
)

// themeIsBaseline reports whether the active theme is the default light
// theme (cached per ThemeGeneration): the fast path where every baseline
// color paints as itself.
func themeIsBaseline() bool {
	if g := ThemeGeneration(); g != baselineGen {
		baselineGen = g
		baselineState = *CurrentTheme() == LightTheme
	}
	return baselineState
}

// Literal baseline grays some constructors use besides the named ones.
var (
	baselineHoverBg    = ThemeDefault(Color{R: 0.90, G: 0.90, B: 0.90, A: 1})
	baselinePressedBg  = ThemeDefault(Color{R: 0.82, G: 0.82, B: 0.82, A: 1})
	baselineDisabledBg = ThemeDefault(Color{R: 0.94, G: 0.94, B: 0.94, A: 1})
	baselineBlue       = ThemeDefault(Color{R: 0x2d / 255.0, G: 0x75 / 255.0, B: 0xcc / 255.0, A: 1}) // = htmlAccent
	baselineTrack      = ThemeDefault(Color{R: 0.88, G: 0.88, B: 0.88, A: 1})
	baselineOffBorder  = ThemeDefault(Color{R: 0.60, G: 0.60, B: 0.60, A: 1})
	baselineTextGray   = ThemeDefault(Color{R: 0.35, G: 0.35, B: 0.35, A: 1})
	// MenuBar's bar surface and caption color.
	baselineBarBg   = ThemeDefault(Color{R: 0.95, G: 0.95, B: 0.95, A: 1})
	baselineBarText = ThemeDefault(Color{R: 0.10, G: 0.10, B: 0.10, A: 1})
)

// themed maps a constructor-seeded baseline color to its theme token (see
// above). Unmarked colors pass through untouched, as does a marked color
// whose RGBA was edited after seeding (it no longer equals a baseline).
//
// Each slot follows its own token. A token the active theme leaves at
// LightTheme's value keeps the baseline pixel, so a light theme that only
// changes Accent recolors only the accent slots.
func themed(c Color) Color {
	if !c.IsThemeDefault() || themeIsBaseline() {
		return c
	}
	th, lt := CurrentTheme(), &LightTheme
	tok := func(f func(*Theme) Color) Color {
		if v := f(th); v != f(lt) {
			return v
		}
		return c
	}
	switch c {
	case htmlControlBorder:
		return tok(func(t *Theme) Color { return t.BorderStrong })
	case htmlControlText, baselineBarText:
		return tok(func(t *Theme) Color { return t.Text })
	case htmlControlBg:
		return tok(func(t *Theme) Color { return t.SurfaceOverlay })
	case htmlButtonBg:
		return tok(func(t *Theme) Color { return t.SurfaceStrong })
	case htmlAccent, baselineBlue:
		return tok(func(t *Theme) Color { return t.Accent })
	case htmlDisabledText:
		return tok(func(t *Theme) Color { return LerpColor(t.Surface, t.Text, 0.45) })
	case htmlDisabledBorder:
		return tok(func(t *Theme) Color { return t.Border })
	case baselineHoverBg:
		return tok(func(t *Theme) Color { return LerpColor(t.SurfaceStrong, t.Text, t.HoverOpacity) })
	case baselinePressedBg:
		return tok(func(t *Theme) Color { return LerpColor(t.SurfaceStrong, t.Text, t.PressedOpacity) })
	case baselineDisabledBg, baselineTrack, baselineBarBg:
		return tok(func(t *Theme) Color { return t.SurfaceRaised })
	case baselineOffBorder, baselineTextGray:
		return tok(func(t *Theme) Color { return t.TextMuted })
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

// nativeBoxPadding reports a raw-HTML control's authored padding (CSS
// `padding` on an <input> / <select>, or Style().Padding set in Go).
// The zero value means "UA geometry": the fixed htmlControlPad* insets and
// the 22 px box. Once set, the text sits at border + padding and the box
// grows to fit it, as a browser lays the control out.
func nativeBoxPadding(st *Style) (Insets, bool) {
	p := st.Padding
	return p, p != (Insets{})
}

// nativePaddedHeight is the box height of a padded raw-HTML control:
// border + padding + one line of text (Style.LineHeight when set, else
// 1.3 em), never below the UA box.
func nativePaddedHeight(st *Style, pad Insets) float32 {
	fs := st.Font.Size
	if fs <= 0 {
		fs = htmlControlFontSize
	}
	line := fs * 1.3
	if st.LineHeight > 0 {
		line = fs * st.LineHeight
	}
	h := 2*st.BorderSize + pad.Top + pad.Bottom + line
	if h < htmlControlHeight {
		h = htmlControlHeight
	}
	return h
}
