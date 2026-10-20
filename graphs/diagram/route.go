package diagram

import (
	"math"

	"github.com/qizhanchan/qui"
)

// RouteResult is the geometry produced for one edge.
type RouteResult struct {
	Path         *qui.Path
	Polyline     []Point
	Start        Point
	End          Point
	StartTangent Point
	EndTangent   Point
	LabelPoint   Point
}

func (p *Page) endpoint(edge *Cell, source bool, opposite Point, shapes *ShapeRegistry) (Point, Port, *Cell) {
	ep := edge.Target
	if source {
		ep = edge.Source
	}
	if ep.Cell != "" {
		c := p.Cell(ep.Cell)
		if c != nil {
			rect := c.Bounds()
			kind := c.Style.Shape
			if kind == "" {
				kind = ShapeRect
			}
			if ep.Port != "" && ep.Port != PortAuto {
				return portPoint(rect, ep.Port), ep.Port, c
			}
			port := nearestPort(rect, opposite, PortAuto)
			if shapes != nil {
				return shapes.perimeter(kind, rect, opposite), port, c
			}
			return perimeterPoint(kind, rect, opposite), port, c
		}
	}
	if source && edge.Geometry.SourcePoint != nil {
		return *edge.Geometry.SourcePoint, PortAuto, nil
	}
	if !source && edge.Geometry.TargetPoint != nil {
		return *edge.Geometry.TargetPoint, PortAuto, nil
	}
	return Point{X: ep.X, Y: ep.Y}, PortAuto, nil
}

func (p *Page) edgeCenters(edge *Cell) (Point, Point) {
	var s, t Point
	if c := p.Cell(edge.Source.Cell); c != nil {
		s = c.Bounds().Center()
	} else if edge.Geometry.SourcePoint != nil {
		s = *edge.Geometry.SourcePoint
	} else {
		s = Point{X: edge.Source.X, Y: edge.Source.Y}
	}
	if c := p.Cell(edge.Target.Cell); c != nil {
		t = c.Bounds().Center()
	} else if edge.Geometry.TargetPoint != nil {
		t = *edge.Geometry.TargetPoint
	} else {
		t = Point{X: edge.Target.X, Y: edge.Target.Y}
	}
	return s, t
}

// Route returns the complete routed geometry for an edge on this page,
// using the built-in perimeter rules.
func (p *Page) Route(edge *Cell) RouteResult {
	return p.RouteWithRegistry(edge, nil)
}

