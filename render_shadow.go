package qui

import "math"

// DrawShadow projects shadow geometry (rect, radius, blur, spread,
// offset) through the current state-stack scale, then hands physical-
// coord parameters to the backend. The CPU backend's DrawShadow
// implementation (drawShadowImplRaw in backend_cpu.go) renders the
// shadow into a small alpha buffer, blurs it via separable box-blur
// passes (a fast Gaussian approximation), and composites tinted.
// HiDPI hosts get visually-matched blur radii; a Save+ClipRect scope
// trims the halo without bleeding into siblings (the Dialog level-3
// hover-shadow bug that motivated the clip parameter).
func (c imageCanvas) DrawShadow(rect Rect, radius float32, spec ElevationSpec, shadowColor Color) {
	if c.backend == nil {
		return
	}
	top := c.topCopy()
	s := top.matrix.avgScale()
	scaledSpec := ElevationSpec{
		X: spec.X * s, Y: spec.Y * s,
		Blur: spec.Blur * s, Spread: spec.Spread * s,
		Opacity: spec.Opacity,
	}
	physRect, physRadius := projectAARoundedShape(top.matrix, rect, radius)
	c.backend.DrawShadow(physRect, physRadius, scaledSpec, shadowColor, top.clip)
}

// stampRoundedAlpha fills `buf` (bufW×bufH, packed row-major) with
// 0xff inside a rounded-rect footprint at offset (margin, margin), and
// with AA coverage at the four corners. Shared by the CPU DrawShadow
// implementation and other blur-based effects.
func stampRoundedAlpha(buf []uint8, bufW, bufH, margin int, w, h, radius float32) {
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
	left := margin
	top := margin
	right := margin + int(math.Round(float64(w)))
	bottom := margin + int(math.Round(float64(h)))
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

// boxBlurH runs one horizontal box-blur pass with kernel radius r.
// Uses a running sum so per-pixel cost is O(1) regardless of r.
func boxBlurH(src, dst []uint8, w, h, r int) {
	if r <= 0 {
		copy(dst, src)
		return
	}
	k := 2*r + 1
	for y := 0; y < h; y++ {
		rowOff := y * w
		var sum int
		sum += int(src[rowOff]) * (r + 1)
		for i := 1; i <= r && i < w; i++ {
			sum += int(src[rowOff+i])
		}
		for x := 0; x < w; x++ {
			dst[rowOff+x] = uint8(sum / k)
			outIdx := x - r
			if outIdx < 0 {
				outIdx = 0
			}
			inIdx := x + r + 1
			if inIdx >= w {
				inIdx = w - 1
			}
			sum += int(src[rowOff+inIdx]) - int(src[rowOff+outIdx])
		}
	}
}

// boxBlurV is the vertical analogue. Same running-sum trick.
func boxBlurV(src, dst []uint8, w, h, r int) {
	if r <= 0 {
		copy(dst, src)
		return
	}
	k := 2*r + 1
	for x := 0; x < w; x++ {
		var sum int
		sum += int(src[x]) * (r + 1)
		for i := 1; i <= r && i < h; i++ {
			sum += int(src[i*w+x])
		}
		for y := 0; y < h; y++ {
			dst[y*w+x] = uint8(sum / k)
			outIdx := y - r
			if outIdx < 0 {
				outIdx = 0
			}
			inIdx := y + r + 1
			if inIdx >= h {
				inIdx = h - 1
			}
			sum += int(src[inIdx*w+x]) - int(src[outIdx*w+x])
		}
	}
}
