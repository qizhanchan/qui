package av1

// Superres upscaling (spec 7.16) and loop restoration (spec 7.17).

// upscaleFilter is the normative 8-tap superres filter.
var upscaleFilter = [64][8]int32{}

var upscaleFilterFlat = [512]int32{0, 0, 0, 128, 0, 0, 0, 0, 0, 0, -1, 128, 2, -1, 0, 0, 0, 1, -3, 127, 4, -2, 1, 0, 0, 1, -4, 127, 6, -3, 1, 0, 0, 2, -6, 126, 8, -3, 1, 0, 0, 2, -7, 125, 11, -4, 1, 0, -1, 2, -8, 125, 13, -5, 2, 0, -1, 3, -9, 124, 15, -6, 2, 0, -1, 3, -10, 123, 18, -6, 2, -1, -1, 3, -11, 122, 20, -7, 3, -1, -1, 4, -12, 121, 22, -8, 3, -1, -1, 4, -13, 120, 25, -9, 3, -1, -1, 4, -14, 118, 28, -9, 3, -1, -1, 4, -15, 117, 30, -10, 4, -1, -1, 5, -16, 116, 32, -11, 4, -1, -1, 5, -16, 114, 35, -12, 4, -1, -1, 5, -17, 112, 38, -12, 4, -1, -1, 5, -18, 111, 40, -13, 5, -1, -1, 5, -18, 109, 43, -14, 5, -1, -1, 6, -19, 107, 45, -14, 5, -1, -1, 6, -19, 105, 48, -15, 5, -1, -1, 6, -19, 103, 51, -16, 5, -1, -1, 6, -20, 101, 53, -16, 6, -1, -1, 6, -20, 99, 56, -17, 6, -1, -1, 6, -20, 97, 58, -17, 6, -1, -1, 6, -20, 95, 61, -18, 6, -1, -2, 7, -20, 93, 64, -18, 6, -2, -2, 7, -20, 91, 66, -19, 6, -1, -2, 7, -20, 88, 69, -19, 6, -1, -2, 7, -20, 86, 71, -19, 6, -1, -2, 7, -20, 84, 74, -20, 7, -2, -2, 7, -20, 81, 76, -20, 7, -1, -2, 7, -20, 79, 79, -20, 7, -2, -1, 7, -20, 76, 81, -20, 7, -2, -2, 7, -20, 74, 84, -20, 7, -2, -1, 6, -19, 71, 86, -20, 7, -2, -1, 6, -19, 69, 88, -20, 7, -2, -1, 6, -19, 66, 91, -20, 7, -2, -2, 6, -18, 64, 93, -20, 7, -2, -1, 6, -18, 61, 95, -20, 6, -1, -1, 6, -17, 58, 97, -20, 6, -1, -1, 6, -17, 56, 99, -20, 6, -1, -1, 6, -16, 53, 101, -20, 6, -1, -1, 5, -16, 51, 103, -19, 6, -1, -1, 5, -15, 48, 105, -19, 6, -1, -1, 5, -14, 45, 107, -19, 6, -1, -1, 5, -14, 43, 109, -18, 5, -1, -1, 5, -13, 40, 111, -18, 5, -1, -1, 4, -12, 38, 112, -17, 5, -1, -1, 4, -12, 35, 114, -16, 5, -1, -1, 4, -11, 32, 116, -16, 5, -1, -1, 4, -10, 30, 117, -15, 4, -1, -1, 3, -9, 28, 118, -14, 4, -1, -1, 3, -9, 25, 120, -13, 4, -1, -1, 3, -8, 22, 121, -12, 4, -1, -1, 3, -7, 20, 122, -11, 3, -1, -1, 2, -6, 18, 123, -10, 3, -1, 0, 2, -6, 15, 124, -9, 3, -1, 0, 2, -5, 13, 125, -8, 2, -1, 0, 1, -4, 11, 125, -7, 2, 0, 0, 1, -3, 8, 126, -6, 2, 0, 0, 1, -3, 6, 127, -4, 1, 0, 0, 1, -2, 4, 127, -3, 1, 0, 0, 0, -1, 2, 128, -1, 0, 0}

