package vp9

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// Loop filter, a port of libvpx vp9_loopfilter.c (masks per 64x64
// superblock) and vpx_dsp/loopfilter.c (the filters).

type lfThresh struct{ mblim, lim, hevThr uint8 }

type lfLevels struct {
	lvl [8][4][2]uint8
	thr [64]lfThresh
}

type lfMask struct {
	leftY, aboveY   [4]uint64
	int4x4Y         uint64
	leftUV, aboveUV [4]uint16
	int4x4UV        uint16
	lflY            [64]uint8
}

func (d *Decoder) lfFrameInit() {
	lf := &d.lf
	li := &d.lfInfo
	for lvl := 0; lvl < 64; lvl++ {
		shift := 0
		if lf.sharpness > 0 {
			shift++
		}
		if lf.sharpness > 4 {
			shift++
		}
		lim := lvl >> shift
		if lf.sharpness > 0 && lim > 9-lf.sharpness {
			lim = 9 - lf.sharpness
		}
		lim = max(lim, 1)
		li.thr[lvl] = lfThresh{uint8(2*(lvl+2) + lim), uint8(lim), uint8(lvl >> 4)}
	}
	def := lf.level
	scale := 1 << (def >> 5)
	for s := 0; s < 8; s++ {
		lvlSeg := def
		if d.seg.active(s, segLvlAltLF) {
			v := d.seg.featureData[s][segLvlAltLF]
			if !d.seg.absDelta {
				v += def
			}
			lvlSeg = min(max(v, 0), 63)
		}
		if !lf.deltaEnabled {
			for r := 0; r < 4; r++ {
				li.lvl[s][r] = [2]uint8{uint8(lvlSeg), uint8(lvlSeg)}
			}
			continue
		}
		li.lvl[s][0][0] = uint8(min(max(lvlSeg+lf.refDeltas[0]*scale, 0), 63))
		for r := 1; r < 4; r++ {
			for m := 0; m < 2; m++ {
				li.lvl[s][r][m] = uint8(min(max(lvlSeg+lf.refDeltas[r]*scale+lf.modeDeltas[m]*scale, 0), 63))
			}
		}
	}
}

func (d *Decoder) filterLevel(b *blockInfo) uint8 {
	r := int(b.ref[0])
	if r < 0 {
		r = 0
	}
	return d.lfInfo.lvl[b.seg][r][modeLFLut[b.mode]]
}

func (d *Decoder) buildMasks(b *blockInfo, shiftY, shiftUV int, withUV bool, m *lfMask) {
	bs := int(b.bsize)
	txY := int(b.tx)
	txUV := int(uvTxsizeLookup[bs][txY][1][1])
	level := d.filterLevel(b)
	if level == 0 {
		return
	}
	w, h := int(num8x8W[bs]), int(num8x8H[bs])
	for i := 0; i < h; i++ {
		for j := 0; j < w; j++ {
			if k := shiftY + i*8 + j; k < 64 {
				m.lflY[k] = level
			}
		}
	}
	m.aboveY[txY] |= abovePredMask[bs] << shiftY
	m.leftY[txY] |= leftPredMask[bs] << shiftY
	if withUV {
		m.aboveUV[txUV] |= abovePredMaskUV[bs] << shiftUV
		m.leftUV[txUV] |= leftPredMaskUV[bs] << shiftUV
	}
	if b.skip && b.isInter {
		return
	}
	m.aboveY[txY] |= (sizeMask[bs] & above64x64TxMask[txY]) << shiftY
	m.leftY[txY] |= (sizeMask[bs] & left64x64TxMask[txY]) << shiftY
	if txY == 0 {
		m.int4x4Y |= sizeMask[bs] << shiftY
	}
	if withUV {
		m.aboveUV[txUV] |= (sizeMaskUV[bs] & above64x64TxMaskUV[txUV]) << shiftUV
		m.leftUV[txUV] |= (sizeMaskUV[bs] & left64x64TxMaskUV[txUV]) << shiftUV
		if txUV == 0 {
			m.int4x4UV |= sizeMaskUV[bs] << shiftUV
		}
	}
}

