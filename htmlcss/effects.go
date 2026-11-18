package htmlcss

import (
	"math"
	"strings"

	"github.com/qizhanchan/qui"
)

// This file parses the "visual" CSS effects (Wave 1): box-shadow,
// opacity, transform, and linear-gradient backgrounds. They compile into
// qui.Style fields / widgets.Box fields at build time — see build.go.

// transformSpec is a parsed CSS `transform` (translate/rotate/scale),
// accumulated across the space-separated function list.
type transformSpec struct {
	tx, ty    float32
	rotate    float32 // radians
	sx, sy    float32 // scale (1 = identity)
	kx, ky    float32 // skew (radians)
	matrix    *qui.Matrix
	ox, oy    float32 // transform-origin fraction (0..1)
	hasOrigin bool
	// ops is the function list in source order; when set it is the
	// transform (the decomposed fields above stay identity). Percentages
	// in translate resolve against the border box at paint time.
	ops []transformOp
}

// transformOp is one transform function.
type transformOp struct {
	kind       byte // 't' translate, 's' scale, 'r' rotate, 'k' skew, 'm' matrix
	a, b       float32
	aPct, bPct float32 // translate percentages (fractions of the box)
	m          qui.Matrix
	// 'L': an interpolation between two op lists at lerpT (animations),
	// resolved per box size.
	lerpA, lerpB []transformOp
	lerpT        float32
}

// resolve composes ops for a w×h box into one matrix about the origin.
func resolveTransformOps(ops []transformOp, w, h float32) qui.Matrix {
	m := qui.IdentityMatrix()
	for _, op := range ops {
		var n qui.Matrix
		switch op.kind {
		case 't':
			n = qui.TranslateMatrix(op.a+op.aPct*w, op.b+op.bPct*h)
		case 's':
			n = qui.ScaleMatrix(op.a, op.b)
		case 'r':
			n = qui.RotateMatrix(op.a)
		case 'k':
			n = qui.Matrix{A: 1, B: float32(math.Tan(float64(op.a))), C: float32(math.Tan(float64(op.b))), D: 1}
		case 'm':
			n = op.m
		case 'L':
			a, b := resolveTransformOps(op.lerpA, w, h), resolveTransformOps(op.lerpB, w, h)
			t := op.lerpT
			l := func(x, y float32) float32 { return x + (y-x)*t }
			n = qui.Matrix{A: l(a.A, b.A), B: l(a.B, b.B), C: l(a.C, b.C), D: l(a.D, b.D), TX: l(a.TX, b.TX), TY: l(a.TY, b.TY)}
		default:
			continue
		}
		m = m.Concat(n)
	}
	return m
}

// lengthOrPct parses a translate argument: px (pct 0) or a percentage.
func lengthOrPct(tok string, fs float32) (px, pct float32, ok bool) {
	tok = strings.TrimSpace(tok)
	if strings.HasSuffix(tok, "%") {
		if f, o := parseFloat(strings.TrimSuffix(tok, "%")); o {
			return 0, f / 100, true
		}
		return 0, 0, false
	}
	if px, ok := parseLength(tok, fs, 0); ok {
		return px, 0, true
	}
	return 0, 0, false
}

// gradientSpec is a parsed `linear-gradient(...)`: a direction angle
// (CSS convention: 0deg = to top, growing clockwise) plus color stops.
type gradientSpec struct {
	angleDeg         float32
	stops            []qui.GradientStop
	radial           bool // radial-gradient (angleDeg unused)
	conic            bool // conic-gradient (angleDeg = start angle)
	centerX, centerY lenPct
	hasCenter        bool
}

