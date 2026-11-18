package vp9

// Residual decoding and reconstruction (libvpx vp9_detokenize.c and
// decode_block's intra / inter reconstruction loops).

type scanOrder struct {
	scan []int16
	nb   []int16
}

var scanOrders = [4][4]scanOrder{
	{{defaultScan4x4[:], defaultScan4x4Nb[:]}, {rowScan4x4[:], rowScan4x4Nb[:]}, {colScan4x4[:], colScan4x4Nb[:]}, {defaultScan4x4[:], defaultScan4x4Nb[:]}},
	{{defaultScan8x8[:], defaultScan8x8Nb[:]}, {rowScan8x8[:], rowScan8x8Nb[:]}, {colScan8x8[:], colScan8x8Nb[:]}, {defaultScan8x8[:], defaultScan8x8Nb[:]}},
	{{defaultScan16x16[:], defaultScan16x16Nb[:]}, {rowScan16x16[:], rowScan16x16Nb[:]}, {colScan16x16[:], colScan16x16Nb[:]}, {defaultScan16x16[:], defaultScan16x16Nb[:]}},
	{{defaultScan32x32[:], defaultScan32x32Nb[:]}, {defaultScan32x32[:], defaultScan32x32Nb[:]}, {defaultScan32x32[:], defaultScan32x32Nb[:]}, {defaultScan32x32[:], defaultScan32x32Nb[:]}},
}

var intraModeToTxType = [10]uint8{0, 1, 2, 0, 3, 1, 2, 2, 1, 3}

var cat6Prob = [14]uint8{254, 254, 254, 252, 249, 243, 230, 196, 177, 153, 140, 133, 130, 129}
var catProbs = [5][]uint8{{159}, {165, 145}, {173, 148, 140}, {176, 155, 140, 135}, {180, 157, 141, 134, 130}}
var catBase = [6]int{5, 7, 11, 19, 35, 67}

// decodeCoefs reads one transform block's tokens into t.coef (dequantized,
// raster order) and returns the end of block.
func (t *tileState) decodeCoefs(plane, tx int, so *scanOrder, ctx int, dq [2]int32) int {
	d := t.d
	ptype := 0
	if plane > 0 {
		ptype = 1
	}
	ref := 0
	if t.bi.isInter {
		ref = 1
	}
	probs := &d.fc.coef[tx][ptype][ref]
	counts := &t.counts.coef[tx][ptype][ref]
	eobBr := &t.counts.eobBranch[tx][ptype][ref]
	maxEOB := 16 << (tx << 1)
	band := coefbandTrans8x8plus[:]
	if tx == 0 {
		band = coefbandTrans4x4[:]
	}
	scan, nb := so.scan, so.nb
	cache := &t.tokCache
	coef := &t.coef
	dqShift := 0
	if tx == 3 {
		dqShift = 1
	}
	bd := &t.bd
	dqv := dq[0]
	c := 0
	for c < maxEOB {
		bnd := band[c]
		p := &probs[bnd][ctx]
		eobBr[bnd][ctx]++
		if bd.read(p[0]) == 0 {
			counts[bnd][ctx][3]++
			break
		}
		for bd.read(p[1]) == 0 {
			counts[bnd][ctx][0]++
			dqv = dq[1]
			cache[scan[c]] = 0
			c++
			if c >= maxEOB {
				return c
			}
			ctx = (1 + int(cache[nb[2*c]]) + int(cache[nb[2*c+1]])) >> 1
			bnd = band[c]
			p = &probs[bnd][ctx]
		}
		var val int
		if bd.read(p[2]) == 0 {
			counts[bnd][ctx][1]++
			cache[scan[c]] = 1
			val = 1
		} else {
			counts[bnd][ctx][2]++
			tok := bd.tree(coefConTree[:], pareto8[p[2]-1][:])
			cache[scan[c]] = ptEnergyClass[tok]
			switch {
			case tok <= 4:
				val = tok
			case tok < 10:
				cp := catProbs[tok-5]
				v := 0
				for _, pr := range cp {
					v = v<<1 | bd.read(pr)
				}
				val = catBase[tok-5] + v
			default:
				v := 0
				for _, pr := range cat6Prob {
					v = v<<1 | bd.read(pr)
				}
				val = catBase[5] + v
			}
		}
		v := (int32(val) * dqv) >> dqShift
		if bd.readBit() != 0 {
			v = -v
		}
		coef[scan[c]] = v
		c++
		ctx = (1 + int(cache[nb[2*c]]) + int(cache[nb[2*c+1]])) >> 1
		dqv = dq[1]
	}
	return c
}