// RouteWithRegistry is Route with application-defined shape perimeter rules.
func (p *Page) RouteWithRegistry(edge *Cell, shapes *ShapeRegistry) RouteResult {
	if p == nil || edge == nil {
		return RouteResult{}
	}
	sc, tc := p.edgeCenters(edge)
	from, fromPort, fromCell := p.endpoint(edge, true, tc, shapes)
	to, toPort, toCell := p.endpoint(edge, false, sc, shapes)
	sourceAuto := isAutoPort(edge.Source.Port)
	targetAuto := isAutoPort(edge.Target.Port)
	style := ResolvedStyle(edge.Style, DefaultTheme().DefaultEdge)
	route := style.Route
	// Orthogonal families must start and end at the side port used by the
	// router. A diagonal perimeter intersection on a diamond can otherwise
	// be hidden by the first orthogonal stub.
	switch route {
	case RouteOrthogonal, RouteRoundedOrthogonal, RoutePolyline, RouteEntityRelation:
		if sourceAuto && fromCell != nil {
			from = portPoint(fromCell.Bounds(), fromPort)
		}
		if targetAuto && toCell != nil {
			to = portPoint(toCell.Bounds(), toPort)
		}
	}

	// Parallel edges fan out around the direct chord.
	if fromCell != nil && toCell != nil {
		off := p.parallelOffset(edge)
		if off != 0 && edge.Source.Cell != edge.Target.Cell {
			dx, dy := to.X-from.X, to.Y-from.Y
			l := float32(math.Hypot(float64(dx), float64(dy)))
			if l > .5 {
				nx, ny := -dy/l, dx/l
				from = Point{X: from.X + nx*off, Y: from.Y + ny*off}
				to = Point{X: to.X + nx*off, Y: to.Y + ny*off}
			}
		}
	}

	if edge.Source.Cell != "" && edge.Source.Cell == edge.Target.Cell {
		if c := p.Cell(edge.Source.Cell); c != nil {
			sfp, tfp := fromPort, toPort
			if sourceAuto {
				sfp = PortAuto
			}
			if targetAuto {
				tfp = PortAuto
			}
			return routeSelfLoop(c.Bounds(), sfp, tfp, p.parallelLoopOffset(edge))
		}
	}

	points := append([]Point(nil), edge.Geometry.Points...)
	var poly, skeleton []Point
	var path *qui.Path

	if len(points) > 0 && route != RouteCurved {
		poly = append([]Point{from}, points...)
		poly = append(poly, to)
		poly = dedupColinear(poly)
		path = polylinePath(poly)
	} else {
		switch route {
		case RouteStraight:
			poly = []Point{from, to}
			path = qui.NewPath().MoveTo(from.X, from.Y).LineTo(to.X, to.Y)
		case RouteEntityRelation:
			poly = entityRelationVertices(from, to, fromPort, toPort, fromCell, toCell, sourceAuto, targetAuto)
			path, poly = roundedOrthogonalPath(poly, 10)
		case RouteCurved:
			path, poly = curvedRoute(from, to, fromPort, toPort, points)
		case RouteRoundedOrthogonal:
			skeleton = p.orthogonalPoly(edge, fromCell, toCell, from, to)
			path, poly = roundedOrthogonalPath(skeleton, 12)
		case RoutePolyline, RouteOrthogonal:
			poly = p.orthogonalPoly(edge, fromCell, toCell, from, to)
			skeleton = poly
			path = polylinePath(poly)
		default:
			poly = []Point{from, to}
			path = qui.NewPath().MoveTo(from.X, from.Y).LineTo(to.X, to.Y)
		}
	}
	poly = dedupColinear(poly)
	if len(poly) < 2 {
		poly = []Point{from, to}
		path = qui.NewPath().MoveTo(from.X, from.Y).LineTo(to.X, to.Y)
	}
	if path == nil {
		path = polylinePath(poly)
	}
	labelPos := edge.Geometry.LabelPosition
	labelPoint := Point{}
	if labelPos <= 0 || labelPos >= 1 {
		labelPos = .5
		labelPoint = pointAlong(poly, labelPos)
		if len(skeleton) > 2 {
			labelPoint = orthLabelPoint(skeleton)
		}
	} else {
		labelPoint = pointAlong(poly, labelPos)
	}
	return RouteResult{
		Path: path, Polyline: poly, Start: poly[0], End: poly[len(poly)-1],
		StartTangent: segmentTangent(poly[1], poly[0]),
		EndTangent:   segmentTangent(poly[len(poly)-2], poly[len(poly)-1]),
		LabelPoint:   labelPoint,
	}
}

// EdgeSamples returns a flattened polyline suitable for hit testing and
// content bounds. extra points are appended to any stored waypoints.
func (p *Page) EdgeSamples(edge *Cell, extra []Point, maxSegments int) []Point {
	if edge == nil {
		return nil
	}
	if len(extra) > 0 && len(edge.Geometry.Points) == 0 {
		clone := *edge
		clone.Geometry.Points = append([]Point(nil), extra...)
		edge = &clone
	}
	r := p.Route(edge)
	if len(r.Polyline) <= maxSegments || maxSegments <= 0 {
		return r.Polyline
	}
	out := make([]Point, 0, maxSegments+1)
	for i := 0; i < maxSegments; i++ {
		t := float32(i) / float32(maxSegments)
		out = append(out, pointAlong(r.Polyline, t))
	}
	out = append(out, r.Polyline[len(r.Polyline)-1])
	return out
}

func (p *Page) parallelOffset(edge *Cell) float32 {
	var peers []*Cell
	for _, c := range p.Cells {
		if c == nil || c.Type != CellEdge {
			continue
		}
		if c.Source.Cell == edge.Source.Cell && c.Target.Cell == edge.Target.Cell {
			peers = append(peers, c)
		}
	}
	if len(peers) <= 1 || edge.Source.Cell == "" || edge.Target.Cell == "" {
		return 0
	}
	idx := 0
	for i, c := range peers {
		if c == edge {
			idx = i
			break
		}
	}
	return (float32(idx) - float32(len(peers)-1)/2) * 28
}

func (p *Page) parallelLoopOffset(edge *Cell) float32 {
	n := 0
	idx := 0
	for _, c := range p.Cells {
		if c == nil || c.Type != CellEdge || c.Source.Cell == "" || c.Source.Cell != c.Target.Cell {
			continue
		}
		if c.Source.Cell == edge.Source.Cell {
			if c == edge {
				idx = n
			}
			n++
		}
	}
	return float32(idx) * 16
}

