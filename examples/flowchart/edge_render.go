package main

import (
	"math"

	"github.com/qizhanchan/qui"
)

// edgeRouting mirrors AntV G6's edge family —
// the five built-in shapes plus a loop variant for self-edges.
//
//	routingLine       — straight segment, G6 "line"
//	routingQuadratic  — single quadratic Bézier, G6 "quadratic"
//	routingCubic      — cubic Bézier with port-direction tangents,
//	                    G6 "cubic-vertical" / "cubic-horizontal"
//	                    (we pick orientation from the source port dir)
//	routingOrth       — manhattan / right-angle routing, G6 polyline
//	                    + orth router
//	routingPolyline   — segmented line with rounded corners, G6 "polyline"
//	                    (we auto-place a midpoint waypoint; user waypoints
//	                    are reserved for a follow-up)
type edgeRouting int

const (
	routingLine edgeRouting = iota
	routingQuadratic
	routingCubic
	routingOrth
	routingPolyline
)

func routingName(r edgeRouting) string {
	switch r {
	case routingLine:
		return "Line"
	case routingQuadratic:
		return "Quadratic"
	case routingCubic:
		return "Cubic"
	case routingOrth:
		return "Orth"
	case routingPolyline:
		return "Polyline"
	}
	return ""
}

// arrowStyle mirrors G6's arrow `type` enum.
type arrowStyle int

const (
	arrowNone arrowStyle = iota
	arrowTriangle
	arrowVee
	arrowDiamond
	arrowCircle
	arrowRect
)

func arrowName(a arrowStyle) string {
	switch a {
	case arrowNone:
		return "None"
	case arrowTriangle:
		return "Triangle"
	case arrowVee:
		return "Vee"
	case arrowDiamond:
		return "Diamond"
	case arrowCircle:
		return "Circle"
	case arrowRect:
		return "Rect"
	}
	return ""
}

func cycleArrow(a arrowStyle) arrowStyle {
	return arrowStyle((int(a) + 1) % 6)
}

// edgeEndpointsWithOffset returns the from/to endpoints for an edge,
// shifted perpendicular to the chord by parallelOffset so siblings
// fan out. Used by both routing and arrow tangent computation.
func edgeEndpointsWithOffset(e *edge, d *diagram) (qui.Point, qui.Point) {
	from, to := e.resolveEndpoints(d)
	if e.parallelOffset == 0 {
		return from, to
	}
	dx := to.X - from.X
	dy := to.Y - from.Y
	dl := float32sqrt(dx*dx + dy*dy)
	if dl < 0.5 {
		return from, to
	}
	nx := -dy / dl
	ny := dx / dl
	off := e.parallelOffset
	from = qui.Point{X: from.X + nx*off, Y: from.Y + ny*off}
	to = qui.Point{X: to.X + nx*off, Y: to.Y + ny*off}
	return from, to
}

// routeEdge returns the (path, tangentAtEnd, tangentAtStart) for an
// edge under its chosen routing. The tangent vectors are unit-length
// (or zero for degenerate cases) and are what the arrow renderer uses
// to orient the head triangle.
func routeEdge(e *edge, d *diagram) (path *qui.Path, startTangent, endTangent qui.Point) {
	from, to := edgeEndpointsWithOffset(e, d)
	// Detect self-loop: same source + target node. G6 calls this
	// path "loop"; we draw a small cardinal arc above the node.
	if e.fromID >= 0 && e.fromID == e.toID {
		n := d.nodeByID(e.fromID)
		if n != nil {
			return routeLoop(n)
		}
	}
	switch e.routing {
	case routingLine:
		return routeLine(from, to)
	case routingCubic:
		return routeCubic(from, to, e.fromDir, e.toDir)
	case routingOrth:
		return routeOrth(from, to, e.fromDir, e.toDir, edgeBBoxes(e, d))
	case routingPolyline:
		return routePolyline(from, to, e.fromDir, e.toDir, edgeBBoxes(e, d))
	default: // routingQuadratic
		return routeQuadratic(from, to, e.fromDir, e.toDir)
	}
}

// routeLine — straight segment.
func routeLine(from, to qui.Point) (*qui.Path, qui.Point, qui.Point) {
	p := qui.NewPath().MoveTo(from.X, from.Y).LineTo(to.X, to.Y)
	tx := to.X - from.X
	ty := to.Y - from.Y
	t := normalize(tx, ty)
	rev := qui.Point{X: -t.X, Y: -t.Y}
	return p, rev, t
}

// routeQuadratic — single Bézier with control point perpendicular to
// the chord midpoint (drawio "freeform" / G6 quadratic).
func routeQuadratic(from, to qui.Point, fromDir, toDir portDir) (*qui.Path, qui.Point, qui.Point) {
	mx := (from.X + to.X) / 2
	my := (from.Y + to.Y) / 2
	dx := to.X - from.X
	dy := to.Y - from.Y
	dl := float32sqrt(dx*dx + dy*dy)
	if dl > 1 {
		nx := -dy / dl
		ny := dx / dl
		bow := dl * 0.20
		mx += nx * bow
		my += ny * bow
	}
	p := qui.NewPath().MoveTo(from.X, from.Y).QuadTo(mx, my, to.X, to.Y)
	// Tangent at t=1 of a quadratic: 2 * (P2 - P1). Unit-normalize.
	endT := normalize(to.X-mx, to.Y-my)
	startT := normalize(from.X-mx, from.Y-my)
	return p, startT, endT
}

