package av1

import (
	"math"
	"math/bits"
)

// Loop restoration decisions. The provisional frame header declares
// switchable restoration on every plane, so the decoder allocates the
// units and pass A records a placeholder wherever read_lr would read a
// unit; pass B then fits a Wiener filter (separable least squares) and
// self-guided weights (least squares on the decoder's own box filter
// outputs) per unit, picks each unit's type by RD and the plane's frame
// type, and the replay codes the units that remain.

// lrUnitShift is the restoration unit size of a frame: 64 luma samples up
// to CIF, 128 above.
func lrUnitShift(w, h int) int {
	if w*h <= 352*288 {
		return 0
	}
	return 1
}

// writeLRPlaceholders mirrors readLR at a superblock start.
func (e *Encoder) writeLRPlaceholders(r, c, bSize int) {
	t := e.t
	h := t.h
	if h.allowIntraBC || !h.usesLR {
		return
	}
	w, hh := num4x4W[bSize], num4x4H[bSize]
	for p := 0; p < t.seq.NumPlanes(); p++ {
		if h.lrType[p] == restoreNone {
			continue
		}
		ss := min(p, 1)
		lp := &t.f.lr[p]
		unit := h.lrUnitSize[p]
		rowStart := (r*(4>>ss) + unit - 1) / unit
		rowEnd := min(lp.rows, ((r+hh)*(4>>ss)+unit-1)/unit)
		colStart := (c*(4>>ss) + unit - 1) / unit
		colEnd := min(lp.cols, ((c+w)*(4>>ss)+unit-1)/unit)
		for ur := rowStart; ur < rowEnd; ur++ {
			for uc := colStart; uc < colEnd; uc++ {
				e.sink.recs = append(e.sink.recs, symRec{kind: recLR, n: uint8(p), val: uint32(ur*lp.cols + uc)})
			}
		}
	}
}

// writeLRUnit codes a unit at replay (mirrors readLRUnit).
func (t *tile) writeLRUnit(me *msacEnc, p int, u *lrUnit) {
	h := t.h
	if h.lrType[p] == restoreNone {
		return
	}
	typ := int(u.typ)
	switch h.lrType[p] {
	case restoreWiener:
		b := 0
		if typ == restoreWiener {
			b = 1
		}
		me.sym(t.cdf.wienerRestore[:], b, 2)
	case restoreSgrproj:
		b := 0
		if typ == restoreSgrproj {
			b = 1
		}
		me.sym(t.cdf.sgrprojRestore[:], b, 2)
	default:
		me.sym(t.cdf.switchableRestore[:], [3]int{restoreNone: 0, restoreWiener: 1, restoreSgrproj: 2}[typ], 3)
	}
	switch typ {
	case restoreWiener:
		for pass := 0; pass < 2; pass++ {
			first := 0
			if p > 0 {
				first = 1
			}
			for j := first; j < 3; j++ {
				v := int(u.wiener[pass][j])
				encodeSubexpBool(me, wienerTapsMin[j], wienerTapsMax[j]+1, wienerTapsK[j], t.refLrWiener[p][pass][j], v)
				t.refLrWiener[p][pass][j] = v
			}
		}
	case restoreSgrproj:
		set := int(u.sgrSet)
		for b := 3; b >= 0; b-- {
			me.bit(set >> uint(b) & 1)
		}
		for i := 0; i < 2; i++ {
			v := int(u.xqd[i])
			if sgrParams[set][i*2] != 0 {
				encodeSubexpBool(me, sgrprojXqdMin[i], sgrprojXqdMax[i]+1, 4, t.refSgrXqd[p][i], v)
			}
			t.refSgrXqd[p][i] = v
		}
	}
}

