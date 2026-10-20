package diagram

// Side selection for orthogonal edges. draw.io's OrthConnector picks each
// terminal's side from the two boxes alone, so it happily runs an edge
// through a third box or leaves a side that another edge enters by. Here
// every side pair is routed with the same connector and scored against the
// rest of the page; draw.io's own pick is kept unless another pair avoids
// a box or a port conflict, so uncluttered edges keep its look.

const (
	orthBendCost       float32 = 30   // per corner
	orthObstacleCost   float32 = 5000 // per segment through a box
	orthOpposeCost     float32 = 200  // side already used the other way (in vs out)
	orthShareCost      float32 = 25   // side already used the same way
	orthNotDrawioCost  float32 = 60   // leaving draw.io's pick needs a real reason
	orthObstacleMargin float32 = 4    // clearance kept around other boxes
)

var orthSides = [4]int{orthNorth, orthEast, orthSouth, orthWest}

// portClaim records that an edge already attached to a cell uses one side.
type portClaim struct {
	cell     string
	dir      int
	incoming bool
}

// orthogonalPoly routes an orthogonal-family edge. from/to are only used
// for unattached ends; attached ends are re-derived from their cell so the
// planner can choose the side.
func (p *Page) orthogonalPoly(edge *Cell, fromCell, toCell *Cell, from, to Point) []Point {
	src, dst := orthEnds(edge, fromCell, toCell, from, to)
	if fromCell != nil && toCell != nil {
		// Parallel edges fan out along their sides instead of across the chord.
		off := p.parallelOffset(edge)
		src.slide, dst.slide = off, off
	}
	poly, _ := p.planOrthogonal(edge, src, dst, fromCell, toCell, p.earlierClaims(edge, fromCell, toCell))
	return poly
}

func orthEnds(edge *Cell, fromCell, toCell *Cell, from, to Point) (orthEnd, orthEnd) {
	end := func(c *Cell, port Port, pt Point) orthEnd {
		if c == nil {
			return orthEnd{rect: Rect{X: pt.X, Y: pt.Y}, loose: true}
		}
		return orthEnd{rect: c.Bounds(), port: port}
	}
	return end(fromCell, edge.Source.Port, from), end(toCell, edge.Target.Port, to)
}

// planOrthogonal returns the cheapest route over every allowed side pair
// and the sides it uses.
func (p *Page) planOrthogonal(edge *Cell, src, dst orthEnd, fromCell, toCell *Cell, claims []portClaim) ([]Point, [2]int) {
	pref := orthDirections(src, dst)
	obstacles := p.routeObstacles(edge, fromCell, toCell)
	candidates := func(e orthEnd, preferred int) []int {
		if !e.loose && !isAutoPort(e.port) {
			return []int{portToOrthDir(e.port)}
		}
		out := []int{preferred}
		for _, d := range orthSides {
			if d != preferred {
				out = append(out, d)
			}
		}
		return out
	}
	var best []Point
	var bestDirs [2]int
	bestCost := float32(-1)
	for _, sd := range candidates(src, pref[0]) {
		for _, td := range candidates(dst, pref[1]) {
			dirs := [2]int{sd, td}
			poly := orthAssemble(src, dst, dirs)
			cost := orthRouteCost(poly, src, dst, obstacles)
			for _, c := range claims {
				if fromCell != nil && c.cell == fromCell.ID && c.dir == sd {
					cost += claimCost(c.incoming, false)
				}
				if toCell != nil && c.cell == toCell.ID && c.dir == td {
					cost += claimCost(c.incoming, true)
				}
			}
			if dirs != pref {
				cost += orthNotDrawioCost
			}
			if bestCost < 0 || cost < bestCost {
				best, bestDirs, bestCost = poly, dirs, cost
			}
		}
	}
	return best, bestDirs
}

func claimCost(claimIncoming, incoming bool) float32 {
	if claimIncoming != incoming {
		return orthOpposeCost
	}
	return orthShareCost
}

func orthRouteCost(poly []Point, src, dst orthEnd, obstacles []Rect) float32 {
	var cost float32
	for i := 1; i < len(poly); i++ {
		a, b := poly[i-1], poly[i]
		cost += abs32(b.X-a.X) + abs32(b.Y-a.Y)
		for _, r := range obstacles {
			if segmentHitsRect(a, b, r) {
				cost += orthObstacleCost
			}
		}
		for _, e := range [2]orthEnd{src, dst} {
			if !e.loose && segmentHitsRect(a, b, e.rect.Inflate(-1)) {
				cost += orthObstacleCost
			}
		}
	}
	if len(poly) > 2 {
		cost += float32(len(poly)-2) * orthBendCost
	}
	return cost
}

// segmentHitsRect reports whether an axis-aligned segment enters r's interior.
func segmentHitsRect(a, b Point, r Rect) bool {
	if r.Empty() {
		return false
	}
	return max32(a.X, b.X) > r.X && min32(a.X, b.X) < r.X+r.W &&
		max32(a.Y, b.Y) > r.Y && min32(a.Y, b.Y) < r.Y+r.H
}

