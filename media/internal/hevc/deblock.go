package hevc

// Deblocking filter (spec 8.7.2), applied to the whole picture: all
// vertical edges first, then all horizontal edges.

var betaTable = [52]int{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 20, 22, 24,
	26, 28, 30, 32, 34, 36, 38, 40, 42, 44, 46, 48, 50, 52, 54, 56, 58, 60, 62, 64,
}

var tcTable = [54]int{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2,
	3, 3, 3, 3, 4, 4, 4, 5, 5, 6, 6, 7, 8, 9, 10, 11, 13, 14, 16, 18, 20, 22, 24,
}

// edgeBS returns the boundary strength between 4x4 blocks p and q, or 0
// when the edge is not filtered. vertical selects the edge direction.
func (d *Decoder) edgeBS(pic *picture, xq, yq int, vertical bool) int {
	fc := &d.fc
	q := &fc.blk[(yq>>2)*fc.w4+xq>>2]
	xp, yp := xq, yq-1
	isEdge, tuEdge := q.edgeH, q.tuEdgeH
	if vertical {
		xp, yp = xq-1, yq
		isEdge, tuEdge = q.edgeV, q.tuEdgeV
	}
	if !isEdge {
		return 0
	}
	sp := d.active
	cq := (yq>>sp.Log2Ctb)*sp.PicWCtb + xq>>sp.Log2Ctb
	cp := (yp>>sp.Log2Ctb)*sp.PicWCtb + xp>>sp.Log2Ctb
	hq := fc.slices[fc.ctbSlice[cq]]
	if hq.deblockDisabled {
		return 0
	}
	if cp != cq {
		if fc.ctbSliceAddr[cp] != fc.ctbSliceAddr[cq] && !hq.lfAcrossSlices {
			return 0
		}
		pps := hq.pps
		if !pps.LoopFilterAcrossTiles && pps.tileID[pps.ctbRsToTs[cp]] != pps.tileID[pps.ctbRsToTs[cq]] {
			return 0
		}
	}
	p := &fc.blk[(yp>>2)*fc.w4+xp>>2]
	if p.predMode == modeIntra || q.predMode == modeIntra {
		return 2
	}
	if tuEdge && (p.cbfY || q.cbfY) {
		return 1
	}
	ip := (yp>>2)*pic.w4 + xp>>2
	iq := (yq>>2)*pic.w4 + xq>>2
	return motionBS(pic, ip, iq)
}

func mvFar(a, b int32) bool {
	ax, ay := a>>16, int32(int16(a))
	bx, by := b>>16, int32(int16(b))
	dx, dy := ax-bx, ay-by
	return dx >= 4 || dx <= -4 || dy >= 4 || dy <= -4
}

// motionBS derives bS 0/1 from the motion of 4x4 blocks i (p) and j (q).
func motionBS(pic *picture, i, j int) int {
	pr0, pr1 := pic.refIdx[0][i] >= 0, pic.refIdx[1][i] >= 0
	qr0, qr1 := pic.refIdx[0][j] >= 0, pic.refIdx[1][j] >= 0
	np, nq := boolInt(pr0)+boolInt(pr1), boolInt(qr0)+boolInt(qr1)
	if np != nq {
		return 1
	}
	if np == 1 {
		lp, lq := 0, 0
		if !pr0 {
			lp = 1
		}
		if !qr0 {
			lq = 1
		}
		if pic.refPOC[lp][i] != pic.refPOC[lq][j] || mvFar(pic.mv[lp][i], pic.mv[lq][j]) {
			return 1
		}
		return 0
	}
	p0, p1 := pic.refPOC[0][i], pic.refPOC[1][i]
	q0, q1 := pic.refPOC[0][j], pic.refPOC[1][j]
	if !((p0 == q0 && p1 == q1) || (p0 == q1 && p1 == q0)) {
		return 1
	}
	mp0, mp1 := pic.mv[0][i], pic.mv[1][i]
	mq0, mq1 := pic.mv[0][j], pic.mv[1][j]
	if p0 != p1 {
		if p0 == q0 {
			if mvFar(mp0, mq0) || mvFar(mp1, mq1) {
				return 1
			}
			return 0
		}
		if mvFar(mp0, mq1) || mvFar(mp1, mq0) {
			return 1
		}
		return 0
	}
	if (mvFar(mp0, mq0) || mvFar(mp1, mq1)) && (mvFar(mp0, mq1) || mvFar(mp1, mq0)) {
		return 1
	}
	return 0
}

