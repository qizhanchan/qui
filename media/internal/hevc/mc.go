package hevc

import "github.com/qizhanchan/qui/media/internal/h264"

// Fractional sample interpolation and weighted sample prediction
// (spec 8.5.3.3).

var lumaFilter = [4][8]int32{
	{0, 0, 0, 64, 0, 0, 0, 0},
	{-1, 4, -10, 58, 17, -5, 1, 0},
	{-1, 4, -11, 40, 40, -11, 4, -1},
	{0, 1, -5, 17, 58, -10, 4, -1},
}

var chromaFilter = [8][4]int32{
	{0, 64, 0, 0},
	{-2, 58, 10, -2},
	{-4, 54, 16, -2},
	{-6, 46, 28, -4},
	{-4, 36, 36, -4},
	{-4, 28, 46, -6},
	{-2, 16, 54, -4},
	{-2, 10, 58, -2},
}

// Prediction samples are kept as int16 with HM's internal offset: the
// stored value is the spec's 14-bit predSample minus 8192, which keeps every
// intermediate of 8-bit video inside 16 bits exactly.
const predOffset = 8192

// mcScratch holds the per-PU prediction buffers (largest PU 64x64). The
// slack after edge and tmp lets the NEON kernels read whole vectors.
type mcScratch struct {
	pred  [2][64 * 64]int16
	edge  [(64+7)*(64+7) + 32]byte
	edge2 [64 * 64]byte
	tmp   [(64+8)*64 + 16]int16
}

// fetch returns the window of w x h source samples whose top-left is
// (x0, y0), with its stride. Windows reaching outside the plane are copied
// into edge with the border samples repeated (8.5.3.3.3.1); so are windows
// lacking slack bytes of readable data after their last row (vector
// kernels read past the window's right edge).
func fetch(edge []byte, src []byte, ss, pw, ph, x0, y0, w, h, slack int) ([]byte, int) {
	if x0 >= 0 && y0 >= 0 && x0+w <= pw && y0+h <= ph && (y0+h-1)*ss+x0+w+slack <= len(src) {
		return src[y0*ss+x0:], ss
	}
	for y := 0; y < h; y++ {
		row := src[clip3(0, ph-1, y0+y)*ss:]
		out := edge[y*w : y*w+w]
		for x := range out {
			out[x] = row[clip3(0, pw-1, x0+x)]
		}
	}
	return edge, w
}

// interpLuma computes the offset 14-bit prediction of a w x h luma block
// at integer position (xi, yi) with quarter-sample fraction (fx, fy).
func (m *mcScratch) interpLuma(dst []int16, src []byte, ss, pw, ph, xi, yi, fx, fy, w, h int) {
	switch {
	case fx == 0 && fy == 0:
		s, st := fetch(m.edge[:], src, ss, pw, ph, xi, yi, w, h, 0)
		copyPred(dst, s, st, w, h)
	case fy == 0:
		s, st := fetch(m.edge[:], src, ss, pw, ph, xi-3, yi, w+7, h, 16)
		filterH(dst, w, s, st, w, h, lumaCoef[fx][:], -predOffset)
	case fx == 0:
		s, st := fetch(m.edge[:], src, ss, pw, ph, xi, yi-3, w, h+7, 16)
		filterV(dst, w, s, st, w, h, lumaCoef[fy][:], -predOffset)
	default:
		s, st := fetch(m.edge[:], src, ss, pw, ph, xi-3, yi-3, w+7, h+7, 16)
		tmp := m.tmp[:]
		filterH(tmp, w, s, st, w, h+7, lumaCoef[fx][:], 0)
		filterV16(dst, w, tmp, w, w, h, lumaCoef[fy][:])
	}
}

