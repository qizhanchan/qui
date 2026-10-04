package htmlcss

import (
	"strconv"
	"strings"

	"github.com/qizhanchan/qui"
)

// namedColorHex is the full CSS named-color set (CSS Color Level 4
// <named-color>, including the gray/grey aliases), as 0xRRGGBB values.
var namedColorHex = map[string]uint32{
	"aliceblue": 0xF0F8FF, "antiquewhite": 0xFAEBD7, "aqua": 0x00FFFF,
	"aquamarine": 0x7FFFD4, "azure": 0xF0FFFF, "beige": 0xF5F5DC,
	"bisque": 0xFFE4C4, "black": 0x000000, "blanchedalmond": 0xFFEBCD,
	"blue": 0x0000FF, "blueviolet": 0x8A2BE2, "brown": 0xA52A2A,
	"burlywood": 0xDEB887, "cadetblue": 0x5F9EA0, "chartreuse": 0x7FFF00,
	"chocolate": 0xD2691E, "coral": 0xFF7F50, "cornflowerblue": 0x6495ED,
	"cornsilk": 0xFFF8DC, "crimson": 0xDC143C, "cyan": 0x00FFFF,
	"darkblue": 0x00008B, "darkcyan": 0x008B8B, "darkgoldenrod": 0xB8860B,
	"darkgray": 0xA9A9A9, "darkgreen": 0x006400, "darkgrey": 0xA9A9A9,
	"darkkhaki": 0xBDB76B, "darkmagenta": 0x8B008B, "darkolivegreen": 0x556B2F,
	"darkorange": 0xFF8C00, "darkorchid": 0x9932CC, "darkred": 0x8B0000,
	"darksalmon": 0xE9967A, "darkseagreen": 0x8FBC8F, "darkslateblue": 0x483D8B,
	"darkslategray": 0x2F4F4F, "darkslategrey": 0x2F4F4F, "darkturquoise": 0x00CED1,
	"darkviolet": 0x9400D3, "deeppink": 0xFF1493, "deepskyblue": 0x00BFFF,
	"dimgray": 0x696969, "dimgrey": 0x696969, "dodgerblue": 0x1E90FF,
	"firebrick": 0xB22222, "floralwhite": 0xFFFAF0, "forestgreen": 0x228B22,
	"fuchsia": 0xFF00FF, "gainsboro": 0xDCDCDC, "ghostwhite": 0xF8F8FF,
	"gold": 0xFFD700, "goldenrod": 0xDAA520, "gray": 0x808080,
	"green": 0x008000, "greenyellow": 0xADFF2F, "grey": 0x808080,
	"honeydew": 0xF0FFF0, "hotpink": 0xFF69B4, "indianred": 0xCD5C5C,
	"indigo": 0x4B0082, "ivory": 0xFFFFF0, "khaki": 0xF0E68C,
	"lavender": 0xE6E6FA, "lavenderblush": 0xFFF0F5, "lawngreen": 0x7CFC00,
	"lemonchiffon": 0xFFFACD, "lightblue": 0xADD8E6, "lightcoral": 0xF08080,
	"lightcyan": 0xE0FFFF, "lightgoldenrodyellow": 0xFAFAD2, "lightgray": 0xD3D3D3,
	"lightgreen": 0x90EE90, "lightgrey": 0xD3D3D3, "lightpink": 0xFFB6C1,
	"lightsalmon": 0xFFA07A, "lightseagreen": 0x20B2AA, "lightskyblue": 0x87CEFA,
	"lightslategray": 0x778899, "lightslategrey": 0x778899, "lightsteelblue": 0xB0C4DE,
	"lightyellow": 0xFFFFE0, "lime": 0x00FF00, "limegreen": 0x32CD32,
	"linen": 0xFAF0E6, "magenta": 0xFF00FF, "maroon": 0x800000,
	"mediumaquamarine": 0x66CDAA, "mediumblue": 0x0000CD, "mediumorchid": 0xBA55D3,
	"mediumpurple": 0x9370DB, "mediumseagreen": 0x3CB371, "mediumslateblue": 0x7B68EE,
	"mediumspringgreen": 0x00FA9A, "mediumturquoise": 0x48D1CC, "mediumvioletred": 0xC71585,
	"midnightblue": 0x191970, "mintcream": 0xF5FFFA, "mistyrose": 0xFFE4E1,
	"moccasin": 0xFFE4B5, "navajowhite": 0xFFDEAD, "navy": 0x000080,
	"oldlace": 0xFDF5E6, "olive": 0x808000, "olivedrab": 0x6B8E23,
	"orange": 0xFFA500, "orangered": 0xFF4500, "orchid": 0xDA70D6,
	"palegoldenrod": 0xEEE8AA, "palegreen": 0x98FB98, "paleturquoise": 0xAFEEEE,
	"palevioletred": 0xDB7093, "papayawhip": 0xFFEFD5, "peachpuff": 0xFFDAB9,
	"peru": 0xCD853F, "pink": 0xFFC0CB, "plum": 0xDDA0DD,
	"powderblue": 0xB0E0E6, "purple": 0x800080, "rebeccapurple": 0x663399,
	"red": 0xFF0000, "rosybrown": 0xBC8F8F, "royalblue": 0x4169E1,
	"saddlebrown": 0x8B4513, "salmon": 0xFA8072, "sandybrown": 0xF4A460,
	"seagreen": 0x2E8B57, "seashell": 0xFFF5EE, "sienna": 0xA0522D,
	"silver": 0xC0C0C0, "skyblue": 0x87CEEB, "slateblue": 0x6A5ACD,
	"slategray": 0x708090, "slategrey": 0x708090, "snow": 0xFFFAFA,
	"springgreen": 0x00FF7F, "steelblue": 0x4682B4, "tan": 0xD2B48C,
	"teal": 0x008080, "thistle": 0xD8BFD8, "tomato": 0xFF6347,
	"turquoise": 0x40E0D0, "violet": 0xEE82EE, "wheat": 0xF5DEB3,
	"white": 0xFFFFFF, "whitesmoke": 0xF5F5F5, "yellow": 0xFFFF00,
	"yellowgreen": 0x9ACD32,
}

