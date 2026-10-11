package av1

// Deblocking loop filter (spec 7.14).

// lfLevel is the adaptive filter strength of one 4x4 unit for an edge
// direction (spec 7.14.4).
func (d *Decoder) lfLevel(f *frameState, m *miInfo, plane, pass int) int {
	h := f.hdr
	i := pass
	if plane > 0 {
		i = plane + 1
	}
	delta := 0
	if h.deltaLFPresent {
		if h.deltaLFMulti {
			delta = int(m.deltaLF[i])
		} else {
			delta = int(m.deltaLF[0])
		}
	}
	lvl := clip3(0, 63, delta+h.lf.level[i])
	seg := int(m.segID)
	if h.seg.enabled && h.seg.featureEnabled[seg][segLvlAltLFYV+i] {
		lvl = clip3(0, 63, lvl+h.seg.featureData[seg][segLvlAltLFYV+i])
	}
	if h.lf.deltaEnabled {
		n := uint(lvl >> 5)
		ref := int(m.refFrame[0])
		if ref <= intraFrame {
			lvl += h.lf.refDeltas[intraFrame] << n
		} else {
			mode := int(m.yMode)
			modeType := 0
			if mode >= nearestMV && mode != globalMV && mode != globalGlobalMV {
				modeType = 1
			}
			lvl += h.lf.refDeltas[ref]<<n + h.lf.modeDeltas[modeType]<<n
		}
		lvl = clip3(0, 63, lvl)
	}
	return lvl
}

// deblock applies the loop filter to the current frame in place.
func (d *Decoder) deblock(f *frameState) {
	h := f.hdr
	if h.lf.level[0] == 0 && h.lf.level[1] == 0 {
		return
	}
	for plane := 0; plane < f.cur.numPlanes; plane++ {
		if plane > 0 && h.lf.level[plane+1] == 0 {
			continue
		}
		for pass := 0; pass < 2; pass++ {
			step := 1
			if plane > 0 {
				step = 2
			}
			// Vertical edges (pass 0) only change samples in their own
			// rows, horizontal ones only in their own columns, so bands of
			// 16 mi rows / columns filter independently.
			const band = 16
			n := (f.miRows + band - 1) / band
			if pass == 1 {
				n = (f.miCols + band - 1) / band
			}
			parallelFor(n, func(_, b int) {
				r0, r1, c0, c1 := b*band, min((b+1)*band, f.miRows), 0, f.miCols
				if pass == 1 {
					r0, r1, c0, c1 = 0, f.miRows, b*band, min((b+1)*band, f.miCols)
				}
				for row := r0; row < r1; row += step {
					for col := c0; col < c1; col += step {
						d.edgeFilter(f, plane, pass, row, col)
					}
				}
			})
		}
	}
}

func (d *Decoder) edgeFilter(f *frameState, plane, pass, row, col int) {
	h := f.hdr
	ss := min(plane, 1)
	dx, dy := 1, 0
	if pass == 1 {
		dx, dy = 0, 1
	}
	x, y := col*4, row*4
	row |= ss
	col |= ss
	switch {
	case x >= h.Width, y >= h.Height:
		return
	case pass == 0 && x == 0, pass == 1 && y == 0:
		return
	}
	xP, yP := x>>ss, y>>ss
	prevRow, prevCol := row-dy<<ss, col-dx<<ss
	m := &f.mi[row*f.miCols+col]
	pm := &f.mi[prevRow*f.miCols+prevCol]
	lfs := f.lfTxSize[plane]
	stride := f.lfStride[plane]
	tx := int(lfs[(row>>ss)*stride+col>>ss])
	prevTx := int(lfs[(prevRow>>ss)*stride+prevCol>>ss])
	psz := int(m.size)
	if plane > 0 {
		psz = chromaSize[psz]
	}
	var isBlockEdge, isTxEdge bool
	var base int
	if pass == 0 {
		isBlockEdge = xP%blockW(psz) == 0
		isTxEdge = xP%txW[tx] == 0
		base = min(txW[prevTx], txW[tx])
	} else {
		isBlockEdge = yP%blockH(psz) == 0
		isTxEdge = yP%txH[tx] == 0
		base = min(txH[prevTx], txH[tx])
	}
	if !isTxEdge {
		return
	}
	skip := m.skip && m.isInter
	prevSkip := pm.skip && pm.isInter
	if !(isBlockEdge || !skip || !prevSkip) {
		return
	}
	size := min(16, base)
	if plane > 0 {
		size = min(8, base)
	}
	lvl := d.lfLevel(f, m, plane, pass)
	if lvl == 0 {
		lvl = d.lfLevel(f, pm, plane, pass)
	}
	if lvl == 0 {
		return
	}
	sharp := h.lf.sharpness
	shift := 0
	if sharp > 4 {
		shift = 2
	} else if sharp > 0 {
		shift = 1
	}
	limit := max(1, lvl>>shift)
	if sharp > 0 {
		limit = clip3(1, 9-sharp, lvl>>shift)
	}
	blimit := 2*(lvl+2) + limit
	thresh := lvl >> 4
	pb := &f.cur.planes[plane]
	for i := 0; i < 4; i++ {
		lfSample(pb, xP+dy*i, yP+dx*i, plane, int32(limit), int32(blimit), int32(thresh), dx, dy, size, f.cur.bitDepth)
	}
}