// interpChroma is interpLuma for a chroma block with eighth-sample
// fractions and the 4-tap filter.
func (m *mcScratch) interpChroma(dst []int16, src []byte, ss, pw, ph, xi, yi, fx, fy, w, h int) {
	switch {
	case fx == 0 && fy == 0:
		s, st := fetch(m.edge[:], src, ss, pw, ph, xi, yi, w, h, 0)
		copyPred(dst, s, st, w, h)
	case fy == 0:
		s, st := fetch(m.edge[:], src, ss, pw, ph, xi-1, yi, w+3, h, 16)
		filterH(dst, w, s, st, w, h, chromaCoef[fx][:], -predOffset)
	case fx == 0:
		s, st := fetch(m.edge[:], src, ss, pw, ph, xi, yi-1, w, h+3, 16)
		filterV(dst, w, s, st, w, h, chromaCoef[fy][:], -predOffset)
	default:
		s, st := fetch(m.edge[:], src, ss, pw, ph, xi-1, yi-1, w+3, h+3, 16)
		tmp := m.tmp[:]
		filterH(tmp, w, s, st, w, h+3, chromaCoef[fx][:], 0)
		filterV16(dst, w, tmp, w, w, h, chromaCoef[fy][:])
	}
}

var lumaCoef, chromaCoef = func() (l [4][8]int16, c [8][4]int16) {
	for i := range lumaFilter {
		for k, v := range lumaFilter[i] {
			l[i][k] = int16(v)
		}
	}
	for i := range chromaFilter {
		for k, v := range chromaFilter[i] {
			c[i][k] = int16(v)
		}
	}
	return
}()

func copyPred(dst []int16, src []byte, ss, w, h int) {
	for y := 0; y < h; y++ {
		row := src[y*ss : y*ss+w]
		d := dst[y*w : y*w+w]
		for x, v := range row {
			d[x] = int16(int32(v)<<6 - predOffset)
		}
	}
}

// filterH applies the horizontal taps (len(f) of 8 or 4) to bytes, adding
// off: the first stage of a 2-D filter (off 0) or a final 1-D prediction
// (off -predOffset). The sums fit int16 exactly.
func filterH(dst []int16, ds int, src []byte, ss, w, h int, f []int16, off int32) {
	if haveNEON && w&3 == 0 {
		if len(f) == 8 {
			neonFilter8H(&dst[0], 2*ds, &src[0], ss, w, h, &f[0], int(off))
		} else {
			neonFilter4H(&dst[0], 2*ds, &src[0], ss, w, h, &f[0], int(off))
		}
		return
	}
	n := len(f)
	for y := 0; y < h; y++ {
		r := src[y*ss : y*ss+w+n-1]
		d := dst[y*ds : y*ds+w]
		for x := range d {
			v := off
			for k, c := range f {
				v += int32(c) * int32(r[x+k])
			}
			d[x] = int16(v)
		}
	}
}

// filterV applies the vertical taps to bytes (a final 1-D prediction).
func filterV(dst []int16, ds int, src []byte, ss, w, h int, f []int16, off int32) {
	if haveNEON && w&3 == 0 {
		if len(f) == 8 {
			neonFilter8V(&dst[0], 2*ds, &src[0], ss, w, h, &f[0], int(off))
		} else {
			neonFilter4V(&dst[0], 2*ds, &src[0], ss, w, h, &f[0], int(off))
		}
		return
	}
	for y := 0; y < h; y++ {
		d := dst[y*ds : y*ds+w]
		for x := range d {
			v := off
			for k, c := range f {
				v += int32(c) * int32(src[(y+k)*ss+x])
			}
			d[x] = int16(v)
		}
	}
}