// shaderFor builds a qui.LinearGradient whose endpoints span the box
// `bounds` along the gradient's angle. The line passes through the box
// center; its half-length is the projection of the box half-extents onto
// the direction (the common browser approximation).
func (g *gradientSpec) shaderFor(bounds qui.Rect) qui.Shader {
	if g == nil || len(g.stops) == 0 {
		return nil
	}
	if g.radial {
		cx := bounds.X + bounds.W/2
		cy := bounds.Y + bounds.H/2
		// Default radial size ≈ farthest-corner: half the box diagonal.
		r := float32(math.Hypot(float64(bounds.W/2), float64(bounds.H/2)))
		return qui.RadialGradient{Center: qui.Point{X: cx, Y: cy}, Radius: r, Stops: g.stops}
	}
	if g.conic {
		cx := bounds.X + bounds.W/2
		cy := bounds.Y + bounds.H/2
		if g.hasCenter {
			cx = bounds.X + g.centerX.resolve(bounds.W)
			cy = bounds.Y + g.centerY.resolve(bounds.H)
		}
		return qui.ConicGradient{Center: qui.Point{X: cx, Y: cy}, Angle: g.angleDeg * float32(math.Pi) / 180, Stops: g.stops}
	}
	rad := float64(g.angleDeg) * math.Pi / 180
	// CSS: 0deg points up, clockwise-positive → dir = (sin, -cos).
	dx := float32(math.Sin(rad))
	dy := float32(-math.Cos(rad))
	cx := bounds.X + bounds.W/2
	cy := bounds.Y + bounds.H/2
	half := (absf(dx)*bounds.W + absf(dy)*bounds.H) / 2
	return qui.LinearGradient{
		Start: qui.Point{X: cx - dx*half, Y: cy - dy*half},
		End:   qui.Point{X: cx + dx*half, Y: cy + dy*half},
		Stops: g.stops,
	}
}

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// parseBoxShadow parses a single-layer `box-shadow` value:
// `<offX> <offY> [blur] [spread] <color>`. `inset` selects an inner
// shadow. Returns ok=false when the value has no drawable layer.
func parseBoxShadow(v string, fs float32) (qui.ShadowStyle, bool) {
	shadows := parseBoxShadows(v, fs)
	if len(shadows) == 0 {
		return qui.ShadowStyle{}, false
	}
	return shadows[0], true
}

// parseBoxShadows parses a possibly-multi-layer `box-shadow` value into its
// layers (in source order), including inset shadows.
func parseBoxShadows(v string, fs float32) []qui.ShadowStyle {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "none") {
		return nil
	}
	var out []qui.ShadowStyle
	for _, layer := range splitTopLevel(v, ',') {
		layer = strings.TrimSpace(layer)
		if layer == "" {
			continue
		}
		var sh qui.ShadowStyle
		var lens []float32
		haveColor := false
		for _, tok := range fieldsTopLevel(layer) {
			if strings.EqualFold(tok, "inset") {
				sh.Inset = true
				continue
			}
			if c, ok := parseColor(tok); ok {
				sh.Color, haveColor = c, true
				continue
			}
			if px, ok := parseLength(tok, fs, 0); ok {
				lens = append(lens, px)
			}
		}
		if len(lens) < 2 {
			continue
		}
		sh.X, sh.Y = lens[0], lens[1]
		if len(lens) >= 3 {
			sh.Blur = lens[2]
		}
		if len(lens) >= 4 {
			sh.Spread = lens[3]
		}
		if !haveColor {
			sh.Color = qui.Color{R: 0, G: 0, B: 0, A: 0.35}
		}
		if sh.Color.A <= 0 {
			continue
		}
		out = append(out, sh)
	}
	return out
}

// parseOpacity parses an `opacity` value clamped to [0,1].
func parseOpacity(v string) (float32, bool) {
	f, ok := parseFloat(strings.TrimSpace(v))
	if !ok {
		return 0, false
	}
	return clamp01(f), true
}

