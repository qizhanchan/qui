package av1

// Intra prediction (spec 7.11.2).

// smWeights holds Sm_Weights_Tx_NxN at offset N.
var smWeights = [128]int32{0, 0, 255, 128, 255, 149, 85, 64, 255, 197, 146, 105, 73, 50, 37, 32, 255, 225, 196, 170, 145, 123, 102, 84, 68, 54, 43, 33, 26, 20, 17, 16, 255, 240, 225, 210, 196, 182, 169, 157, 145, 133, 122, 111, 101, 92, 83, 74, 66, 59, 52, 45, 39, 34, 29, 25, 21, 17, 14, 12, 10, 9, 8, 8, 255, 248, 240, 233, 225, 218, 210, 203, 196, 189, 182, 176, 169, 163, 156, 150, 144, 138, 133, 127, 121, 116, 111, 106, 101, 96, 91, 86, 82, 77, 73, 69, 65, 61, 57, 54, 50, 47, 44, 41, 38, 35, 32, 29, 27, 25, 22, 20, 18, 16, 15, 13, 12, 10, 9, 8, 7, 6, 6, 5, 5, 4, 4, 4}

// drIntraDerivative is Dr_Intra_Derivative, indexed by angle.
var drIntraDerivative = [90]int32{0, 0, 0, 1023, 0, 0, 547, 0, 0, 372, 0, 0, 0, 0, 273, 0, 0, 215, 0, 0, 178, 0, 0, 151, 0, 0, 132, 0, 0, 116, 0, 0, 102, 0, 0, 0, 90, 0, 0, 80, 0, 0, 71, 0, 0, 64, 0, 0, 57, 0, 0, 51, 0, 0, 45, 0, 0, 0, 40, 0, 0, 35, 0, 0, 31, 0, 0, 27, 0, 0, 23, 0, 0, 19, 0, 0, 15, 0, 0, 0, 0, 11, 0, 0, 7, 0, 0, 3, 0, 0}

// intraFilterTaps is Intra_Filter_Taps (the 8th tap is padding).
var intraFilterTaps = [5][8][8]int32{}

var intraFilterTapsFlat = [320]int32{-6, 10, 0, 0, 0, 12, 0, 0, -5, 2, 10, 0, 0, 9, 0, 0, -3, 1, 1, 10, 0, 7, 0, 0, -3, 1, 1, 2, 10, 5, 0, 0, -4, 6, 0, 0, 0, 2, 12, 0, -3, 2, 6, 0, 0, 2, 9, 0, -3, 2, 2, 6, 0, 2, 7, 0, -3, 1, 2, 2, 6, 3, 5, 0, -10, 16, 0, 0, 0, 10, 0, 0, -6, 0, 16, 0, 0, 6, 0, 0, -4, 0, 0, 16, 0, 4, 0, 0, -2, 0, 0, 0, 16, 2, 0, 0, -10, 16, 0, 0, 0, 0, 10, 0, -6, 0, 16, 0, 0, 0, 6, 0, -4, 0, 0, 16, 0, 0, 4, 0, -2, 0, 0, 0, 16, 0, 2, 0, -8, 8, 0, 0, 0, 16, 0, 0, -8, 0, 8, 0, 0, 16, 0, 0, -8, 0, 0, 8, 0, 16, 0, 0, -8, 0, 0, 0, 8, 16, 0, 0, -4, 4, 0, 0, 0, 0, 16, 0, -4, 0, 4, 0, 0, 0, 16, 0, -4, 0, 0, 4, 0, 0, 16, 0, -4, 0, 0, 0, 4, 0, 16, 0, -2, 8, 0, 0, 0, 10, 0, 0, -1, 3, 8, 0, 0, 6, 0, 0, -1, 2, 3, 8, 0, 4, 0, 0, 0, 1, 2, 3, 8, 2, 0, 0, -1, 4, 0, 0, 0, 3, 10, 0, -1, 3, 4, 0, 0, 4, 6, 0, -1, 2, 3, 4, 0, 4, 4, 0, -1, 2, 2, 3, 4, 3, 3, 0, -12, 14, 0, 0, 0, 14, 0, 0, -10, 0, 14, 0, 0, 12, 0, 0, -9, 0, 0, 14, 0, 11, 0, 0, -8, 0, 0, 0, 14, 10, 0, 0, -10, 12, 0, 0, 0, 0, 14, 0, -9, 1, 12, 0, 0, 0, 12, 0, -8, 0, 0, 12, 0, 1, 11, 0, -7, 0, 0, 1, 12, 1, 9, 0}