// namedColors maps every CSS named color (plus `transparent`) to a
// qui.Color; built once from namedColorHex.
var namedColors = func() map[string]qui.Color {
	m := make(map[string]qui.Color, len(namedColorHex)+1)
	for name, hex := range namedColorHex {
		m[name] = qui.Color{
			R: float32((hex>>16)&0xFF) / 255,
			G: float32((hex>>8)&0xFF) / 255,
			B: float32(hex&0xFF) / 255,
			A: 1,
		}
	}
	m["transparent"] = qui.Color{}
	return m
}()

// parseColor parses a CSS color into a qui.Color. ok is false when the
// value is unrecognized (caller keeps the inherited/default color).
func parseColor(s string) (qui.Color, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return qui.Color{}, false
	}
	if c, ok := namedColors[s]; ok {
		return c, true
	}
	if strings.HasPrefix(s, "#") {
		return parseHexColor(s)
	}
	if strings.HasPrefix(s, "rgb") {
		return parseRGBColor(s)
	}
	if strings.HasPrefix(s, "hsl") {
		return parseHSLColor(s)
	}
	return qui.Color{}, false
}

// parseHSLColor parses hsl()/hsla() — `hsl(h, s%, l%)` or the space form
// `hsl(h s% l% / a)`. Hue is degrees (unitless or with a `deg` suffix),
// saturation/lightness are percentages, alpha 0–1 or a percentage.
func parseHSLColor(s string) (qui.Color, bool) {
	open := strings.IndexByte(s, '(')
	closeI := strings.LastIndexByte(s, ')')
	if open < 0 || closeI < 0 || closeI < open {
		return qui.Color{}, false
	}
	fields := strings.FieldsFunc(s[open+1:closeI], func(r rune) bool {
		return r == ',' || r == ' ' || r == '/'
	})
	if len(fields) < 3 {
		return qui.Color{}, false
	}
	h, ok := parseHue(fields[0])
	if !ok {
		return qui.Color{}, false
	}
	sat, ok := parsePercentage(fields[1])
	if !ok {
		return qui.Color{}, false
	}
	light, ok := parsePercentage(fields[2])
	if !ok {
		return qui.Color{}, false
	}
	a := float32(1)
	if len(fields) >= 4 {
		if av, aok := parseAlpha(fields[3]); aok {
			a = av
		}
	}
	r, g, b := hslToRGB(h, sat, light)
	return qui.Color{R: r, G: g, B: b, A: a}, true
}