// setupMask is vp9_setup_mask for the superblock at (miRow, miCol).
func (d *Decoder) setupMask(miRow, miCol int, m *lfMask) {
	*m = lfMask{}
	maxRows := min(8, d.miRows-miRow)
	maxCols := min(8, d.miCols-miCol)
	for r := 0; r < maxRows; r++ {
		for c := 0; c < maxCols; c++ {
			b := d.mi[(miRow+r)*d.miStride+miCol+c]
			if b == nil {
				continue
			}
			// Only at the block's top-left cell.
			if r > 0 && d.mi[(miRow+r-1)*d.miStride+miCol+c] == b {
				continue
			}
			if c > 0 && d.mi[(miRow+r)*d.miStride+miCol+c-1] == b {
				continue
			}
			withUV := r&1 == 0 && c&1 == 0
			d.buildMasks(b, r*8+c, (r>>1)*4+(c>>1), withUV, m)
		}
	}
	m.leftY[2] |= m.leftY[3]
	m.aboveY[2] |= m.aboveY[3]
	m.leftUV[2] |= m.leftUV[3]
	m.aboveUV[2] |= m.aboveUV[3]

	const leftBorder = 0x1111111111111111
	const aboveBorder = 0x000000ff000000ff
	const leftBorderUV = 0x1111
	const aboveBorderUV = 0x000f
	m.leftY[1] |= m.leftY[0] & leftBorder
	m.leftY[0] &^= leftBorder
	m.aboveY[1] |= m.aboveY[0] & aboveBorder
	m.aboveY[0] &^= aboveBorder
	m.leftUV[1] |= m.leftUV[0] & leftBorderUV
	m.leftUV[0] &^= leftBorderUV
	m.aboveUV[1] |= m.aboveUV[0] & aboveBorderUV
	m.aboveUV[0] &^= aboveBorderUV

	if miRow+8 > d.miRows {
		rows := uint(d.miRows - miRow)
		maskY := uint64(1)<<(rows<<3) - 1
		maskUV := uint16(uint32(1)<<(((rows+1)>>1)<<2) - 1)
		for i := 0; i < 3; i++ {
			m.leftY[i] &= maskY
			m.aboveY[i] &= maskY
			m.leftUV[i] &= maskUV
			m.aboveUV[i] &= maskUV
		}
		m.int4x4Y &= maskY
		m.int4x4UV &= maskUV
		if rows == 1 {
			m.aboveUV[1] |= m.aboveUV[2]
			m.aboveUV[2] = 0
		}
		if rows == 5 {
			m.aboveUV[1] |= m.aboveUV[2] & 0xff00
			m.aboveUV[2] &^= m.aboveUV[2] & 0xff00
		}
	}
	if miCol+8 > d.miCols {
		cols := uint(d.miCols - miCol)
		maskY := (uint64(1)<<cols - 1) * 0x0101010101010101
		maskUV := uint16((uint32(1)<<((cols+1)>>1) - 1) * 0x1111)
		maskUVInt := uint16((uint32(1)<<(cols>>1) - 1) * 0x1111)
		for i := 0; i < 3; i++ {
			m.leftY[i] &= maskY
			m.aboveY[i] &= maskY
			m.leftUV[i] &= maskUV
			m.aboveUV[i] &= maskUV
		}
		m.int4x4Y &= maskY
		m.int4x4UV &= maskUVInt
		if cols == 1 {
			m.leftUV[1] |= m.leftUV[2]
			m.leftUV[2] = 0
		}
		if cols == 5 {
			m.leftUV[1] |= m.leftUV[2] & 0xcccc
			m.leftUV[2] &^= m.leftUV[2] & 0xcccc
		}
	}
	if miCol == 0 {
		for i := 0; i < 3; i++ {
			m.leftY[i] &= 0xfefefefefefefefe
			m.leftUV[i] &= 0xeeee
		}
	}
}

