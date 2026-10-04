package qui

import (
	"reflect"
	"sort"
)

// Color is a simple RGBA color definition.
type Color struct {
	R, G, B, A float32
}

var (
	ColorTransparent = Color{0, 0, 0, 0}
	ColorWhite       = Color{1, 1, 1, 1}
	ColorBlack       = Color{0, 0, 0, 1}
	ColorGray        = Color{0.5, 0.5, 0.5, 1}
	ColorBlue        = Color{0.2, 0.4, 0.9, 1}
)

// FontWeight tracks CSS-like numeric weight.
type FontWeight int

const (
	FontWeightThin       FontWeight = 100
	FontWeightExtraLight FontWeight = 200
	FontWeightLight      FontWeight = 300
	FontWeightNormal     FontWeight = 400
	FontWeightMedium     FontWeight = 500
	FontWeightSemiBold   FontWeight = 600
	FontWeightBold       FontWeight = 700
	FontWeightExtraBold  FontWeight = 800
	FontWeightBlack      FontWeight = 900
)

// Font represents font styling; actual rasterization is delegated to renderer.
type Font struct {
	Family string
	Size   float32
	// Bold is kept for backward compatibility. When Weight is 0, Bold
	// maps to Weight=700 (else Weight=400).
	Bold bool
	// Weight is preferred over Bold for new code.
	Weight FontWeight
	Italic bool
	// Features stores OpenType feature tags like "liga", "tnum", "ss01".
	Features []string
	// Variations stores variable-font axis values, e.g. {"wght": 640}.
	Variations map[string]float32
	// LetterSpacing (CSS letter-spacing) is extra tracking in pixels added
	// after every glyph. WordSpacing (CSS word-spacing) is extra space in
	// pixels added after each U+0020 space. Both default to 0 (no effect);
	// measurement (runeAdvanceWithFaces) and the CPU glyph drawer consume
	// them together so layout and painting stay aligned.
	LetterSpacing float32
	WordSpacing   float32
	// SmallCaps draws lowercase letters as capitals at a reduced size (CSS
	// `font-variant: small-caps`, OOXML w:smallCaps). It is synthesized in
	// the shaper rather than requested as an OpenType feature, so it works
	// on every face; the text itself is untouched, which keeps caret
	// offsets, selection and copied text in the source's own case.
	SmallCaps bool
}