func init() {
	for i := 0; i < 64; i++ {
		for k := 0; k < 8; k++ {
			upscaleFilter[i][k] = upscaleFilterFlat[i*8+k]
		}
	}
}

// cloneFrame returns a copy of f's planes.
func cloneFrame(f *frame) *frame {
	g := *f
	for p := 0; p < f.numPlanes; p++ {
		if f.planes[p].pix16 != nil {
			g.planes[p].pix16 = append([]uint16(nil), f.planes[p].pix16...)
		} else {
			g.planes[p].pix8 = append([]uint8(nil), f.planes[p].pix8...)
		}
	}
	return &g
}

// upscale returns the superres-upscaled frame (spec 7.16).
func upscale(f *frameState, src *frame) *frame {
	h := f.hdr
	dst := newFrame(h.UpscaledWidth, h.Height, src.bitDepth, src.numPlanes)
	for p := 0; p < src.numPlanes; p++ {
		ss := min(p, 1)
		inW := (h.Width + ss) >> ss
		outW := (h.UpscaledWidth + ss) >> ss
		ph := (h.Height + ss) >> ss
		step := ((inW << 14) + outW/2) / outW
		err := outW*step - inW<<14
		x0 := ((-((outW-inW)<<13)+outW/2)/outW + 128 - err/2) & 0x3fff
		maxX := (f.miCols*4)>>ss - 1
		sp, dp := &src.planes[p], &dst.planes[p]
		maxV := int32(1)<<src.bitDepth - 1
		for y := 0; y < ph; y++ {
			for x := 0; x < outW; x++ {
				srcX := -(1 << 14) + x0 + x*step
				srcP := srcX >> 14
				fl := &upscaleFilter[(srcX&0x3fff)>>8]
				sum := int32(0)
				for k := 0; k < 8; k++ {
					sum += sp.get(clip3(0, maxX, srcP+k-3), y) * fl[k]
				}
				dp.set(x, y, min(max((sum+64)>>7, 0), maxV))
			}
		}
	}
	return dst
}

// lrState holds the inputs of loop restoration for one plane and the
// stripe being filtered. Each stripe (64 luma rows, offset 8 rows up) is
// first copied into buf with three samples of padding on every side,
// following the spec's sample fetch (7.17.6): coordinates are clamped to
// the plane, and rows above / below the stripe come from the deblocked
// (pre-CDEF) frame, at most two rows beyond the stripe.
type lrState struct {
	cdef, deblock *frame // UpscaledCdefFrame, UpscaledCurrFrame
	bd            int
	planeW        int
	stride        int
	top           int     // first plane row of the stripe; buf row 3
	buf           []int32 // buf[(r+3)*stride + c+3] = sample(c, top+r)
	inter         []int32
	sgrA, sgrB    []int32
	flt           [2][]int32
	hs, hq        []int32
}

// rowFrom returns the samples of plane row y of f as int32 into dst
// (planeW samples).
func rowFrom(f *frame, plane, y int, dst []int32) {
	pb := &f.planes[plane]
	if pb.pix16 != nil {
		for i, v := range pb.pix16[y*pb.stride : y*pb.stride+len(dst)] {
			dst[i] = int32(v)
		}
		return
	}
	for i, v := range pb.pix8[y*pb.stride : y*pb.stride+len(dst)] {
		dst[i] = int32(v)
	}
}

// loadStripe fills buf for the stripe [stripeStart, stripeEnd] whose
// visible rows are top..bot.
func (s *lrState) loadStripe(plane, stripeStart, stripeEnd, top, bot, planeEndY int) {
	s.top = top
	w := s.planeW
	for r := -3; r <= bot-top+3; r++ {
		y := clip3(0, planeEndY, top+r)
		src := s.cdef
		switch {
		case y < stripeStart:
			src, y = s.deblock, max(stripeStart-2, y)
		case y > stripeEnd:
			src, y = s.deblock, min(stripeEnd+2, y)
		}
		row := s.buf[(r+3)*s.stride:]
		rowFrom(src, plane, y, row[3:3+w])
		row[0], row[1], row[2] = row[3], row[3], row[3]
		row[3+w], row[4+w], row[5+w] = row[2+w], row[2+w], row[2+w]
	}
}

