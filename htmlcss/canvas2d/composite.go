package canvas2d

import (
	"image"
	"math"
)

// Compositing for the slow path: the source is drawn alone into a scratch
// layer, then merged into the bitmap pixel by pixel with the composite
// operation, global alpha and clip mask. (source-over without shadows
// draws straight into the bitmap instead.)

// compositeOps lists the globalCompositeOperation values.
var compositeOps = map[string]bool{
	"source-over": true, "source-in": true, "source-out": true, "source-atop": true,
	"destination-over": true, "destination-in": true, "destination-out": true, "destination-atop": true,
	"lighter": true, "copy": true, "xor": true,
	"multiply": true, "screen": true, "overlay": true, "darken": true, "lighten": true,
	"color-dodge": true, "color-burn": true, "hard-light": true, "soft-light": true,
	"difference": true, "exclusion": true, "hue": true, "saturation": true, "color": true, "luminosity": true,
}

// boundedOp reports whether pixels outside the source stay as they are.
// The others (source-in, copy, …) clear the destination wherever the
// source is transparent, inside the clip.
func boundedOp(op string) bool {
	switch op {
	case "source-in", "source-out", "destination-in", "destination-atop", "copy":
		return false
	}
	return true
}

type px4 struct{ r, g, b, a float32 } // premultiplied, 0..1

// compositePixel merges src over dst with op (both premultiplied).
func compositePixel(op string, s, d px4) px4 {
	var fa, fb float32
	switch op {
	case "source-over":
		fa, fb = 1, 1-s.a
	case "source-in":
		fa, fb = d.a, 0
	case "source-out":
		fa, fb = 1-d.a, 0
	case "source-atop":
		fa, fb = d.a, 1-s.a
	case "destination-over":
		fa, fb = 1-d.a, 1
	case "destination-in":
		fa, fb = 0, s.a
	case "destination-out":
		fa, fb = 0, 1-s.a
	case "destination-atop":
		fa, fb = 1-d.a, s.a
	case "lighter":
		return px4{min(1, s.r+d.r), min(1, s.g+d.g), min(1, s.b+d.b), min(1, s.a+d.a)}
	case "copy":
		return s
	case "xor":
		fa, fb = 1-d.a, 1-s.a
	default:
		return blendPixel(op, s, d)
	}
	return px4{s.r*fa + d.r*fb, s.g*fa + d.g*fb, s.b*fa + d.b*fb, s.a*fa + d.a*fb}
}

// blendPixel is source-over with a W3C blend mode:
// co = cs·(1−αb) + cb·(1−αs) + αs·αb·B(Cb, Cs).
func blendPixel(op string, s, d px4) px4 {
	if s.a <= 0 {
		return d
	}
	if d.a <= 0 {
		return s
	}
	cs := [3]float32{s.r / s.a, s.g / s.a, s.b / s.a}
	cb := [3]float32{d.r / d.a, d.g / d.a, d.b / d.a}
	var bl [3]float32
	switch op {
	case "hue", "saturation", "color", "luminosity":
		bl = nonSeparable(op, cb, cs)
	default:
		for i := range bl {
			bl[i] = separable(op, cb[i], cs[i])
		}
	}
	ab := s.a * d.a
	out := px4{a: s.a + d.a*(1-s.a)}
	sp := [3]float32{s.r, s.g, s.b}
	dp := [3]float32{d.r, d.g, d.b}
	var o [3]float32
	for i := range o {
		o[i] = sp[i]*(1-d.a) + dp[i]*(1-s.a) + ab*clamp01(bl[i])
	}
	out.r, out.g, out.b = o[0], o[1], o[2]
	return out
}

func clamp01(v float32) float32 { return max(0, min(1, v)) }

func separable(op string, b, s float32) float32 {
	switch op {
	case "multiply":
		return b * s
	case "screen":
		return b + s - b*s
	case "overlay":
		return hardLight(s, b)
	case "darken":
		return min(b, s)
	case "lighten":
		return max(b, s)
	case "color-dodge":
		switch {
		case b == 0:
			return 0
		case s >= 1:
			return 1
		}
		return min(1, b/(1-s))
	case "color-burn":
		switch {
		case b >= 1:
			return 1
		case s <= 0:
			return 0
		}
		return 1 - min(1, (1-b)/s)
	case "hard-light":
		return hardLight(b, s)
	case "soft-light":
		if s <= 0.5 {
			return b - (1-2*s)*b*(1-b)
		}
		var dd float32
		if b <= 0.25 {
			dd = ((16*b-12)*b + 4) * b
		} else {
			dd = float32(math.Sqrt(float64(b)))
		}
		return b + (2*s-1)*(dd-b)
	case "difference":
		if b > s {
			return b - s
		}
		return s - b
	case "exclusion":
		return b + s - 2*b*s
	}
	return s
}

