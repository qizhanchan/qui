package av1

// Inter prediction (spec 7.11.3).

// mcScratch holds prediction buffers.
type mcScratch struct {
	preds [2][128 * 128]int32
	mask  [128 * 128]int32
	inter [(2*128 + 16) * 128]int32
	obmc  [64 * 64]int32
}

// refPic describes a prediction source.
type refPic struct {
	f         *frame
	upscaledW int
	height    int
}

func (t *tile) refPicture(ref int) refPic {
	if t.b.useIntraBC {
		f := t.f
		return refPic{f: f.cur, upscaledW: f.miCols * 4, height: f.miRows * 4}
	}
	slot := &t.f.refs[ref]
	return refPic{f: slot.pic, upscaledW: slot.upscaledWidth, height: slot.frameHeight}
}

// roundVars is the rounding variables derivation process.
func (t *tile) roundVars(compound bool) (r0, r1, post uint) {
	r0, r1 = 3, 11
	if compound {
		r1 = 7
	}
	if t.seq.BitDepth == 12 {
		r0 = 5
		if !compound {
			r1 = 9
		}
	}
	return r0, r1, 14 - r0 - r1
}

// scaleMv is the motion vector scaling process (spec 7.11.3.3).
func (t *tile) scaleMv(rp refPic, plane, x, y int, m mv) (startX, startY, xStep, yStep int) {
	h := t.h
	ss := min(plane, 1)
	xScale := ((rp.upscaledW << 14) + h.Width/2) / h.Width
	yScale := ((rp.height << 14) + h.Height/2) / h.Height
	if t.b.useIntraBC {
		// The current frame is never scaled; its MiCols / MiRows size
		// only sets the clamping edges.
		xScale, yScale = 1<<14, 1<<14
	}
	origX := (x << 4) + ((2 * int(m.col)) >> ss) + 8
	origY := (y << 4) + ((2 * int(m.row)) >> ss) + 8
	baseX := int64(origX)*int64(xScale) - 8<<14
	baseY := int64(origY)*int64(yScale) - 8<<14
	startX = int(round2Signed(baseX, 8)) + 32
	startY = int(round2Signed(baseY, 8)) + 32
	xStep = int(round2Signed(int64(xScale), 4))
	yStep = int(round2Signed(int64(yScale), 4))
	return
}

func filterIndex(f, size int) int {
	if size <= 4 {
		switch f {
		case filterEightTap, filterEightTapSharp:
			return 4
		case filterEightTapSmooth:
			return 5
		}
	}
	return f
}

// blockInterPred is the block inter prediction process (spec 7.11.3.4).
func (t *tile) blockInterPred(rp refPic, plane, x, y, xStep, yStep, w, h int, interp [2]uint8, r0, r1 uint, pred []int32) {
	ss := min(plane, 1)
	pb := &rp.f.planes[plane]
	lastX := (rp.upscaledW+ss)>>ss - 1
	lastY := (rp.height+ss)>>ss - 1
	interH := (((h-1)*yStep + 1<<10 - 1) >> 10) + 8
	fx := &subpelFilters[filterIndex(int(interp[1]), w)]
	fy := &subpelFilters[filterIndex(int(interp[0]), h)]
	inter := t.mc.inter[:interH*w]
	rnd0 := int32(1) << (r0 - 1)
	for r := 0; r < interH; r++ {
		sy := clip3(0, lastY, (y>>10)+r-3)
		for c := 0; c < w; c++ {
			p := x + xStep*c
			k := &fx[(p>>6)&15]
			s := int32(0)
			px := (p >> 10) - 3
			if pb.pix16 != nil {
				row := pb.pix16[sy*pb.stride:]
				for k8 := 0; k8 < 8; k8++ {
					s += k[k8] * int32(row[clip3(0, lastX, px+k8)])
				}
			} else {
				row := pb.pix8[sy*pb.stride:]
				for k8 := 0; k8 < 8; k8++ {
					s += k[k8] * int32(row[clip3(0, lastX, px+k8)])
				}
			}
			inter[r*w+c] = (s + rnd0) >> r0
		}
	}
	rnd1 := int32(1) << (r1 - 1)
	for r := 0; r < h; r++ {
		p := (y & 1023) + yStep*r
		k := &fy[(p>>6)&15]
		base := (p >> 10) * w
		for c := 0; c < w; c++ {
			s := int32(0)
			for k8 := 0; k8 < 8; k8++ {
				s += k[k8] * inter[base+k8*w+c]
			}
			pred[r*w+c] = (s + rnd1) >> r1
		}
	}
}