// lfSample is the sample filtering process (spec 7.14.6) across the edge
// before sample (x, y).
func lfSample(pb *planeBuf, x, y, plane int, limit, blimit, thresh int32, dx, dy, size, bd int) {
	step := dy*pb.stride + dx
	base := y*pb.stride + x
	var px [16]int32 // px[8+k] = sample at offset k (k = -8..7)
	n := 4
	if size > 4 {
		n = 4
		if size == 16 {
			n = 7
		}
		if plane > 0 {
			n = 3
		}
	}
	if pb.pix16 != nil {
		for k := -n; k < n; k++ {
			px[8+k] = int32(pb.pix16[base+k*step])
		}
	} else {
		for k := -n; k < n; k++ {
			px[8+k] = int32(pb.pix8[base+k*step])
		}
	}
	p := func(i int) int32 { return px[7-i] }
	q := func(i int) int32 { return px[8+i] }
	sh := uint(bd - 8)
	threshBd, limitBd, blimitBd := thresh<<sh, limit<<sh, blimit<<sh
	hev := iabs32(p(1)-p(0)) > threshBd || iabs32(q(1)-q(0)) > threshBd
	filterLen := 4
	switch {
	case size == 4:
	case plane > 0:
		filterLen = 6
	case size == 8:
		filterLen = 8
	default:
		filterLen = 16
	}
	edge := iabs32(p(0)-q(0))*2+iabs32(p(1)-q(1))/2 > blimitBd
	mask := edge || iabs32(p(1)-p(0)) > limitBd || iabs32(q(1)-q(0)) > limitBd
	if filterLen >= 6 {
		mask = mask || iabs32(p(2)-p(1)) > limitBd || iabs32(q(2)-q(1)) > limitBd
	}
	if filterLen >= 8 {
		mask = mask || iabs32(p(3)-p(2)) > limitBd || iabs32(q(3)-q(2)) > limitBd
	}
	if mask {
		return
	}
	one := int32(1) << sh
	flat, flat2 := false, false
	if size >= 8 {
		flat = iabs32(p(1)-p(0)) <= one && iabs32(q(1)-q(0)) <= one && iabs32(p(2)-p(0)) <= one && iabs32(q(2)-q(0)) <= one
		if filterLen != 6 {
			flat = flat && iabs32(p(3)-p(0)) <= one && iabs32(q(3)-q(0)) <= one
		}
	}
	if size >= 16 {
		flat2 = iabs32(p(6)-p(0)) <= one && iabs32(q(6)-q(0)) <= one && iabs32(p(5)-p(0)) <= one && iabs32(q(5)-q(0)) <= one && iabs32(p(4)-p(0)) <= one && iabs32(q(4)-q(0)) <= one
	}
	var out [16]int32
	lo, hi := 0, 0 // offsets written: [lo, hi)
	switch {
	case size == 4 || !flat:
		// Narrow filter.
		half := int32(0x80) << sh
		lim := int32(1) << (bd - 1)
		c4 := func(v int32) int32 { return min(max(v, -lim), lim-1) }
		ps1, ps0, qs0, qs1 := p(1)-half, p(0)-half, q(0)-half, q(1)-half
		fl := int32(0)
		if hev {
			fl = c4(ps1 - qs1)
		}
		fl = c4(fl + 3*(qs0-ps0))
		f1 := c4(fl+4) >> 3
		f2 := c4(fl+3) >> 3
		out[8] = c4(qs0-f1) + half
		out[7] = c4(ps0+f2) + half
		lo, hi = -1, 1
		if !hev {
			fl = (f1 + 1) >> 1
			out[9] = c4(qs1-fl) + half
			out[6] = c4(ps1+fl) + half
			lo, hi = -2, 2
		}
	default:
		log2 := 3
		if size == 16 && flat2 {
			log2 = 4
		}
		var nn, n2 int
		switch {
		case log2 == 4:
			nn, n2 = 6, 1
		case plane == 0:
			nn, n2 = 3, 0
		default:
			nn, n2 = 2, 1
		}
		for i := -nn; i < nn; i++ {
			t := int32(0)
			for j := -nn; j <= nn; j++ {
				pp := clip3(-(nn + 1), nn, i+j)
				tap := int32(1)
				if j <= n2 && j >= -n2 {
					tap = 2
				}
				t += px[8+pp] * tap
			}
			out[8+i] = (t + 1<<(log2-1)) >> log2
		}
		lo, hi = -nn, nn
	}
	if pb.pix16 != nil {
		for k := lo; k < hi; k++ {
			pb.pix16[base+k*step] = uint16(out[8+k])
		}
	} else {
		for k := lo; k < hi; k++ {
			pb.pix8[base+k*step] = uint8(out[8+k])
		}
	}
}
