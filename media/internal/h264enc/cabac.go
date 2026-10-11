package h264enc

import "github.com/qizhanchan/qui/media/internal/h264"

// CABAC encoding (spec 9.3.4). Context selection mirrors the decoder's
// syntax readers (h264/syntax.go) element for element.

var (
	lpsTab, mpsNext, lpsNext = h264.CABACTransitions()
	resCtx                   = h264.CABACResidual()
	initStates               [2][4][52][1024]uint8 // [intra][initIdc][qp]
	initDone                 [2][4][52]bool
)

func cabacInit(intra bool, idc, qp int) *[1024]uint8 {
	i := 0
	if intra {
		i, idc = 1, 0
	}
	if !initDone[i][idc][qp] {
		initStates[i][idc][qp] = h264.CABACInitStates(intra, idc, qp)
		initDone[i][idc][qp] = true
	}
	return &initStates[i][idc][qp]
}

// cabacEnc is the arithmetic encoder of one slice.
type cabacEnc struct {
	w           *bitWriter
	low, rng    uint32
	outstanding int
	first       bool
	states      [1024]uint8
}

func (c *cabacEnc) init(w *bitWriter, typ, initIdc, qp int) {
	c.w = w
	c.low, c.rng = 0, 510
	c.outstanding = 0
	c.first = true
	c.states = *cabacInit(typ == sliceI, initIdc, qp)
}

func (c *cabacEnc) putBit(b uint32) {
	if c.first {
		c.first = false
	} else {
		c.w.u(1, b)
	}
	for ; c.outstanding > 0; c.outstanding-- {
		c.w.u(1, 1-b)
	}
}

func (c *cabacEnc) renorm() {
	for c.rng < 256 {
		switch {
		case c.low < 256:
			c.putBit(0)
		case c.low >= 512:
			c.low -= 512
			c.putBit(1)
		default:
			c.low -= 256
			c.outstanding++
		}
		c.rng <<= 1
		c.low <<= 1
	}
}

// encode codes one context-coded bin.
func (c *cabacEnc) encode(ctx int, bin int) {
	st := &c.states[ctx]
	s := *st
	lps := uint32(lpsTab[s][(c.rng>>6)&3])
	c.rng -= lps
	if uint8(bin) != s&1 {
		c.low += c.rng
		c.rng = lps
		*st = lpsNext[s]
	} else {
		*st = mpsNext[s]
	}
	c.renorm()
}

func (c *cabacEnc) bypass(bin int) {
	c.low <<= 1
	if bin != 0 {
		c.low += c.rng
	}
	switch {
	case c.low >= 1024:
		c.putBit(1)
		c.low -= 1024
	case c.low < 512:
		c.putBit(0)
	default:
		c.low -= 512
		c.outstanding++
	}
}

// encodeTerminate codes end_of_slice_flag (or the I_PCM / mb_type
// terminate bin).
func (c *cabacEnc) encodeTerminate(last bool) {
	c.rng -= 2
	if last {
		c.low += c.rng
		c.rng = 2
		c.renorm()
		c.putBit((c.low >> 9) & 1)
		c.w.u(2, ((c.low>>7)&3)|1)
		return
	}
	c.renorm()
}

// finish completes the slice data: the flush already wrote the stop bit,
// so only alignment zeros remain.
func (c *cabacEnc) finish() { c.w.align(0) }

// Neighbour helpers.

func (s *sliceEnc) nbA() *mbInfo {
	if !s.availA {
		return nil
	}
	return s.left()
}

func (s *sliceEnc) nbB() *mbInfo {
	if !s.availB {
		return nil
	}
	return s.top()
}

// cabacSkip codes mb_skip_flag.
func (s *sliceEnc) cabacSkip(skip bool) {
	base := 11
	if s.typ == sliceB {
		base = 24
	}
	inc := 0
	if a := s.nbA(); a != nil && !a.skip {
		inc++
	}
	if b := s.nbB(); b != nil && !b.skip {
		inc++
	}
	bin := 0
	if skip {
		bin = 1
		s.prevQPNZ = false
	}
	s.cb.encode(base+inc, bin)
}

