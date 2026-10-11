package h264

// Macroblock reconstruction: prediction plus dequantised residual.

// intraAvail reports intra-prediction availability of the left (A), top (B),
// top-right (C) and top-left (D) neighbouring macroblocks, honouring
// constrained_intra_pred.
func (s *sliceCtx) intraAvail() (a, b, c, d bool) {
	pc := s.pc
	ok := func(addr int) bool {
		n := &pc.mbs[addr]
		return !s.pps.ConstrainedIntraPred || n.typ&mtIntra != 0
	}
	addr := s.mbAddr
	a = s.mbA != nil && ok(addr-1)
	b = s.mbB != nil && ok(addr-pc.mbW)
	c = s.availC && ok(addr-pc.mbW+1)
	d = s.availD && ok(addr-pc.mbW-1)
	return
}

func (s *sliceCtx) intraRecon() {
	pic := s.pic
	aA, aB, aC, aD := s.intraAvail()
	x0, y0 := s.mbX*16, s.mbY*16
	ys := pic.stride
	Y := pic.y
	mb := s.mb
	switch {
	case s.typ == mtIntra16x16:
		var top, left [16]byte
		var tl byte
		var topS, leftS []byte
		if aB {
			copy(top[:], Y[(y0-1)*ys+x0:])
			topS = top[:]
		}
		if aA {
			for i := 0; i < 16; i++ {
				left[i] = Y[(y0+i)*ys+x0-1]
			}
			leftS = left[:]
		}
		if aD {
			tl = Y[(y0-1)*ys+x0-1]
		}
		pred := predict16x16(s.i16Mode, topS, leftS, tl)
		for y := 0; y < 16; y++ {
			copy(Y[(y0+y)*ys+x0:], pred[y][:])
		}
		// DC: inverse Hadamard + scaling into each block's position 0.
		if mb.cbf&(1<<24) != 0 {
			hadamard4(&s.coefDC)
			q := s.pc.dq.q4[0][s.qp][0]
			for r := 0; r < 16; r++ {
				s.coef[r][0] = (s.coefDC[r]*q + 32) >> 6
			}
			s.coefDC = [16]int32{}
		}
		for r := 0; r < 16; r++ {
			bx, by := r&3, r>>2
			idct4Add(Y[(y0+by*4)*ys+x0+bx*4:], ys, &s.coef[r])
		}
	case s.t8x8:
		for b8 := 0; b8 < 4; b8++ {
			bx, by := (b8&1)*8, (b8>>1)*8
			left := bx > 0 || aA
			top := by > 0 || aB
			var tr, tl bool
			switch b8 {
			case 0:
				tr, tl = aB, aD
			case 1:
				tr, tl = aC, aB
			case 2:
				tr, tl = true, aA
			case 3:
				tr, tl = false, true
			}
			px, py := x0+bx, y0+by
			ref := s.luma8x8Refs(px, py, left, top, tr, tl)
			mode := int(mb.intraMode[(by/4)*4+bx/4])
			pred := predict8x8(mode, ref, left, top)
			for y := 0; y < 8; y++ {
				copy(Y[(py+y)*ys+px:], pred[y][:])
			}
			if s.lumaNZ&(0x33<<uint((by/4)*4+bx/4)) != 0 {
				idct8Add(Y[py*ys+px:], ys, &s.coef8[b8])
			}
		}
	default:
		for blk := 0; blk < 16; blk++ {
			bx, by := int(blkX[blk]), int(blkY[blk])
			left := bx > 0 || aA
			top := by > 0 || aB
			var tl, tr bool
			switch {
			case bx > 0 && by > 0:
				tl = true
			case by > 0:
				tl = aA
			case bx > 0:
				tl = aB
			default:
				tl = aD
			}
			if by == 0 {
				if bx < 3 {
					tr = aB
				} else {
					tr = aC
				}
			} else if bx < 3 {
				tr = rasterToBlk[(by-1)*4+bx+1] < uint8(blk)
			}
			px, py := x0+bx*4, y0+by*4
			var ref [13]byte
			for i := 0; i < 13; i++ {
				ref[i] = 128
			}
			if left {
				for i := 0; i < 4; i++ {
					ref[3-i] = Y[(py+i)*ys+px-1]
				}
			}
			if tl {
				ref[4] = Y[(py-1)*ys+px-1]
			}
			if top {
				copy(ref[5:9], Y[(py-1)*ys+px:])
				if tr {
					copy(ref[9:13], Y[(py-1)*ys+px+4:])
				} else {
					ref[9], ref[10], ref[11], ref[12] = ref[8], ref[8], ref[8], ref[8]
				}
			}
			pred := predict4x4(int(mb.intraMode[by*4+bx]), ref, left, top)
			for y := 0; y < 4; y++ {
				copy(Y[(py+y)*ys+px:px+4+(py+y)*ys], pred[y][:])
			}
			if s.lumaNZ&(1<<uint(by*4+bx)) != 0 {
				idct4Add(Y[py*ys+px:], ys, &s.coef[by*4+bx])
			}
		}
	}
	s.chromaRecon(aA, aB, aD)
}

