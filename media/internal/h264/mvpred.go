package h264

// Motion vector prediction (spec 8.4.1.3) and direct-mode derivation
// (spec 8.4.1.2), operating on the neighbour caches.

func median3(a, b, c int16) int16 {
	if a > b {
		a, b = b, a
	}
	if b > c {
		b = c
	}
	if a > b {
		return a
	}
	return b
}

// neighboursC returns the cache index of neighbour C for a partition at
// (x, y) of width w (4x4 units), falling back to D when C is unavailable.
func (s *sliceCtx) neighbourC(l, x, y, w int) int {
	c := cidx(x+w, y-1)
	if s.refCache[l][c] == refNA {
		return cidx(x-1, y-1)
	}
	return c
}

// mvPred is the median motion vector predictor for list l.
func (s *sliceCtx) mvPred(l, x, y, w int, ref int8) (int16, int16) {
	a, b, c := cidx(x-1, y), cidx(x, y-1), s.neighbourC(l, x, y, w)
	rc, mc := &s.refCache[l], &s.mvCache[l]
	ra, rb, rcc := rc[a], rc[b], rc[c]
	if rb == refNA && rcc == refNA && ra != refNA {
		return mc[a][0], mc[a][1]
	}
	match := 0
	var m int
	if ra == ref {
		match++
		m = a
	}
	if rb == ref {
		match++
		m = b
	}
	if rcc == ref {
		match++
		m = c
	}
	if match == 1 {
		return mc[m][0], mc[m][1]
	}
	return median3(mc[a][0], mc[b][0], mc[c][0]), median3(mc[a][1], mc[b][1], mc[c][1])
}

// mvPredPart predicts a partition of a 16x8 / 8x16 macroblock with the
// directional rules of spec 8.4.1.3, falling back to the median.
func (s *sliceCtx) mvPredPart(l int, typ uint32, part, x, y, w int, ref int8) (int16, int16) {
	rc, mc := &s.refCache[l], &s.mvCache[l]
	switch {
	case typ&mt16x8 != 0:
		if part == 0 {
			if b := cidx(x, y-1); rc[b] == ref {
				return mc[b][0], mc[b][1]
			}
		} else if a := cidx(x-1, y); rc[a] == ref {
			return mc[a][0], mc[a][1]
		}
	case typ&mt8x16 != 0:
		if part == 0 {
			if a := cidx(x-1, y); rc[a] == ref {
				return mc[a][0], mc[a][1]
			}
		} else if c := s.neighbourC(l, x, y, w); rc[c] == ref {
			return mc[c][0], mc[c][1]
		}
	}
	return s.mvPred(l, x, y, w, ref)
}

// predPSkip derives the P_Skip motion vector (spec 8.4.1.1).
func (s *sliceCtx) predPSkip() (int16, int16) {
	rc, mc := &s.refCache[0], &s.mvCache[0]
	a, b := cidx(-1, 0), cidx(0, -1)
	if rc[a] == refNA || rc[b] == refNA {
		return 0, 0
	}
	if (rc[a] == 0 && mc[a] == [2]int16{}) || (rc[b] == 0 && mc[b] == [2]int16{}) {
		return 0, 0
	}
	return s.mvPred(0, 0, 0, 4, 0)
}

// colocated returns the co-located motion for 4x4 block (bx, by) of the
// current macroblock: mvCol, refIdxCol and the referenced picture id.
func (s *sliceCtx) colocated(bx, by int) (mvx, mvy int16, ref int8, refID int32) {
	col := s.refs[1][0]
	if s.sps.Direct8x8Inference {
		bx = (bx >> 1) * 3
		by = (by >> 1) * 3
	}
	if col.mbType[s.mbAddr]&mtIntra != 0 {
		return 0, 0, -1, -1
	}
	i := (s.mbY*4+by)*s.pc.mbW*4 + s.mbX*4 + bx
	l := 0
	if col.ref[0][i] < 0 {
		l = 1
	}
	return col.mv[l][2*i], col.mv[l][2*i+1], col.ref[l][i], col.refID[l][i]
}