// cabacIntraType codes the intra mb_type bins (spec 9.3.2.5).
func (s *sliceEnc) cabacIntraType(ctx0, base int, intraSlice bool) {
	c := &s.cb
	if !s.i16 {
		c.encode(ctx0, 0)
		return
	}
	c.encode(ctx0, 1)
	c.encodeTerminate(false)
	is := 0
	if intraSlice {
		is = 1
		base += 2
	}
	c.encode(base+1, b2i(s.cbpL != 0))
	c.encode(base+2, b2i(s.cbpC != 0))
	if s.cbpC != 0 {
		c.encode(base+2+is, b2i(s.cbpC == 2))
	}
	c.encode(base+3+is, s.i16Mode>>1)
	c.encode(base+3+2*is, s.i16Mode&1)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *sliceEnc) cabacMbType() {
	c := &s.cb
	switch s.typ {
	case sliceI:
		inc := 0
		if a := s.nbA(); a != nil && a.typ&h264.MBIntra16x16 != 0 {
			inc++
		}
		if b := s.nbB(); b != nil && b.typ&h264.MBIntra16x16 != 0 {
			inc++
		}
		s.cabacIntraType(3+inc, 3, true)
	case sliceP:
		if s.intra {
			c.encode(14, 1)
			s.cabacIntraType(17, 17, false)
			return
		}
		c.encode(14, 0)
		switch s.part {
		case 0:
			c.encode(15, 0)
			c.encode(16, 0)
		case 3:
			c.encode(15, 0)
			c.encode(16, 1)
		case 1:
			c.encode(15, 1)
			c.encode(17, 1)
		case 2:
			c.encode(15, 1)
			c.encode(17, 0)
		}
	default:
		inc := 0
		if a := s.nbA(); a != nil && !a.direct {
			inc++
		}
		if b := s.nbB(); b != nil && !b.direct {
			inc++
		}
		t := s.bType
		if s.intra {
			t = -1
		}
		switch {
		case t == 0:
			c.encode(27+inc, 0)
		case t == 1 || t == 2:
			c.encode(27+inc, 1)
			c.encode(27+3, 0)
			c.encode(27+5, t-1)
		default:
			c.encode(27+inc, 1)
			c.encode(27+3, 1)
			var b int
			switch {
			case t < 0:
				b = 13
			case t == 11:
				b = 14
			case t == 22:
				b = 15
			case t <= 10:
				b = t - 3
			default:
				b = t + 4 // 6-bit codes 12..21 → 16..25 (b>>1 in 8..12)
			}
			if t < 12 || t > 21 {
				c.encode(27+4, b>>3&1)
				c.encode(27+5, b>>2&1)
				c.encode(27+5, b>>1&1)
				c.encode(27+5, b&1)
			} else {
				c.encode(27+4, b>>4&1)
				c.encode(27+5, b>>3&1)
				c.encode(27+5, b>>2&1)
				c.encode(27+5, b>>1&1)
				c.encode(27+5, b&1)
			}
			if t < 0 {
				s.cabacIntraType(32, 32, false)
			}
		}
	}
}

// cabacSubMbType codes sub_mb_type.
func (s *sliceEnc) cabacSubMbType(t int) {
	c := &s.cb
	if s.typ != sliceB {
		switch t {
		case 0:
			c.encode(21, 1)
		case 1:
			c.encode(21, 0)
			c.encode(22, 0)
		case 2:
			c.encode(21, 0)
			c.encode(22, 1)
			c.encode(23, 1)
		default:
			c.encode(21, 0)
			c.encode(22, 1)
			c.encode(23, 0)
		}
		return
	}
	switch {
	case t == 0:
		c.encode(36, 0)
	case t <= 2:
		c.encode(36, 1)
		c.encode(37, 0)
		c.encode(39, t-1)
	case t >= 11:
		c.encode(36, 1)
		c.encode(37, 1)
		c.encode(38, 1)
		c.encode(39, 1)
		c.encode(39, t-11)
	default:
		c.encode(36, 1)
		c.encode(37, 1)
		v := t - 3
		c.encode(38, v>>2&1)
		c.encode(39, v>>1&1)
		c.encode(39, v&1)
	}
}

