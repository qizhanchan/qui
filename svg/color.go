package svg

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/qizhanchan/qui"
	"golang.org/x/image/colornames"
)

// parsePaint accepts an SVG paint string (fill / stroke attribute or
// declaration value) and turns it into a Paint. Recognised forms:
//
//	"none"                       → PaintNone
//	"inherit" / "currentColor"   → PaintInherit (we treat currentColor
//	                                as inherit because v1 has no
//	                                "current color" concept)
//	"#rgb" / "#rrggbb"           → PaintSolid
//	"rgb(R,G,B)" / "rgba(R,G,B,A)" with int / float / percent components
//	any name from CSS named colors (via golang.org/x/image/colornames)
//
// Unknown strings return an error rather than silently defaulting so
// typos surface during parse.
func parsePaint(s string) (Paint, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Paint{}, nil
	}
	switch strings.ToLower(s) {
	case "none":
		return Paint{Kind: PaintNone}, nil
	case "inherit", "currentcolor":
		return Paint{Kind: PaintInherit}, nil
	}
	c, err := parseColor(s)
	if err != nil {
		return Paint{}, err
	}
	return Paint{Kind: PaintSolid, Color: c}, nil
}

// parseColor handles just the color-literal cases (no "none" /
// "inherit"). Called by parsePaint and also by anywhere we need a
// concrete color (e.g. dashoffset markers — though none in v1).
func parseColor(s string) (qui.Color, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return qui.Color{}, fmt.Errorf("empty color")
	}
	if s[0] == '#' {
		return parseHexColor(s)
	}
	low := strings.ToLower(s)
	if strings.HasPrefix(low, "rgb(") || strings.HasPrefix(low, "rgba(") {
		return parseRGBColor(low)
	}
	// Try CSS named color. colornames.Map is keyed lowercase.
	if c, ok := colornames.Map[strings.ToLower(s)]; ok {
		return qui.Color{
			R: float32(c.R) / 255,
			G: float32(c.G) / 255,
			B: float32(c.B) / 255,
			A: float32(c.A) / 255,
		}, nil
	}
	return qui.Color{}, fmt.Errorf("unknown color %q", s)
}

func parseHexColor(s string) (qui.Color, error) {
	body := s[1:]
	switch len(body) {
	case 3, 4:
		// #rgb or #rgba — each nibble doubles to a full byte (#abc → #aabbcc)
		var rgba [4]float32
		rgba[3] = 1
		for i, ch := range body {
			n, err := hexNibble(byte(ch))
			if err != nil {
				return qui.Color{}, err
			}
			rgba[i] = float32(n*16+n) / 255
		}
		return qui.Color{R: rgba[0], G: rgba[1], B: rgba[2], A: rgba[3]}, nil
	case 6, 8:
		var rgba [4]float32
		rgba[3] = 1
		for i := 0; i < len(body); i += 2 {
			hi, err := hexNibble(body[i])
			if err != nil {
				return qui.Color{}, err
			}
			lo, err := hexNibble(body[i+1])
			if err != nil {
				return qui.Color{}, err
			}
			rgba[i/2] = float32(hi*16+lo) / 255
		}
		return qui.Color{R: rgba[0], G: rgba[1], B: rgba[2], A: rgba[3]}, nil
	}
	return qui.Color{}, fmt.Errorf("bad hex color %q", s)
}

func hexNibble(b byte) (int, error) {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0'), nil
	case b >= 'a' && b <= 'f':
		return int(b-'a') + 10, nil
	case b >= 'A' && b <= 'F':
		return int(b-'A') + 10, nil
	}
	return 0, fmt.Errorf("bad hex digit %q", b)
}

func parseRGBColor(s string) (qui.Color, error) {
	open := strings.IndexByte(s, '(')
	close := strings.LastIndexByte(s, ')')
	if open < 0 || close < 0 || close < open {
		return qui.Color{}, fmt.Errorf("malformed rgb() %q", s)
	}
	parts := splitCommaOrSpace(s[open+1 : close])
	if len(parts) != 3 && len(parts) != 4 {
		return qui.Color{}, fmt.Errorf("rgb() needs 3 or 4 components, got %d", len(parts))
	}
	out := qui.Color{A: 1}
	dst := []*float32{&out.R, &out.G, &out.B, &out.A}
	for i, p := range parts {
		v, err := parseColorComponent(p, i == 3)
		if err != nil {
			return qui.Color{}, err
		}
		*dst[i] = v
	}
	return out, nil
}

// parseColorComponent handles "128", "50%", or "0.5" — the alpha
// flag is true when the component is in rgba's 4th slot, in which
// case bare numbers are [0,1] (CSS legacy form treats alpha that
// way), and integers / percents follow the same scaling.
func parseColorComponent(tok string, alpha bool) (float32, error) {
	tok = strings.TrimSpace(tok)
	if strings.HasSuffix(tok, "%") {
		v, err := strconv.ParseFloat(strings.TrimSuffix(tok, "%"), 32)
		if err != nil {
			return 0, err
		}
		return clampUnit(float32(v) / 100), nil
	}
	v, err := strconv.ParseFloat(tok, 32)
	if err != nil {
		return 0, err
	}
	if alpha {
		return clampUnit(float32(v)), nil
	}
	return clampUnit(float32(v) / 255), nil
}

func splitCommaOrSpace(s string) []string {
	var out []string
	start := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case ' ', '\t', '\n', '\r', ',', '/':
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
		default:
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

// formatColor renders a qui.Color as the most compact SVG color string
// — hex when alpha is 1, rgba() otherwise. Used by the serializer; not
// performance-critical.
func formatColor(c qui.Color) string {
	r := byte(clampUnit(c.R)*255 + 0.5)
	g := byte(clampUnit(c.G)*255 + 0.5)
	b := byte(clampUnit(c.B)*255 + 0.5)
	if c.A >= 1 {
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	}
	return fmt.Sprintf("rgba(%d,%d,%d,%s)", r, g, b, fmtNum(clampUnit(c.A)))
}