func routeSelfLoop(rect Rect, sourcePort, targetPort Port, extra float32) RouteResult {
	if sourcePort == "" || sourcePort == PortAuto {
		sourcePort = PortEast
	}
	if targetPort == "" || targetPort == PortAuto || targetPort == sourcePort {
		targetPort = PortNorth
	}
	from := portPoint(rect, sourcePort)
	to := portPoint(rect, targetPort)
	st, tt := portTangent(sourcePort), portTangent(targetPort)
	out := float32(34) + extra
	bend := float32(28) + extra
	var poly []Point
	switch {
	case st.X > .5 && tt.Y < -.5:
		a := Point{X: from.X + out, Y: from.Y}
		b := Point{X: from.X + out, Y: rect.Y - bend}
		c := Point{X: to.X, Y: rect.Y - bend}
		poly = []Point{from, a, b, c, to}
	default:
		a := Point{X: from.X + st.X*out, Y: from.Y + st.Y*out}
		b := Point{X: to.X + tt.X*(out*.55), Y: to.Y + tt.Y*(out*.55)}
		mid := Point{X: (a.X + b.X) / 2, Y: (a.Y + b.Y) / 2}
		poly = []Point{from, a, mid, b, to}
	}
	poly = dedupColinear(poly)
	path, samples := roundedOrthogonalPath(poly, 10)
	return RouteResult{
		Path: path, Polyline: samples, Start: from, End: to,
		StartTangent: segmentTangent(poly[1], poly[0]),
		EndTangent:   segmentTangent(poly[len(poly)-2], poly[len(poly)-1]),
		LabelPoint:   pointAlong(samples, .5),
	}
}

func orthogonalVertices(from, to Point, fp, tp Port, fc, tc *Cell) []Point {
	if fp == "" || fp == PortAuto {
		fp = PortEast
		if to.X < from.X {
			fp = PortWest
		}
	}
	if tp == "" || tp == PortAuto {
		tp = PortWest
		if to.X < from.X {
			tp = PortEast
		}
	}
	fs := portTangent(fp)
	ts := portTangent(tp)
	const stub float32 = 22
	fstub := Point{X: from.X + fs.X*stub, Y: from.Y + fs.Y*stub}
	tstub := Point{X: to.X + ts.X*stub, Y: to.Y + ts.Y*stub}
	var pts []Point
	if abs32(fs.X) > .5 && abs32(ts.X) > .5 {
		trunk := (fstub.X + tstub.X) / 2
		if fc != nil && tc != nil && rectsOverlap(fc.Bounds().Inflate(12), tc.Bounds().Inflate(12)) {
			if fstub.X <= tstub.X {
				trunk = max32(fc.Bounds().X+fc.Bounds().W, tc.Bounds().X+tc.Bounds().W) + 28
			} else {
				trunk = min32(fc.Bounds().X, tc.Bounds().X) - 28
			}
		}
		pts = []Point{from, fstub, {X: trunk, Y: fstub.Y}, {X: trunk, Y: tstub.Y}, tstub, to}
	} else if abs32(fs.Y) > .5 && abs32(ts.Y) > .5 {
		trunk := (fstub.Y + tstub.Y) / 2
		if fc != nil && tc != nil && rectsOverlap(fc.Bounds().Inflate(12), tc.Bounds().Inflate(12)) {
			if fstub.Y <= tstub.Y {
				trunk = max32(fc.Bounds().Y+fc.Bounds().H, tc.Bounds().Y+tc.Bounds().H) + 28
			} else {
				trunk = min32(fc.Bounds().Y, tc.Bounds().Y) - 28
			}
		}
		pts = []Point{from, fstub, {X: fstub.X, Y: trunk}, {X: tstub.X, Y: trunk}, tstub, to}
	} else if abs32(fs.X) > .5 {
		pts = []Point{from, fstub, {X: fstub.X, Y: tstub.Y}, tstub, to}
	} else {
		pts = []Point{from, fstub, {X: tstub.X, Y: fstub.Y}, tstub, to}
	}
	return dedupColinear(pts)
}