// planeGeom describes the current block within plane p.
type planeGeom struct {
	ss         int
	n4wl, n4hl int
	maxW, maxH int // visible 4x4 columns / rows
	limW, limH bool
	tx         int
	x0, y0     int // pixel origin
	aOff, lOff int // context offsets
}

func (t *tileState) planeGeom(p int) planeGeom {
	var g planeGeom
	if p > 0 {
		g.ss = 1
	}
	g.n4wl = max(t.bwl-g.ss, 0)
	g.n4hl = max(t.bhl-g.ss, 0)
	n4w, n4h := 1<<g.n4wl, 1<<g.n4hl
	g.maxW, g.maxH = n4w, n4h
	if t.toRight < 0 {
		g.maxW += t.toRight >> (5 + g.ss)
		g.limW = true
	}
	if t.toBottom < 0 {
		g.maxH += t.toBottom >> (5 + g.ss)
		g.limH = true
	}
	g.tx = int(t.bi.tx)
	if p > 0 {
		g.tx = min(g.tx, min(g.n4wl, g.n4hl))
	}
	g.x0 = (t.miCol * 8) >> g.ss
	g.y0 = (t.miRow * 8) >> g.ss
	g.aOff = (t.miCol * 2) >> g.ss
	g.lOff = ((t.miRow & 7) * 2) >> g.ss
	return g
}

// tokens decodes the transform block at (col,row) (4x4 units) and updates
// the nonzero contexts.
func (t *tileState) tokens(p int, g *planeGeom, col, row int, so *scanOrder) int {
	d := t.d
	n := 1 << g.tx
	a := d.aboveNZ[p][g.aOff+col:]
	l := t.leftNZ[p][g.lOff+row:]
	ctx := 0
	for i := 0; i < n && i < len(a); i++ {
		if a[i] != 0 {
			ctx = 1
			break
		}
	}
	for i := 0; i < n && i < len(l); i++ {
		if l[i] != 0 {
			ctx++
			break
		}
	}
	pt := 0
	if p > 0 {
		pt = 1
	}
	eob := t.decodeCoefs(p, g.tx, so, ctx, [2]int32{d.dequant[t.bi.seg][pt][0], d.dequant[t.bi.seg][pt][1]})
	nz := uint8(0)
	if eob > 0 {
		nz = 1
	}
	for i := 0; i < n && i < len(a); i++ {
		if g.limW && col+i >= g.maxW {
			a[i] = 0
		} else {
			a[i] = nz
		}
	}
	for i := 0; i < n && i < len(l); i++ {
		if g.limH && row+i >= g.maxH {
			l[i] = 0
		} else {
			l[i] = nz
		}
	}
	return eob
}

func (t *tileState) reconIntra() {
	d := t.d
	b := t.bi
	cur := d.cur
	for p := 0; p < 3; p++ {
		g := t.planeGeom(p)
		step := 1 << g.tx
		stride := cur.stride[p]
		pix := cur.planes[p]
		for row := 0; row < g.maxH; row += step {
			for col := 0; col < g.maxW; col += step {
				mode := b.uvMode
				if p == 0 {
					mode = b.mode
					if b.bsize < block8x8 {
						mode = b.bmode[(row<<1)+col]
					}
				}
				x, y := g.x0+col*4, g.y0+row*4
				haveTop := row > 0 || t.above != nil || t.miRow > 0
				haveLeft := col > 0 || t.left != nil
				haveRight := col+step < 1<<g.n4wl
				t.predictIntra(p, g.tx, int(mode), x, y, haveTop, haveLeft, haveRight)
				if b.skip {
					continue
				}
				txType := 0
				if p == 0 && !d.hdr.lossless {
					txType = int(intraModeToTxType[mode])
				}
				so := &scanOrders[g.tx][txType]
				if d.hdr.lossless {
					so = &scanOrders[0][0]
				}
				eob := t.tokens(p, &g, col, row, so)
				if eob > 0 {
					t.inverseTransform(g.tx, txType, eob, so, pix[y*stride+x:], stride)
				}
			}
		}
	}
}

func (t *tileState) reconInter() int {
	d := t.d
	cur := d.cur
	total := 0
	for p := 0; p < 3; p++ {
		g := t.planeGeom(p)
		step := 1 << g.tx
		stride := cur.stride[p]
		pix := cur.planes[p]
		so := &scanOrders[g.tx][0]
		for row := 0; row < g.maxH; row += step {
			for col := 0; col < g.maxW; col += step {
				eob := t.tokens(p, &g, col, row, so)
				if eob > 0 {
					x, y := g.x0+col*4, g.y0+row*4
					t.inverseTransform(g.tx, 0, eob, so, pix[y*stride+x:], stride)
				}
				total += eob
			}
		}
	}
	return total
}
