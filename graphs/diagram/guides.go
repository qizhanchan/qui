package diagram

import (
	"math"

	"github.com/qizhanchan/qui"
)

// Alignment guides, after draw.io's mxGuide: while shapes are moved or
// resized, their edges and centre lines snap to the matching lines of the
// other shapes on the page, and the lines they snapped to are drawn. An
// axis that finds no guide falls back to the grid. Holding Alt disables
// guides for that gesture.

// Guide is one alignment line. A vertical guide is the line X=Pos running
// from Y=From to Y=To; a horizontal guide is Y=Pos from X=From to X=To.
// Coordinates are in page (world) space.
type Guide struct {
	Vertical bool
	Pos      float32
	From     float32
	To       float32
}

// guideTolerance is the snapping distance in screen pixels; it stays
// constant on screen at any zoom.
const guideTolerance float32 = 6

// guideOverhang extends a drawn guide past the shapes it joins.
const guideOverhang float32 = 10

// Rect features along one axis.
const (
	featLow    = 1 << iota // left / top
	featCenter             // centre line
	featHigh               // right / bottom
	featAll    = featLow | featCenter | featHigh
)

func axisFeatures(lo, size float32, mask int) []float32 {
	var out []float32
	if mask&featLow != 0 {
		out = append(out, lo)
	}
	if mask&featCenter != 0 {
		out = append(out, lo+size/2)
	}
	if mask&featHigh != 0 {
		out = append(out, lo+size)
	}
	return out
}

// snapAxis returns the smallest shift (within tol) that puts one of the
// moving features on one of the targets' features.
func snapAxis(moving []float32, targets []Rect, vertical bool, tol float32) (float32, bool) {
	best, found := tol, false
	var shift float32
	for _, t := range targets {
		lo, size := t.Y, t.H
		if vertical {
			lo, size = t.X, t.W
		}
		for _, tf := range axisFeatures(lo, size, featAll) {
			for _, mf := range moving {
				if d := tf - mf; abs32(d) <= best && (!found || abs32(d) < abs32(shift)) {
					best, shift, found = abs32(d), d, true
				}
			}
		}
	}
	return shift, found
}

// guidesFor lists the lines where r's selected features coincide with a
// target's features, each spanning r and every target on that line.
func guidesFor(r Rect, targets []Rect, xMask, yMask int) []Guide {
	var out []Guide
	add := func(vertical bool, pos float32, span [2]float32, t Rect) {
		lo, hi := t.Y, t.Y+t.H
		if !vertical {
			lo, hi = t.X, t.X+t.W
		}
		for i := range out {
			if out[i].Vertical == vertical && abs32(out[i].Pos-pos) < .01 {
				out[i].From = min32(out[i].From, lo-guideOverhang)
				out[i].To = max32(out[i].To, hi+guideOverhang)
				return
			}
		}
		out = append(out, Guide{Vertical: vertical, Pos: pos,
			From: min32(span[0], lo) - guideOverhang, To: max32(span[1], hi) + guideOverhang})
	}
	for _, t := range targets {
		for _, mf := range axisFeatures(r.X, r.W, xMask) {
			for _, tf := range axisFeatures(t.X, t.W, featAll) {
				if abs32(mf-tf) < .01 {
					add(true, mf, [2]float32{r.Y, r.Y + r.H}, t)
				}
			}
		}
		for _, mf := range axisFeatures(r.Y, r.H, yMask) {
			for _, tf := range axisFeatures(t.Y, t.H, featAll) {
				if abs32(mf-tf) < .01 {
					add(false, mf, [2]float32{r.X, r.X + r.W}, t)
				}
			}
		}
	}
	return out
}