// nbRefCtx reports the ref_idx context condition of the 4x4 neighbour at
// (xr, yr): a list-l reference index above 0 not predicted in direct mode.
// Blocks inside the current MB come from the partitions already coded.
func (s *sliceEnc) nbRefCtx(l, xr, yr int) bool {
	var ref int8
	var direct bool
	switch {
	case xr >= 0 && yr >= 0:
		ref = s.curRef[l][yr*4+xr]
		direct = s.curDirect&(1<<uint((yr>>1)*2+xr>>1)) != 0
	case xr < 0 && yr >= 0:
		if !s.availA {
			return false
		}
		n := s.left()
		ref = s.e.ref[l][s.idx4(xr, yr)]
		direct = n.directMask&(1<<uint((yr>>1)*2+1)) != 0
	default:
		if !s.availB {
			return false
		}
		n := s.top()
		ref = s.e.ref[l][s.idx4(xr, yr)]
		direct = n.directMask&(1<<uint(2+xr>>1)) != 0
	}
	return ref > 0 && !direct
}

func (s *sliceEnc) cabacRefIdx(l, x, y, ref int) {
	ctx := 0
	if s.nbRefCtx(l, x-1, y) {
		ctx++
	}
	if s.nbRefCtx(l, x, y-1) {
		ctx += 2
	}
	for i := 0; i < ref; i++ {
		s.cb.encode(54+ctx, 1)
		ctx = (ctx >> 2) + 4
	}
	s.cb.encode(54+ctx, 0)
}

// nbMvd returns the absolute mvd (clipped) of the neighbour 4x4.
func (s *sliceEnc) nbMvd(l, xr, yr int) [2]uint8 {
	switch {
	case xr >= 0 && yr >= 0:
		return s.mb.mvd[l][yr*4+xr]
	case xr < 0:
		if !s.availA {
			return [2]uint8{}
		}
		return s.left().mvd[l][yr*4+3]
	default:
		if !s.availB {
			return [2]uint8{}
		}
		return s.top().mvd[l][12+xr]
	}
}

func (s *sliceEnc) cabacMvd(l, x, y, w, h, dx, dy int) {
	a, b := s.nbMvd(l, x-1, y), s.nbMvd(l, x, y-1)
	s.cabacMvdComp(40, int(a[0])+int(b[0]), dx)
	s.cabacMvdComp(47, int(a[1])+int(b[1]), dy)
	ax, ay := uint8(min(iabs(dx), 70)), uint8(min(iabs(dy), 70))
	for j := y; j < y+h; j++ {
		for i := x; i < x+w; i++ {
			s.mb.mvd[l][j*4+i] = [2]uint8{ax, ay}
		}
	}
}

func (s *sliceEnc) cabacMvdComp(base, amvd, v int) {
	c := &s.cb
	inc := 0
	if amvd > 32 {
		inc = 2
	} else if amvd >= 3 {
		inc = 1
	}
	a := iabs(v)
	if a == 0 {
		c.encode(base+inc, 0)
		return
	}
	c.encode(base+inc, 1)
	ctx := base + 3
	k := 1
	for k < 9 {
		if a > k {
			c.encode(ctx, 1)
		} else {
			c.encode(ctx, 0)
			break
		}
		if k < 4 {
			ctx++
		}
		k++
	}
	if a >= 9 {
		c.expGolombBypass(a-9, 3)
	}
	c.bypass(b2i(v < 0))
}

// expGolombBypass writes x as a k-th order Exp-Golomb bypass code.
func (c *cabacEnc) expGolombBypass(x, k int) {
	for x >= 1<<uint(k) {
		c.bypass(1)
		x -= 1 << uint(k)
		k++
	}
	c.bypass(0)
	for k > 0 {
		k--
		c.bypass(x >> uint(k) & 1)
	}
}

