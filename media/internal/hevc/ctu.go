package hevc

// Slice segment data: coding tree units (spec 7.3.8, 9.3).

const (
	modeNone  = 0
	modeIntra = 1
	modeInter = 2
)

const (
	part2Nx2N = iota
	part2NxN
	partNx2N
	partNxN
	part2NxnU
	part2NxnD
	partnLx2N
	partnRx2N
)

// blkInfo is the per-4x4 coding state of the current picture.
type blkInfo struct {
	predMode uint8 // modeNone / modeIntra / modeInter
	skip     bool
	depth    uint8 // coding quadtree depth
	noFilter bool  // pcm with loop filter off, or transquant bypass
	qp       int8  // QpY
	ipm      uint8 // IntraPredModeY
	cbfY     bool  // luma TB containing the block has coefficients
	edgeV    bool  // left edge is a TU or PU boundary
	edgeH    bool  // top edge is a TU or PU boundary
	tuEdgeV  bool  // left edge is a TU boundary
	tuEdgeH  bool
}

type saoParams struct {
	typ     [3]uint8 // 0 off, 1 band, 2 edge
	bandPos [3]uint8
	eoClass [3]uint8
	offset  [3][4]int8
}

// sliceDec decodes the CTUs of one slice segment.
type sliceDec struct {
	d   *Decoder
	h   *sliceHeader
	sps *SPS
	pps *PPS
	pic *picture
	cb  cabac
	ctx [numCtx]uint8

	sliceIdx             int
	ctbAddrRs, ctbAddrTs int
	ctbX, ctbY           int // in CTBs

	qpY                int
	qpYPred            int // qPY_PREV for the next quantization group
	firstQG            bool
	cuQPDeltaCoded     bool
	cuQPDeltaVal       int
	cuTransquantBypass bool

	// Current CU.
	cuX, cuY, cuLog2 int
	predMode         int
	partMode         int
	intraChromaMode  int // derived chroma mode (0..34)
	maxTrafoDepth    int
	intraSplit       bool
	mergeFlag        bool

	coeff [32 * 32]int32
	tmp   [32 * 32]int32
}

// ctbAddrOf returns the RS address of the CTB containing (x, y).
func (s *sliceDec) ctbAddrOf(x, y int) int {
	return (y>>s.sps.Log2Ctb)*s.sps.PicWCtb + x>>s.sps.Log2Ctb
}

// available implements the z-scan availability check (6.4.1).
func (s *sliceDec) available(xCur, yCur, xN, yN int) bool {
	sp := s.sps
	if xN < 0 || yN < 0 || xN >= sp.Width || yN >= sp.Height {
		return false
	}
	p := s.pps
	tw := sp.PicWCtb << (sp.Log2Ctb - sp.Log2MinTb)
	zN := p.minTbAddrZs[(yN>>sp.Log2MinTb)*tw+xN>>sp.Log2MinTb]
	zC := p.minTbAddrZs[(yCur>>sp.Log2MinTb)*tw+xCur>>sp.Log2MinTb]
	if zN > zC {
		return false
	}
	fc := &s.d.fc
	cn, cc := s.ctbAddrOf(xN, yN), s.ctbAddrOf(xCur, yCur)
	if fc.ctbSliceAddr[cn] != fc.ctbSliceAddr[cc] {
		return false
	}
	return p.tileID[p.ctbRsToTs[cn]] == p.tileID[p.ctbRsToTs[cc]]
}

func (s *sliceDec) blk(x, y int) *blkInfo {
	fc := &s.d.fc
	return &fc.blk[(y>>2)*fc.w4+x>>2]
}

