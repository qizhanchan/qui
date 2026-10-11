package hevc

import (
	"math"
	"runtime"
	"sync"
	"sync/atomic"
)

// SAO decisions: per CTB and component, band offset, one of the four
// edge-offset classes, or off, chosen by distortion change plus lambda
// times the parameter bits, with merge-left / merge-up candidates. The
// statistics compare the deblocked reconstruction with the source.

// deblockPicture deblocks the current picture with the decoder's filter
// (the filter job then applies SAO only), CTB rows in parallel: first all
// vertical edges (each touches only its own lines), then all horizontal
// edges (on the 8-sample grid an edge changes at most three lines on each
// side and reads four, so no two edges interact). The result equals the
// picture-wide order of 8.7.2.
func (e *Encoder) deblockPicture() {
	d := e.d
	lf := &d.lf
	lf.active, lf.fc = d.active, d.fc
	sp := d.active
	for _, vertical := range []bool{true, false} {
		parallelFor(sp.PicHCtb, func(r int) {
			top, bot := r<<sp.Log2Ctb, min((r+1)<<sp.Log2Ctb, sp.Height)
			lf.deblockEdges(d.cur, vertical, top, bot)
		})
	}
}

// parallelFor runs fn(0..n-1) on up to GOMAXPROCS goroutines.
func parallelFor(n int, fn func(i int)) {
	nw := min(runtime.GOMAXPROCS(0), n)
	if nw <= 1 {
		for i := 0; i < n; i++ {
			fn(i)
		}
		return
	}
	var next atomic.Int32
	var wg sync.WaitGroup
	for k := 0; k < nw; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= n {
					return
				}
				fn(i)
			}
		}()
	}
	wg.Wait()
}

// saoStats are the per-category error sums of one CTB component: for
// each edge class the categories e = 0..4 (2 unused), and the 32 bands.
type saoStats struct {
	eoCnt, eoSum [4][5]int64
	bCnt, bSum   [32]int64
}

func (e *Encoder) saoStats(st *saoStats, ctb, c int) {
	*st = saoStats{}
	d := e.d
	sp := d.active
	pic := d.cur
	rec, stride, src, ss := pic.y, pic.ys, e.srcY, e.ys
	scale := 1
	if c > 0 {
		rec, stride, ss, scale = pic.cb, pic.cs, e.cs, 2
		src = e.srcCb
		if c == 2 {
			rec, src = pic.cr, e.srcCr
		}
	}
	size := sp.CtbSize / scale
	w, h := sp.Width/scale, sp.Height/scale
	x0, y0 := (ctb%sp.PicWCtb)*size, (ctb/sp.PicWCtb)*size
	x1, y1 := min(x0+size, w), min(y0+size, h)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			v := int(rec[y*stride+x])
			diff := int64(src[y*ss+x]) - int64(v)
			b := v >> 3
			st.bCnt[b]++
			st.bSum[b] += diff
			for k := 0; k < 4; k++ {
				eo := saoEO[k]
				ax, ay := x+eo[0][0], y+eo[0][1]
				bx, by := x+eo[1][0], y+eo[1][1]
				if ax < 0 || ay < 0 || bx < 0 || by < 0 || ax >= w || bx >= w || ay >= h || by >= h {
					continue
				}
				cat := 2 + sign(v-int(rec[ay*stride+ax])) + sign(v-int(rec[by*stride+bx]))
				st.eoCnt[k][cat]++
				st.eoSum[k][cat] += diff
			}
		}
	}
}

// offsetGain returns the best offset in [lo, hi] for a category with
// count n and error sum s, and the distortion change it brings.
func offsetGain(n, s int64, lo, hi int) (int, int64) {
	if n == 0 {
		return 0, 0
	}
	o := int(math.Round(float64(s) / float64(n)))
	o = clip3(lo, hi, o)
	best, bestD := 0, int64(0)
	for _, c := range []int{o, o - 1, o + 1} {
		if c < lo || c > hi {
			continue
		}
		dd := n*int64(c*c) - 2*int64(c)*s
		if dd < bestD {
			best, bestD = c, dd
		}
	}
	return best, bestD
}

// saoEval returns the distortion change of applying parameters prm to
// component c with stats st.
func saoEval(st *saoStats, prm *saoParams, c int) int64 {
	var dd int64
	switch prm.typ[c] {
	case 1:
		for k := 0; k < 4; k++ {
			b := (int(prm.bandPos[c]) + k) & 31
			o := int64(prm.offset[c][k])
			dd += st.bCnt[b]*o*o - 2*o*st.bSum[b]
		}
	case 2:
		cats := [4]int{0, 1, 3, 4}
		for k, cat := range cats {
			o := int64(prm.offset[c][k])
			n, s := st.eoCnt[prm.eoClass[c]][cat], st.eoSum[prm.eoClass[c]][cat]
			dd += n*o*o - 2*o*s
		}
	}
	return dd
}

