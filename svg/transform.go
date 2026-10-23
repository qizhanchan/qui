package svg

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Transform is a 2D affine matrix in the same shape as SVG's
// matrix(a,b,c,d,e,f) — column-major:
//
//	| A C E |
//	| B D F |
//	| 0 0 1 |
//
// Construction helpers all return a fresh *Transform so they can be
// chained: svg.Translate(10,0).Then(svg.Rotate(45)). The zero value is
// not the identity matrix — use Identity() if you need a starting
// point.
type Transform struct {
	A, B, C, D, E, F float32
}

// Identity returns the identity transform (no translation, scale, rotation).
func Identity() *Transform { return &Transform{A: 1, D: 1} }

// Translate returns a translation-only transform.
func Translate(x, y float32) *Transform { return &Transform{A: 1, D: 1, E: x, F: y} }

// Scale returns a (sx, sy) scale transform.
func Scale(sx, sy float32) *Transform { return &Transform{A: sx, D: sy} }

// Rotate returns a rotation by deg degrees about the origin.
func Rotate(deg float32) *Transform {
	r := float64(deg) * math.Pi / 180
	c := float32(math.Cos(r))
	s := float32(math.Sin(r))
	return &Transform{A: c, B: s, C: -s, D: c}
}

// RotateAround returns rotation about (cx, cy), i.e. SVG's
// rotate(deg cx cy) = translate(cx cy) rotate(deg) translate(-cx -cy): a
// point is moved so (cx, cy) is the origin, rotated, and moved back.
func RotateAround(deg, cx, cy float32) *Transform {
	return Translate(-cx, -cy).Then(Rotate(deg)).Then(Translate(cx, cy))
}

// SkewX returns a skew along the X axis by deg degrees.
func SkewX(deg float32) *Transform {
	t := math.Tan(float64(deg) * math.Pi / 180)
	return &Transform{A: 1, D: 1, C: float32(t)}
}

// SkewY returns a skew along the Y axis by deg degrees.
func SkewY(deg float32) *Transform {
	t := math.Tan(float64(deg) * math.Pi / 180)
	return &Transform{A: 1, D: 1, B: float32(t)}
}

// Matrix wraps an explicit (a,b,c,d,e,f) matrix.
func Matrix(a, b, c, d, e, f float32) *Transform {
	return &Transform{A: a, B: b, C: c, D: d, E: e, F: f}
}

// Then returns the transform that applies t to a point first and u
// second. Note this is the reverse of an SVG transform list, where the
// rightmost function applies first: transform="translate(10 0) rotate(45)"
// is Rotate(45).Then(Translate(10, 0)).
//
// Returning a new value (never mutating either operand) makes it safe to
// share *Transform pointers across elements.
func (t *Transform) Then(u *Transform) *Transform {
	if t == nil {
		return u.clone()
	}
	if u == nil {
		return t.clone()
	}
	return multiply(t, u)
}

// multiply returns the matrix that applies a to a point first and b
// second: p' = b * (a * p) = (b*a) * p.
func multiply(a, b *Transform) *Transform {
	// out = b * a
	return &Transform{
		A: b.A*a.A + b.C*a.B,
		B: b.B*a.A + b.D*a.B,
		C: b.A*a.C + b.C*a.D,
		D: b.B*a.C + b.D*a.D,
		E: b.A*a.E + b.C*a.F + b.E,
		F: b.B*a.E + b.D*a.F + b.F,
	}
}

