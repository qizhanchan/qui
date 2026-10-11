package av1

import "math"

// Rate-distortion search for intra superblocks. Trials run with the sink
// estimating (no records, no CDF adaptation) and are undone with
// snapshots; the decided tree is then coded again with the sink
// recording (commit), which reproduces the same reconstruction.

// encSnap saves the reconstruction and coding state of a block region:
// samples, mode info, transform sizes, coefficient contexts and the
// intra-edge availability flags.
type encSnap struct {
	r0, c0, r1, c1 int // mi region, rounded out to 8x8 and clipped
	pix            [3][]uint8
	mi             []miInfo
	itx            []uint8
	lftx           [3][]uint8
	aLvl, aDC      [3][]uint8
	lLvl, lDC      [3][32]uint8
	bd             [3][34][34]bool
}

func (e *Encoder) save(s *encSnap, r, c, bw4, bh4 int) {
	t := e.t
	f := t.f
	s.r0, s.c0 = r&^1, c&^1
	s.r1, s.c1 = min((r+bh4+1)&^1, f.miRows), min((c+bw4+1)&^1, f.miCols)
	rows, cols := s.r1-s.r0, s.c1-s.c0
	for p := 0; p < 3; p++ {
		ss := min(p, 1)
		pb := &f.cur.planes[p]
		x0, y0 := (s.c0*4)>>ss, (s.r0*4)>>ss
		w, h := (cols*4)>>ss, (rows*4)>>ss
		s.pix[p] = s.pix[p][:0]
		for y := 0; y < h; y++ {
			o := (y0+y)*pb.stride + x0
			s.pix[p] = append(s.pix[p], pb.pix8[o:o+w]...)
		}
		ls := f.lfStride[p]
		s.lftx[p] = s.lftx[p][:0]
		for y := s.r0 >> ss; y < (s.r1+ss)>>ss; y++ {
			s.lftx[p] = append(s.lftx[p], f.lfTxSize[p][y*ls+s.c0>>ss:y*ls+(s.c1+ss)>>ss]...)
		}
		s.aLvl[p] = append(s.aLvl[p][:0], t.aboveLevel[p][s.c0>>ss:(s.c1+ss)>>ss]...)
		s.aDC[p] = append(s.aDC[p][:0], t.aboveDC[p][s.c0>>ss:(s.c1+ss)>>ss]...)
		s.lLvl[p], s.lDC[p] = t.leftLevel[p], t.leftDC[p]
	}
	s.mi, s.itx = s.mi[:0], s.itx[:0]
	for y := s.r0; y < s.r1; y++ {
		o := y*f.miCols + s.c0
		s.mi = append(s.mi, f.mi[o:o+cols]...)
		s.itx = append(s.itx, f.interTxSize[o:o+cols]...)
	}
	s.bd = t.blockDecoded
}

func (e *Encoder) load(s *encSnap) {
	t := e.t
	f := t.f
	rows, cols := s.r1-s.r0, s.c1-s.c0
	for p := 0; p < 3; p++ {
		ss := min(p, 1)
		pb := &f.cur.planes[p]
		x0, y0 := (s.c0*4)>>ss, (s.r0*4)>>ss
		w, h := (cols*4)>>ss, (rows*4)>>ss
		for y := 0; y < h; y++ {
			o := (y0+y)*pb.stride + x0
			copy(pb.pix8[o:o+w], s.pix[p][y*w:])
		}
		ls := f.lfStride[p]
		n := (s.c1+ss)>>ss - s.c0>>ss
		for i, y := 0, s.r0>>ss; y < (s.r1+ss)>>ss; i, y = i+1, y+1 {
			copy(f.lfTxSize[p][y*ls+s.c0>>ss:], s.lftx[p][i*n:i*n+n])
		}
		copy(t.aboveLevel[p][s.c0>>ss:], s.aLvl[p])
		copy(t.aboveDC[p][s.c0>>ss:], s.aDC[p])
		t.leftLevel[p], t.leftDC[p] = s.lLvl[p], s.lDC[p]
	}
	for i, y := 0, s.r0; y < s.r1; i, y = i+1, y+1 {
		o := y*f.miCols + s.c0
		copy(f.mi[o:o+cols], s.mi[i*cols:])
		copy(f.interTxSize[o:o+cols], s.itx[i*cols:])
	}
	t.blockDecoded = s.bd
}

