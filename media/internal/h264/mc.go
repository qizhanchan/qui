package h264

// Motion compensation (spec 8.5.6). Motion vectors are in quarter-luma-sample
// units; the fractional luma samples use the 6-tap Wiener filter and chroma
// uses 1/8-sample bilinear interpolation. All source coordinates are clamped to
// the reference picture edges.

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// interpLuma returns one interpolated luma sample. (x,y) is the integer source
// position and (fx,fy) are the fractional parts in quarter-sample units.
func interpLuma(p *Plane, x, y, fx, fy int) byte {
	s := func(dx, dy int) int {
		return int(p.At(clampInt(x+dx, 0, p.W-1), clampInt(y+dy, 0, p.H-1)))
	}
	bh := func(dx, dy int) int { // half-pel horizontal between (x+dx) and (x+dx+1)
		return clampInt((s(dx-2, dy)-5*s(dx-1, dy)+20*s(dx, dy)+20*s(dx+1, dy)-5*s(dx+2, dy)+s(dx+3, dy)+16)>>5, 0, 255)
	}
	bv := func(dx, dy int) int { // half-pel vertical between (y+dy) and (y+dy+1)
		return clampInt((s(dx, dy-2)-5*s(dx, dy-1)+20*s(dx, dy)+20*s(dx, dy+1)-5*s(dx, dy+2)+s(dx, dy+3)+16)>>5, 0, 255)
	}
	bvRaw := func(dx, dy int) int { // vertical 6-tap, unclipped (16-bit intermediate)
		return s(dx, dy-2) - 5*s(dx, dy-1) + 20*s(dx, dy) + 20*s(dx, dy+1) - 5*s(dx, dy+2) + s(dx, dy+3)
	}
	j := func(dx, dy int) int { // half-pel both directions: vertical then horizontal
		return clampInt((bvRaw(dx-2, dy)-5*bvRaw(dx-1, dy)+20*bvRaw(dx, dy)+20*bvRaw(dx+1, dy)-5*bvRaw(dx+2, dy)+bvRaw(dx+3, dy)+512)>>10, 0, 255)
	}

	var v int
	switch {
	case fx == 0 && fy == 0:
		v = s(0, 0)
	case fx == 2 && fy == 0:
		v = bh(0, 0)
	case fx == 0 && fy == 2:
		v = bv(0, 0)
	case fx == 2 && fy == 2:
		v = j(0, 0)
	case fx == 1 && fy == 0:
		v = (s(0, 0) + bh(0, 0) + 1) >> 1
	case fx == 3 && fy == 0:
		v = (bh(0, 0) + s(1, 0) + 1) >> 1
	case fx == 0 && fy == 1:
		v = (s(0, 0) + bv(0, 0) + 1) >> 1
	case fx == 0 && fy == 3:
		v = (bv(0, 0) + s(0, 1) + 1) >> 1
	case fx == 2 && fy == 1:
		v = (bh(0, 0) + j(0, 0) + 1) >> 1
	case fx == 2 && fy == 3:
		v = (bh(0, 1) + j(0, 0) + 1) >> 1
	case fx == 1 && fy == 2:
		v = (bv(0, 0) + j(0, 0) + 1) >> 1
	case fx == 3 && fy == 2:
		v = (j(0, 0) + bv(1, 0) + 1) >> 1
	case fx == 1 && fy == 1:
		v = (bh(0, 0) + bv(0, 0) + 1) >> 1
	case fx == 3 && fy == 1:
		v = (bh(0, 0) + bv(1, 0) + 1) >> 1
	case fx == 1 && fy == 3:
		v = (bh(0, 1) + bv(0, 0) + 1) >> 1
	case fx == 3 && fy == 3:
		v = (bh(0, 1) + bv(1, 0) + 1) >> 1
	}
	return byte(v)
}