// parseHue parses an hsl hue: a number optionally suffixed `deg`, normalized
// into [0,360).
func parseHue(s string) (float32, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "deg")
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 32)
	if err != nil {
		return 0, false
	}
	h := float32(v)
	h = float32(int(h) % 360)
	if h < 0 {
		h += 360
	}
	return h, true
}

// parsePercentage parses "50%" into a 0..1 fraction (clamped).
func parsePercentage(s string) (float32, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasSuffix(s, "%") {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 32)
	if err != nil {
		return 0, false
	}
	return clamp01(float32(v) / 100), true
}

// parseAlpha parses an alpha channel — a 0..1 number or a percentage.
func parseAlpha(s string) (float32, bool) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "%") {
		return parsePercentage(s)
	}
	v, err := strconv.ParseFloat(s, 32)
	if err != nil {
		return 0, false
	}
	return clamp01(float32(v)), true
}

// hslToRGB converts HSL (h in degrees, s/l in 0..1) to RGB in 0..1.
func hslToRGB(h, s, l float32) (r, g, b float32) {
	c := (1 - absf(2*l-1)) * s
	hp := h / 60
	x := c * (1 - absf(mod2(hp)-1))
	var r1, g1, b1 float32
	switch {
	case hp < 1:
		r1, g1, b1 = c, x, 0
	case hp < 2:
		r1, g1, b1 = x, c, 0
	case hp < 3:
		r1, g1, b1 = 0, c, x
	case hp < 4:
		r1, g1, b1 = 0, x, c
	case hp < 5:
		r1, g1, b1 = x, 0, c
	default:
		r1, g1, b1 = c, 0, x
	}
	m := l - c/2
	return r1 + m, g1 + m, b1 + m
}

// mod2 returns v mod 2 in [0,2) for the hslToRGB chroma helper.
func mod2(v float32) float32 {
	r := v - 2*float32(int(v/2))
	if r < 0 {
		r += 2
	}
	return r
}

func parseHexColor(s string) (qui.Color, bool) {
	h := strings.TrimPrefix(s, "#")
	switch len(h) {
	case 3: // #rgb
		r := hexNibble(h[0])
		g := hexNibble(h[1])
		b := hexNibble(h[2])
		return qui.Color{R: float32(r*17) / 255, G: float32(g*17) / 255, B: float32(b*17) / 255, A: 1}, true
	case 6: // #rrggbb
		r, ok1 := hexByte(h[0:2])
		g, ok2 := hexByte(h[2:4])
		b, ok3 := hexByte(h[4:6])
		if ok1 && ok2 && ok3 {
			return qui.Color{R: float32(r) / 255, G: float32(g) / 255, B: float32(b) / 255, A: 1}, true
		}
	case 8: // #rrggbbaa
		r, ok1 := hexByte(h[0:2])
		g, ok2 := hexByte(h[2:4])
		b, ok3 := hexByte(h[4:6])
		a, ok4 := hexByte(h[6:8])
		if ok1 && ok2 && ok3 && ok4 {
			return qui.Color{R: float32(r) / 255, G: float32(g) / 255, B: float32(b) / 255, A: float32(a) / 255}, true
		}
	}
	return qui.Color{}, false
}

