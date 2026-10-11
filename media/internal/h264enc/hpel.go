package h264enc

import (
	"sync"

	"github.com/qizhanchan/qui/media/internal/h264"
)

// Half-sample planes of reference pictures for sub-sample motion search:
// H (between x and x+1), V (between y and y+1) and HV (centre), computed
// once per reference with the decoder's interpolation filters. Quarter
// positions are averages of two of {full, H, V, HV} — exactly the spec's
// derivation — so search costs see the true prediction without running a
// 6-tap filter per candidate. Final predictions still use the decoder's MC.

func (p *refPic) ensureHpel() {
	for i := range p.hp {
		if len(p.hp[i]) != len(p.y) {
			p.hp[i] = make([]byte, len(p.y))
		}
	}
}

// buildHpel fills p.hp from p.y (already padded), in bands of 16 rows,
// in parallel when workers > 1.
func (e *Encoder) buildHpel(p *refPic, workers int) {
	p.ensureHpel()
	pw, ph := e.mbW*16+2*lumaPad, e.mbH*16+2*lumaPad
	rows := ph / 16
	var next int
	var mu sync.Mutex
	work := func(mc *h264.MC) {
		for {
			mu.Lock()
			r := next
			next++
			mu.Unlock()
			if r >= rows {
				return
			}
			y := r * 16
			for x := 0; x < pw; x += 16 {
				o := y*p.ys + x
				mc.Luma(p.hp[0][o:], p.ys, p.y, p.ys, pw, ph, x, y, 2, 0, 16, 16)
				mc.Luma(p.hp[1][o:], p.ys, p.y, p.ys, pw, ph, x, y, 0, 2, 16, 16)
				mc.Luma(p.hp[2][o:], p.ys, p.y, p.ys, pw, ph, x, y, 2, 2, 16, 16)
			}
		}
	}
	if workers <= 1 {
		work(e.slices[0].mc)
		return
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		mc := e.slices[i%len(e.slices)].mc
		go func() {
			defer wg.Done()
			work(mc)
		}()
	}
	wg.Wait()
}

// qpelSources gives, per quarter-sample fraction (fx, fy), the two
// (plane, dx, dy) samples averaged; plane 0 = full, 1 = H, 2 = V, 3 = HV.
// Half/full positions use a single source (the second equals the first).
var qpelSources = [4][4][2][3]int8{}

func init() {
	set := func(fx, fy int, a, b [3]int8) { qpelSources[fy][fx] = [2][3]int8{a, b} }
	G, H, V, J := int8(0), int8(1), int8(2), int8(3)
	set(0, 0, [3]int8{G, 0, 0}, [3]int8{G, 0, 0})
	set(2, 0, [3]int8{H, 0, 0}, [3]int8{H, 0, 0})
	set(0, 2, [3]int8{V, 0, 0}, [3]int8{V, 0, 0})
	set(2, 2, [3]int8{J, 0, 0}, [3]int8{J, 0, 0})
	set(1, 0, [3]int8{G, 0, 0}, [3]int8{H, 0, 0})
	set(3, 0, [3]int8{H, 0, 0}, [3]int8{G, 1, 0})
	set(0, 1, [3]int8{G, 0, 0}, [3]int8{V, 0, 0})
	set(0, 3, [3]int8{V, 0, 0}, [3]int8{G, 0, 1})
	set(2, 1, [3]int8{H, 0, 0}, [3]int8{J, 0, 0})
	set(2, 3, [3]int8{J, 0, 0}, [3]int8{H, 0, 1})
	set(1, 2, [3]int8{V, 0, 0}, [3]int8{J, 0, 0})
	set(3, 2, [3]int8{J, 0, 0}, [3]int8{V, 1, 0})
	set(1, 1, [3]int8{H, 0, 0}, [3]int8{V, 0, 0})
	set(3, 1, [3]int8{H, 0, 0}, [3]int8{V, 1, 0})
	set(1, 3, [3]int8{H, 0, 1}, [3]int8{V, 0, 0})
	set(3, 3, [3]int8{H, 0, 1}, [3]int8{V, 1, 0})
}

// qpel returns the w x h prediction of the block at picture position
// (x0, y0) displaced by the quarter-sample vector (mx, my): a slice into a
// plane when no averaging is needed, else buf (stride 16).
func (p *refPic) qpel(buf []byte, x0, y0, mx, my, w, h int) ([]byte, int) {
	ix, iy := x0+mx>>2, y0+my>>2
	src := qpelSources[my&3][mx&3]
	plane := func(i int8) []byte {
		if i == 0 {
			return p.y
		}
		return p.hp[i-1]
	}
	a, b := src[0], src[1]
	pa := plane(a[0])[p.yo+(iy+int(a[2]))*p.ys+ix+int(a[1]):]
	if a == b {
		return pa, p.ys
	}
	pb := plane(b[0])[p.yo+(iy+int(b[2]))*p.ys+ix+int(b[1]):]
	h264.Avg2(buf, 16, pa, p.ys, pb, p.ys, w, h)
	return buf, 16
}