// resolveDivisor is resolve_divisor (spec 7.11.3.7).
func resolveDivisor(d int64) (shift uint, factor int64) {
	a := d
	if a < 0 {
		a = -a
	}
	n := 0
	for v := a; v > 1; v >>= 1 {
		n++
	}
	e := a - int64(1)<<n
	var f int64
	if n > 8 {
		f = round2Signed(e, uint(n-8))
	} else {
		f = e << (8 - n)
	}
	shift = uint(n + 14)
	factor = int64(divLut[f])
	if d < 0 {
		factor = -factor
	}
	return
}

// setupShear returns the shear parameters of a warp model and whether it
// is valid (spec 7.11.3.6).
func setupShear(wm *[6]int32) (alpha, beta, gamma, delta int32, valid bool) {
	cl := func(v int64) int64 { return min(max(v, -32768), 32767) }
	alpha0 := cl(int64(wm[2]) - 1<<16)
	beta0 := cl(int64(wm[3]))
	shift, factor := resolveDivisor(int64(wm[2]))
	v := int64(wm[4]) << 16
	gamma0 := cl(round2Signed(v*factor, shift))
	w := int64(wm[3]) * int64(wm[4])
	delta0 := cl(int64(wm[5]) - round2Signed(w*factor, shift) - 1<<16)
	r := func(v int64) int32 { return int32(round2Signed(v, 6) << 6) }
	alpha, beta, gamma, delta = r(alpha0), r(beta0), r(gamma0), r(delta0)
	valid = wm[2] > 0 && 4*absI32(alpha)+7*absI32(beta) < 1<<16 && 4*absI32(gamma)+4*absI32(delta) < 1<<16
	return
}

func absI32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// warpEstimation is the warp estimation process (spec 7.11.3.8).
func (t *tile) warpEstimation() bool {
	b := &t.b
	p := &t.mvp
	var A [2][2]int64
	var Bx, By [2]int64
	midY := b.miRow*4 + b.bh4*2 - 1
	midX := b.miCol*4 + b.bw4*2 - 1
	suy, sux := midY*8, midX*8
	duy, dux := suy+int(b.mv[0].row), sux+int(b.mv[0].col)
	ls := func(a, b int) int64 { return int64((a*b)>>2 + (a + b)) }
	for i := 0; i < p.numSamples; i++ {
		c := &p.candList[i]
		sy, sx := c[0]-suy, c[1]-sux
		dy, dx := c[2]-duy, c[3]-dux
		if iabs(sx-dx) < 256 && iabs(sy-dy) < 256 {
			A[0][0] += ls(sx, sx) + 8
			A[0][1] += ls(sx, sy) + 4
			A[1][1] += ls(sy, sy) + 8
			Bx[0] += ls(sx, dx) + 8
			Bx[1] += ls(sy, dx) + 4
			By[0] += ls(sx, dy) + 4
			By[1] += ls(sy, dy) + 8
		}
	}
	det := A[0][0]*A[1][1] - A[0][1]*A[0][1]
	if det == 0 {
		return false
	}
	shift, factor := resolveDivisor(det)
	sh := int(shift) - 16
	if sh < 0 {
		factor <<= uint(-sh)
		sh = 0
	}
	nondiag := func(v int64) int32 {
		return int32(min(max(round2Signed64Mul(v, factor, uint(sh)), -(1<<13)+1), (1<<13)-1))
	}
	diag := func(v int64) int32 {
		return int32(min(max(round2Signed64Mul(v, factor, uint(sh)), (1<<16)-(1<<13)+1), (1<<16)+(1<<13)-1))
	}
	wm := &p.localWarp
	wm[2] = diag(A[1][1]*Bx[0] - A[0][1]*Bx[1])
	wm[3] = nondiag(-A[0][1]*Bx[0] + A[0][0]*Bx[1])
	wm[4] = nondiag(A[1][1]*By[0] - A[0][1]*By[1])
	wm[5] = diag(-A[0][1]*By[0] + A[0][0]*By[1])
	vx := int64(b.mv[0].col)*(1<<13) - (int64(midX)*(int64(wm[2])-(1<<16)) + int64(midY)*int64(wm[3]))
	vy := int64(b.mv[0].row)*(1<<13) - (int64(midX)*int64(wm[4]) + int64(midY)*(int64(wm[5])-(1<<16)))
	wm[0] = int32(min(max(vx, -(1<<23)), (1<<23)-1))
	wm[1] = int32(min(max(vy, -(1<<23)), (1<<23)-1))
	return true
}