func parseRGBColor(s string) (qui.Color, bool) {
	open := strings.IndexByte(s, '(')
	closeI := strings.IndexByte(s, ')')
	if open < 0 || closeI < 0 || closeI < open {
		return qui.Color{}, false
	}
	fields := strings.FieldsFunc(s[open+1:closeI], func(r rune) bool {
		return r == ',' || r == ' ' || r == '/'
	})
	if len(fields) < 3 {
		return qui.Color{}, false
	}
	r := parseColorComponent(fields[0])
	g := parseColorComponent(fields[1])
	b := parseColorComponent(fields[2])
	a := float32(1)
	if len(fields) >= 4 {
		if v, err := strconv.ParseFloat(strings.TrimSpace(fields[3]), 32); err == nil {
			a = float32(v)
		}
	}
	return qui.Color{R: r, G: g, B: b, A: a}, true
}

func parseColorComponent(s string) float32 {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "%") {
		if v, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 32); err == nil {
			return clamp01(float32(v) / 100)
		}
		return 0
	}
	if v, err := strconv.ParseFloat(s, 32); err == nil {
		return clamp01(float32(v) / 255)
	}
	return 0
}

func hexNibble(b byte) int {
	v, _ := hexByte(string([]byte{b}))
	return v
}

func hexByte(s string) (int, bool) {
	v, err := strconv.ParseInt(s, 16, 32)
	if err != nil {
		return 0, false
	}
	return int(v), true
}

func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// parseLength converts a CSS length to logical pixels. em is relative to
// the supplied font size; rem uses 16px; % returns a fraction of pct
// (caller passes the reference). Returns ok=false for unparseable/auto.
func parseLength(s string, fontSize, pctRef float32) (float32, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "auto" || s == "none" {
		return 0, false
	}
	if strings.HasPrefix(s, "calc(") {
		return parseCalc(s, fontSize, pctRef)
	}
	if strings.HasPrefix(s, "min(") || strings.HasPrefix(s, "max(") || strings.HasPrefix(s, "clamp(") {
		return parseMinMaxClamp(s, fontSize, pctRef)
	}
	switch {
	case strings.HasSuffix(s, "px"):
		return parseFloat(strings.TrimSuffix(s, "px"))
	case strings.HasSuffix(s, "rem"):
		if v, ok := parseFloat(strings.TrimSuffix(s, "rem")); ok {
			return v * 16, true
		}
	case strings.HasSuffix(s, "em"):
		if v, ok := parseFloat(strings.TrimSuffix(s, "em")); ok {
			if fontSize <= 0 {
				fontSize = 16
			}
			return v * fontSize, true
		}
	case strings.HasSuffix(s, "pt"):
		if v, ok := parseFloat(strings.TrimSuffix(s, "pt")); ok {
			return v * 96 / 72, true
		}
	case strings.HasSuffix(s, "%"):
		if v, ok := parseFloat(strings.TrimSuffix(s, "%")); ok {
			return v / 100 * pctRef, true
		}
	default:
		// Unitless number → treat as px.
		return parseFloat(s)
	}
	return 0, false
}

// parseMinMaxClamp evaluates the CSS math functions min()/max()/clamp() to
// logical pixels. Each argument is resolved via parseLength (so calc()/px/%/em
// nest freely). clamp(min, val, max) returns val clamped to [min, max]. Any
// unparseable argument fails the whole function.
func parseMinMaxClamp(s string, fontSize, pctRef float32) (float32, bool) {
	open := strings.IndexByte(s, '(')
	if open < 0 || !strings.HasSuffix(s, ")") {
		return 0, false
	}
	fn := s[:open]
	args := splitTopLevelCommas(s[open+1 : len(s)-1])
	vals := make([]float32, 0, len(args))
	for _, a := range args {
		v, ok := parseLength(strings.TrimSpace(a), fontSize, pctRef)
		if !ok {
			return 0, false
		}
		vals = append(vals, v)
	}
	switch fn {
	case "min":
		if len(vals) == 0 {
			return 0, false
		}
		m := vals[0]
		for _, v := range vals[1:] {
			if v < m {
				m = v
			}
		}
		return m, true
	case "max":
		if len(vals) == 0 {
			return 0, false
		}
		m := vals[0]
		for _, v := range vals[1:] {
			if v > m {
				m = v
			}
		}
		return m, true
	case "clamp":
		if len(vals) != 3 {
			return 0, false
		}
		lo, val, hi := vals[0], vals[1], vals[2]
		if val < lo {
			val = lo
		}
		if val > hi {
			val = hi
		}
		return val, true
	}
	return 0, false
}