// luma8x8Refs gathers and filters the reference samples for 8x8 intra
// prediction (spec 8.3.2.2.1). Layout: [L7..L0, TL, T0..T15].
func (s *sliceCtx) luma8x8Refs(px, py int, left, top, tr, tl bool) [25]byte {
	Y, ys := s.pic.y, s.pic.stride
	var p [25]int
	if left {
		for i := 0; i < 8; i++ {
			p[i] = int(Y[(py+7-i)*ys+px-1])
		}
	}
	if tl {
		p[8] = int(Y[(py-1)*ys+px-1])
	}
	if top {
		for i := 0; i < 8; i++ {
			p[9+i] = int(Y[(py-1)*ys+px+i])
		}
		for i := 0; i < 8; i++ {
			if tr {
				p[17+i] = int(Y[(py-1)*ys+px+8+i])
			} else {
				p[17+i] = p[16]
			}
		}
	}
	var ref [25]byte
	if top {
		if tl {
			ref[9] = byte((p[8] + 2*p[9] + p[10] + 2) >> 2)
		} else {
			ref[9] = byte((3*p[9] + p[10] + 2) >> 2)
		}
		for i := 10; i < 24; i++ {
			ref[i] = byte((p[i-1] + 2*p[i] + p[i+1] + 2) >> 2)
		}
		ref[24] = byte((p[23] + 3*p[24] + 2) >> 2)
	}
	if tl {
		switch {
		case top && left:
			ref[8] = byte((p[9] + 2*p[8] + p[7] + 2) >> 2)
		case top:
			ref[8] = byte((3*p[8] + p[9] + 2) >> 2)
		case left:
			ref[8] = byte((3*p[8] + p[7] + 2) >> 2)
		default:
			ref[8] = byte(p[8])
		}
	}
	if left {
		if tl {
			ref[7] = byte((p[8] + 2*p[7] + p[6] + 2) >> 2)
		} else {
			ref[7] = byte((3*p[7] + p[6] + 2) >> 2)
		}
		for i := 1; i < 7; i++ {
			ref[i] = byte((p[i+1] + 2*p[i] + p[i-1] + 2) >> 2)
		}
		ref[0] = byte((p[1] + 3*p[0] + 2) >> 2)
	}
	return ref
}

func (s *sliceCtx) chromaRecon(aA, aB, aD bool) {
	pic := s.pic
	cs := pic.cstride
	x0, y0 := s.mbX*8, s.mbY*8
	for c := 0; c < 2; c++ {
		P := pic.cb
		if c == 1 {
			P = pic.cr
		}
		var top, left [8]byte
		var tl byte
		var topS, leftS []byte
		if aB {
			copy(top[:], P[(y0-1)*cs+x0:])
			topS = top[:]
		}
		if aA {
			for i := 0; i < 8; i++ {
				left[i] = P[(y0+i)*cs+x0-1]
			}
			leftS = left[:]
		}
		if aD {
			tl = P[(y0-1)*cs+x0-1]
		}
		pred := predictChroma(s.chromaMode, topS, leftS, tl)
		for y := 0; y < 8; y++ {
			copy(P[(y0+y)*cs+x0:], pred[y][:])
		}
	}
	s.addChromaResidual(true)
}

// addChromaResidual applies the chroma DC/AC residual of the current
// macroblock.
func (s *sliceCtx) addChromaResidual(intra bool) {
	if s.chromaNZ == 0 {
		return
	}
	pic := s.pic
	cs := pic.cstride
	x0, y0 := s.mbX*8, s.mbY*8
	for c := 0; c < 2; c++ {
		if s.chromaNZ>>uint(4*c)&0xf == 0 {
			continue
		}
		P := pic.cb
		if c == 1 {
			P = pic.cr
		}
		m := 4 + c
		if intra {
			m = 1 + c
		}
		dc := &s.chromaDC[c]
		c0, c1, c2, c3 := dc[0], dc[1], dc[2], dc[3]
		f := [4]int32{c0 + c1 + c2 + c3, c0 - c1 + c2 - c3, c0 + c1 - c2 - c3, c0 - c1 - c2 + c3}
		q := s.pc.dq.q4[m][s.mb.qpc[c]][0]
		for blk := 0; blk < 4; blk++ {
			b := &s.chromaAC[c][blk]
			b[0] = (f[blk] * q) >> 5
			if s.chromaNZ&(1<<uint(4*c+blk)) != 0 || b[0] != 0 {
				idct4Add(P[(y0+(blk>>1)*4)*cs+x0+(blk&1)*4:], cs, b)
			}
		}
		*dc = [4]int32{}
	}
}

