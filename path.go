package qui

import "math"

// Path is a Skia-style sequence of moveTo / lineTo / quadTo / cubicTo /
// close commands that compose an arbitrary shape. Use as the geometry
// input to Canvas.DrawShape via ShapePath.
//
// Construction is chainable:
//
//	p := qui.NewPath().
//	    MoveTo(10, 10).
//	    QuadTo(50, 0, 100, 10).
//	    LineTo(100, 50).
//	    Close()
//
// Stroke rasterization flattens quadratic and cubic Béziers into short
// line segments (16 / 24 steps per curve at the default tolerance)
// and walks them with the existing capsule rasterizer. Fill is
// implemented via 4x-supersampled scanline with a non-zero winding
// rule by default; Paint.FillRule can switch to even-odd for SVG-style
// self-intersecting shapes. Paint.AntiAlias enables the 4x subsample
// path — set to false for pixel-perfect snapshot tests.
type Path struct {
	cmds  []pathCmd
	start Point // last MoveTo target — Close() draws back to here
	pen   Point // current pen position
}

type pathCmd struct {
	op      pathOp
	a, b, c Point
}

type pathOp int8

const (
	pathMove  pathOp = iota // a = target
	pathLine                // a = end
	pathQuad                // a = control, b = end
	pathCubic               // a, b = controls, c = end
	pathClose               // a = original moveTo target (snapshot)
)

// NewPath returns an empty Path ready for building.
func NewPath() *Path { return &Path{} }

// MoveTo starts a new subpath at (x, y). Subsequent LineTo / QuadTo /
// CubicTo are relative to this anchor for Close purposes.
func (p *Path) MoveTo(x, y float32) *Path {
	pt := Point{X: x, Y: y}
	p.cmds = append(p.cmds, pathCmd{op: pathMove, a: pt})
	p.start = pt
	p.pen = pt
	return p
}

// LineTo draws a straight line from the current pen position to (x, y).
func (p *Path) LineTo(x, y float32) *Path {
	pt := Point{X: x, Y: y}
	p.cmds = append(p.cmds, pathCmd{op: pathLine, a: pt})
	p.pen = pt
	return p
}

// QuadTo draws a quadratic Bézier from the current pen position to
// (x, y), with control point (cx, cy). Flattened at stroke time.
func (p *Path) QuadTo(cx, cy, x, y float32) *Path {
	p.cmds = append(p.cmds, pathCmd{
		op: pathQuad,
		a:  Point{X: cx, Y: cy},
		b:  Point{X: x, Y: y},
	})
	p.pen = Point{X: x, Y: y}
	return p
}

// CubicTo draws a cubic Bézier from the current pen position to (x, y),
// with control points (c1x, c1y) and (c2x, c2y).
func (p *Path) CubicTo(c1x, c1y, c2x, c2y, x, y float32) *Path {
	p.cmds = append(p.cmds, pathCmd{
		op: pathCubic,
		a:  Point{X: c1x, Y: c1y},
		b:  Point{X: c2x, Y: c2y},
		c:  Point{X: x, Y: y},
	})
	p.pen = Point{X: x, Y: y}
	return p
}

// Close draws a straight line from the current pen position back to
// the most recent MoveTo target, closing the subpath.
func (p *Path) Close() *Path {
	p.cmds = append(p.cmds, pathCmd{op: pathClose, a: p.start})
	p.pen = p.start
	return p
}

// AddRect adds a closed rectangle subpath. Useful for compound shapes
// (e.g. a clipping mask, or a card with an internal cutout).
func (p *Path) AddRect(rect Rect) *Path {
	p.MoveTo(rect.X, rect.Y)
	p.LineTo(rect.X+rect.W, rect.Y)
	p.LineTo(rect.X+rect.W, rect.Y+rect.H)
	p.LineTo(rect.X, rect.Y+rect.H)
	p.Close()
	return p
}