// decode decodes the slice segment data starting at byte offset off of
// the RBSP.
func (s *sliceDec) decode(rbsp []byte, off int) error {
	sp, p, h := s.sps, s.pps, s.h
	fc := &s.d.fc
	s.ctbAddrRs = h.address
	s.ctbAddrTs = p.ctbRsToTs[h.address]
	sliceQP := 26 + (p.InitQP - 26) + h.qpDelta
	if sliceQP < 0 || sliceQP > 51 {
		return errCorrupt
	}
	s.cb.start(rbsp, off)
	initType := 0
	switch h.typ {
	case sliceP:
		initType = 1
		if h.cabacInit {
			initType = 2
		}
	case sliceB:
		initType = 2
		if h.cabacInit {
			initType = 1
		}
	}
	if h.dependent {
		s.qpY, s.qpYPred = s.d.dsQP[0], s.d.dsQP[1]
	} else {
		s.qpY, s.qpYPred = sliceQP, sliceQP
		s.firstQG = true
	}
	sliceAddr := h.address
	if h.dependent {
		sliceAddr = s.d.sliceAddrRs
	} else {
		s.d.sliceAddrRs = h.address
	}
	nCtb := sp.PicWCtb * sp.PicHCtb
	first := true
	for {
		if s.ctbAddrTs >= nCtb {
			return errCorrupt
		}
		s.ctbAddrRs = p.ctbTsToRs[s.ctbAddrTs]
		s.ctbX, s.ctbY = s.ctbAddrRs%sp.PicWCtb, s.ctbAddrRs/sp.PicWCtb
		fc.ctbSliceAddr[s.ctbAddrRs] = sliceAddr
		fc.ctbSlice[s.ctbAddrRs] = s.sliceIdx
		x0, y0 := s.ctbX<<sp.Log2Ctb, s.ctbY<<sp.Log2Ctb
		tileStart := s.ctbAddrTs == 0 || p.tileID[s.ctbAddrTs] != p.tileID[s.ctbAddrTs-1]
		rowStart := p.EntropySync && s.ctbX == p.colBd[s.tileCol()]
		// Context initialisation / synchronisation (9.3.1).
		switch {
		case tileStart:
			initContexts(&s.ctx, initType, sliceQP)
		case rowStart:
			initContexts(&s.ctx, initType, sliceQP)
			if s.d.wppValid && s.available(x0, y0, x0+sp.CtbSize, y0-sp.CtbSize) {
				s.ctx = s.d.wppCtx
			}
		case first && h.dependent && s.d.dsCtxValid:
			s.ctx = s.d.dsCtx
		case first:
			initContexts(&s.ctx, initType, sliceQP)
		}
		if tileStart || rowStart {
			s.firstQG = true
			s.qpY, s.qpYPred = sliceQP, sliceQP
		}
		first = false
		if h.saoLuma || h.saoChroma {
			s.parseSAO()
		}
		if err := s.codingQuadtree(x0, y0, sp.Log2Ctb, 0); err != nil {
			return err
		}
		end := s.cb.terminate()
		// WPP storage after the second CTB of a row in a tile.
		if p.EntropySync {
			col := s.tileCol()
			if s.ctbX == p.colBd[col]+1 || (p.colBd[col+1]-p.colBd[col] == 1 && s.ctbX == p.colBd[col]) {
				s.d.wppCtx = s.ctx
				s.d.wppValid = true
			}
		}
		if end == 1 {
			s.d.dsCtx = s.ctx
			s.d.dsCtxValid = true
			s.d.dsQP = [2]int{s.qpY, s.qpYPred}
			return nil
		}
		s.ctbAddrTs++
		if s.ctbAddrTs >= nCtb {
			return errCorrupt
		}
		nextRs := p.ctbTsToRs[s.ctbAddrTs]
		newTile := p.tileID[s.ctbAddrTs] != p.tileID[s.ctbAddrTs-1]
		newRow := p.EntropySync && nextRs%sp.PicWCtb == p.colBd[s.tileColOf(nextRs%sp.PicWCtb)]
		if newTile || newRow {
			if s.cb.terminate() != 1 { // end_of_subset_one_bit
				return errCorrupt
			}
			s.cb.start(rbsp, s.cb.alignAfterTerminate())
		}
	}
}

func (s *sliceDec) tileCol() int { return s.tileColOf(s.ctbX) }

func (s *sliceDec) tileColOf(x int) int {
	p := s.pps
	c := 0
	for i := range p.colWidths {
		if x >= p.colBd[i] {
			c = i
		}
	}
	return c
}