// routeCubic — cubic Bézier with handles aligned to each port's
// outward tangent. Mirrors G6's cubic-vertical / cubic-horizontal:
// the handle length is half the axis-aligned distance to the other
// endpoint.
func routeCubic(from, to qui.Point, fromDir, toDir portDir) (*qui.Path, qui.Point, qui.Point) {
	st := portTangent(fromDir)
	et := portTangent(toDir)
	dx := to.X - from.X
	dy := to.Y - from.Y
	// Handle length: half the distance projected on each tangent's
	// dominant axis. Bounded below by 40 so short chord still bends.
	handle := func(t qui.Point) float32 {
		mag := absF(t.X*dx+t.Y*dy) * 0.5
		if mag < 40 {
			mag = 40
		}
		return mag
	}
	hl1 := handle(st)
	hl2 := handle(et)
	c1 := qui.Point{X: from.X + st.X*hl1, Y: from.Y + st.Y*hl1}
	c2 := qui.Point{X: to.X + et.X*hl2, Y: to.Y + et.Y*hl2}
	p := qui.NewPath().MoveTo(from.X, from.Y).CubicTo(c1.X, c1.Y, c2.X, c2.Y, to.X, to.Y)
	endT := normalize(to.X-c2.X, to.Y-c2.Y)
	startT := normalize(from.X-c1.X, from.Y-c1.Y)
	return p, startT, endT
}

// routeOrth — manhattan / right-angle routing leaving each endpoint
// perpendicular to its port direction. G6's "polyline + orth router"
// equivalent. The path obeys two contracts:
//
//  1. It leaves source perpendicular to fromDir, and arrives at
//     target perpendicular to toDir.
//  2. With non-zero source/target bboxes provided, it routes AROUND
//     those boxes (with `nodePadding` slack) — so dragging the target
//     so its bbox would intersect the natural corner causes an
//     automatic detour instead of slicing through the node.
//
// edgeBoxes carries the source + target node bounds; pass zero
// rects if you don't want obstacle avoidance.
func routeOrth(from, to qui.Point, fromDir, toDir portDir, boxes edgeBoxes) (*qui.Path, qui.Point, qui.Point) {
	verts := orthVertices(from, to, fromDir, toDir, boxes)
	p := qui.NewPath().MoveTo(verts[0].X, verts[0].Y)
	for i := 1; i < len(verts); i++ {
		p.LineTo(verts[i].X, verts[i].Y)
	}
	startT := normalize(verts[0].X-verts[1].X, verts[0].Y-verts[1].Y)
	endT := normalize(verts[len(verts)-1].X-verts[len(verts)-2].X,
		verts[len(verts)-1].Y-verts[len(verts)-2].Y)
	return p, startT, endT
}

// edgeBoxes carries the inflated bboxes the orth router uses for
// obstacle avoidance. Zero-value (empty rects) disables avoidance —
// useful for dragging-edge previews where the target isn't real yet.
type edgeBoxes struct {
	from, to qui.Rect
}

// edgeBBoxes resolves source + target bounds for an edge from the
// diagram. Either side falls back to a zero rect when the endpoint is
// floating (dragging-edge preview).
func edgeBBoxes(e *edge, d *diagram) edgeBoxes {
	var b edgeBoxes
	if n := d.nodeByID(e.fromID); n != nil {
		b.from = n.bounds()
	}
	if n := d.nodeByID(e.toID); n != nil {
		b.to = n.bounds()
	}
	return b
}

const nodePadding float32 = 10 // matches G6's default OrthRouterOptions.padding

// orthVertices ports G6's orth router. The algorithm:
//
//  1. Inflate both bboxes by nodePadding.
//  2. From the source side, pick a single L-corner aligned with the
//     source's port direction (horizontal port → corner has source's
//     inflated Y; vertical port → corner has source's inflated X).
//  3. If that corner lies INSIDE the target's inflated bbox, retry
//     from the target side (symmetric).
//  4. If both sides conflict, fall back to a midpoint detour through
//     a Z-shape that runs along the combined boundary.
//
// freeJoin picks the L-corner outside a bbox; nodeToPoint picks the
// corner aligned with the port side; nodeToNode chains the two
// directions. Port positions sit on the original (non-inflated) bbox
// boundary, and we extend OUTWARD by padding to get the "inflated
// boundary point" the router treats as the routing endpoint.
func orthVertices(from, to qui.Point, fromDir, toDir portDir, boxes edgeBoxes) []qui.Point {
	// Inflated bboxes — the surface the routing avoids.
	fromBBox := inflateRect(boxes.from, nodePadding)
	toBBox := inflateRect(boxes.to, nodePadding)

	// Port-anchored inflated points — the boundary point on the
	// inflated bbox that aligns with the port direction.
	fromInflated := inflatedPortPoint(from, fromDir, nodePadding)
	toInflated := inflatedPortPoint(to, toDir, nodePadding)

	// G6's nodeToNode body (orth.ts:264). Detect bbox overlap →
	// insideNode; otherwise try nodeToPoint forward, then reverse
	// if the corner sits in the target.
	verts := []qui.Point{from, fromInflated}
	if !rectIsZero(fromBBox) && !rectIsZero(toBBox) && rectsIntersect(fromBBox, toBBox) {
		verts = append(verts, insideNodeRoute(fromInflated, toInflated, fromBBox, toBBox, fromDir, toDir)...)
	} else {
		verts = append(verts, nodeToNodeRoute(fromInflated, toInflated, fromBBox, toBBox, fromDir, toDir)...)
	}
	verts = append(verts, toInflated, to)
	return dedupColinear(verts)
}