// Style controls widget presentation.
//
// Zero-value convention: every field's zero value means "unspecified"
// so an incomplete Style can be merged onto a base without clobbering
// the base's fields. Colors treat `A == 0` as unspecified (a fully
// transparent color is indistinguishable from "no color set" — the
// framework never actually paints something you can't see, so this
// costs nothing in practice). Float scales (Radius, BorderSize,
// widths, opacity, grow/shrink/basis, line-height, letter-spacing)
// treat 0 as unspecified. Enums with a purpose-built default value
// (AlignDefault, TextAlignStart) collapse "unspecified" and "explicit
// default" — that's harmless because both resolve to the same visual.
//
// The extended field groups below (Sizing / Typography / Layout /
// Effects) exist so widget default styles, recipe outputs, and user
// overrides can all be expressed as `Style` values and merged in a
// single pipeline. `Merge(base, override)` (or `base.MergedWith(over)`)
// walks field-by-field and picks non-zero from override — Draw and
// Measure receive the merged Style; there is no separate
// `ResolvedStyle` type.
type Style struct {
	// Box model.
	Background Color
	Foreground Color
	Border     Color
	BorderSize float32
	// BorderWidths gives per-side border widths (CSS border-top/right/
	// bottom/left-width). When any side is non-zero the box paints per-side
	// borders (honoring BorderColors + BorderStyle) instead of the uniform
	// BorderSize/Border pair; radius is ignored for per-side borders.
	BorderWidths Insets
	// BorderColors gives per-side border colors (Top/Right/Bottom/Left). A
	// side whose color has alpha 0 falls back to Border.
	BorderColors SideColors
	// BorderStyle selects the stroke pattern for per-side borders
	// (solid default; dashed / dotted approximated).
	BorderStyle BorderStyle
	Radius      float32
	// Corners gives independent per-corner radii (CSS border-*-radius). When
	// non-zero it overrides the uniform Radius; a zero value means "use
	// Radius for every corner". Widgets paint per-corner via Path.AddRRectCorners.
	Corners CornerRadii
	Padding Insets
	// Margin is outside spacing, consumed by FlexLayout (plain + wrap)
	// and Container.Measure with CSS margin-box semantics: the child's
	// occupied flex slot grows by the margin and the child is laid out
	// inset within it. Grid/Absolute layouts ignore it.
	Margin Insets

	// Typography. Font carries family/size/weight/italic/features/
	// variations. LineHeight is a multiplier over Font.Size (1.2 =
	// baseline gap 1.2× font size); LetterSpacing is em-scaled tracking.
	// TextAlign controls horizontal alignment inside a text box.
	Font          Font
	LineHeight    float32
	LetterSpacing float32
	TextAlign     TextAlign

	// Sizing. Zero on any axis means "no constraint from Style" — layout
	// then falls back to widget MinSize/MaxSize/PreferredSize and finally
	// to Measure. Width/Height act as a preferred-size override on the
	// respective axis; MinWidth/MinHeight/MaxWidth/MaxHeight clamp.
	Width     float32
	Height    float32
	MinWidth  float32
	MinHeight float32
	MaxWidth  float32
	MaxHeight float32

	// WidthPct/HeightPct express the preferred size as a FRACTION (0–1]
	// of the parent's content box on that axis (CSS `width: 50%`). The
	// engines resolve them at layout time — FlowLayout block bands,
	// FlexLayout basis/cross — because only they know the containing
	// size. Width/Height win when both are set. The resolved size is the
	// widget's outer frame (border-box), then min/max-clamped as usual.
	WidthPct  float32
	HeightPct float32

	// MarginLeftAuto/MarginRightAuto mark a horizontal margin as CSS
	// `auto`. FlowLayout places an explicit-width block centered in its
	// band when both are auto (`margin: 0 auto`) and right-aligned when
	// only the left one is. The numeric Margin side should stay 0.
	//
	// In FlexLayout, auto margins on ANY side absorb the line's positive
	// free space: main-axis auto margins push/center items (and suppress
	// justify-content), cross-axis auto margins center the item on the
	// cross axis (overriding align-items/align-self). MarginTop/BottomAuto
	// exist so column flex and cross-axis centering work too.
	MarginLeftAuto   bool
	MarginRightAuto  bool
	MarginTopAuto    bool
	MarginBottomAuto bool

	// Min*Pct/Max*Pct express min/max sizes as a FRACTION (0–1] of the
	// containing block on that axis (CSS `max-width: 100%`). Like
	// Width/HeightPct they resolve at layout time in the engine that knows
	// the containing size; the absolute Min*/Max* fields win when set.
	MinWidthPct  float32
	MinHeightPct float32
	MaxWidthPct  float32
	MaxHeightPct float32

	// Layout participation. Mirror FlexItem so a child's flex behavior
	// can be declared in Style. FlexLayout reads FlexItem() first; when
	// FlexItem is zero-valued, it falls back to these fields.
	Grow      float32
	Shrink    float32
	Basis     float32
	AlignSelf AlignCross

	// Effects. Opacity 0 means "unspecified" (renderer treats as 1). A
	// non-zero Shadow paints a Skia-style drop shadow behind the box
	// — see canvas.DrawShadow. Widget default Draw paths do not paint
	// Shadow yet; recipe-driven variants will.
	Opacity float32
	Shadow  ShadowStyle
	// ExtraShadows are additional box-shadow layers (CSS comma-separated
	// shadows) painted behind Shadow, farthest layer first. Outset only.
	ExtraShadows []ShadowStyle
}

// StyleFields is the presence mask used by StylePatch. Unlike MergedWith's
// legacy non-zero convention, a present field is applied even when its value
// is zero, false, transparent, nil, or empty.
type StyleFields uint64