// round2Signed64Mul is Round2Signed(v * f, n) without overflowing.
func round2Signed64Mul(v, f int64, n uint) int64 {
	neg := (v < 0) != (f < 0)
	if v < 0 {
		v = -v
	}
	if f < 0 {
		f = -f
	}
	hi, lo := mul64(uint64(v), uint64(f))
	// (hi:lo + 2^(n-1)) >> n
	if n > 0 {
		add := uint64(1) << (n - 1)
		lo2 := lo + add
		if lo2 < lo {
			hi++
		}
		lo = lo2
		lo = lo>>n | hi<<(64-n)
		hi >>= n
	}
	r := int64(lo)
	if hi != 0 || r < 0 {
		r = 1<<62 - 1
	}
	if neg {
		return -r
	}
	return r
}

func mul64(a, b uint64) (hi, lo uint64) {
	const mask32 = 1<<32 - 1
	a0, a1 := a&mask32, a>>32
	b0, b1 := b&mask32, b>>32
	w0 := a0 * b0
	t := a1*b0 + w0>>32
	w1 := t&mask32 + a0*b1
	hi = a1*b1 + t>>32 + w1>>32
	lo = a * b
	return
}

// blockWarp is the block warp process (spec 7.11.3.5) for the 8x8 block
// (i8, j8) of a w x h prediction.
func (t *tile) blockWarp(rp refPic, wm *[6]int32, plane, x, y, i8, j8, w, h int, r0, r1 uint, pred []int32) {
	ss := uint(min(plane, 1))
	pb := &rp.f.planes[plane]
	lastX := (rp.upscaledW+int(ss))>>ss - 1
	lastY := (rp.height+int(ss))>>ss - 1
	srcX := (x + j8*8 + 4) << ss
	srcY := (y + i8*8 + 4) << ss
	dstX := int64(wm[2])*int64(srcX) + int64(wm[3])*int64(srcY) + int64(wm[0])
	dstY := int64(wm[4])*int64(srcX) + int64(wm[5])*int64(srcY) + int64(wm[1])
	alpha, beta, gamma, delta, _ := setupShear(wm)
	x4, y4 := dstX>>ss, dstY>>ss
	ix4 := int(x4 >> 16)
	sx4 := int32(x4&0xffff) &^ 63
	iy4 := int(y4 >> 16)
	sy4 := int32(y4&0xffff) &^ 63
	var inter [15][8]int32
	rnd0 := int32(1) << (r0 - 1)
	for i1 := -7; i1 < 8; i1++ {
		sy := clip3(0, lastY, iy4+i1)
		for i2 := -4; i2 < 4; i2++ {
			sx := sx4 + alpha*int32(i2) + beta*int32(i1)
			offs := (sx+512)>>10 + 64
			k := &warpedFilters[offs]
			s := int32(0)
			for i3 := 0; i3 < 8; i3++ {
				s += k[i3] * pb.get(clip3(0, lastX, ix4+i2-3+i3), sy)
			}
			inter[i1+7][i2+4] = (s + rnd0) >> r0
		}
	}
	rnd1 := int32(1) << (r1 - 1)
	for i1 := -4; i1 < min(4, h-i8*8-4); i1++ {
		for i2 := -4; i2 < min(4, w-j8*8-4); i2++ {
			sy := sy4 + gamma*int32(i2) + delta*int32(i1)
			offs := (sy+512)>>10 + 64
			k := &warpedFilters[offs]
			s := int32(0)
			for i3 := 0; i3 < 8; i3++ {
				s += k[i3] * inter[i1+i3+4][i2+4]
			}
			pred[(i8*8+i1+4)*w+j8*8+i2+4] = (s + rnd1) >> r1
		}
	}
}

