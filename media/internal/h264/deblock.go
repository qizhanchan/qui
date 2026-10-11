package h264

// Deblocking filter (spec 8.7), applied to a whole picture after all its
// slices are decoded.

var alphaTab = [52]uint8{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 4, 4, 5, 6,
	7, 8, 9, 10, 12, 13, 15, 17, 20, 22, 25, 28, 32, 36, 40, 45,
	50, 56, 63, 71, 80, 90, 101, 113, 127, 144, 162, 182, 203, 226, 255, 255,
}

var betaTab = [52]uint8{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 2, 2, 3,
	3, 3, 3, 4, 4, 4, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10,
	11, 11, 12, 12, 13, 13, 14, 14, 15, 15, 16, 16, 17, 17, 18, 18,
}

var tc0Tab = [52][3]uint8{
	{0, 0, 0}, {0, 0, 0}, {0, 0, 0}, {0, 0, 0}, {0, 0, 0}, {0, 0, 0}, {0, 0, 0}, {0, 0, 0},
	{0, 0, 0}, {0, 0, 0}, {0, 0, 0}, {0, 0, 0}, {0, 0, 0}, {0, 0, 0}, {0, 0, 0}, {0, 0, 0},
	{0, 0, 0}, {0, 0, 1}, {0, 0, 1}, {0, 0, 1}, {0, 0, 1}, {0, 1, 1}, {0, 1, 1}, {1, 1, 1},
	{1, 1, 1}, {1, 1, 1}, {1, 1, 1}, {1, 1, 2}, {1, 1, 2}, {1, 1, 2}, {1, 1, 2}, {1, 2, 3},
	{1, 2, 3}, {2, 2, 3}, {2, 2, 4}, {2, 3, 4}, {2, 3, 4}, {3, 3, 5}, {3, 4, 6}, {3, 4, 6},
	{4, 5, 7}, {4, 5, 8}, {4, 6, 9}, {5, 7, 10}, {6, 8, 11}, {6, 8, 13}, {7, 10, 14}, {8, 11, 16},
	{9, 12, 18}, {10, 13, 20}, {11, 15, 23}, {13, 17, 25},
}

func mvFar(a, b []int16, i, j int) bool {
	dx := int(a[2*i]) - int(b[2*j])
	dy := int(a[2*i+1]) - int(b[2*j+1])
	return dx >= 4 || dx <= -4 || dy >= 4 || dy <= -4
}

// bsMotion derives bS (0 or 1) from the motion of 4x4 blocks i (p) and j
// (q) of picture p (spec 8.7.2.1).
func bsMotion(p *picture, i, j int) uint8 {
	pr0, pr1 := p.refID[0][i], p.refID[1][i]
	qr0, qr1 := p.refID[0][j], p.refID[1][j]
	np, nq := 0, 0
	if pr0 >= 0 {
		np++
	}
	if pr1 >= 0 {
		np++
	}
	if qr0 >= 0 {
		nq++
	}
	if qr1 >= 0 {
		nq++
	}
	if np != nq {
		return 1
	}
	m0, m1 := p.mv[0], p.mv[1]
	if np == 1 {
		pr, pm := pr0, m0
		if pr0 < 0 {
			pr, pm = pr1, m1
		}
		qr, qm := qr0, m0
		if qr0 < 0 {
			qr, qm = qr1, m1
		}
		if pr != qr || mvFar(pm, qm, i, j) {
			return 1
		}
		return 0
	}
	if np == 0 {
		return 0
	}
	if !((pr0 == qr0 && pr1 == qr1) || (pr0 == qr1 && pr1 == qr0)) {
		return 1
	}
	if pr0 != pr1 {
		if pr0 == qr0 {
			if mvFar(m0, m0, i, j) || mvFar(m1, m1, i, j) {
				return 1
			}
			return 0
		}
		if mvFar(m0, m1, i, j) || mvFar(m1, m0, i, j) {
			return 1
		}
		return 0
	}
	if (mvFar(m0, m0, i, j) || mvFar(m1, m1, i, j)) && (mvFar(m0, m1, i, j) || mvFar(m1, m0, i, j)) {
		return 1
	}
	return 0
}