// weightBuf applies weighted-prediction scaling to n samples in place:
//
//	out = clip(((weight * in + (1<<(denom-1))) >> denom) + offset)
//
// When the weight is the default (1<<denom) and offset is 0 it is a no-op.
func weightBuf(buf []byte, n, weight, offset, denom int) {
	if weight == (1<<uint(denom)) && offset == 0 {
		return
	}
	round := 0
	if denom > 0 {
		round = 1 << uint(denom-1)
	}
	for i := 0; i < n; i++ {
		v := (weight*int(buf[i]) + round) >> uint(denom)
		buf[i] = clipPixel(v + offset)
	}
}

// predLuma writes the (w x h) luma prediction of a partition at (x0,y0) into a
// byte buffer with the given stride (used for bi-prediction).
func predLuma(dst []byte, dstStride int, ref *Plane, x0, y0, w, h, mvx, mvy int) {
	ix, iy := mvx>>2, mvy>>2
	fx, fy := mvx&3, mvy&3
	bx, by := x0+ix, y0+iy
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			var v byte
			if fx == 0 && fy == 0 {
				v = ref.At(clampInt(bx+i, 0, ref.W-1), clampInt(by+j, 0, ref.H-1))
			} else {
				v = interpLuma(ref, bx+i, by+j, fx, fy)
			}
			dst[j*dstStride+i] = v
		}
	}
}

// predChroma writes the (w x h) chroma prediction into a byte buffer.
func predChroma(dst []byte, dstStride int, ref *Plane, x0, y0, w, h, mvx, mvy int) {
	ix, iy := mvx>>3, mvy>>3
	fx, fy := mvx&7, mvy&7
	bx, by := x0+ix, y0+iy
	at := func(x, y int) int {
		return int(ref.At(clampInt(x, 0, ref.W-1), clampInt(y, 0, ref.H-1)))
	}
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			a := at(bx+i, by+j)
			b := at(bx+i+1, by+j)
			c := at(bx+i, by+j+1)
			d := at(bx+i+1, by+j+1)
			v := ((8-fx)*(8-fy)*a + fx*(8-fy)*b + (8-fx)*fy*c + fx*fy*d + 32) >> 6
			dst[j*dstStride+i] = byte(v)
		}
	}
}

// mcLuma copies the (w x h) luma prediction for a partition at (x0,y0) in the
// destination from ref, displaced by (mvx,mvy) quarter-samples.
func mcLuma(dst, ref *Plane, x0, y0, w, h, mvx, mvy int) {
	ix, iy := mvx>>2, mvy>>2
	fx, fy := mvx&3, mvy&3
	bx, by := x0+ix, y0+iy
	if fx == 0 && fy == 0 {
		for j := 0; j < h; j++ {
			for i := 0; i < w; i++ {
				dst.Set(x0+i, y0+j, ref.At(clampInt(bx+i, 0, ref.W-1), clampInt(by+j, 0, ref.H-1)))
			}
		}
		return
	}
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			dst.Set(x0+i, y0+j, interpLuma(ref, bx+i, by+j, fx, fy))
		}
	}
}

// mcChroma copies the (w x h) chroma prediction using 1/8-sample bilinear
// interpolation. The luma motion vector maps to chroma integer = mv>>3 and
// eighth fraction = mv&7.
func mcChroma(dst, ref *Plane, x0, y0, w, h, mvx, mvy int) {
	ix, iy := mvx>>3, mvy>>3
	fx, fy := mvx&7, mvy&7
	bx, by := x0+ix, y0+iy
	at := func(x, y int) int {
		return int(ref.At(clampInt(x, 0, ref.W-1), clampInt(y, 0, ref.H-1)))
	}
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			a := at(bx+i, by+j)
			b := at(bx+i+1, by+j)
			c := at(bx+i, by+j+1)
			d := at(bx+i+1, by+j+1)
			v := ((8-fx)*(8-fy)*a + fx*(8-fy)*b + (8-fx)*fy*c + fx*fy*d + 32) >> 6
			dst.Set(x0+i, y0+j, byte(v))
		}
	}
}