// Wedge masks (spec 7.11.3.11).
var wedgeMasks [blockSizes][2][16][]int32

func wedgeShape(b int) int {
	switch {
	case blockH(b) > blockW(b):
		return 1
	case blockH(b) < blockW(b):
		return 2
	}
	return 0
}

func initWedgeMasks() {
	const n = 64
	var master [6][n][n]int32
	const (
		wHoriz = 0
		wVert  = 1
		w27    = 2
		w63    = 3
		w117   = 4
		w153   = 5
	)
	for j := 0; j < n; j++ {
		shift := n / 4
		for i := 0; i < n; i += 2 {
			master[w63][i][j] = wedgeMasterObliqueEven[clip3(0, n-1, j-shift)]
			shift--
			master[w63][i+1][j] = wedgeMasterObliqueOdd[clip3(0, n-1, j-shift)]
			master[wVert][i][j] = wedgeMasterVertical[j]
			master[wVert][i+1][j] = wedgeMasterVertical[j]
		}
	}
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			m := master[w63][i][j]
			master[w27][j][i] = m
			master[w117][i][n-1-j] = 64 - m
			master[w153][n-1-j][i] = 64 - m
			master[wHoriz][j][i] = master[wVert][i][j]
		}
	}
	for b := block8x8; b < blockSizes; b++ {
		if wedgeBits[b] == 0 {
			continue
		}
		w, h := blockW(b), blockH(b)
		for wedge := 0; wedge < 16; wedge++ {
			cb := &wedgeCodebook[wedgeShape(b)][wedge]
			dir := cb[0]
			xoff := n/2 - (cb[1]*w)>>3
			yoff := n/2 - (cb[2]*h)>>3
			sum := int32(0)
			for i := 0; i < w; i++ {
				sum += master[dir][yoff][xoff+i]
			}
			for i := 1; i < h; i++ {
				sum += master[dir][yoff+i][xoff]
			}
			avg := (sum + int32(w+h-1)/2) / int32(w+h-1)
			flip := 0
			if avg < 32 {
				flip = 1
			}
			m0 := make([]int32, w*h)
			m1 := make([]int32, w*h)
			for i := 0; i < h; i++ {
				for j := 0; j < w; j++ {
					v := master[dir][yoff+i][xoff+j]
					m0[i*w+j] = v
					m1[i*w+j] = 64 - v
				}
			}
			wedgeMasks[b][flip][wedge] = m0
			wedgeMasks[b][1-flip][wedge] = m1
		}
	}
}

var (
	quantDistWeight = [4][2]int{{2, 3}, {2, 5}, {2, 7}, {1, 31}}
	quantDistLookup = [4][2]int32{{9, 7}, {11, 5}, {12, 4}, {13, 3}}
)

