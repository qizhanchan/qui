package gfx

import (
	"sort"
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

// MergeFont overlays override on base field-wise. Unlike simple types
// where the whole Style field is replaced atomically, Font is composite
// (family / size / weight / italic / features / variations) and callers
// often want to override just size or just weight while inheriting the
// family from the theme. Zero on a scalar or "" on family means "keep
// base". Features / Variations slices/maps are replaced atomically
// when override supplies a non-empty one.
func MergeFont(base, override Font) Font {
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

func (f Font) EffectiveWeight() FontWeight {
	if f.Weight != 0 {
		return f.Weight
	}
	if f.Bold {
		return FontWeightBold
	}
	return FontWeightNormal
}

func FontStyleEqual(a, b Font) bool {
	if a.Family != b.Family || a.Size != b.Size || a.EffectiveWeight() != b.EffectiveWeight() ||
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