// snap returns a snapshot from the pool; release it with unsnap (LIFO).
func (e *Encoder) snap() *encSnap {
	if e.snapTop == len(e.snaps) {
		e.snaps = append(e.snaps, new(encSnap))
	}
	s := e.snaps[e.snapTop]
	e.snapTop++
	return s
}

func (e *Encoder) unsnap() { e.snapTop-- }

// encNode is a decided partition: its type and, for the blocks it codes
// directly, their choices (split nodes have kids).
type encNode struct {
	part int
	ch   [4]blkChoice
	kids [4]*encNode
}

// newNode returns a zeroed node from the superblock's arena.
func (e *Encoder) newNode(part int) *encNode {
	if e.nodeTop == len(e.nodes) {
		e.nodes = append(e.nodes, make([]encNode, 256)...)
	}
	n := &e.nodes[e.nodeTop]
	e.nodeTop++
	*n = encNode{part: part}
	return n
}

// encodeSB searches the superblock's partition tree and commits it.
func (e *Encoder) encodeSB(r, c int) {
	t := e.t
	e.nodeTop = 0
	sb := t.sbSize()
	s0 := e.snap()
	defer e.unsnap()
	e.save(s0, r, c, num4x4W[sb], num4x4H[sb])
	e.sink.record = false
	node, _ := e.searchNode(r, c, sb, math.Inf(1))
	e.load(s0)
	e.sink.record = true
	e.applyNode(r, c, sb, node)
}

// partBlocks lists the blocks a partition codes directly (not SPLIT).
func partBlocks(part, r, c, bSize int) (blocks [4][2]int, n, sub int) {
	half := num4x4W[bSize] >> 1
	sub = partitionSubsize[part][bSize]
	switch part {
	case partNone:
		return [4][2]int{{r, c}}, 1, sub
	case partHorz:
		return [4][2]int{{r, c}, {r + half, c}}, 2, sub
	case partVert:
		return [4][2]int{{r, c}, {r, c + half}}, 2, sub
	}
	return blocks, 0, sub
}

// applyNode codes a decided tree (estimating or recording, as the sink
// is set).
func (e *Encoder) applyNode(r, c, bSize int, n *encNode) {
	f := e.t.f
	if r >= f.miRows || c >= f.miCols {
		return
	}
	e.writePartition(r, c, bSize, n.part)
	if n.part == partSplit {
		half := num4x4W[bSize] >> 1
		sub := partitionSubsize[partSplit][bSize]
		for i, o := range [4][2]int{{0, 0}, {0, half}, {half, 0}, {half, half}} {
			if n.kids[i] != nil {
				e.applyNode(r+o[0], c+o[1], sub, n.kids[i])
			}
		}
		return
	}
	blocks, nb, sub := partBlocks(n.part, r, c, bSize)
	for i := 0; i < nb; i++ {
		br, bc := blocks[i][0], blocks[i][1]
		if br < f.miRows && bc < f.miCols {
			ch := n.ch[i]
			e.codeBlock(br, bc, sub, &ch)
		}
	}
}

// partCost is the estimated cost of the partition symbol.
func (e *Encoder) partCost(r, c, bSize, part int) float64 {
	k := &e.sink
	before := k.cost
	e.writePartition(r, c, bSize, part)
	bits := k.cost - before
	k.cost = before
	return e.lambda * float64(bits) / 256
}