func (s *sliceEnc) cabacCBP() {
	c := &s.cb
	cbp := s.cbpL
	a, b := s.nbA(), s.nbB()
	for b8 := 0; b8 < 4; b8++ {
		x8, y8 := b8&1, b8>>1
		inc := 0
		if x8 > 0 {
			if cbp&(1<<uint(b8-1)) == 0 {
				inc++
			}
		} else if a != nil && (a.cbp>>uint(b8+1))&1 == 0 {
			inc++
		}
		if y8 > 0 {
			if cbp&(1<<uint(b8-2)) == 0 {
				inc += 2
			}
		} else if b != nil && (b.cbp>>uint(b8+2))&1 == 0 {
			inc += 2
		}
		c.encode(73+inc, cbp>>uint(b8)&1)
	}
	ca, cb := 0, 0
	if a != nil {
		ca = int(a.cbp >> 4)
	}
	if b != nil {
		cb = int(b.cbp >> 4)
	}
	inc := 0
	if ca > 0 {
		inc++
	}
	if cb > 0 {
		inc += 2
	}
	if s.cbpC == 0 {
		c.encode(77+inc, 0)
		return
	}
	c.encode(77+inc, 1)
	inc = 4
	if ca == 2 {
		inc++
	}
	if cb == 2 {
		inc += 2
	}
	c.encode(77+inc, b2i(s.cbpC == 2))
}

func (s *sliceEnc) cabacT8x8() {
	inc := 0
	if a := s.nbA(); a != nil && a.t8x8 {
		inc++
	}
	if b := s.nbB(); b != nil && b.t8x8 {
		inc++
	}
	s.cb.encode(399+inc, b2i(s.t8x8))
}

func (s *sliceEnc) cabacQPDelta(dq int) {
	c := &s.cb
	ctx := 0
	if s.prevQPNZ {
		ctx = 1
	}
	v := 0
	if dq > 0 {
		v = 2*dq - 1
	} else if dq < 0 {
		v = -2 * dq
	}
	if v == 0 {
		c.encode(60+ctx, 0)
	} else {
		c.encode(60+ctx, 1)
		ctx = 2
		for i := 1; i < v; i++ {
			c.encode(60+ctx, 1)
			ctx = 3
		}
		c.encode(60+ctx, 0)
	}
	s.prevQPNZ = dq != 0
}

func (s *sliceEnc) cabacIntraMode(pred, mode int) {
	c := &s.cb
	if mode == pred {
		c.encode(68, 1)
		return
	}
	c.encode(68, 0)
	if mode > pred {
		mode--
	}
	c.encode(69, mode&1)
	c.encode(69, mode>>1&1)
	c.encode(69, mode>>2&1)
}

func (s *sliceEnc) cabacChromaMode() {
	c := &s.cb
	inc := 0
	if a := s.nbA(); a != nil && a.chromaMode != 0 {
		inc++
	}
	if b := s.nbB(); b != nil && b.chromaMode != 0 {
		inc++
	}
	cm := s.chromaMode
	if cm == 0 {
		c.encode(64+inc, 0)
		return
	}
	c.encode(64+inc, 1)
	c.encode(67, b2i(cm > 1))
	if cm > 1 {
		c.encode(67, b2i(cm > 2))
	}
}

// Residual block categories.
const (
	catLumaDC   = 0
	catLumaAC   = 1
	catLuma4x4  = 2
	catChromaDC = 3
	catChromaAC = 4
	catLuma8x8  = 5
)

// cbfInc mirrors the decoder's coded_block_flag ctxIdxInc.
func (s *sliceEnc) cbfInc(cat, x, y int) int {
	mb := s.mb
	cond := func(n *mbInfo, bit uint) int {
		if n == nil {
			return b2i(s.intra)
		}
		return int(n.cbf>>bit) & 1
	}
	a, b := s.nbA(), s.nbB()
	var ca, cb int
	switch cat {
	case catLumaDC:
		ca, cb = cond(a, 24), cond(b, 24)
	case catLumaAC, catLuma4x4:
		if x > 0 {
			ca = int(mb.cbf>>uint(y*4+x-1)) & 1
		} else {
			ca = cond(a, uint(y*4+3))
		}
		if y > 0 {
			cb = int(mb.cbf>>uint((y-1)*4+x)) & 1
		} else {
			cb = cond(b, uint(12+x))
		}
	case catChromaDC:
		ca, cb = cond(a, uint(25+x)), cond(b, uint(25+x))
	case catChromaAC:
		c, blk := x, y
		bx, by := blk&1, blk>>1
		base := uint(16 + 4*c)
		if bx > 0 {
			ca = int(mb.cbf>>(base+uint(blk-1))) & 1
		} else {
			ca = cond(a, base+uint(by*2+1))
		}
		if by > 0 {
			cb = int(mb.cbf>>(base+uint(blk-2))) & 1
		} else {
			cb = cond(b, base+uint(2+bx))
		}
	}
	return ca + 2*cb
}