// inflatedPortPoint returns the port position shifted OUTWARD by
// padding — the boundary point on the inflated bbox aligned with the
// port direction. Mirrors G6's getNearestBoundaryPoint for the case
// where the input already IS a port (= boundary point of the
// non-inflated bbox).
func inflatedPortPoint(port qui.Point, dir portDir, padding float32) qui.Point {
	t := portTangent(dir)
	return qui.Point{X: port.X + t.X*padding, Y: port.Y + t.Y*padding}
}

// nodeToNodeRoute returns the intermediate vertices between two
// inflated boundary points. Equivalent to G6's nodeToNode + freeJoin
// (orth.ts:264, orth.ts:339) with a stricter "outside BOTH bboxes"
// check than G6's "outside the relevant bbox" — G6's algorithm can
// still produce a corner inside the *other* bbox when the natural
// port-aligned L lands there, which is exactly the case the user
// reported (showErr.portS center column intersects End's x-range
// after End is dragged underneath).
//
// We try both L-corners (one per diagonal) and pick the first that
// lies outside both inflated bboxes. If both fail, fall back to a
// midpoint detour through the gap between the boxes.
func nodeToNodeRoute(from, to qui.Point, fromBBox, toBBox qui.Rect, fromDir, toDir portDir) []qui.Point {
	// L1 has source's X and target's Y → first move VERTICAL, then HORIZONTAL.
	// L2 has target's X and source's Y → first move HORIZONTAL, then VERTICAL.
	L1 := qui.Point{X: from.X, Y: to.Y}
	L2 := qui.Point{X: to.X, Y: from.Y}

	// Prefer the corner aligned with the source's port direction —
	// that way the port→inflated leg AND the inflated→corner leg are
	// colinear, and dedupColinear merges them into a single segment.
	// Result: a clean two-segment L from the source's perspective.
	var preferred, fallback qui.Point
	if isVerticalDir(fromDir) {
		preferred, fallback = L1, L2 // source vertical → first leg vertical
	} else {
		preferred, fallback = L2, L1 // source horizontal → first leg horizontal
	}

	cornerOK := func(c qui.Point) bool {
		if pointInRect(c, fromBBox) || pointInRect(c, toBBox) {
			return false
		}
		// Also reject if the segments would slice through a bbox
		// interior. Axis-aligned shortcut.
		if axisSegmentCrossesRect(from, c, fromBBox) ||
			axisSegmentCrossesRect(from, c, toBBox) ||
			axisSegmentCrossesRect(c, to, fromBBox) ||
			axisSegmentCrossesRect(c, to, toBBox) {
			return false
		}
		return true
	}
	if cornerOK(preferred) {
		return []qui.Point{preferred}
	}
	if cornerOK(fallback) {
		return []qui.Point{fallback}
	}
	return midpointDetour(from, to, fromBBox, toBBox, fromDir, toDir)
}

// axisSegmentCrossesRect reports whether the axis-aligned segment a→b
// crosses r's interior. The segment must be horizontal (a.Y == b.Y)
// or vertical (a.X == b.X) — non-axis-aligned segments aren't used
// by the orth router and we treat them conservatively as crossing.
func axisSegmentCrossesRect(a, b qui.Point, r qui.Rect) bool {
	if rectIsZero(r) {
		return false
	}
	if a.Y == b.Y {
		// Horizontal segment.
		if a.Y <= r.Y || a.Y >= r.Y+r.H {
			return false
		}
		lo, hi := a.X, b.X
		if hi < lo {
			lo, hi = hi, lo
		}
		// Strict crossing: segment's X-range must extend INTO the
		// interior (not just touch the boundary).
		return hi > r.X && lo < r.X+r.W
	}
	if a.X == b.X {
		if a.X <= r.X || a.X >= r.X+r.W {
			return false
		}
		lo, hi := a.Y, b.Y
		if hi < lo {
			lo, hi = hi, lo
		}
		return hi > r.Y && lo < r.Y+r.H
	}
	return false
}

