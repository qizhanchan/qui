package htmlcss

import (
	"strings"

	"github.com/qizhanchan/qui"
)

// Helpers for web APIs that take CSS values outside a stylesheet — a
// canvas's fillStyle / font.

// ParseColor parses a CSS <color> (named, #hex, rgb(), hsl(), color(),
// color-mix(), transparent). currentcolor is not resolved: ok is false.
func ParseColor(s string) (qui.Color, bool) {
	if strings.EqualFold(strings.TrimSpace(s), "currentcolor") {
		return qui.Color{}, false
	}
	return parseColor(s)
}

// ParseFont parses the CSS `font` shorthand as CanvasRenderingContext2D.font
// does: em and % sizes resolve against emBase, and the family list picks
// the first installed family. ok is false for a value that is not a valid
// shorthand (no size), which a canvas ignores.
func ParseFont(v string, emBase float32) (qui.Font, bool) {
	m := map[string]string{}
	expandFont(v, m)
	size, ok := m["font-size"]
	if !ok {
		return qui.Font{}, false
	}
	px, ok := parseLength(size, emBase, emBase)
	if !ok || px <= 0 {
		return qui.Font{}, false
	}
	f := qui.Font{Size: px, Weight: qui.FontWeightNormal}
	if w, ok := m["font-weight"]; ok {
		f.Weight = parseFontWeight(w, qui.FontWeightNormal)
	}
	f.Italic = m["font-style"] == "italic"
	if fam, ok := m["font-family"]; ok {
		f.Family = firstFontFamily(fam)
	}
	return f, true
}