// parseSAO parses sao() for the current CTB (7.3.8.3).
func (s *sliceDec) parseSAO() {
	sp, p, h := s.sps, s.pps, s.h
	fc := &s.d.fc
	rx, ry := s.ctbX, s.ctbY
	cur := &fc.sao[s.ctbAddrRs]
	*cur = saoParams{}
	if rx > 0 {
		left := s.ctbAddrRs - 1
		if fc.ctbSliceAddr[left] == fc.ctbSliceAddr[s.ctbAddrRs] && p.tileID[p.ctbRsToTs[left]] == p.tileID[s.ctbAddrTs] {
			if s.cb.decision(&s.ctx[ctxSaoMerge]) == 1 {
				*cur = fc.sao[left]
				return
			}
		}
	}
	if ry > 0 {
		up := s.ctbAddrRs - sp.PicWCtb
		if fc.ctbSliceAddr[up] == fc.ctbSliceAddr[s.ctbAddrRs] && p.tileID[p.ctbRsToTs[up]] == p.tileID[s.ctbAddrTs] {
			if s.cb.decision(&s.ctx[ctxSaoMerge]) == 1 {
				*cur = fc.sao[up]
				return
			}
		}
	}
	for c := 0; c < 3; c++ {
		if (c == 0 && !h.saoLuma) || (c > 0 && !h.saoChroma) {
			cur.typ[c] = 0
			continue
		}
		if c == 2 {
			cur.typ[2] = cur.typ[1]
			cur.eoClass[2] = cur.eoClass[1]
		} else {
			t := 0
			if s.cb.decision(&s.ctx[ctxSaoType]) == 1 {
				t = 1 + s.cb.bypass()
			}
			cur.typ[c] = uint8(t)
		}
		if cur.typ[c] == 0 {
			continue
		}
		var abs [4]int
		for i := 0; i < 4; i++ {
			for abs[i] < 7 && s.cb.bypass() == 1 {
				abs[i]++
			}
		}
		if cur.typ[c] == 1 {
			for i := 0; i < 4; i++ {
				if abs[i] != 0 && s.cb.bypass() == 1 {
					abs[i] = -abs[i]
				}
			}
			cur.bandPos[c] = uint8(s.cb.bypassBits(5))
		} else {
			abs[2], abs[3] = -abs[2], -abs[3]
			if c == 0 {
				cur.eoClass[0] = uint8(s.cb.bypassBits(2))
			} else if c == 1 {
				cur.eoClass[1] = uint8(s.cb.bypassBits(2))
				cur.eoClass[2] = cur.eoClass[1]
			}
		}
		for i := 0; i < 4; i++ {
			cur.offset[c][i] = int8(abs[i])
		}
	}
}

// codingQuadtree parses coding_quadtree() (7.3.8.4).
func (s *sliceDec) codingQuadtree(x0, y0, log2, depth int) error {
	sp, p := s.sps, s.pps
	size := 1 << log2
	split := log2 > sp.Log2MinCb
	if x0+size <= sp.Width && y0+size <= sp.Height && log2 > sp.Log2MinCb {
		inc := 0
		if s.available(x0, y0, x0-1, y0) && int(s.blk(x0-1, y0).depth) > depth {
			inc++
		}
		if s.available(x0, y0, x0, y0-1) && int(s.blk(x0, y0-1).depth) > depth {
			inc++
		}
		split = s.cb.decision(&s.ctx[ctxSplitCU+inc]) == 1
	}
	qgLog2 := sp.Log2Ctb - p.DiffCuQPDeltaDepth
	if p.CuQPDeltaEnabled && log2 >= qgLog2 {
		s.cuQPDeltaCoded = false
		s.cuQPDeltaVal = 0
	}
	qgMask := 1<<qgLog2 - 1
	if split {
		h := size >> 1
		for i := 0; i < 4; i++ {
			x, y := x0+(i&1)*h, y0+(i>>1)*h
			if x < sp.Width && y < sp.Height {
				if err := s.codingQuadtree(x, y, log2-1, depth+1); err != nil {
					return err
				}
			}
		}
	} else {
		if err := s.codingUnit(x0, y0, log2, depth); err != nil {
			return err
		}
	}
	if (x0+size)&qgMask == 0 && (y0+size)&qgMask == 0 {
		s.qpYPred = s.qpY
	}
	return nil
}