// offsetBits approximates the bits of coding four offsets.
func offsetBits(off [4]int8, band bool) float64 {
	b := 0.0
	for _, o := range off {
		a := iabs(int(o))
		b += float64(min(a+1, 7))
		if band && a != 0 {
			b++
		}
	}
	return b
}

// bestSAO picks explicit parameters for component c (or the chroma pair
// when c == 1, sharing type and class) and returns them with their cost.
func (e *Encoder) bestSAO(st []*saoStats, comps []int) (saoParams, float64) {
	var best saoParams
	bestCost := e.lambda * 1 // sao_type_idx = 0
	// Band offset (each component its own band position).
	{
		var prm saoParams
		cost := e.lambda * 2
		for i, c := range comps {
			s := st[i]
			bestBand, bestD := 0, int64(0)
			var bestOff [4]int8
			for b := 0; b < 32; b++ {
				var dd int64
				var off [4]int8
				for k := 0; k < 4; k++ {
					bb := (b + k) & 31
					o, g := offsetGain(s.bCnt[bb], s.bSum[bb], -7, 7)
					off[k] = int8(o)
					dd += g
				}
				if dd < bestD {
					bestBand, bestD, bestOff = b, dd, off
				}
			}
			prm.typ[c], prm.bandPos[c], prm.offset[c] = 1, uint8(bestBand), bestOff
			cost += float64(bestD) + e.lambda*(offsetBits(bestOff, true)+5)
		}
		if cost < bestCost {
			best, bestCost = prm, cost
		}
	}
	// Edge offset classes.
	for k := 0; k < 4; k++ {
		var prm saoParams
		cost := e.lambda * (2 + 2)
		for i, c := range comps {
			s := st[i]
			var off [4]int8
			var dd int64
			for j, cat := range [4]int{0, 1, 3, 4} {
				lo, hi := 0, 7
				if j >= 2 {
					lo, hi = -7, 0
				}
				o, g := offsetGain(s.eoCnt[k][cat], s.eoSum[k][cat], lo, hi)
				off[j] = int8(o)
				dd += g
			}
			prm.typ[c], prm.eoClass[c], prm.offset[c] = 2, uint8(k), off
			cost += float64(dd) + e.lambda*offsetBits(off, false)
		}
		if cost < bestCost {
			best, bestCost = prm, cost
		}
	}
	return best, bestCost
}

// decideSAO sets the SAO parameters of every CTB (fc.sao) and its merge
// choice (e.saoMerge).
func (e *Encoder) decideSAO(enabled [2]bool) {
	d := e.d
	sp := d.active
	fc := d.fc
	p := d.pps[0]
	n := sp.PicWCtb * sp.PicHCtb
	if cap(e.saoSt) < n {
		e.saoSt = make([][3]saoStats, n)
	}
	all := e.saoSt[:n]
	parallelFor(n, func(rs int) {
		for c := 0; c < 3; c++ {
			if enabled[min(c, 1)] {
				e.saoStats(&all[rs][c], rs, c)
			}
		}
	})
	for rs := 0; rs < n; rs++ {
		st := &all[rs]
		var prm saoParams
		cost := 0.0
		if enabled[0] {
			pl, cl := e.bestSAO([]*saoStats{&st[0]}, []int{0})
			prm.typ[0], prm.bandPos[0], prm.eoClass[0], prm.offset[0] = pl.typ[0], pl.bandPos[0], pl.eoClass[0], pl.offset[0]
			cost += cl
		}
		if enabled[1] {
			pc, cc := e.bestSAO([]*saoStats{&st[1], &st[2]}, []int{1, 2})
			for c := 1; c <= 2; c++ {
				prm.typ[c], prm.bandPos[c], prm.eoClass[c], prm.offset[c] = pc.typ[c], pc.bandPos[c], pc.eoClass[c], pc.offset[c]
			}
			cost += cc
		}
		merge := 0
		evalMerge := func(nb int) float64 {
			n := &fc.sao[nb]
			c := e.lambda
			for comp := 0; comp < 3; comp++ {
				if enabled[min(comp, 1)] {
					c += float64(saoEval(&st[comp], n, comp))
				}
			}
			return c
		}
		rx, ry := rs%sp.PicWCtb, rs/sp.PicWCtb
		ts := p.ctbRsToTs[rs]
		if rx > 0 && fc.ctbSliceAddr[rs-1] == fc.ctbSliceAddr[rs] && p.tileID[p.ctbRsToTs[rs-1]] == p.tileID[ts] {
			if c := evalMerge(rs - 1); c < cost {
				cost, merge, prm = c, 1, fc.sao[rs-1]
			}
		}
		if ry > 0 && fc.ctbSliceAddr[rs-sp.PicWCtb] == fc.ctbSliceAddr[rs] && p.tileID[p.ctbRsToTs[rs-sp.PicWCtb]] == p.tileID[ts] {
			if c := evalMerge(rs - sp.PicWCtb); c < cost {
				cost, merge, prm = c, 2, fc.sao[rs-sp.PicWCtb]
			}
		}
		fc.sao[rs] = prm
		e.saoMerge[rs] = int8(merge)
	}
}