// filterSB filters one superblock: luma then both chroma planes, each
// vertical edges first, edges left to right / top to bottom.
func (d *Decoder) filterSB(miRow, miCol int, m *lfMask) {
	f := d.cur
	thr := &d.lfInfo.thr
	// Luma.
	{
		stride := f.stride[0]
		pix := f.planes[0]
		base := (miRow*8)*stride + miCol*8
		for r := 0; r < 8 && miRow+r < d.miRows; r++ {
			sh := uint(r * 8)
			m16 := uint8(m.leftY[2] >> sh)
			m8 := uint8(m.leftY[1] >> sh)
			m4 := uint8(m.leftY[0] >> sh)
			mi := uint8(m.int4x4Y >> sh)
			row := base + r*8*stride
			for c := 0; c < 8; c++ {
				bit := uint8(1) << c
				if (m16|m8|m4|mi)&bit == 0 {
					continue
				}
				t := &thr[m.lflY[r*8+c]]
				o := row + c*8
				edge(pix, o, stride, 1, m16&bit != 0, m8&bit != 0, m4&bit != 0, t)
				if mi&bit != 0 {
					lpf4(pix, o+4, stride, 1, t)
				}
			}
		}
		for r := 0; r < 8 && miRow+r < d.miRows; r++ {
			sh := uint(r * 8)
			var m16, m8, m4 uint8
			if miRow+r != 0 {
				m16 = uint8(m.aboveY[2] >> sh)
				m8 = uint8(m.aboveY[1] >> sh)
				m4 = uint8(m.aboveY[0] >> sh)
			}
			mi := uint8(m.int4x4Y >> sh)
			row := base + r*8*stride
			for c := 0; c < 8; c++ {
				bit := uint8(1) << c
				if (m16|m8|m4|mi)&bit == 0 {
					continue
				}
				t := &thr[m.lflY[r*8+c]]
				o := row + c*8
				edge(pix, o, 1, stride, m16&bit != 0, m8&bit != 0, m4&bit != 0, t)
				if mi&bit != 0 {
					lpf4(pix, o+4*stride, 1, stride, t)
				}
			}
		}
	}
	// Chroma (4:2:0): 4x4 cells of 8x8 chroma pixels.
	var lfl [16]uint8
	for r := 0; r < 4; r++ {
		for c := 0; c < 4; c++ {
			lfl[r*4+c] = m.lflY[(r*2)*8+c*2]
		}
	}
	for p := 1; p < 3; p++ {
		stride := f.stride[p]
		pix := f.planes[p]
		base := (miRow*4)*stride + miCol*4
		for r := 0; r < 4 && miRow+r*2 < d.miRows; r++ {
			sh := uint(r * 4)
			m16 := uint8(m.leftUV[2]>>sh) & 0xf
			m8 := uint8(m.leftUV[1]>>sh) & 0xf
			m4 := uint8(m.leftUV[0]>>sh) & 0xf
			mi := uint8(m.int4x4UV>>sh) & 0xf
			row := base + r*8*stride
			for c := 0; c < 4; c++ {
				bit := uint8(1) << c
				if (m16|m8|m4|mi)&bit == 0 {
					continue
				}
				t := &thr[lfl[r*4+c]]
				o := row + c*8
				edge(pix, o, stride, 1, m16&bit != 0, m8&bit != 0, m4&bit != 0, t)
				if mi&bit != 0 {
					lpf4(pix, o+4, stride, 1, t)
				}
			}
		}
		for r := 0; r < 4 && miRow+r*2 < d.miRows; r++ {
			sh := uint(r * 4)
			var m16, m8, m4 uint8
			if miRow+r*2 != 0 {
				m16 = uint8(m.aboveUV[2]>>sh) & 0xf
				m8 = uint8(m.aboveUV[1]>>sh) & 0xf
				m4 = uint8(m.aboveUV[0]>>sh) & 0xf
			}
			mi := uint8(m.int4x4UV>>sh) & 0xf
			if miRow+r*2 == d.miRows-1 {
				mi = 0
			}
			row := base + r*8*stride
			for c := 0; c < 4; c++ {
				bit := uint8(1) << c
				if (m16|m8|m4|mi)&bit == 0 {
					continue
				}
				t := &thr[lfl[r*4+c]]
				o := row + c*8
				edge(pix, o, 1, stride, m16&bit != 0, m8&bit != 0, m4&bit != 0, t)
				if mi&bit != 0 {
					lpf4(pix, o+4*stride, 1, stride, t)
				}
			}
		}
	}
}

func edge(pix []byte, o, along, across int, f16, f8, f4 bool, t *lfThresh) {
	switch {
	case f16:
		lpf16(pix, o, along, across, t)
	case f8:
		lpf8(pix, o, along, across, t)
	case f4:
		lpf4(pix, o, along, across, t)
	}
}

