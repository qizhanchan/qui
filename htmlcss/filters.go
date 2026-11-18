package htmlcss

import (
	"image"
	"math"
	"strconv"
	"strings"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// CSS `filter` (Filter Effects 1 § 13): a space-separated function list
// applied in order to the element's rendered subtree. blur() and
// drop-shadow() are whole-layer filters; the rest are per-pixel color
// matrices (§ 13.1's equivalent SVG matrices), each clamped to [0, 1] before
// the next runs, as consecutive SVG filter primitives are.

// parseFilter parses a CSS `filter` value into a qui.ImageFilter. A lone
// blur() or drop-shadow() returns the plain qui filter; a list or any color
// function returns a cssFilterChain. ok=false when nothing parses (or the
// value is none).
func parseFilter(v string, fs float32) (qui.ImageFilter, bool) {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "none") {
		return nil, false
	}
	var chain cssFilterChain
	for _, fn := range splitFunctions(v) {
		if st, ok := parseFilterFunction(fn, fs); ok {
			chain = append(chain, st)
		}
	}
	switch {
	case len(chain) == 0:
		return nil, false
	case len(chain) == 1 && chain[0].image != nil:
		return chain[0].image, true
	}
	return chain, true
}

// parseFilterFunction parses one filter function.
func parseFilterFunction(fn cssFunction, fs float32) (filterStep, bool) {
	// amount reads a <number> or <percentage> argument (def when absent).
	amount := func(def float32) (float32, bool) {
		if len(fn.args) == 0 {
			return def, true
		}
		a := strings.TrimSpace(fn.args[0])
		if strings.HasSuffix(a, "%") {
			f, ok := parseFloat(strings.TrimSuffix(a, "%"))
			return f / 100, ok && f >= 0
		}
		f, ok := parseFloat(a)
		return f, ok && f >= 0
	}
	clampAmt := func(a float32) float32 { return min(a, 1) }
	switch fn.name {
	case "blur":
		if len(fn.args) == 0 {
			return filterStep{}, false
		}
		if px, ok := parseLength(fn.args[0], fs, 0); ok && px > 0 {
			return filterStep{image: qui.BlurImageFilter{Radius: px}}, true
		}
		return filterStep{}, false
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
				} else if f == "0" {
					lens = append(lens, 0)
				}
			}
		}
		if len(lens) < 2 {
			return filterStep{}, false
		}
		if !haveCol {
			col = qui.Color{A: 0.5}
		}
		blur := float32(0)
		if len(lens) >= 3 {
			blur = lens[2]
		}
		return filterStep{image: qui.DropShadowImageFilter{Offset: qui.Point{X: lens[0], Y: lens[1]}, Blur: blur, Color: col}}, true
	case "brightness":
		a, ok := amount(1)
		return matrixStep(scaleMatrix(a, a, a, 1, 0)), ok
	case "contrast":
		a, ok := amount(1)
		o := 0.5 - 0.5*a
		return matrixStep(scaleMatrix(a, a, a, 1, o)), ok
	case "opacity":
		a, ok := amount(1)
		return matrixStep(scaleMatrix(1, 1, 1, clampAmt(a), 0)), ok
	case "invert":
		a, ok := amount(1)
		a = clampAmt(a)
		return matrixStep(scaleMatrix(1-2*a, 1-2*a, 1-2*a, 1, a)), ok
	case "grayscale":
		a, ok := amount(1)
		return matrixStep(saturateMatrix(1 - clampAmt(a))), ok
	case "saturate":
		a, ok := amount(1)
		return matrixStep(saturateMatrix(a)), ok
	case "sepia":
		a, ok := amount(1)
		s := 1 - clampAmt(a)
		return matrixStep([20]float32{
			0.393 + 0.607*s, 0.769 - 0.769*s, 0.189 - 0.189*s, 0, 0,
			0.349 - 0.349*s, 0.686 + 0.314*s, 0.168 - 0.168*s, 0, 0,
			0.272 - 0.272*s, 0.534 - 0.534*s, 0.131 + 0.869*s, 0, 0,
			0, 0, 0, 1, 0,
		}), ok
	case "hue-rotate":
		th := float32(0)
		if len(fn.args) > 0 {
			th = parseAngle(fn.args[0])
		}
		c, s := float32(math.Cos(float64(th))), float32(math.Sin(float64(th)))
		return matrixStep([20]float32{
			0.213 + c*0.787 - s*0.213, 0.715 - c*0.715 - s*0.715, 0.072 - c*0.072 + s*0.928, 0, 0,
			0.213 - c*0.213 + s*0.143, 0.715 + c*0.285 + s*0.140, 0.072 - c*0.072 - s*0.283, 0, 0,
			0.213 - c*0.213 - s*0.787, 0.715 - c*0.715 + s*0.715, 0.072 + c*0.928 + s*0.072, 0, 0,
			0, 0, 0, 1, 0,
		}), true
	}
	return filterStep{}, false
}

func matrixStep(m [20]float32) filterStep { return filterStep{matrix: &m} }

// scaleMatrix multiplies r/g/b by rgb and alpha by a, adding off to r/g/b.
func scaleMatrix(r, g, b, a, off float32) [20]float32 {
	return [20]float32{
		r, 0, 0, 0, off,
		0, g, 0, 0, off,
		0, 0, b, 0, off,
		0, 0, 0, a, 0,
	}
}

// saturateMatrix is SVG feColorMatrix type="saturate".
func saturateMatrix(s float32) [20]float32 {
	return [20]float32{
		0.213 + 0.787*s, 0.715 - 0.715*s, 0.072 - 0.072*s, 0, 0,
		0.213 - 0.213*s, 0.715 + 0.285*s, 0.072 - 0.072*s, 0, 0,
		0.213 - 0.213*s, 0.715 - 0.715*s, 0.072 + 0.928*s, 0, 0,
		0, 0, 0, 1, 0,
	}
}