// distWeights is the distance weights process (spec 7.11.3.15).
func (t *tile) distWeights(m *miInfo) (fwd, bck int32) {
	h := t.h
	var dist [2]int
	for l := 0; l < 2; l++ {
		dist[l] = clip3(0, 31, iabs(t.seq.relDist(h.orderHints[m.refFrame[l]], h.OrderHint)))
	}
	d0, d1 := dist[1], dist[0]
	order := 0
	if d0 <= d1 {
		order = 1
	}
	if d0 == 0 || d1 == 0 {
		return quantDistLookup[3][order], quantDistLookup[3][1-order]
	}
	i := 0
	for ; i < 3; i++ {
		c0, c1 := quantDistWeight[i][order], quantDistWeight[i][1-order]
		if order == 1 {
			if d0*c0 < d1*c1 {
				break
			}
		} else if d0*c0 > d1*c1 {
			break
		}
	}
	return quantDistLookup[i][order], quantDistLookup[i][1-order]
}

// predictInter is predict_inter (spec 7.11.3.1) for a w x h region of
// plane at (baseX, baseY) using the motion of block (candRow, candCol).
func (t *tile) predictInter(plane, baseX, baseY, w, h, candRow, candCol int) {
	b := &t.b
	hdr := t.h
	f := t.f
	m := t.mi(candRow, candCol)
	compound := m.refFrame[1] > intraFrame
	r0, r1, post := t.roundVars(compound)
	n := 1
	if compound {
		n = 2
	}
	pb := &f.cur.planes[plane]
	maxV := int32(1)<<t.seq.BitDepth - 1
	for l := 0; l < n; l++ {
		ref := int(m.refFrame[l])
		rp := t.refPicture(ref)
		pred := t.mc.preds[l][:w*h]
		useWarp := 0
		var wm *[6]int32
		if w >= 8 && h >= 8 && !hdr.forceIntegerMV {
			switch {
			case b.motionMode == motionLocalWarp && t.mvp.localValid:
				useWarp, wm = 1, &t.mvp.localWarp
			case (b.yMode == globalMV || b.yMode == globalGlobalMV) && hdr.gmType[ref] > gmTranslation && !f.scaled[ref] && f.gmValid[ref]:
				useWarp, wm = 2, &hdr.gmParams[ref]
			}
		}
		if useWarp != 0 {
			for i8 := 0; i8 <= (h-1)>>3; i8++ {
				for j8 := 0; j8 <= (w-1)>>3; j8++ {
					t.blockWarp(rp, wm, plane, baseX, baseY, i8, j8, w, h, r0, r1, pred)
				}
			}
			continue
		}
		sx, sy, xs, ys := t.scaleMv(rp, plane, baseX, baseY, m.mv[l])
		t.blockInterPred(rp, plane, sx, sy, xs, ys, w, h, m.interp, r0, r1, pred)
	}
	p0, p1 := t.mc.preds[0][:w*h], t.mc.preds[1][:w*h]
	ss := min(plane, 1)
	switch {
	case b.compoundType == compoundWedge && plane == 0:
		copy(t.mc.mask[:], wedgeMasks[b.size][b.wedgeSign][b.wedgeIndex])
	case b.compoundType == compoundIntra:
		scale := 128 / max(w, h)
		for i := 0; i < h; i++ {
			for j := 0; j < w; j++ {
				v := int32(32)
				switch b.interIntraMode {
				case 1: // II_V_PRED
					v = iiWeights1d[i*scale]
				case 2: // II_H_PRED
					v = iiWeights1d[j*scale]
				case 3: // II_SMOOTH_PRED
					v = iiWeights1d[min(i, j)*scale]
				}
				t.mc.mask[i*w+j] = v
			}
		}
	case b.compoundType == compoundDiffWtd && plane == 0:
		sh := uint(t.seq.BitDepth-8) + post
		for i := 0; i < w*h; i++ {
			d := absI32(p0[i] - p1[i])
			d = (d + int32(1)<<sh>>1) >> sh
			v := min(max(38+d/16, 0), 64)
			if b.maskType != 0 {
				v = 64 - v
			}
			t.mc.mask[i] = v
		}
	}
	isII := b.isInter && b.refFrame[1] == intraFrame
	rndPost := func(v int32, n uint) int32 {
		if n == 0 {
			return v
		}
		return (v + 1<<(n-1)) >> n
	}
	switch {
	case !compound && !isII:
		for i := 0; i < h; i++ {
			for j := 0; j < w; j++ {
				pb.set(baseX+j, baseY+i, min(max(rndPost(p0[i*w+j], post), 0), maxV))
			}
		}
	case compound && b.compoundType == compoundAverage:
		for i := 0; i < h; i++ {
			for j := 0; j < w; j++ {
				pb.set(baseX+j, baseY+i, min(max(rndPost(p0[i*w+j]+p1[i*w+j], 1+post), 0), maxV))
			}
		}
	case compound && b.compoundType == compoundDistance:
		fw, bw := t.distWeights(m)
		for i := 0; i < h; i++ {
			for j := 0; j < w; j++ {
				pb.set(baseX+j, baseY+i, min(max(rndPost(fw*p0[i*w+j]+bw*p1[i*w+j], 4+post), 0), maxV))
			}
		}
	default:
		// Mask blend (spec 7.11.3.14); the luma mask is subsampled for
		// chroma except for the non-wedge inter-intra mask.
		mw := w << ss
		full := ss == 0 || (isII && !b.wedgeInterIntra)
		if full {
			mw = w
		}
		mk := t.mc.mask[:]
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				var mv int32
				if full {
					mv = mk[y*mw+x]
				} else {
					mv = (mk[2*y*mw+2*x] + mk[2*y*mw+2*x+1] + mk[(2*y+1)*mw+2*x] + mk[(2*y+1)*mw+2*x+1] + 2) >> 2
				}
				if isII {
					pr0 := min(max(rndPost(p0[y*w+x], post), 0), maxV)
					pr1 := pb.get(baseX+x, baseY+y)
					pb.set(baseX+x, baseY+y, (mv*pr1+(64-mv)*pr0+32)>>6)
				} else {
					pb.set(baseX+x, baseY+y, min(max(rndPost(mv*p0[y*w+x]+(64-mv)*p1[y*w+x], 6+post), 0), maxV))
				}
			}
		}
	}
	if b.motionMode == motionOBMC {
		t.obmc(plane, w, h)
	}
}

