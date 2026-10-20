package diagram

// Orthogonal connector ported from draw.io's mxEdgeStyle.OrthConnector
// (mxgraph/src/view/mxEdgeStyle.js). The algorithm picks the sides each
// terminal leaves from by relative placement, rotates the layout into a
// canonical quadrant, then replays a fixed route pattern for that side pair.
// Each pattern step moves the current waypoint toward a terminal's buffered
// limit or a centre line, so routes leave and enter perpendicular to the
// sides and wrap around boxes instead of cutting through them.

// Direction masks, as mxConstants.DIRECTION_MASK_*.
const (
	orthWest  = 1
	orthNorth = 2
	orthSouth = 4
	orthEast  = 8
	orthAll   = orthWest | orthNorth | orthSouth | orthEast
)

// Route-pattern step bits, as mxEdgeStyle.*_MASK.
const (
	orthSideMask   = 480
	orthCenterMask = 512
	orthSourceMask = 1024
	orthTargetMask = 2048
)

// orthJetty is the default straight run out of and into a terminal: draw.io's
// orthBuffer of 10, doubled by jettySize=auto.
const orthJetty float32 = 20

var orthDirVectors = [4][2]float32{{-1, 0}, {0, -1}, {1, 0}, {0, 1}}

var orthRoutePatterns = [4][4][]int{
	{{513, 2308, 2081, 2562}, {513, 1090, 514, 2184, 2114, 2561},
		{513, 1090, 514, 2564, 2184, 2562},
		{513, 2308, 2561, 1090, 514, 2568, 2308}},
	{{514, 1057, 513, 2308, 2081, 2562}, {514, 2184, 2114, 2561},
		{514, 2184, 2562, 1057, 513, 2564, 2184},
		{514, 1057, 513, 2568, 2308, 2561}},
	{{1090, 514, 1057, 513, 2308, 2081, 2562}, {2114, 2561},
		{1090, 2562, 1057, 513, 2564, 2184},
		{1090, 514, 1057, 513, 2308, 2561, 2568}},
	{{2081, 2562}, {1057, 513, 1090, 514, 2184, 2114, 2561},
		{1057, 513, 1090, 514, 2184, 2562, 2564},
		{1057, 2561, 1090, 514, 2568, 2308}},
}

// orthEnd is one terminal of an orthogonal connection.
type orthEnd struct {
	rect  Rect
	port  Port    // fixed side; "" or PortAuto lets the router choose
	slide float32 // offset along the chosen side (parallel-edge fan-out)
	loose bool    // unattached end: rect is a 1x1 box at the point
}

func portToOrthDir(p Port) int {
	switch p {
	case PortWest:
		return orthWest
	case PortNorth:
		return orthNorth
	case PortSouth:
		return orthSouth
	case PortEast:
		return orthEast
	}
	return orthAll
}

func orthDirToPort(d int) Port {
	switch d {
	case orthWest:
		return PortWest
	case orthNorth:
		return PortNorth
	case orthSouth:
		return PortSouth
	}
	return PortEast
}

func reverseOrthDir(d int) int {
	return (d&orthWest)<<3 | (d&orthNorth)<<1 | (d&orthSouth)>>1 | (d&orthEast)>>3
}

// terminal returns where the end attaches once its side is known.
func (e orthEnd) terminal(dir int) Point {
	if e.loose {
		return Point{X: e.rect.X, Y: e.rect.Y}
	}
	pt := portPoint(e.rect, orthDirToPort(dir))
	if dir == orthWest || dir == orthEast {
		pt.Y = clamp32(pt.Y+e.slide, e.rect.Y, e.rect.Y+e.rect.H)
	} else {
		pt.X = clamp32(pt.X+e.slide, e.rect.X, e.rect.X+e.rect.W)
	}
	return pt
}

// orthogonalConnect returns the full polyline from the source terminal to
// the target terminal.
func orthogonalConnect(src, dst orthEnd) []Point {
	return orthAssemble(src, dst, orthDirections(src, dst))
}

// orthAssemble routes between the two ends leaving and entering by dirs.
func orthAssemble(src, dst orthEnd, dirs [2]int) []Point {
	from, to := src.terminal(dirs[0]), dst.terminal(dirs[1])
	wps := orthWaypoints(src, dst, dirs, from, to)
	pts := make([]Point, 0, len(wps)+4)
	pts = append(pts, from)
	pts = append(pts, wps...)
	// The pattern lands on the target's centre line in almost every case;
	// when it does not (e.g. the boxes overlap), add the elbow that keeps
	// the final run perpendicular to the target side.
	last := pts[len(pts)-1]
	if abs32(last.X-to.X) > .01 && abs32(last.Y-to.Y) > .01 {
		if dirs[1] == orthWest || dirs[1] == orthEast {
			pts = append(pts, Point{X: last.X, Y: to.Y})
		} else {
			pts = append(pts, Point{X: to.X, Y: last.Y})
		}
	}
	pts = append(pts, to)
	return dedupColinear(dedupPoints(pts))
}