// AddOval inscribes an oval (ellipse) into rect using 4 cubic Bézier
// segments, the standard "kappa" approximation. For a perfect circle
// pass a square rect.
func (p *Path) AddOval(rect Rect) *Path {
	const kappa float32 = 0.5522847498
	cx := rect.X + rect.W/2
	cy := rect.Y + rect.H/2
	rx := rect.W / 2
	ry := rect.H / 2
	kx := rx * kappa
	ky := ry * kappa

	p.MoveTo(cx+rx, cy)
	p.CubicTo(cx+rx, cy+ky, cx+kx, cy+ry, cx, cy+ry)
	p.CubicTo(cx-kx, cy+ry, cx-rx, cy+ky, cx-rx, cy)
	p.CubicTo(cx-rx, cy-ky, cx-kx, cy-ry, cx, cy-ry)
	p.CubicTo(cx+kx, cy-ry, cx+rx, cy-ky, cx+rx, cy)
	p.Close()
	return p
}

// AddCircle is a convenience wrapper for a centered circle.
func (p *Path) AddCircle(cx, cy, radius float32) *Path {
	return p.AddOval(Rect{X: cx - radius, Y: cy - radius, W: 2 * radius, H: 2 * radius})
}

// AddRRect adds a closed rounded-rectangle subpath. Radius clamps to
// min(W, H)/2 so a radius larger than the rect produces a pill/circle.
// Uses the same kappa cubic-Bézier approximation as AddOval at each
// corner.
func (p *Path) AddRRect(rect Rect, radius float32) *Path {
	if radius < 0 {
		radius = 0
	}
	maxR := rect.W / 2
	if rect.H/2 < maxR {
		maxR = rect.H / 2
	}
	if radius > maxR {
		radius = maxR
	}
	if radius == 0 {
		return p.AddRect(rect)
	}
	const kappa float32 = 0.5522847498
	r := radius
	k := r * kappa
	x0 := rect.X
	y0 := rect.Y
	x1 := rect.X + rect.W
	y1 := rect.Y + rect.H

	p.MoveTo(x0+r, y0)
	p.LineTo(x1-r, y0)
	p.CubicTo(x1-r+k, y0, x1, y0+r-k, x1, y0+r)
	p.LineTo(x1, y1-r)
	p.CubicTo(x1, y1-r+k, x1-r+k, y1, x1-r, y1)
	p.LineTo(x0+r, y1)
	p.CubicTo(x0+r-k, y1, x0, y1-r+k, x0, y1-r)
	p.LineTo(x0, y0+r)
	p.CubicTo(x0, y0+r-k, x0+r-k, y0, x0+r, y0)
	p.Close()
	return p
}

// AddRRectCorners adds a closed rounded-rectangle subpath with an
// independent radius per corner (CSS border-*-radius). Corners are given
// clockwise from the top-left: tl, tr, br, bl. Negative radii clamp to 0.
// Adjacent radii sharing an edge are scaled down proportionally when their
// sum would exceed that edge's length (the CSS overlap rule), so a corner
// never bulges past the rect. When all four are equal this matches AddRRect.
func (p *Path) AddRRectCorners(rect Rect, tl, tr, br, bl float32) *Path {
	clamp := func(v float32) float32 {
		if v < 0 {
			return 0
		}
		return v
	}
	tl, tr, br, bl = clamp(tl), clamp(tr), clamp(br), clamp(bl)
	if tl == 0 && tr == 0 && br == 0 && bl == 0 {
		return p.AddRect(rect)
	}
	// CSS overlap rule: scale every radius by the min per-edge ratio so no
	// two corners on the same edge overrun it.
	scale := float32(1)
	edge := func(sum, length float32) {
		if sum > length && sum > 0 {
			if r := length / sum; r < scale {
				scale = r
			}
		}
	}
	edge(tl+tr, rect.W)
	edge(bl+br, rect.W)
	edge(tl+bl, rect.H)
	edge(tr+br, rect.H)
	if scale < 1 {
		tl, tr, br, bl = tl*scale, tr*scale, br*scale, bl*scale
	}
	const kappa float32 = 0.5522847498
	x0, y0 := rect.X, rect.Y
	x1, y1 := rect.X+rect.W, rect.Y+rect.H

	p.MoveTo(x0+tl, y0)
	p.LineTo(x1-tr, y0)
	if tr > 0 {
		k := tr * kappa
		p.CubicTo(x1-tr+k, y0, x1, y0+tr-k, x1, y0+tr)
	}
	p.LineTo(x1, y1-br)
	if br > 0 {
		k := br * kappa
		p.CubicTo(x1, y1-br+k, x1-br+k, y1, x1-br, y1)
	}
	p.LineTo(x0+bl, y1)
	if bl > 0 {
		k := bl * kappa
		p.CubicTo(x0+bl-k, y1, x0, y1-bl+k, x0, y1-bl)
	}
	p.LineTo(x0, y0+tl)
	if tl > 0 {
		k := tl * kappa
		p.CubicTo(x0, y0+tl-k, x0+tl-k, y0, x0+tl, y0)
	}
	p.Close()
	return p
}