// searchNode finds the best partition of the node within budget and
// leaves its reconstruction in place. It returns the node and its cost
// (Inf when nothing beats the budget).
func (e *Encoder) searchNode(r, c, bSize int, budget float64) (*encNode, float64) {
	t := e.t
	f := t.f
	if r >= f.miRows || c >= f.miCols {
		return e.newNode(partNone), 0
	}
	half := num4x4W[bSize] >> 1
	hasRows := r+half < f.miRows
	hasCols := c+half < f.miCols
	var partBuf [4]int
	parts := partBuf[:0]
	switch {
	case bSize == block4x4:
		parts = append(parts, partNone)
	case hasRows && hasCols:
		parts = append(parts, partNone, partHorz, partVert)
		if bSize > block8x8 || e.allow4x4 {
			parts = append(parts, partSplit)
		}
	case hasCols:
		parts = append(parts, partHorz, partSplit)
	case hasRows:
		parts = append(parts, partVert, partSplit)
	default:
		parts = append(parts, partSplit)
	}
	s0 := e.snap()
	defer e.unsnap()
	e.save(s0, r, c, num4x4W[bSize], num4x4H[bSize])
	var best *encNode
	bestCost := budget
	lastBest := false
	for pi, part := range parts {
		if pi > 0 {
			e.load(s0)
		}
		cost := e.partCost(r, c, bSize, part)
		node := e.newNode(part)
		if part == partSplit {
			sub := partitionSubsize[partSplit][bSize]
			for i, o := range [4][2]int{{0, 0}, {0, half}, {half, 0}, {half, half}} {
				if cost >= bestCost {
					break
				}
				kid, kc := e.searchNode(r+o[0], c+o[1], sub, bestCost-cost)
				node.kids[i] = kid
				cost += kc
			}
		} else {
			blocks, nb, sub := partBlocks(part, r, c, bSize)
			for i := 0; i < nb && cost < bestCost; i++ {
				br, bc := blocks[i][0], blocks[i][1]
				if br >= f.miRows || bc >= f.miCols {
					continue
				}
				ch, bcost := e.searchBlock(br, bc, sub)
				node.ch[i] = ch
				cost += bcost
			}
		}
		lastBest = cost < bestCost
		if lastBest {
			best, bestCost = node, cost
		}
	}
	if best == nil {
		e.load(s0)
		return e.newNode(parts[0]), math.Inf(1)
	}
	if !lastBest {
		e.load(s0)
		e.applyNode(r, c, bSize, best)
	}
	return best, bestCost
}

// searchBlock picks the modes of an intra block and leaves its
// reconstruction in place, returning the choice and its RD cost.
func (e *Encoder) searchBlock(r, c, size int) (blkChoice, float64) {
	t := e.t
	s0 := e.snap()
	defer e.unsnap()
	e.save(s0, r, c, num4x4W[size], num4x4H[size])
	best := blkChoice{yMode: dcPred, uvMode: dcPred}
	bestCost := math.Inf(1)
	try := func(ch blkChoice) {
		e.load(s0)
		cost := e.trial(r, c, size, &ch)
		if cost < bestCost {
			best, bestCost = ch, cost
		}
	}
	if !t.h.intra {
		inter := append(e.interList[:0], e.interCandidates(r, c, size, lastFrame)...)
		e.interList = inter
		for _, ch := range inter {
			try(ch)
		}
		e.load(s0)
	}
	bestInter := best
	t.initBlock(r, c, size)
	cands := e.rankLumaModes(r, c, size)
	intraBest := math.Inf(1)
	for _, m := range cands {
		ch := blkChoice{yMode: m.mode, angleY: m.angle, uvMode: dcPred}
		e.load(s0)
		if cost := e.trial(r, c, size, &ch); cost < intraBest {
			intraBest = cost
			if cost < bestCost {
				best, bestCost = ch, cost
			}
		}
	}
	if best.inter {
		e.load(s0)
		ch := best
		cost := e.trial(r, c, size, &ch)
		return ch, cost
	}
	_ = bestInter
	// Chroma: the luma choice with each chroma candidate.
	if t.b.hasChroma || e.chromaCodedBy(r, c, size) {
		y := best
		for _, uv := range e.rankChromaModes(r, c, size, &y) {
			ch := y
			ch.uvMode, ch.angleUV, ch.cflU, ch.cflV = uv.mode, uv.angle, uv.cflU, uv.cflV
			if ch.uvMode == y.uvMode && ch.angleUV == y.angleUV && ch.cflU == y.cflU && ch.cflV == y.cflV {
				continue
			}
			try(ch)
		}
	}
	// Transform depth.
	if maxTxDepth[size] > 0 && t.h.txMode == txModeSelect {
		base := best
		for d := 1; d <= min(maxTxDepth[size], 2); d++ {
			ch := base
			ch.txDepth = d
			try(ch)
		}
	}
	e.load(s0)
	ch := best
	cost := e.trial(r, c, size, &ch)
	return ch, cost
}