// encodeSubexpBool is the inverse of subexpBool.
func encodeSubexpBool(me *msacEnc, low, high, k, ref, v int) {
	mx := high - low
	r := ref - low
	v -= low
	var u int
	if r<<1 <= mx {
		u = recenter(r, v)
	} else {
		u = recenter(mx-1-r, mx-1-v)
	}
	i, mk := 0, 0
	for {
		b2 := k
		if i != 0 {
			b2 = k + i - 1
		}
		a := 1 << uint(b2)
		if mx <= mk+3*a {
			encodeNS(me, mx-mk, u-mk)
			return
		}
		more := u >= mk+a
		if more {
			me.bit(1)
		} else {
			me.bit(0)
			for b := b2 - 1; b >= 0; b-- {
				me.bit((u - mk) >> uint(b) & 1)
			}
			return
		}
		i++
		mk += a
	}
}

// recenter is the inverse of inverseRecenter.
func recenter(r, v int) int {
	switch {
	case v > 2*r:
		return v
	case v >= r:
		return (v - r) << 1
	}
	return (r-v)<<1 - 1
}

// encodeNS codes v < n as msac.ns reads it.
func encodeNS(me *msacEnc, n, v int) {
	w := bits.Len(uint(n))
	k := (1 << uint(w)) - n
	if v < k {
		for b := w - 2; b >= 0; b-- {
			me.bit(v >> uint(b) & 1)
		}
		return
	}
	x := k + (v-k)>>1
	for b := w - 2; b >= 0; b-- {
		me.bit(x >> uint(b) & 1)
	}
	me.bit((v - k) & 1)
}

// lrUnitOf maps plane sample (x, y) to its restoration unit, as restore
// assigns stripes to units.
func lrUnitOf(lp *lrPlane, unit, ss, x, y int) int {
	off := 8 >> ss
	stripe := (y + off) / (64 >> ss)
	top := max(stripe*(64>>ss)-off, 0)
	ur := min(lp.rows-1, (top+off)/unit)
	uc := min(lp.cols-1, x/unit)
	return ur*lp.cols + uc
}

// unitSSD adds each unit's squared error of plane p of fr against the
// source into out.
func (e *Encoder) unitSSD(fr *frame, p int, lp *lrPlane, unit int, out []float64) {
	clear(out)
	pb := &fr.planes[p]
	ss := min(p, 1)
	src, sst := e.src[p], e.srcStride[p]
	for y := 0; y < pb.h; y++ {
		a := src[y*sst : y*sst+pb.w]
		b := pb.pix8[y*pb.stride:]
		for x := 0; x < pb.w; x += unit {
			ui := lrUnitOf(lp, unit, ss, x, y)
			var s int64
			for i := x; i < min(x+unit, pb.w); i++ {
				d := int64(a[i]) - int64(b[i])
				s += d * d
			}
			out[ui] += float64(s)
		}
	}
}