// filterV16 is the second (vertical) stage over first-stage sums:
// (sum >> 6) - predOffset, computed in 32 bits.
func filterV16(dst []int16, ds int, src []int16, ss, w, h int, f []int16) {
	if haveNEON && w&3 == 0 {
		if len(f) == 8 {
			neonFilter8V16(&dst[0], 2*ds, &src[0], 2*ss, w, h, &f[0])
		} else {
			neonFilter4V16(&dst[0], 2*ds, &src[0], 2*ss, w, h, &f[0])
		}
		return
	}
	for y := 0; y < h; y++ {
		d := dst[y*ds : y*ds+w]
		for x := range d {
			var v int32 = -predOffset << 6
			for k, c := range f {
				v += int32(c) * int32(src[(y+k)*ss+x])
			}
			d[x] = int16(v >> 6)
		}
	}
}

// motionCompensate predicts the w x h PU at (x0, y0) into the picture.
// Each component whose motion vectors are all integer at its sample
// precision, without explicit weights, reduces to a copy (uni) or a
// rounding average (bi) of reference samples.
func (s *sliceDec) motionCompensate(x0, y0, w, h int, m *motion) {
	hd := s.h
	pic := s.pic
	sp := s.sps
	mc := &s.mc
	weighted := (hd.typ == sliceP && s.pps.WeightedPred) || (hd.typ == sliceB && s.pps.WeightedBipred)
	var refs [2]*picture
	for l := 0; l < 2; l++ {
		if m.pred[l] {
			refs[l] = hd.refList[l][m.ref[l]].pic
			// Wait for the reference's in-loop filters to finish the lines
			// the interpolation taps reach (pipeline.go).
			need := max(y0+m.mv[l][1]>>2+h+4, 2*(y0/2+m.mv[l][1]>>3+h/2+2))
			refs[l].prog.waitLines(min(need, sp.Height))
		}
	}
	for comp := 0; comp < 3; comp++ {
		bx, by, bw, bh := x0, y0, w, h
		pw, ph := sp.Width, sp.Height
		shift, mask := 2, 3
		dst, ds := pic.y, pic.ys
		if comp > 0 {
			bx, by, bw, bh = x0/2, y0/2, w/2, h/2
			pw, ph = pw/2, ph/2
			shift, mask = 3, 7
			dst, ds = pic.cb, pic.cs
			if comp == 2 {
				dst = pic.cr
			}
		}
		dst = dst[by*ds+bx:]
		var src [2][]byte
		var st [2]int
		integer := true
		for l := 0; l < 2; l++ {
			if !m.pred[l] {
				continue
			}
			switch comp {
			case 0:
				src[l], st[l] = refs[l].y, refs[l].ys
			case 1:
				src[l], st[l] = refs[l].cb, refs[l].cs
			default:
				src[l], st[l] = refs[l].cr, refs[l].cs
			}
			if m.mv[l][0]&mask != 0 || m.mv[l][1]&mask != 0 {
				integer = false
			}
		}
		if integer && !weighted {
			var blk [2][]byte
			var bs [2]int
			n := 0
			edge := [2][]byte{mc.edge[:], mc.edge2[:]}
			for l := 0; l < 2; l++ {
				if m.pred[l] {
					blk[n], bs[n] = fetch(edge[n], src[l], st[l], pw, ph, bx+m.mv[l][0]>>shift, by+m.mv[l][1]>>shift, bw, bh, 0)
					n++
				}
			}
			if n == 1 {
				for y := 0; y < bh; y++ {
					copy(dst[y*ds:y*ds+bw], blk[0][y*bs[0]:y*bs[0]+bw])
				}
			} else {
				avgBlock(dst, ds, blk[0], bs[0], blk[1], bs[1], bw, bh)
			}
			continue
		}
		var pred [2][]int16
		for l := 0; l < 2; l++ {
			if !m.pred[l] {
				continue
			}
			mv := m.mv[l]
			pred[l] = mc.pred[l][:bw*bh]
			if comp == 0 {
				mc.interpLuma(pred[l], src[l], st[l], pw, ph, bx+mv[0]>>2, by+mv[1]>>2, mv[0]&3, mv[1]&3, bw, bh)
			} else {
				mc.interpChroma(pred[l], src[l], st[l], pw, ph, bx+mv[0]>>3, by+mv[1]>>3, mv[0]&7, mv[1]&7, bw, bh)
			}
		}
		s.writePred(dst, ds, pred[0], pred[1], bw, bh, comp, m, weighted)
	}
}