func obmcMask(n int) []int32 {
	switch n {
	case 2:
		return obmcMasks[0]
	case 4:
		return obmcMasks[1]
	case 8:
		return obmcMasks[2]
	case 16:
		return obmcMasks[3]
	}
	return obmcMasks[4]
}

// obmc is the overlapped motion compensation process (spec 7.11.3.10).
func (t *tile) obmc(plane, w, h int) {
	b := &t.b
	f := t.f
	ss := min(plane, 1)
	psz := b.size
	if plane > 0 {
		psz = chromaSize[psz]
	}
	pb := &f.cur.planes[plane]
	maxV := int32(1)<<t.seq.BitDepth - 1
	r0, r1, post := t.roundVars(false)
	overlap := func(candRow, candCol, predX, predY, predW, predH int, mask []int32, above bool) {
		m := t.mi(candRow, candCol)
		ref := int(m.refFrame[0])
		rp := t.refPicture(ref)
		sx, sy, xs, ys := t.scaleMv(rp, plane, predX, predY, m.mv[0])
		pred := t.mc.obmc[:predW*predH]
		t.blockInterPred(rp, plane, sx, sy, xs, ys, predW, predH, m.interp, r0, r1, pred)
		for i := 0; i < predH; i++ {
			for j := 0; j < predW; j++ {
				op := min(max((pred[i*predW+j]+int32(1)<<post>>1)>>post, 0), maxV)
				var mk int32
				if above {
					mk = mask[i]
				} else {
					mk = mask[j]
				}
				cur := pb.get(predX+j, predY+i)
				pb.set(predX+j, predY+i, (mk*cur+(64-mk)*op+32)>>6)
			}
		}
	}
	if b.availU && !(plane > 0 && (psz == block4x4 || psz == block4x8 || psz == block8x4)) {
		count, limit := 0, min(4, miWLog2[b.size])
		for x4 := b.miCol; count < limit && x4 < min(f.miCols, b.miCol+b.bw4); {
			candRow, candCol := b.miRow-1, x4|1
			m := t.mi(candRow, candCol)
			step := clip3(2, 16, num4x4W[m.size])
			if m.refFrame[0] > intraFrame {
				count++
				predW := min(w, (step*4)>>ss)
				predH := min(h>>1, 32>>ss)
				overlap(candRow, candCol, (x4*4)>>ss, (b.miRow*4)>>ss, predW, predH, obmcMask(predH), true)
			}
			x4 += step
		}
	}
	if b.availL {
		count, limit := 0, min(4, miHLog2[b.size])
		for y4 := b.miRow; count < limit && y4 < min(f.miRows, b.miRow+b.bh4); {
			candCol, candRow := b.miCol-1, y4|1
			m := t.mi(candRow, candCol)
			step := clip3(2, 16, num4x4H[m.size])
			if m.refFrame[0] > intraFrame {
				count++
				predW := min(w>>1, 32>>ss)
				predH := min(h, (step*4)>>ss)
				overlap(candRow, candCol, (b.miCol*4)>>ss, (y4*4)>>ss, predW, predH, obmcMask(predW), false)
			}
			y4 += step
		}
	}
}