// parseTransform parses a `transform` function list into a transformSpec
// whose ops keep the source order (CSS Transforms 1: `translate(…)
// rotate(…)` differs from `rotate(…) translate(…)`). 3D functions keep
// their 2D part (translate3d's x/y, rotateZ, scale3d's x/y, matrix3d's 2D
// coefficients); rotateX / rotateY project as a scale by the cosine.
func parseTransform(v string, fs float32) (*transformSpec, bool) {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "none") {
		return nil, false
	}
	t := &transformSpec{sx: 1, sy: 1}
	add := func(op transformOp) { t.ops = append(t.ops, op) }
	num := func(args []string, i int, def float32) float32 {
		if i < len(args) {
			if f, ok := parseFloat(strings.TrimSpace(args[i])); ok {
				return f
			}
		}
		return def
	}
	for _, fn := range splitFunctions(v) {
		name, args := strings.ToLower(fn.name), fn.args
		switch name {
		case "translate", "translate3d":
			var op transformOp
			op.kind = 't'
			if len(args) >= 1 {
				op.a, op.aPct, _ = lengthOrPct(args[0], fs)
			}
			if len(args) >= 2 {
				op.b, op.bPct, _ = lengthOrPct(args[1], fs)
			}
			add(op)
		case "translatex":
			if len(args) >= 1 {
				op := transformOp{kind: 't'}
				op.a, op.aPct, _ = lengthOrPct(args[0], fs)
				add(op)
			}
		case "translatey":
			if len(args) >= 1 {
				op := transformOp{kind: 't'}
				op.b, op.bPct, _ = lengthOrPct(args[0], fs)
				add(op)
			}
		case "translatez", "perspective":
			add(transformOp{kind: 't'}) // no 2D effect, still a transform
		case "rotate", "rotatez":
			if len(args) >= 1 {
				add(transformOp{kind: 'r', a: parseAngle(args[0])})
			}
		case "rotate3d":
			if len(args) == 4 && num(args, 0, 0) == 0 && num(args, 1, 0) == 0 {
				a := parseAngle(args[3])
				if num(args, 2, 1) < 0 {
					a = -a
				}
				add(transformOp{kind: 'r', a: a})
			}
		case "rotatex":
			if len(args) >= 1 {
				add(transformOp{kind: 's', a: 1, b: float32(math.Cos(float64(parseAngle(args[0]))))})
			}
		case "rotatey":
			if len(args) >= 1 {
				add(transformOp{kind: 's', a: float32(math.Cos(float64(parseAngle(args[0])))), b: 1})
			}
		case "scale", "scale3d":
			if len(args) >= 1 {
				sx := num(args, 0, 1)
				add(transformOp{kind: 's', a: sx, b: num(args, 1, sx)})
			}
		case "scalex":
			add(transformOp{kind: 's', a: num(args, 0, 1), b: 1})
		case "scaley":
			add(transformOp{kind: 's', a: 1, b: num(args, 0, 1)})
		case "scalez":
			add(transformOp{kind: 's', a: 1, b: 1})
		case "skew":
			op := transformOp{kind: 'k'}
			if len(args) >= 1 {
				op.a = parseAngle(args[0])
			}
			if len(args) >= 2 {
				op.b = parseAngle(args[1])
			}
			add(op)
		case "skewx":
			if len(args) >= 1 {
				add(transformOp{kind: 'k', a: parseAngle(args[0])})
			}
		case "skewy":
			if len(args) >= 1 {
				add(transformOp{kind: 'k', b: parseAngle(args[0])})
			}
		case "matrix":
			if len(args) == 6 {
				var m [6]float32
				ok := true
				for i := 0; i < 6; i++ {
					if v, o := parseFloat(strings.TrimSpace(args[i])); o {
						m[i] = v
					} else {
						ok = false
					}
				}
				if ok {
					// CSS matrix(a,b,c,d,e,f) = [a c e; b d f].
					add(transformOp{kind: 'm', m: qui.Matrix{A: m[0], C: m[1], B: m[2], D: m[3], TX: m[4], TY: m[5]}})
				}
			}
		case "matrix3d":
			if len(args) == 16 {
				f := func(i int) float32 { return num(args, i, 0) }
				// Column-major 4×4: the 2D part is m11 m12 m21 m22 m41 m42.
				add(transformOp{kind: 'm', m: qui.Matrix{A: f(0), C: f(1), B: f(4), D: f(5), TX: f(12), TY: f(13)}})
			}
		}
	}
	if len(t.ops) == 0 {
		return nil, false
	}
	// The decomposed fields mirror the ops for code that reads a simple
	// transform (pixel translations summed, rotations summed, scales
	// multiplied); painting uses the ordered ops.
	for _, op := range t.ops {
		switch op.kind {
		case 't':
			t.tx += op.a
			t.ty += op.b
		case 'r':
			t.rotate += op.a
		case 's':
			t.sx *= op.a
			t.sy *= op.b
		case 'k':
			t.kx += op.a
			t.ky += op.b
		case 'm':
			m := op.m
			t.matrix = &m
		}
	}
	return t, true
}

