package h264

// Syntax element readers for both entropy coders (CAVLC: spec 7.3/9.1-9.2,
// CABAC: spec 9.3).

func (s *sliceCtx) readSkipFlag() bool {
	base := 11
	if s.h.isB() {
		base = 24
	}
	inc := 0
	if s.mbA != nil && s.mbA.typ&mtSkip == 0 {
		inc++
	}
	if s.mbB != nil && s.mbB.typ&mtSkip == 0 {
		inc++
	}
	return s.cb.decision(base+inc) == 1
}

func (s *sliceCtx) readMbType() (int, error) {
	if !s.cabacMode {
		v := int(s.br.ue())
		return v, s.br.err()
	}
	c := &s.cb
	switch {
	case s.h.isI():
		inc := 0
		if s.mbA != nil && s.mbA.typ&(mtIntra16x16|mtIPCM) != 0 {
			inc++
		}
		if s.mbB != nil && s.mbB.typ&(mtIntra16x16|mtIPCM) != 0 {
			inc++
		}
		return s.cabacIntraType(3+inc, 3, true), nil
	case s.h.isP():
		if c.decision(14) == 1 {
			return 5 + s.cabacIntraType(17, 17, false), nil
		}
		if c.decision(15) == 0 {
			return 3 * c.decision(16), nil
		}
		return 2 - c.decision(17), nil
	default:
		inc := 0
		if s.mbA != nil && s.mbA.typ&mtDirect == 0 {
			inc++
		}
		if s.mbB != nil && s.mbB.typ&mtDirect == 0 {
			inc++
		}
		if c.decision(27+inc) == 0 {
			return 0, nil
		}
		if c.decision(27+3) == 0 {
			return 1 + c.decision(27+5), nil
		}
		b := c.decision(27+4) << 3
		b |= c.decision(27+5) << 2
		b |= c.decision(27+5) << 1
		b |= c.decision(27 + 5)
		switch {
		case b < 8:
			return b + 3, nil
		case b == 13:
			return 23 + s.cabacIntraType(32, 32, false), nil
		case b == 14:
			return 11, nil
		case b == 15:
			return 22, nil
		}
		b = b<<1 | c.decision(27+5)
		return b - 4, nil
	}
}

// cabacIntraType decodes the I mb_type bins (prefix context ctx0, the rest
// based at base).
func (s *sliceCtx) cabacIntraType(ctx0, base int, intraSlice bool) int {
	c := &s.cb
	if c.decision(ctx0) == 0 {
		return 0
	}
	if c.terminate() == 1 {
		return 25
	}
	is := 0
	if intraSlice {
		is = 1
		base += 2
	}
	t := 1
	t += 12 * c.decision(base+1)
	if c.decision(base+2) == 1 {
		t += 4 + 4*c.decision(base+2+is)
	}
	t += 2 * c.decision(base+3+is)
	t += c.decision(base + 3 + 2*is)
	return t
}

func (s *sliceCtx) readSubMbType() (int, error) {
	if !s.cabacMode {
		v := int(s.br.ue())
		return v, s.br.err()
	}
	c := &s.cb
	if !s.h.isB() {
		if c.decision(21) == 1 {
			return 0, nil
		}
		if c.decision(22) == 0 {
			return 1, nil
		}
		if c.decision(23) == 1 {
			return 2, nil
		}
		return 3, nil
	}
	if c.decision(36) == 0 {
		return 0, nil
	}
	if c.decision(37) == 0 {
		return 1 + c.decision(39), nil
	}
	t := 3
	if c.decision(38) == 1 {
		if c.decision(39) == 1 {
			return 11 + c.decision(39), nil
		}
		t += 4
	}
	t += 2 * c.decision(39)
	t += c.decision(39)
	return t, nil
}

func (s *sliceCtx) readRefIdx(l, x, y int) (int, error) {
	if !s.cabacMode {
		v := s.br.te(s.h.numRefIdx[l] - 1)
		return v, s.br.err()
	}
	a, b := cidx(x-1, y), cidx(x, y-1)
	ctx := 0
	if s.refCache[l][a] > 0 && !s.directCache[a] {
		ctx++
	}
	if s.refCache[l][b] > 0 && !s.directCache[b] {
		ctx += 2
	}
	ref := 0
	for s.cb.decision(54+ctx) == 1 {
		ref++
		ctx = (ctx >> 2) + 4
		if ref >= 32 {
			return 0, errMB
		}
	}
	return ref, nil
}