func init() {
	for m := 0; m < 5; m++ {
		for i := 0; i < 8; i++ {
			for j := 0; j < 8; j++ {
				intraFilterTaps[m][i][j] = intraFilterTapsFlat[(m*8+i)*8+j]
			}
		}
	}
}

var modeToAngle = [intraModes]int{0, 90, 180, 45, 135, 113, 157, 203, 67, 0, 0, 0, 0}

var intraEdgeKernel = [3][5]int32{{0, 4, 8, 4, 0}, {0, 5, 6, 5, 0}, {2, 4, 4, 4, 2}}

// edgeOff is the offset of index 0 in the edge buffers (room for -1, -2).
const edgeOff = 16

// intraEdges holds AboveRow / LeftCol with room for negative indices and
// upsampling.
type intraEdges struct {
	above, left [edgeOff + 2*128 + 32]int32
	pred        [64 * 64]int32
}

func (pb *planeBuf) get(x, y int) int32 {
	if pb.pix16 != nil {
		return int32(pb.pix16[y*pb.stride+x])
	}
	return int32(pb.pix8[y*pb.stride+x])
}

func (pb *planeBuf) set(x, y int, v int32) {
	if pb.pix16 != nil {
		pb.pix16[y*pb.stride+x] = uint16(v)
	} else {
		pb.pix8[y*pb.stride+x] = uint8(v)
	}
}

// store writes a w x h prediction block into the plane.
func (pb *planeBuf) store(x, y, w, h int, pred []int32) {
	if pb.pix16 != nil {
		for i := 0; i < h; i++ {
			row := pb.pix16[(y+i)*pb.stride+x:]
			for j := 0; j < w; j++ {
				row[j] = uint16(pred[i*w+j])
			}
		}
		return
	}
	for i := 0; i < h; i++ {
		row := pb.pix8[(y+i)*pb.stride+x:]
		for j := 0; j < w; j++ {
			row[j] = uint8(pred[i*w+j])
		}
	}
}

func (t *tile) isSmooth(row, col, plane int) bool {
	m := t.mi(row, col)
	mode := int(m.yMode)
	if plane > 0 {
		if m.refFrame[0] > intraFrame {
			return false
		}
		mode = int(m.uvMode)
	}
	return mode == smoothPred || mode == smoothVPred || mode == smoothHPred
}

// filterType is get_filter_type.
func (t *tile) filterType(plane int) int {
	b := &t.b
	above, left := false, false
	if (plane == 0 && b.availU) || (plane > 0 && b.availUC) {
		r, c := b.miRow-1, b.miCol
		if plane > 0 {
			if b.miCol&1 == 0 {
				c++
			}
			if b.miRow&1 != 0 {
				r--
			}
		}
		above = t.isSmooth(r, c, plane)
	}
	if (plane == 0 && b.availL) || (plane > 0 && b.availLC) {
		r, c := b.miRow, b.miCol-1
		if plane > 0 {
			if b.miCol&1 != 0 {
				c--
			}
			if b.miRow&1 == 0 {
				r++
			}
		}
		left = t.isSmooth(r, c, plane)
	}
	if above || left {
		return 1
	}
	return 0
}

func edgeStrength(w, h, typ, delta int) int {
	d := delta
	if d < 0 {
		d = -d
	}
	wh := w + h
	s := 0
	if typ == 0 {
		switch {
		case wh <= 8:
			if d >= 56 {
				s = 1
			}
		case wh <= 16:
			if d >= 40 {
				s = 1
			}
		case wh <= 24:
			if d >= 8 {
				s = 1
			}
			if d >= 16 {
				s = 2
			}
			if d >= 32 {
				s = 3
			}
		case wh <= 32:
			if d >= 1 {
				s = 1
			}
			if d >= 4 {
				s = 2
			}
			if d >= 32 {
				s = 3
			}
		default:
			if d >= 1 {
				s = 3
			}
		}
		return s
	}
	switch {
	case wh <= 8:
		if d >= 40 {
			s = 1
		}
		if d >= 64 {
			s = 2
		}
	case wh <= 16:
		if d >= 20 {
			s = 1
		}
		if d >= 48 {
			s = 2
		}
	case wh <= 24:
		if d >= 4 {
			s = 3
		}
	default:
		if d >= 1 {
			s = 3
		}
	}
	return s
}

func useUpsample(w, h, typ, delta int) bool {
	d := delta
	if d < 0 {
		d = -d
	}
	if d <= 0 || d >= 40 {
		return false
	}
	if typ == 0 {
		return w+h <= 16
	}
	return w+h <= 8
}

