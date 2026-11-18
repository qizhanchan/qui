package canvas2d

import (
	"math"

	"github.com/qizhanchan/qui"
)

// The path model. A canvas path is built in user space but stored in
// device space: every point is mapped through the transform current when
// it is added (so `translate(); lineTo()` moves only the new point). A
// Path2D is stored in its own (user) space and mapped by the transform
// current when it is drawn. Both are pathData; the matrix passed to the
// builders is the one to apply.

type segOp uint8

const (
	segMove segOp = iota
	segLine
	segQuad
	segCubic
	segClose
)

type seg struct {
	op segOp
	p  [3]qui.Point
}

type pathData struct {
	segs  []seg
	start qui.Point // current subpath's first point
	cur   qui.Point
	has   bool // a current point exists
}

func (d *pathData) reset() { *d = pathData{segs: d.segs[:0]} }

func (d *pathData) clone() pathData {
	c := *d
	c.segs = append([]seg(nil), d.segs...)
	return c
}

func finite(vs ...float64) bool {
	for _, v := range vs {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

func pt(m qui.Matrix, x, y float64) qui.Point {
	return m.TransformPoint(qui.Point{X: float32(x), Y: float32(y)})
}

func (d *pathData) moveTo(m qui.Matrix, x, y float64) {
	if !finite(x, y) {
		return
	}
	p := pt(m, x, y)
	d.segs = append(d.segs, seg{op: segMove, p: [3]qui.Point{p}})
	d.start, d.cur, d.has = p, p, true
}

// ensure is the spec's "ensure there is a subpath": a builder with no
// current point starts one at (x, y).
func (d *pathData) ensure(m qui.Matrix, x, y float64) {
	if !d.has {
		d.moveTo(m, x, y)
	}
}

func (d *pathData) lineTo(m qui.Matrix, x, y float64) {
	if !finite(x, y) {
		return
	}
	if !d.has {
		d.moveTo(m, x, y)
		return
	}
	p := pt(m, x, y)
	d.segs = append(d.segs, seg{op: segLine, p: [3]qui.Point{p}})
	d.cur = p
}

func (d *pathData) quadTo(m qui.Matrix, cx, cy, x, y float64) {
	if !finite(cx, cy, x, y) {
		return
	}
	d.ensure(m, cx, cy)
	c, p := pt(m, cx, cy), pt(m, x, y)
	d.segs = append(d.segs, seg{op: segQuad, p: [3]qui.Point{c, p}})
	d.cur = p
}

func (d *pathData) cubicTo(m qui.Matrix, c1x, c1y, c2x, c2y, x, y float64) {
	if !finite(c1x, c1y, c2x, c2y, x, y) {
		return
	}
	d.ensure(m, c1x, c1y)
	a, b, p := pt(m, c1x, c1y), pt(m, c2x, c2y), pt(m, x, y)
	d.segs = append(d.segs, seg{op: segCubic, p: [3]qui.Point{a, b, p}})
	d.cur = p
}

func (d *pathData) closePath() {
	if !d.has {
		return
	}
	d.segs = append(d.segs, seg{op: segClose})
	d.cur = d.start
}

func (d *pathData) rect(m qui.Matrix, x, y, w, h float64) {
	if !finite(x, y, w, h) {
		return
	}
	d.moveTo(m, x, y)
	d.lineTo(m, x+w, y)
	d.lineTo(m, x+w, y+h)
	d.lineTo(m, x, y+h)
	d.closePath()
	d.moveTo(m, x, y)
}

// ellipse appends an elliptical arc (arc() is the circular case). Angles
// follow the HTML sweep rules.
func (d *pathData) ellipse(m qui.Matrix, cx, cy, rx, ry, rot, a0, a1 float64, ccw bool) bool {
	if !finite(cx, cy, rx, ry, rot, a0, a1) {
		return true
	}
	if rx < 0 || ry < 0 {
		return false // IndexSizeError
	}
	const tau = 2 * math.Pi
	var sweep float64
	switch {
	case !ccw && a1-a0 >= tau:
		sweep = tau
	case ccw && a0-a1 >= tau:
		sweep = -tau
	case !ccw:
		sweep = math.Mod(a1-a0, tau)
		if sweep < 0 {
			sweep += tau
		}
	default:
		sweep = math.Mod(a0-a1, tau)
		if sweep < 0 {
			sweep += tau
		}
		sweep = -sweep
	}
	sr, cr := math.Sincos(rot)
	at := func(t float64) (float64, float64) {
		s, c := math.Sincos(t)
		x, y := rx*c, ry*s
		return cx + x*cr - y*sr, cy + x*sr + y*cr
	}
	x0, y0 := at(a0)
	if d.has {
		d.lineTo(m, x0, y0)
	} else {
		d.moveTo(m, x0, y0)
	}
	if sweep == 0 || rx == 0 || ry == 0 {
		x1, y1 := at(a0 + sweep)
		d.lineTo(m, x1, y1)
		return true
	}
	n := int(math.Ceil(math.Abs(sweep) / (math.Pi / 2)))
	step := sweep / float64(n)
	k := 4.0 / 3.0 * math.Tan(step/4)
	deriv := func(t float64) (float64, float64) {
		s, c := math.Sincos(t)
		x, y := -rx*s, ry*c
		return x*cr - y*sr, x*sr + y*cr
	}
	t := a0
	for i := 0; i < n; i++ {
		t1 := t + step
		if i == n-1 {
			t1 = a0 + sweep
		}
		p0x, p0y := at(t)
		p1x, p1y := at(t1)
		d0x, d0y := deriv(t)
		d1x, d1y := deriv(t1)
		d.cubicTo(m, p0x+k*d0x, p0y+k*d0y, p1x-k*d1x, p1y-k*d1y, p1x, p1y)
		t = t1
	}
	return true
}

// arcTo appends the arc tangent to the lines (cur→p1) and (p1→p2).
// curUser is the current point in user space.
func (d *pathData) arcTo(m qui.Matrix, curUser qui.Point, x1, y1, x2, y2, r float64) bool {
	if !finite(x1, y1, x2, y2, r) {
		return true
	}
	if r < 0 {
		return false
	}
	if !d.has {
		d.moveTo(m, x1, y1)
		return true
	}
	x0, y0 := float64(curUser.X), float64(curUser.Y)
	if (x0 == x1 && y0 == y1) || (x1 == x2 && y1 == y2) || r == 0 {
		d.lineTo(m, x1, y1)
		return true
	}
	// Unit vectors from p1 toward p0 and p2.
	ax, ay := x0-x1, y0-y1
	bx, by := x2-x1, y2-y1
	la, lb := math.Hypot(ax, ay), math.Hypot(bx, by)
	ax, ay, bx, by = ax/la, ay/la, bx/lb, by/lb
	cross := ax*by - ay*bx
	if math.Abs(cross) < 1e-9 {
		d.lineTo(m, x1, y1)
		return true
	}
	cosT := ax*bx + ay*by
	theta := math.Acos(math.Max(-1, math.Min(1, cosT))) // angle at p1
	dist := r / math.Tan(theta/2)                         // p1 → tangent points
	t0x, t0y := x1+ax*dist, y1+ay*dist
	t1x, t1y := x1+bx*dist, y1+by*dist
	// Center: along the bisector at r / sin(theta/2).
	hx, hy := ax+bx, ay+by
	hl := math.Hypot(hx, hy)
	cdist := r / math.Sin(theta/2)
	ccx, ccy := x1+hx/hl*cdist, y1+hy/hl*cdist
	a0 := math.Atan2(t0y-ccy, t0x-ccx)
	a1 := math.Atan2(t1y-ccy, t1x-ccx)
	d.lineTo(m, t0x, t0y)
	d.ellipse(m, ccx, ccy, r, r, 0, a0, a1, cross > 0)
	return true
}

// Radius is one roundRect corner radius.
type Radius struct{ X, Y float64 }

// roundRect appends a rounded rectangle; radii has 1–4 entries in the CSS
// order. It returns false for a negative radius (RangeError).
func (d *pathData) roundRect(m qui.Matrix, x, y, w, h float64, radii []Radius) bool {
	if !finite(x, y, w, h) {
		return true
	}
	for _, r := range radii {
		if !finite(r.X, r.Y) {
			return true
		}
		if r.X < 0 || r.Y < 0 {
			return false
		}
	}
	var tl, tr, br, bl Radius
	switch len(radii) {
	case 0:
	case 1:
		tl, tr, br, bl = radii[0], radii[0], radii[0], radii[0]
	case 2:
		tl, br = radii[0], radii[0]
		tr, bl = radii[1], radii[1]
	case 3:
		tl, tr, bl, br = radii[0], radii[1], radii[1], radii[2]
	default:
		tl, tr, br, bl = radii[0], radii[1], radii[2], radii[3]
	}
	// Negative sizes flip the rectangle (and its corners).
	if w < 0 {
		x, w = x+w, -w
		tl, tr = tr, tl
		bl, br = br, bl
	}
	if h < 0 {
		y, h = y+h, -h
		tl, bl = bl, tl
		tr, br = br, tr
	}
	// Scale down overlapping radii.
	f := 1.0
	for _, s := range [][3]float64{
		{w, tl.X, tr.X}, {w, bl.X, br.X}, {h, tl.Y, bl.Y}, {h, tr.Y, br.Y},
	} {
		if sum := s[1] + s[2]; sum > s[0] && sum > 0 {
			f = math.Min(f, s[0]/sum)
		}
	}
	for _, c := range []*Radius{&tl, &tr, &br, &bl} {
		c.X *= f
		c.Y *= f
	}
	const hp = math.Pi / 2
	d.moveTo(m, x+tl.X, y)
	d.lineTo(m, x+w-tr.X, y)
	d.ellipse(m, x+w-tr.X, y+tr.Y, tr.X, tr.Y, 0, -hp, 0, false)
	d.lineTo(m, x+w, y+h-br.Y)
	d.ellipse(m, x+w-br.X, y+h-br.Y, br.X, br.Y, 0, 0, hp, false)
	d.lineTo(m, x+bl.X, y+h)
	d.ellipse(m, x+bl.X, y+h-bl.Y, bl.X, bl.Y, 0, hp, math.Pi, false)
	d.lineTo(m, x, y+tl.Y)
	d.ellipse(m, x+tl.X, y+tl.Y, tl.X, tl.Y, 0, math.Pi, math.Pi+hp, false)
	d.closePath()
	d.moveTo(m, x, y)
	return true
}

// appendPath adds src (in its own space) mapped through m.
func (d *pathData) appendPath(src *pathData, m qui.Matrix) {
	for _, s := range src.segs {
		switch s.op {
		case segMove:
			p := m.TransformPoint(s.p[0])
			d.segs = append(d.segs, seg{op: segMove, p: [3]qui.Point{p}})
			d.start, d.cur, d.has = p, p, true
		case segClose:
			d.segs = append(d.segs, s)
			d.cur = d.start
		default:
			n := s
			for i := range n.p {
				n.p[i] = m.TransformPoint(n.p[i])
			}
			d.segs = append(d.segs, n)
			d.cur = n.p[segEnd(s.op)]
		}
	}
}

func segEnd(op segOp) int {
	switch op {
	case segQuad:
		return 1
	case segCubic:
		return 2
	}
	return 0
}

// gfxPath converts to a qui path, mapping every point through m.
func (d *pathData) gfxPath(m qui.Matrix) *qui.Path {
	p := qui.NewPath()
	open := false
	for _, s := range d.segs {
		switch s.op {
		case segMove:
			q := m.TransformPoint(s.p[0])
			p.MoveTo(q.X, q.Y)
			open = true
		case segLine:
			q := m.TransformPoint(s.p[0])
			p.LineTo(q.X, q.Y)
		case segQuad:
			c, q := m.TransformPoint(s.p[0]), m.TransformPoint(s.p[1])
			p.QuadTo(c.X, c.Y, q.X, q.Y)
		case segCubic:
			a, b, q := m.TransformPoint(s.p[0]), m.TransformPoint(s.p[1]), m.TransformPoint(s.p[2])
			p.CubicTo(a.X, a.Y, b.X, b.Y, q.X, q.Y)
		case segClose:
			if open {
				p.Close()
			}
		}
	}
	return p
}

// polyline is one flattened subpath.
type polyline struct {
	pts    []qui.Point
	closed bool
}

// flatten maps the path through m and flattens curves (tol in output
// units).
func (d *pathData) flatten(m qui.Matrix, tol float32) []polyline {
	var out []polyline
	var cur *polyline
	var last qui.Point
	add := func(p qui.Point) {
		cur.pts = append(cur.pts, p)
		last = p
	}
	for _, s := range d.segs {
		switch s.op {
		case segMove:
			out = append(out, polyline{})
			cur = &out[len(out)-1]
			add(m.TransformPoint(s.p[0]))
		case segLine:
			add(m.TransformPoint(s.p[0]))
		case segQuad:
			c, q := m.TransformPoint(s.p[0]), m.TransformPoint(s.p[1])
			p0 := last
			n := curveSteps(dist(p0, c)+dist(c, q), tol)
			for i := 1; i <= n; i++ {
				t := float32(i) / float32(n)
				u := 1 - t
				add(qui.Point{X: u*u*p0.X + 2*u*t*c.X + t*t*q.X, Y: u*u*p0.Y + 2*u*t*c.Y + t*t*q.Y})
			}
		case segCubic:
			a, b, q := m.TransformPoint(s.p[0]), m.TransformPoint(s.p[1]), m.TransformPoint(s.p[2])
			p0 := last
			n := curveSteps(dist(p0, a)+dist(a, b)+dist(b, q), tol)
			for i := 1; i <= n; i++ {
				t := float32(i) / float32(n)
				u := 1 - t
				add(qui.Point{
					X: u*u*u*p0.X + 3*u*u*t*a.X + 3*u*t*t*b.X + t*t*t*q.X,
					Y: u*u*u*p0.Y + 3*u*u*t*a.Y + 3*u*t*t*b.Y + t*t*t*q.Y,
				})
			}
		case segClose:
			if cur != nil {
				cur.closed = true
				first := cur.pts[0]
				// A new segment after close starts at the subpath start.
				out = append(out, polyline{})
				cur = &out[len(out)-1]
				add(first)
			}
		}
	}
	// Drop the empty continuations close() opened.
	kept := out[:0]
	for _, p := range out {
		if len(p.pts) > 1 || p.closed {
			kept = append(kept, p)
		}
	}
	return kept
}

func dist(a, b qui.Point) float32 {
	return float32(math.Hypot(float64(a.X-b.X), float64(a.Y-b.Y)))
}

func curveSteps(length, tol float32) int {
	if tol <= 0 {
		tol = 0.25
	}
	n := int(math.Ceil(math.Sqrt(float64(length / tol))))
	return max(2, min(n, 256))
}

// contains is the winding test against device-space polylines.
func containsPoint(polys []polyline, p qui.Point, evenOdd bool) bool {
	wind := 0
	for _, pl := range polys {
		n := len(pl.pts)
		for i := 0; i < n; i++ {
			a, b := pl.pts[i], pl.pts[(i+1)%n]
			if a.Y <= p.Y {
				if b.Y > p.Y && cross(a, b, p) > 0 {
					wind++
				}
			} else if b.Y <= p.Y && cross(a, b, p) < 0 {
				wind--
			}
		}
	}
	if evenOdd {
		return wind%2 != 0
	}
	return wind != 0
}

func cross(a, b, p qui.Point) float32 {
	return (b.X-a.X)*(p.Y-a.Y) - (p.X-a.X)*(b.Y-a.Y)
}

// nearStroke reports whether p lies within half a line width of a segment.
func nearStroke(polys []polyline, p qui.Point, half float32) bool {
	for _, pl := range polys {
		n := len(pl.pts)
		segs := n - 1
		if pl.closed {
			segs = n
		}
		for i := 0; i < segs; i++ {
			a, b := pl.pts[i], pl.pts[(i+1)%n]
			if segDist(a, b, p) <= half {
				return true
			}
		}
	}
	return false
}

func segDist(a, b, p qui.Point) float32 {
	dx, dy := b.X-a.X, b.Y-a.Y
	l2 := dx*dx + dy*dy
	t := float32(0)
	if l2 > 0 {
		t = ((p.X-a.X)*dx + (p.Y-a.Y)*dy) / l2
		t = max(0, min(1, t))
	}
	return dist(p, qui.Point{X: a.X + t*dx, Y: a.Y + t*dy})
}

// dashed applies a dash pattern (user units) to flattened polylines and
// returns the "on" pieces as a path of open subpaths.
func dashed(polys []polyline, dash []float64, offset float64) *qui.Path {
	out := qui.NewPath()
	total := 0.0
	for _, d := range dash {
		total += d
	}
	if total <= 0 {
		return nil
	}
	for _, pl := range polys {
		pts := pl.pts
		if pl.closed && len(pts) > 0 {
			pts = append(append([]qui.Point(nil), pts...), pts[0])
		}
		// Position in the pattern at the start of this subpath.
		pos := math.Mod(offset, total)
		if pos < 0 {
			pos += total
		}
		idx := 0
		for pos >= dash[idx] {
			pos -= dash[idx]
			idx = (idx + 1) % len(dash)
		}
		on := idx%2 == 0
		left := dash[idx] - pos
		penDown := false
		for i := 0; i+1 < len(pts); i++ {
			a, b := pts[i], pts[i+1]
			segLen := float64(dist(a, b))
			done := 0.0
			for segLen-done > 1e-9 {
				step := math.Min(left, segLen-done)
				t0, t1 := done/segLen, (done+step)/segLen
				if on {
					p0 := lerpPt(a, b, t0)
					p1 := lerpPt(a, b, t1)
					if !penDown {
						out.MoveTo(p0.X, p0.Y)
						penDown = true
					}
					out.LineTo(p1.X, p1.Y)
				}
				done += step
				left -= step
				if left <= 1e-9 {
					idx = (idx + 1) % len(dash)
					left = dash[idx]
					on = idx%2 == 0
					penDown = false
				}
			}
		}
	}
	return out
}

func lerpPt(a, b qui.Point, t float64) qui.Point {
	f := float32(t)
	return qui.Point{X: a.X + (b.X-a.X)*f, Y: a.Y + (b.Y-a.Y)*f}
}