// spatialDirectSetup derives the per-macroblock spatial direct references
// and predictors (spec 8.4.1.2.2).
func (s *sliceCtx) spatialDirectSetup() {
	if s.spatialDone {
		return
	}
	s.spatialDone = true
	s.spZero = false
	for l := 0; l < 2; l++ {
		rc := &s.refCache[l]
		ra := rc[cidx(-1, 0)]
		rb := rc[cidx(0, -1)]
		rcc := rc[s.neighbourC(l, 0, 0, 4)]
		s.spRef[l] = minPositive(ra, minPositive(rb, rcc))
	}
	if s.spRef[0] < 0 && s.spRef[1] < 0 {
		s.spRef = [2]int8{0, 0}
		s.spMv = [2][2]int16{}
		s.spZero = true
		return
	}
	for l := 0; l < 2; l++ {
		if s.spRef[l] >= 0 {
			x, y := s.mvPred(l, 0, 0, 4, s.spRef[l])
			s.spMv[l] = [2]int16{x, y}
		} else {
			s.spRef[l] = -1
			s.spMv[l] = [2]int16{}
		}
	}
}

func minPositive(a, b int8) int8 {
	if a >= 0 && b >= 0 {
		return min(a, b)
	}
	return max(a, b)
}

// predDirect8x8 derives direct-mode motion for 8x8 quadrant q into the
// caches (spec 8.4.1.2).
func (s *sliceCtx) predDirect8x8(q int) {
	x0, y0 := (q&1)*2, (q>>1)*2
	if s.h.directSpatial {
		s.spatialDirectSetup()
		for y := y0; y < y0+2; y++ {
			for x := x0; x < x0+2; x++ {
				var mv [2][2]int16
				mv = s.spMv
				if !s.spZero && s.colZeroOK && (s.spRef[0] == 0 || s.spRef[1] == 0) {
					mx, my, r, _ := s.colocated(x, y)
					if r == 0 && mx >= -1 && mx <= 1 && my >= -1 && my <= 1 {
						for l := 0; l < 2; l++ {
							if s.spRef[l] == 0 {
								mv[l] = [2]int16{}
							}
						}
					}
				}
				for l := 0; l < 2; l++ {
					c := cidx(x, y)
					s.refCache[l][c] = s.spRef[l]
					s.mvCache[l][c] = mv[l]
				}
			}
		}
		return
	}
	// Temporal direct (spec 8.4.1.2.3).
	for y := y0; y < y0+2; y++ {
		for x := x0; x < x0+2; x++ {
			mx, my, r, id := s.colocated(x, y)
			ref0 := 0
			if r >= 0 {
				for i, p := range s.refs[0] {
					if p.id == id {
						ref0 = i
						break
					}
				}
			}
			var mv0, mv1 [2]int16
			if s.dsfLong[ref0] {
				mv0 = [2]int16{mx, my}
			} else {
				f := s.dsf[ref0]
				mv0 = [2]int16{int16((f*int(mx) + 128) >> 8), int16((f*int(my) + 128) >> 8)}
				mv1 = [2]int16{mv0[0] - mx, mv0[1] - my}
			}
			c := cidx(x, y)
			s.refCache[0][c] = int8(ref0)
			s.mvCache[0][c] = mv0
			s.refCache[1][c] = 0
			s.mvCache[1][c] = mv1
		}
	}
}

// setupSliceDirect precomputes the temporal-direct scale factors, implicit
// weights and the col-zero condition for a B slice.
func (s *sliceCtx) setupSliceDirect() {
	cur := s.pic.poc
	l0, l1 := s.refs[0], s.refs[1]
	s.colZeroOK = len(l1) > 0 && l1[0].refState == refShort
	if len(l1) == 0 {
		return
	}
	for i, p0 := range l0 {
		s.dsf[i], s.dsfLong[i] = 256, false
		if p0.refState == refLong {
			s.dsfLong[i] = true
			continue
		}
		tb := clip3(-128, 127, cur-p0.poc)
		td := clip3(-128, 127, l1[0].poc-p0.poc)
		if td == 0 {
			s.dsfLong[i] = true
			continue
		}
		tx := (16384 + absInt(td/2)) / td
		s.dsf[i] = clip3(-1024, 1023, (tb*tx+32)>>6)
	}
	if s.pps.WeightedBipredIDC == 2 {
		for i, p0 := range l0 {
			for j, p1 := range l1 {
				w := 32
				if p0.refState != refLong && p1.refState != refLong {
					tb := clip3(-128, 127, cur-p0.poc)
					td := clip3(-128, 127, p1.poc-p0.poc)
					if td != 0 {
						tx := (16384 + absInt(td/2)) / td
						f := clip3(-1024, 1023, (tb*tx+32)>>6)
						if f>>2 >= -64 && f>>2 <= 128 {
							w = f >> 2
						}
					}
				}
				s.implicitW[i][j] = int16(w)
			}
		}
	}
}
