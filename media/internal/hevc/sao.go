package hevc

// Sample adaptive offset (spec 8.7.3), applied per CTB to the deblocked
// picture.

var saoEO = [4][2][2]int{ // [class][neighbour a/b] = (dx, dy)
	{{-1, 0}, {1, 0}},
	{{0, -1}, {0, 1}},
	{{-1, -1}, {1, 1}},
	{{1, -1}, {-1, 1}},
}

func (d *Decoder) applySAO(pic *picture) {
	sp := d.active
	fc := &d.fc
	any := false
	for _, h := range fc.slices {
		if h.saoLuma || h.saoChroma {
			any = true
		}
	}
	if !any {
		return
	}
	// SAO reads deblocked samples: keep a copy.
	src := [3][]byte{
		append(d.saoTmp[0][:0], pic.y...),
		append(d.saoTmp[1][:0], pic.cb...),
		append(d.saoTmp[2][:0], pic.cr...),
	}
	d.saoTmp = src
	dst := [3][]byte{pic.y, pic.cb, pic.cr}
	for ctb := 0; ctb < sp.PicWCtb*sp.PicHCtb; ctb++ {
		if fc.ctbSliceAddr[ctb] < 0 {
			continue
		}
		prm := &fc.sao[ctb]
		for c := 0; c < 3; c++ {
			if prm.typ[c] == 0 {
				continue
			}
			d.saoCTB(pic, ctb, c, prm, src[c], dst[c])
		}
	}
}

// saoCTB applies SAO to component c of CTB ctb.
func (d *Decoder) saoCTB(pic *picture, ctb, c int, prm *saoParams, src, dst []byte) {
	sp := d.active
	fc := &d.fc
	scale, stride := 1, pic.ys
	if c > 0 {
		scale, stride = 2, pic.cs
	}
	size := sp.CtbSize / scale
	cx, cy := ctb%sp.PicWCtb, ctb/sp.PicWCtb
	x0, y0 := cx*size, cy*size
	w, h := sp.Width/scale, sp.Height/scale
	x1, y1 := min(x0+size, w), min(y0+size, h)
	skip := fc.anyNoFilter
	if prm.typ[c] == 1 {
		var table [32]int16
		for k := 0; k < 4; k++ {
			table[(k+int(prm.bandPos[c]))&31] = int16(prm.offset[c][k])
		}
		for y := y0; y < y1; y++ {
			srow := src[y*stride+x0 : y*stride+x1]
			drow := dst[y*stride+x0 : y*stride+x1]
			for i, v := range srow {
				if o := table[v>>3]; o != 0 {
					if skip && d.saoSkip((x0+i)*scale, y*scale) {
						continue
					}
					drow[i] = clipU8(int(v) + int(o))
				}
			}
		}
		return
	}
	// eoff maps 2 + sign(v-a) + sign(v-b) to the offset of its edge
	// category (0: local minimum ... 4: local maximum; 2: none).
	var eoff [5]int
	eoff[0], eoff[1], eoff[3], eoff[4] = int(prm.offset[c][0]), int(prm.offset[c][1]), int(prm.offset[c][2]), int(prm.offset[c][3])
	eo := saoEO[prm.eoClass[c]]
	ax, ay, bx, by := eo[0][0], eo[0][1], eo[1][0], eo[1][1]
	// ok[dy+1][dx+1]: whether samples of the neighbouring CTB at (dx, dy)
	// (or beyond the picture) may be used.
	var ok [3][3]bool
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			xn, yn := x0, y0
			if dx < 0 {
				xn = x0 - 1
			} else if dx > 0 {
				xn = x1
			}
			if dy < 0 {
				yn = y0 - 1
			} else if dy > 0 {
				yn = y1
			}
			ok[dy+1][dx+1] = d.saoNeighbourOK(ctb, xn, yn, w, h, scale)
		}
	}
	// region returns which neighbouring CTB (-1, 0, 1) holds coordinate
	// v+delta of a sample at v in [lo, hi).
	region := func(v, delta, lo, hi int) int {
		switch n := v + delta; {
		case n < lo:
			return -1
		case n >= hi:
			return 1
		}
		return 0
	}
	// The interior rectangle (all but the first and last column) of the
	// rows whose neighbours are usable goes to the vector kernel.
	vecY0, vecY1 := y1, y1
	if haveNEON && !skip && x1-x0-2 >= 16 {
		var tbl [16]int8
		for i, o := range eoff {
			tbl[i] = int8(o)
		}
		vecY0, vecY1 = y0, y1
		for vecY0 < y1 && !(ok[region(vecY0, ay, y0, y1)+1][1] && ok[region(vecY0, by, y0, y1)+1][1]) {
			vecY0++
		}
		for vecY1 > vecY0 && !(ok[region(vecY1-1, ay, y0, y1)+1][1] && ok[region(vecY1-1, by, y0, y1)+1][1]) {
			vecY1--
		}
		if vecY1 > vecY0 {
			o := vecY0*stride + x0 + 1
			neonSAOEdge(&dst[o], &src[o], stride, ay*stride+ax, by*stride+bx, x1-x0-2, vecY1-vecY0, &tbl)
		}
	}
	for y := y0; y < y1; y++ {
		rya, ryb := region(y, ay, y0, y1)+1, region(y, by, y0, y1)+1
		// Rows outside the picture are never read (ok is false there).
		ia, ib := y+ay, y+by
		if ia < 0 || ia >= h {
			ia = y
		}
		if ib < 0 || ib >= h {
			ib = y
		}
		srow, arow, brow := src[y*stride:], src[ia*stride:], src[ib*stride:]
		drow := dst[y*stride:]
		// Interior columns share one validity; the first and last column
		// may reach into the left / right neighbours.
		for _, x := range [2]int{x0, x1 - 1} {
			if !ok[rya][region(x, ax, x0, x1)+1] || !ok[ryb][region(x, bx, x0, x1)+1] {
				continue
			}
			if skip && d.saoSkip(x*scale, y*scale) {
				continue
			}
			v := int(srow[x])
			e := 2 + sign(v-int(arow[x+ax])) + sign(v-int(brow[x+bx]))
			drow[x] = clipU8(v + eoff[e])
		}
		xs, xe := x0+1, x1-1
		if xs >= xe || !ok[rya][1] || !ok[ryb][1] || y >= vecY0 && y < vecY1 {
			continue
		}
		s := srow[xs:xe]
		a := arow[xs+ax : xe+ax]
		b := brow[xs+bx : xe+bx]
		dr := drow[xs:xe]
		if skip {
			for i, v := range s {
				if d.saoSkip((xs+i)*scale, y*scale) {
					continue
				}
				e := 2 + sign(int(v)-int(a[i])) + sign(int(v)-int(b[i]))
				dr[i] = clipU8(int(v) + eoff[e])
			}
			continue
		}
		for i, v := range s {
			e := 2 + sign(int(v)-int(a[i])) + sign(int(v)-int(b[i]))
			if o := eoff[e]; o != 0 {
				dr[i] = clipU8(int(v) + o)
			}
		}
	}
}