// loopFilter filters the frame. Superblock rows run in parallel as a
// wavefront: row r may filter superblock c once row r-1 finished c+1.
func (d *Decoder) loopFilter() {
	d.lfFrameInit()
	sbRows := (d.miRows + 7) >> 3
	sbCols := (d.miCols + 7) >> 3
	workers := min(runtime.GOMAXPROCS(0), sbRows)
	if workers <= 1 || sbRows*sbCols < 8 {
		var m lfMask
		for r := 0; r < sbRows; r++ {
			for c := 0; c < sbCols; c++ {
				d.setupMask(r*8, c*8, &m)
				d.filterSB(r*8, c*8, &m)
			}
		}
		return
	}
	progress := make([]atomic.Int32, sbRows)
	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	var waiters atomic.Int32
	var next atomic.Int32
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var m lfMask
			for {
				r := int(next.Add(1)) - 1
				if r >= sbRows {
					return
				}
				for c := 0; c < sbCols; c++ {
					if r > 0 {
						need := int32(min(c+2, sbCols))
						if progress[r-1].Load() < need {
							waiters.Add(1)
							mu.Lock()
							for progress[r-1].Load() < need {
								cond.Wait()
							}
							mu.Unlock()
							waiters.Add(-1)
						}
					}
					d.setupMask(r*8, c*8, &m)
					d.filterSB(r*8, c*8, &m)
					progress[r].Store(int32(c + 1))
					if waiters.Load() > 0 {
						mu.Lock()
						cond.Broadcast()
						mu.Unlock()
					}
				}
			}
		}()
	}
	wg.Wait()
}

// --- filters (vpx_dsp/loopfilter.c) ---
// Each filters 8 pixels along an edge: o is q0 of the first, along the
// step between them, across the step over the edge.

func iabs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func sclamp(v int) int {
	if v < -128 {
		return -128
	}
	if v > 127 {
		return 127
	}
	return v
}

func filterMask(lim, blim, p3, p2, p1, p0, q0, q1, q2, q3 int) bool {
	return iabs(p3-p2) <= lim && iabs(p2-p1) <= lim && iabs(p1-p0) <= lim &&
		iabs(q1-q0) <= lim && iabs(q2-q1) <= lim && iabs(q3-q2) <= lim &&
		iabs(p0-q0)*2+iabs(p1-q1)/2 <= blim
}

func flat4(p3, p2, p1, p0, q0, q1, q2, q3 int) bool {
	return iabs(p1-p0) <= 1 && iabs(q1-q0) <= 1 && iabs(p2-p0) <= 1 &&
		iabs(q2-q0) <= 1 && iabs(p3-p0) <= 1 && iabs(q3-q0) <= 1
}

// applyFilter4 is libvpx filter4 (mask already true).
func applyFilter4(pix []byte, o, across, thr, p1, p0, q0, q1 int) {
	ps1, ps0, qs0, qs1 := p1-128, p0-128, q0-128, q1-128
	hev := iabs(p1-p0) > thr || iabs(q1-q0) > thr
	filter := 0
	if hev {
		filter = sclamp(ps1 - qs1)
	}
	filter = sclamp(filter + 3*(qs0-ps0))
	f1 := sclamp(filter+4) >> 3
	f2 := sclamp(filter+3) >> 3
	pix[o] = uint8(sclamp(qs0-f1) + 128)
	pix[o-across] = uint8(sclamp(ps0+f2) + 128)
	if !hev {
		f := (f1 + 1) >> 1
		pix[o+across] = uint8(sclamp(qs1-f) + 128)
		pix[o-2*across] = uint8(sclamp(ps1+f) + 128)
	}
}

func lpf4(pix []byte, o, along, across int, t *lfThresh) {
	lim, blim, thr := int(t.lim), int(t.mblim), int(t.hevThr)
	for i := 0; i < 8; i++ {
		x := o + i*along
		p3, p2, p1, p0 := int(pix[x-4*across]), int(pix[x-3*across]), int(pix[x-2*across]), int(pix[x-across])
		q0, q1, q2, q3 := int(pix[x]), int(pix[x+across]), int(pix[x+2*across]), int(pix[x+3*across])
		if filterMask(lim, blim, p3, p2, p1, p0, q0, q1, q2, q3) {
			applyFilter4(pix, x, across, thr, p1, p0, q0, q1)
		}
	}
}