// addResidualInter adds the residual of an inter macroblock.
func (s *sliceCtx) addResidualInter() {
	pic := s.pic
	ys := pic.stride
	x0, y0 := s.mbX*16, s.mbY*16
	if s.lumaNZ != 0 {
		if s.t8x8 {
			for b8 := 0; b8 < 4; b8++ {
				bx, by := (b8&1)*8, (b8>>1)*8
				if s.lumaNZ&(0x33<<uint((by/4)*4+bx/4)) != 0 {
					idct8Add(pic.y[(y0+by)*ys+x0+bx:], ys, &s.coef8[b8])
				}
			}
		} else {
			for r := 0; r < 16; r++ {
				if s.lumaNZ&(1<<uint(r)) != 0 {
					idct4Add(pic.y[(y0+(r>>2)*4)*ys+x0+(r&3)*4:], ys, &s.coef[r])
				}
			}
		}
	}
	s.addChromaResidual(false)
}

// interPred performs motion-compensated prediction of the macroblock from
// the motion in the caches.
func (s *sliceCtx) interPred() {
	if s.uniform(0, 0, 4, 4) {
		s.predPart(0, 0, 4, 4)
		return
	}
	switch {
	case s.typ&mt16x8 != 0:
		s.predPart(0, 0, 4, 2)
		s.predPart(0, 2, 4, 2)
		return
	case s.typ&mt8x16 != 0:
		s.predPart(0, 0, 2, 4)
		s.predPart(2, 0, 2, 4)
		return
	}
	for q := 0; q < 4; q++ {
		x, y := (q&1)*2, (q>>1)*2
		switch {
		case s.uniform(x, y, 2, 2):
			s.predPart(x, y, 2, 2)
		case s.uniform(x, y, 2, 1) && s.uniform(x, y+1, 2, 1):
			s.predPart(x, y, 2, 1)
			s.predPart(x, y+1, 2, 1)
		case s.uniform(x, y, 1, 2) && s.uniform(x+1, y, 1, 2):
			s.predPart(x, y, 1, 2)
			s.predPart(x+1, y, 1, 2)
		default:
			s.predPart(x, y, 1, 1)
			s.predPart(x+1, y, 1, 1)
			s.predPart(x, y+1, 1, 1)
			s.predPart(x+1, y+1, 1, 1)
		}
	}
}

// uniform reports whether every 4x4 block of the region has identical
// motion.
func (s *sliceCtx) uniform(x, y, w, h int) bool {
	c0 := cidx(x, y)
	for l := 0; l < 2; l++ {
		r, mv := s.refCache[l][c0], s.mvCache[l][c0]
		for j := y; j < y+h; j++ {
			for i := x; i < x+w; i++ {
				c := cidx(i, j)
				if s.refCache[l][c] != r || (r >= 0 && s.mvCache[l][c] != mv) {
					return false
				}
			}
		}
	}
	return true
}