// subIsClosed reports whether the subpath whose flattened points are
// `sub` originated from a Close()'d subpath. Used by the stroke
// rasterizer to decide whether to paint a join (closed) or caps
// (open) at the subpath boundary.
//
// Heuristic: closed subpaths have a final point equal to their first.
// Path.flatten emits exactly that when it sees pathClose.
func (p *Path) subIsClosed(sub []Point) bool {
	if len(sub) < 2 {
		return false
	}
	first, last := sub[0], sub[len(sub)-1]
	dx := first.X - last.X
	dy := first.Y - last.Y
	return dx*dx+dy*dy < 0.25 // within half-a-pixel
}

// Bounds returns the axis-aligned bounding box of every command point
// in the path. Bézier curves are approximated by their hull (control
// points + endpoints), so the result is a conservative over-estimate
// — good enough for early-out clip checks but not for tight layout.
func (p *Path) Bounds() Rect {
	if len(p.cmds) == 0 {
		return Rect{}
	}
	first := true
	var minX, minY, maxX, maxY float32
	expand := func(q Point) {
		if first {
			minX, minY, maxX, maxY = q.X, q.Y, q.X, q.Y
			first = false
			return
		}
		if q.X < minX {
			minX = q.X
		}
		if q.X > maxX {
			maxX = q.X
		}
		if q.Y < minY {
			minY = q.Y
		}
		if q.Y > maxY {
			maxY = q.Y
		}
	}
	for _, cmd := range p.cmds {
		switch cmd.op {
		case pathMove, pathLine, pathClose:
			expand(cmd.a)
		case pathQuad:
			expand(cmd.a)
			expand(cmd.b)
		case pathCubic:
			expand(cmd.a)
			expand(cmd.b)
			expand(cmd.c)
		}
	}
	return Rect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}
}

// IsEmpty reports whether the path contains no drawing commands.
func (p *Path) IsEmpty() bool { return len(p.cmds) == 0 }

// Polygons returns the path flattened to polylines — one per subpath, with
// every Bézier subdivided into straight segments at the same density the
// rasterizer uses. Open subpaths are returned open; closed ones repeat their
// first point at the end.
//
// This is the geometry hook for callers that need to reason about a path
// rather than paint it: hit-testing (see Contains), exporting to a
// polygon-only format, or measuring an outline.
func (p *Path) Polygons() [][]Point {
	return p.flatten(func(pt Point) Point { return pt })
}

// Contains reports whether pt lies inside the path under the NON-ZERO
// winding rule — the same rule DrawShape fills with by default, so a hit
// test agrees with the pixels the user sees. Every subpath is treated as
// closed (an open subpath is implicitly closed for the purpose of the test,
// matching how fill rasterization treats it).
//
// Cost is O(total flattened segments); callers that test many points against
// one path should keep the path around rather than rebuilding it.
func (p *Path) Contains(pt Point) bool {
	winding := 0
	for _, sub := range p.Polygons() {
		n := len(sub)
		if n < 2 {
			continue
		}
		for i := 0; i < n; i++ {
			a := sub[i]
			b := sub[(i+1)%n] // wraps: implicit close
			if a.Y == b.Y {
				continue
			}
			// Crossing test on a half-open edge span so a vertex shared by
			// two edges is counted exactly once.
			if a.Y <= pt.Y && b.Y > pt.Y {
				if crossX(a, b, pt.Y) > pt.X {
					winding++
				}
			} else if b.Y <= pt.Y && a.Y > pt.Y {
				if crossX(a, b, pt.Y) > pt.X {
					winding--
				}
			}
		}
	}
	return winding != 0
}