// cabacBlock codes one residual block (scan order coeff) including its
// coded_block_flag (except for 8x8 blocks) and returns the number of
// non-zero coefficients.
func (s *sliceEnc) cabacBlock(cat, x, y int, coeff []int32) int {
	c := &s.cb
	last := -1
	for i := len(coeff) - 1; i >= 0; i-- {
		if coeff[i] != 0 {
			last = i
			break
		}
	}
	if cat != catLuma8x8 {
		c.encode(85+resCtx.CbfCatOffset[cat]+s.cbfInc(cat, x, y), b2i(last >= 0))
		if last < 0 {
			return 0
		}
	}
	var sigBase, lastBase, absBase int
	if cat == catLuma8x8 {
		sigBase, lastBase, absBase = 402, 417, 426
	} else {
		sigBase = 105 + resCtx.SigCatOffset[cat]
		lastBase = 166 + resCtx.SigCatOffset[cat]
		absBase = 227 + resCtx.AbsCatOffset[cat]
	}
	maxNum := len(coeff)
	n := 0
	for i := 0; i < maxNum-1; i++ {
		var sc, lc int
		if cat == catLuma8x8 {
			sc, lc = sigBase+int(resCtx.Sig8x8[i]), lastBase+int(resCtx.Last8x8[i])
		} else {
			sc, lc = sigBase+i, lastBase+i
		}
		if coeff[i] == 0 {
			c.encode(sc, 0)
			continue
		}
		c.encode(sc, 1)
		if i == last {
			c.encode(lc, 1)
			break
		}
		c.encode(lc, 0)
	}
	isDC := b2i(cat == catChromaDC)
	node := 0
	for i := last; i >= 0; i-- {
		v := coeff[i]
		if v == 0 {
			continue
		}
		n++
		a := int(v)
		if a < 0 {
			a = -a
		}
		if a == 1 {
			c.encode(absBase+int(resCtx.AbsLevel1[node]), 0)
			node = int(resCtx.AbsLevelTrans[0][node])
		} else {
			c.encode(absBase+int(resCtx.AbsLevel1[node]), 1)
			ctx := absBase + int(resCtx.AbsLevelGt1[isDC][node])
			node = int(resCtx.AbsLevelTrans[1][node])
			for k := 2; k < 15; k++ {
				if a > k {
					c.encode(ctx, 1)
				} else {
					c.encode(ctx, 0)
					break
				}
			}
			if a >= 15 {
				c.expGolombBypass(a-15, 0)
			}
		}
		c.bypass(b2i(v < 0))
	}
	return n
}

// writeMBCABAC writes the macroblock layer with CABAC.
func (s *sliceEnc) writeMBCABAC() {
	mb := s.mb
	if s.typ != sliceI {
		s.cabacSkip(false)
	}
	s.cabacMbType()
	cbp := s.cbpL | s.cbpC<<4
	if s.intra {
		if !s.i16 {
			if s.e.cfg.Transform8x8 {
				s.cabacT8x8()
			}
			if s.t8x8 {
				for b8 := 0; b8 < 4; b8++ {
					r := (b8>>1)*8 + (b8&1)*2
					s.cabacIntraMode(s.predIntraMode(r&3, r>>2), int(s.modes[r]))
				}
			} else {
				for _, r := range blkOrder {
					s.cabacIntraMode(s.predIntraMode(r&3, r>>2), int(s.modes[r]))
				}
			}
		}
		s.cabacChromaMode()
		if !s.i16 {
			s.cabacCBP()
		}
	} else {
		s.writeInterPredCABAC()
		s.cabacCBP()
		if s.cbpL != 0 && s.e.cfg.Transform8x8 && s.allow8x8() {
			s.cabacT8x8()
		}
	}
	mb.cbp = uint8(cbp)
	if cbp > 0 || (s.intra && s.i16) {
		s.cabacQPDelta(s.qp - s.lastQP)
	} else {
		s.prevQPNZ = false
	}
	s.writeResidualCABAC()
}