const (
	StyleFieldBackground StyleFields = 1 << iota
	StyleFieldForeground
	StyleFieldBorder
	StyleFieldBorderSize
	StyleFieldBorderWidths
	StyleFieldBorderColors
	StyleFieldBorderStyle
	StyleFieldRadius
	StyleFieldCorners
	StyleFieldPadding
	StyleFieldMargin
	StyleFieldFont
	StyleFieldLineHeight
	StyleFieldLetterSpacing
	StyleFieldTextAlign
	StyleFieldWidth
	StyleFieldHeight
	StyleFieldMinWidth
	StyleFieldMinHeight
	StyleFieldMaxWidth
	StyleFieldMaxHeight
	StyleFieldWidthPct
	StyleFieldHeightPct
	StyleFieldMarginLeftAuto
	StyleFieldMarginRightAuto
	StyleFieldMarginTopAuto
	StyleFieldMarginBottomAuto
	StyleFieldMinWidthPct
	StyleFieldMinHeightPct
	StyleFieldMaxWidthPct
	StyleFieldMaxHeightPct
	StyleFieldGrow
	StyleFieldShrink
	StyleFieldBasis
	StyleFieldAlignSelf
	StyleFieldOpacity
	StyleFieldShadow
	StyleFieldExtraShadows
)

// StylePatch is an explicit-presence style override. Values contains the
// replacement values and Fields says which ones are authored. This is the
// preferred merge representation when zero is meaningful.
type StylePatch struct {
	Values Style
	Fields StyleFields
}

// Apply overlays the present fields onto base and returns an ownership-safe
// result.
func (p StylePatch) Apply(base Style) Style {
	out := base.Clone()
	v := p.Values
	has := func(field StyleFields) bool { return p.Fields&field != 0 }
	if has(StyleFieldBackground) {
		out.Background = v.Background
	}
	if has(StyleFieldForeground) {
		out.Foreground = v.Foreground
	}
	if has(StyleFieldBorder) {
		out.Border = v.Border
	}
	if has(StyleFieldBorderSize) {
		out.BorderSize = v.BorderSize
	}
	if has(StyleFieldBorderWidths) {
		out.BorderWidths = v.BorderWidths
	}
	if has(StyleFieldBorderColors) {
		out.BorderColors = v.BorderColors
	}
	if has(StyleFieldBorderStyle) {
		out.BorderStyle = v.BorderStyle
	}
	if has(StyleFieldRadius) {
		out.Radius = v.Radius
	}
	if has(StyleFieldCorners) {
		out.Corners = v.Corners
	}
	if has(StyleFieldPadding) {
		out.Padding = v.Padding
	}
	if has(StyleFieldMargin) {
		out.Margin = v.Margin
	}
	if has(StyleFieldFont) {
		out.Font = v.Font
	}
	if has(StyleFieldLineHeight) {
		out.LineHeight = v.LineHeight
	}
	if has(StyleFieldLetterSpacing) {
		out.LetterSpacing = v.LetterSpacing
	}
	if has(StyleFieldTextAlign) {
		out.TextAlign = v.TextAlign
	}
	if has(StyleFieldWidth) {
		out.Width = v.Width
	}
	if has(StyleFieldHeight) {
		out.Height = v.Height
	}
	if has(StyleFieldMinWidth) {
		out.MinWidth = v.MinWidth
	}
	if has(StyleFieldMinHeight) {
		out.MinHeight = v.MinHeight
	}
	if has(StyleFieldMaxWidth) {
		out.MaxWidth = v.MaxWidth
	}
	if has(StyleFieldMaxHeight) {
		out.MaxHeight = v.MaxHeight
	}
	if has(StyleFieldWidthPct) {
		out.WidthPct = v.WidthPct
	}
	if has(StyleFieldHeightPct) {
		out.HeightPct = v.HeightPct
	}
	if has(StyleFieldMarginLeftAuto) {
		out.MarginLeftAuto = v.MarginLeftAuto
	}
	if has(StyleFieldMarginRightAuto) {
		out.MarginRightAuto = v.MarginRightAuto
	}
	if has(StyleFieldMarginTopAuto) {
		out.MarginTopAuto = v.MarginTopAuto
	}
	if has(StyleFieldMarginBottomAuto) {
		out.MarginBottomAuto = v.MarginBottomAuto
	}
	if has(StyleFieldMinWidthPct) {
		out.MinWidthPct = v.MinWidthPct
	}
	if has(StyleFieldMinHeightPct) {
		out.MinHeightPct = v.MinHeightPct
	}
	if has(StyleFieldMaxWidthPct) {
		out.MaxWidthPct = v.MaxWidthPct
	}
	if has(StyleFieldMaxHeightPct) {
		out.MaxHeightPct = v.MaxHeightPct
	}
	if has(StyleFieldGrow) {
		out.Grow = v.Grow
	}
	if has(StyleFieldShrink) {
		out.Shrink = v.Shrink
	}
	if has(StyleFieldBasis) {
		out.Basis = v.Basis
	}
	if has(StyleFieldAlignSelf) {
		out.AlignSelf = v.AlignSelf
	}
	if has(StyleFieldOpacity) {
		out.Opacity = v.Opacity
	}
	if has(StyleFieldShadow) {
		out.Shadow = v.Shadow
	}
	if has(StyleFieldExtraShadows) {
		out.ExtraShadows = append([]ShadowStyle(nil), v.ExtraShadows...)
	}
	return out.Clone()
}

