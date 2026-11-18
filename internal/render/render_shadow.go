package render

import (
	"math"

	"github.com/qizhanchan/qui/internal/gfx"
)

// DrawShadow projects shadow geometry (rect, radius, blur, spread,
// offset) through the current state-stack scale, then hands physical-
// coord parameters to the backend. The CPU backend's DrawShadow
// implementation (drawShadowImplRaw in backend_cpu.go) renders the
// shadow into a small alpha buffer, blurs it via separable box-blur
// passes (a fast Gaussian approximation), and composites tinted.
// HiDPI hosts get visually-matched blur radii; a Save+ClipRect scope
// trims the halo without bleeding into siblings (the Dialog level-3
// hover-shadow bug that motivated the clip parameter).
func (c imageCanvas) DrawShadow(rect gfx.Rect, radius float32, spec gfx.ElevationSpec, shadowColor gfx.Color) {
	if c.Backend == nil {
		return
	}
	top := c.TopCopy()
	s := top.Matrix.AvgScale()
	scaledSpec := gfx.ElevationSpec{
		X: spec.X * s, Y: spec.Y * s,
		Blur: spec.Blur * s, Spread: spec.Spread * s,
		Opacity: spec.Opacity, Knockout: spec.Knockout, Inset: spec.Inset,
	}
	physRect, physRadius := projectAARoundedShape(top.Matrix, rect, radius)
	c.Backend.DrawShadow(physRect, physRadius, scaledSpec, shadowColor, top.Clip)
}

// stampRoundedAlpha fills `buf` (bufW×bufH, packed row-major) with
// 0xff inside a rounded-rect footprint at offset (margin, margin), and
// with AA coverage at the four corners. Shared by the CPU DrawShadow
// implementation and other blur-based effects.
func stampRoundedAlpha(buf []uint8, bufW, bufH, margin int, w, h, radius float32) {
	stampRoundedAlphaAt(buf, bufW, bufH, margin, margin, w, h, radius)
}

// stampRoundedAlphaAt is stampRoundedAlpha with the rect's top-left at
// (left, top) in the buffer — which may be negative when the buffer is a
// window onto a larger shadow.
func stampRoundedAlphaAt(buf []uint8, bufW, bufH, left, top int, w, h, radius float32) {
	if radius < 0 {
		radius = 0
	}
	maxR := w / 2
	if h/2 < maxR {
		maxR = h / 2
	}
	if radius > maxR {
		radius = maxR
	}
	right := left + int(math.Round(float64(w)))
	bottom := top + int(math.Round(float64(h)))
	rInt := int(math.Round(float64(radius)))
	if maxRX := (right - left) / 2; rInt > maxRX {
		rInt = maxRX
	}
	if maxRY := (bottom - top) / 2; rInt > maxRY {
		rInt = maxRY
	}
	fillBand := func(x0, y0, x1, y1 int) {
		if x0 < 0 {
			x0 = 0
		}
		if y0 < 0 {
			y0 = 0
		}
		if x1 > bufW {
			x1 = bufW
		}
		if y1 > bufH {
			y1 = bufH
		}
		for y := y0; y < y1; y++ {
			row := buf[y*bufW+x0 : y*bufW+x1]
			for i := range row {
				row[i] = 0xff
			}
		}
	}
	fillBand(left, top+rInt, right, bottom-rInt)
	fillBand(left+rInt, top, right-rInt, top+rInt)
	fillBand(left+rInt, bottom-rInt, right-rInt, bottom)

	stampCorner := func(cxF, cyF float32, x0, y0, x1, y1 int) {
		for y := y0; y < y1; y++ {
			if y < 0 || y >= bufH {
				continue
			}
			for x := x0; x < x1; x++ {
				if x < 0 || x >= bufW {
					continue
				}
				dx := float32(x) + 0.5 - cxF
				dy := float32(y) + 0.5 - cyF
				dist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
				cov := radius + 0.5 - dist
				if cov >= 1 {
					buf[y*bufW+x] = 0xff
				} else if cov > 0 {
					buf[y*bufW+x] = uint8(cov * 255)
				}
			}
		}
	}
	if rInt > 0 {
		stampCorner(float32(left)+radius, float32(top)+radius, left, top, left+rInt, top+rInt)
		stampCorner(float32(right)-radius, float32(top)+radius, right-rInt, top, right, top+rInt)
		stampCorner(float32(left)+radius, float32(bottom)-radius, left, bottom-rInt, left+rInt, bottom)
		stampCorner(float32(right)-radius, float32(bottom)-radius, right-rInt, bottom-rInt, right, bottom)
	}
}