// predPart predicts a (w x h 4x4 units) region with uniform motion.
func (s *sliceCtx) predPart(x, y, w, h int) {
	c := cidx(x, y)
	r0, r1 := s.refCache[0][c], s.refCache[1][c]
	pic := s.pic
	lw, lh := w*4, h*4
	cw, ch := w*2, h*2
	lx, ly := s.mbX*16+x*4, s.mbY*16+y*4
	cx, cy := s.mbX*8+x*2, s.mbY*8+y*2
	pw, ph := pic.mbW*16, pic.mbH*16
	// One list without explicit weights: predict straight into the
	// picture, skipping the scratch copy.
	if (r0 < 0) != (r1 < 0) && !s.h.hasWeights {
		l, r := 0, r0
		if r0 < 0 {
			l, r = 1, r1
		}
		ref := s.refs[l][r]
		mv := s.mvCache[l][c]
		mx, my := int(mv[0]), int(mv[1])
		ref.waitRef(ly+(my>>2)+lh+2, cy+(my>>3)+ch)
		ys, cs := pic.stride, pic.cstride
		s.mcLuma(pic.y[ly*ys+lx:], ys, ref.y, ref.stride, pw, ph, lx+(mx>>2), ly+(my>>2), mx&3, my&3, lw, lh)
		s.mcChroma(pic.cb[cy*cs+cx:], cs, ref.cb, ref.cstride, pw/2, ph/2, cx+(mx>>3), cy+(my>>3), mx&7, my&7, cw, ch)
		s.mcChroma(pic.cr[cy*cs+cx:], cs, ref.cr, ref.cstride, pw/2, ph/2, cx+(mx>>3), cy+(my>>3), mx&7, my&7, cw, ch)
		return
	}
	n := 0
	var used [2]int
	for l := 0; l < 2; l++ {
		r := s.refCache[l][c]
		if r < 0 {
			continue
		}
		used[n] = l
		n++
		ref := s.refs[l][r]
		mv := s.mvCache[l][c]
		mx, my := int(mv[0]), int(mv[1])
		ref.waitRef(ly+(my>>2)+lh+2, cy+(my>>3)+ch)
		s.mcLuma(s.predTmp[l][:], 16, ref.y, ref.stride, pw, ph, lx+(mx>>2), ly+(my>>2), mx&3, my&3, lw, lh)
		s.mcChroma(s.cpredTmp[l][0][:], 8, ref.cb, ref.cstride, pw/2, ph/2, cx+(mx>>3), cy+(my>>3), mx&7, my&7, cw, ch)
		s.mcChroma(s.cpredTmp[l][1][:], 8, ref.cr, ref.cstride, pw/2, ph/2, cx+(mx>>3), cy+(my>>3), mx&7, my&7, cw, ch)
	}
	if n == 0 {
		return // corrupt: leave whatever is there
	}
	h0 := s.h
	ys, cs := pic.stride, pic.cstride
	type plane struct {
		dst        []byte
		ds, w, hgt int
		p          [2][]byte
		ps         int
	}
	planes := [3]plane{
		{pic.y[ly*ys+lx:], ys, lw, lh, [2][]byte{s.predTmp[0][:], s.predTmp[1][:]}, 16},
		{pic.cb[cy*cs+cx:], cs, cw, ch, [2][]byte{s.cpredTmp[0][0][:], s.cpredTmp[1][0][:]}, 8},
		{pic.cr[cy*cs+cx:], cs, cw, ch, [2][]byte{s.cpredTmp[0][1][:], s.cpredTmp[1][1][:]}, 8},
	}
	if n == 1 {
		l := used[0]
		r := int(s.refCache[l][c])
		explicit := h0.hasWeights
		for pi := range planes {
			pl := &planes[pi]
			src := pl.p[l]
			if !explicit {
				for j := 0; j < pl.hgt; j++ {
					copy(pl.dst[j*pl.ds:j*pl.ds+pl.w], src[j*pl.ps:j*pl.ps+pl.w])
				}
				continue
			}
			var wgt, off, logWD int
			if pi == 0 {
				wgt, off, logWD = h0.lumaWeight[l][r], h0.lumaOffset[l][r], h0.lumaLog2WD
			} else {
				wgt, off, logWD = h0.chromaWeight[l][r][pi-1], h0.chromaOffset[l][r][pi-1], h0.chromaLog2WD
			}
			round := 0
			if logWD >= 1 {
				round = 1 << uint(logWD-1)
			}
			for j := 0; j < pl.hgt; j++ {
				d := pl.dst[j*pl.ds : j*pl.ds+pl.w]
				sr := src[j*pl.ps:]
				for i := range d {
					d[i] = clip255(int32(((int(sr[i])*wgt + round) >> uint(logWD)) + off))
				}
			}
		}
		return
	}
	// Bi-prediction.
	mode := 0 // default
	if s.pps.WeightedBipredIDC == 1 {
		mode = 1
	} else if s.pps.WeightedBipredIDC == 2 {
		mode = 2
	}
	for pi := range planes {
		pl := &planes[pi]
		a, b := pl.p[0], pl.p[1]
		if mode == 0 {
			avg2(pl.dst, pl.ds, a, pl.ps, b, pl.ps, pl.w, pl.hgt)
			continue
		}
		var w0, w1, o, logWD int
		if mode == 2 {
			w1 = int(s.implicitW[r0][r1])
			w0 = 64 - w1
			logWD = 5
		} else if pi == 0 {
			w0, w1 = h0.lumaWeight[0][r0], h0.lumaWeight[1][r1]
			o = (h0.lumaOffset[0][r0] + h0.lumaOffset[1][r1] + 1) >> 1
			logWD = h0.lumaLog2WD
		} else {
			k := pi - 1
			w0, w1 = h0.chromaWeight[0][r0][k], h0.chromaWeight[1][r1][k]
			o = (h0.chromaOffset[0][r0][k] + h0.chromaOffset[1][r1][k] + 1) >> 1
			logWD = h0.chromaLog2WD
		}
		round := 1 << uint(logWD)
		for j := 0; j < pl.hgt; j++ {
			d := pl.dst[j*pl.ds : j*pl.ds+pl.w]
			sa, sb := a[j*pl.ps:], b[j*pl.ps:]
			for i := range d {
				d[i] = clip255(int32(((int(sa[i])*w0 + int(sb[i])*w1 + round) >> uint(logWD+1)) + o))
			}
		}
	}
}
