package main

import (
	"fmt"

	"github.com/qizhanchan/qui"
)

// The inspector panel is a right-side strip that exposes per-selection
// properties as clickable rows. It mirrors what G6's
// `behavior:edge-edit` plugin would surface in a side panel.
//
// Rendered in screen coords (not the canvas transform). When an edge
// is selected the panel shows: routing radio-buttons, dashed toggle,
// start/end arrow cyclers. When a node is selected it shows position +
// shape (read-only for now). With nothing selected it shows a help
// block.

type inspectorButton struct {
	rect    qui.Rect
	onClick func()
	active  bool
	label   string
}

// inspectorLayout returns the buttons currently visible on the
// inspector — used by both drawInspector (for rendering) and
// inspectorHandleClick (for hit-testing). Single source of truth so
// the two stay in sync.
func (e *editor) inspectorLayout() []inspectorButton {
	pr := e.inspectorRect()
	if e.selEdgeID < 0 && e.selNodeID < 0 {
		return nil
	}
	if e.selEdgeID >= 0 {
		ed := e.d.edgeByID(e.selEdgeID)
		if ed == nil {
			return nil
		}
		return e.layoutEdgeInspector(pr, ed)
	}
	return e.layoutNodeInspector(pr)
}

func (e *editor) layoutEdgeInspector(pr qui.Rect, ed *edge) []inspectorButton {
	const rowH float32 = 26
	const rowGap float32 = 4
	const groupGap float32 = 18
	const margin float32 = 14

	var btns []inspectorButton
	y := pr.Y + 70

	// Group: routing.
	routings := []edgeRouting{routingLine, routingQuadratic, routingCubic, routingOrth, routingPolyline}
	y += 22 // group title gap
	for _, r := range routings {
		row := qui.Rect{X: pr.X + margin, Y: y, W: pr.W - 2*margin, H: rowH}
		rr := r
		btns = append(btns, inspectorButton{
			rect:    row,
			label:   routingName(r),
			active:  ed.routing == r,
			onClick: func() { ed.routing = rr; e.defaultRouting = rr },
		})
		y += rowH + rowGap
	}
	y += groupGap

	// Group: dashed.
	dashedRow := qui.Rect{X: pr.X + margin, Y: y, W: pr.W - 2*margin, H: rowH}
	btns = append(btns, inspectorButton{
		rect:    dashedRow,
		label:   "Dashed",
		active:  ed.dashed,
		onClick: func() { ed.dashed = !ed.dashed; e.defaultDashed = ed.dashed },
	})
	y += rowH + groupGap

	// Group: end arrow.
	y += 22
	arrows := []arrowStyle{arrowNone, arrowTriangle, arrowVee, arrowDiamond, arrowCircle, arrowRect}
	// End arrow as a single row that cycles when clicked, with all 6
	// thumbnails on a grid below. Pragmatic for the example.
	endLabel := qui.Rect{X: pr.X + margin, Y: y, W: pr.W - 2*margin, H: rowH}
	btns = append(btns, inspectorButton{
		rect:    endLabel,
		label:   fmt.Sprintf("End arrow: %s", arrowName(ed.endArrow)),
		active:  false,
		onClick: func() { ed.endArrow = cycleArrow(ed.endArrow); e.defaultEndArrow = ed.endArrow },
	})
	y += rowH + rowGap
	// 6 thumbnails on a row.
	thumbW := (pr.W - 2*margin - 5*4) / 6
	for i, a := range arrows {
		x := pr.X + margin + float32(i)*(thumbW+4)
		row := qui.Rect{X: x, Y: y, W: thumbW, H: rowH}
		aa := a
		btns = append(btns, inspectorButton{
			rect:    row,
			label:   "•" + arrowGlyph(a),
			active:  ed.endArrow == a,
			onClick: func() { ed.endArrow = aa; e.defaultEndArrow = aa },
		})
	}
	y += rowH + groupGap

	// Group: start arrow.
	startLabel := qui.Rect{X: pr.X + margin, Y: y, W: pr.W - 2*margin, H: rowH}
	btns = append(btns, inspectorButton{
		rect:    startLabel,
		label:   fmt.Sprintf("Start arrow: %s", arrowName(ed.startArrow)),
		active:  false,
		onClick: func() { ed.startArrow = cycleArrow(ed.startArrow); e.defaultStartArrow = ed.startArrow },
	})
	y += rowH + rowGap
	for i, a := range arrows {
		x := pr.X + margin + float32(i)*(thumbW+4)
		row := qui.Rect{X: x, Y: y, W: thumbW, H: rowH}
		aa := a
		btns = append(btns, inspectorButton{
			rect:    row,
			label:   "•" + arrowGlyph(a),
			active:  ed.startArrow == a,
			onClick: func() { ed.startArrow = aa; e.defaultStartArrow = aa },
		})
	}
	return btns
}

func (e *editor) layoutNodeInspector(pr qui.Rect) []inspectorButton {
	// Node panel is read-only for now — return no buttons; drawInspector
	// will show plain text instead.
	return nil
}

func (e *editor) inspectorHandleClick(p qui.Point) bool {
	btns := e.inspectorLayout()
	for _, b := range btns {
		if b.rect.Contains(p) {
			if b.onClick != nil {
				b.onClick()
			}
			return true
		}
	}
	return false
}

func arrowGlyph(a arrowStyle) string {
	switch a {
	case arrowNone:
		return ""
	case arrowTriangle:
		return "▶"
	case arrowVee:
		return ">"
	case arrowDiamond:
		return "◆"
	case arrowCircle:
		return "●"
	case arrowRect:
		return "■"
	}
	return ""
}

// ---- Drawing ----

