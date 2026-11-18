package htmlcss

import (
	"math"
	"strconv"
	"strings"

	"github.com/qizhanchan/qui"
)

// CSS Color 4/5 functions beyond rgb()/hsl(): hwb(), lab(), lch(),
// oklab(), oklch(), color-mix() and light-dark(). Colors are produced in
// sRGB (clamped), which is what the renderer paints.

// resolveLightDark rewrites light-dark(a, b) to its light branch: qui
// documents render with color-scheme: light. Done textually at parse
// time so shorthands (`background: light-dark(...)`) see a plain color.
func resolveLightDark(val string) string {
	for {
		low := strings.ToLower(val)
		i := strings.Index(low, "light-dark(")
		if i < 0 {
			return val
		}
		open := i + len("light-dark")
		closeI := matchParen(val, open)
		if closeI < 0 {
			return val
		}
		args := splitTopLevelCommas(val[open+1 : closeI])
		light := ""
		if len(args) > 0 {
			light = strings.TrimSpace(args[0])
		}
		val = val[:i] + light + val[closeI+1:]
	}
}

// matchParen returns the index of the ')' closing the '(' at open.
func matchParen(s string, open int) int {
	depth := 0
	for j := open; j < len(s); j++ {
		switch s[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

// parseColor4 handles the functions parseColor does not.
func parseColor4(s string) (qui.Color, bool) {
	open := strings.IndexByte(s, '(')
	if open < 0 || !strings.HasSuffix(s, ")") {
		return qui.Color{}, false
	}
	fn := s[:open]
	body := s[open+1 : len(s)-1]
	if fn == "color-mix" {
		return parseColorMix(body)
	}
	if fn == "light-dark" {
		args := splitTopLevelCommas(body)
		if len(args) == 2 {
			return parseColor(strings.TrimSpace(args[0]))
		}
		return qui.Color{}, false
	}
	comps, alpha, ok := splitColorArgs(body)
	if !ok || len(comps) != 3 {
		return qui.Color{}, false
	}
	var r, g, b float64
	switch fn {
	case "hwb":
		h := hueDeg(comps[0])
		w, bl := pct(comps[1], 1), pct(comps[2], 1)
		r, g, b = hwbToRGB(h, w, bl)
	case "lab":
		L, A, B := num(comps[0], 100), num(comps[1], 125), num(comps[2], 125)
		r, g, b = labToSRGB(L, A, B)
	case "lch":
		L, C, H := num(comps[0], 100), num(comps[1], 150), hueDeg(comps[2])
		r, g, b = labToSRGB(L, C*math.Cos(H*math.Pi/180), C*math.Sin(H*math.Pi/180))
	case "oklab":
		L, A, B := num(comps[0], 1), num(comps[1], 0.4), num(comps[2], 0.4)
		r, g, b = oklabToSRGB(L, A, B)
	case "oklch":
		L, C, H := num(comps[0], 1), num(comps[1], 0.4), hueDeg(comps[2])
		r, g, b = oklabToSRGB(L, C*math.Cos(H*math.Pi/180), C*math.Sin(H*math.Pi/180))
	default:
		return qui.Color{}, false
	}
	return qui.Color{R: clamp01(float32(r)), G: clamp01(float32(g)), B: clamp01(float32(b)), A: alpha}, true
}

// splitColorArgs splits `a b c / alpha` (or comma-separated) arguments.
func splitColorArgs(body string) (comps []string, alpha float32, ok bool) {
	alpha = 1
	main := body
	if i := strings.IndexByte(body, '/'); i >= 0 {
		main = body[:i]
		a := strings.TrimSpace(body[i+1:])
		alpha = float32(pct(a, 1))
	}
	comps = strings.FieldsFunc(main, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' })
	if len(comps) == 4 && !strings.Contains(body, "/") {
		alpha = float32(pct(comps[3], 1))
		comps = comps[:3]
	}
	return comps, clamp01(alpha), len(comps) == 3
}

// num parses a number or a percentage of pctScale ("none" = 0).
func num(s string, pctScale float64) float64 {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "%") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		return v / 100 * pctScale
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// pct parses a 0..1 number or a percentage into 0..scale.
func pct(s string, scale float64) float64 {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "%") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		return v / 100 * scale
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return scale
	}
	return v * scale
}

func hueDeg(s string) float64 {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasSuffix(s, "deg"):
		s = strings.TrimSuffix(s, "deg")
	case strings.HasSuffix(s, "turn"):
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "turn"), 64)
		return v * 360
	case strings.HasSuffix(s, "rad"):
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "rad"), 64)
		return v * 180 / math.Pi
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func hwbToRGB(h, w, b float64) (float64, float64, float64) {
	if w+b >= 1 {
		g := w / (w + b)
		return g, g, g
	}
	r, g, bl := hslToRGBf(h, 1, 0.5)
	f := func(c float64) float64 { return c*(1-w-b) + w }
	return f(r), f(g), f(bl)
}