// restore applies loop restoration, returning the restored frame.
// Stripes are filtered in parallel, each worker with its own buffers.
func (d *Decoder) restore(f *frameState, cdefF, deblockF *frame) *frame {
	h := f.hdr
	out := d.cloneFrame(cdefF)
	states := make([]lrState, workers())
	for p := 0; p < cdefF.numPlanes; p++ {
		if h.lrType[p] == restoreNone {
			continue
		}
		ss := min(p, 1)
		lp := &f.lr[p]
		unit := h.lrUnitSize[p]
		planeEndX := (h.UpscaledWidth+ss)>>ss - 1
		planeEndY := (h.Height+ss)>>ss - 1
		nStripes := (planeEndY + 1 + 8>>ss + 64>>ss - 1) / (64 >> ss)
		parallelFor(nStripes, func(wk, stripe int) {
			s := &states[wk]
			s.cdef, s.deblock, s.bd = cdefF, deblockF, cdefF.bitDepth
			s.planeW = planeEndX + 1
			s.stride = s.planeW + 6
			if need := s.stride * (64 + 6); len(s.buf) < need {
				s.buf = make([]int32, need)
			}
			stripeStart := (-8 + stripe*64) >> ss
			stripeEnd := stripeStart + 64>>ss - 1
			top, bot := max(stripeStart, 0), min(stripeEnd, planeEndY)
			if top > planeEndY {
				return
			}
			unitRow := min(lp.rows-1, (top+8>>ss)/unit)
			loaded := false
			for uc := 0; uc < lp.cols; uc++ {
				u := &lp.units[unitRow*lp.cols+uc]
				if u.typ != restoreWiener && u.typ != restoreSgrproj {
					continue
				}
				x0, x1 := uc*unit, (uc+1)*unit
				if uc == lp.cols-1 {
					x1 = s.planeW
				}
				x1 = min(x1, s.planeW)
				if x0 >= x1 {
					continue
				}
				if !loaded {
					s.loadStripe(p, stripeStart, stripeEnd, top, bot, planeEndY)
					loaded = true
				}
				if u.typ == restoreWiener {
					s.wiener(out, p, u, x0, x1-x0, bot-top+1)
				} else {
					s.selfGuided(out, p, u, x0, x1-x0, bot-top+1)
				}
			}
		})
	}
	return out
}

// storeRow writes clipped values to plane row y from column x.
func storeRow(out *frame, plane, x, y int, v []int32) {
	pb := &out.planes[plane]
	maxV := int32(1)<<out.bitDepth - 1
	if pb.pix16 != nil {
		d := pb.pix16[y*pb.stride+x : y*pb.stride+x+len(v)]
		for i, s := range v {
			d[i] = uint16(min(max(s, 0), maxV))
		}
		return
	}
	d := pb.pix8[y*pb.stride+x : y*pb.stride+x+len(v)]
	for i, s := range v {
		d[i] = uint8(min(max(s, 0), maxV))
	}
}

func grow(b []int32, n int) []int32 {
	if cap(b) < n {
		return make([]int32, n)
	}
	return b[:n]
}