// saoNeighbourOK reports whether SAO of CTB ctb may read the sample at
// (xn, yn) (component coordinates; scale converts to luma): inside the
// picture, decoded, and not across a slice or tile boundary that forbids
// in-loop filtering.
func (d *Decoder) saoNeighbourOK(ctb, xn, yn, w, h, scale int) bool {
	sp := d.active
	fc := &d.fc
	if xn < 0 || yn < 0 || xn >= w || yn >= h {
		return false
	}
	nctb := (yn*scale>>sp.Log2Ctb)*sp.PicWCtb + (xn*scale)>>sp.Log2Ctb
	if nctb == ctb {
		return true
	}
	if fc.ctbSliceAddr[nctb] < 0 {
		return false
	}
	pps := fc.slices[fc.ctbSlice[ctb]].pps
	if fc.ctbSliceAddr[nctb] != fc.ctbSliceAddr[ctb] {
		// The flag of the slice decoded later governs the boundary.
		later := ctb
		if pps.ctbRsToTs[nctb] > pps.ctbRsToTs[ctb] {
			later = nctb
		}
		if !fc.slices[fc.ctbSlice[later]].lfAcrossSlices {
			return false
		}
	}
	if !pps.LoopFilterAcrossTiles && pps.tileID[pps.ctbRsToTs[nctb]] != pps.tileID[pps.ctbRsToTs[ctb]] {
		return false
	}
	return true
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// saoSkip reports samples SAO leaves alone (pcm with the loop filter off,
// transquant bypass).
func (d *Decoder) saoSkip(xl, yl int) bool {
	fc := &d.fc
	return fc.blk[(yl>>2)*fc.w4+xl>>2].noFilter
}
