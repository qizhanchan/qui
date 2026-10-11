package av1

import "math"

// In-loop filter decisions (pass B): the deblocking levels by measured
// distortion, then CDEF strengths per 64x64 and the frame's strength set
// by RD. Both run the decoder's own filters on the reconstruction.

// cdefStrengths are the (primary, secondary) strengths searched, luma and
// chroma alike: secondary is coded as 0..3 for 0, 1, 2, 4.
var cdefStrengths = func() (s [][2]int) {
	for pri := 0; pri < 16; pri++ {
		for _, sec := range []int{0, 1, 2, 4} {
			s = append(s, [2]int{pri, sec})
		}
	}
	return
}()

// cdefStrengthSubset is the strength pairs of the given primary and
// secondary values.
func cdefStrengthSubset(pri, sec []int) (s [][2]int) {
	for _, p := range pri {
		for _, q := range sec {
			s = append(s, [2]int{p, q})
		}
	}
	return
}

// planesSSD is the squared error of the frame's visible samples against
// the source, per plane (in row bands on all cores).
func (e *Encoder) planesSSD(fr *frame) (ssd [3]int64) {
	for p := 0; p < fr.numPlanes; p++ {
		pb := &fr.planes[p]
		src, ss := e.src[p], e.srcStride[p]
		const band = 32
		nb := (pb.h + band - 1) / band
		part := make([]int64, nb)
		parallelFor(nb, func(_, b int) {
			var sum int64
			for y := b * band; y < min(pb.h, (b+1)*band); y++ {
				a := src[y*ss : y*ss+pb.w]
				r := pb.pix8[y*pb.stride : y*pb.stride+pb.w]
				for i, v := range a {
					d := int64(v) - int64(r[i])
					sum += d * d
				}
			}
			part[b] = sum
		})
		for _, v := range part {
			ssd[p] += v
		}
	}
	return
}

// copyPlanes copies src's samples into dst (same geometry), in bands on
// all cores.
func copyPlanes(dst, src *frame) {
	const chunk = 256 << 10
	for p := 0; p < src.numPlanes; p++ {
		d, s := dst.planes[p].pix8, src.planes[p].pix8
		n := (len(s) + chunk - 1) / chunk
		parallelFor(n, func(_, i int) {
			lo, hi := i*chunk, min(len(s), (i+1)*chunk)
			copy(d[lo:hi], s[lo:hi])
		})
	}
}

// decideLoopFilter searches the deblocking levels: luma (both
// directions alike) then each chroma plane, keeping the level with the
// least distortion. The reconstruction is left unfiltered.
func (e *Encoder) decideLoopFilter(fh *encFrameHdr) {
	d := e.d
	f, h := d.fs, d.hdr
	fh.lfLevel = [4]int{}
	if fh.baseQ == 0 {
		return
	}
	backup := d.cloneFrame(f.cur)
	defer d.release(&backup.planes, backup.numPlanes)
	try := func(lv [4]int) [3]int64 {
		h.lf.level = lv
		d.deblock(f)
		ssd := e.planesSSD(f.cur)
		copyPlanes(f.cur, backup)
		return ssd
	}
	// Luma: coarse then fine around the best.
	q := float64(acQ(8, fh.baseQ)) / 4
	guess := clip3(0, 63, int(q*0.33+0.5))
	bestY, bestD := 0, e.planesSSD(f.cur)[0]
	evalY := func(l int) {
		if l < 0 || l > 63 {
			return
		}
		if dd := try([4]int{l, l, 0, 0})[0]; dd < bestD {
			bestY, bestD = l, dd
		}
	}
	for _, l := range []int{guess / 2, guess, guess * 3 / 2, min(63, guess*2)} {
		evalY(l)
	}
	for step := 4; step >= e.lfStep; step /= 2 {
		c := bestY
		evalY(c - step)
		evalY(c + step)
	}
	fh.lfLevel[0], fh.lfLevel[1] = bestY, bestY
	if bestY == 0 {
		// Chroma levels are coded only with a luma level.
		return
	}
	for p := 1; p <= 2; p++ {
		best, bestD := 0, int64(math.MaxInt64)
		for _, l := range []int{0, bestY / 2, bestY, min(63, bestY*3/2)} {
			lv := fh.lfLevel
			lv[p+1] = l
			if dd := try(lv)[p]; dd < bestD {
				best, bestD = l, dd
			}
		}
		fh.lfLevel[p+1] = best
	}
}