// decideLR chooses the restoration of every plane on the deblocked and
// CDEF-filtered frames, setting the header and the units.
func (e *Encoder) decideLR(fh *encFrameHdr, cdefF, deblockF *frame) {
	d := e.d
	f, h := d.fs, d.hdr
	setNone := func() {
		h.usesLR = false
		h.lrType = [3]int{}
		fh.lrType = [3]int{}
	}
	if !h.usesLR || len(e.sgrSets) == 0 && !e.lrWiener {
		setNone()
		return
	}
	for p := 0; p < cdefF.numPlanes; p++ {
		lp := &f.lr[p]
		n := lp.rows * lp.cols
		unit := h.lrUnitSize[p]
		ss := min(p, 1)
		base := make([]float64, n)
		e.unitSSD(cdefF, p, lp, unit, base)
		// Candidates per unit: none, Wiener, each self-guided set.
		type cand struct {
			u    lrUnit
			cost float64
		}
		best := make([]cand, n)
		bestW := make([]cand, n)
		bestS := make([]cand, n)
		for i := range best {
			best[i] = cand{lrUnit{typ: restoreNone}, base[i]}
			bestW[i], bestS[i] = best[i], best[i]
			bestW[i].cost, bestS[i].cost = math.Inf(1), math.Inf(1)
		}
		ssd := make([]float64, n)
		// Wiener.
		if e.lrWiener {
			for i := range lp.units {
				lp.units[i] = e.fitWiener(cdefF, p, lp, unit, ss, i)
			}
			out := d.restoreOnly(f, cdefF, deblockF, p)
			e.unitSSD(out, p, lp, unit, ssd)
			d.release(&out.planes, out.numPlanes)
			for i := range lp.units {
				c := ssd[i] + e.lambda*40
				if c < bestW[i].cost {
					bestW[i] = cand{lp.units[i], c}
				}
			}
		}
		// Self-guided: the decoder's box filters give each set's two
		// outputs; the weights are fitted per unit.
		for _, set := range e.sgrSets {
			f0 := e.sgrOutput(f, cdefF, deblockF, p, set, [2]int16{128, 0})
			f1 := e.sgrOutput(f, cdefF, deblockF, p, set, [2]int16{0, 0})
			for i := range lp.units {
				lp.units[i] = e.fitSgr(cdefF, f0, f1, p, lp, unit, ss, i, set)
			}
			d.release(&f0.planes, f0.numPlanes)
			d.release(&f1.planes, f1.numPlanes)
			out := d.restoreOnly(f, cdefF, deblockF, p)
			e.unitSSD(out, p, lp, unit, ssd)
			d.release(&out.planes, out.numPlanes)
			for i := range lp.units {
				c := ssd[i] + e.lambda*24
				if c < bestS[i].cost {
					bestS[i] = cand{lp.units[i], c}
				}
			}
		}
		// The plane's frame type by total cost (flag bits included).
		var totW, totS, totSw float64
		for i := range best {
			if math.IsInf(bestW[i].cost, 1) {
				bestW[i].cost = base[i]
			}
			if math.IsInf(bestS[i].cost, 1) {
				bestS[i].cost = base[i]
			}
			totW += min(bestW[i].cost, base[i]) + e.lambda
			totS += min(bestS[i].cost, base[i]) + e.lambda
			totSw += min(bestW[i].cost, bestS[i].cost, base[i]) + e.lambda*1.6
		}
		totNone := 0.0
		for _, v := range base {
			totNone += v
		}
		if lrDebugHook != nil {
			lrDebugHook(p, totNone, totW, totS, totSw)
		}
		typ := restoreNone
		bestTot := totNone
		if e.lrForce {
			// Tests: every unit restored, alternating the kinds.
			h.lrType[p], fh.lrType[p] = restoreSwitchable, restoreSwitchable
			for i := range lp.units {
				if i%2 == 0 && e.lrWiener {
					lp.units[i] = bestW[i].u
				} else {
					lp.units[i] = bestS[i].u
				}
				if lp.units[i].typ == restoreNone {
					lp.units[i] = bestW[i].u
				}
			}
			continue
		}
		for _, c := range []struct {
			t   int
			tot float64
		}{{restoreWiener, totW}, {restoreSgrproj, totS}, {restoreSwitchable, totSw}} {
			if c.tot < bestTot {
				typ, bestTot = c.t, c.tot
			}
		}
		h.lrType[p] = typ
		fh.lrType[p] = typ
		for i := range lp.units {
			u := lrUnit{typ: restoreNone}
			switch typ {
			case restoreWiener:
				if bestW[i].cost < base[i] {
					u = bestW[i].u
				}
			case restoreSgrproj:
				if bestS[i].cost < base[i] {
					u = bestS[i].u
				}
			case restoreSwitchable:
				switch {
				case bestW[i].cost < base[i] && bestW[i].cost <= bestS[i].cost:
					u = bestW[i].u
				case bestS[i].cost < base[i]:
					u = bestS[i].u
				}
			}
			lp.units[i] = u
		}
	}
	h.usesLR = false
	for p := 0; p < 3; p++ {
		if h.lrType[p] != restoreNone {
			h.usesLR = true
		}
	}
	if lrStatsHook != nil {
		for p := 0; p < cdefF.numPlanes; p++ {
			if h.lrType[p] == restoreNone {
				continue
			}
			for _, u := range f.lr[p].units {
				lrStatsHook(p, int(u.typ))
			}
		}
	}
	if !h.usesLR {
		setNone()
	}
}

