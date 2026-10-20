package diagram

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// checkOrthRoute asserts the invariants every orthogonal route must hold:
// axis-aligned segments, no segment doubling back over the previous one,
// both terminals on a side port, and no segment cutting through either
// terminal's box.
func checkOrthRoute(t *testing.T, name string, r RouteResult, src, dst Rect) {
	t.Helper()
	pts := dedupColinear(r.Polyline)
	if len(pts) < 2 {
		t.Fatalf("%s: route has %d points", name, len(pts))
	}
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		if abs32(a.X-b.X) > .01 && abs32(a.Y-b.Y) > .01 {
			t.Errorf("%s: segment %d %v→%v is diagonal (poly %v)", name, i, a, b, pts)
		}
		if segmentCrossesRect(a, b, src.Inflate(-1)) {
			t.Errorf("%s: segment %d %v→%v cuts through the source (poly %v)", name, i, a, b, pts)
		}
		if segmentCrossesRect(a, b, dst.Inflate(-1)) {
			t.Errorf("%s: segment %d %v→%v cuts through the target (poly %v)", name, i, a, b, pts)
		}
	}
	for i := 2; i < len(pts); i++ {
		d1 := segmentTangent(pts[i-2], pts[i-1])
		d2 := segmentTangent(pts[i-1], pts[i])
		if d1.X*d2.X+d1.Y*d2.Y < -.5 {
			t.Errorf("%s: route doubles back at %v (poly %v)", name, pts[i-1], pts)
		}
	}
	onPort := func(p Point, rect Rect) bool {
		for _, port := range []Port{PortNorth, PortEast, PortSouth, PortWest} {
			if dist32(p, portPoint(rect, port)) <= .01 {
				return true
			}
		}
		return false
	}
	if !onPort(pts[0], src) {
		t.Errorf("%s: route starts at %v, not a source side port", name, pts[0])
	}
	if !onPort(pts[len(pts)-1], dst) {
		t.Errorf("%s: route ends at %v, not a target side port", name, pts[len(pts)-1])
	}
}

func segmentCrossesRect(a, b Point, r Rect) bool {
	if r.Empty() {
		return false
	}
	minX, maxX := min32(a.X, b.X), max32(a.X, b.X)
	minY, maxY := min32(a.Y, b.Y), max32(a.Y, b.Y)
	return maxX > r.X && minX < r.X+r.W && maxY > r.Y && minY < r.Y+r.H
}

// The examples/diagram flowchart with "Load input" dragged left of and
// below "Start": the old midpoint trunk sent Start's west stub back east
// through the ellipse and overshot Load's east side.
func TestOrthogonalRouteDraggedFlowchart(t *testing.T) {
	doc := NewDocument(1000, 640)
	p := doc.Active()
	_ = p.AddCell(&Cell{ID: "start", Type: CellVertex, Geometry: Geometry{X: 780, Y: 60, W: 160, H: 60}, Style: Style{Shape: ShapeEllipse}})
	_ = p.AddCell(&Cell{ID: "load", Type: CellVertex, Geometry: Geometry{X: 605, Y: 200, W: 210, H: 70}})
	_ = p.AddCell(&Cell{ID: "valid", Type: CellVertex, Geometry: Geometry{X: 420, Y: 315, W: 160, H: 90}, Style: Style{Shape: ShapeDiamond}})
	for _, e := range [][2]string{{"start", "load"}, {"load", "valid"}} {
		for _, route := range []RouteKind{RouteOrthogonal, RouteRoundedOrthogonal, RoutePolyline} {
			edge := &Cell{ID: e[0] + "-" + e[1], Type: CellEdge,
				Source: Endpoint{Cell: e[0], Port: PortAuto}, Target: Endpoint{Cell: e[1], Port: PortAuto},
				Style: Style{Route: route}}
			r := p.Route(edge)
			if route == RouteRoundedOrthogonal {
				// Rounded corners sample the bends; check the vertex skeleton.
				r.Polyline = orthogonalSkeleton(p, edge)
			}
			checkOrthRoute(t, fmt.Sprintf("%s %s→%s", route, e[0], e[1]), r, p.Cell(e[0]).Bounds(), p.Cell(e[1]).Bounds())
		}
	}
}

func orthogonalSkeleton(p *Page, edge *Cell) []Point {
	clone := *edge
	clone.Style.Route = RouteOrthogonal
	return p.Route(&clone).Polyline
}

// Sweeps the target all around the source, at distances from overlapping
// to far apart, and checks every auto-port orthogonal route.
func TestOrthogonalRouteSweep(t *testing.T) {
	src := Rect{X: 400, Y: 300, W: 160, H: 60}
	for dx := float32(-420); dx <= 420; dx += 30 {
		for dy := float32(-300); dy <= 300; dy += 25 {
			dst := Rect{X: src.X + dx, Y: src.Y + dy, W: 200, H: 70}
			if rectsOverlap(src.Inflate(20), dst.Inflate(20)) {
				continue // overlapping/abutting boxes have no clean orthogonal route
			}
			doc := NewDocument(1400, 1000)
			p := doc.Active()
			_ = p.AddCell(&Cell{ID: "a", Type: CellVertex, Geometry: Geometry{X: src.X, Y: src.Y, W: src.W, H: src.H}})
			_ = p.AddCell(&Cell{ID: "b", Type: CellVertex, Geometry: Geometry{X: dst.X, Y: dst.Y, W: dst.W, H: dst.H}})
			edge := &Cell{ID: "e", Type: CellEdge, Source: Endpoint{Cell: "a", Port: PortAuto}, Target: Endpoint{Cell: "b", Port: PortAuto},
				Style: Style{Route: RouteOrthogonal}}
			checkOrthRoute(t, fmt.Sprintf("dx=%v dy=%v", dx, dy), p.Route(edge), src, dst)
		}
	}
}