func entityRelationVertices(from, to Point, fp, tp Port, fc, tc *Cell, sourceAuto, targetAuto bool) []Point {
	if fc != nil && tc != nil {
		sr, tr := fc.Bounds(), tc.Bounds()
		sc, tcCenter := sr.Center(), tr.Center()
		verticallyAligned := abs32(sc.X-tcCenter.X) < min32(sr.W, tr.W)/2
		if verticallyAligned && abs32(sc.Y-tcCenter.Y) > (sr.H+tr.H)/2 {
			side := PortEast
			if tcCenter.X < sc.X {
				side = PortWest
			}
			if sourceAuto {
				fp = side
			}
			if targetAuto {
				tp = side
			}
		} else {
			if sourceAuto {
				fp = nearestPort(sr, tcCenter, PortEast)
			}
			if targetAuto {
				tp = nearestPort(tr, sc, PortWest)
			}
		}
		from = portPoint(sr, fp)
		to = portPoint(tr, tp)
	}
	fs, ts := portTangent(fp), portTangent(tp)
	stub := float32(26)
	if (fs.X > 0 && ts.X > 0) || (fs.X < 0 && ts.X < 0) || (fs.Y > 0 && ts.Y > 0) || (fs.Y < 0 && ts.Y < 0) {
		stub = 20
	}
	fstub := Point{X: from.X + fs.X*stub, Y: from.Y + fs.Y*stub}
	tstub := Point{X: to.X + ts.X*stub, Y: to.Y + ts.Y*stub}
	var pts []Point
	if abs32(fs.X) > .5 && abs32(ts.X) > .5 {
		trunk := (fstub.X + tstub.X) / 2
		if (fs.X > 0 && ts.X > 0) || (fs.X < 0 && ts.X < 0) {
			trunk = max32(fstub.X, tstub.X) + 10
			if fs.X < 0 {
				trunk = min32(fstub.X, tstub.X) - 10
			}
		}
		pts = []Point{from, fstub, {X: trunk, Y: fstub.Y}, {X: trunk, Y: tstub.Y}, tstub, to}
	} else if abs32(fs.Y) > .5 && abs32(ts.Y) > .5 {
		trunk := (fstub.Y + tstub.Y) / 2
		pts = []Point{from, fstub, {X: fstub.X, Y: trunk}, {X: tstub.X, Y: trunk}, tstub, to}
	} else {
		pts = orthogonalVertices(from, to, fp, tp, fc, tc)
	}
	return dedupColinear(pts)
}

func rectsOverlap(a, b Rect) bool {
	return !(a.X+a.W <= b.X || b.X+b.W <= a.X || a.Y+a.H <= b.Y || b.Y+b.H <= a.Y)
}

func curvedRoute(from, to Point, fp, tp Port, waypoints []Point) (*qui.Path, []Point) {
	if len(waypoints) == 0 {
		fs, ts := portTangent(fp), portTangent(tp)
		l := float32(math.Hypot(float64(to.X-from.X), float64(to.Y-from.Y)))
		h := clamp32(l*.38, 32, 180)
		c1 := Point{X: from.X + fs.X*h, Y: from.Y + fs.Y*h}
		c2 := Point{X: to.X + ts.X*h, Y: to.Y + ts.Y*h}
		path := qui.NewPath().MoveTo(from.X, from.Y).CubicTo(c1.X, c1.Y, c2.X, c2.Y, to.X, to.Y)
		poly := make([]Point, 0, 25)
		for i := 0; i <= 24; i++ {
			poly = append(poly, cubicPoint(from, c1, c2, to, float32(i)/24))
		}
		return path, poly
	}
	pts := append([]Point{from}, waypoints...)
	pts = append(pts, to)
	path := qui.NewPath().MoveTo(pts[0].X, pts[0].Y)
	var samples []Point
	samples = append(samples, pts[0])
	for i := 0; i < len(pts)-1; i++ {
		p0 := pts[i]
		if i > 0 {
			p0 = pts[i-1]
		}
		p1 := pts[i]
		p2 := pts[i+1]
		p3 := p2
		if i+2 < len(pts) {
			p3 = pts[i+2]
		}
		c1 := Point{X: p1.X + (p2.X-p0.X)/6, Y: p1.Y + (p2.Y-p0.Y)/6}
		c2 := Point{X: p2.X - (p3.X-p1.X)/6, Y: p2.Y - (p3.Y-p1.Y)/6}
		path.CubicTo(c1.X, c1.Y, c2.X, c2.Y, p2.X, p2.Y)
		for j := 1; j <= 12; j++ {
			samples = append(samples, cubicPoint(p1, c1, c2, p2, float32(j)/12))
		}
	}
	return path, samples
}