// filterEdge is the intra edge filter process on buf[-1 .. sz-2] (index
// edgeOff-1 onwards).
func filterEdge(buf []int32, sz, strength int) {
	if strength == 0 {
		return
	}
	var edge [2*128 + 32]int32
	for i := 0; i < sz; i++ {
		edge[i] = buf[edgeOff+i-1]
	}
	k := &intraEdgeKernel[strength-1]
	for i := 1; i < sz; i++ {
		s := int32(0)
		for j := 0; j < 5; j++ {
			s += k[j] * edge[clip3(0, sz-1, i-2+j)]
		}
		buf[edgeOff+i-1] = (s + 8) >> 4
	}
}

// upsampleEdge is the intra edge upsample process.
func upsampleEdge(buf []int32, numPx int, maxV int32) {
	var dup [64 + 3]int32
	dup[0] = buf[edgeOff-1]
	for i := -1; i < numPx; i++ {
		dup[i+2] = buf[edgeOff+i]
	}
	dup[numPx+2] = buf[edgeOff+numPx-1]
	buf[edgeOff-2] = dup[0]
	for i := 0; i < numPx; i++ {
		s := -dup[i] + 9*dup[i+1] + 9*dup[i+2] - dup[i+3]
		s = min(max((s+8)>>4, 0), maxV)
		buf[edgeOff+2*i-1] = s
		buf[edgeOff+2*i] = dup[i+2]
	}
}

// predictIntra is predict_intra (spec 7.11.2).
func (t *tile) predictIntra(plane, x, y int, haveLeft, haveAbove, haveAboveRt, haveBelowLt bool, mode, log2W, log2H int) {
	b := &t.b
	f := t.f
	pb := &f.cur.planes[plane]
	bd := t.seq.BitDepth
	ss := min(plane, 1)
	maxX := (f.miCols*4)>>ss - 1
	maxY := (f.miRows*4)>>ss - 1
	w, h := 1<<log2W, 1<<log2H
	e := &t.edges
	above, left := e.above[:], e.left[:]
	n := w + h
	switch {
	case !haveAbove && haveLeft:
		v := pb.get(x-1, y)
		for i := 0; i < n; i++ {
			above[edgeOff+i] = v
		}
	case !haveAbove && !haveLeft:
		v := int32(1)<<(bd-1) - 1
		for i := 0; i < n; i++ {
			above[edgeOff+i] = v
		}
	default:
		lim := x + w - 1
		if haveAboveRt {
			lim = x + 2*w - 1
		}
		lim = min(maxX, lim)
		for i := 0; i < n; i++ {
			above[edgeOff+i] = pb.get(min(lim, x+i), y-1)
		}
	}
	switch {
	case !haveLeft && haveAbove:
		v := pb.get(x, y-1)
		for i := 0; i < n; i++ {
			left[edgeOff+i] = v
		}
	case !haveLeft && !haveAbove:
		v := int32(1)<<(bd-1) + 1
		for i := 0; i < n; i++ {
			left[edgeOff+i] = v
		}
	default:
		lim := y + h - 1
		if haveBelowLt {
			lim = y + 2*h - 1
		}
		lim = min(maxY, lim)
		for i := 0; i < n; i++ {
			left[edgeOff+i] = pb.get(x-1, min(lim, y+i))
		}
	}
	var corner int32
	switch {
	case haveAbove && haveLeft:
		corner = pb.get(x-1, y-1)
	case haveAbove:
		corner = pb.get(x, y-1)
	case haveLeft:
		corner = pb.get(x-1, y)
	default:
		corner = 1 << (bd - 1)
	}
	above[edgeOff-1], left[edgeOff-1] = corner, corner
	pred := e.pred[:w*h]
	maxV := int32(1)<<bd - 1
	switch {
	case plane == 0 && b.useFilterIntra:
		t.filterIntraPred(pred, w, h, maxV)
	case isDirectional(mode):
		delta := b.angleDeltaY
		if plane > 0 {
			delta = b.angleDeltaUV
		}
		t.directionalPred(pred, plane, x, y, w, h, haveLeft, haveAbove, modeToAngle[mode]+delta*3, maxX, maxY, maxV)
	case mode == smoothPred:
		wx, wy := smWeights[w:2*w], smWeights[h:2*h]
		for i := 0; i < h; i++ {
			for j := 0; j < w; j++ {
				s := wy[i]*above[edgeOff+j] + (256-wy[i])*left[edgeOff+h-1] + wx[j]*left[edgeOff+i] + (256-wx[j])*above[edgeOff+w-1]
				pred[i*w+j] = (s + 256) >> 9
			}
		}
	case mode == smoothVPred:
		wy := smWeights[h : 2*h]
		for i := 0; i < h; i++ {
			for j := 0; j < w; j++ {
				pred[i*w+j] = (wy[i]*above[edgeOff+j] + (256-wy[i])*left[edgeOff+h-1] + 128) >> 8
			}
		}
	case mode == smoothHPred:
		wx := smWeights[w : 2*w]
		for i := 0; i < h; i++ {
			for j := 0; j < w; j++ {
				pred[i*w+j] = (wx[j]*left[edgeOff+i] + (256-wx[j])*above[edgeOff+w-1] + 128) >> 8
			}
		}
	case mode == dcPred:
		var avg int32
		switch {
		case haveAbove && haveLeft:
			s := int32(0)
			for k := 0; k < h; k++ {
				s += left[edgeOff+k]
			}
			for k := 0; k < w; k++ {
				s += above[edgeOff+k]
			}
			avg = (s + int32((w+h)>>1)) / int32(w+h)
		case haveLeft:
			s := int32(0)
			for k := 0; k < h; k++ {
				s += left[edgeOff+k]
			}
			avg = (s + int32(h>>1)) >> log2H
		case haveAbove:
			s := int32(0)
			for k := 0; k < w; k++ {
				s += above[edgeOff+k]
			}
			avg = (s + int32(w>>1)) >> log2W
		default:
			avg = 1 << (bd - 1)
		}
		for i := range pred {
			pred[i] = avg
		}
	default: // Paeth
		tl := above[edgeOff-1]
		for i := 0; i < h; i++ {
			l := left[edgeOff+i]
			for j := 0; j < w; j++ {
				a := above[edgeOff+j]
				base := a + l - tl
				pl, pt, ptl := iabs32(base-l), iabs32(base-a), iabs32(base-tl)
				switch {
				case pl <= pt && pl <= ptl:
					pred[i*w+j] = l
				case pt <= ptl:
					pred[i*w+j] = a
				default:
					pred[i*w+j] = tl
				}
			}
		}
	}
	pb.store(x, y, w, h, pred)
}