// trial codes the block with the sink estimating and returns its RD
// cost.
func (e *Encoder) trial(r, c, size int, ch *blkChoice) float64 {
	if ch.inter {
		return e.trialInter(r, c, size, ch)
	}
	return e.trialIntra(r, c, size, ch)
}

// chromaCodedBy reports whether a block without its own chroma is the
// one whose chroma is coded (never: chroma goes with the last block).
func (e *Encoder) chromaCodedBy(r, c, size int) bool { return false }

// trialIntra codes the block with the sink estimating and returns its RD
// cost; ch.skip is set when no coefficient is coded.
func (e *Encoder) trialIntra(r, c, size int, ch *blkChoice) float64 {
	k := &e.sink
	ch.skip = false
	before := k.cost
	e.nonZero = false
	dist := e.codeIntraBlock(r, c, size, ch)
	coefBits := k.cost - before
	if !e.nonZero {
		ch.skip = true
		coefBits = 0
		e.t.b.skip = true
		e.t.storeModeInfo()
	}
	k.cost = before
	e.writeIntraModeInfo(ch.txDepth)
	modeBits := k.cost - before
	k.cost = before
	return float64(dist) + e.lambda*float64(coefBits+modeBits)/256
}

type modeCand struct {
	mode, angle int
	cflU, cflV  int
	score       float64
}

// rankLumaModes predicts the block with every luma mode and angle delta
// and returns the best few by SATD plus estimated mode bits.
func (e *Encoder) rankLumaModes(r, c, size int) []modeCand {
	t := e.t
	b := &t.b
	if blockW(size) > 64 || blockH(size) > 64 {
		return []modeCand{{mode: dcPred}}
	}
	modeCDF := t.cdf.yMode[sizeGroup[size]][:]
	if t.h.intra {
		above, left := dcPred, dcPred
		if b.availU {
			above = int(t.mi(r-1, c).yMode)
		}
		if b.availL {
			left = int(t.mi(r, c-1).yMode)
		}
		modeCDF = t.cdf.kfYMode[intraModeContext[above]][intraModeContext[left]][:]
	}
	cands := e.lumaBuf[:0]
	for mode := dcPred; mode <= paethPred; mode++ {
		for angle := -3; angle <= 3; angle++ {
			if angle != 0 && (!isDirectional(mode) || size < block8x8) {
				continue
			}
			bits := symCost(modeCDF, mode)
			if size >= block8x8 && isDirectional(mode) {
				bits += symCost(t.cdf.angleDelta[mode-vPred][:], angle+3)
			}
			b.angleDeltaY = angle
			sad := e.predSATD(0, r, c, size, mode)
			cands = append(cands, modeCand{mode: mode, angle: angle, score: float64(sad) + e.sqrtLambda*float64(bits)/256})
		}
	}
	b.angleDeltaY = 0
	e.lumaBuf = cands
	return topCands(cands, e.lumaCands)
}