// Patched applies an explicit-presence override to s.
func (s Style) Patched(patch StylePatch) Style { return patch.Apply(s) }

// Clone returns an ownership-safe copy of the style. Style contains slices
// and maps, so a plain struct assignment would still let later mutations of
// Font.Features, Font.Variations, or ExtraShadows bypass widget invalidation.
func (s Style) Clone() Style {
	out := s
	out.Font.Features = append([]string(nil), s.Font.Features...)
	if s.Font.Variations != nil {
		out.Font.Variations = make(map[string]float32, len(s.Font.Variations))
		for axis, value := range s.Font.Variations {
			out.Font.Variations[axis] = value
		}
	}
	out.ExtraShadows = append([]ShadowStyle(nil), s.ExtraShadows...)
	return out
}

func stylesEqual(a, b Style) bool { return reflect.DeepEqual(a, b) }

// styleLayoutEqual compares only fields that can affect measurement or
// placement. Paint-only changes should repaint without forcing a full tree
// layout pass.
func styleLayoutEqual(a, b Style) bool {
	return a.BorderSize == b.BorderSize &&
		a.BorderWidths == b.BorderWidths &&
		a.Padding == b.Padding &&
		a.Margin == b.Margin &&
		fontStyleEqual(a.Font, b.Font) &&
		a.LineHeight == b.LineHeight &&
		a.LetterSpacing == b.LetterSpacing &&
		a.TextAlign == b.TextAlign &&
		a.Width == b.Width && a.Height == b.Height &&
		a.MinWidth == b.MinWidth && a.MinHeight == b.MinHeight &&
		a.MaxWidth == b.MaxWidth && a.MaxHeight == b.MaxHeight &&
		a.WidthPct == b.WidthPct && a.HeightPct == b.HeightPct &&
		a.MarginLeftAuto == b.MarginLeftAuto &&
		a.MarginRightAuto == b.MarginRightAuto &&
		a.MarginTopAuto == b.MarginTopAuto &&
		a.MarginBottomAuto == b.MarginBottomAuto &&
		a.MinWidthPct == b.MinWidthPct && a.MinHeightPct == b.MinHeightPct &&
		a.MaxWidthPct == b.MaxWidthPct && a.MaxHeightPct == b.MaxHeightPct &&
		a.Grow == b.Grow && a.Shrink == b.Shrink && a.Basis == b.Basis &&
		a.AlignSelf == b.AlignSelf
}

// SideColors holds one color per box edge (CSS border-*-color).
type SideColors struct {
	Top, Right, Bottom, Left Color
}

// CornerRadii holds one radius per box corner, clockwise from top-left
// (CSS border-top-left/top-right/bottom-right/bottom-left-radius).
type CornerRadii struct {
	TL, TR, BR, BL float32
}

