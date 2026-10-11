package av1

// Dequantization and reconstruction (spec 7.12.3, 7.13.3).

var txRowShift = [txSizesAll]uint{0, 1, 2, 2, 2, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2}

func dcQ(bd, q int) int32 {
	q = clip3(0, 255, q)
	switch bd {
	case 10:
		return int32(dcQLookup10[q])
	case 12:
		return int32(dcQLookup12[q])
	}
	return int32(dcQLookup8[q])
}

func acQ(bd, q int) int32 {
	q = clip3(0, 255, q)
	switch bd {
	case 10:
		return int32(acQLookup10[q])
	case 12:
		return int32(acQLookup12[q])
	}
	return int32(acQLookup8[q])
}

// reconstruct dequantizes t.coef, inverse transforms it and adds the
// residual to the prediction at (x, y) of plane.
func (t *tile) reconstruct(plane, x, y, tx int) {
	b := &t.b
	h := t.h
	bd := t.seq.BitDepth
	rs := &t.recon
	a := adjustedTx[tx]
	tw, th := txW[a], txH[a]
	w, hh := txW[tx], txH[tx]
	q := t.qindex()
	var dcq, acq int32
	switch plane {
	case 0:
		dcq, acq = dcQ(bd, q+h.deltaQYDc), acQ(bd, q)
	case 1:
		dcq, acq = dcQ(bd, q+h.deltaQUDc), acQ(bd, q+h.deltaQUAc)
	default:
		dcq, acq = dcQ(bd, q+h.deltaQVDc), acQ(bd, q+h.deltaQVAc)
	}
	var qm []uint8
	if h.usingQMatrix && !b.lossless {
		if lvl := h.segQMLevel[plane][b.segID]; lvl < 15 {
			qm = quantMatrix(lvl, min(plane, 1), tx)
		}
	}
	shift := uint(0)
	switch n := w * hh; {
	case n > 1024:
		shift = 2
	case n > 256:
		shift = 1
	}
	maxV := int32(1)<<(7+bd) - 1
	minV := -int32(1) << (7 + bd)
	for i := 0; i < th*tw; i++ {
		c := t.coef[i]
		if c == 0 {
			rs.dq[i] = 0
			continue
		}
		dq := acq
		if i == 0 {
			dq = dcq
		}
		if qm != nil {
			dq = (int32(qm[i])*dq + 16) >> 5
		}
		v := int32(int64(iabs32(c))*int64(dq)&0xFFFFFF) >> shift
		if c < 0 {
			v = -v
		}
		rs.dq[i] = min(max(v, minV), maxV)
	}
	t.inverseTransform(tx, t.planeType, b.lossless)
	t.addResidual(plane, x, y, w, hh)
}

// inverseTransform turns rs.dq (row-major, width min(w, 32)) into
// rs.res (row-major, width w), following libaom's 2-D process (which the
// spec's matches for conformant streams): rectangular scaling, a
// (bd+8)-bit clamp of the row input, row shift, a max(bd+6, 16)-bit clamp
// of the column input, column shift 4; flips are applied here.
func (t *tile) inverseTransform(tx, typ int, lossless bool) {
	rs := &t.recon
	bd := t.seq.BitDepth
	w, h := txW[tx], txH[tx]
	lw, lh := txWLog2[tx], txHLog2[tx]
	tw, th := min(w, 32), min(h, 32)
	if lossless {
		t.inverseWHT()
		return
	}
	rowT, colT, flipLR, flipUD := txKinds(typ)
	rect := lw-lh == 1 || lh-lw == 1
	rowShift := txRowShift[tx]
	rowMax := int32(1)<<(bd+7) - 1
	rowMin := -int32(1) << (bd + 7)
	cb := max(bd+6, 16)
	colMax := int32(1)<<(cb-1) - 1
	colMin := -int32(1) << (cb - 1)
	row := rs.tmp[:w]
	for i := 0; i < h; i++ {
		out := rs.res[i*w : i*w+w]
		if i >= th || allZero(rs.dq[i*tw:i*tw+tw]) {
			clear(out)
			continue
		}
		for j := 0; j < w; j++ {
			v := int32(0)
			if j < tw {
				v = rs.dq[i*tw+j]
			}
			if rect {
				v = round12(int64(v) * 2896)
			}
			row[j] = min(max(v, rowMin), rowMax)
		}
		tx1D(row, lw, rowT)
		for j := 0; j < w; j++ {
			v := row[j]
			if rowShift > 0 {
				v = (v + 1<<(rowShift-1)) >> rowShift
			}
			out[j] = min(max(v, colMin), colMax)
		}
	}
	col := rs.tmp[:h]
	for j := 0; j < w; j++ {
		for i := 0; i < h; i++ {
			col[i] = rs.res[i*w+j]
		}
		tx1D(col, lh, colT)
		for i := 0; i < h; i++ {
			rs.res[i*w+j] = (col[i] + 8) >> 4
		}
	}
	if flipUD {
		for i := 0; i < h/2; i++ {
			a, b := rs.res[i*w:i*w+w], rs.res[(h-1-i)*w:(h-1-i)*w+w]
			for j := range a {
				a[j], b[j] = b[j], a[j]
			}
		}
	}
	if flipLR {
		for i := 0; i < h; i++ {
			r := rs.res[i*w : i*w+w]
			for j := 0; j < w/2; j++ {
				r[j], r[w-1-j] = r[w-1-j], r[j]
			}
		}
	}
}