// Fixed ports that face away from each other must still route around both
// boxes instead of through them.
func TestOrthogonalRouteFixedPortsAllCombinations(t *testing.T) {
	ports := []Port{PortNorth, PortEast, PortSouth, PortWest}
	offsets := []Point{{X: 320, Y: 0}, {X: -320, Y: 0}, {X: 0, Y: 220}, {X: 0, Y: -220}, {X: 300, Y: 200}, {X: -300, Y: -200}, {X: 300, Y: -200}, {X: -300, Y: 200}}
	for _, off := range offsets {
		for _, sp := range ports {
			for _, tp := range ports {
				doc := NewDocument(1400, 1000)
				p := doc.Active()
				src := Rect{X: 500, Y: 400, W: 160, H: 60}
				dst := Rect{X: src.X + off.X, Y: src.Y + off.Y, W: 160, H: 60}
				_ = p.AddCell(&Cell{ID: "a", Type: CellVertex, Geometry: Geometry{X: src.X, Y: src.Y, W: src.W, H: src.H}})
				_ = p.AddCell(&Cell{ID: "b", Type: CellVertex, Geometry: Geometry{X: dst.X, Y: dst.Y, W: dst.W, H: dst.H}})
				edge := &Cell{ID: "e", Type: CellEdge, Source: Endpoint{Cell: "a", Port: sp}, Target: Endpoint{Cell: "b", Port: tp},
					Style: Style{Route: RouteOrthogonal}}
				r := p.Route(edge)
				name := fmt.Sprintf("off=%v %s→%s", off, sp, tp)
				checkOrthRoute(t, name, r, src, dst)
				if dist32(r.Start, portPoint(src, sp)) > .01 || dist32(r.End, portPoint(dst, tp)) > .01 {
					t.Errorf("%s: fixed ports not honoured: %v→%v", name, r.Start, r.End)
				}
			}
		}
	}
}

// Layouts reported from examples/diagram (Copy JSON) live in
// testdata/routes; every orthogonal edge in them must route cleanly.
func TestOrthogonalRouteFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/routes/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no route fixtures: %v", err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := DecodeJSONBytes(data)
			if err != nil {
				t.Fatal(err)
			}
			checkPageRoutes(t, doc.Active())
		})
	}
}

func checkPageRoutes(t *testing.T, p *Page) {
	t.Helper()
	type use struct{ in, out bool }
	sides := map[string]*use{}
	for _, e := range p.Cells {
		if e.Type != CellEdge {
			continue
		}
		switch ResolvedStyle(e.Style, DefaultTheme().DefaultEdge).Route {
		case RouteOrthogonal, RouteRoundedOrthogonal, RoutePolyline:
		default:
			continue
		}
		src, dst := p.Cell(e.Source.Cell), p.Cell(e.Target.Cell)
		if src == nil || dst == nil || src == dst {
			continue
		}
		r := p.Route(e)
		skel := orthogonalSkeleton(p, e)
		r.Polyline = skel
		checkOrthRoute(t, e.ID, r, src.Bounds(), dst.Bounds())
		for _, c := range p.Cells {
			if c.Type != CellVertex || c == src || c == dst {
				continue
			}
			for i := 1; i < len(skel); i++ {
				if segmentCrossesRect(skel[i-1], skel[i], c.Bounds()) {
					t.Errorf("%s: segment %v→%v crosses %s", e.ID, skel[i-1], skel[i], c.ID)
				}
			}
		}
		if e.Label != "" {
			// The label must sit on a run long enough to hold it, clear of
			// both of that run's corners.
			lp := p.Route(e).LabelPoint
			placed := false
			for i := 1; i < len(skel); i++ {
				a, b := skel[i-1], skel[i]
				if abs32(dist32(a, lp)+dist32(lp, b)-dist32(a, b)) > .5 {
					continue
				}
				placed = dist32(a, b) >= 60 && dist32(a, lp) >= 20 && dist32(lp, b) >= 20
				break
			}
			if !placed {
				t.Errorf("%s: label %v is not on a clear run of %v", e.ID, lp, skel)
			}
		}
		for _, end := range []struct {
			cell *Cell
			pt   Point
			out  bool
		}{{src, skel[0], true}, {dst, skel[len(skel)-1], false}} {
			key := end.cell.ID + "/" + string(sideOf(end.pt, end.cell.Bounds()))
			u := sides[key]
			if u == nil {
				u = &use{}
				sides[key] = u
			}
			if end.out {
				u.out = true
			} else {
				u.in = true
			}
		}
	}
	for key, u := range sides {
		if u.in && u.out {
			t.Errorf("side %s carries both an incoming and an outgoing edge", key)
		}
	}
}

func sideOf(pt Point, r Rect) Port {
	switch {
	case abs32(pt.X-r.X) < .5:
		return PortWest
	case abs32(pt.X-(r.X+r.W)) < .5:
		return PortEast
	case abs32(pt.Y-r.Y) < .5:
		return PortNorth
	}
	return PortSouth
}