func lpf8(pix []byte, o, along, across int, t *lfThresh) {
	lim, blim, thr := int(t.lim), int(t.mblim), int(t.hevThr)
	for i := 0; i < 8; i++ {
		x := o + i*along
		p3, p2, p1, p0 := int(pix[x-4*across]), int(pix[x-3*across]), int(pix[x-2*across]), int(pix[x-across])
		q0, q1, q2, q3 := int(pix[x]), int(pix[x+across]), int(pix[x+2*across]), int(pix[x+3*across])
		if !filterMask(lim, blim, p3, p2, p1, p0, q0, q1, q2, q3) {
			continue
		}
		if flat4(p3, p2, p1, p0, q0, q1, q2, q3) {
			pix[x-3*across] = uint8((p3 + p3 + p3 + 2*p2 + p1 + p0 + q0 + 4) >> 3)
			pix[x-2*across] = uint8((p3 + p3 + p2 + 2*p1 + p0 + q0 + q1 + 4) >> 3)
			pix[x-across] = uint8((p3 + p2 + p1 + 2*p0 + q0 + q1 + q2 + 4) >> 3)
			pix[x] = uint8((p2 + p1 + p0 + 2*q0 + q1 + q2 + q3 + 4) >> 3)
			pix[x+across] = uint8((p1 + p0 + q0 + 2*q1 + q2 + q3 + q3 + 4) >> 3)
			pix[x+2*across] = uint8((p0 + q0 + q1 + 2*q2 + q3 + q3 + q3 + 4) >> 3)
		} else {
			applyFilter4(pix, x, across, thr, p1, p0, q0, q1)
		}
	}
}

func lpf16(pix []byte, o, along, across int, t *lfThresh) {
	lim, blim, thr := int(t.lim), int(t.mblim), int(t.hevThr)
	var a [16]int
	for i := 0; i < 8; i++ {
		x := o + i*along
		p3, p2, p1, p0 := int(pix[x-4*across]), int(pix[x-3*across]), int(pix[x-2*across]), int(pix[x-across])
		q0, q1, q2, q3 := int(pix[x]), int(pix[x+across]), int(pix[x+2*across]), int(pix[x+3*across])
		if !filterMask(lim, blim, p3, p2, p1, p0, q0, q1, q2, q3) {
			continue
		}
		if !flat4(p3, p2, p1, p0, q0, q1, q2, q3) {
			applyFilter4(pix, x, across, thr, p1, p0, q0, q1)
			continue
		}
		for k := 0; k < 16; k++ {
			a[k] = int(pix[x+(k-8)*across])
		}
		p0v, q0v := a[7], a[8]
		flat2 := true
		for k := 0; k < 4; k++ {
			if iabs(a[k]-p0v) > 1 || iabs(a[15-k]-q0v) > 1 {
				flat2 = false
				break
			}
		}
		if !flat2 {
			pix[x-3*across] = uint8((p3 + p3 + p3 + 2*p2 + p1 + p0 + q0 + 4) >> 3)
			pix[x-2*across] = uint8((p3 + p3 + p2 + 2*p1 + p0 + q0 + q1 + 4) >> 3)
			pix[x-across] = uint8((p3 + p2 + p1 + 2*p0 + q0 + q1 + q2 + 4) >> 3)
			pix[x] = uint8((p2 + p1 + p0 + 2*q0 + q1 + q2 + q3 + 4) >> 3)
			pix[x+across] = uint8((p1 + p0 + q0 + 2*q1 + q2 + q3 + q3 + 4) >> 3)
			pix[x+2*across] = uint8((p0 + q0 + q1 + 2*q2 + q3 + q3 + q3 + 4) >> 3)
			continue
		}
		// 15-tap filter over a[0..15] = p7..q7, outputs p6..q6.
		for k := 1; k < 15; k++ {
			sum := a[k]
			for j := k - 7; j <= k+7; j++ {
				sum += a[min(max(j, 0), 15)]
			}
			pix[x+(k-8)*across] = uint8((sum + 8) >> 4)
		}
	}
}