func hardLight(b, s float32) float32 {
	if s <= 0.5 {
		return b * 2 * s
	}
	return separable("screen", b, 2*s-1)
}

func lum(c [3]float32) float32 { return 0.3*c[0] + 0.59*c[1] + 0.11*c[2] }

func clipColor(c [3]float32) [3]float32 {
	l := lum(c)
	n := min(c[0], c[1], c[2])
	x := max(c[0], c[1], c[2])
	if n < 0 {
		for i := range c {
			c[i] = l + (c[i]-l)*l/(l-n)
		}
	}
	if x > 1 {
		for i := range c {
			c[i] = l + (c[i]-l)*(1-l)/(x-l)
		}
	}
	return c
}

func setLum(c [3]float32, l float32) [3]float32 {
	d := l - lum(c)
	return clipColor([3]float32{c[0] + d, c[1] + d, c[2] + d})
}

func sat(c [3]float32) float32 { return max(c[0], c[1], c[2]) - min(c[0], c[1], c[2]) }

func setSat(c [3]float32, s float32) [3]float32 {
	// Indices of max, mid, min.
	i := [3]int{0, 1, 2}
	if c[i[0]] < c[i[1]] {
		i[0], i[1] = i[1], i[0]
	}
	if c[i[1]] < c[i[2]] {
		i[1], i[2] = i[2], i[1]
	}
	if c[i[0]] < c[i[1]] {
		i[0], i[1] = i[1], i[0]
	}
	var out [3]float32
	if c[i[0]] > c[i[2]] {
		out[i[1]] = (c[i[1]] - c[i[2]]) * s / (c[i[0]] - c[i[2]])
		out[i[0]] = s
	}
	return out
}

func nonSeparable(op string, b, s [3]float32) [3]float32 {
	switch op {
	case "hue":
		return setLum(setSat(s, sat(b)), lum(b))
	case "saturation":
		return setLum(setSat(b, sat(s)), lum(b))
	case "color":
		return setLum(s, lum(b))
	}
	return setLum(b, lum(s)) // luminosity
}

// mergeLayer composites layer (premultiplied) into dst over rect with op,
// alpha and the clip mask (nil = no clip; one byte of coverage per pixel,
// stride w).
func mergeLayer(dst, layer *image.RGBA, rect image.Rectangle, op string, alpha float32, mask []uint8) {
	w := dst.Rect.Dx()
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		row := y * dst.Stride
		for x := rect.Min.X; x < rect.Max.X; x++ {
			m := float32(1)
			if mask != nil {
				m = float32(mask[y*w+x]) / 255
				if m == 0 {
					continue
				}
			}
			o := row + 4*x
			sp := layer.Pix[o : o+4 : o+4]
			if sp[3] == 0 && boundedOp(op) {
				continue
			}
			dp := dst.Pix[o : o+4 : o+4]
			s := px4{float32(sp[0]) / 255 * alpha, float32(sp[1]) / 255 * alpha, float32(sp[2]) / 255 * alpha, float32(sp[3]) / 255 * alpha}
			d := px4{float32(dp[0]) / 255, float32(dp[1]) / 255, float32(dp[2]) / 255, float32(dp[3]) / 255}
			r := compositePixel(op, s, d)
			if m < 1 {
				r = px4{d.r + (r.r-d.r)*m, d.g + (r.g-d.g)*m, d.b + (r.b-d.b)*m, d.a + (r.a-d.a)*m}
			}
			a := clamp01(r.a)
			dp[3] = uint8(a*255 + 0.5)
			dp[0] = uint8(min(a, clamp01(r.r))*255 + 0.5)
			dp[1] = uint8(min(a, clamp01(r.g))*255 + 0.5)
			dp[2] = uint8(min(a, clamp01(r.b))*255 + 0.5)
		}
	}
}

