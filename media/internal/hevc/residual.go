package hevc

// residual_coding() (7.3.8.11) and the reconstruction of one transform
// block, plus intra reference sample gathering.

var ctxIdxMap4x4 = [16]uint8{0, 1, 4, 5, 2, 3, 4, 5, 6, 6, 8, 8, 7, 7, 8, 8}

// scans[log2n][scanIdx] lists (x, y) for n = 1, 2, 4, 8.
var scans [4][3][][2]uint8

func init() {
	for l := 0; l < 4; l++ {
		n := 1 << l
		scans[l][0] = diagScan(n)
		h := make([][2]uint8, 0, n*n)
		v := make([][2]uint8, 0, n*n)
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				h = append(h, [2]uint8{uint8(x), uint8(y)})
				v = append(v, [2]uint8{uint8(y), uint8(x)})
			}
		}
		scans[l][1] = h
		scans[l][2] = v
	}
}

var chromaQPTab = [14]int{29, 30, 31, 32, 33, 33, 34, 34, 35, 35, 36, 36, 37, 37}

func chromaQP(qpi int) int {
	switch {
	case qpi < 30:
		return qpi
	case qpi > 43:
		return qpi - 6
	}
	return chromaQPTab[qpi-30]
}

func (s *sliceDec) qpFor(cIdx int) int {
	if cIdx == 0 {
		return s.qpY
	}
	off := s.pps.CbQPOffset + s.h.cbQPOffset
	if cIdx == 2 {
		off = s.pps.CrQPOffset + s.h.crQPOffset
	}
	return chromaQP(clip3(0, 57, s.qpY+off))
}

func (s *sliceDec) lastPrefix(base, log2, cIdx int) int {
	var off, shift int
	if cIdx == 0 {
		off, shift = 3*(log2-2)+((log2-1)>>2), (log2+1)>>2
	} else {
		off, shift = 15, log2-2
	}
	max := log2<<1 - 1
	p := 0
	for p < max && s.cb.decision(&s.ctx[base+off+p>>shift]) == 1 {
		p++
	}
	return p
}

func (s *sliceDec) lastSuffix(prefix int) int {
	if prefix <= 3 {
		return prefix
	}
	nb := prefix>>1 - 1
	return (1<<uint(nb))*(2+prefix&1) + s.cb.bypassBits(nb)
}