func allZero(v []int32) bool {
	for _, x := range v {
		if x != 0 {
			return false
		}
	}
	return true
}

// 1-D transform kinds.
const (
	kDCT = iota
	kADST
	kIdentity
)

// txKinds returns the row (horizontal) and column (vertical) transforms
// of a transform type and its flips.
func txKinds(typ int) (row, col int, flipLR, flipUD bool) {
	switch typ {
	case dctDct:
		return kDCT, kDCT, false, false
	case adstDct:
		return kDCT, kADST, false, false
	case dctAdst:
		return kADST, kDCT, false, false
	case adstAdst:
		return kADST, kADST, false, false
	case flipadstDct:
		return kDCT, kADST, false, true
	case dctFlipadst:
		return kADST, kDCT, true, false
	case flipadstFlipadst:
		return kADST, kADST, true, true
	case adstFlipadst:
		return kADST, kADST, true, false
	case flipadstAdst:
		return kADST, kADST, false, true
	case idtx:
		return kIdentity, kIdentity, false, false
	case vDct:
		return kIdentity, kDCT, false, false
	case hDct:
		return kDCT, kIdentity, false, false
	case vAdst:
		return kIdentity, kADST, false, false
	case hAdst:
		return kADST, kIdentity, false, false
	case vFlipadst:
		return kIdentity, kADST, false, true
	case hFlipadst:
		return kADST, kIdentity, true, false
	}
	return kDCT, kDCT, false, false
}

func tx1D(v []int32, n, kind int) {
	switch kind {
	case kDCT:
		idct(v, n)
	case kADST:
		switch n {
		case 2:
			iadst4(v)
		case 3:
			iadst8(v)
		default:
			iadst16(v)
		}
	default:
		switch n {
		case 2:
			for i := range v {
				v[i] = round12(int64(v[i]) * 5793)
			}
		case 3:
			for i := range v {
				v[i] *= 2
			}
		case 4:
			for i := range v {
				v[i] = round12(int64(v[i]) * 11586)
			}
		default:
			for i := range v {
				v[i] *= 4
			}
		}
	}
}

// inverseWHT is the lossless 4x4 Walsh-Hadamard inverse (spec 7.13.2.10).
func (t *tile) inverseWHT() {
	rs := &t.recon
	wht := func(v []int32, stride int, shift uint) {
		a := v[0] >> shift
		c := v[stride] >> shift
		d := v[2*stride] >> shift
		b := v[3*stride] >> shift
		a += c
		d -= b
		e := (a - d) >> 1
		b = e - b
		c = e - c
		a -= b
		d += c
		v[0], v[stride], v[2*stride], v[3*stride] = a, b, c, d
	}
	copy(rs.res[:16], rs.dq[:16])
	for i := 0; i < 4; i++ {
		wht(rs.res[i*4:], 1, 2)
	}
	for j := 0; j < 4; j++ {
		wht(rs.res[j:], 4, 0)
	}
}

// addResidual adds rs.res (w x h) to the picture at (x, y) of plane.
func (t *tile) addResidual(plane, x, y, w, h int) {
	pb := &t.f.cur.planes[plane]
	res := t.recon.res[:]
	if pb.pix16 != nil {
		maxV := int32(1)<<t.seq.BitDepth - 1
		for i := 0; i < h; i++ {
			row := pb.pix16[(y+i)*pb.stride+x:]
			for j := 0; j < w; j++ {
				row[j] = uint16(min(max(int32(row[j])+res[i*w+j], 0), maxV))
			}
		}
		return
	}
	for i := 0; i < h; i++ {
		row := pb.pix8[(y+i)*pb.stride+x:]
		for j := 0; j < w; j++ {
			row[j] = uint8(min(max(int32(row[j])+res[i*w+j], 0), 255))
		}
	}
}

// quantMatrix returns the quantizer matrix of a level, plane type and
// transform size (not yet available).
func quantMatrix(level, ptype, tx int) []uint8 { return nil }