func roundedOrthogonalPath(pts []Point, radius float32) (*qui.Path, []Point) {
	pts = dedupColinear(pts)
	if len(pts) < 2 {
		return qui.NewPath(), pts
	}
	path := qui.NewPath().MoveTo(pts[0].X, pts[0].Y)
	samples := []Point{pts[0]}
	appendLine := func(a, b Point) {
		path.LineTo(b.X, b.Y)
	}
	for i := 1; i < len(pts)-1; i++ {
		a, b, c := pts[i-1], pts[i], pts[i+1]
		la, lc := dist32(a, b), dist32(b, c)
		r := radius
		if la/2 < r {
			r = la / 2
		}
		if lc/2 < r {
			r = lc / 2
		}
		if r <= .5 {
			appendLine(a, b)
			samples = append(samples, b)
			continue
		}
		ab := normalize32(Point{X: b.X - a.X, Y: b.Y - a.Y})
		bc := normalize32(Point{X: c.X - b.X, Y: c.Y - b.Y})
		in := Point{X: b.X - ab.X*r, Y: b.Y - ab.Y*r}
		out := Point{X: b.X + bc.X*r, Y: b.Y + bc.Y*r}
		appendLine(a, in)
		samples = append(samples, in)
		path.QuadTo(b.X, b.Y, out.X, out.Y)
		for j := 1; j <= 8; j++ {
			samples = append(samples, quadPoint(in, b, out, float32(j)/8))
		}
	}
	last := pts[len(pts)-1]
	appendLine(pts[len(pts)-2], last)
	samples = append(samples, last)
	return path, dedupColinear(samples)
}

func polylinePath(pts []Point) *qui.Path {
	if len(pts) == 0 {
		return qui.NewPath()
	}
	p := qui.NewPath().MoveTo(pts[0].X, pts[0].Y)
	for _, pt := range pts[1:] {
		p.LineTo(pt.X, pt.Y)
	}
	return p
}

func dedupColinear(pts []Point) []Point {
	if len(pts) <= 2 {
		return pts
	}
	out := make([]Point, 0, len(pts))
	out = append(out, pts[0])
	for i := 1; i < len(pts)-1; i++ {
		a, b, c := pts[i-1], pts[i], pts[i+1]
		if a == b {
			continue
		}
		cross := (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X)
		if abs32(cross) >= .01 {
			out = append(out, b)
			continue
		}
		dot := (b.X-a.X)*(c.X-b.X) + (b.Y-a.Y)*(c.Y-b.Y)
		if dot < 0 {
			out = append(out, b)
		}
	}
	out = append(out, pts[len(pts)-1])
	return out
}

func segmentTangent(a, b Point) Point { return normalize32(Point{X: b.X - a.X, Y: b.Y - a.Y}) }

func normalize32(p Point) Point {
	l := float32(math.Hypot(float64(p.X), float64(p.Y)))
	if l < 1e-6 {
		return Point{}
	}
	return Point{X: p.X / l, Y: p.Y / l}
}

func dist32(a, b Point) float32 {
	return float32(math.Hypot(float64(b.X-a.X), float64(b.Y-a.Y)))
}

func cubicPoint(p0, p1, p2, p3 Point, t float32) Point {
	u := 1 - t
	a := u * u * u
	b := 3 * u * u * t
	c := 3 * u * t * t
	d := t * t * t
	return Point{X: a*p0.X + b*p1.X + c*p2.X + d*p3.X, Y: a*p0.Y + b*p1.Y + c*p2.Y + d*p3.Y}
}

func quadPoint(p0, p1, p2 Point, t float32) Point {
	u := 1 - t
	return Point{X: u*u*p0.X + 2*u*t*p1.X + t*t*p2.X, Y: u*u*p0.Y + 2*u*t*p1.Y + t*t*p2.Y}
}

func pointAlong(pts []Point, t float32) Point {
	if len(pts) == 0 {
		return Point{}
	}
	if len(pts) == 1 {
		return pts[0]
	}
	if t <= 0 {
		return pts[0]
	}
	if t >= 1 {
		return pts[len(pts)-1]
	}
	var total float32
	lengths := make([]float32, len(pts)-1)
	for i := 0; i < len(pts)-1; i++ {
		lengths[i] = dist32(pts[i], pts[i+1])
		total += lengths[i]
	}
	if total == 0 {
		return pts[0]
	}
	target := total * t
	for i, l := range lengths {
		if target <= l || i == len(lengths)-1 {
			u := target / l
			return Point{X: pts[i].X + (pts[i+1].X-pts[i].X)*u, Y: pts[i].Y + (pts[i+1].Y-pts[i].Y)*u}
		}
		target -= l
	}
	return pts[len(pts)-1]
}

func clamp32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func isAutoPort(port Port) bool { return port == "" || port == PortAuto }