// deblock filters picture p.
func (pc *picCtx) deblock(p *picture) {
	for addr := 0; addr < pc.mbW*pc.mbH; addr++ {
		pc.deblockMB(p, addr)
	}
}

// deblockMB filters the edges of one macroblock (spec 8.7).
func (pc *picCtx) deblockMB(p *picture, addr int) {
	mbW := pc.mbW
	stride4 := mbW * 4
	{
		mb := &pc.mbs[addr]
		if mb.slice < 0 {
			return
		}
		si := &pc.slices[mb.slice]
		if si.disable == 1 {
			return
		}
		mbX, mbY := addr%mbW, addr/mbW
		var left, top *mbState
		if mbX > 0 {
			n := &pc.mbs[addr-1]
			if n.slice >= 0 && (si.disable == 0 || n.slice == mb.slice) {
				left = n
			}
		}
		if mbY > 0 {
			n := &pc.mbs[addr-mbW]
			if n.slice >= 0 && (si.disable == 0 || n.slice == mb.slice) {
				top = n
			}
		}
		intra := mb.typ&mtIntra != 0
		bx0, by0 := mbX*4, mbY*4

		// Boundary strengths: bs[dir][edge][segment].
		var bs [2][4][4]uint8
		for dir := 0; dir < 2; dir++ {
			for e := 0; e < 4; e++ {
				if mb.t8x8 && e&1 == 1 {
					continue
				}
				var nb *mbState
				if e == 0 {
					if dir == 0 {
						nb = left
					} else {
						nb = top
					}
					if nb == nil {
						continue
					}
				} else {
					nb = mb
				}
				same := e > 0 && samePartition(mb.typ, dir, e)
				for k := 0; k < 4; k++ {
					var qx, qy, px, py int
					if dir == 0 {
						qx, qy = e, k
						px, py = e-1, k
					} else {
						qx, qy = k, e
						px, py = k, e-1
					}
					if intra || nb.typ&mtIntra != 0 {
						if e == 0 {
							bs[dir][e][k] = 4
						} else {
							bs[dir][e][k] = 3
						}
						continue
					}
					pRaster := ((py+4)&3)*4 + (px+4)&3
					if mb.nzMask&(1<<uint(qy*4+qx)) != 0 || nb.nzMask&(1<<uint(pRaster)) != 0 {
						bs[dir][e][k] = 2
						continue
					}
					if same {
						continue // one partition: identical motion, bS 0
					}
					i := (by0+py)*stride4 + bx0 + px
					j := (by0+qy)*stride4 + bx0 + qx
					bs[dir][e][k] = bsMotion(p, i, j)
				}
			}
		}

		ys := p.stride
		lx, ly := mbX*16, mbY*16
		for dir := 0; dir < 2; dir++ {
			for e := 0; e < 4; e++ {
				b := &bs[dir][e]
				if b[0]|b[1]|b[2]|b[3] == 0 {
					continue
				}
				qp := int(mb.qp)
				if e == 0 {
					nb := left
					if dir == 1 {
						nb = top
					}
					qp = (int(nb.qp) + qp + 1) >> 1
				}
				if dir == 0 {
					filterLuma(p.y, ly*ys+lx+e*4, 1, ys, b, qp, si)
				} else {
					filterLuma(p.y, (ly+e*4)*ys+lx, ys, 1, b, qp, si)
				}
			}
		}
		cs := p.cstride
		cx, cy := mbX*8, mbY*8
		// Cb and Cr are independent, so both planes' edges are filtered
		// together (in the same dir/edge order each plane needs).
		for dir := 0; dir < 2; dir++ {
			for e := 0; e < 4; e += 2 {
				b := &bs[dir][e]
				if b[0]|b[1]|b[2]|b[3] == 0 {
					continue
				}
				var qp [2]int
				for c := range qp {
					qp[c] = int(mb.qpc[c])
					if e == 0 {
						nb := left
						if dir == 1 {
							nb = top
						}
						qp[c] = (int(nb.qpc[c]) + qp[c] + 1) >> 1
					}
				}
				if dir == 0 {
					filterChroma2(p.cb, p.cr, cy*cs+cx+e*2, 1, cs, b, qp, si)
				} else {
					filterChroma2(p.cb, p.cr, (cy+e*2)*cs+cx, cs, 1, b, qp, si)
				}
			}
		}
	}
}