// restoreOnly runs loop restoration of plane p only with the current
// units and returns the result (from the pool).
func (d *Decoder) restoreOnly(f *frameState, cdefF, deblockF *frame, p int) *frame {
	h := f.hdr
	saved := h.lrType
	for q := range h.lrType {
		if q != p {
			h.lrType[q] = restoreNone
		}
	}
	if h.lrType[p] == restoreNone {
		h.lrType[p] = restoreSwitchable
	}
	out := d.restore(f, cdefF, deblockF)
	h.lrType = saved
	return out
}

// sgrOutput is plane p restored with self-guided set set and weights xqd
// on every unit: {128, 0} yields the radius-0 filter output (/16), {0, 0}
// the radius-1 one.
func (e *Encoder) sgrOutput(f *frameState, cdefF, deblockF *frame, p, set int, xqd [2]int16) *frame {
	lp := &f.lr[p]
	for i := range lp.units {
		lp.units[i] = lrUnit{typ: restoreSgrproj, sgrSet: uint8(set), xqd: xqd}
	}
	return e.d.restoreOnly(f, cdefF, deblockF, p)
}

// fitSgr fits the self-guided weights of unit ui for set from the two
// filter outputs (least squares, clamped to the coded ranges).
func (e *Encoder) fitSgr(cdefF, f0, f1 *frame, p int, lp *lrPlane, unit, ss, ui, set int) lrUnit {
	r0, r1 := sgrParams[set][0], sgrParams[set][2]
	var a00, a01, a11, b0, b1 float64
	e.unitSamples(cdefF, p, lp, unit, ss, ui, func(x, y int, s, u float64) {
		v0 := 16 * float64(f0.planes[p].pix8[y*f0.planes[p].stride+x])
		v1 := 16 * float64(f1.planes[p].pix8[y*f1.planes[p].stride+x])
		if r0 == 0 {
			v0 = u
		}
		if r1 == 0 {
			v1 = u
		}
		// out * 2^11 = 128 v1 + w0 (v0 - v1) + w1 (u - v1)
		t := s*2048 - 128*v1
		g0, g1 := v0-v1, u-v1
		a00 += g0 * g0
		a01 += g0 * g1
		a11 += g1 * g1
		b0 += g0 * t
		b1 += g1 * t
	})
	var w0, w1 float64
	switch {
	case r0 == 0:
		// w0 multiplies u like w1: only their sum matters; w0 = 0.
		if a11 > 0 {
			w1 = b1 / a11
		}
	case r1 == 0:
		// w2 = 0: w1 = 128 - w0, out*2^11 = 128 u + w0 (v0 - u).
		var aa, bb float64
		e.unitSamples(cdefF, p, lp, unit, ss, ui, func(x, y int, s, u float64) {
			v0 := 16 * float64(f0.planes[p].pix8[y*f0.planes[p].stride+x])
			g := v0 - u
			aa += g * g
			bb += g * (s*2048 - 128*u)
		})
		if aa > 0 {
			w0 = bb / aa
		}
		w1 = 128 - w0
	default:
		det := a00*a11 - a01*a01
		if det != 0 {
			w0 = (b0*a11 - b1*a01) / det
			w1 = (a00*b1 - a01*b0) / det
		}
	}
	u := lrUnit{typ: restoreSgrproj, sgrSet: uint8(set)}
	u.xqd[0] = int16(clip3(sgrprojXqdMin[0], sgrprojXqdMax[0], int(math.Round(w0))))
	u.xqd[1] = int16(clip3(sgrprojXqdMin[1], sgrprojXqdMax[1], int(math.Round(w1))))
	if r0 == 0 {
		u.xqd[0] = 0
	}
	if r1 == 0 {
		u.xqd[1] = int16(clip3(sgrprojXqdMin[1], sgrprojXqdMax[1], 128-int(u.xqd[0])))
	}
	return u
}