// decideCDEF measures every luma and chroma strength on every coded
// 64x64 of the deblocked frame and picks the frame's strength set (1, 2,
// 4 or 8 pairs) and each 64x64's index by RD. The reconstruction is left
// deblocked but unfiltered by CDEF.
func (e *Encoder) decideCDEF(fh *encFrameHdr) {
	d := e.d
	f, h := d.fs, d.hdr
	fh.cdefDamping = 3 + fh.baseQ>>6
	h.cdefDamping = fh.cdefDamping
	nFB := len(f.cdefIdx)
	coded := make([]int, 0, nFB)
	for i, seen := range e.cdefSeen {
		f.cdefIdx[i] = -1
		if seen {
			coded = append(coded, i)
		}
	}
	setNone := func() {
		fh.cdefBits = 0
		fh.cdefYPri[0], fh.cdefYSec[0], fh.cdefUVPri[0], fh.cdefUVSec[0] = 0, 0, 0, 0
		h.cdefBits = 0
		h.cdefYPri[0], h.cdefYSec[0], h.cdefUVPri[0], h.cdefUVSec[0] = 0, 0, 0, 0
		for _, i := range coded {
			f.cdefIdx[i] = 0
		}
	}
	if fh.baseQ == 0 || len(coded) == 0 {
		setNone()
		return
	}
	// Distortion per coded 64x64 and strength.
	ns := len(e.cdefCands)
	dy := make([]int64, len(coded)*ns)
	duv := make([]int64, len(coded)*ns)
	scratch := d.cloneFrame(f.cur)
	defer d.release(&scratch.planes, scratch.numPlanes)
	parallelFor(len(coded), func(_, k int) {
		e.cdefMeasure(f, scratch, coded[k], dy[k*ns:(k+1)*ns], duv[k*ns:(k+1)*ns])
	})
	// Choose 2^bits pairs greedily (each addition lowers the sum over the
	// 64x64s of their best pair's distortion the most), for each bits,
	// and keep the best RD.
	type pair struct{ y, uv int }
	nc := len(coded)
	curMin := make([]int64, nc)
	for k := range curMin {
		curMin[k] = math.MaxInt64
	}
	var set, bestSet []pair
	bestCost := math.Inf(1)
	for bits := 0; bits <= 3; bits++ {
		for len(set) < 1<<bits {
			var add pair
			addTotal := int64(math.MaxInt64)
			for y := 0; y < ns; y++ {
				for uv := 0; uv < ns; uv++ {
					var total int64
					for k := 0; k < nc && total < addTotal; k++ {
						total += min(curMin[k], dy[k*ns+y]+duv[k*ns+uv])
					}
					if total < addTotal {
						add, addTotal = pair{y, uv}, total
					}
				}
			}
			set = append(set, add)
			for k := range curMin {
				curMin[k] = min(curMin[k], dy[k*ns+add.y]+duv[k*ns+add.uv])
			}
		}
		var total int64
		for _, v := range curMin {
			total += v
		}
		c := float64(total) + e.lambda*float64(bits*nc+bits*12)
		if c < bestCost {
			bestCost = c
			bestSet = append(bestSet[:0], set...)
		}
	}
	bestIdx := make([]int, nc)
	for k := range coded {
		best := int64(math.MaxInt64)
		for j, p := range bestSet {
			if v := dy[k*ns+p.y] + duv[k*ns+p.uv]; v < best {
				best, bestIdx[k] = v, j
			}
		}
	}
	bits := 0
	for 1<<bits < len(bestSet) {
		bits++
	}
	fh.cdefBits, h.cdefBits = bits, bits
	for j, p := range bestSet {
		fh.cdefYPri[j], fh.cdefYSec[j] = e.cdefCands[p.y][0], e.cdefCands[p.y][1]
		fh.cdefUVPri[j], fh.cdefUVSec[j] = e.cdefCands[p.uv][0], e.cdefCands[p.uv][1]
		h.cdefYPri[j], h.cdefYSec[j] = fh.cdefYPri[j], fh.cdefYSec[j]
		h.cdefUVPri[j], h.cdefUVSec[j] = fh.cdefUVPri[j], fh.cdefUVSec[j]
	}
	for k, i := range coded {
		f.cdefIdx[i] = int8(bestIdx[k])
	}
}