// midpointDetour: both single-corner Ls fail. Run two nodeToPoint
// calls through a midpoint that lies on the combined-bbox boundary.
// Lighter-weight than G6's full insideNode (we already know the boxes
// don't intersect when this is reached for the non-overlap path).
func midpointDetour(from, to qui.Point, fromBBox, toBBox qui.Rect, fromDir, toDir portDir) []qui.Point {
	// Midpoint outside both bboxes. Pick the axis with more slack:
	// the axis where the two bboxes are FURTHEST apart has clear room.
	gapX := axisGap(fromBBox.X, fromBBox.X+fromBBox.W, toBBox.X, toBBox.X+toBBox.W)
	gapY := axisGap(fromBBox.Y, fromBBox.Y+fromBBox.H, toBBox.Y, toBBox.Y+toBBox.H)
	var midPoint qui.Point
	if gapX >= gapY {
		// Use the x-axis gap. midpoint.X between the two bboxes.
		midX := (max32(fromBBox.X+fromBBox.W, toBBox.X+toBBox.W)*0 + // unused
			((maxF(fromBBox.X, toBBox.X) + minF(fromBBox.X+fromBBox.W, toBBox.X+toBBox.W)) / 2))
		// Simpler: midpoint between near edges.
		midX = (minF(fromBBox.X+fromBBox.W, toBBox.X+toBBox.W) +
			maxF(fromBBox.X, toBBox.X)) / 2
		midPoint = qui.Point{X: midX, Y: (from.Y + to.Y) / 2}
	} else {
		midY := (minF(fromBBox.Y+fromBBox.H, toBBox.Y+toBBox.H) +
			maxF(fromBBox.Y, toBBox.Y)) / 2
		midPoint = qui.Point{X: (from.X + to.X) / 2, Y: midY}
	}
	// Use port-aligned corner from source perspective, and port-
	// aligned corner from target perspective, joined through midPoint.
	var p1, p2 qui.Point
	if isVerticalDir(fromDir) {
		p1 = qui.Point{X: from.X, Y: midPoint.Y}
	} else {
		p1 = qui.Point{X: midPoint.X, Y: from.Y}
	}
	if isVerticalDir(toDir) {
		p2 = qui.Point{X: to.X, Y: midPoint.Y}
	} else {
		p2 = qui.Point{X: midPoint.X, Y: to.Y}
	}
	return []qui.Point{p1, p2}
}

// axisGap returns the signed distance between two intervals along an
// axis. Positive = they don't overlap; negative = overlap depth.
func axisGap(a0, a1, b0, b1 float32) float32 {
	if a1 < b0 {
		return b0 - a1
	}
	if b1 < a0 {
		return a0 - b1
	}
	return -(minF(a1, b1) - maxF(a0, b0))
}

// insideNodeRoute handles the case where source and target inflated
// bboxes overlap. Simplified version of G6's insideNode: routes
// through the corner of the combined bbox furthest from the source
// in the source's port direction.
func insideNodeRoute(from, to qui.Point, fromBBox, toBBox qui.Rect, fromDir, toDir portDir) []qui.Point {
	// Combined bbox.
	combined := qui.Rect{
		X: minF(fromBBox.X, toBBox.X),
		Y: minF(fromBBox.Y, toBBox.Y),
		W: maxF(fromBBox.X+fromBBox.W, toBBox.X+toBBox.W) - minF(fromBBox.X, toBBox.X),
		H: maxF(fromBBox.Y+fromBBox.H, toBBox.Y+toBBox.H) - minF(fromBBox.Y, toBBox.Y),
	}
	// Route around the combined bbox via two corners: one near the
	// source's port direction, the other on the way to target.
	st := portTangent(fromDir)
	tt := portTangent(toDir)
	var p1, p2 qui.Point
	if absF(st.X) > 0.5 {
		// Source exits horizontally — first corner offsets x.
		p1 = qui.Point{X: from.X + st.X*combined.W, Y: from.Y}
	} else {
		p1 = qui.Point{X: from.X, Y: from.Y + st.Y*combined.H}
	}
	if absF(tt.X) > 0.5 {
		p2 = qui.Point{X: to.X + tt.X*combined.W, Y: to.Y}
	} else {
		p2 = qui.Point{X: to.X, Y: to.Y + tt.Y*combined.H}
	}
	return []qui.Point{p1, p2}
}

// rectIsZero reports whether the rect is the zero-value sentinel used
// for "no bbox available" (e.g., dragging-edge preview).
func rectIsZero(r qui.Rect) bool { return r.W == 0 && r.H == 0 }

func rectsIntersect(a, b qui.Rect) bool {
	return !(a.X+a.W <= b.X || b.X+b.W <= a.X ||
		a.Y+a.H <= b.Y || b.Y+b.H <= a.Y)
}

// pointInRect tests the STRICT interior — a point exactly on the rect
// edge is NOT inside. Critical for orth routing: corners often land
// on inflated bbox edges by construction (e.g. corner.Y = source's
// inflated bottom = source.bottom + padding), and treating those
// boundary-aligned corners as "inside" sends every well-formed L into
// the midpoint detour.
func pointInRect(p qui.Point, r qui.Rect) bool {
	if rectIsZero(r) {
		return false
	}
	return p.X > r.X && p.X < r.X+r.W && p.Y > r.Y && p.Y < r.Y+r.H
}

func inflateRect(r qui.Rect, pad float32) qui.Rect {
	if rectIsZero(r) {
		return r
	}
	return qui.Rect{X: r.X - pad, Y: r.Y - pad, W: r.W + 2*pad, H: r.H + 2*pad}
}