func (s *sliceCtx) readMvd(l, x, y int) (int, int, error) {
	if !s.cabacMode {
		dx := int(s.br.se())
		dy := int(s.br.se())
		return dx, dy, s.br.err()
	}
	a, b := cidx(x-1, y), cidx(x, y-1)
	ma, mb := s.mvdCache[l][a], s.mvdCache[l][b]
	dx, err := s.cabacMvd(40, int(ma[0])+int(mb[0]))
	if err != nil {
		return 0, 0, err
	}
	dy, err := s.cabacMvd(47, int(ma[1])+int(mb[1]))
	return dx, dy, err
}

func (s *sliceCtx) cabacMvd(base, amvd int) (int, error) {
	c := &s.cb
	inc := 0
	if amvd > 32 {
		inc = 2
	} else if amvd >= 3 {
		inc = 1
	}
	if c.decision(base+inc) == 0 {
		return 0, nil
	}
	v := 1
	ctx := base + 3
	for v < 9 && c.decision(ctx) == 1 {
		if v < 4 {
			ctx++
		}
		v++
	}
	if v >= 9 {
		k := 3
		for c.bypass() == 1 {
			v += 1 << uint(k)
			k++
			if k > 24 {
				return 0, errMB
			}
		}
		for k > 0 {
			k--
			v += c.bypass() << uint(k)
		}
	}
	if c.bypass() == 1 {
		return -v, nil
	}
	return v, nil
}

func (s *sliceCtx) readCBP(intraNxN bool) (int, error) {
	if !s.cabacMode {
		v := int(s.br.ue())
		if s.br.err() != nil {
			return 0, s.br.err()
		}
		if v > 47 {
			return 0, errMB
		}
		if intraNxN {
			return int(golombToIntraCBP[v]), nil
		}
		return int(golombToInterCBP[v]), nil
	}
	c := &s.cb
	cbp := 0
	for b8 := 0; b8 < 4; b8++ {
		x8, y8 := b8&1, b8>>1
		inc := 0
		if x8 > 0 {
			if cbp&(1<<uint(b8-1)) == 0 {
				inc++
			}
		} else if s.mbA != nil && (s.mbA.cbp>>uint(b8+1))&1 == 0 {
			inc++
		}
		if y8 > 0 {
			if cbp&(1<<uint(b8-2)) == 0 {
				inc += 2
			}
		} else if s.mbB != nil && (s.mbB.cbp>>uint(b8+2))&1 == 0 {
			inc += 2
		}
		cbp |= c.decision(73+inc) << uint(b8)
	}
	ca, cbb := 0, 0
	if s.mbA != nil {
		ca = int(s.mbA.cbp >> 4)
	}
	if s.mbB != nil {
		cbb = int(s.mbB.cbp >> 4)
	}
	inc := 0
	if ca > 0 {
		inc++
	}
	if cbb > 0 {
		inc += 2
	}
	if c.decision(77+inc) == 0 {
		return cbp, nil
	}
	inc = 4
	if ca == 2 {
		inc++
	}
	if cbb == 2 {
		inc += 2
	}
	return cbp | (1+c.decision(77+inc))<<4, nil
}

func (s *sliceCtx) readT8x8() bool {
	if !s.cabacMode {
		return s.br.u1()
	}
	inc := 0
	if s.mbA != nil && s.mbA.t8x8 {
		inc++
	}
	if s.mbB != nil && s.mbB.t8x8 {
		inc++
	}
	return s.cb.decision(399+inc) == 1
}

func (s *sliceCtx) readQPDelta() error {
	var dq int
	if !s.cabacMode {
		dq = int(s.br.se())
		if s.br.err() != nil {
			return s.br.err()
		}
	} else {
		c := &s.cb
		ctx := 0
		if s.lastDQuant != 0 {
			ctx = 1
		}
		if c.decision(60+ctx) == 1 {
			v := 1
			ctx = 2
			for c.decision(60+ctx) == 1 {
				ctx = 3
				v++
				if v > 104 {
					return errMB
				}
			}
			if v&1 == 1 {
				dq = (v + 1) >> 1
			} else {
				dq = -((v + 1) >> 1)
			}
		}
	}
	if dq < -26 || dq > 25 {
		return errMB
	}
	s.lastDQuant = dq
	s.qp = (s.qp + dq + 52) % 52
	return nil
}

func (s *sliceCtx) readIntraMode(pred int) (int, error) {
	var prev bool
	var rem int
	if !s.cabacMode {
		prev = s.br.u1()
		if !prev {
			rem = int(s.br.u(3))
		}
	} else {
		c := &s.cb
		prev = c.decision(68) == 1
		if !prev {
			rem = c.decision(69)
			rem |= c.decision(69) << 1
			rem |= c.decision(69) << 2
		}
	}
	if prev {
		return pred, nil
	}
	if rem < pred {
		return rem, nil
	}
	return rem + 1, nil
}