// cdefMeasure fills the luma and chroma distortion of 64x64 number fb for
// every strength (the decoder's CDEF on the deblocked frame, written to
// scratch).
func (e *Encoder) cdefMeasure(f *frameState, scratch *frame, fb int, dy, duv []int64) {
	src := f.cur
	h := f.hdr
	r0 := (fb / f.cdefStride) * 16
	c0 := (fb % f.cdefStride) * 16
	skip := func(rr, cc int) bool {
		if rr >= f.miRows || cc >= f.miCols {
			return true
		}
		return f.mi[rr*f.miCols+cc].skip
	}
	for r := r0; r < min(r0+16, f.miRows); r += 2 {
		for c := c0; c < min(c0+16, f.miCols); c += 2 {
			if skip(r, c) && skip(r+1, c) && skip(r, c+1) && skip(r+1, c+1) {
				continue
			}
			if e.cdefSparse && (r+c)&2 != 0 {
				continue // a checkerboard of the 8x8 blocks
			}
			yDir, variance := cdefDirection(&src.planes[0], c*4, r*4, 8)
			varStr := int32(0)
			if variance>>6 != 0 {
				varStr = min(int32(floorLog2(int(variance>>6))), 12)
			}
			for si, s := range e.cdefCands {
				pri, sec := int32(s[0]), int32(s[1])
				dir := yDir
				if pri == 0 {
					dir = 0
				}
				adj := int32(0)
				if variance != 0 {
					adj = (pri*(4+varStr) + 8) >> 4
				}
				dy[si] += e.cdefBlockSSD(f, src, scratch, 0, r, c, adj, sec, h.cdefDamping, dir)
				if src.numPlanes > 1 {
					duv[si] += e.cdefBlockSSD(f, src, scratch, 1, r, c, pri, sec, h.cdefDamping-1, dir) +
						e.cdefBlockSSD(f, src, scratch, 2, r, c, pri, sec, h.cdefDamping-1, dir)
				}
			}
		}
	}
}

// cdefBlockSSD filters one 8x8 (chroma 4x4) block of plane into scratch
// and returns its squared error against the source.
func (e *Encoder) cdefBlockSSD(f *frameState, src, scratch *frame, plane, r, c int, pri, sec int32, damping, dir int) int64 {
	ss := min(plane, 1)
	x0, y0 := (c*4)>>ss, (r*4)>>ss
	n := 8 >> ss
	sp, dp := &src.planes[plane], &scratch.planes[plane]
	if pri == 0 && sec == 0 {
		// cdefFilter leaves the block alone.
		for y := 0; y < n; y++ {
			copy(dp.pix8[(y0+y)*dp.stride+x0:(y0+y)*dp.stride+x0+n], sp.pix8[(y0+y)*sp.stride+x0:])
		}
	} else {
		cdefFilter(f, src, scratch, plane, r, c, pri, sec, damping, dir)
	}
	w, hh := min(n, dp.w-x0), min(n, dp.h-y0)
	var ssd int64
	s, sst := e.src[plane], e.srcStride[plane]
	for y := 0; y < hh; y++ {
		a := s[(y0+y)*sst+x0 : (y0+y)*sst+x0+w]
		b := dp.pix8[(y0+y)*dp.stride+x0:]
		for i, v := range a {
			dd := int64(v) - int64(b[i])
			ssd += dd * dd
		}
	}
	return ssd
}