func minF(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
func maxF(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
func max32(a, b float32) float32 { return maxF(a, b) }

func isVerticalDir(p portDir) bool { return p == portN || p == portS }

// dedupColinear removes adjacent points that lie on the same straight
// segment AND in monotonic order (no direction reversal). A "colinear
// but reversing" triplet — say (0,0), (10,0), (5,0) — is a backtrack
// and MUST keep its middle point: dropping it produces a clean line
// that hides a routing bug. The original v1 dedupe dropped every
// colinear middle indiscriminately and hid exactly that bug on
// stretched S→E paths.
func dedupColinear(pts []qui.Point) []qui.Point {
	if len(pts) <= 2 {
		return pts
	}
	out := pts[:1]
	for i := 1; i < len(pts); i++ {
		if i == len(pts)-1 {
			out = append(out, pts[i])
			continue
		}
		a, b, c := pts[i-1], pts[i], pts[i+1]
		cross := (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X)
		if absF(cross) >= 0.01 {
			out = append(out, b)
			continue
		}
		// Colinear. Check monotonicity: (b-a) and (c-b) must point in
		// the same direction along the shared axis. If they reverse,
		// b is a turning point.
		abx := b.X - a.X
		aby := b.Y - a.Y
		bcx := c.X - b.X
		bcy := c.Y - b.Y
		if abx*bcx+aby*bcy < 0 {
			out = append(out, b)
		}
		// else: monotonic colinear, drop b.
	}
	return out
}

// routePolyline — same manhattan vertices as routeOrth, but joints
// rounded via short quadratic Béziers at each corner. G6's "polyline"
// with non-zero `radius`.
func routePolyline(from, to qui.Point, fromDir, toDir portDir, boxes edgeBoxes) (*qui.Path, qui.Point, qui.Point) {
	verts := orthVertices(from, to, fromDir, toDir, boxes)
	const radius float32 = 12
	p := qui.NewPath().MoveTo(verts[0].X, verts[0].Y)
	for i := 1; i < len(verts)-1; i++ {
		a := verts[i-1]
		b := verts[i]
		c := verts[i+1]
		// Limit the corner radius by the shorter of the two adjacent
		// segments so the rounded portion never overshoots.
		la := dist(a, b)
		lc := dist(b, c)
		r := radius
		if la/2 < r {
			r = la / 2
		}
		if lc/2 < r {
			r = lc / 2
		}
		if r <= 0.5 {
			p.LineTo(b.X, b.Y)
			continue
		}
		// Pre-corner approach point + post-corner exit point.
		ab := normalize(b.X-a.X, b.Y-a.Y)
		bc := normalize(c.X-b.X, c.Y-b.Y)
		in := qui.Point{X: b.X - ab.X*r, Y: b.Y - ab.Y*r}
		out := qui.Point{X: b.X + bc.X*r, Y: b.Y + bc.Y*r}
		p.LineTo(in.X, in.Y)
		p.QuadTo(b.X, b.Y, out.X, out.Y)
	}
	last := verts[len(verts)-1]
	p.LineTo(last.X, last.Y)
	startT := normalize(verts[0].X-verts[1].X, verts[0].Y-verts[1].Y)
	endT := normalize(last.X-verts[len(verts)-2].X, last.Y-verts[len(verts)-2].Y)
	return p, startT, endT
}

// routeLoop draws a self-loop above the node — a small cubic Bézier
// arc that starts at portN and returns to portN. Mirrors G6's loop
// edge.
func routeLoop(n *node) (*qui.Path, qui.Point, qui.Point) {
	b := n.bounds()
	cx := b.X + b.W/2
	const loopR float32 = 36
	leftX := cx - 18
	rightX := cx + 18
	topY := b.Y
	arcY := b.Y - loopR
	p := qui.NewPath().
		MoveTo(leftX, topY).
		CubicTo(leftX, arcY, rightX, arcY, rightX, topY)
	startT := qui.Point{X: 0, Y: -1}
	endT := qui.Point{X: 0, Y: 1}
	return p, startT, endT
}

// routeEdgeSampled returns N points along the edge's curve by
// re-routing through the same math the renderer uses. The Path type
// keeps its command list unexported so we can't introspect the path
// we already built — pragmatic alternative is to re-sample here.
// Cheap (one O(N) sweep per hit-test) and stays in lockstep with
// drawEdge's geometry.
func routeEdgeSampled(e *edge, d *diagram, samples int) []qui.Point {
	from, to := edgeEndpointsWithOffset(e, d)
	if e.fromID >= 0 && e.fromID == e.toID {
		n := d.nodeByID(e.fromID)
		if n != nil {
			b := n.bounds()
			cx := b.X + b.W/2
			leftX := cx - 18
			rightX := cx + 18
			topY := b.Y
			arcY := b.Y - 36
			return sampleCubic(qui.Point{X: leftX, Y: topY},
				qui.Point{X: leftX, Y: arcY},
				qui.Point{X: rightX, Y: arcY},
				qui.Point{X: rightX, Y: topY}, samples)
		}
	}
	switch e.routing {
	case routingLine:
		return []qui.Point{from, to}
	case routingCubic:
		st := portTangent(e.fromDir)
		et := portTangent(e.toDir)
		dx := to.X - from.X
		dy := to.Y - from.Y
		handle := func(t qui.Point) float32 {
			mag := absF(t.X*dx+t.Y*dy) * 0.5
			if mag < 40 {
				mag = 40
			}
			return mag
		}
		hl1 := handle(st)
		hl2 := handle(et)
		c1 := qui.Point{X: from.X + st.X*hl1, Y: from.Y + st.Y*hl1}
		c2 := qui.Point{X: to.X + et.X*hl2, Y: to.Y + et.Y*hl2}
		return sampleCubic(from, c1, c2, to, samples)
	case routingOrth:
		return orthVertices(from, to, e.fromDir, e.toDir, edgeBBoxes(e, d))
	case routingPolyline:
		return orthVertices(from, to, e.fromDir, e.toDir, edgeBBoxes(e, d))
	default: // quadratic
		mx := (from.X + to.X) / 2
		my := (from.Y + to.Y) / 2
		dx := to.X - from.X
		dy := to.Y - from.Y
		dl := float32sqrt(dx*dx + dy*dy)
		if dl > 1 {
			nx := -dy / dl
			ny := dx / dl
			bow := dl * 0.20
			mx += nx * bow
			my += ny * bow
		}
		return sampleQuadratic(from, qui.Point{X: mx, Y: my}, to, samples)
	}
}

func sampleQuadratic(p0, p1, p2 qui.Point, samples int) []qui.Point {
	if samples < 2 {
		samples = 2
	}
	pts := make([]qui.Point, 0, samples+1)
	for i := 0; i <= samples; i++ {
		t := float32(i) / float32(samples)
		u := 1 - t
		pts = append(pts, qui.Point{
			X: u*u*p0.X + 2*u*t*p1.X + t*t*p2.X,
			Y: u*u*p0.Y + 2*u*t*p1.Y + t*t*p2.Y,
		})
	}
	return pts
}

func sampleCubic(p0, p1, p2, p3 qui.Point, samples int) []qui.Point {
	if samples < 2 {
		samples = 2
	}
	pts := make([]qui.Point, 0, samples+1)
	for i := 0; i <= samples; i++ {
		t := float32(i) / float32(samples)
		u := 1 - t
		pts = append(pts, qui.Point{
			X: u*u*u*p0.X + 3*u*u*t*p1.X + 3*u*t*t*p2.X + t*t*t*p3.X,
			Y: u*u*u*p0.Y + 3*u*u*t*p1.Y + 3*u*t*t*p2.Y + t*t*t*p3.Y,
		})
	}
	return pts
}

// drawEdge paints the edge body — stroke + label only. Arrows are
// painted in a second pass via drawEdgeArrows, AFTER nodes, so the
// target's fill can never occlude the arrowhead. Splitting the two
// passes is the fix for the "drag End right, inbound arrow vanishes
// behind the ellipse fill" report: the arrow tip lives at the port
// position, which is on the silhouette; the body extends backward
// along the tangent, which under a cubic routing often grazes the
// node's interior — and the node paint runs after the edge body.
func drawEdge(canvas qui.Canvas, e *edge, d *diagram, selected bool) {
	drawEdgeStrokeOnly(canvas, e, d, selected)
	if e.label != "" {
		drawEdgeLabel(canvas, e.label, edgeLabelCenter(e, d), false)
	}
}

// drawEdgeStrokeOnly paints the edge's stroke (or dashed pattern) but
// not the label — used when the editor wants to render the label
// separately in a later pass (e.g. for the edge being edited, whose
// label is drawn on top of arrows with a caret).
func drawEdgeStrokeOnly(canvas qui.Canvas, e *edge, d *diagram, selected bool) {
	stroke := edgeStroke
	width := float32(1.8)
	if selected {
		stroke = selStroke
		width = 2.4
	}

	p, _, _ := routeEdge(e, d)

	if e.dashed {
		dashOn := 6 + 2*width
		dashOff := 4 + width
		samples := routeEdgeSampled(e, d, 80)
		drawDashedPolyline(canvas, samples, stroke, width, dashOn, dashOff)
	} else {
		canvas.DrawShape(qui.ShapePath{Path: p},
			qui.Paint{Color: stroke, Style: qui.PaintStroke, StrokeWidth: width,
				Cap: qui.CapRound, Join: qui.JoinRound, AntiAlias: true})
	}
}

// edgeLabelCenter returns the world-space midpoint of the edge's
// flattened polyline — where the label floats. Sampled fresh each
// frame so labels track the route exactly even when bend points
// change due to a node move.
func edgeLabelCenter(e *edge, d *diagram) qui.Point {
	samples := routeEdgeSampled(e, d, 32)
	if len(samples) == 0 {
		return qui.Point{}
	}
	return samples[len(samples)/2]
}

// edgeLabelRect returns the world-space bounding rect for an edge
// label, used by both the renderer and the hit-tester so the visible
// surface matches the clickable surface exactly.
//
// `text` may be the edge's saved label or an in-progress edit buffer
// — the rect adapts to the rendered string width with margins.
func edgeLabelRect(text string, center qui.Point) qui.Rect {
	display := text
	if display == "" {
		display = " " // give an empty label a small clickable footprint
	}
	tw, th := qui.TextMetrics(display, edgeLabelFont)
	const padX float32 = 6
	const padY float32 = 3
	return qui.Rect{
		X: center.X - tw/2 - padX,
		Y: center.Y - th/2 - padY,
		W: tw + 2*padX,
		H: th + 2*padY,
	}
}

var edgeLabelFont = qui.Font{Size: 11}

// drawEdgeLabel renders the floating label text on an edge. No
// border, just a subtle background patch for legibility — distinct
// from a node silhouette. `editing` thickens the background and
// shows a caret so the user has a clear "this is now a text field"
// affordance.
func drawEdgeLabel(canvas qui.Canvas, text string, center qui.Point, editing bool) {
	display := text
	if display == "" {
		display = " "
	}
	rect := edgeLabelRect(text, center)
	bg := withAlpha(bgFill, 0.92)
	if editing {
		bg = withAlpha(selStroke, 0.18)
	}
	canvas.DrawShape(qui.ShapeRRect{Rect: rect, Radius: 3},
		qui.Paint{Color: bg, AntiAlias: true})
	tw, th := qui.TextMetrics(display, edgeLabelFont)
	textRect := qui.Rect{X: center.X - tw/2, Y: center.Y - th/2, W: tw, H: th}
	color := subtextCol
	if editing {
		color = textCol
	}
	canvas.DrawText(display, textRect, color, edgeLabelFont)
	if editing {
		// Caret at end of string. Always-visible (no blink) for
		// the demo — keeps the renderer stateless. Width 1px,
		// height matches the text.
		caret := qui.Rect{
			X: textRect.X + tw + 1,
			Y: textRect.Y,
			W: 1,
			H: th,
		}
		canvas.FillRect(caret, textCol)
	}
}

// drawEdgeArrows paints the arrows for an edge — called from a pass
// that runs AFTER nodes have been drawn, so the target node's fill
// can't occlude the arrowhead.
func drawEdgeArrows(canvas qui.Canvas, e *edge, d *diagram, selected bool) {
	stroke := edgeStroke
	width := float32(1.8)
	if selected {
		stroke = selStroke
		width = 2.4
	}
	_, startT, endT := routeEdge(e, d)
	from, to := edgeEndpointsWithOffset(e, d)
	if e.endArrow != arrowNone {
		drawArrow(canvas, to, endT, e.endArrow, stroke, width)
	}
	if e.startArrow != arrowNone {
		drawArrow(canvas, from, startT, e.startArrow, stroke, width)
	}
}

// drawArrow paints an arrow head of `style` at `tip`, oriented so the
// "incoming" tangent equals `tangent` (the unit vector pointing IN to
// the tip — i.e. the direction the line is moving). The arrow's body
// extends BACKWARD from the tip along the negative tangent, so a tip
// placed on a node's silhouette lands the body OUTSIDE the node.
//
// We also nudge the tip 1.5 px FORWARD along the tangent so the apex
// of the head sits just past the silhouette instead of right on it —
// the on-silhouette case had the head merging into the target node's
// outline stroke, making it hard to read on long cubic edges.
func drawArrow(canvas qui.Canvas, tip, tangent qui.Point, style arrowStyle, color qui.Color, width float32) {
	// 11 + 0.7w gives a head ~12-14 px on typical edges — slightly
	// larger than v1's 10 so the head reads against long cubics
	// without overpowering short orth segments.
	size := 11 + 0.7*width
	// Tangent must be unit. If degenerate, bail.
	tl := float32sqrt(tangent.X*tangent.X + tangent.Y*tangent.Y)
	if tl < 1e-3 {
		return
	}
	tx := tangent.X / tl
	ty := tangent.Y / tl
	// Pull tip BACKWARD along the tangent by ~2 px so the apex sits
	// just outside the target node's silhouette. With the tip ON the
	// silhouette, the arrow head would merge into the node's outline
	// stroke and read poorly on long cubic edges (the user's drag-End
	// report). Body extends further back, also outside the node.
	const tipNudge float32 = 2
	tip = qui.Point{X: tip.X - tx*tipNudge, Y: tip.Y - ty*tipNudge}
	// Perpendicular.
	px := -ty
	py := tx
	switch style {
	case arrowTriangle:
		bx := tip.X - tx*size
		by := tip.Y - ty*size
		half := size * 0.5
		head := qui.NewPath().
			MoveTo(tip.X, tip.Y).
			LineTo(bx+px*half, by+py*half).
			LineTo(bx-px*half, by-py*half).
			Close()
		canvas.DrawShape(qui.ShapePath{Path: head},
			qui.Paint{Color: color, AntiAlias: true})
	case arrowVee:
		// Two strokes meeting at the tip.
		bx := tip.X - tx*size
		by := tip.Y - ty*size
		half := size * 0.6
		canvas.DrawLine(tip, qui.Point{X: bx + px*half, Y: by + py*half}, color, width)
		canvas.DrawLine(tip, qui.Point{X: bx - px*half, Y: by - py*half}, color, width)
	case arrowDiamond:
		half := size * 0.5
		front := qui.Point{X: tip.X, Y: tip.Y}
		mid := qui.Point{X: tip.X - tx*half, Y: tip.Y - ty*half}
		back := qui.Point{X: tip.X - tx*size, Y: tip.Y - ty*size}
		diamond := qui.NewPath().
			MoveTo(front.X, front.Y).
			LineTo(mid.X+px*half, mid.Y+py*half).
			LineTo(back.X, back.Y).
			LineTo(mid.X-px*half, mid.Y-py*half).
			Close()
		canvas.DrawShape(qui.ShapePath{Path: diamond},
			qui.Paint{Color: color, AntiAlias: true})
	case arrowCircle:
		r := size * 0.45
		cx := tip.X - tx*r
		cy := tip.Y - ty*r
		disk := qui.NewPath().AddCircle(cx, cy, r)
		canvas.DrawShape(qui.ShapePath{Path: disk},
			qui.Paint{Color: color, AntiAlias: true})
	case arrowRect:
		// A short rectangle aligned with the tangent.
		half := size * 0.4
		long := size
		c0 := qui.Point{X: tip.X - tx*long + px*half, Y: tip.Y - ty*long + py*half}
		c1 := qui.Point{X: tip.X + px*half, Y: tip.Y + py*half}
		c2 := qui.Point{X: tip.X - px*half, Y: tip.Y - py*half}
		c3 := qui.Point{X: tip.X - tx*long - px*half, Y: tip.Y - ty*long - py*half}
		rect := qui.NewPath().
			MoveTo(c0.X, c0.Y).
			LineTo(c1.X, c1.Y).
			LineTo(c2.X, c2.Y).
			LineTo(c3.X, c3.Y).
			Close()
		canvas.DrawShape(qui.ShapePath{Path: rect},
			qui.Paint{Color: color, AntiAlias: true})
	}
}

// drawDashedPolyline emits dashes at fixed arc-length intervals along
// the polyline. The implementation lives at the editor layer because
// the renderer doesn't (yet) understand stroke-dash on Paint.
//
// Algorithm: precompute cumulative arc length, then walk dashes at
// stride `period` from offset 0 to total. For each dash, emit ONE
// or MORE line segments depending on how many polyline segments it
// straddles. The outer loop advances by `period` (strictly positive,
// since dashOn + dashOff > 0 is checked up front), so the function
// can't hang on float drift the way a per-segment "consumed += step"
// walker can — the v1 implementation had exactly that bug, where
// `step` shrunk to a sub-ULP value at the corner of an orth-routed
// path and pegged a CPU core.
func drawDashedPolyline(canvas qui.Canvas, pts []qui.Point, color qui.Color, width, dashOn, dashOff float32) {
	if len(pts) < 2 || dashOn <= 0 || dashOff <= 0 {
		return
	}
	period := dashOn + dashOff
	// Cumulative arc-length offsets — offsets[i] = arc length from
	// pts[0] to pts[i]. Skip degenerate zero-length segments so the
	// per-segment normalization below can't divide by zero.
	offsets := make([]float32, 1, len(pts))
	keep := pts[:1]
	for i := 1; i < len(pts); i++ {
		d := dist(pts[i-1], pts[i])
		if d < 0.001 {
			continue
		}
		keep = append(keep, pts[i])
		offsets = append(offsets, offsets[len(offsets)-1]+d)
	}
	pts = keep
	if len(pts) < 2 {
		return
	}
	total := offsets[len(offsets)-1]
	if total < 0.5 {
		return
	}
	// Walk dashes. Outer loop advances by `period` — guaranteed
	// positive, so termination is structural.
	for start := float32(0); start < total; start += period {
		end := start + dashOn
		if end > total {
			end = total
		}
		emitDashSegments(canvas, pts, offsets, start, end, color, width)
	}
}

// emitDashSegments draws the portion of the polyline between two arc-
// length offsets, splitting at polyline vertices so a dash that
// straddles a corner renders as two short segments instead of a
// straight chord through the bend.
func emitDashSegments(canvas qui.Canvas, pts []qui.Point, offsets []float32, start, end float32, color qui.Color, width float32) {
	if end <= start {
		return
	}
	for i := 1; i < len(offsets); i++ {
		segStart := offsets[i-1]
		segEnd := offsets[i]
		if segEnd <= start {
			continue
		}
		if segStart >= end {
			break
		}
		s := start
		if segStart > s {
			s = segStart
		}
		e := end
		if segEnd < e {
			e = segEnd
		}
		segLen := segEnd - segStart
		ts := (s - segStart) / segLen
		te := (e - segStart) / segLen
		a := pts[i-1]
		b := pts[i]
		p0 := qui.Point{X: a.X + ts*(b.X-a.X), Y: a.Y + ts*(b.Y-a.Y)}
		p1 := qui.Point{X: a.X + te*(b.X-a.X), Y: a.Y + te*(b.Y-a.Y)}
		canvas.DrawLine(p0, p1, color, width)
	}
}

func normalize(x, y float32) qui.Point {
	l := float32sqrt(x*x + y*y)
	if l < 1e-6 {
		return qui.Point{}
	}
	return qui.Point{X: x / l, Y: y / l}
}

func float32sqrt(v float32) float32 {
	return float32(math.Sqrt(float64(v)))
}

func pow10(v float32) float32 {
	return float32(math.Pow(10, float64(v)))
}