// orthDirections picks the side each terminal uses (OrthConnector's
// "source and target direction determination").
func orthDirections(src, dst orthEnd) [2]int {
	geo := [2]Rect{src.rect, dst.rect}
	portConstraint := [2]int{orthAll, orthAll}
	if !src.loose && !isAutoPort(src.port) {
		portConstraint[0] = portToOrthDir(src.port)
	}
	if !dst.loose && !isAutoPort(dst.port) {
		portConstraint[1] = portToOrthDir(dst.port)
	}
	sourceTopDist := geo[0].Y - (geo[1].Y + geo[1].H)
	sourceLeftDist := geo[0].X - (geo[1].X + geo[1].W)
	sourceBottomDist := geo[1].Y - (geo[0].Y + geo[0].H)
	sourceRightDist := geo[1].X - (geo[0].X + geo[0].W)

	var horPref, vertPref [2]int
	horPref[0], vertPref[0] = orthEast, orthSouth
	if sourceLeftDist >= sourceRightDist {
		horPref[0] = orthWest
	}
	if sourceTopDist >= sourceBottomDist {
		vertPref[0] = orthNorth
	}
	horPref[1] = reverseOrthDir(horPref[0])
	vertPref[1] = reverseOrthDir(vertPref[0])
	preferredHorizDist := max32(sourceLeftDist, sourceRightDist)
	preferredVertDist := max32(sourceTopDist, sourceBottomDist)

	var prefOrdering [2][2]int
	for i := 0; i < 2; i++ {
		if horPref[i]&portConstraint[i] == 0 {
			horPref[i] = reverseOrthDir(horPref[i])
		}
		if vertPref[i]&portConstraint[i] == 0 {
			vertPref[i] = reverseOrthDir(vertPref[i])
		}
		prefOrdering[i] = [2]int{vertPref[i], horPref[i]}
	}
	set := false
	if preferredVertDist > 0 && preferredHorizDist > 0 {
		// Two-segment (L-shaped) connection possible.
		if horPref[0]&portConstraint[0] > 0 && vertPref[1]&portConstraint[1] > 0 {
			prefOrdering = [2][2]int{{horPref[0], vertPref[0]}, {vertPref[1], horPref[1]}}
			set = true
		} else if vertPref[0]&portConstraint[0] > 0 && horPref[1]&portConstraint[1] > 0 {
			prefOrdering = [2][2]int{{vertPref[0], horPref[0]}, {horPref[1], vertPref[1]}}
			set = true
		}
	}
	if preferredVertDist > 0 && !set {
		prefOrdering = [2][2]int{{vertPref[0], horPref[0]}, {vertPref[1], horPref[1]}}
		set = true
	}
	if preferredHorizDist > 0 && !set {
		prefOrdering = [2][2]int{{horPref[0], vertPref[0]}, {horPref[1], vertPref[1]}}
	}

	var dir [2]int
	for i := 0; i < 2; i++ {
		if prefOrdering[i][0]&portConstraint[i] == 0 {
			prefOrdering[i][0] = prefOrdering[i][1]
		}
		pref := prefOrdering[i][0] & portConstraint[i]
		pref |= (prefOrdering[i][1] & portConstraint[i]) << 8
		pref |= (prefOrdering[1-i][i] & portConstraint[i]) << 16
		pref |= (prefOrdering[1-i][1-i] & portConstraint[i]) << 24
		if pref&0xF == 0 {
			pref <<= 8
		}
		if pref&0xF00 == 0 {
			pref = pref&0xF | pref>>8
		}
		if pref&0xF0000 == 0 {
			pref = pref&0xFFFF | (pref&0xF000000)>>8
		}
		dir[i] = pref & 0xF
		switch portConstraint[i] {
		case orthWest, orthNorth, orthEast, orthSouth:
			dir[i] = portConstraint[i]
		}
		if dir[i] == 0 {
			dir[i] = orthEast
		}
	}
	return dir
}