// wiener filters columns x..x+w-1 of the loaded stripe's h rows (spec
// 7.17.4).
func (s *lrState) wiener(out *frame, plane int, u *lrUnit, x, w, h int) {
	bd := s.bd
	r0, r1 := uint(3), uint(11)
	if bd == 12 {
		r0, r1 = 5, 9
	}
	var vf, hf [7]int32
	mk := func(c [3]int8, fl *[7]int32) {
		fl[3] = 128
		for i := 0; i < 3; i++ {
			fl[i] = int32(c[i])
			fl[6-i] = int32(c[i])
			fl[3] -= 2 * int32(c[i])
		}
	}
	mk(u.wiener[0], &vf)
	mk(u.wiener[1], &hf)
	offset := int32(1) << (uint(bd) + 7 - r0 - 1)
	limit := int32(1)<<(uint(bd)+1+7-r0) - 1
	s.inter = grow(s.inter, (h+6)*w)
	inter := s.inter
	h0, h1, h2, h3, h4, h5, h6 := hf[0], hf[1], hf[2], hf[3], hf[4], hf[5], hf[6]
	rnd0 := int32(1) << (r0 - 1)
	for r := 0; r < h+6; r++ {
		src := s.buf[r*s.stride+x:]
		src = src[:w+6]
		d := inter[r*w : r*w+w]
		for c := range d {
			p := src[c : c+7 : c+7]
			sum := h0*p[0] + h1*p[1] + h2*p[2] + h3*p[3] + h4*p[4] + h5*p[5] + h6*p[6]
			d[c] = min(max((sum+rnd0)>>r0, -offset), limit-offset)
		}
	}
	v0, v1, v2, v3, v4, v5, v6 := vf[0], vf[1], vf[2], vf[3], vf[4], vf[5], vf[6]
	rnd1 := int32(1) << (r1 - 1)
	row := grow(s.hs, w)
	s.hs = row
	for r := 0; r < h; r++ {
		a := inter[r*w:]
		a = a[:7*w]
		for c := range row {
			sum := v0*a[c] + v1*a[w+c] + v2*a[2*w+c] + v3*a[3*w+c] + v4*a[4*w+c] + v5*a[5*w+c] + v6*a[6*w+c]
			row[c] = (sum + rnd1) >> r1
		}
		storeRow(out, plane, x, s.top+r, row)
	}
}

