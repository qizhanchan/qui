package h264

// H.264 deblocking filter (spec clause 8.7). This implements the intra-only
// case: for an all-intra picture every edge is an intra edge, so the boundary
// strength is 4 on macroblock boundaries and 3 on internal transform-block
// edges (Table 8-16). Inter-coded pictures (P/B slices) will need the full
// boundary-strength derivation on top of these primitives.

// alphaTable / betaTable / tc0Table are Tables 8-16..8-18. They are indexed by
// qp + 52 + offset; the first 52 entries are the "index < 0" clamp and the
// last 52 the "index > 51" clamp.
var alphaTable = [156]int{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 4, 4, 5, 6,
	7, 8, 9, 10, 12, 13, 15, 17, 20, 22,
	25, 28, 32, 36, 40, 45, 50, 56, 63, 71,
	80, 90, 101, 113, 127, 144, 162, 182, 203, 226,
	255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
}

var betaTable = [156]int{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 2, 2, 2, 3,
	3, 3, 3, 4, 4, 4, 6, 6, 7, 7,
	8, 8, 9, 9, 10, 10, 11, 11, 12, 12,
	13, 13, 14, 14, 15, 15, 16, 16, 17, 17,
	18, 18,
	18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18,
	18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18,
	18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18,
	18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18,
}

// tc0Table[indexA][bS]; column 0 is bS=0 (always -1), columns 1..3 are bS 1..3.
var tc0Table = [156][4]int{
	{-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0},
	{-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0},
	{-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0},
	{-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0},
	{-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0},
	{-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0},
	{-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0},
	{-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0},
	{-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0},
	{-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0},
	{-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0},
	{-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 1},
	{-1, 0, 0, 1}, {-1, 0, 0, 1}, {-1, 0, 0, 1}, {-1, 0, 1, 1}, {-1, 0, 1, 1}, {-1, 1, 1, 1},
	{-1, 1, 1, 1}, {-1, 1, 1, 1}, {-1, 1, 1, 1}, {-1, 1, 1, 2}, {-1, 1, 1, 2}, {-1, 1, 1, 2},
	{-1, 1, 1, 2}, {-1, 1, 2, 3}, {-1, 1, 2, 3}, {-1, 2, 2, 3}, {-1, 2, 2, 4}, {-1, 2, 3, 4},
	{-1, 2, 3, 4}, {-1, 3, 3, 5}, {-1, 3, 4, 6}, {-1, 3, 4, 6}, {-1, 4, 5, 7}, {-1, 4, 5, 8},
	{-1, 4, 6, 9}, {-1, 5, 7, 10}, {-1, 6, 8, 11}, {-1, 6, 8, 13}, {-1, 7, 10, 14}, {-1, 8, 11, 16},
	{-1, 9, 12, 18}, {-1, 10, 13, 20}, {-1, 11, 15, 23}, {-1, 13, 17, 25},
	{-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25},
	{-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25},
	{-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25},
	{-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25},
	{-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25},
	{-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25},
	{-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25},
}

// rasterToZScan maps a 4x4 block raster index to its z-scan (quadrant) index.
var rasterToZScan = [16]int{0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15}

// blockInfo returns the deblocking inputs of the 4x4 block at (px, py) in the
// picture: whether it has non-zero coefficients, its reference index and motion
// vector, and whether it is intra-coded.
func (s *picState) blockInfo(px, py int) (nz bool, ref, mvx, mvy int, intra bool) {
	stride := s.mbWidth * 4
	idx := py*stride + px
	ref = s.refLX[idx]
	mvx, mvy = s.mvLX[idx], s.mvLY[idx]
	intra = ref < 0
	if intra {
		return
	}
	mbIdx := (py/4)*s.mbWidth + px/4
	mb := &s.mbs[mbIdx]
	blk := rasterToZScan[(py%4)*4+(px%4)]
	if mb.transform8x8 {
		nz = mb.lumaLevel8x8[blk/4] != [64]int32{}
	} else {
		nz = mb.nzLuma[blk] != 0
	}
	return
}