// setQP derives QpY for the CU at (x0, y0) (8.6.1).
func (s *sliceDec) setQP(xBase, yBase int) {
	sp, p := s.sps, s.pps
	qgLog2 := sp.Log2Ctb - p.DiffCuQPDeltaDepth
	mask := 1<<qgLog2 - 1
	ctbMask := sp.CtbSize - 1
	xQg, yQg := xBase-(xBase&mask), yBase-(yBase&mask)
	var pred int
	if s.firstQG || (xQg == 0 && yQg == 0) {
		s.firstQG = !s.cuQPDeltaCoded
		pred = s.h.sliceQP()
	} else {
		pred = s.qpYPred
	}
	qa, qb := pred, pred
	if xBase&ctbMask != 0 && xQg&ctbMask != 0 {
		qa = int(s.blk(xQg-1, yQg).qp)
	}
	if yBase&ctbMask != 0 && yQg&ctbMask != 0 {
		qb = int(s.blk(xQg, yQg-1).qp)
	}
	pred = (qa + qb + 1) >> 1
	s.qpY = ((pred+s.cuQPDeltaVal+52)%52 + 52) % 52
}

func (h *sliceHeader) sliceQP() int { return h.pps.InitQP + h.qpDelta }

// codingUnit parses coding_unit() (7.3.8.5) and reconstructs intra CUs.
func (s *sliceDec) codingUnit(x0, y0, log2, depth int) error {
	sp, p, h := s.sps, s.pps, s.h
	fc := &s.d.fc
	n := 1 << log2
	s.cuX, s.cuY, s.cuLog2 = x0, y0, log2
	s.cuTransquantBypass = false
	if p.TransquantBypass {
		s.cuTransquantBypass = s.cb.decision(&s.ctx[ctxTransquantBypass]) == 1
	}
	skip := false
	if h.typ != sliceI {
		inc := 0
		if s.available(x0, y0, x0-1, y0) && s.blk(x0-1, y0).skip {
			inc++
		}
		if s.available(x0, y0, x0, y0-1) && s.blk(x0, y0-1).skip {
			inc++
		}
		skip = s.cb.decision(&s.ctx[ctxSkip+inc]) == 1
	}
	s.predMode = modeIntra
	if h.typ != sliceI {
		s.predMode = modeInter
	}
	s.partMode = part2Nx2N
	s.intraSplit = false
	pcm := false
	// Record the CU geometry for neighbours before parsing PUs (merge
	// candidates of later PUs inside this CU use the 4x4 state).
	s.fillCU(x0, y0, n, depth, skip)
	if skip {
		s.predMode = modeInter
		s.setCUMode(x0, y0, n, modeInter)
		if err := s.predictionUnit(x0, y0, n, n, 0, true); err != nil {
			return err
		}
		s.markTUEdges(x0, y0, n)
		s.finishCUQP(x0, y0, n)
		return nil
	}
	if h.typ != sliceI {
		if s.cb.decision(&s.ctx[ctxPredMode]) == 1 {
			s.predMode = modeIntra
		}
	}
	s.setCUMode(x0, y0, n, s.predMode)
	if s.predMode != modeIntra || log2 == sp.Log2MinCb {
		s.partMode = s.parsePartMode(log2)
		s.intraSplit = s.predMode == modeIntra && s.partMode == partNxN
	}
	if s.predMode == modeIntra {
		if s.partMode == part2Nx2N && sp.PCM && log2 >= sp.Log2MinPCM && log2 <= sp.Log2MaxPCM {
			pcm = s.cb.terminate() == 1
		}
		if pcm {
			if err := s.decodePCM(x0, y0, log2); err != nil {
				return err
			}
			s.finishCUQP(x0, y0, n)
			return nil
		}
		s.parseIntraModes(x0, y0, log2)
	} else {
		if err := s.parsePUs(x0, y0, n); err != nil {
			return err
		}
	}
	rqtRoot := true
	if s.predMode != modeIntra && !(s.partMode == part2Nx2N && s.mergeFlag) {
		rqtRoot = s.cb.decision(&s.ctx[ctxRqtRootCbf]) == 1
	}
	if rqtRoot {
		if s.predMode == modeIntra {
			s.maxTrafoDepth = sp.MaxTrDepthIntra + boolInt(s.intraSplit)
		} else {
			s.maxTrafoDepth = sp.MaxTrDepthInter
		}
		if err := s.transformTree(x0, y0, x0, y0, log2, 0, 0, [2]bool{true, true}); err != nil {
			return err
		}
	} else if s.predMode == modeIntra {
		// Not reachable: intra CUs always have rqt_root_cbf = 1.
	} else {
		s.markTUEdges(x0, y0, n)
	}
	_ = fc
	s.finishCUQP(x0, y0, n)
	return nil
}