// residual parses one residual block and adds it to the picture. (x0, y0)
// is the luma location of the block (chroma blocks are at half).
func (s *sliceDec) residual(x0, y0, log2, cIdx int) error {
	n := 1 << log2
	c := s.coeff[:n*n]
	clear(c)
	bypass := s.cuTransquantBypass
	tskip := false
	if s.pps.TransformSkip && !bypass && log2 <= s.pps.Log2MaxTransformSkipSize {
		inc := 0
		if cIdx > 0 {
			inc = 1
		}
		tskip = s.cb.decision(&s.ctx[ctxTransformSkip+inc]) == 1
	}
	lastX := s.lastPrefix(ctxLastX, log2, cIdx)
	lastY := s.lastPrefix(ctxLastY, log2, cIdx)
	lastX = s.lastSuffix(lastX)
	lastY = s.lastSuffix(lastY)
	if lastX >= n || lastY >= n {
		return errCorrupt
	}
	scanIdx := 0
	if s.predMode == modeIntra && (log2 == 2 || (log2 == 3 && cIdx == 0)) {
		m := s.intraChromaMode
		if cIdx == 0 {
			m = int(s.blk(x0, y0).ipm)
		}
		switch {
		case m >= 6 && m <= 14:
			scanIdx = 2
		case m >= 22 && m <= 30:
			scanIdx = 1
		}
	}
	if scanIdx == 2 {
		lastX, lastY = lastY, lastX
	}
	log2SB := log2 - 2
	nSB := 1 << log2SB
	scanSB := scans[log2SB][scanIdx]
	scan4 := scans[2][scanIdx]
	lastSB := nSB*nSB - 1
	lastPos := 16
	for {
		if lastPos == 0 {
			lastPos = 16
			lastSB--
			if lastSB < 0 {
				return errCorrupt
			}
		}
		lastPos--
		sb := scanSB[lastSB]
		xC := int(sb[0])<<2 + int(scan4[lastPos][0])
		yC := int(sb[1])<<2 + int(scan4[lastPos][1])
		if xC == lastX && yC == lastY {
			break
		}
	}
	var csbf [8][8]bool
	greater1Ctx := 1
	chroma := cIdx > 0
	sdh := s.pps.SignDataHiding && !bypass
	for i := lastSB; i >= 0; i-- {
		xS, yS := int(scanSB[i][0]), int(scanSB[i][1])
		inferDC := false
		if i < lastSB && i > 0 {
			cc := 0
			if xS < nSB-1 && csbf[xS+1][yS] {
				cc++
			}
			if yS < nSB-1 && csbf[xS][yS+1] {
				cc++
			}
			inc := min(cc, 1)
			if chroma {
				inc += 2
			}
			csbf[xS][yS] = s.cb.decision(&s.ctx[ctxCodedSubBlock+inc]) == 1
			inferDC = true
		} else {
			csbf[xS][yS] = true
		}
		var sig [16]uint8
		nsig := 0
		start := 15
		if i == lastSB {
			start = lastPos - 1
			sig[0] = uint8(lastPos)
			nsig = 1
		}
		if csbf[xS][yS] {
			prev := 0
			if xS < nSB-1 && csbf[xS+1][yS] {
				prev |= 1
			}
			if yS < nSB-1 && csbf[xS][yS+1] {
				prev |= 2
			}
			for p := start; p >= 0; p-- {
				xC := xS<<2 + int(scan4[p][0])
				yC := yS<<2 + int(scan4[p][1])
				if p > 0 || !inferDC {
					inc := sigCtxInc(log2, cIdx, xC, yC, scanIdx, prev)
					if s.cb.decision(&s.ctx[ctxSig+inc]) == 1 {
						sig[nsig] = uint8(p)
						nsig++
						inferDC = false
					}
				} else {
					sig[nsig] = 0
					nsig++
				}
			}
		}
		if nsig == 0 {
			continue
		}
		ctxSet := 0
		if i != 0 && !chroma {
			ctxSet = 2
		}
		if i != lastSB && greater1Ctx == 0 {
			ctxSet++
		}
		greater1Ctx = 1
		var g1 [8]bool
		firstG1 := -1
		for m := 0; m < nsig && m < 8; m++ {
			inc := ctxSet*4 + greater1Ctx
			if chroma {
				inc += 16
			}
			if s.cb.decision(&s.ctx[ctxGt1+inc]) == 1 {
				g1[m] = true
				greater1Ctx = 0
				if firstG1 < 0 {
					firstG1 = m
				}
			} else if greater1Ctx > 0 && greater1Ctx < 3 {
				greater1Ctx++
			}
		}
		g2 := false
		if firstG1 >= 0 {
			inc := ctxSet
			if chroma {
				inc += 4
			}
			g2 = s.cb.decision(&s.ctx[ctxGt2+inc]) == 1
		}
		hidden := sdh && int(sig[0])-int(sig[nsig-1]) > 3
		nSigns := nsig
		if hidden {
			nSigns--
		}
		signs := s.cb.bypassBits(nSigns) << uint(32-nSigns)
		rice := 0
		sum := 0
		for m := 0; m < nsig; m++ {
			base := 1
			threshold := 1
			if m < 8 {
				threshold = 2
				if g1[m] {
					base++
					if m == firstG1 {
						threshold = 3
						if g2 {
							base++
						}
					}
				}
			}
			level := base
			if base == threshold {
				rem, err := s.remaining(rice)
				if err != nil {
					return err
				}
				level += rem
				if level > 3*(1<<uint(rice)) {
					rice = min(rice+1, 4)
				}
			}
			p := int(sig[m])
			xC := xS<<2 + int(scan4[p][0])
			yC := yS<<2 + int(scan4[p][1])
			v := int32(level)
			sum += level
			if m < nSigns {
				if signs&(1<<31) != 0 {
					v = -v
				}
				signs <<= 1
			} else if sum&1 == 1 {
				v = -v
			}
			c[yC*n+xC] = v
		}
	}
	return s.reconstruct(x0, y0, log2, cIdx, tskip)
}

func sigCtxInc(log2, cIdx, xC, yC, scanIdx, prev int) int {
	var sc int
	switch {
	case log2 == 2:
		sc = int(ctxIdxMap4x4[yC<<2+xC])
	case xC+yC == 0:
		sc = 0
	default:
		xP, yP := xC&3, yC&3
		switch prev {
		case 0:
			switch {
			case xP+yP == 0:
				sc = 2
			case xP+yP < 3:
				sc = 1
			}
		case 1:
			switch yP {
			case 0:
				sc = 2
			case 1:
				sc = 1
			}
		case 2:
			switch xP {
			case 0:
				sc = 2
			case 1:
				sc = 1
			}
		default:
			sc = 2
		}
		if cIdx == 0 {
			if xC>>2+yC>>2 > 0 {
				sc += 3
			}
			if log2 == 3 {
				if scanIdx == 0 {
					sc += 9
				} else {
					sc += 15
				}
			} else {
				sc += 21
			}
		} else {
			if log2 == 3 {
				sc += 9
			} else {
				sc += 12
			}
		}
	}
	if cIdx > 0 {
		return 27 + sc
	}
	return sc
}

// remaining decodes coeff_abs_level_remaining with Rice parameter k.
func (s *sliceDec) remaining(k int) (int, error) {
	prefix := 0
	for prefix < 32 && s.cb.bypass() == 1 {
		prefix++
	}
	if prefix == 32 {
		return 0, errCorrupt
	}
	if prefix <= 3 {
		return prefix<<uint(k) + s.cb.bypassBits(k), nil
	}
	if prefix-3+k > 30 {
		return 0, errCorrupt
	}
	return ((1<<uint(prefix-3))+2)<<uint(k) + s.cb.bypassBits(prefix-3+k), nil
}