func (t *tile) directionalPred(pred []int32, plane, x, y, w, h int, haveLeft, haveAbove bool, pAngle, maxX, maxY int, maxV int32) {
	e := &t.edges
	above, left := e.above[:], e.left[:]
	upA, upL := 0, 0
	if t.seq.EnableIntraEdge {
		ft := t.filterType(plane)
		if pAngle != 90 && pAngle != 180 {
			if pAngle > 90 && pAngle < 180 && w+h >= 24 {
				c := (left[edgeOff]*5 + above[edgeOff-1]*6 + above[edgeOff]*5 + 8) >> 4
				above[edgeOff-1], left[edgeOff-1] = c, c
			}
			if haveAbove {
				s := edgeStrength(w, h, ft, pAngle-90)
				n := min(w, maxX-x+1) + 1
				if pAngle < 90 {
					n += h
				}
				filterEdge(above, n, s)
			}
			if haveLeft {
				s := edgeStrength(w, h, ft, pAngle-180)
				n := min(h, maxY-y+1) + 1
				if pAngle > 180 {
					n += w
				}
				filterEdge(left, n, s)
			}
		}
		if useUpsample(w, h, ft, pAngle-90) {
			upA = 1
			n := w
			if pAngle < 90 {
				n += h
			}
			upsampleEdge(above, n, maxV)
		}
		if useUpsample(w, h, ft, pAngle-180) {
			upL = 1
			n := h
			if pAngle > 180 {
				n += w
			}
			upsampleEdge(left, n, maxV)
		}
	}
	var dx, dy int
	if pAngle < 90 {
		dx = int(drIntraDerivative[pAngle])
	} else if pAngle > 90 && pAngle < 180 {
		dx = int(drIntraDerivative[180-pAngle])
	}
	if pAngle > 90 && pAngle < 180 {
		dy = int(drIntraDerivative[pAngle-90])
	} else if pAngle > 180 {
		dy = int(drIntraDerivative[270-pAngle])
	}
	for i := 0; i < h; i++ {
		for j := 0; j < w; j++ {
			var v int32
			switch {
			case pAngle < 90:
				idx := (i + 1) * dx
				base := idx>>(6-upA) + j<<upA
				shift := int32((idx<<upA)>>1) & 0x1f
				maxBase := (w + h - 1) << upA
				if base < maxBase {
					v = (above[edgeOff+base]*(32-shift) + above[edgeOff+base+1]*shift + 16) >> 5
				} else {
					v = above[edgeOff+maxBase]
				}
			case pAngle > 90 && pAngle < 180:
				idx := j<<6 - (i+1)*dx
				base := idx >> (6 - upA)
				if base >= -(1 << upA) {
					shift := int32((idx<<upA)>>1) & 0x1f
					v = (above[edgeOff+base]*(32-shift) + above[edgeOff+base+1]*shift + 16) >> 5
				} else {
					idx = i<<6 - (j+1)*dy
					base = idx >> (6 - upL)
					shift := int32((idx<<upL)>>1) & 0x1f
					v = (left[edgeOff+base]*(32-shift) + left[edgeOff+base+1]*shift + 16) >> 5
				}
			case pAngle > 180:
				idx := (j + 1) * dy
				base := idx>>(6-upL) + i<<upL
				shift := int32((idx<<upL)>>1) & 0x1f
				v = (left[edgeOff+base]*(32-shift) + left[edgeOff+base+1]*shift + 16) >> 5
			case pAngle == 90:
				v = above[edgeOff+j]
			default:
				v = left[edgeOff+i]
			}
			pred[i*w+j] = v
		}
	}
}