// shadowLayer builds the shadow of layer's pixels in rect: its alpha,
// blurred (Gaussian, σ = blur/2, three box passes), offset and tinted.
// It returns the shadow image (full size) and its bounds.
func shadowLayer(layer *image.RGBA, rect image.Rectangle, blur, ox, oy float64, r, g, b, a float32) (*image.RGBA, image.Rectangle) {
	w, h := layer.Rect.Dx(), layer.Rect.Dy()
	sigma := blur / 2
	pad := int(math.Ceil(sigma * 3))
	dx, dy := int(math.Round(ox)), int(math.Round(oy))
	out := rect.Inset(-pad).Add(image.Pt(dx, dy)).Intersect(layer.Rect)
	if out.Empty() {
		return nil, out
	}
	// Alpha plane over the padded source region.
	src := rect.Inset(-pad).Intersect(image.Rect(-1<<20, -1<<20, 1<<20, 1<<20))
	sw, sh := src.Dx(), src.Dy()
	plane := make([]float32, sw*sh)
	for y := max(src.Min.Y, 0); y < min(src.Max.Y, h); y++ {
		for x := max(src.Min.X, 0); x < min(src.Max.X, w); x++ {
			plane[(y-src.Min.Y)*sw+(x-src.Min.X)] = float32(layer.Pix[y*layer.Stride+4*x+3]) / 255
		}
	}
	if sigma > 0 {
		for _, box := range boxSizes(sigma) {
			boxBlur(plane, sw, sh, box)
		}
	}
	img := image.NewRGBA(layer.Rect)
	for y := out.Min.Y; y < out.Max.Y; y++ {
		sy := y - dy - src.Min.Y
		if sy < 0 || sy >= sh {
			continue
		}
		for x := out.Min.X; x < out.Max.X; x++ {
			sx := x - dx - src.Min.X
			if sx < 0 || sx >= sw {
				continue
			}
			v := plane[sy*sw+sx] * a
			if v <= 0 {
				continue
			}
			o := y*img.Stride + 4*x
			img.Pix[o] = uint8(r*v*255 + 0.5)
			img.Pix[o+1] = uint8(g*v*255 + 0.5)
			img.Pix[o+2] = uint8(b*v*255 + 0.5)
			img.Pix[o+3] = uint8(v*255 + 0.5)
		}
	}
	return img, out
}

// boxSizes gives three box widths approximating a Gaussian of sigma.
func boxSizes(sigma float64) [3]int {
	wIdeal := math.Sqrt(12*sigma*sigma/3 + 1)
	wl := int(math.Floor(wIdeal))
	if wl%2 == 0 {
		wl--
	}
	wu := wl + 2
	mIdeal := (12*sigma*sigma - 3*float64(wl*wl) - 12*float64(wl) - 9) / (-4*float64(wl) - 4)
	m := int(math.Round(mIdeal))
	var out [3]int
	for i := range out {
		if i < m {
			out[i] = wl
		} else {
			out[i] = wu
		}
	}
	return out
}

// boxBlur blurs plane in place, horizontally then vertically, with a box
// of width size (odd).
func boxBlur(p []float32, w, h, size int) {
	r := size / 2
	if r <= 0 {
		return
	}
	tmp := make([]float32, max(w, h))
	inv := 1 / float32(2*r+1)
	for y := 0; y < h; y++ {
		row := p[y*w : y*w+w]
		var acc float32
		for x := -r; x <= r; x++ {
			if x >= 0 && x < w {
				acc += row[x]
			}
		}
		for x := 0; x < w; x++ {
			tmp[x] = acc * inv
			if o := x - r; o >= 0 {
				acc -= row[o]
			}
			if n := x + r + 1; n < w {
				acc += row[n]
			}
		}
		copy(row, tmp[:w])
	}
	for x := 0; x < w; x++ {
		var acc float32
		for y := -r; y <= r; y++ {
			if y >= 0 && y < h {
				acc += p[y*w+x]
			}
		}
		for y := 0; y < h; y++ {
			tmp[y] = acc * inv
			if o := y - r; o >= 0 {
				acc -= p[o*w+x]
			}
			if n := y + r + 1; n < h {
				acc += p[n*w+x]
			}
		}
		for y := 0; y < h; y++ {
			p[y*w+x] = tmp[y]
		}
	}
}