// finishCUQP derives (when no delta was coded) and stores the QpY of the
// CU.
func (s *sliceDec) finishCUQP(x0, y0, n int) {
	sp := s.sps
	if s.pps.CuQPDeltaEnabled && !s.cuQPDeltaCoded {
		s.setQP(x0, y0)
	}
	for y := y0; y < min(y0+n, sp.Height); y += 4 {
		for x := x0; x < min(x0+n, sp.Width); x += 4 {
			b := s.blk(x, y)
			b.qp = int8(s.qpY)
			if s.cuTransquantBypass {
				b.noFilter = true
			}
		}
	}
}

func (s *sliceDec) fillCU(x0, y0, n, depth int, skip bool) {
	sp := s.sps
	for y := y0; y < min(y0+n, sp.Height); y += 4 {
		for x := x0; x < min(x0+n, sp.Width); x += 4 {
			b := s.blk(x, y)
			*b = blkInfo{depth: uint8(depth), skip: skip, qp: int8(s.qpY), ipm: 1}
		}
	}
}

func (s *sliceDec) setCUMode(x0, y0, n, mode int) {
	sp := s.sps
	for y := y0; y < min(y0+n, sp.Height); y += 4 {
		for x := x0; x < min(x0+n, sp.Width); x += 4 {
			s.blk(x, y).predMode = uint8(mode)
		}
	}
}

func (s *sliceDec) parsePartMode(log2 int) int {
	sp := s.sps
	c := &s.cb
	if c.decision(&s.ctx[ctxPartMode]) == 1 {
		return part2Nx2N
	}
	if s.predMode == modeIntra {
		return partNxN
	}
	if log2 == sp.Log2MinCb {
		if c.decision(&s.ctx[ctxPartMode+1]) == 1 {
			return part2NxN
		}
		if log2 == 3 {
			return partNx2N
		}
		if c.decision(&s.ctx[ctxPartMode+2]) == 1 {
			return partNx2N
		}
		return partNxN
	}
	if !sp.AMP {
		if c.decision(&s.ctx[ctxPartMode+1]) == 1 {
			return part2NxN
		}
		return partNx2N
	}
	if c.decision(&s.ctx[ctxPartMode+1]) == 1 {
		if c.decision(&s.ctx[ctxPartMode+3]) == 1 {
			return part2NxN
		}
		if c.bypass() == 1 {
			return part2NxnD
		}
		return part2NxnU
	}
	if c.decision(&s.ctx[ctxPartMode+3]) == 1 {
		return partNx2N
	}
	if c.bypass() == 1 {
		return partnRx2N
	}
	return partnLx2N
}