func (d *Decoder) deblock(pic *picture) {
	sp := d.active
	fc := &d.fc
	W, H := sp.Width, sp.Height
	for dir := 0; dir < 2; dir++ {
		vertical := dir == 0
		// Luma.
		for y := 0; y < H; y += 4 {
			for x := 0; x < W; x += 4 {
				if vertical && (x == 0 || x&7 != 0) || !vertical && (y == 0 || y&7 != 0) {
					continue
				}
				bs := d.edgeBS(pic, x, y, vertical)
				if bs == 0 {
					continue
				}
				q := &fc.blk[(y>>2)*fc.w4+x>>2]
				var p *blkInfo
				if vertical {
					p = &fc.blk[(y>>2)*fc.w4+(x-4)>>2]
				} else {
					p = &fc.blk[((y-4)>>2)*fc.w4+x>>2]
				}
				h := d.sliceAt(x, y)
				qpL := (int(p.qp) + int(q.qp) + 1) >> 1
				beta := betaTable[clip3(0, 51, qpL+h.betaOffset)]
				tc := tcTable[clip3(0, 53, qpL+2*(bs-1)+h.tcOffset)]
				if vertical {
					filterLuma(pic.y, y*pic.ys+x, 1, pic.ys, beta, tc, p.noFilter, q.noFilter)
				} else {
					filterLuma(pic.y, y*pic.ys+x, pic.ys, 1, beta, tc, p.noFilter, q.noFilter)
				}
				// Chroma: bS 2 edges on the 8x8 chroma grid.
				if bs == 2 && ((vertical && x&15 == 0) || (!vertical && y&15 == 0)) {
					for c := 0; c < 2; c++ {
						plane := pic.cb
						off := h.pps.CbQPOffset
						if c == 1 {
							plane, off = pic.cr, h.pps.CrQPOffset
						}
						qpc := chromaQP(((int(p.qp) + int(q.qp) + 1) >> 1) + off)
						tcc := tcTable[clip3(0, 53, qpc+2+h.tcOffset)]
						o := (y/2)*pic.cs + x/2
						if vertical {
							filterChroma(plane, o, 1, pic.cs, tcc, p.noFilter, q.noFilter)
						} else {
							filterChroma(plane, o, pic.cs, 1, tcc, p.noFilter, q.noFilter)
						}
					}
				}
			}
		}
	}
}

// sliceAt returns the header of the slice containing luma sample (x, y).
func (d *Decoder) sliceAt(x, y int) *sliceHeader {
	sp := d.active
	fc := &d.fc
	return fc.slices[fc.ctbSlice[(y>>sp.Log2Ctb)*sp.PicWCtb+x>>sp.Log2Ctb]]
}

// filterLuma filters one 4-sample luma edge segment. off is q0 of the
// first line; step crosses the edge and along moves along it.
func filterLuma(pix []byte, off, step, along, beta, tc int, noP, noQ bool) {
	at := func(line, i int) int { return int(pix[off+line*along+i*step]) } // i<0: p side
	dp0 := iabs(at(0, -3) - 2*at(0, -2) + at(0, -1))
	dp3 := iabs(at(3, -3) - 2*at(3, -2) + at(3, -1))
	dq0 := iabs(at(0, 2) - 2*at(0, 1) + at(0, 0))
	dq3 := iabs(at(3, 2) - 2*at(3, 1) + at(3, 0))
	dpq0, dpq3 := dp0+dq0, dp3+dq3
	dp, dq := dp0+dp3, dq0+dq3
	if dpq0+dpq3 >= beta {
		return
	}
	dSam := func(line, dpq int) bool {
		return 2*dpq < beta>>2 &&
			iabs(at(line, -4)-at(line, -1))+iabs(at(line, 0)-at(line, 3)) < beta>>3 &&
			iabs(at(line, -1)-at(line, 0)) < (5*tc+1)>>1
	}
	strong := dSam(0, dpq0) && dSam(3, dpq3)
	dEp := dp < (beta+beta>>1)>>3
	dEq := dq < (beta+beta>>1)>>3
	for k := 0; k < 4; k++ {
		b := off + k*along
		p0, p1, p2, p3 := int(pix[b-step]), int(pix[b-2*step]), int(pix[b-3*step]), int(pix[b-4*step])
		q0, q1, q2, q3 := int(pix[b]), int(pix[b+step]), int(pix[b+2*step]), int(pix[b+3*step])
		if strong {
			if !noP {
				pix[b-step] = byte(clip3(p0-2*tc, p0+2*tc, (p2+2*p1+2*p0+2*q0+q1+4)>>3))
				pix[b-2*step] = byte(clip3(p1-2*tc, p1+2*tc, (p2+p1+p0+q0+2)>>2))
				pix[b-3*step] = byte(clip3(p2-2*tc, p2+2*tc, (2*p3+3*p2+p1+p0+q0+4)>>3))
			}
			if !noQ {
				pix[b] = byte(clip3(q0-2*tc, q0+2*tc, (p1+2*p0+2*q0+2*q1+q2+4)>>3))
				pix[b+step] = byte(clip3(q1-2*tc, q1+2*tc, (p0+q0+q1+q2+2)>>2))
				pix[b+2*step] = byte(clip3(q2-2*tc, q2+2*tc, (p0+q0+q1+3*q2+2*q3+4)>>3))
			}
			continue
		}
		delta := (9*(q0-p0) - 3*(q1-p1) + 8) >> 4
		if iabs(delta) >= tc*10 {
			continue
		}
		delta = clip3(-tc, tc, delta)
		if !noP {
			pix[b-step] = clipU8(p0 + delta)
		}
		if !noQ {
			pix[b] = clipU8(q0 - delta)
		}
		if dEp && !noP {
			dp := clip3(-(tc >> 1), tc>>1, (((p2+p0+1)>>1)-p1+delta)>>1)
			pix[b-2*step] = clipU8(p1 + dp)
		}
		if dEq && !noQ {
			dq := clip3(-(tc >> 1), tc>>1, (((q2+q0+1)>>1)-q1-delta)>>1)
			pix[b+step] = clipU8(q1 + dq)
		}
	}
}

// filterChroma filters a 2-sample chroma edge segment (4:2:0).
func filterChroma(pix []byte, off, step, along, tc int, noP, noQ bool) {
	for k := 0; k < 2; k++ {
		b := off + k*along
		p0, p1 := int(pix[b-step]), int(pix[b-2*step])
		q0, q1 := int(pix[b]), int(pix[b+step])
		delta := clip3(-tc, tc, ((((q0 - p0) << 2) + p1 - q1 + 4) >> 3))
		if !noP {
			pix[b-step] = clipU8(p0 + delta)
		}
		if !noQ {
			pix[b] = clipU8(q0 - delta)
		}
	}
}