// chromaGather/chromaScatter are the TBL indices that turn eight 4-byte
// rows of Cb and of Cr (p1 p0 q0 q1 across a vertical edge) into four
// 16-lane vectors and back (neonChromaEdge).
var chromaGather, chromaScatter [64]byte

func init() {
	for c := 0; c < 4; c++ {
		for l := 0; l < 16; l++ {
			chromaGather[c*16+l] = byte((l/8)*32 + (l%8)*4 + c)
		}
	}
	for r := 0; r < 4; r++ {
		for b := 0; b < 16; b++ {
			plane, row, col := r/2, (r%2)*4+b/4, b%4
			chromaScatter[r*16+b] = byte(col*16 + plane*8 + row)
		}
	}
}

// filterChroma2 filters the same 8-sample edge in Cb and Cr.
func filterChroma2(cb, cr []byte, off, step, along int, bs *[4]uint8, qp [2]int, si *sliceInfo) {
	if haveNEON {
		strong := bs[0] == 4 && bs[1] == 4 && bs[2] == 4 && bs[3] == 4
		lo, hi := off-2*step, off+step+8
		if step == 1 {
			lo, hi = off-2, off+7*along+2
		}
		if (strong || (bs[0] < 4 && bs[1] < 4 && bs[2] < 4 && bs[3] < 4)) &&
			lo >= 0 && hi <= len(cb) && hi <= len(cr) {
			var prm [48]int8
			for c := 0; c < 2; c++ {
				indexA := clip3(0, 51, qp[c]+si.alphaOffset)
				indexB := clip3(0, 51, qp[c]+si.betaOffset)
				alpha, beta := alphaTab[indexA], betaTab[indexB]
				for k := 0; k < 8; k++ {
					l := c*8 + k
					prm[l] = int8(alpha)
					prm[16+l] = int8(beta)
					tc := int8(-1)
					if b := bs[k>>1]; b != 0 && b < 4 {
						tc = int8(tc0Tab[indexA][b-1]) + 1
					}
					prm[32+l] = tc
				}
			}
			st, vert := 0, 0
			if strong {
				st = 1
			}
			stride := step
			if step == 1 {
				vert, stride = 1, along
			}
			neonChromaEdge(&cb[off], &cr[off], stride, vert, &prm, st, &chromaGather, &chromaScatter)
			return
		}
	}
	filterChroma(cb, off, step, along, bs, qp[0], si)
	filterChroma(cr, off, step, along, bs, qp[1], si)
}

// samePartition reports whether internal edge e (1..3) in direction dir
// (0 = vertical edges) of an inter macroblock of type typ lies inside one
// motion partition, so both sides share references and motion vectors.
// Direct macroblocks may carry per-4x4 motion and never qualify.
func samePartition(typ uint32, dir, e int) bool {
	if typ&(mtIntra|mtDirect) != 0 {
		return false
	}
	switch {
	case typ&mt16x16 != 0:
		return true
	case typ&mt16x8 != 0:
		return dir == 0 || e != 2
	case typ&mt8x16 != 0:
		return dir == 1 || e != 2
	}
	return false
}

