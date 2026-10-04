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