func (t *Transform) clone() *Transform {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

// apply transforms a single (x, y) point.
func (t *Transform) apply(x, y float32) (float32, float32) {
	if t == nil {
		return x, y
	}
	return t.A*x + t.C*y + t.E, t.B*x + t.D*y + t.F
}

// Apply is the exported form of apply — useful for callers that want
// to project an authoring-coord point through a transform.
func (t *Transform) Apply(x, y float32) (float32, float32) { return t.apply(x, y) }

// String emits an SVG transform-attribute encoding. Prefers translate /
// scale / rotate / matrix in that order based on which components are
// non-trivial — produces "translate(10,0)" instead of
// "matrix(1,0,0,1,10,0)" when possible so serialized output reads.
func (t *Transform) String() string {
	if t == nil {
		return ""
	}
	switch {
	case t.A == 1 && t.B == 0 && t.C == 0 && t.D == 1:
		if t.E == 0 && t.F == 0 {
			return ""
		}
		return fmt.Sprintf("translate(%s %s)", fmtNum(t.E), fmtNum(t.F))
	case t.B == 0 && t.C == 0 && t.E == 0 && t.F == 0:
		if t.A == t.D {
			return fmt.Sprintf("scale(%s)", fmtNum(t.A))
		}
		return fmt.Sprintf("scale(%s %s)", fmtNum(t.A), fmtNum(t.D))
	default:
		return fmt.Sprintf("matrix(%s %s %s %s %s %s)",
			fmtNum(t.A), fmtNum(t.B), fmtNum(t.C),
			fmtNum(t.D), fmtNum(t.E), fmtNum(t.F))
	}
}

// fmtNum prints float32 like SVG does — trim trailing zeros, no
// exponent, keep enough precision to round-trip the common cases.
func fmtNum(v float32) string {
	s := strconv.FormatFloat(float64(v), 'f', -1, 32)
	return s
}

// parseTransformAttr accepts an SVG transform attribute string and
// returns the composed *Transform. A list is the product of its matrices
// in written order, so the rightmost function is applied to a point first
// (as in a browser); whitespace and commas separate arguments.
// Unknown primitive names are an error so silent typos don't ship.
func parseTransformAttr(s string) (*Transform, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out *Transform
	for len(s) > 0 {
		// Read function name up to '('
		paren := strings.IndexByte(s, '(')
		if paren < 0 {
			return nil, fmt.Errorf("svg: transform: missing '(' near %q", s)
		}
		name := strings.TrimSpace(s[:paren])
		end := strings.IndexByte(s, ')')
		if end < 0 || end < paren {
			return nil, fmt.Errorf("svg: transform: missing ')' near %q", s)
		}
		argsStr := s[paren+1 : end]
		s = strings.TrimLeft(s[end+1:], " \t\n\r,")

		args, err := parseNumberList(argsStr)
		if err != nil {
			return nil, fmt.Errorf("svg: transform %s: %w", name, err)
		}
		t, err := buildTransform(name, args)
		if err != nil {
			return nil, err
		}
		if out == nil {
			out = t
		} else {
			out = t.Then(out)
		}
	}
	return out, nil
}

func buildTransform(name string, args []float32) (*Transform, error) {
	switch name {
	case "translate":
		switch len(args) {
		case 1:
			return Translate(args[0], 0), nil
		case 2:
			return Translate(args[0], args[1]), nil
		}
	case "scale":
		switch len(args) {
		case 1:
			return Scale(args[0], args[0]), nil
		case 2:
			return Scale(args[0], args[1]), nil
		}
	case "rotate":
		switch len(args) {
		case 1:
			return Rotate(args[0]), nil
		case 3:
			return RotateAround(args[0], args[1], args[2]), nil
		}
	case "skewX":
		if len(args) == 1 {
			return SkewX(args[0]), nil
		}
	case "skewY":
		if len(args) == 1 {
			return SkewY(args[0]), nil
		}
	case "matrix":
		if len(args) == 6 {
			return Matrix(args[0], args[1], args[2], args[3], args[4], args[5]), nil
		}
	default:
		return nil, fmt.Errorf("svg: unknown transform %q", name)
	}
	return nil, fmt.Errorf("svg: transform %s: wrong arg count %d", name, len(args))
}

// parseNumberList parses an SVG number list into float32s. Shared by
// transform / points / viewBox parsing — SVG uses the same number grammar
// everywhere, path data's included, so it reuses the path tokenizer: a
// sign or a second '.' starts a new number ("10-5", "1.5.5", which
// minifiers emit), and only SVG numbers parse (no NaN / Inf / hex).
func parseNumberList(s string) ([]float32, error) {
	t := pathTokenizer{s: strings.TrimSpace(s)}
	var out []float32
	for {
		t.skipSep()
		if t.i >= len(t.s) {
			return out, nil
		}
		v, err := t.number()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
}