// IsZero reports whether no per-corner radius is set.
func (c CornerRadii) IsZero() bool { return c == CornerRadii{} }

// Resolved returns the four corner radii, substituting the uniform radius
// for any axis left at zero when Corners is entirely unset. When Corners
// carries any non-zero value it is authoritative (a 0 corner stays square).
func (c CornerRadii) Resolved(uniform float32) CornerRadii {
	if c.IsZero() {
		return CornerRadii{TL: uniform, TR: uniform, BR: uniform, BL: uniform}
	}
	return c
}

// BorderStyle selects the stroke pattern for per-side borders.
type BorderStyle uint8

const (
	BorderSolid  BorderStyle = iota // continuous line (default)
	BorderDashed                    // evenly spaced dashes
	BorderDotted                    // square dots
	BorderNone                      // no line (CSS border-style: none/hidden)
)

// ShadowStyle describes an offset/blur/spread drop shadow. Zero value
// (all fields zero including Color.A == 0) means "no shadow". Intended
// for Style-declared shadows; the elevation ladder lives in
// Theme.Elevation as ElevationLevel pairs.
type ShadowStyle struct {
	X, Y, Blur, Spread float32
	Color              Color
}

// IsZero reports whether the shadow is unspecified.
func (s ShadowStyle) IsZero() bool {
	return s.X == 0 && s.Y == 0 && s.Blur == 0 && s.Spread == 0 && s.Color.A == 0
}

func DefaultStyle() Style {
	return Style{
		Background: ColorTransparent,
		Foreground: ColorBlack,
		Border:     ColorTransparent,
		BorderSize: 0,
		Radius:     0,
		Padding:    Insets{Top: 4, Right: 6, Bottom: 4, Left: 6},
		Margin:     Insets{},
		Font: Font{
			Family: "",
			Size:   14,
			Weight: FontWeightNormal,
		},
	}
}

// MergedWith returns a new Style where every field of override that
// carries a non-zero (specified) value replaces the corresponding
// field in the receiver. Zero-value fields in override leave the
// receiver's field untouched. This is the single-choke-point used by
// the future recipe pipeline (defaults → tokens → state → local) but
// callers can use it directly today to overlay a partial Style on
// top of a base.
//
// Merge semantics per field group:
//   - Colors: override wins when A > 0
//   - Floats (BorderSize, Radius, widths, opacity, grow/shrink/basis,
//     LineHeight, LetterSpacing): override wins when != 0
//   - Insets (Padding, Margin): override wins when any side != 0
//   - Font: field-wise merge (see mergeFont); a fully-zero Font in
//     override keeps base's font entirely
//   - Enums (TextAlign, AlignSelf): override wins when != 0 (i.e. the
//     enum's zero value collapses with "unspecified")
//   - Shadow: override wins when !IsZero()
func (s Style) MergedWith(override Style) Style {
	out := s
	if override.Background.A > 0 {
		out.Background = override.Background
	}
	if override.Foreground.A > 0 {
		out.Foreground = override.Foreground
	}
	if override.Border.A > 0 {
		out.Border = override.Border
	}
	if override.BorderSize != 0 {
		out.BorderSize = override.BorderSize
	}
	if override.Radius != 0 {
		out.Radius = override.Radius
	}
	if !override.Corners.IsZero() {
		out.Corners = override.Corners
	}
	if !isZeroInsets(override.Padding) {
		out.Padding = override.Padding
	}
	if !isZeroInsets(override.Margin) {
		out.Margin = override.Margin
	}
	out.Font = mergeFont(s.Font, override.Font)
	if override.LineHeight != 0 {
		out.LineHeight = override.LineHeight
	}
	if override.LetterSpacing != 0 {
		out.LetterSpacing = override.LetterSpacing
	}
	if override.TextAlign != 0 {
		out.TextAlign = override.TextAlign
	}
	if override.Width != 0 {
		out.Width = override.Width
	}
	if override.Height != 0 {
		out.Height = override.Height
	}
	if override.MinWidth != 0 {
		out.MinWidth = override.MinWidth
	}
	if override.MinHeight != 0 {
		out.MinHeight = override.MinHeight
	}
	if override.MaxWidth != 0 {
		out.MaxWidth = override.MaxWidth
	}
	if override.MaxHeight != 0 {
		out.MaxHeight = override.MaxHeight
	}
	if override.WidthPct != 0 {
		out.WidthPct = override.WidthPct
	}
	if override.HeightPct != 0 {
		out.HeightPct = override.HeightPct
	}
	if override.MarginLeftAuto {
		out.MarginLeftAuto = true
	}
	if override.MarginRightAuto {
		out.MarginRightAuto = true
	}
	if override.MarginTopAuto {
		out.MarginTopAuto = true
	}
	if override.MarginBottomAuto {
		out.MarginBottomAuto = true
	}
	if override.MinWidthPct != 0 {
		out.MinWidthPct = override.MinWidthPct
	}
	if override.MinHeightPct != 0 {
		out.MinHeightPct = override.MinHeightPct
	}
	if override.MaxWidthPct != 0 {
		out.MaxWidthPct = override.MaxWidthPct
	}
	if override.MaxHeightPct != 0 {
		out.MaxHeightPct = override.MaxHeightPct
	}
	if override.Grow != 0 {
		out.Grow = override.Grow
	}
	if override.Shrink != 0 {
		out.Shrink = override.Shrink
	}
	if override.Basis != 0 {
		out.Basis = override.Basis
	}
	if override.AlignSelf != 0 {
		out.AlignSelf = override.AlignSelf
	}
	if override.Opacity != 0 {
		out.Opacity = override.Opacity
	}
	if !override.Shadow.IsZero() {
		out.Shadow = override.Shadow
	}
	return out.Clone()
}