// individualTransformOps parses the CSS Transforms 2 `translate`, `rotate`
// and `scale` properties, which apply in that order before `transform`.
func individualTransformOps(m map[string]string, fs float32) []transformOp {
	var ops []transformOp
	if v := strings.TrimSpace(m["translate"]); v != "" && v != "none" {
		f := strings.Fields(v)
		op := transformOp{kind: 't'}
		op.a, op.aPct, _ = lengthOrPct(f[0], fs)
		if len(f) > 1 {
			op.b, op.bPct, _ = lengthOrPct(f[1], fs)
		}
		ops = append(ops, op)
	}
	if v := strings.TrimSpace(m["rotate"]); v != "" && v != "none" {
		f := strings.Fields(v)
		ops = append(ops, transformOp{kind: 'r', a: parseAngle(f[len(f)-1])})
	}
	if v := strings.TrimSpace(m["scale"]); v != "" && v != "none" {
		f := strings.Fields(v)
		if sx, ok := parseFloat(f[0]); ok {
			sy := sx
			if len(f) > 1 {
				if y, ok := parseFloat(f[1]); ok {
					sy = y
				}
			}
			ops = append(ops, transformOp{kind: 's', a: sx, b: sy})
		}
	}
	return ops
}

// parseTransformOrigin resolves transform-origin into (0..1, 0..1) fractions.
// Accepts keywords (left/center/right/top/bottom) and percentages.
func parseTransformOrigin(v string) (ox, oy float32, ok bool) {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(v)))
	if len(fields) == 0 {
		return 0, 0, false
	}
	axis := func(f string, horiz bool) (float32, bool) {
		switch f {
		case "left":
			return 0, true
		case "right":
			return 1, true
		case "top":
			return 0, true
		case "bottom":
			return 1, true
		case "center":
			return 0.5, true
		}
		if strings.HasSuffix(f, "%") {
			if p, o := parseFloat(strings.TrimSuffix(f, "%")); o {
				return p / 100, true
			}
		}
		return 0, false
	}
	ox, oy = 0.5, 0.5
	if len(fields) >= 1 {
		if x, o := axis(fields[0], true); o {
			ox = x
		}
	}
	if len(fields) >= 2 {
		if y, o := axis(fields[1], false); o {
			oy = y
		}
	}
	return ox, oy, true
}

// parseLinearGradient parses `linear-gradient(<dir>?, <stop>, <stop>...)`.
// Radial and repeating gradients are not supported (ok=false).
func parseLinearGradient(v string) (*gradientSpec, bool) {
	v = strings.TrimSpace(v)
	low := strings.ToLower(v)
	if !strings.HasPrefix(low, "linear-gradient(") || !strings.HasSuffix(v, ")") {
		return nil, false
	}
	inner := v[len("linear-gradient(") : len(v)-1]
	parts := splitTopLevel(inner, ',')
	if len(parts) < 2 {
		return nil, false
	}
	g := &gradientSpec{angleDeg: 180} // CSS default direction: to bottom
	idx := 0
	if dir, ok := parseGradientDirection(strings.TrimSpace(parts[0])); ok {
		g.angleDeg = dir
		idx = 1
	}
	g.stops = parseGradientStops(parts[idx:])
	if len(g.stops) < 2 {
		return nil, false
	}
	return g, true
}