func hslToRGBf(h, s, l float64) (float64, float64, float64) {
	h = math.Mod(math.Mod(h, 360)+360, 360) / 360
	f := func(n float64) float64 {
		k := math.Mod(n+h*12, 12)
		a := s * math.Min(l, 1-l)
		return l - a*math.Max(-1, math.Min(math.Min(k-3, 9-k), 1))
	}
	return f(0), f(8), f(4)
}

func linearToSRGB(c float64) float64 {
	if c <= 0.0031308 {
		return 12.92 * c
	}
	return 1.055*math.Pow(c, 1/2.4) - 0.055
}

func oklabToSRGB(L, a, b float64) (float64, float64, float64) {
	l := L + 0.3963377774*a + 0.2158037573*b
	m := L - 0.1055613458*a - 0.0638541728*b
	s := L - 0.0894841775*a - 1.2914855480*b
	l, m, s = l*l*l, m*m*m, s*s*s
	r := 4.0767416621*l - 3.3077115913*m + 0.2309699292*s
	g := -1.2684380046*l + 2.6097574011*m - 0.3413193965*s
	bl := -0.0041960863*l - 0.7034186147*m + 1.7076147010*s
	return linearToSRGB(r), linearToSRGB(g), linearToSRGB(bl)
}

// labToSRGB converts CIE Lab (D50) to sRGB.
func labToSRGB(L, a, b float64) (float64, float64, float64) {
	fy := (L + 16) / 116
	fx := fy + a/500
	fz := fy - b/200
	inv := func(t float64) float64 {
		if t*t*t > 0.008856 {
			return t * t * t
		}
		return (116*t - 16) / 903.3
	}
	x, y, z := 0.96422*inv(fx), 1.0*inv(fy), 0.82521*inv(fz)
	// D50 → D65 (Bradford), then XYZ → linear sRGB.
	x2 := 0.9555766*x - 0.0230393*y + 0.0631636*z
	y2 := -0.0282895*x + 1.0099416*y + 0.0210077*z
	z2 := 0.0122982*x - 0.0204830*y + 1.3299098*z
	r := 3.2404542*x2 - 1.5371385*y2 - 0.4985314*z2
	g := -0.9692660*x2 + 1.8760108*y2 + 0.0415560*z2
	bl := 0.0556434*x2 - 0.2040259*y2 + 1.0572252*z2
	return linearToSRGB(r), linearToSRGB(g), linearToSRGB(bl)
}

// parseColorMix handles `color-mix(in <space>, a [p%], b [p%])`,
// interpolating in sRGB whatever space is named (close enough for UI
// tints; exact for srgb).
func parseColorMix(body string) (qui.Color, bool) {
	args := splitTopLevelCommas(body)
	if len(args) != 3 {
		return qui.Color{}, false
	}
	type part struct {
		c  qui.Color
		p  float64
		ok bool
	}
	parse := func(s string) part {
		s = strings.TrimSpace(s)
		p := -1.0
		if i := strings.LastIndexByte(s, ' '); i > 0 && strings.HasSuffix(s, "%") {
			if v, err := strconv.ParseFloat(strings.TrimSuffix(s[i+1:], "%"), 64); err == nil {
				p = v / 100
				s = strings.TrimSpace(s[:i])
			}
		}
		c, ok := parseColor(s)
		return part{c, p, ok}
	}
	a, b := parse(args[1]), parse(args[2])
	if !a.ok || !b.ok {
		return qui.Color{}, false
	}
	switch {
	case a.p < 0 && b.p < 0:
		a.p, b.p = 0.5, 0.5
	case a.p < 0:
		a.p = 1 - b.p
	case b.p < 0:
		b.p = 1 - a.p
	}
	sum := a.p + b.p
	if sum <= 0 {
		return qui.Color{}, false
	}
	wa, wb := float32(a.p/sum), float32(b.p/sum)
	// Premultiplied interpolation, as CSS specifies.
	alpha := a.c.A*wa + b.c.A*wb
	mix := func(x, y float32) float32 {
		if alpha == 0 {
			return 0
		}
		return (x*a.c.A*wa + y*b.c.A*wb) / alpha
	}
	out := qui.Color{R: mix(a.c.R, b.c.R), G: mix(a.c.G, b.c.G), B: mix(a.c.B, b.c.B), A: alpha}
	if sum < 1 {
		out.A *= float32(sum)
	}
	return out, true
}