// bsInter derives the boundary strength for an inter edge (spec 8.7.2.3). It
// matches OpenH264's BS_EDGE macro: bS = 2 when either side has non-zero
// coefficients, 1 when the reference index or motion vector differs, else 0.
func bsInter(pNz, qNz bool, pRef, qRef, pMvx, pMvy, qMvx, qMvy int) int {
	if pNz || qNz {
		return 2
	}
	if pRef != qRef || absInt(pMvx-qMvx) >= 4 || absInt(pMvy-qMvy) >= 4 {
		return 1
	}
	return 0
}

func clipInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clipPixel(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// sliceDeblock holds the per-slice deblocking parameters.
type sliceDeblock struct {
	disableIDC  uint32
	alphaOffset int // 52 + slice_alpha_c0_offset_div2*2
	betaOffset  int // 52 + slice_beta_offset_div2*2
}

// edgeBSV returns the four boundary strengths of a vertical edge between the
// 4x4 columns px-1 and px over block rows by0..by0+3 (inter edges only).
func (s *picState) edgeBSV(px, by0 int) [4]int {
	var bs [4]int
	for k := 0; k < 4; k++ {
		py := by0 + k
		pNz, pRef, pmx, pmy, _ := s.blockInfo(px-1, py)
		qNz, qRef, qmx, qmy, _ := s.blockInfo(px, py)
		bs[k] = bsInter(pNz, qNz, pRef, qRef, pmx, pmy, qmx, qmy)
	}
	return bs
}

// edgeBSH returns the four boundary strengths of a horizontal edge between the
// 4x4 rows py-1 and py over block columns bx0..bx0+3 (inter edges only).
func (s *picState) edgeBSH(py, bx0 int) [4]int {
	var bs [4]int
	for k := 0; k < 4; k++ {
		px := bx0 + k
		pNz, pRef, pmx, pmy, _ := s.blockInfo(px, py-1)
		qNz, qRef, qmx, qmy, _ := s.blockInfo(px, py)
		bs[k] = bsInter(pNz, qNz, pRef, qRef, pmx, pmy, qmx, qmy)
	}
	return bs
}

// deblock applies the deblocking filter to the whole (coded-size) frame. Intra
// edges use the strong (bS=4) macroblock-edge filter or the bS=3 normal filter;
// inter edges use the full boundary-strength derivation (spec 8.7.2.3).
func (s *picState) deblock(f *Frame) {
	if forceNoDeblock {
		return
	}
	for mbIdx := 0; mbIdx < len(s.mbs); mbIdx++ {
		mb := &s.mbs[mbIdx]
		if mb.sliceNum <= 0 || mb.sliceNum > len(s.slices) {
			continue // macroblock not decoded (e.g. an error-concealed gap)
		}
		sp := s.slices[mb.sliceNum-1]
		if sp.disableIDC == 1 {
			continue
		}
		mbX, mbY := mbIdx%s.mbWidth, mbIdx/s.mbWidth
		a, b := sp.alphaOffset, sp.betaOffset
		qp := mb.qpY
		is8x8 := mb.transform8x8
		intra := !mb.isInter

		hasLeft := mbX > 0
		hasTop := mbY > 0
		if sp.disableIDC == 2 {
			hasLeft = s.mbAvailA(mbIdx) != nil
			hasTop = s.mbAvailB(mbIdx) != nil
		}

		x0, y0 := mbX*16, mbY*16
		cx0, cy0 := mbX*8, mbY*8
		qpc := chromaQP(qp + s.chromaQpIndexOffset)
		bx0, by0 := mbX*4, mbY*4
		strong := [4]int{3, 3, 3, 3} // bS = 3 for internal edges of an intra MB

		// Vertical edges.
		if hasLeft {
			left := &s.mbs[mbIdx-1]
			qpLeft := left.qpY
			qpAvg := (qp + qpLeft + 1) >> 1
			qpcAvg := (qpc + chromaQP(qpLeft+s.chromaQpIndexOffset) + 1) >> 1
			if intra || !left.isInter {
				filterLumaIntraV(f.Y, x0, y0, qpAvg+a, qpAvg+b)
				filterChromaIntraV(f.Cb, cx0, cy0, qpcAvg+a, qpcAvg+b)
				filterChromaIntraV(f.Cr, cx0, cy0, qpcAvg+a, qpcAvg+b)
			} else {
				bs := s.edgeBSV(bx0, by0)
				filterLumaNormalV(f.Y, x0, y0, qpAvg+a, qpAvg+b, bs)
				filterChromaNormalV(f.Cb, cx0, cy0, qpcAvg+a, qpcAvg+b, bs)
				filterChromaNormalV(f.Cr, cx0, cy0, qpcAvg+a, qpcAvg+b, bs)
			}
		}
		// Internal vertical edges.
		if intra {
			if is8x8 {
				filterLumaNormalV(f.Y, x0+8, y0, qp+a, qp+b, strong)
			} else {
				filterLumaNormalV(f.Y, x0+4, y0, qp+a, qp+b, strong)
				filterLumaNormalV(f.Y, x0+8, y0, qp+a, qp+b, strong)
				filterLumaNormalV(f.Y, x0+12, y0, qp+a, qp+b, strong)
			}
			filterChromaNormalV(f.Cb, cx0+4, cy0, qpc+a, qpc+b, strong)
			filterChromaNormalV(f.Cr, cx0+4, cy0, qpc+a, qpc+b, strong)
		} else {
			bsMid := s.edgeBSV(bx0+2, by0) // luma x = 8 (chroma x = 4) edge
			if is8x8 {
				filterLumaNormalV(f.Y, x0+8, y0, qp+a, qp+b, bsMid)
			} else {
				filterLumaNormalV(f.Y, x0+4, y0, qp+a, qp+b, s.edgeBSV(bx0+1, by0))
				filterLumaNormalV(f.Y, x0+8, y0, qp+a, qp+b, bsMid)
				filterLumaNormalV(f.Y, x0+12, y0, qp+a, qp+b, s.edgeBSV(bx0+3, by0))
			}
			filterChromaNormalV(f.Cb, cx0+4, cy0, qpc+a, qpc+b, bsMid)
			filterChromaNormalV(f.Cr, cx0+4, cy0, qpc+a, qpc+b, bsMid)
		}

		// Horizontal edges.
		if hasTop {
			top := &s.mbs[mbIdx-s.mbWidth]
			qpTop := top.qpY
			qpAvg := (qp + qpTop + 1) >> 1
			qpcAvg := (qpc + chromaQP(qpTop+s.chromaQpIndexOffset) + 1) >> 1
			if intra || !top.isInter {
				filterLumaIntraH(f.Y, x0, y0, qpAvg+a, qpAvg+b)
				filterChromaIntraH(f.Cb, cx0, cy0, qpcAvg+a, qpcAvg+b)
				filterChromaIntraH(f.Cr, cx0, cy0, qpcAvg+a, qpcAvg+b)
			} else {
				bs := s.edgeBSH(by0, bx0)
				filterLumaNormalH(f.Y, x0, y0, qpAvg+a, qpAvg+b, bs)
				filterChromaNormalH(f.Cb, cx0, cy0, qpcAvg+a, qpcAvg+b, bs)
				filterChromaNormalH(f.Cr, cx0, cy0, qpcAvg+a, qpcAvg+b, bs)
			}
		}
		// Internal horizontal edges.
		if intra {
			if is8x8 {
				filterLumaNormalH(f.Y, x0, y0+8, qp+a, qp+b, strong)
			} else {
				filterLumaNormalH(f.Y, x0, y0+4, qp+a, qp+b, strong)
				filterLumaNormalH(f.Y, x0, y0+8, qp+a, qp+b, strong)
				filterLumaNormalH(f.Y, x0, y0+12, qp+a, qp+b, strong)
			}
			filterChromaNormalH(f.Cb, cx0, cy0+4, qpc+a, qpc+b, strong)
			filterChromaNormalH(f.Cr, cx0, cy0+4, qpc+a, qpc+b, strong)
		} else {
			bsMid := s.edgeBSH(by0+2, bx0) // luma y = 8 (chroma y = 4) edge
			if is8x8 {
				filterLumaNormalH(f.Y, x0, y0+8, qp+a, qp+b, bsMid)
			} else {
				filterLumaNormalH(f.Y, x0, y0+4, qp+a, qp+b, s.edgeBSH(by0+1, bx0))
				filterLumaNormalH(f.Y, x0, y0+8, qp+a, qp+b, bsMid)
				filterLumaNormalH(f.Y, x0, y0+12, qp+a, qp+b, s.edgeBSH(by0+3, bx0))
			}
			filterChromaNormalH(f.Cb, cx0, cy0+4, qpc+a, qpc+b, bsMid)
			filterChromaNormalH(f.Cr, cx0, cy0+4, qpc+a, qpc+b, bsMid)
		}
	}
}

// filterLumaIntraV filters a vertical luma edge with the strong intra filter.
// edgeX is the x of q0; 16 rows from y0.
func filterLumaIntraV(p *Plane, edgeX, y0, indexA, indexB int) {
	alpha, beta := alphaTable[indexA], betaTable[indexB]
	if alpha == 0 || beta == 0 {
		return
	}
	plane, stride := p.Pix, p.Stride
	for d := 0; d < 16; d++ {
		off := (y0+d)*stride + edgeX
		p0, p1, p2 := int(plane[off-1]), int(plane[off-2]), int(plane[off-3])
		q0, q1, q2 := int(plane[off]), int(plane[off+1]), int(plane[off+2])
		if absInt(p0-q0) >= alpha || absInt(p1-p0) >= beta || absInt(q1-q0) >= beta {
			continue
		}
		if absInt(p0-q0) < ((alpha >> 2) + 2) {
			if absInt(p2-p0) < beta {
				p3 := int(plane[off-4])
				plane[off-1] = byte((p2 + 2*p1 + 2*p0 + 2*q0 + q1 + 4) >> 3)
				plane[off-2] = byte((p2 + p1 + p0 + q0 + 2) >> 2)
				plane[off-3] = byte((2*p3 + 3*p2 + p1 + p0 + q0 + 4) >> 3)
			} else {
				plane[off-1] = byte((2*p1 + p0 + q1 + 2) >> 2)
			}
			if absInt(q2-q0) < beta {
				q3 := int(plane[off+3])
				plane[off] = byte((p1 + 2*p0 + 2*q0 + 2*q1 + q2 + 4) >> 3)
				plane[off+1] = byte((p0 + q0 + q1 + q2 + 2) >> 2)
				plane[off+2] = byte((2*q3 + 3*q2 + q1 + q0 + p0 + 4) >> 3)
			} else {
				plane[off] = byte((2*q1 + q0 + p1 + 2) >> 2)
			}
		} else {
			plane[off-1] = byte((2*p1 + p0 + q1 + 2) >> 2)
			plane[off] = byte((2*q1 + q0 + p1 + 2) >> 2)
		}
	}
}

// filterLumaIntraH filters a horizontal luma edge with the strong intra filter.
func filterLumaIntraH(p *Plane, x0, edgeY, indexA, indexB int) {
	alpha, beta := alphaTable[indexA], betaTable[indexB]
	if alpha == 0 || beta == 0 {
		return
	}
	plane, stride := p.Pix, p.Stride
	for d := 0; d < 16; d++ {
		x := x0 + d
		p0 := int(plane[(edgeY-1)*stride+x])
		p1 := int(plane[(edgeY-2)*stride+x])
		p2 := int(plane[(edgeY-3)*stride+x])
		q0 := int(plane[edgeY*stride+x])
		q1 := int(plane[(edgeY+1)*stride+x])
		q2 := int(plane[(edgeY+2)*stride+x])
		if absInt(p0-q0) >= alpha || absInt(p1-p0) >= beta || absInt(q1-q0) >= beta {
			continue
		}
		if absInt(p0-q0) < ((alpha >> 2) + 2) {
			if absInt(p2-p0) < beta {
				p3 := int(plane[(edgeY-4)*stride+x])
				plane[(edgeY-1)*stride+x] = byte((p2 + 2*p1 + 2*p0 + 2*q0 + q1 + 4) >> 3)
				plane[(edgeY-2)*stride+x] = byte((p2 + p1 + p0 + q0 + 2) >> 2)
				plane[(edgeY-3)*stride+x] = byte((2*p3 + 3*p2 + p1 + p0 + q0 + 4) >> 3)
			} else {
				plane[(edgeY-1)*stride+x] = byte((2*p1 + p0 + q1 + 2) >> 2)
			}
			if absInt(q2-q0) < beta {
				q3 := int(plane[(edgeY+3)*stride+x])
				plane[edgeY*stride+x] = byte((p1 + 2*p0 + 2*q0 + 2*q1 + q2 + 4) >> 3)
				plane[(edgeY+1)*stride+x] = byte((p0 + q0 + q1 + q2 + 2) >> 2)
				plane[(edgeY+2)*stride+x] = byte((2*q3 + 3*q2 + q1 + q0 + p0 + 4) >> 3)
			} else {
				plane[edgeY*stride+x] = byte((2*q1 + q0 + p1 + 2) >> 2)
			}
		} else {
			plane[(edgeY-1)*stride+x] = byte((2*p1 + p0 + q1 + 2) >> 2)
			plane[edgeY*stride+x] = byte((2*q1 + q0 + p1 + 2) >> 2)
		}
	}
}

// filterLumaNormalV filters a vertical luma edge with the normal filter. bs
// holds the boundary strength of each of the four 4x4-block segments; a segment
// with bS 0 is left untouched.
func filterLumaNormalV(p *Plane, edgeX, y0, indexA, indexB int, bs [4]int) {
	alpha, beta := alphaTable[indexA], betaTable[indexB]
	if alpha == 0 || beta == 0 {
		return
	}
	plane, stride := p.Pix, p.Stride
	for d := 0; d < 16; d++ {
		tcOrig := tc0Table[indexA][bs[d/4]]
		if tcOrig < 0 {
			continue
		}
		off := (y0+d)*stride + edgeX
		p0, p1, p2 := int(plane[off-1]), int(plane[off-2]), int(plane[off-3])
		q0, q1, q2 := int(plane[off]), int(plane[off+1]), int(plane[off+2])
		if absInt(p0-q0) >= alpha || absInt(p1-p0) >= beta || absInt(q1-q0) >= beta {
			continue
		}
		tc := tcOrig
		if absInt(p2-p0) < beta {
			if tcOrig > 0 {
				plane[off-2] = clipPixel(p1 + clipInt(((p2+((p0+q0+1)>>1))>>1)-p1, -tcOrig, tcOrig))
			}
			tc++
		}
		if absInt(q2-q0) < beta {
			if tcOrig > 0 {
				plane[off+1] = clipPixel(q1 + clipInt(((q2+((p0+q0+1)>>1))>>1)-q1, -tcOrig, tcOrig))
			}
			tc++
		}
		delta := clipInt(((q0-p0)*4+(p1-q1)+4)>>3, -tc, tc)
		plane[off-1] = clipPixel(p0 + delta)
		plane[off] = clipPixel(q0 - delta)
	}
}

// filterLumaNormalH filters a horizontal luma edge with the normal filter.
func filterLumaNormalH(p *Plane, x0, edgeY, indexA, indexB int, bs [4]int) {
	alpha, beta := alphaTable[indexA], betaTable[indexB]
	if alpha == 0 || beta == 0 {
		return
	}
	plane, stride := p.Pix, p.Stride
	for d := 0; d < 16; d++ {
		tcOrig := tc0Table[indexA][bs[d/4]]
		if tcOrig < 0 {
			continue
		}
		x := x0 + d
		p0 := int(plane[(edgeY-1)*stride+x])
		p1 := int(plane[(edgeY-2)*stride+x])
		p2 := int(plane[(edgeY-3)*stride+x])
		q0 := int(plane[edgeY*stride+x])
		q1 := int(plane[(edgeY+1)*stride+x])
		q2 := int(plane[(edgeY+2)*stride+x])
		if absInt(p0-q0) >= alpha || absInt(p1-p0) >= beta || absInt(q1-q0) >= beta {
			continue
		}
		tc := tcOrig
		if absInt(p2-p0) < beta {
			if tcOrig > 0 {
				plane[(edgeY-2)*stride+x] = clipPixel(p1 + clipInt(((p2+((p0+q0+1)>>1))>>1)-p1, -tcOrig, tcOrig))
			}
			tc++
		}
		if absInt(q2-q0) < beta {
			if tcOrig > 0 {
				plane[(edgeY+1)*stride+x] = clipPixel(q1 + clipInt(((q2+((p0+q0+1)>>1))>>1)-q1, -tcOrig, tcOrig))
			}
			tc++
		}
		delta := clipInt(((q0-p0)*4+(p1-q1)+4)>>3, -tc, tc)
		plane[(edgeY-1)*stride+x] = clipPixel(p0 + delta)
		plane[edgeY*stride+x] = clipPixel(q0 - delta)
	}
}

// filterChromaIntraV filters a vertical chroma edge with the strong intra filter.
func filterChromaIntraV(p *Plane, edgeX, y0, indexA, indexB int) {
	alpha, beta := alphaTable[indexA], betaTable[indexB]
	if alpha == 0 || beta == 0 {
		return
	}
	plane, stride := p.Pix, p.Stride
	for d := 0; d < 8; d++ {
		off := (y0+d)*stride + edgeX
		p0, p1 := int(plane[off-1]), int(plane[off-2])
		q0, q1 := int(plane[off]), int(plane[off+1])
		if absInt(p0-q0) >= alpha || absInt(p1-p0) >= beta || absInt(q1-q0) >= beta {
			continue
		}
		plane[off-1] = byte((2*p1 + p0 + q1 + 2) >> 2)
		plane[off] = byte((2*q1 + q0 + p1 + 2) >> 2)
	}
}

// filterChromaIntraH filters a horizontal chroma edge with the strong intra filter.
func filterChromaIntraH(p *Plane, x0, edgeY, indexA, indexB int) {
	alpha, beta := alphaTable[indexA], betaTable[indexB]
	if alpha == 0 || beta == 0 {
		return
	}
	plane, stride := p.Pix, p.Stride
	for d := 0; d < 8; d++ {
		x := x0 + d
		p0 := int(plane[(edgeY-1)*stride+x])
		p1 := int(plane[(edgeY-2)*stride+x])
		q0 := int(plane[edgeY*stride+x])
		q1 := int(plane[(edgeY+1)*stride+x])
		if absInt(p0-q0) >= alpha || absInt(p1-p0) >= beta || absInt(q1-q0) >= beta {
			continue
		}
		plane[(edgeY-1)*stride+x] = byte((2*p1 + p0 + q1 + 2) >> 2)
		plane[edgeY*stride+x] = byte((2*q1 + q0 + p1 + 2) >> 2)
	}
}

// filterChromaNormalV filters a vertical chroma edge with the normal filter.
// Each of the four bs entries covers two chroma rows (one luma 4x4 block row).
func filterChromaNormalV(p *Plane, edgeX, y0, indexA, indexB int, bs [4]int) {
	alpha, beta := alphaTable[indexA], betaTable[indexB]
	if alpha == 0 || beta == 0 {
		return
	}
	plane, stride := p.Pix, p.Stride
	for d := 0; d < 8; d++ {
		tc := tc0Table[indexA][bs[d/2]] + 1
		if tc <= 0 {
			continue
		}
		off := (y0+d)*stride + edgeX
		p0, p1 := int(plane[off-1]), int(plane[off-2])
		q0, q1 := int(plane[off]), int(plane[off+1])
		if absInt(p0-q0) >= alpha || absInt(p1-p0) >= beta || absInt(q1-q0) >= beta {
			continue
		}
		delta := clipInt(((q0-p0)*4+(p1-q1)+4)>>3, -tc, tc)
		plane[off-1] = clipPixel(p0 + delta)
		plane[off] = clipPixel(q0 - delta)
	}
}

// filterChromaNormalH filters a horizontal chroma edge with the normal filter.
func filterChromaNormalH(p *Plane, x0, edgeY, indexA, indexB int, bs [4]int) {
	alpha, beta := alphaTable[indexA], betaTable[indexB]
	if alpha == 0 || beta == 0 {
		return
	}
	plane, stride := p.Pix, p.Stride
	for d := 0; d < 8; d++ {
		tc := tc0Table[indexA][bs[d/2]] + 1
		if tc <= 0 {
			continue
		}
		x := x0 + d
		p0 := int(plane[(edgeY-1)*stride+x])
		p1 := int(plane[(edgeY-2)*stride+x])
		q0 := int(plane[edgeY*stride+x])
		q1 := int(plane[(edgeY+1)*stride+x])
		if absInt(p0-q0) >= alpha || absInt(p1-p0) >= beta || absInt(q1-q0) >= beta {
			continue
		}
		delta := clipInt(((q0-p0)*4+(p1-q1)+4)>>3, -tc, tc)
		plane[(edgeY-1)*stride+x] = clipPixel(p0 + delta)
		plane[edgeY*stride+x] = clipPixel(q0 - delta)
	}
}

var forceNoDeblock = false