// rankChromaModes ranks chroma modes for a block whose luma is decided
// (and reconstructed, for CfL).
func (e *Encoder) rankChromaModes(r, c, size int, y *blkChoice) []modeCand {
	t := e.t
	b := &t.b
	cflAllowed := !b.lossless && max(blockW(size), blockH(size)) <= 32
	cands := e.chromaBuf[:0]
	n, cdf := 13, t.cdf.uvMode[0][y.yMode][:]
	if cflAllowed {
		n, cdf = 14, t.cdf.uvMode[1][y.yMode][:]
	}
	b.angleDeltaUV = 0
	for mode := dcPred; mode < n; mode++ {
		if mode == uvCflPred {
			continue
		}
		bits := symCost(cdf, mode)
		sad := e.predSATD(1, r, c, size, mode) + e.predSATD(2, r, c, size, mode)
		cands = append(cands, modeCand{mode: mode, score: float64(sad) + e.sqrtLambda*float64(bits)/256})
	}
	e.chromaBuf = cands
	out := append(e.chromaOut[:0], topCands(cands, e.chromaCands)...)
	defer func() { e.chromaOut = out }()
	if cflAllowed {
		for _, a := range e.cflAlphas() {
			out = append(out, modeCand{mode: uvCflPred, cflU: a[0], cflV: a[1]})
		}
	}
	return out
}

// cflAlphas are the CfL alpha pairs tried.
func (e *Encoder) cflAlphas() [][2]int {
	return [][2]int{{-2, -2}, {-2, 2}, {2, -2}, {2, 2}, {-4, 4}, {4, -4}, {1, 1}, {-1, -1}}
}

func topCands(c []modeCand, n int) []modeCand {
	for i := 1; i < len(c); i++ {
		for j := i; j > 0 && c[j].score < c[j-1].score; j-- {
			c[j], c[j-1] = c[j-1], c[j]
		}
	}
	if len(c) > n {
		c = c[:n]
	}
	return c
}

// predSATD predicts the block's plane with mode (as one transform block)
// and returns the SATD against the source.
func (e *Encoder) predSATD(plane, r, c, size int, mode int) int {
	t := e.t
	b := &t.b
	f := t.f
	ss := min(plane, 1)
	psz := size
	if plane > 0 {
		psz = chromaSize[size]
	}
	w, h := blockW(psz), blockH(psz)
	x, y := (c>>ss)*4, (r>>ss)*4
	sbRow, sbCol := ((y<<ss)>>2)&15, ((x<<ss)>>2)&15
	availL, availU := b.availL, b.availU
	if plane > 0 {
		availL, availU = b.availLC, b.availUC
	}
	bd := &t.blockDecoded[plane]
	stepX, stepY := w>>2, h>>2
	haveAboveRt := bd[(sbRow>>ss)-1+1][(sbCol>>ss)+stepX+1]
	haveBelowLt := bd[(sbRow>>ss)+stepY+1][(sbCol>>ss)-1+1]
	lw, lh := txWLog2[txSizeOf(w, h)], txHLog2[txSizeOf(w, h)]
	t.predictIntra(plane, x, y, availL, availU, haveAboveRt, haveBelowLt, mode, lw, lh)
	pb := &f.cur.planes[plane]
	return satd(e.src[plane][y*e.srcStride[plane]+x:], e.srcStride[plane], pb.pix8[y*pb.stride+x:], pb.stride, w, h)
}

// satd is the sum of absolute 4x4 Hadamard-transformed differences.
func satd(a []byte, as int, b []byte, bs int, w, h int) int {
	sum := 0
	var d [16]int
	for y := 0; y < h; y += 4 {
		for x := 0; x < w; x += 4 {
			for i := 0; i < 4; i++ {
				ra := a[(y+i)*as+x:]
				rb := b[(y+i)*bs+x:]
				for j := 0; j < 4; j++ {
					d[i*4+j] = int(ra[j]) - int(rb[j])
				}
			}
			for i := 0; i < 4; i++ {
				r := d[i*4 : i*4+4]
				s0, s1, d0, d1 := r[0]+r[1], r[2]+r[3], r[0]-r[1], r[2]-r[3]
				r[0], r[1], r[2], r[3] = s0+s1, s0-s1, d0+d1, d0-d1
			}
			for j := 0; j < 4; j++ {
				s0, s1 := d[j]+d[4+j], d[8+j]+d[12+j]
				d0, d1 := d[j]-d[4+j], d[8+j]-d[12+j]
				sum += abs(s0+s1) + abs(s0-s1) + abs(d0+d1) + abs(d0-d1)
			}
		}
	}
	return sum >> 1
}