// crossX is the X coordinate where segment a→b crosses the horizontal line
// y. Callers guarantee a.Y != b.Y.
func crossX(a, b Point, y float32) float32 {
	return a.X + (y-a.Y)/(b.Y-a.Y)*(b.X-a.X)
}

// flatten produces a slice of polylines (one per subpath) representing
// the path with every Bézier subdivided into straight segments. Used by
// both stroke and fill rasterizers so the curve-flattening logic stays
// in one place.
//
// `transform` is applied to each command point before flattening —
// callers (DrawShape) pass the current state-stack's scale so the
// flattening density matches physical pixels at HiDPI.
func (p *Path) flatten(transform func(Point) Point) [][]Point {
	if len(p.cmds) == 0 {
		return nil
	}
	var subs [][]Point
	var cur []Point
	pen := Point{}
	start := Point{}

	flushSub := func() {
		if len(cur) >= 2 {
			subs = append(subs, cur)
		}
		cur = nil
	}

	for _, cmd := range p.cmds {
		switch cmd.op {
		case pathMove:
			flushSub()
			pen = transform(cmd.a)
			start = pen
			cur = []Point{pen}
		case pathLine:
			end := transform(cmd.a)
			cur = append(cur, end)
			pen = end
		case pathQuad:
			ctrl := transform(cmd.a)
			end := transform(cmd.b)
			cur = flattenQuad(pen, ctrl, end, 16, cur)
			pen = end
		case pathCubic:
			c1 := transform(cmd.a)
			c2 := transform(cmd.b)
			end := transform(cmd.c)
			cur = flattenCubic(pen, c1, c2, end, 24, cur)
			pen = end
		case pathClose:
			if pen != start {
				cur = append(cur, start)
			}
			flushSub()
			pen = start
		}
	}
	flushSub()
	return subs
}

// flattenQuad evaluates a quadratic Bézier at `steps` uniform parameter
// values and appends each sample (excluding the start, which the caller
// already added) to `out`. Pure parametric — adaptive subdivision would
// allocate less for nearly-straight curves but adds complexity not
// worth the win at flowchart densities.
func flattenQuad(p0, p1, p2 Point, steps int, out []Point) []Point {
	if steps < 2 {
		steps = 2
	}
	inv := 1 / float32(steps)
	for i := 1; i <= steps; i++ {
		t := float32(i) * inv
		u := 1 - t
		x := u*u*p0.X + 2*u*t*p1.X + t*t*p2.X
		y := u*u*p0.Y + 2*u*t*p1.Y + t*t*p2.Y
		out = append(out, Point{X: x, Y: y})
	}
	return out
}

func flattenCubic(p0, p1, p2, p3 Point, steps int, out []Point) []Point {
	if steps < 2 {
		steps = 2
	}
	inv := 1 / float32(steps)
	for i := 1; i <= steps; i++ {
		t := float32(i) * inv
		u := 1 - t
		x := u*u*u*p0.X + 3*u*u*t*p1.X + 3*u*t*t*p2.X + t*t*t*p3.X
		y := u*u*u*p0.Y + 3*u*u*t*p1.Y + 3*u*t*t*p2.Y + t*t*t*p3.Y
		out = append(out, Point{X: x, Y: y})
	}
	return out
}

// Compile-time assertion that *Path satisfies the Shape sealing
// interface via the ShapePath wrapper, so DrawShape's type switch
// covers it exhaustively.
var _ Shape = ShapePath{}

// ShapePath is the Shape-compatible wrapper around a Path.
type ShapePath struct {
	*Path
}

func (ShapePath) isShape() {}

// math.Sin / math.Cos float32 wrappers — used by the flowchart-style
// "arrowhead at end of curve" pattern. Live in this file so callers
// don't need to import math just for two helpers.
func sinF(theta float32) float32 { return float32(math.Sin(float64(theta))) }
func cosF(theta float32) float32 { return float32(math.Cos(float64(theta))) }
func atan2F(y, x float32) float32 {
	return float32(math.Atan2(float64(y), float64(x)))
}