// parseIntraModes parses and derives the luma and chroma intra modes.
func (s *sliceDec) parseIntraModes(x0, y0, log2 int) {
	sp := s.sps
	n := 1 << log2
	pb := n
	nParts := 1
	if s.partMode == partNxN {
		pb = n / 2
		nParts = 4
	}
	var prev [4]bool
	for i := 0; i < nParts; i++ {
		prev[i] = s.cb.decision(&s.ctx[ctxPrevIntra]) == 1
	}
	var modes [4]int
	for i := 0; i < nParts; i++ {
		xp, yp := x0+(i&1)*pb, y0+(i>>1)*pb
		var mpm, rem int
		if prev[i] {
			mpm = 0
			for mpm < 2 && s.cb.bypass() == 1 {
				mpm++
			}
		} else {
			rem = s.cb.bypassBits(5)
		}
		// Candidates (8.4.2).
		candA, candB := 1, 1
		if s.available(xp, yp, xp-1, yp) {
			if b := s.blk(xp-1, yp); b.predMode == modeIntra && !b.pcmLike() {
				candA = int(b.ipm)
			}
		}
		if s.available(xp, yp, xp, yp-1) && yp-1 >= (yp>>sp.Log2Ctb)<<sp.Log2Ctb {
			if b := s.blk(xp, yp-1); b.predMode == modeIntra && !b.pcmLike() {
				candB = int(b.ipm)
			}
		}
		var list [3]int
		if candA == candB {
			if candA < 2 {
				list = [3]int{0, 1, 26}
			} else {
				list = [3]int{candA, 2 + (candA+29)%32, 2 + (candA-2+1)%32}
			}
		} else {
			list[0], list[1] = candA, candB
			switch {
			case candA != 0 && candB != 0:
				list[2] = 0
			case candA != 1 && candB != 1:
				list[2] = 1
			default:
				list[2] = 26
			}
		}
		var mode int
		if prev[i] {
			mode = list[mpm]
		} else {
			if list[0] > list[1] {
				list[0], list[1] = list[1], list[0]
			}
			if list[0] > list[2] {
				list[0], list[2] = list[2], list[0]
			}
			if list[1] > list[2] {
				list[1], list[2] = list[2], list[1]
			}
			mode = rem
			for j := 0; j < 3; j++ {
				if mode >= list[j] {
					mode++
				}
			}
		}
		modes[i] = mode
		for y := yp; y < min(yp+pb, sp.Height); y += 4 {
			for x := xp; x < min(xp+pb, sp.Width); x += 4 {
				s.blk(x, y).ipm = uint8(mode)
			}
		}
	}
	// intra_chroma_pred_mode
	c := 4
	if s.cb.decision(&s.ctx[ctxChromaMode]) == 1 {
		c = s.cb.bypassBits(2)
	}
	luma := modes[0]
	if c == 4 {
		s.intraChromaMode = luma
	} else {
		m := [4]int{0, 26, 10, 1}[c]
		if m == luma {
			m = 34
		}
		s.intraChromaMode = m
	}
}

func (b *blkInfo) pcmLike() bool { return false }

// transformTree parses transform_tree() (7.3.8.8). parentCbf holds the
// parent's cbf_cb / cbf_cr.
func (s *sliceDec) transformTree(x0, y0, xBase, yBase, log2, depth, blkIdx int, parentCbf [2]bool) error {
	sp := s.sps
	var split bool
	if log2 <= sp.Log2MaxTb && log2 > sp.Log2MinTb && depth < s.maxTrafoDepth && !(s.intraSplit && depth == 0) {
		split = s.cb.decision(&s.ctx[ctxSplitTransform+5-log2]) == 1
	} else {
		interSplit := sp.MaxTrDepthInter == 0 && s.predMode == modeInter && s.partMode != part2Nx2N && depth == 0
		split = log2 > sp.Log2MaxTb || (s.intraSplit && depth == 0) || interSplit
	}
	var cbfC [2]bool
	if log2 > 2 {
		for c := 0; c < 2; c++ {
			if depth == 0 || parentCbf[c] {
				cbfC[c] = s.cb.decision(&s.ctx[ctxCbfChroma+depth]) == 1
			}
		}
	} else {
		cbfC = parentCbf
	}
	if split {
		h := 1 << (log2 - 1)
		for i := 0; i < 4; i++ {
			if err := s.transformTree(x0+(i&1)*h, y0+(i>>1)*h, x0, y0, log2-1, depth+1, i, cbfC); err != nil {
				return err
			}
		}
		return nil
	}
	cbfY := true
	if s.predMode == modeIntra || depth != 0 || cbfC[0] || cbfC[1] {
		inc := 0
		if depth == 0 {
			inc = 1
		}
		cbfY = s.cb.decision(&s.ctx[ctxCbfLuma+inc]) == 1
	}
	return s.transformUnit(x0, y0, xBase, yBase, log2, depth, blkIdx, cbfY, cbfC)
}