var (
	inspectorBG     = qui.Color{R: 0.10, G: 0.11, B: 0.13, A: 1}
	inspectorCard   = qui.Color{R: 0.14, G: 0.16, B: 0.20, A: 1}
	inspectorActive = qui.Color{R: 0.20, G: 0.36, B: 0.54, A: 1}
	inspectorHover  = qui.Color{R: 0.18, G: 0.22, B: 0.28, A: 1}
)

func (e *editor) drawInspector(canvas qui.Canvas) {
	pr := e.inspectorRect()
	canvas.FillRect(pr, inspectorBG)
	canvas.DrawLine(qui.Point{X: pr.X + 0.5, Y: pr.Y},
		qui.Point{X: pr.X + 0.5, Y: pr.Y + pr.H},
		qui.Color{R: 0.06, G: 0.07, B: 0.09, A: 1}, 1)

	titleRect := qui.Rect{X: pr.X + 14, Y: pr.Y + 18, W: pr.W - 28, H: 24}
	switch {
	case e.selEdgeID >= 0:
		canvas.DrawText("Edge", titleRect, textCol, qui.Font{Size: 15, Bold: true})
		ed := e.d.edgeByID(e.selEdgeID)
		if ed != nil {
			sub := fmt.Sprintf("id #%d   %s → %s", ed.id, portName(ed.fromDir), portName(ed.toDir))
			canvas.DrawText(sub,
				qui.Rect{X: pr.X + 14, Y: pr.Y + 42, W: pr.W - 28, H: 16},
				subtextCol, qui.Font{Size: 11})
		}
		e.drawInspectorEdge(canvas, pr, ed)
	case e.selNodeID >= 0:
		canvas.DrawText("Node", titleRect, textCol, qui.Font{Size: 15, Bold: true})
		n := e.d.nodeByID(e.selNodeID)
		if n != nil {
			e.drawInspectorNode(canvas, pr, n)
		}
	default:
		canvas.DrawText("Inspector", titleRect, textCol, qui.Font{Size: 15, Bold: true})
		e.drawInspectorHelp(canvas, pr)
	}
}

func (e *editor) drawInspectorEdge(canvas qui.Canvas, pr qui.Rect, ed *edge) {
	// Section headings + buttons. inspectorLayout returns the buttons
	// in order; we render section headings inline by tracking the y
	// offsets it uses.
	const rowH float32 = 26
	const margin float32 = 14
	y := pr.Y + 70

	// Heading: Routing.
	canvas.DrawText("Routing", qui.Rect{X: pr.X + margin, Y: y, W: pr.W - 2*margin, H: 16},
		subtextCol, qui.Font{Size: 11, Bold: true})
	y += 22

	btns := e.inspectorLayout()
	hover := e.lastMouse
	for _, b := range btns {
		fill := inspectorCard
		if b.active {
			fill = inspectorActive
		} else if b.rect.Contains(hover) {
			fill = inspectorHover
		}
		canvas.DrawShape(qui.ShapeRRect{Rect: b.rect, Radius: 5},
			qui.Paint{Color: fill, AntiAlias: true})
		canvas.DrawText(b.label,
			qui.Rect{X: b.rect.X + 10, Y: b.rect.Y + 6, W: b.rect.W - 16, H: 16},
			textCol, qui.Font{Size: 12})
	}

	// Sub-headings (interleaved between groups). Reuse y math.
	y += float32(5)*(rowH+4) + 18                                     // skip routing rows
	canvas.DrawText("", qui.Rect{X: pr.X + margin, Y: y, W: 1, H: 1}, // marker only
		subtextCol, qui.Font{Size: 1})
	// (Heading text per group is omitted — labels already encode the
	// section, e.g. "Dashed", "End arrow: Triangle".)
	_ = ed
}

func (e *editor) drawInspectorNode(canvas qui.Canvas, pr qui.Rect, n *node) {
	y := pr.Y + 70
	const margin float32 = 14
	rows := []string{
		fmt.Sprintf("shape:  %s", shapeName(n.shape)),
		fmt.Sprintf("label:  %s", n.label),
		fmt.Sprintf("x:      %.0f", n.pos.X),
		fmt.Sprintf("y:      %.0f", n.pos.Y),
		fmt.Sprintf("w:      %.0f", n.size.W),
		fmt.Sprintf("h:      %.0f", n.size.H),
	}
	for _, r := range rows {
		canvas.DrawText(r,
			qui.Rect{X: pr.X + margin, Y: y, W: pr.W - 2*margin, H: 18},
			textCol, qui.Font{Size: 12})
		y += 22
	}
}

func (e *editor) drawInspectorHelp(canvas qui.Canvas, pr qui.Rect) {
	const margin float32 = 14
	y := pr.Y + 60
	tips := []string{
		"Select an edge to restyle it:",
		"  • Routing  Line / Quadratic /",
		"               Cubic / Orth / Polyline",
		"  • Dashed   toggle",
		"  • Start / End arrows",
		"",
		"Keyboard:",
		"  1-5  edge routing",
		"  D    dashed toggle",
		"  A    cycle end arrow",
		"  S    cycle start arrow",
		"  ⌫    delete selection",
		"",
		"Canvas:",
		"  Space+drag pan",
		"  Scroll wheel zoom",
	}
	for _, t := range tips {
		canvas.DrawText(t,
			qui.Rect{X: pr.X + margin, Y: y, W: pr.W - 2*margin, H: 16},
			subtextCol, qui.Font{Size: 12})
		y += 18
	}
}

func portName(p portDir) string {
	switch p {
	case portN:
		return "N"
	case portE:
		return "E"
	case portS:
		return "S"
	case portW:
		return "W"
	}
	return ""
}