// Shadow edges and their knockout must share the exact fractional border
// geometry. Rounding origins and dimensions independently can erase a thin
// bottom ring, particularly at HiDPI scales.
//
// Coverage is analytic: a pixel's coverage of the straight parts is the
// exact overlap of the pixel with the rect (so sub-pixel rings keep their
// true weight); in the corner squares it is additionally limited by the
// distance to the corner circle. Rows of the straight middle band only
// evaluate their edge pixels. dst covers [x, x+w) × [y, y+h) in pixels.
func stampShadowAlpha(dst []uint8, w, h, x, y int, rect gfx.Rect, radius float32) {
	clear(dst[:w*h])
	radius = max(0, min(radius, min(rect.W, rect.H)/2))
	l, t := float64(rect.X), float64(rect.Y)
	r, b := l+float64(rect.W), t+float64(rect.H)
	if r <= l || b <= t {
		return
	}
	rad := float64(radius)
	overlap := func(p, lo, hi float64) float64 { // [p, p+1] ∩ [lo, hi]
		return max(0, min(p+1, hi)-max(p, lo))
	}
	// Pixel columns that can be touched, in buffer coordinates.
	cx0 := max(int(math.Floor(l))-x, 0)
	cx1 := min(int(math.Ceil(r))-x, w)
	cy0 := max(int(math.Floor(t))-y, 0)
	cy1 := min(int(math.Ceil(b))-y, h)
	if cx0 >= cx1 || cy0 >= cy1 {
		return
	}
	// Columns strictly inside the straight horizontal span need no
	// x-edge or corner work.
	inX0 := max(int(math.Ceil(l+rad))-x, cx0)
	inX1 := min(int(math.Floor(r-rad))-x, cx1)
	for j := cy0; j < cy1; j++ {
		py := float64(y + j)
		covY := overlap(py, t, b)
		if covY <= 0 {
			continue
		}
		row := dst[j*w : (j+1)*w]
		// Is the whole pixel row inside the straight vertical band?
		midRow := py >= t+rad && py+1 <= b-rad
		for i := cx0; i < cx1; i++ {
			if midRow && i >= inX0 && i < inX1 && covY >= 1 {
				row[i] = 0xff
				continue
			}
			px := float64(x + i)
			cov := overlap(px, l, r) * covY
			if rad > 0 {
				// Corner squares: limit by the circle's coverage estimate.
				qx, qy := px+0.5, py+0.5
				var ccx, ccy float64
				inCorner := false
				switch {
				case qx < l+rad && qy < t+rad:
					ccx, ccy, inCorner = l+rad, t+rad, true
				case qx > r-rad && qy < t+rad:
					ccx, ccy, inCorner = r-rad, t+rad, true
				case qx < l+rad && qy > b-rad:
					ccx, ccy, inCorner = l+rad, b-rad, true
				case qx > r-rad && qy > b-rad:
					ccx, ccy, inCorner = r-rad, b-rad, true
				}
				if inCorner {
					cc := rad + 0.5 - math.Hypot(qx-ccx, qy-ccy)
					cov = min(cov, max(0, min(1, cc)))
				}
			}
			if cov > 0 {
				row[i] = uint8(math.Round(cov * 255))
			}
		}
	}
}