// transformUnit parses transform_unit() (7.3.8.10) and reconstructs the
// block (intra prediction for intra CUs, then the residual).
func (s *sliceDec) transformUnit(x0, y0, xBase, yBase, log2, depth, blkIdx int, cbfY bool, cbfC [2]bool) error {
	p := s.pps
	n := 1 << log2
	s.markTU(x0, y0, n, cbfY)
	if cbfY || cbfC[0] || cbfC[1] {
		if p.CuQPDeltaEnabled && !s.cuQPDeltaCoded {
			v := 0
			for v < 5 && s.cb.decision(&s.ctx[ctxCuQPDelta+min(v, 1)]) == 1 {
				v++
			}
			if v == 5 {
				k := 0
				for s.cb.bypass() == 1 {
					k++
					if k > 31 {
						return errCorrupt
					}
				}
				v += (1<<uint(k) - 1) + s.cb.bypassBits(k)
			}
			if v > 0 && s.cb.bypass() == 1 {
				v = -v
			}
			s.cuQPDeltaCoded = true
			s.cuQPDeltaVal = v
			if v < -26 || v > 25 {
				return errCorrupt
			}
			s.setQP(s.cuX, s.cuY)
		}
	}
	intra := s.predMode == modeIntra
	pic := s.pic
	if intra {
		s.intraPredict(0, x0, y0, n, int(s.blk(x0, y0).ipm))
	}
	if cbfY {
		if err := s.residual(x0, y0, log2, 0); err != nil {
			return err
		}
	}
	_ = pic
	if log2 > 2 {
		xc, yc := x0/2, y0/2
		for c := 1; c <= 2; c++ {
			if intra {
				s.intraPredict(c, xc, yc, n/2, s.intraChromaMode)
			}
			if cbfC[c-1] {
				if err := s.residual(x0, y0, log2-1, c); err != nil {
					return err
				}
			}
		}
	} else if blkIdx == 3 {
		xc, yc := xBase/2, yBase/2
		for c := 1; c <= 2; c++ {
			if intra {
				s.intraPredict(c, xc, yc, 4, s.intraChromaMode)
			}
			if cbfC[c-1] {
				if err := s.residual(xBase, yBase, 2, c); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// markTU records a transform block's edges and coded flag.
func (s *sliceDec) markTU(x0, y0, n int, cbf bool) {
	sp := s.sps
	for y := y0; y < min(y0+n, sp.Height); y += 4 {
		for x := x0; x < min(x0+n, sp.Width); x += 4 {
			b := s.blk(x, y)
			b.cbfY = cbf
			if x == x0 {
				b.edgeV, b.tuEdgeV = true, true
			}
			if y == y0 {
				b.edgeH, b.tuEdgeH = true, true
			}
		}
	}
}

// markTUEdges marks the CU boundary as a transform edge (CUs without a
// residual are one transform unit).
func (s *sliceDec) markTUEdges(x0, y0, n int) { s.markTU(x0, y0, n, false) }

// markPUEdges marks a prediction block's edges.
func (s *sliceDec) markPUEdges(x0, y0, w, h int) {
	sp := s.sps
	for y := y0; y < min(y0+h, sp.Height); y += 4 {
		if x0 < sp.Width {
			s.blk(x0, y).edgeV = true
		}
	}
	for x := x0; x < min(x0+w, sp.Width); x += 4 {
		if y0 < sp.Height {
			s.blk(x, y0).edgeH = true
		}
	}
}

// decodePCM reads pcm_sample() after pcm_flag.
func (s *sliceDec) decodePCM(x0, y0, log2 int) error {
	sp := s.sps
	off := s.cb.alignAfterTerminate()
	var r bitReader
	r.reset(s.cb.data)
	r.pos = off * 8
	n := 1 << log2
	pic := s.pic
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			v := int(r.u(sp.PCMBitDepth)) << uint(8-sp.PCMBitDepth)
			pic.y[(y0+y)*pic.ys+x0+x] = byte(v)
		}
	}
	for c := 0; c < 2; c++ {
		pl := pic.cb
		if c == 1 {
			pl = pic.cr
		}
		for y := 0; y < n/2; y++ {
			for x := 0; x < n/2; x++ {
				v := int(r.u(sp.PCMBitDepthC)) << uint(8-sp.PCMBitDepthC)
				pl[(y0/2+y)*pic.cs+x0/2+x] = byte(v)
			}
		}
	}
	if r.e != nil {
		return r.e
	}
	s.cb.start(s.cb.data, (r.pos+7)>>3)
	for y := y0; y < min(y0+n, sp.Height); y += 4 {
		for x := x0; x < min(x0+n, sp.Width); x += 4 {
			b := s.blk(x, y)
			b.noFilter = sp.PCMLoopFilterOff
			b.cbfY = true
			b.ipm = 1
		}
	}
	s.markTU(x0, y0, n, true)
	return nil
}