// unitSamples calls fn for every sample of unit ui with its source value
// s and degraded value u (scaled by 16, as the self-guided filter sees
// it).
func (e *Encoder) unitSamples(fr *frame, p int, lp *lrPlane, unit, ss, ui int, fn func(x, y int, s, u float64)) {
	pb := &fr.planes[p]
	src, sst := e.src[p], e.srcStride[p]
	uc := ui % lp.cols
	x0 := uc * unit
	x1 := pb.w
	if uc < lp.cols-1 {
		x1 = min(pb.w, x0+unit)
	}
	for y := 0; y < pb.h; y++ {
		if lrUnitOf(lp, unit, ss, x0, y) != ui {
			continue
		}
		for x := x0; x < x1; x++ {
			fn(x, y, float64(src[y*sst+x]), 16*float64(pb.pix8[y*pb.stride+x]))
		}
	}
}

// fitWiener fits unit ui's separable symmetric Wiener filter: the
// horizontal taps by least squares on the degraded samples, then the
// vertical taps on the horizontally filtered ones, quantised to the coded
// 7-bit taps.
func (e *Encoder) fitWiener(fr *frame, p int, lp *lrPlane, unit, ss, ui int) lrUnit {
	pb := &fr.planes[p]
	get := func(x, y int) float64 {
		return float64(pb.pix8[clip3(0, pb.h-1, y)*pb.stride+clip3(0, pb.w-1, x)])
	}
	solve := func(sample func(x, y, k int) float64) [3]float64 {
		// y - x0 = sum_k c_k (x_-k + x_k - 2 x_0), k = 3, 2, 1
		var a [3][3]float64
		var b [3]float64
		e.unitSamples(fr, p, lp, unit, ss, ui, func(x, y int, s, _ float64) {
			c := sample(x, y, 0)
			var g [3]float64
			for k := 0; k < 3; k++ {
				g[k] = sample(x, y, -(3-k)) + sample(x, y, 3-k) - 2*c
			}
			t := s - c
			for i := 0; i < 3; i++ {
				for j := 0; j < 3; j++ {
					a[i][j] += g[i] * g[j]
				}
				b[i] += g[i] * t
			}
		})
		return solve3(a, b)
	}
	hc := solve(func(x, y, k int) float64 { return get(x+k, y) })
	// Horizontally filtered samples for the vertical fit.
	hf := func(x, y int) float64 {
		c := get(x, y)
		v := c
		for k := 0; k < 3; k++ {
			v += hc[k] * (get(x-(3-k), y) + get(x+(3-k), y) - 2*c)
		}
		return v
	}
	vc := solve(func(x, y, k int) float64 { return hf(x, y+k) })
	u := lrUnit{typ: restoreWiener}
	for k := 0; k < 3; k++ {
		u.wiener[0][k] = int8(clip3(wienerTapsMin[k], wienerTapsMax[k], int(math.Round(vc[k]*128))))
		u.wiener[1][k] = int8(clip3(wienerTapsMin[k], wienerTapsMax[k], int(math.Round(hc[k]*128))))
	}
	if p > 0 {
		u.wiener[0][0], u.wiener[1][0] = 0, 0
	}
	return u
}

// solve3 solves a 3x3 linear system (zero when singular).
func solve3(a [3][3]float64, b [3]float64) [3]float64 {
	for col := 0; col < 3; col++ {
		p := col
		for r := col + 1; r < 3; r++ {
			if math.Abs(a[r][col]) > math.Abs(a[p][col]) {
				p = r
			}
		}
		a[p], a[col] = a[col], a[p]
		b[p], b[col] = b[col], b[p]
		if math.Abs(a[col][col]) < 1e-9 {
			return [3]float64{}
		}
		for r := 0; r < 3; r++ {
			if r == col {
				continue
			}
			f := a[r][col] / a[col][col]
			for k := 0; k < 3; k++ {
				a[r][k] -= f * a[col][k]
			}
			b[r] -= f * b[col]
		}
	}
	return [3]float64{b[0] / a[0][0], b[1] / a[1][1], b[2] / a[2][2]}
}

// lrStatsHook (tests) sees each coded restoration unit's type.
var lrStatsHook func(plane, typ int)

// lrDebugHook (tests) sees each plane's total costs: no restoration,
// Wiener, self-guided, switchable.
var lrDebugHook func(plane int, none, wiener, sgr, sw float64)