// reconstruct dequantises and inverse-transforms s.coeff and adds the
// residual to the picture.
func (s *sliceDec) reconstruct(x0, y0, log2, cIdx int, tskip bool) error {
	n := 1 << log2
	c := s.coeff[:n*n]
	if !s.cuTransquantBypass {
		var m []uint8
		if s.sps.ScalingListEnabled && !(tskip && n > 4) {
			sl := s.sps.ScalingList
			if s.pps.ScalingList != nil {
				sl = s.pps.ScalingList
			}
			id := cIdx
			if s.predMode != modeIntra {
				id += 3
			}
			m = sl.factor[log2-2][id]
		}
		dequant(c, n, s.qpFor(cIdx), m)
		if tskip {
			for i := range c {
				c[i] = (c[i]<<7 + 2048) >> 12
			}
		} else {
			inverseTransform(c, n, cIdx == 0 && n == 4 && s.predMode == modeIntra, s.tmp[:n*n])
		}
	}
	pic := s.pic
	switch cIdx {
	case 0:
		addResidual(pic.y[y0*pic.ys+x0:], pic.ys, c, n)
	case 1:
		addResidual(pic.cb[(y0/2)*pic.cs+x0/2:], pic.cs, c, n)
	default:
		addResidual(pic.cr[(y0/2)*pic.cs+x0/2:], pic.cs, c, n)
	}
	return nil
}

// intraPredict predicts the n x n block of component cIdx at (x, y)
// (component sample units) with mode and writes it into the picture.
func (s *sliceDec) intraPredict(cIdx, x, y, n, mode int) {
	var refs intraRefs
	plane, stride := s.intraRefs(&refs, cIdx, x, y, n)
	predictIntra(plane[y*stride+x:], stride, &refs, n, mode, cIdx == 0, s.sps.StrongIntraSmoothing)
}

// intraRefs gathers and substitutes the reference samples of the n x n
// block of component cIdx at (x, y) (8.4.4.2.2) and returns the plane.
func (s *sliceDec) intraRefs(refs *intraRefs, cIdx, x, y, n int) ([]byte, int) {
	pic := s.pic
	plane, stride := pic.y, pic.ys
	scale := 1
	if cIdx > 0 {
		plane, stride, scale = pic.cb, pic.cs, 2
		if cIdx == 2 {
			plane = pic.cr
		}
	}
	sp := s.sps
	w, h := sp.Width/scale, sp.Height/scale
	constrained := s.pps.ConstrainedIntraPred
	xl, yl := x*scale, y*scale // luma location of the block
	avail := func(xn, yn int) bool {
		// (xn, yn) in component units.
		lx, ly := xn*scale, yn*scale
		if !s.available(xl, yl, lx, ly) {
			return false
		}
		return !constrained || s.blk(lx, ly).predMode == modeIntra
	}
	var ok [129]bool // index: left from bottom (0..2n-1) reversed, then corner, then top
	// Gather in the substitution order: p[-1][2n-1] .. p[-1][-1], then
	// p[0][-1] .. p[2n-1][-1], in units of 4 (or 2 for chroma) samples.
	unit := 4 / scale
	if unit < 1 {
		unit = 1
	}
	total := 4*n + 1
	var vals [129]int
	k := 0
	any := false
	for i := 2*n - 1; i >= 0; i-- {
		yy := y + i
		a := yy < h && x-1 >= 0 && avail(x-1, yy-yy%unit)
		if a {
			vals[k] = int(plane[yy*stride+x-1])
			any = true
		}
		ok[k] = a
		k++
	}
	{
		a := x-1 >= 0 && y-1 >= 0 && avail(x-1, y-1)
		if a {
			vals[k] = int(plane[(y-1)*stride+x-1])
			any = true
		}
		ok[k] = a
		k++
	}
	for i := 0; i < 2*n; i++ {
		xx := x + i
		a := xx < w && y-1 >= 0 && avail(xx-xx%unit, y-1)
		if a {
			vals[k] = int(plane[(y-1)*stride+xx])
			any = true
		}
		ok[k] = a
		k++
	}
	if !any {
		for i := 0; i < total; i++ {
			vals[i] = 128
		}
	} else {
		if !ok[0] {
			for i := 1; i < total; i++ {
				if ok[i] {
					vals[0] = vals[i]
					break
				}
			}
		}
		for i := 1; i < total; i++ {
			if !ok[i] {
				vals[i] = vals[i-1]
			}
		}
	}
	// vals[0..2n-1] = p[-1][2n-1..0], vals[2n] = p[-1][-1], vals[2n+1..] = p[0..][-1].
	refs.left[0] = vals[2*n]
	refs.top[0] = vals[2*n]
	for i := 0; i < 2*n; i++ {
		refs.left[i+1] = vals[2*n-1-i]
		refs.top[i+1] = vals[2*n+1+i]
	}
	return plane, stride
}