// routeObstacles is every visible box the edge should go around: all
// vertices except its own terminals and any container holding them.
func (p *Page) routeObstacles(edge *Cell, fromCell, toCell *Cell) []Rect {
	var out []Rect
	for _, c := range p.Cells {
		if c == nil || c == fromCell || c == toCell || c == edge {
			continue
		}
		if c.Type != CellVertex && c.Type != CellGroup {
			continue
		}
		r := c.Bounds()
		if r.Empty() || !p.CellVisible(c) {
			continue
		}
		if (fromCell != nil && rectContains(r, fromCell.Bounds())) || (toCell != nil && rectContains(r, toCell.Bounds())) {
			continue
		}
		if (fromCell != nil && rectContains(fromCell.Bounds(), r)) || (toCell != nil && rectContains(toCell.Bounds(), r)) {
			continue
		}
		out = append(out, r.Inflate(orthObstacleMargin))
	}
	return out
}

func rectContains(outer, inner Rect) bool {
	return inner.X >= outer.X && inner.Y >= outer.Y &&
		inner.X+inner.W <= outer.X+outer.W && inner.Y+inner.H <= outer.Y+outer.H
}

// earlierClaims lists the sides of edge's terminals already taken by edges
// that come before it in the page. Earlier edges win, which keeps the
// result deterministic and avoids two edges each dodging the other. Their
// sides are planned with obstacles only, so this never recurses further.
func (p *Page) earlierClaims(edge *Cell, fromCell, toCell *Cell) []portClaim {
	if fromCell == nil && toCell == nil {
		return nil
	}
	attached := func(id string) bool {
		return id != "" && ((fromCell != nil && id == fromCell.ID) || (toCell != nil && id == toCell.ID))
	}
	var claims []portClaim
	for _, c := range p.Cells {
		// Match by ID too: callers route copies of page edges.
		if c == edge || (edge.ID != "" && c != nil && c.ID == edge.ID) {
			break
		}
		if c == nil || c.Type != CellEdge || !p.CellVisible(c) {
			continue
		}
		if !attached(c.Source.Cell) && !attached(c.Target.Cell) {
			continue
		}
		if c.Source.Cell != "" && c.Source.Cell == c.Target.Cell {
			continue
		}
		sc, tc := p.Cell(c.Source.Cell), p.Cell(c.Target.Cell)
		dirs, ok := p.edgeSides(c, sc, tc)
		if !ok {
			continue
		}
		if sc != nil && attached(sc.ID) && dirs[0] != 0 {
			claims = append(claims, portClaim{cell: sc.ID, dir: dirs[0]})
		}
		if tc != nil && attached(tc.ID) && dirs[1] != 0 {
			claims = append(claims, portClaim{cell: tc.ID, dir: dirs[1], incoming: true})
		}
	}
	return claims
}

// edgeSides reports which sides another edge uses at its terminals; 0
// means unknown (e.g. a straight edge with an automatic port).
func (p *Page) edgeSides(c *Cell, sc, tc *Cell) ([2]int, bool) {
	var dirs [2]int
	if !isAutoPort(c.Source.Port) {
		dirs[0] = portToOrthDir(c.Source.Port)
	}
	if !isAutoPort(c.Target.Port) {
		dirs[1] = portToOrthDir(c.Target.Port)
	}
	if dirs[0] != 0 && dirs[1] != 0 {
		return dirs, true
	}
	switch ResolvedStyle(c.Style, DefaultTheme().DefaultEdge).Route {
	case RouteOrthogonal, RouteRoundedOrthogonal, RoutePolyline:
	default:
		return dirs, dirs != [2]int{}
	}
	if len(c.Geometry.Points) > 0 {
		return dirs, dirs != [2]int{}
	}
	from, _, _ := p.endpoint(c, true, Point{}, nil)
	to, _, _ := p.endpoint(c, false, Point{}, nil)
	src, dst := orthEnds(c, sc, tc, from, to)
	_, planned := p.planOrthogonal(c, src, dst, sc, tc, nil)
	if dirs[0] == 0 {
		dirs[0] = planned[0]
	}
	if dirs[1] == 0 {
		dirs[1] = planned[1]
	}
	return dirs, true
}

// orthLabelClear is how far a default label keeps from a corner.
const orthLabelClear float32 = 24

// orthLabelPoint places a default (midpoint) label on an orthogonal route.
// The plain midpoint often lands on a short jog, where the label hides both
// corners; instead the label goes on the run holding the midpoint when it
// is long enough, otherwise on the longest run, clamped clear of corners.
func orthLabelPoint(skel []Point) Point {
	var total float32
	for i := 1; i < len(skel); i++ {
		total += dist32(skel[i-1], skel[i])
	}
	place := func(i int, at float32) Point {
		a, b := skel[i-1], skel[i]
		l := dist32(a, b)
		at = clamp32(at, min32(orthLabelClear, l/2), max32(l-orthLabelClear, l/2))
		u := at / l
		return Point{X: a.X + (b.X-a.X)*u, Y: a.Y + (b.Y-a.Y)*u}
	}
	half := total / 2
	var walked float32
	for i := 1; i < len(skel); i++ {
		l := dist32(skel[i-1], skel[i])
		if half <= walked+l {
			if l >= 2.5*orthLabelClear {
				return place(i, half-walked)
			}
			break
		}
		walked += l
	}
	// Longest run; ties go to the one nearest the middle of the route.
	best, bestLen, bestOff := 1, float32(-1), float32(0)
	walked = 0
	for i := 1; i < len(skel); i++ {
		l := dist32(skel[i-1], skel[i])
		off := abs32(walked + l/2 - half)
		if l > bestLen+.5 || (abs32(l-bestLen) <= .5 && off < bestOff) {
			best, bestLen, bestOff = i, l, off
		}
		walked += l
	}
	return place(best, bestLen/2)
}