func iabs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// filterLuma filters one 16-sample luma edge. off is the first q0 sample,
// step crosses the edge and along moves along it.
func filterLuma(pix []byte, off, step, along int, bs *[4]uint8, qp int, si *sliceInfo) {
	if haveNEON {
		indexA := clip3(0, 51, qp+si.alphaOffset)
		indexB := clip3(0, 51, qp+si.betaOffset)
		alpha, beta := int(alphaTab[indexA]), int(betaTab[indexB])
		if alpha == 0 || beta == 0 {
			return
		}
		strong := bs[0] == 4 && bs[1] == 4 && bs[2] == 4 && bs[3] == 4
		if strong || (bs[0] < 4 && bs[1] < 4 && bs[2] < 4 && bs[3] < 4) {
			vert := 0
			lo, hi := off-4*step, off+3*step+16 // horizontal edge: 8 rows of 16
			if step == 1 {
				vert = 1
				lo, hi = off-4, off+15*along+4 // vertical edge: 16 rows of 8
			}
			if lo >= 0 && hi <= len(pix) {
				var tc0 [16]int8
				st := 1
				if !strong {
					st = 0
					for seg, b := range bs {
						t := int8(-1)
						if b != 0 {
							t = int8(tc0Tab[indexA][b-1])
						}
						for k := 0; k < 4; k++ {
							tc0[seg*4+k] = t
						}
					}
				}
				neonLumaEdge(&pix[off], along*vert+step*(1-vert), vert, alpha, beta, &tc0[0], st)
				return
			}
		}
	}
	filterLumaGo(pix, off, step, along, bs, qp, si)
}

// filterLumaGo is the scalar luma edge filter (and the NEON reference).
func filterLumaGo(pix []byte, off, step, along int, bs *[4]uint8, qp int, si *sliceInfo) {
	indexA := clip3(0, 51, qp+si.alphaOffset)
	indexB := clip3(0, 51, qp+si.betaOffset)
	alpha, beta := int(alphaTab[indexA]), int(betaTab[indexB])
	if alpha == 0 || beta == 0 {
		return
	}
	for k := 0; k < 16; k++ {
		bS := bs[k>>2]
		if bS == 0 {
			continue
		}
		o := off + k*along
		p0, p1, p2 := int(pix[o-step]), int(pix[o-2*step]), int(pix[o-3*step])
		q0, q1, q2 := int(pix[o]), int(pix[o+step]), int(pix[o+2*step])
		if iabs(p0-q0) >= alpha || iabs(p1-p0) >= beta || iabs(q1-q0) >= beta {
			continue
		}
		ap, aq := iabs(p2-p0), iabs(q2-q0)
		if bS < 4 {
			tc0 := int(tc0Tab[indexA][bS-1])
			tc := tc0
			if ap < beta {
				tc++
			}
			if aq < beta {
				tc++
			}
			delta := clip3(-tc, tc, (((q0-p0)<<2)+(p1-q1)+4)>>3)
			pix[o-step] = clip255(int32(p0 + delta))
			pix[o] = clip255(int32(q0 - delta))
			if ap < beta {
				pix[o-2*step] = byte(p1 + clip3(-tc0, tc0, (p2+((p0+q0+1)>>1)-(p1<<1))>>1))
			}
			if aq < beta {
				pix[o+step] = byte(q1 + clip3(-tc0, tc0, (q2+((p0+q0+1)>>1)-(q1<<1))>>1))
			}
			continue
		}
		p3, q3 := int(pix[o-4*step]), int(pix[o+3*step])
		small := iabs(p0-q0) < (alpha>>2)+2
		if ap < beta && small {
			pix[o-step] = byte((p2 + 2*p1 + 2*p0 + 2*q0 + q1 + 4) >> 3)
			pix[o-2*step] = byte((p2 + p1 + p0 + q0 + 2) >> 2)
			pix[o-3*step] = byte((2*p3 + 3*p2 + p1 + p0 + q0 + 4) >> 3)
		} else {
			pix[o-step] = byte((2*p1 + p0 + q1 + 2) >> 2)
		}
		if aq < beta && small {
			pix[o] = byte((p1 + 2*p0 + 2*q0 + 2*q1 + q2 + 4) >> 3)
			pix[o+step] = byte((p0 + q0 + q1 + q2 + 2) >> 2)
			pix[o+2*step] = byte((2*q3 + 3*q2 + q1 + q0 + p0 + 4) >> 3)
		} else {
			pix[o] = byte((2*q1 + q0 + p1 + 2) >> 2)
		}
	}
}

