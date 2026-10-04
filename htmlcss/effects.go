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
}

// gradientSpec is a parsed `linear-gradient(...)`: a direction angle
// (CSS convention: 0deg = to top, growing clockwise) plus color stops.
type gradientSpec struct {
	angleDeg float32
	stops    []qui.GradientStop
	radial   bool // radial-gradient (angleDeg unused)
	conic    bool // conic-gradient (angleDeg = start angle)
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
// `<offX> <offY> [blur] [spread] <color>`. `inset` layers are skipped (no
// inner-shadow primitive). Returns ok=false when the value has no drawable
// outset layer.
func parseBoxShadow(v string, fs float32) (qui.ShadowStyle, bool) {
	shadows := parseBoxShadows(v, fs)
	if len(shadows) == 0 {
		return qui.ShadowStyle{}, false
	}
	return shadows[0], true
}

// parseBoxShadows parses a possibly-multi-layer `box-shadow` value into its
// outset layers (in source order). inset layers are dropped (unsupported).
func parseBoxShadows(v string, fs float32) []qui.ShadowStyle {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "none") {
		return nil
	}
	var out []qui.ShadowStyle
	for _, layer := range splitTopLevel(v, ',') {
		layer = strings.TrimSpace(layer)
		if layer == "" || strings.Contains(strings.ToLower(layer), "inset") {
			continue // inner shadows aren't supported by the primitive
		}
		var sh qui.ShadowStyle
		var lens []float32
		haveColor := false
		for _, tok := range fieldsTopLevel(layer) {
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

// parseFilter parses a CSS `filter` value into a qui.ImageFilter. Supports
// `blur(<len>)` and `drop-shadow(<x> <y> [blur] <color>)`. Multiple functions
// are not composed — the first recognized one wins. ok=false when none parse.
func parseFilter(v string, fs float32) (qui.ImageFilter, bool) {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "none") {
		return nil, false
	}
	for _, fn := range splitFunctions(v) {
		switch fn.name {
		case "blur":
			if len(fn.args) >= 1 {
				if px, ok := parseLength(fn.args[0], fs, 0); ok && px > 0 {
					return qui.BlurImageFilter{Radius: px}, true
				}
			}
		case "drop-shadow":
			var lens []float32
			var col qui.Color
			haveCol := false
			for _, tok := range fn.args {
				for _, f := range fieldsTopLevel(tok) {
					if c, ok := parseColor(f); ok {
						col, haveCol = c, true
					} else if px, ok := parseLength(f, fs, 0); ok {
						lens = append(lens, px)
					}
				}
			}
			if len(lens) >= 2 {
				if !haveCol {
					col = qui.Color{A: 0.5}
				}
				blur := float32(0)
				if len(lens) >= 3 {
					blur = lens[2]
				}
				return qui.DropShadowImageFilter{Offset: qui.Point{X: lens[0], Y: lens[1]}, Blur: blur, Color: col}, true
			}
		}
	}
	return nil, false
}

// parseOpacity parses an `opacity` value clamped to [0,1].
func parseOpacity(v string) (float32, bool) {
	f, ok := parseFloat(strings.TrimSpace(v))
	if !ok {
		return 0, false
	}
	return clamp01(f), true
}

// parseTransform parses a `transform` function list into a transformSpec.
func parseTransform(v string, fs float32) (*transformSpec, bool) {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "none") {
		return nil, false
	}
	t := &transformSpec{sx: 1, sy: 1}
	found := false
	for _, fn := range splitFunctions(v) {
		name, args := fn.name, fn.args
		switch name {
		case "translate":
			if len(args) >= 1 {
				if px, ok := parseLength(args[0], fs, 0); ok {
					t.tx = px
				}
			}
			if len(args) >= 2 {
				if px, ok := parseLength(args[1], fs, 0); ok {
					t.ty = px
				}
			}
			found = true
		case "translatex":
			if len(args) >= 1 {
				if px, ok := parseLength(args[0], fs, 0); ok {
					t.tx = px
				}
			}
			found = true
		case "translatey":
			if len(args) >= 1 {
				if px, ok := parseLength(args[0], fs, 0); ok {
					t.ty = px
				}
			}
			found = true
		case "rotate":
			if len(args) >= 1 {
				t.rotate, found = parseAngle(args[0]), true
			}
		case "scale":
			if len(args) >= 1 {
				if s, ok := parseFloat(args[0]); ok {
					t.sx, t.sy = s, s
				}
			}
			if len(args) >= 2 {
				if s, ok := parseFloat(args[1]); ok {
					t.sy = s
				}
			}
			found = true
		case "scalex":
			if len(args) >= 1 {
				if s, ok := parseFloat(args[0]); ok {
					t.sx = s
				}
			}
			found = true
		case "scaley":
			if len(args) >= 1 {
				if s, ok := parseFloat(args[0]); ok {
					t.sy = s
				}
			}
			found = true
		case "skew":
			if len(args) >= 1 {
				t.kx = parseAngle(args[0])
			}
			if len(args) >= 2 {
				t.ky = parseAngle(args[1])
			}
			found = true
		case "skewx":
			if len(args) >= 1 {
				t.kx, found = parseAngle(args[0]), true
			}
		case "skewy":
			if len(args) >= 1 {
				t.ky, found = parseAngle(args[0]), true
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
					t.matrix = &qui.Matrix{A: m[0], C: m[1], B: m[2], D: m[3], TX: m[4], TY: m[5]}
					found = true
				}
			}
		}
	}
	if !found {
		return nil, false
	}
	return t, true
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
// Only the `from <angle>` prefix is honored (position ignored → center).
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
		for _, f := range strings.Fields(first) {
			if strings.HasSuffix(f, "deg") || strings.HasSuffix(f, "turn") || strings.HasSuffix(f, "rad") {
				g.angleDeg = parseAngle(f) * 180 / float32(3.14159265)
			}
		}
		idx = 1
	} else if _, ok := parseColor(fieldsTopLevel(parts[0])[0]); !ok {
		idx = 1 // unrecognized non-color prefix
	}
	g.stops = parseGradientStops(parts[idx:])
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
		for _, a := range strings.Split(argStr, ",") {
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