func (s *sliceCtx) readChromaMode() (int, error) {
	if !s.cabacMode {
		v := int(s.br.ue())
		return v, s.br.err()
	}
	c := &s.cb
	inc := 0
	if s.mbA != nil && s.mbA.chromaMode != 0 {
		inc++
	}
	if s.mbB != nil && s.mbB.chromaMode != 0 {
		inc++
	}
	if c.decision(64+inc) == 0 {
		return 0, nil
	}
	if c.decision(67) == 0 {
		return 1, nil
	}
	if c.decision(67) == 0 {
		return 2, nil
	}
	return 3, nil
}

// readBlock reads one residual block. (x, y) locate the block: a luma 4x4
// position for luma categories, (component, -) for chroma DC and
// (component, blkIdx) for chroma AC.
func (s *sliceCtx) readBlock(cat, x, y, maxNum int, coeff []int32) (int, error) {
	if !s.cabacMode {
		var nC int
		switch cat {
		case catChromaDC:
			nC = -1
		case catChromaAC:
			nC = s.chromaNC(x, y)
		default:
			nC = s.lumaNC(x, y)
		}
		return s.br.residualCAVLC(nC, maxNum, coeff)
	}
	if cat != catLuma8x8 {
		if s.cb.decision(85+cbfCatOffset[cat]+s.cbfInc(cat, x, y)) == 0 {
			return 0, nil
		}
	}
	n := s.cb.residual(cat, maxNum, coeff)
	if s.cb.overrun() {
		return 0, errMB
	}
	return n, nil
}

func (s *sliceCtx) lumaNC(x, y int) int {
	mb := s.mb
	nA, nB := -1, -1
	if x > 0 {
		nA = int(mb.nz[y*4+x-1])
	} else if s.mbA != nil {
		nA = int(s.mbA.nz[y*4+3])
	}
	if y > 0 {
		nB = int(mb.nz[(y-1)*4+x])
	} else if s.mbB != nil {
		nB = int(s.mbB.nz[12+x])
	}
	return combineNC(nA, nB)
}

func (s *sliceCtx) chromaNC(c, blk int) int {
	mb := s.mb
	x, y := blk&1, blk>>1
	nA, nB := -1, -1
	if x > 0 {
		nA = int(mb.nzC[c][blk-1])
	} else if s.mbA != nil {
		nA = int(s.mbA.nzC[c][y*2+1])
	}
	if y > 0 {
		nB = int(mb.nzC[c][blk-2])
	} else if s.mbB != nil {
		nB = int(s.mbB.nzC[c][2+x])
	}
	return combineNC(nA, nB)
}

func combineNC(a, b int) int {
	switch {
	case a >= 0 && b >= 0:
		return (a + b + 1) >> 1
	case a >= 0:
		return a
	case b >= 0:
		return b
	}
	return 0
}

// cbfInc derives ctxIdxInc for coded_block_flag (spec 9.3.3.1.1.9).
func (s *sliceCtx) cbfInc(cat, x, y int) int {
	mb := s.mb
	intra := s.typ&mtIntra != 0
	cond := func(n *mbState, bit uint) int {
		if n == nil {
			if intra {
				return 1
			}
			return 0
		}
		return int(n.cbf>>bit) & 1
	}
	var a, b int
	switch cat {
	case catLumaDC:
		a, b = cond(s.mbA, 24), cond(s.mbB, 24)
	case catLumaAC, catLuma4x4:
		if x > 0 {
			a = int(mb.cbf>>uint(y*4+x-1)) & 1
		} else {
			a = cond(s.mbA, uint(y*4+3))
		}
		if y > 0 {
			b = int(mb.cbf>>uint((y-1)*4+x)) & 1
		} else {
			b = cond(s.mbB, uint(12+x))
		}
	case catChromaDC:
		a, b = cond(s.mbA, uint(25+x)), cond(s.mbB, uint(25+x))
	case catChromaAC:
		c, blk := x, y
		bx, by := blk&1, blk>>1
		base := uint(16 + 4*c)
		if bx > 0 {
			a = int(mb.cbf>>(base+uint(blk-1))) & 1
		} else {
			a = cond(s.mbA, base+uint(by*2+1))
		}
		if by > 0 {
			b = int(mb.cbf>>(base+uint(blk-2))) & 1
		} else {
			b = cond(s.mbB, base+uint(2+bx))
		}
	}
	return a + 2*b
}