// computePrediction runs the prediction of an inter block (intra blocks
// predict per transform block in residual) (spec 5.11.33 / 7.11.1).
func (t *tile) computePrediction() error {
	b := &t.b
	if !b.isInter {
		return nil
	}
	f := t.f
	if b.motionMode == motionLocalWarp {
		t.mvp.localValid = t.warpEstimation()
		if t.mvp.localValid {
			_, _, _, _, t.mvp.localValid = setupShear(&t.mvp.localWarp)
		}
	}
	sbMask := 15
	if t.seq.Use128x128 {
		sbMask = 31
	}
	sbRow, sbCol := b.miRow&sbMask, b.miCol&sbMask
	planes := 1
	if b.hasChroma {
		planes = 3
	}
	for plane := 0; plane < planes; plane++ {
		ss := min(plane, 1)
		psz := b.size
		if plane > 0 {
			psz = chromaSize[psz]
		}
		n4w, n4h := num4x4W[psz], num4x4H[psz]
		baseX := (b.miCol >> ss) * 4
		baseY := (b.miRow >> ss) * 4
		candRow := (b.miRow >> ss) << ss
		candCol := (b.miCol >> ss) << ss
		if b.refFrame[1] == intraFrame { // inter-intra
			mode := [4]int{dcPred, vPred, hPred, smoothPred}[b.interIntraMode]
			availL, availU := b.availL, b.availU
			if plane > 0 {
				availL, availU = b.availLC, b.availUC
			}
			bd := &t.blockDecoded[plane]
			aboveRt := bd[(sbRow>>ss)-1+1][(sbCol>>ss)+n4w+1]
			belowLt := bd[(sbRow>>ss)+n4h+1][(sbCol>>ss)-1+1]
			t.predictIntra(plane, baseX, baseY, availL, availU, aboveRt, belowLt, mode, 2+miWLog2[psz], 2+miHLog2[psz])
		}
		predW, predH := blockW(b.size)>>ss, blockH(b.size)>>ss
		someIntra := false
		for r := 0; r < n4h<<ss; r++ {
			for c := 0; c < n4w<<ss; c++ {
				if candRow+r < f.miRows && candCol+c < f.miCols && t.mi(candRow+r, candCol+c).refFrame[0] == intraFrame {
					someIntra = true
				}
			}
		}
		if someIntra {
			predW, predH = n4w*4, n4h*4
			candRow, candCol = b.miRow, b.miCol
		}
		r := 0
		for y := 0; y < n4h*4; y += predH {
			c := 0
			for x := 0; x < n4w*4; x += predW {
				t.predictInter(plane, baseX+x, baseY+y, predW, predH, candRow+r, candCol+c)
				c++
			}
			r++
		}
	}
	return nil
}