// parseGradient dispatches to the linear or radial parser (background-image).
func parseGradient(v string) (*gradientSpec, bool) {
	low := strings.ToLower(strings.TrimSpace(v))
	switch {
	case strings.HasPrefix(low, "linear-gradient("):
		return parseLinearGradient(v)
	case strings.HasPrefix(low, "radial-gradient("):
		return parseRadialGradient(v)
	case strings.HasPrefix(low, "conic-gradient("):
		return parseConicGradient(v)
	}
	return nil, false
}

// parseConicGradient parses `conic-gradient([from <angle>]? [at …]?, stops…)`.
// Position percentages are relative to the background positioning area.
func parseConicGradient(v string) (*gradientSpec, bool) {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(strings.ToLower(v), "conic-gradient(") || !strings.HasSuffix(v, ")") {
		return nil, false
	}
	inner := v[len("conic-gradient(") : len(v)-1]
	parts := splitTopLevel(inner, ',')
	if len(parts) < 2 {
		return nil, false
	}
	g := &gradientSpec{conic: true}
	idx := 0
	first := strings.ToLower(strings.TrimSpace(parts[0]))
	if strings.HasPrefix(first, "from") || strings.HasPrefix(first, "at") {
		fields := strings.Fields(first)
		for i, f := range fields {
			if f == "from" && i+1 < len(fields) {
				g.angleDeg = parseAngle(fields[i+1]) * 180 / float32(math.Pi)
			}
			if f == "at" {
				x, y, ok := parsePosition(fields[i+1:], 16)
				if !ok {
					return nil, false
				}
				g.centerX, g.centerY, g.hasCenter = x, y, true
				break
			}
		}
		idx = 1
	} else if _, ok := parseColor(fieldsTopLevel(parts[0])[0]); !ok {
		idx = 1 // unrecognized non-color prefix
	}
	g.stops = parseGradientStopsMode(parts[idx:], true)
	if len(g.stops) < 2 {
		return nil, false
	}
	return g, true
}

// parseRadialGradient parses `radial-gradient([shape/size/position ,]? <stop>…)`.
// The optional shape/size/position prefix (circle / ellipse / at … / sizes) is
// ignored — the gradient always renders as a farthest-corner radial fill.
func parseRadialGradient(v string) (*gradientSpec, bool) {
	v = strings.TrimSpace(v)
	low := strings.ToLower(v)
	if !strings.HasPrefix(low, "radial-gradient(") || !strings.HasSuffix(v, ")") {
		return nil, false
	}
	inner := v[len("radial-gradient(") : len(v)-1]
	parts := splitTopLevel(inner, ',')
	if len(parts) < 2 {
		return nil, false
	}
	idx := 0
	// If the first part isn't a color stop, treat it as the shape/size/position.
	if fields := fieldsTopLevel(strings.TrimSpace(parts[0])); len(fields) > 0 {
		if _, ok := parseColor(fields[0]); !ok {
			idx = 1
		}
	}
	g := &gradientSpec{radial: true}
	g.stops = parseGradientStops(parts[idx:])
	if len(g.stops) < 2 {
		return nil, false
	}
	return g, true
}

// parseGradientStops parses a list of `<color> [<pos%>]` stop tokens into
// evenly-spaced stops (overridden by any explicit percentage).
func parseGradientStops(stopToks []string) []qui.GradientStop {
	return parseGradientStopsMode(stopToks, false)
}

func parseGradientStopsMode(stopToks []string, angular bool) []qui.GradientStop {
	var stops []qui.GradientStop
	n := len(stopToks)
	for i, tok := range stopToks {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		fields := fieldsTopLevel(tok)
		if len(fields) == 0 {
			continue
		}
		col, ok := parseColor(fields[0])
		if !ok {
			continue
		}
		offset := float32(i)
		if n > 1 {
			offset = float32(i) / float32(n-1)
		}
		if len(fields) >= 2 && strings.HasSuffix(fields[1], "%") {
			if p, ok := parseFloat(strings.TrimSuffix(fields[1], "%")); ok {
				offset = clamp01(p / 100)
			}
		}
		if angular && len(fields) >= 2 {
			v := fields[1]
			if strings.HasSuffix(v, "deg") || strings.HasSuffix(v, "turn") || strings.HasSuffix(v, "rad") || strings.HasSuffix(v, "grad") || v == "0" {
				offset = parseAngle(v) / (2 * float32(math.Pi))
			}
		}
		if len(stops) > 0 {
			offset = max(offset, stops[len(stops)-1].Offset)
		}
		stops = append(stops, qui.GradientStop{Offset: offset, Color: col})
	}
	return stops
}