// splitTopLevelCommas splits on commas that are not nested inside parentheses,
// so nested calc()/min()/… arguments stay intact.
func splitTopLevelCommas(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

// parseCalc evaluates a CSS calc() expression to logical pixels. Each length
// token is resolved via parseLength (so px/em/rem/%/unitless all work and mix
// freely), then combined with + - * / and parentheses at normal precedence.
// Nested calc() is accepted (each "calc(" is treated as an opening paren).
// Division by zero or any parse/structure error returns ok=false.
func parseCalc(s string, fontSize, pctRef float32) (float32, bool) {
	expr := strings.ReplaceAll(s, "calc(", "(")
	p := &calcParser{src: expr, fontSize: fontSize, pctRef: pctRef}
	v, ok := p.expr()
	if !ok {
		return 0, false
	}
	p.skipSpace()
	if p.pos != len(p.src) {
		return 0, false
	}
	return v, true
}

type calcParser struct {
	src              string
	pos              int
	fontSize, pctRef float32
}

func (p *calcParser) skipSpace() {
	for p.pos < len(p.src) && p.src[p.pos] == ' ' {
		p.pos++
	}
}

func (p *calcParser) expr() (float32, bool) {
	v, ok := p.term()
	if !ok {
		return 0, false
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			break
		}
		op := p.src[p.pos]
		if op != '+' && op != '-' {
			break
		}
		p.pos++
		rhs, ok := p.term()
		if !ok {
			return 0, false
		}
		if op == '+' {
			v += rhs
		} else {
			v -= rhs
		}
	}
	return v, true
}

func (p *calcParser) term() (float32, bool) {
	v, ok := p.factor()
	if !ok {
		return 0, false
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			break
		}
		op := p.src[p.pos]
		if op != '*' && op != '/' {
			break
		}
		p.pos++
		rhs, ok := p.factor()
		if !ok {
			return 0, false
		}
		if op == '*' {
			v *= rhs
		} else {
			if rhs == 0 {
				return 0, false
			}
			v /= rhs
		}
	}
	return v, true
}

func (p *calcParser) factor() (float32, bool) {
	p.skipSpace()
	if p.pos >= len(p.src) {
		return 0, false
	}
	if p.src[p.pos] == '(' {
		p.pos++
		v, ok := p.expr()
		if !ok {
			return 0, false
		}
		p.skipSpace()
		if p.pos >= len(p.src) || p.src[p.pos] != ')' {
			return 0, false
		}
		p.pos++
		return v, true
	}
	start := p.pos
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case '+', '-', '*', '/', '(', ')', ' ':
			goto done
		}
		p.pos++
	}
done:
	tok := p.src[start:p.pos]
	if tok == "" {
		return 0, false
	}
	return parseLength(tok, p.fontSize, p.pctRef)
}

// parseCSSURL extracts the target of a CSS `url(...)` value, stripping optional
// single/double quotes. Returns ok=false when v isn't a url() token.
func parseCSSURL(v string) (string, bool) {
	v = strings.TrimSpace(v)
	low := strings.ToLower(v)
	i := strings.Index(low, "url(")
	if i < 0 {
		return "", false
	}
	rest := v[i+len("url("):]
	j := strings.IndexByte(rest, ')')
	if j < 0 {
		return "", false
	}
	u := strings.TrimSpace(rest[:j])
	u = strings.Trim(u, `"'`)
	if u == "" {
		return "", false
	}
	return u, true
}

// parseFloatOr parses s as a float, returning def when empty/unparseable.
func parseFloatOr(s string, def float32) float32 {
	if v, ok := parseFloat(s); ok {
		return v
	}
	return def
}

func parseFloat(s string) (float32, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 32)
	if err != nil {
		return 0, false
	}
	return float32(v), true
}