// orthWaypoints replays the route pattern for the chosen sides and returns
// the bend points strictly between the two terminals.
func orthWaypoints(src, dst orthEnd, dir [2]int, from, to Point) []Point {
	geo := [2]Rect{src.rect, dst.rect}
	if src.loose {
		geo[0] = Rect{X: from.X, Y: from.Y, W: 1, H: 1}
	}
	if dst.loose {
		geo[1] = Rect{X: to.X, Y: to.Y, W: 1, H: 1}
	}
	sourceBuffer, targetBuffer := orthJetty, orthJetty
	totalBuffer := sourceBuffer + targetBuffer
	if dist32(from, to) < totalBuffer {
		return nil
	}
	buffer := [2]float32{sourceBuffer, targetBuffer}
	var limits [2][9]float32
	for i := 0; i < 2; i++ {
		limits[i][1] = geo[i].X - buffer[i]
		limits[i][2] = geo[i].Y - buffer[i]
		limits[i][4] = geo[i].X + geo[i].W + buffer[i]
		limits[i][8] = geo[i].Y + geo[i].H + buffer[i]
	}

	// Quadrant of the target relative to the source:
	// 0 | 1
	// -----
	// 3 | 2
	sc, tc := geo[0].Center(), geo[1].Center()
	dx, dy := sc.X-tc.X, sc.Y-tc.Y
	quad := 0
	if dx < 0 {
		if dy < 0 {
			quad = 2
		} else {
			quad = 1
		}
	} else if dy <= 0 {
		quad = 3
		if dx == 0 {
			quad = 2
		}
	}

	// Where on each box the terminal sits, as a fraction of its size.
	terms := [2]Point{from, to}
	var constraint [2][2]float32
	for i := 0; i < 2; i++ {
		constraint[i][0] = (terms[i].X - geo[i].X) / geo[i].W
		constraint[i][1] = (terms[i].Y - geo[i].Y) / geo[i].H
	}

	var seps [5]float32
	seps[1] = max32(geo[0].X-(geo[1].X+geo[1].W)-totalBuffer, 0)
	seps[2] = max32(geo[0].Y-(geo[1].Y+geo[1].H)-totalBuffer, 0)
	seps[3] = max32(geo[1].X-(geo[0].X+geo[0].W)-totalBuffer, 0)
	seps[4] = max32(geo[1].Y-(geo[0].Y+geo[0].H)-totalBuffer, 0)

	index := func(d int) int {
		if d == orthEast {
			return 3
		}
		return d
	}
	sourceIndex, targetIndex := index(dir[0])-quad, index(dir[1])-quad
	if sourceIndex < 1 {
		sourceIndex += 4
	}
	if targetIndex < 1 {
		targetIndex += 4
	}
	pattern := orthRoutePatterns[sourceIndex-1][targetIndex-1]

	wp := make([][2]float32, 1, len(pattern)+1)
	wp[0] = [2]float32{geo[0].X, geo[0].Y}
	switch dir[0] {
	case orthWest:
		wp[0][0] -= sourceBuffer
		wp[0][1] += constraint[0][1] * geo[0].H
	case orthSouth:
		wp[0][0] += constraint[0][0] * geo[0].W
		wp[0][1] += geo[0].H + sourceBuffer
	case orthEast:
		wp[0][0] += geo[0].W + sourceBuffer
		wp[0][1] += constraint[0][1] * geo[0].H
	case orthNorth:
		wp[0][0] += constraint[0][0] * geo[0].W
		wp[0][1] -= sourceBuffer
	}

	cur := 0
	horizontal := func(d int) int {
		if d&(orthEast|orthWest) > 0 {
			return 0
		}
		return 1
	}
	lastOrientation := horizontal(dir[0])
	initialOrientation := lastOrientation
	for _, step := range pattern {
		directionIndex := index(step&0xF) + quad
		if directionIndex > 4 {
			directionIndex -= 4
		}
		direction := orthDirVectors[directionIndex-1]
		orientation := 1
		if directionIndex%2 > 0 {
			orientation = 0
		}
		if orientation != lastOrientation {
			cur++
			if cur >= len(wp) {
				wp = append(wp, wp[cur-1])
			} else {
				wp[cur] = wp[cur-1]
			}
		}
		tar := step&orthTargetMask > 0
		sou := step&orthSourceMask > 0
		side := ((step & orthSideMask) >> 5) << quad
		if side > 0xF {
			side >>= 4
		}
		center := step&orthCenterMask > 0
		if (sou || tar) && side < 9 {
			st := 1
			if sou {
				st = 0
			}
			var limit float32
			switch {
			case center && orientation == 0:
				limit = geo[st].X + constraint[st][0]*geo[st].W
			case center:
				limit = geo[st].Y + constraint[st][1]*geo[st].H
			default:
				limit = limits[st][side]
			}
			if delta := (limit - wp[cur][orientation]) * direction[orientation]; delta > 0 {
				wp[cur][orientation] += direction[orientation] * delta
			}
		} else if center {
			wp[cur][0] += direction[0] * abs32(seps[directionIndex]/2)
			wp[cur][1] += direction[1] * abs32(seps[directionIndex]/2)
		}
		if cur > 0 && wp[cur][orientation] == wp[cur-1][orientation] {
			cur--
		} else {
			lastOrientation = orientation
		}
	}

	out := make([]Point, 0, cur+1)
	for i := 0; i <= cur; i++ {
		if i == cur {
			// Same terminal orientation needs an even number of bends,
			// different needs odd; drop a surplus final point.
			sameOrient := 1
			if horizontal(dir[1]) == initialOrientation {
				sameOrient = 0
			}
			if sameOrient != (cur+1)%2 {
				break
			}
		}
		out = append(out, Point{X: wp[i][0], Y: wp[i][1]})
	}
	return out
}

func dedupPoints(pts []Point) []Point {
	out := pts[:0:0]
	for _, p := range pts {
		if len(out) > 0 && dist32(out[len(out)-1], p) < .01 {
			continue
		}
		out = append(out, p)
	}
	return out
}