// boxFilter is the box filter process (spec 7.17.3) for one pass over
// columns x..x+w-1 of the loaded stripe's h rows; the output is in
// s.flt[pass] (stride w).
func (s *lrState) boxFilter(x, w, h, set, pass int) {
	bd := s.bd
	r := sgrParams[set][pass*2]
	sc := int64(sgrParams[set][pass*2+1])
	n := (2*r + 1) * (2*r + 1)
	oneOverN := int64(((1 << 12) + n/2) / n)
	aw := w + 2 // A and B cover rows -1..h, columns x-1..x+w
	s.sgrA = grow(s.sgrA, (h+2)*aw)
	s.sgrB = grow(s.sgrB, (h+2)*aw)
	// Horizontal box sums of samples and squares for buffer rows
	// -1-r..h+r, columns x-1..x+w.
	rows := h + 2 + 2*r
	s.hs = grow(s.hs, rows*aw)
	s.hq = grow(s.hq, rows*aw)
	for i := 0; i < rows; i++ {
		src := s.buf[(i+2-r)*s.stride+x+2-r:]
		src = src[:aw+2*r]
		hs, hq := s.hs[i*aw:i*aw+aw], s.hq[i*aw:i*aw+aw]
		for j := range hs {
			var a, b int32
			for _, v := range src[j : j+2*r+1] {
				a += v * v
				b += v
			}
			hs[j], hq[j] = b, a
		}
	}
	sh := uint(bd - 8)
	for i := 0; i < h+2; i++ {
		absRow := s.top + i - 1
		if pass == 0 && absRow&1 == 0 {
			// Pass 0 only reads A and B on odd rows.
			continue
		}
		A, B := s.sgrA[i*aw:i*aw+aw], s.sgrB[i*aw:i*aw+aw]
		for j := range A {
			var a, b int64
			for k := 0; k <= 2*r; k++ {
				a += int64(s.hq[(i+k)*aw+j])
				b += int64(s.hs[(i+k)*aw+j])
			}
			if sh > 0 {
				a = (a + 1<<(2*sh-1)) >> (2 * sh)
			}
			dd := b
			if sh > 0 {
				dd = (b + 1<<(sh-1)) >> sh
			}
			p := max(0, a*int64(n)-dd*dd)
			z := (p*sc + 1<<19) >> 20
			var a2 int64
			switch {
			case z >= 255:
				a2 = 256
			case z == 0:
				a2 = 1
			default:
				a2 = ((z << 8) + z/2) / (z + 1)
			}
			b2 := ((1 << 8) - a2) * b * oneOverN
			A[j] = int32(a2)
			B[j] = int32((b2 + 1<<11) >> 12)
		}
	}
	s.flt[pass] = grow(s.flt[pass], h*w)
	flt := s.flt[pass]
	for i := 0; i < h; i++ {
		absRow := s.top + i
		src := s.buf[(i+3)*s.stride+x+3:]
		src = src[:w]
		out := flt[i*w : i*w+w]
		up, mid, dn := (i)*aw, (i+1)*aw, (i+2)*aw
		if pass == 0 {
			if absRow&1 != 0 {
				A, B := s.sgrA[mid:mid+aw], s.sgrB[mid:mid+aw]
				for j := range out {
					a := 5*(A[j]+A[j+2]) + 6*A[j+1]
					b := 5*(B[j]+B[j+2]) + 6*B[j+1]
					out[j] = int32((int64(a)*int64(src[j]) + int64(b) + 1<<7) >> 8)
				}
			} else {
				Au, Bu := s.sgrA[up:up+aw], s.sgrB[up:up+aw]
				Ad, Bd := s.sgrA[dn:dn+aw], s.sgrB[dn:dn+aw]
				for j := range out {
					a := 5*(Au[j]+Au[j+2]+Ad[j]+Ad[j+2]) + 6*(Au[j+1]+Ad[j+1])
					b := 5*(Bu[j]+Bu[j+2]+Bd[j]+Bd[j+2]) + 6*(Bu[j+1]+Bd[j+1])
					out[j] = int32((int64(a)*int64(src[j]) + int64(b) + 1<<8) >> 9)
				}
			}
			continue
		}
		Au, Bu := s.sgrA[up:up+aw], s.sgrB[up:up+aw]
		Am, Bm := s.sgrA[mid:mid+aw], s.sgrB[mid:mid+aw]
		Ad, Bd := s.sgrA[dn:dn+aw], s.sgrB[dn:dn+aw]
		for j := range out {
			a := 3*(Au[j]+Au[j+2]+Ad[j]+Ad[j+2]) + 4*(Au[j+1]+Ad[j+1]+Am[j]+Am[j+1]+Am[j+2])
			b := 3*(Bu[j]+Bu[j+2]+Bd[j]+Bd[j+2]) + 4*(Bu[j+1]+Bd[j+1]+Bm[j]+Bm[j+1]+Bm[j+2])
			out[j] = int32((int64(a)*int64(src[j]) + int64(b) + 1<<8) >> 9)
		}
	}
}

// selfGuided is the self guided filter process (spec 7.17.2).
func (s *lrState) selfGuided(out *frame, plane int, u *lrUnit, x, w, h int) {
	set := int(u.sgrSet)
	r0, r1 := sgrParams[set][0], sgrParams[set][2]
	if r0 != 0 {
		s.boxFilter(x, w, h, set, 0)
	}
	if r1 != 0 {
		s.boxFilter(x, w, h, set, 1)
	}
	w0, w1 := int64(u.xqd[0]), int64(u.xqd[1])
	w2 := (1 << 7) - w0 - w1
	s.inter = grow(s.inter, w)
	row := s.inter
	for i := 0; i < h; i++ {
		src := s.buf[(i+3)*s.stride+x+3:]
		src = src[:w]
		var f0, f1 []int32
		if r0 != 0 {
			f0 = s.flt[0][i*w : i*w+w]
		}
		if r1 != 0 {
			f1 = s.flt[1][i*w : i*w+w]
		}
		for j, c := range src {
			uu := int64(c) << 4
			v := w1 * uu
			if f0 != nil {
				v += w0 * int64(f0[j])
			} else {
				v += w0 * uu
			}
			if f1 != nil {
				v += w2 * int64(f1[j])
			} else {
				v += w2 * uu
			}
			row[j] = int32((v + 1<<10) >> 11)
		}
		storeRow(out, plane, x, s.top+i, row)
	}
}
