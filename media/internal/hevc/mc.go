package hevc

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

// interp computes the 14-bit prediction of a w x h block of plane src
// (pw x ph, stride ss) at integer position (xi, yi) with fraction (fx, fy)
// using taps (8 for luma, 4 for chroma) into dst (stride w).
func interp(dst []int32, src []byte, ss, pw, ph, xi, yi, fx, fy, w, h int, filt func(int) []int32, taps int) {
	half := taps/2 - 1
	at := func(x, y int) int32 {
		return int32(src[clip3(0, ph-1, y)*ss+clip3(0, pw-1, x)])
	}
	switch {
	case fx == 0 && fy == 0:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst[y*w+x] = at(xi+x, yi+y) << 6
			}
		}
	case fy == 0:
		f := filt(fx)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				var s int32
				for i := 0; i < taps; i++ {
					s += f[i] * at(xi+x+i-half, yi+y)
				}
				dst[y*w+x] = s
			}
		}
	case fx == 0:
		f := filt(fy)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				var s int32
				for i := 0; i < taps; i++ {
					s += f[i] * at(xi+x, yi+y+i-half)
				}
				dst[y*w+x] = s
			}
		}
	default:
		fh, fv := filt(fx), filt(fy)
		th := h + taps - 1
		tmp := make([]int32, w*th)
		for y := 0; y < th; y++ {
			for x := 0; x < w; x++ {
				var s int32
				for i := 0; i < taps; i++ {
					s += fh[i] * at(xi+x+i-half, yi+y-half)
				}
				tmp[y*w+x] = s
			}
		}
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				var s int32
				for i := 0; i < taps; i++ {
					s += fv[i] * tmp[(y+i)*w+x]
				}
				dst[y*w+x] = s >> 6
			}
		}
	}
}

func lumaTaps(f int) []int32   { return lumaFilter[f][:] }
func chromaTaps(f int) []int32 { return chromaFilter[f][:] }

// motionCompensate predicts the w x h PU at (x0, y0) into the picture.
func (s *sliceDec) motionCompensate(x0, y0, w, h int, m *motion) {
	hd := s.h
	pic := s.pic
	sp := s.sps
	var pred [2][]int32
	var predC [2][2][]int32
	for l := 0; l < 2; l++ {
		if !m.pred[l] {
			continue
		}
		ref := hd.refList[l][m.ref[l]].pic
		mv := m.mv[l]
		pred[l] = make([]int32, w*h)
		interp(pred[l], ref.y, ref.ys, sp.Width, sp.Height, x0+mv[0]>>2, y0+mv[1]>>2, mv[0]&3, mv[1]&3, w, h, lumaTaps, 8)
		cw, ch := w/2, h/2
		for c := 0; c < 2; c++ {
			plane := ref.cb
			if c == 1 {
				plane = ref.cr
			}
			predC[l][c] = make([]int32, cw*ch)
			interp(predC[l][c], plane, ref.cs, sp.Width/2, sp.Height/2, x0/2+mv[0]>>3, y0/2+mv[1]>>3, mv[0]&7, mv[1]&7, cw, ch, chromaTaps, 4)
		}
	}
	weighted := (hd.typ == sliceP && s.pps.WeightedPred) || (hd.typ == sliceB && s.pps.WeightedBipred)
	write := func(dst []byte, ds int, p0, p1 []int32, w, h int, comp int) {
		bi := p0 != nil && p1 != nil
		for y := 0; y < h; y++ {
			row := dst[y*ds : y*ds+w]
			for x := range row {
				i := y*w + x
				var v int
				switch {
				case weighted:
					v = s.weightedSample(p0, p1, i, m, comp)
				case bi:
					v = int((p0[i] + p1[i] + 64) >> 7)
				case p0 != nil:
					v = int((p0[i] + 32) >> 6)
				default:
					v = int((p1[i] + 32) >> 6)
				}
				row[x] = clipU8(v)
			}
		}
	}
	write(pic.y[y0*pic.ys+x0:], pic.ys, pred[0], pred[1], w, h, 0)
	for c := 0; c < 2; c++ {
		plane := pic.cb
		if c == 1 {
			plane = pic.cr
		}
		write(plane[(y0/2)*pic.cs+x0/2:], pic.cs, predC[0][c], predC[1][c], w/2, h/2, c+1)
	}
}

// weightedSample applies explicit weighted prediction (8.5.3.3.4.3).
func (s *sliceDec) weightedSample(p0, p1 []int32, i int, m *motion, comp int) int {
	pw := &s.h.pw
	var wt [2]int
	var of [2]int
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
	log2WD := denom + 6
	switch {
	case p0 != nil && p1 != nil:
		return (int(p0[i])*wt[0] + int(p1[i])*wt[1] + (of[0]+of[1]+1)<<uint(log2WD)) >> uint(log2WD+1)
	case p0 != nil:
		return ((int(p0[i])*wt[0] + 1<<uint(log2WD-1)) >> uint(log2WD)) + of[0]
	default:
		return ((int(p1[i])*wt[1] + 1<<uint(log2WD-1)) >> uint(log2WD)) + of[1]
	}
}