// writeInterPredCABAC writes sub_mb_type / ref_idx / mvd.
func (s *sliceEnc) writeInterPredCABAC() {
	if s.typ == sliceB {
		s.writeBPredCABAC()
		return
	}
	numRefs := s.hdr.numRefs
	s.curDirect = 0
	for i := range s.curRef[0] {
		s.curRef[0][i] = -2
	}
	if s.part == 3 {
		for i := 0; i < 4; i++ {
			s.cabacSubMbType(0)
		}
	}
	geom := partGeom[s.part]
	if numRefs > 1 {
		for i, g := range geom {
			s.cabacRefIdx(0, g[0], g[1], s.pref[i][0])
			s.fillCurRef(0, g, s.pref[i][0])
		}
	}
	for i, g := range geom {
		s.cabacMvd(0, g[0], g[1], g[2], g[3], s.pmv[i][0][0]-s.mvp[i][0][0], s.pmv[i][0][1]-s.mvp[i][0][1])
	}
}

func (s *sliceEnc) fillCurRef(l int, g [4]int, ref int) {
	for y := g[1]; y < g[1]+g[3]; y++ {
		for x := g[0]; x < g[0]+g[2]; x++ {
			s.curRef[l][y*4+x] = int8(ref)
		}
	}
}

// writeResidualCABAC writes the residual and records coded_block_flags.
func (s *sliceEnc) writeResidualCABAC() {
	mb := s.mb
	var sc [64]int32
	if s.intra && s.i16 {
		for i := 0; i < 16; i++ {
			sc[i] = s.dcLv[zz4[i]]
		}
		if s.cabacBlock(catLumaDC, 0, 0, sc[:16]) > 0 {
			mb.cbf |= 1 << 24
		}
		if s.cbpL != 0 {
			for _, r := range blkOrder {
				for i := 0; i < 15; i++ {
					sc[i] = s.lumaLv[r][zz4[i+1]]
				}
				n := s.cabacBlock(catLumaAC, r&3, r>>2, sc[:15])
				mb.nz[r] = uint8(n)
				if n > 0 {
					mb.cbf |= 1 << uint(r)
				}
			}
		}
	} else if s.t8x8 {
		for b8 := 0; b8 < 4; b8++ {
			if s.cbpL&(1<<uint(b8)) == 0 {
				continue
			}
			for i := 0; i < 64; i++ {
				sc[i] = s.luma8Lv[b8][zz8[i]]
			}
			n := s.cabacBlock(catLuma8x8, 0, 0, sc[:64])
			x0, y0 := (b8&1)*2, (b8>>1)*2
			for k := 0; k < 4; k++ {
				r := (y0+k>>1)*4 + x0 + k&1
				mb.nz[r] = uint8(n)
				mb.cbf |= 1 << uint(r)
			}
		}
	} else {
		for _, r := range blkOrder {
			if s.cbpL&(1<<uint((r>>3)*2+(r&3)>>1)) == 0 {
				continue
			}
			for i := 0; i < 16; i++ {
				sc[i] = s.lumaLv[r][zz4[i]]
			}
			n := s.cabacBlock(catLuma4x4, r&3, r>>2, sc[:16])
			mb.nz[r] = uint8(n)
			if n > 0 {
				mb.cbf |= 1 << uint(r)
			}
		}
	}
	if s.cbpC != 0 {
		for c := 0; c < 2; c++ {
			if s.cabacBlock(catChromaDC, c, 0, s.cDCLv[c][:]) > 0 {
				mb.cbf |= 1 << uint(25+c)
			}
		}
	}
	if s.cbpC == 2 {
		for c := 0; c < 2; c++ {
			for blk := 0; blk < 4; blk++ {
				for i := 0; i < 15; i++ {
					sc[i] = s.cACLv[c][blk][zz4[i+1]]
				}
				n := s.cabacBlock(catChromaAC, c, blk, sc[:15])
				mb.nzC[c][blk] = uint8(n)
				if n > 0 {
					mb.cbf |= 1 << uint(16+4*c+blk)
				}
			}
		}
	}
}