// filterChroma filters one 8-sample chroma edge.
func filterChroma(pix []byte, off, step, along int, bs *[4]uint8, qp int, si *sliceInfo) {
	indexA := clip3(0, 51, qp+si.alphaOffset)
	indexB := clip3(0, 51, qp+si.betaOffset)
	alpha, beta := int(alphaTab[indexA]), int(betaTab[indexB])
	if alpha == 0 || beta == 0 {
		return
	}
	for k := 0; k < 8; k++ {
		bS := bs[k>>1]
		if bS == 0 {
			continue
		}
		o := off + k*along
		p0, p1 := int(pix[o-step]), int(pix[o-2*step])
		q0, q1 := int(pix[o]), int(pix[o+step])
		if iabs(p0-q0) >= alpha || iabs(p1-p0) >= beta || iabs(q1-q0) >= beta {
			continue
		}
		if bS < 4 {
			tc := int(tc0Tab[indexA][bS-1]) + 1
			delta := clip3(-tc, tc, (((q0-p0)<<2)+(p1-q1)+4)>>3)
			pix[o-step] = clip255(int32(p0 + delta))
			pix[o] = clip255(int32(q0 - delta))
			continue
		}
		pix[o-step] = byte((2*p1 + p0 + q1 + 2) >> 2)
		pix[o] = byte((2*q1 + q0 + p1 + 2) >> 2)
	}
}

// conceal fills macroblocks no slice covered (lost or corrupt data) with
// the co-located samples of the most recent reference picture.
func (pc *picCtx) conceal(p *picture, d *Decoder) {
	var src *picture
	missing := false
	for i := range pc.mbs {
		if pc.mbs[i].slice < 0 {
			missing = true
			break
		}
	}
	if !missing {
		return
	}
	src = d.fallbackRef()
	if src != nil {
		src.prog.wait()
	}
	pc.slices = append(pc.slices, sliceInfo{disable: 1})
	sn := int32(len(pc.slices) - 1)
	stride4 := pc.mbW * 4
	for addr := range pc.mbs {
		mb := &pc.mbs[addr]
		if mb.slice >= 0 {
			continue
		}
		d.concealedMBs++
		mb.slice = sn
		mb.typ = mtIntra16x16
		mb.qp, mb.qpc = 0, [2]int8{}
		mbX, mbY := addr%pc.mbW, addr/pc.mbW
		for y := 0; y < 16; y++ {
			o := (mbY*16+y)*p.stride + mbX*16
			if src != nil {
				copy(p.y[o:o+16], src.y[o:o+16])
			} else {
				for i := 0; i < 16; i++ {
					p.y[o+i] = 128
				}
			}
		}
		for y := 0; y < 8; y++ {
			o := (mbY*8+y)*p.cstride + mbX*8
			if src != nil {
				copy(p.cb[o:o+8], src.cb[o:o+8])
				copy(p.cr[o:o+8], src.cr[o:o+8])
			} else {
				for i := 0; i < 8; i++ {
					p.cb[o+i], p.cr[o+i] = 128, 128
				}
			}
		}
		p.mbType[addr] = mtIntra16x16
		for l := 0; l < 2; l++ {
			for y := 0; y < 4; y++ {
				for x := 0; x < 4; x++ {
					i := (mbY*4+y)*stride4 + mbX*4 + x
					p.ref[l][i], p.refID[l][i] = -1, -1
					p.mv[l][2*i], p.mv[l][2*i+1] = 0, 0
				}
			}
		}
	}
}