// avgBlock sets dst = (a + b + 1) >> 1 over a w x h block, in 16- or
// 8-wide strips through the H.264 decoder's kernel where possible.
func avgBlock(dst []byte, ds int, a []byte, as int, b []byte, bs int, w, h int) {
	strip := 0
	switch {
	case w&15 == 0:
		strip = 16
	case w&7 == 0:
		strip = 8
	}
	if strip == 0 {
		for y := 0; y < h; y++ {
			d := dst[y*ds : y*ds+w]
			ra, rb := a[y*as:y*as+w], b[y*bs:y*bs+w]
			for x := range d {
				d[x] = byte((int(ra[x]) + int(rb[x]) + 1) >> 1)
			}
		}
		return
	}
	for x := 0; x < w; x += strip {
		h264.Avg2(dst[x:], ds, a[x:], as, b[x:], bs, strip, h)
	}
}

// writePred stores the final prediction of one component: the default
// uni / bi rounding or explicit weighted prediction (8.5.3.3.4).
func (s *sliceDec) writePred(dst []byte, ds int, p0, p1 []int16, w, h, comp int, m *motion, weighted bool) {
	if !weighted {
		switch {
		case p0 != nil && p1 != nil:
			if haveNEON && w&3 == 0 {
				neonPutBi(&dst[0], ds, &p0[0], &p1[0], w, h)
				return
			}
			for y := 0; y < h; y++ {
				row := dst[y*ds : y*ds+w]
				a, b := p0[y*w:y*w+w], p1[y*w:y*w+w]
				for x := range row {
					row[x] = clipU8((int(a[x]) + int(b[x]) + 2*predOffset + 64) >> 7)
				}
			}
		default:
			if p0 == nil {
				p0 = p1
			}
			if haveNEON && w&3 == 0 {
				neonPutUni(&dst[0], ds, &p0[0], w, h)
				return
			}
			for y := 0; y < h; y++ {
				row := dst[y*ds : y*ds+w]
				a := p0[y*w : y*w+w]
				for x := range row {
					row[x] = clipU8((int(a[x]) + predOffset + 32) >> 6)
				}
			}
		}
		return
	}
	pw := &s.h.pw
	var wt, of [2]int
	denom := pw.lumaDenom
	if comp > 0 {
		denom = pw.chromaDenom
	}
	for l := 0; l < 2; l++ {
		if !m.pred[l] {
			continue
		}
		r := m.ref[l]
		if comp == 0 {
			wt[l], of[l] = pw.lw[l][r], pw.lo[l][r]
		} else {
			wt[l], of[l] = pw.cw[l][r][comp-1], pw.co[l][r][comp-1]
		}
	}
	log2WD := uint(denom + 6)
	if p0 != nil && p1 != nil {
		w0, w1 := wt[0], wt[1]
		add := (of[0]+of[1]+1)<<log2WD + predOffset*(w0+w1)
		for y := 0; y < h; y++ {
			row := dst[y*ds : y*ds+w]
			a, b := p0[y*w:y*w+w], p1[y*w:y*w+w]
			for x := range row {
				row[x] = clipU8((int(a[x])*w0 + int(b[x])*w1 + add) >> (log2WD + 1))
			}
		}
		return
	}
	l := 0
	if p0 == nil {
		p0, l = p1, 1
	}
	wl, ol := wt[l], of[l]
	rnd := 1<<(log2WD-1) + predOffset*wl
	for y := 0; y < h; y++ {
		row := dst[y*ds : y*ds+w]
		a := p0[y*w : y*w+w]
		for x := range row {
			row[x] = clipU8((int(a[x])*wl+rnd)>>log2WD + ol)
		}
	}
}