// Merge is the free-function form of Style.MergedWith — mirrors CSS
// cascade order: later declarations win.
func Merge(base, override Style) Style { return base.MergedWith(override) }

func isZeroInsets(i Insets) bool {
	return i.Top == 0 && i.Right == 0 && i.Bottom == 0 && i.Left == 0
}

// mergeFont overlays override on base field-wise. Unlike simple types
// where the whole Style field is replaced atomically, Font is composite
// (family / size / weight / italic / features / variations) and callers
// often want to override just size or just weight while inheriting the
// family from the theme. Zero on a scalar or "" on family means "keep
// base". Features / Variations slices/maps are replaced atomically
// when override supplies a non-empty one.
func mergeFont(base, override Font) Font {
	out := base
	if override.Family != "" {
		out.Family = override.Family
	}
	if override.Size != 0 {
		out.Size = override.Size
	}
	if override.Weight != 0 {
		out.Weight = override.Weight
	}
	// Bold is legacy; only override when explicitly set (true). A false
	// override cannot distinguish "keep base" from "explicit not-bold".
	if override.Bold {
		out.Bold = true
	}
	if override.Italic {
		out.Italic = true
	}
	if len(override.Features) > 0 {
		out.Features = override.Features
	}
	if len(override.Variations) > 0 {
		out.Variations = override.Variations
	}
	return out
}

func (f Font) effectiveWeight() FontWeight {
	if f.Weight != 0 {
		return f.Weight
	}
	if f.Bold {
		return FontWeightBold
	}
	return FontWeightNormal
}

func fontStyleEqual(a, b Font) bool {
	if a.Family != b.Family || a.Size != b.Size || a.effectiveWeight() != b.effectiveWeight() ||
		a.Italic != b.Italic || a.SmallCaps != b.SmallCaps {
		return false
	}
	if len(a.Features) != len(b.Features) {
		return false
	}
	if len(a.Features) > 0 {
		aa := append([]string(nil), a.Features...)
		bb := append([]string(nil), b.Features...)
		sort.Strings(aa)
		sort.Strings(bb)
		for i := range aa {
			if aa[i] != bb[i] {
				return false
			}
		}
	}
	if len(a.Variations) != len(b.Variations) {
		return false
	}
	for k, v := range a.Variations {
		if bv, ok := b.Variations[k]; !ok || bv != v {
			return false
		}
	}
	return true
}