// filterStep is one filter function: a whole-layer image filter or a
// color matrix over straight RGBA.
type filterStep struct {
	image  qui.ImageFilter
	matrix *[20]float32
}

// cssFilterChain applies its steps in order. Lengths are logical pixels;
// widgets.Box scales them for the device through ScaledImageFilter.
type cssFilterChain []filterStep

var _ widgets.ScalableImageFilter = cssFilterChain(nil)

// ScaledImageFilter implements widgets.ScalableImageFilter.
func (c cssFilterChain) ScaledImageFilter(scale float32) qui.ImageFilter {
	out := make(cssFilterChain, len(c))
	for i, st := range c {
		if st.image != nil {
			st.image = widgets.ScaleImageFilter(st.image, scale)
		}
		out[i] = st
	}
	return out
}

// ApplyImage implements qui.ImageFilter.
func (c cssFilterChain) ApplyImage(src *image.RGBA) *image.RGBA {
	img := src
	owned := false // img is ours to modify in place
	for _, st := range c {
		if img == nil {
			return nil
		}
		if st.image != nil {
			out := st.image.ApplyImage(img)
			owned = out != img
			img = out
			continue
		}
		if !owned {
			cp := image.NewRGBA(img.Rect)
			copy(cp.Pix, img.Pix)
			img, owned = cp, true
		}
		applyColorMatrix(img, st.matrix)
	}
	return img
}

// applyColorMatrix runs a 4×5 matrix over premultiplied img in place,
// working on straight color and clamping the result.
func applyColorMatrix(img *image.RGBA, m *[20]float32) {
	b := img.Rect
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := img.PixOffset(b.Min.X, y)
		for x := 0; x < b.Dx(); x++ {
			i := row + x*4
			p := img.Pix[i : i+4 : i+4]
			a := float32(p[3]) / 255
			var r, g, bl float32
			if a > 0 {
				r, g, bl = float32(p[0])/255/a, float32(p[1])/255/a, float32(p[2])/255/a
			}
			nr := clamp01(m[0]*r + m[1]*g + m[2]*bl + m[3]*a + m[4])
			ng := clamp01(m[5]*r + m[6]*g + m[7]*bl + m[8]*a + m[9])
			nb := clamp01(m[10]*r + m[11]*g + m[12]*bl + m[13]*a + m[14])
			na := clamp01(m[15]*r + m[16]*g + m[17]*bl + m[18]*a + m[19])
			p[0] = uint8(nr*na*255 + 0.5)
			p[1] = uint8(ng*na*255 + 0.5)
			p[2] = uint8(nb*na*255 + 0.5)
			p[3] = uint8(na*255 + 0.5)
		}
	}
}

// parseTextShadows parses `text-shadow`: comma-separated layers of
// `<x> <y> [blur] [color]` (color anywhere; default currentColor = cur).
// none / an unparsable value → nil.
func parseTextShadows(v string, fs float32, cur qui.Color) []qui.ShadowStyle {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "none") {
		return nil
	}
	var out []qui.ShadowStyle
	for _, layer := range splitTopLevel(v, ',') {
		var sh qui.ShadowStyle
		var lens []float32
		sh.Color = cur
		for _, tok := range fieldsTopLevel(strings.TrimSpace(layer)) {
			if strings.EqualFold(tok, "currentcolor") {
				sh.Color = cur
			} else if c, ok := parseColor(tok); ok {
				sh.Color = c
			} else if tok == "0" {
				lens = append(lens, 0)
			} else if px, ok := parseLength(tok, fs, 0); ok {
				lens = append(lens, px)
			} else {
				return nil // invalid layer invalidates the declaration
			}
		}
		if len(lens) < 2 || len(lens) > 3 {
			return nil
		}
		sh.X, sh.Y = lens[0], lens[1]
		if len(lens) == 3 && lens[2] > 0 {
			sh.Blur = lens[2]
		}
		out = append(out, sh)
	}
	return out
}

// resolveCurrentColor substitutes the element's color for the currentColor
// keyword in every declaration but `color` itself. The map is copied only
// when some value uses the keyword.
func resolveCurrentColor(m map[string]string, c qui.Color) map[string]string {
	var out map[string]string
	for k, v := range m {
		if k == "color" || len(v) < 12 || !strings.Contains(strings.ToLower(v), "currentcolor") {
			continue
		}
		if out == nil {
			out = make(map[string]string, len(m))
			for k2, v2 := range m {
				out[k2] = v2
			}
		}
		out[k] = replaceFold(v, "currentcolor", colorCSS(c))
	}
	if out == nil {
		return m
	}
	return out
}

// colorCSS formats c as an rgba() value parseColor reads back.
func colorCSS(c qui.Color) string {
	b := func(f float32) string { return strconv.Itoa(int(clamp01(f)*255 + 0.5)) }
	return "rgba(" + b(c.R) + "," + b(c.G) + "," + b(c.B) + "," + strconv.FormatFloat(float64(clamp01(c.A)), 'f', 4, 32) + ")"
}

// replaceFold replaces every ASCII-case-insensitive occurrence of old
// (lowercase) in s with new.
func replaceFold(s, old, new string) string {
	low := strings.ToLower(s)
	var b strings.Builder
	i := 0
	for {
		j := strings.Index(low[i:], old)
		if j < 0 {
			b.WriteString(s[i:])
			return b.String()
		}
		b.WriteString(s[i : i+j])
		b.WriteString(new)
		i += j + len(old)
	}
}
