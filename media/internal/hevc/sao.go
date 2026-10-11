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
	x1, y1 := min(x0+size, w), min(y0+size, hh(h))
	hdr := fc.slices[fc.ctbSlice[ctb]]
	pps := hdr.pps
	var off [5]int
	if prm.typ[c] == 1 {
		var table [32]int
		for k := 0; k < 4; k++ {
			table[(k+int(prm.bandPos[c]))&31] = k + 1
		}
		for k := 0; k < 4; k++ {
			off[k+1] = int(prm.offset[c][k])
		}
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				if d.saoSkip(x*scale, y*scale) {
					continue
				}
				v := int(src[y*stride+x])
				if b := table[v>>3]; b != 0 {
					dst[y*stride+x] = clipU8(v + off[b])
				}
			}
		}
		return
	}
	for k := 0; k < 4; k++ {
		off[k+1] = int(prm.offset[c][k])
	}
	eo := saoEO[prm.eoClass[c]]
	// Neighbour CTB acceptance (slices, tiles) for samples near the CTB
	// border.
	nbOK := func(xn, yn int) bool {
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
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			ax, ay := x+eo[0][0], y+eo[0][1]
			bx, by := x+eo[1][0], y+eo[1][1]
			if !nbOK(ax, ay) || !nbOK(bx, by) {
				continue
			}
			if d.saoSkip(x*scale, y*scale) {
				continue
			}
			v := int(src[y*stride+x])
			a, b := int(src[ay*stride+ax]), int(src[by*stride+bx])
			e := 2 + sign(v-a) + sign(v-b)
			switch e {
			case 0:
				e = 1
			case 1:
				e = 2
			case 2:
				e = 0
			}
			if e != 0 {
				dst[y*stride+x] = clipU8(v + off[e])
			}
		}
	}
}

func hh(h int) int { return h }

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