// filterIntraPred is the recursive intra prediction process.
func (t *tile) filterIntraPred(pred []int32, w, h int, maxV int32) {
	e := &t.edges
	above, left := e.above[:], e.left[:]
	taps := &intraFilterTaps[t.b.filterIntraMode]
	var p [7]int32
	for i2 := 0; i2 < h>>1; i2++ {
		for j4 := 0; j4 < w>>2; j4++ {
			for i := 0; i < 7; i++ {
				if i < 5 {
					switch {
					case i2 == 0:
						p[i] = above[edgeOff+j4<<2+i-1]
					case j4 == 0 && i == 0:
						p[i] = left[edgeOff+i2<<1-1]
					default:
						p[i] = pred[(i2<<1-1)*w+j4<<2+i-1]
					}
				} else {
					if j4 == 0 {
						p[i] = left[edgeOff+i2<<1+i-5]
					} else {
						p[i] = pred[(i2<<1+i-5)*w+j4<<2-1]
					}
				}
			}
			for i := 0; i < 8; i++ {
				pr := int32(0)
				for j := 0; j < 7; j++ {
					pr += taps[i][j] * p[j]
				}
				// Round2Signed(pr, 4)
				if pr >= 0 {
					pr = (pr + 8) >> 4
				} else {
					pr = -((-pr + 8) >> 4)
				}
				pred[(i2<<1+i>>2)*w+j4<<2+i&3] = min(max(pr, 0), maxV)
			}
		}
	}
}

// predictPalette is predict_palette.
func (t *tile) predictPalette(plane, startX, startY, x, y, tx int) {
	b := &t.b
	w, h := txW[tx], txH[tx]
	pal := &b.paletteColors[plane]
	m := &b.colorMap[min(plane, 1)]
	pred := t.edges.pred[:w*h]
	for i := 0; i < h; i++ {
		for j := 0; j < w; j++ {
			pred[i*w+j] = int32(pal[m[(y*4+i)*64+x*4+j]])
		}
	}
	t.f.cur.planes[plane].store(startX, startY, w, h, pred)
}

// predictCfl is predict_chroma_from_luma (spec 7.11.5).
func (t *tile) predictCfl(plane, startX, startY, tx int) {
	b := &t.b
	f := t.f
	w, h := txW[tx], txH[tx]
	alpha := int32(b.cflAlphaU)
	if plane == 2 {
		alpha = int32(b.cflAlphaV)
	}
	luma := &f.cur.planes[0]
	pb := &f.cur.planes[plane]
	l := t.edges.pred[:w*h]
	var sum int32
	for i := 0; i < h; i++ {
		ly := min((startY+i)<<1, b.maxLumaH-2)
		for j := 0; j < w; j++ {
			lx := min((startX+j)<<1, b.maxLumaW-2)
			v := (luma.get(lx, ly) + luma.get(lx+1, ly) + luma.get(lx, ly+1) + luma.get(lx+1, ly+1)) << 1
			l[i*w+j] = v
			sum += v
		}
	}
	sh := uint(txWLog2[tx] + txHLog2[tx])
	avg := (sum + 1<<(sh-1)) >> sh
	maxV := int32(1)<<t.seq.BitDepth - 1
	for i := 0; i < h; i++ {
		for j := 0; j < w; j++ {
			d := alpha * (l[i*w+j] - avg)
			if d >= 0 {
				d = (d + 32) >> 6
			} else {
				d = -((-d + 32) >> 6)
			}
			pb.set(startX+j, startY+i, min(max(pb.get(startX+j, startY+i)+d, 0), maxV))
		}
	}
}