// guideTargets are the visible shapes on the active page a gesture can
// align to: everything except the moving cells, their descendants, and
// containers that hold them.
func (d *Diagram) guideTargets(moving map[string]bool, movingRect Rect) []Rect {
	page := d.doc.Active()
	if page == nil {
		return nil
	}
	var out []Rect
	for _, c := range page.Cells {
		if c == nil || moving[c.ID] || (c.Type != CellVertex && c.Type != CellGroup) || !page.CellVisible(c) {
			continue
		}
		r := c.Bounds()
		if r.Empty() || rectContains(r, movingRect) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// SetGuides toggles alignment guides for move and resize gestures.
func (d *Diagram) SetGuides(on bool) {
	d.guides = on
	if !on {
		d.interact.guides = nil
	}
}

// GuidesEnabled reports whether alignment guides are on.
func (d *Diagram) GuidesEnabled() bool { return d.guides }

// ActiveGuides returns the guide lines shown by the gesture in progress, in
// page coordinates; empty when nothing is being moved or nothing aligns.
func (d *Diagram) ActiveGuides() []Guide {
	return append([]Guide(nil), d.interact.guides...)
}

func (d *Diagram) gridRound(v float32) float32 {
	if !d.snap || d.snapSz <= 0 {
		return v
	}
	return float32(math.Round(float64(v/d.snapSz))) * d.snapSz
}

func (d *Diagram) guideTol() float32 {
	s := d.vp.Scale
	if s <= 0 {
		s = 1
	}
	return guideTolerance / s
}

// snapMove turns a raw drag delta into the applied one: per axis, a guide
// snap of the dragged selection's bounding box when one is in reach,
// otherwise the grid. It records the guides to draw.
func (d *Diagram) snapMove(dx, dy float32, noGuides bool) (float32, float32) {
	gdx, gdy := d.gridRound(dx), d.gridRound(dy)
	d.interact.guides = nil
	if !d.guides || noGuides {
		return gdx, gdy
	}
	var box Rect
	first := true
	moving := map[string]bool{}
	for id, origin := range d.interact.dragOrigins {
		moving[id] = true
		c := d.doc.Cell(id)
		if c == nil || c.Type == CellEdge {
			continue
		}
		r := Rect{X: origin.X, Y: origin.Y, W: c.Geometry.W, H: c.Geometry.H}
		if first {
			box, first = r, false
		} else {
			box = box.Union(r)
		}
	}
	if first {
		return gdx, gdy
	}
	raw := Rect{X: box.X + dx, Y: box.Y + dy, W: box.W, H: box.H}
	targets := d.guideTargets(moving, raw)
	tol := d.guideTol()
	xMask, yMask := 0, 0
	if s, ok := snapAxis(axisFeatures(raw.X, raw.W, featAll), targets, true, tol); ok {
		gdx, xMask = dx+s, featAll
	}
	if s, ok := snapAxis(axisFeatures(raw.Y, raw.H, featAll), targets, false, tol); ok {
		gdy, yMask = dy+s, featAll
	}
	if xMask|yMask != 0 {
		final := Rect{X: box.X + gdx, Y: box.Y + gdy, W: box.W, H: box.H}
		d.interact.guides = guidesFor(final, targets, xMask, yMask)
	}
	return gdx, gdy
}

// resizeEdges reports which edges a resize handle moves: -1 low, +1 high,
// 0 none, for x and y. Handles run clockwise from the top-left corner.
func resizeEdges(handle int) (int, int) {
	switch handle {
	case 0:
		return -1, -1
	case 1:
		return 0, -1
	case 2:
		return 1, -1
	case 3:
		return 1, 0
	case 4:
		return 1, 1
	case 5:
		return 0, 1
	case 6:
		return -1, 1
	case 7:
		return -1, 0
	}
	return 0, 0
}

// snapResizeEdge places one moving edge: onto a guide when one is in reach,
// otherwise onto the grid. It returns the edge position and whether a guide
// was used.
func (d *Diagram) snapResizeEdge(edge float32, vertical bool, targets []Rect, useGuides bool) (float32, bool) {
	if useGuides {
		if s, ok := snapAxis([]float32{edge}, targets, vertical, d.guideTol()); ok {
			return edge + s, true
		}
	}
	return d.gridRound(edge), false
}

func (d *Diagram) drawGuides(canvas qui.Canvas) {
	if len(d.interact.guides) == 0 {
		return
	}
	col, ok := ParseColor(d.theme.Guide)
	if !ok {
		col, _ = ParseColor(DefaultTheme().Guide)
	}
	// Drawn in screen space on pixel centres so the 1px line stays crisp
	// at every zoom.
	b := d.Bounds()
	depth := canvas.Save()
	canvas.ClipRect(b)
	crisp := func(v float32) float32 { return float32(math.Floor(float64(v))) + .5 }
	sx := func(x float32) float32 { return b.X + d.vp.Pan.X + x*d.vp.Scale }
	sy := func(y float32) float32 { return b.Y + d.vp.Pan.Y + y*d.vp.Scale }
	for _, g := range d.interact.guides {
		a := qui.Point{X: sx(g.From), Y: crisp(sy(g.Pos))}
		z := qui.Point{X: sx(g.To), Y: a.Y}
		if g.Vertical {
			a = qui.Point{X: crisp(sx(g.Pos)), Y: sy(g.From)}
			z = qui.Point{X: a.X, Y: sy(g.To)}
		}
		canvas.DrawLine(a, z, col, 1)
	}
	canvas.RestoreTo(depth)
}