// parseGradientDirection parses "<angle>deg" or "to <side(s)>" into a CSS
// angle (0deg = up, clockwise-positive). ok=false when the token is a
// color stop rather than a direction.
func parseGradientDirection(tok string) (float32, bool) {
	low := strings.ToLower(strings.TrimSpace(tok))
	if strings.HasSuffix(low, "deg") {
		if f, ok := parseFloat(strings.TrimSuffix(low, "deg")); ok {
			return f, true
		}
		return 0, false
	}
	if strings.HasPrefix(low, "to ") {
		switch strings.TrimSpace(strings.TrimPrefix(low, "to ")) {
		case "top":
			return 0, true
		case "right":
			return 90, true
		case "bottom":
			return 180, true
		case "left":
			return 270, true
		case "top right", "right top":
			return 45, true
		case "bottom right", "right bottom":
			return 135, true
		case "bottom left", "left bottom":
			return 225, true
		case "top left", "left top":
			return 315, true
		}
	}
	return 0, false
}

// parseAngle parses "<n>deg" / "<n>rad" / "<n>turn" into radians.
func parseAngle(tok string) float32 {
	low := strings.ToLower(strings.TrimSpace(tok))
	switch {
	case strings.HasSuffix(low, "deg"):
		if f, ok := parseFloat(strings.TrimSuffix(low, "deg")); ok {
			return f * float32(math.Pi) / 180
		}
	case strings.HasSuffix(low, "grad"):
		if f, ok := parseFloat(strings.TrimSuffix(low, "grad")); ok {
			return f * float32(math.Pi) / 200
		}
	case strings.HasSuffix(low, "rad"):
		if f, ok := parseFloat(strings.TrimSuffix(low, "rad")); ok {
			return f
		}
	case strings.HasSuffix(low, "turn"):
		if f, ok := parseFloat(strings.TrimSuffix(low, "turn")); ok {
			return f * 2 * float32(math.Pi)
		}
	}
	return 0
}

// fieldsTopLevel splits on whitespace, keeping `(...)`-parenthesized runs
// (e.g. rgba(0,0,0,.2)) intact.
func fieldsTopLevel(s string) []string {
	var out []string
	var cur strings.Builder
	depth := 0
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '(':
			depth++
			cur.WriteByte(c)
		case c == ')':
			if depth > 0 {
				depth--
			}
			cur.WriteByte(c)
		case depth == 0 && isSpace(c):
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

type cssFunction struct {
	name string
	args []string
}

// splitFunctions parses a space-separated list of `name(args)` calls
// (e.g. "translate(1px,2px) rotate(10deg)"), lowercasing names and
// comma-splitting each call's arguments.
func splitFunctions(s string) []cssFunction {
	var out []cssFunction
	i := 0
	for i < len(s) {
		for i < len(s) && (isSpace(s[i]) || s[i] == ',') {
			i++
		}
		start := i
		for i < len(s) && s[i] != '(' {
			i++
		}
		if i >= len(s) {
			break
		}
		name := strings.ToLower(strings.TrimSpace(s[start:i]))
		i++ // consume '('
		argStart := i
		depth := 1
		for i < len(s) && depth > 0 {
			if s[i] == '(' {
				depth++
			} else if s[i] == ')' {
				depth--
				if depth == 0 {
					break
				}
			}
			i++
		}
		argStr := s[argStart:i]
		if i < len(s) {
			i++ // consume ')'
		}
		var args []string
		for _, a := range splitTopLevel(argStr, ',') {
			if a = strings.TrimSpace(a); a != "" {
				args = append(args, a)
			}
		}
		if name != "" {
			out = append(out, cssFunction{name: name, args: args})
		}
	}
	return out
}
