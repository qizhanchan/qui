// flowchart is a drawio-style 2D diagram editor that mirrors the core
// edge-routing surface of AntV G6 (/opt/cpp_work/G6). Files:
//
//   - main.go          — entry point, diagram model, editor state machine,
//     event dispatch.
//   - shapes.go        — node silhouettes (rect / rounded / ellipse /
//     diamond / parallelogram / hex / cylinder).
//   - edge_render.go   — edge routing (line / quadratic / cubic / orth /
//     polyline / loop), arrow heads, dashed strokes.
//   - inspector.go     — right-side panel exposing per-edge routing,
//     arrow style, dashed toggle.
//
// Exercises the Phase E rendering features (affine matrix in the canvas
// state stack, AA non-zero winding fill, stroke caps/joins, Path) end
// to end.
package main

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/agent"
)

const (
	doubleClickWindow = 350 * time.Millisecond
	doubleClickRadius = float32(6)
)

// ---- Node shapes ----

type nodeShape int

const (
	shapeRect nodeShape = iota
	shapeRoundedRect
	shapeEllipse
	shapeDiamond
	shapeParallelogram
	shapeHexagon
	shapeCylinder
)

func shapeName(s nodeShape) string {
	switch s {
	case shapeRect:
		return "Process"
	case shapeRoundedRect:
		return "Terminator"
	case shapeEllipse:
		return "Start / End"
	case shapeDiamond:
		return "Decision"
	case shapeParallelogram:
		return "Data"
	case shapeHexagon:
		return "Preparation"
	case shapeCylinder:
		return "Database"
	}
	return ""
}

// ---- Diagram model ----

type node struct {
	id    int
	shape nodeShape
	pos   qui.Point // top-left in world coords
	size  qui.Size
	label string
}

func (n *node) bounds() qui.Rect {
	return qui.Rect{X: n.pos.X, Y: n.pos.Y, W: n.size.W, H: n.size.H}
}

func (n *node) center() qui.Point {
	return qui.Point{X: n.pos.X + n.size.W/2, Y: n.pos.Y + n.size.H/2}
}

type portDir int

const (
	portN portDir = iota
	portE
	portS
	portW
)

// portPos returns the world-space position of a port on a node.
func (n *node) portPos(p portDir) qui.Point {
	b := n.bounds()
	switch p {
	case portN:
		return qui.Point{X: b.X + b.W/2, Y: b.Y}
	case portE:
		return qui.Point{X: b.X + b.W, Y: b.Y + b.H/2}
	case portS:
		return qui.Point{X: b.X + b.W/2, Y: b.Y + b.H}
	case portW:
		return qui.Point{X: b.X, Y: b.Y + b.H/2}
	}
	return n.center()
}

// portTangent returns a unit vector pointing OUTWARD from the port.
// Routing code uses this to bias control points so curves and orth
// segments leave / arrive perpendicular to the node side.
func portTangent(p portDir) qui.Point {
	switch p {
	case portN:
		return qui.Point{X: 0, Y: -1}
	case portE:
		return qui.Point{X: 1, Y: 0}
	case portS:
		return qui.Point{X: 0, Y: 1}
	case portW:
		return qui.Point{X: -1, Y: 0}
	}
	return qui.Point{}
}

// nearestPort picks the port whose position is closest to `target` in
// world coords.
func (n *node) nearestPort(target qui.Point) portDir {
	best := portN
	bd := dist(n.portPos(portN), target)
	for _, p := range []portDir{portE, portS, portW} {
		d := dist(n.portPos(p), target)
		if d < bd {
			bd = d
			best = p
		}
	}
	return best
}

type edge struct {
	id      int
	fromID  int // -1 = floating endpoint
	fromDir portDir
	toID    int // -1 = floating endpoint
	toDir   portDir
	// Floating endpoints (used when fromID/toID is -1 — typically during
	// drag-to-create).
	fromPos qui.Point
	toPos   qui.Point
	label   string

	// G6-style style props.
	routing    edgeRouting
	dashed     bool
	startArrow arrowStyle
	endArrow   arrowStyle

	// parallelOffset is auto-assigned by recomputeParallelOffsets so
	// edges sharing the same (from, to) pair fan out instead of stacking
	// on top of each other.
	parallelOffset float32
}

// resolveEndpoints returns the world-space (from, to) for an edge,
// looking up the connected nodes via the diagram. If an endpoint is
// floating it uses the stored position directly.
func (e *edge) resolveEndpoints(d *diagram) (qui.Point, qui.Point) {
	var from, to qui.Point
	if e.fromID >= 0 {
		if n := d.nodeByID(e.fromID); n != nil {
			from = n.portPos(e.fromDir)
		}
	} else {
		from = e.fromPos
	}
	if e.toID >= 0 {
		if n := d.nodeByID(e.toID); n != nil {
			to = n.portPos(e.toDir)
		}
	} else {
		to = e.toPos
	}
	return from, to
}

type diagram struct {
	nodes    []*node
	edges    []*edge
	nextNode int
	nextEdge int
}

func newDiagram() *diagram {
	d := &diagram{nextNode: 1, nextEdge: 1}
	// A small starter graph so the canvas isn't blank on first launch.
	a := d.addNode(shapeEllipse, qui.Point{X: 320, Y: 80}, "Start")
	b := d.addNode(shapeRect, qui.Point{X: 320, Y: 200}, "Load input")
	c := d.addNode(shapeDiamond, qui.Point{X: 320, Y: 320}, "Valid?")
	yes := d.addNode(shapeRect, qui.Point{X: 160, Y: 460}, "Process")
	no := d.addNode(shapeRect, qui.Point{X: 480, Y: 460}, "Show error")
	end := d.addNode(shapeEllipse, qui.Point{X: 320, Y: 600}, "End")
	d.addEdge(a, portS, b, portN, "", routingQuadratic)
	d.addEdge(b, portS, c, portN, "", routingQuadratic)
	d.addEdge(c, portW, yes, portN, "yes", routingOrth)
	d.addEdge(c, portE, no, portN, "no", routingOrth)
	d.addEdge(yes, portS, end, portW, "", routingCubic)
	d.addEdge(no, portS, end, portE, "", routingCubic)
	d.recomputeParallelOffsets()
	return d
}

func (d *diagram) addNode(shape nodeShape, pos qui.Point, label string) int {
	id := d.nextNode
	d.nextNode++
	d.nodes = append(d.nodes, &node{
		id:    id,
		shape: shape,
		pos:   pos,
		size:  defaultNodeSize(shape),
		label: label,
	})
	return id
}

func (d *diagram) addEdge(fromID int, fromDir portDir, toID int, toDir portDir, label string, routing edgeRouting) int {
	id := d.nextEdge
	d.nextEdge++
	e := &edge{
		id:         id,
		fromID:     fromID,
		fromDir:    fromDir,
		toID:       toID,
		toDir:      toDir,
		label:      label,
		routing:    routing,
		endArrow:   arrowTriangle,
		startArrow: arrowNone,
	}
	d.edges = append(d.edges, e)
	d.recomputeParallelOffsets()
	return id
}

func (d *diagram) nodeByID(id int) *node {
	for _, n := range d.nodes {
		if n.id == id {
			return n
		}
	}
	return nil
}

func (d *diagram) edgeByID(id int) *edge {
	for _, e := range d.edges {
		if e.id == id {
			return e
		}
	}
	return nil
}

func (d *diagram) removeNode(id int) {
	for i, n := range d.nodes {
		if n.id == id {
			d.nodes = append(d.nodes[:i], d.nodes[i+1:]...)
			break
		}
	}
	out := d.edges[:0]
	for _, e := range d.edges {
		if e.fromID != id && e.toID != id {
			out = append(out, e)
		}
	}
	d.edges = out
	d.recomputeParallelOffsets()
}

func (d *diagram) removeEdge(id int) {
	for i, e := range d.edges {
		if e.id == id {
			d.edges = append(d.edges[:i], d.edges[i+1:]...)
			break
		}
	}
	d.recomputeParallelOffsets()
}

// recomputeParallelOffsets walks the edge list and assigns a curving
// offset to every edge that shares the same unordered (fromID, toID)
// pair as a sibling. The Nth edge in a group of K gets offset
// (N - (K-1)/2) * stepPx so two parallel edges fan apart instead of
// rendering directly on top of each other. G6 calls this "loopCfg"
// for self-loops and there's no parallel-edge helper out of the box —
// drawio's behavior is the design reference here.
func (d *diagram) recomputeParallelOffsets() {
	const stepPx = 32
	type key struct{ a, b int }
	groups := map[key][]*edge{}
	for _, e := range d.edges {
		if e.fromID < 0 || e.toID < 0 {
			continue
		}
		k := key{e.fromID, e.toID}
		if e.toID < e.fromID {
			k = key{e.toID, e.fromID}
		}
		groups[k] = append(groups[k], e)
	}
	for _, group := range groups {
		// Stable order — older edges keep their place; new edges land
		// at the outermost offset.
		n := len(group)
		for i, e := range group {
			if n == 1 {
				e.parallelOffset = 0
				continue
			}
			e.parallelOffset = (float32(i) - float32(n-1)/2) * stepPx
		}
	}
}

func defaultNodeSize(s nodeShape) qui.Size {
	switch s {
	case shapeEllipse, shapeRoundedRect:
		return qui.Size{W: 140, H: 56}
	case shapeDiamond:
		return qui.Size{W: 160, H: 90}
	case shapeCylinder:
		return qui.Size{W: 120, H: 80}
	case shapeHexagon:
		return qui.Size{W: 160, H: 70}
	default:
		return qui.Size{W: 160, H: 60}
	}
}

// ---- Editor widget ----

type editor struct {
	qui.BaseWidget

	d *diagram

	// View transform: canvas-screen = pan + zoom * world.
	pan  qui.Point
	zoom float32

	// Selection.
	selNodeID int // -1 if none
	selEdgeID int // -1 if none

	// Hover state.
	hoverNodeID int // node currently in port-approach range (for showing ports)

	// Interaction mode.
	mode editMode

	// drag-node state
	dragNodeOff qui.Point
	dragNode    *node

	// drag-connector state — from a port to a free point / another node.
	dragEdge     *edge
	dragFromNode *node
	dragFromPort portDir

	// drag-palette state
	dragPaletteShape nodeShape
	hasPaletteDrag   bool

	// pan-drag state
	panStart    qui.Point
	panStartPan qui.Point

	// Last cursor position (screen coords) for redrawing preview lines
	// and palette-row hover highlight.
	lastMouse qui.Point

	spaceHeld bool

	// Inspector state — last edge selected gets a default routing if
	// none chosen via the side panel.
	defaultRouting    edgeRouting
	defaultEndArrow   arrowStyle
	defaultStartArrow arrowStyle
	defaultDashed     bool

	// Inline edit state — the edge whose label is currently being
	// typed into, plus the staged buffer (committed to ed.label on
	// Enter / focus-leave). -1 = not editing.
	editingEdgeID int
	editBuffer    string

	// Double-click detection — we synthesize this manually because
	// qui doesn't expose a dedicated EventMouseDoubleClick. Two
	// MouseDowns within doubleClickWindow and within doubleClickRadius
	// of each other count as a double click. `hasPrevDown` gates the
	// time comparison so the very first click isn't accidentally
	// classified as a double click against the zero-time sentinel.
	hasPrevDown bool
	lastDownAt  time.Time
	lastDownPos qui.Point
}

type editMode int

const (
	modeIdle editMode = iota
	modeDragNode
	modeDragEdge
	modeDragPalette
	modePan
)

// Layout constants.
const (
	paletteWidth    float32 = 200
	inspectorWidth  float32 = 220
	portApproachPad float32 = 24 // world-px slack around a node for port reveal
)

func newEditor() *editor {
	e := &editor{
		BaseWidget:        qui.NewBaseWidget(),
		d:                 newDiagram(),
		zoom:              1.0,
		selNodeID:         -1,
		selEdgeID:         -1,
		hoverNodeID:       -1,
		editingEdgeID:     -1,
		defaultRouting:    routingQuadratic,
		defaultEndArrow:   arrowTriangle,
		defaultStartArrow: arrowNone,
	}
	e.SetSelf(e)
	e.SetID("flowchart-editor")
	return e
}

func (e *editor) Measure(_ qui.Size) qui.Size {
	return qui.Size{W: 1180, H: 760}
}

// HitTest returns the OUTER editor pointer via Self so the framework's
// dispatcher reaches editor.Handle, not the embedded BaseWidget.Handle
// (which always returns false). Without this, every mouse event would
// fall into the inner BaseWidget no-op.
func (e *editor) HitTest(p qui.Point) qui.Widget {
	if !e.Bounds().Contains(p) {
		return nil
	}
	if s := e.Self(); s != nil {
		return s
	}
	return e
}

// Focusable so we receive key events.
func (e *editor) Focusable() bool         { return true }
func (e *editor) SetFocused(focused bool) {}

// ---- Geometry helpers ----

func (e *editor) canvasRect() qui.Rect {
	b := e.Bounds()
	return qui.Rect{
		X: b.X + paletteWidth,
		Y: b.Y,
		W: b.W - paletteWidth - inspectorWidth,
		H: b.H,
	}
}

func (e *editor) paletteRect() qui.Rect {
	b := e.Bounds()
	return qui.Rect{X: b.X, Y: b.Y, W: paletteWidth, H: b.H}
}

func (e *editor) inspectorRect() qui.Rect {
	b := e.Bounds()
	return qui.Rect{X: b.X + b.W - inspectorWidth, Y: b.Y, W: inspectorWidth, H: b.H}
}

func (e *editor) worldFromScreen(p qui.Point) qui.Point {
	cr := e.canvasRect()
	if e.zoom == 0 {
		return qui.Point{}
	}
	return qui.Point{
		X: (p.X - cr.X - e.pan.X) / e.zoom,
		Y: (p.Y - cr.Y - e.pan.Y) / e.zoom,
	}
}

func (e *editor) screenFromWorld(p qui.Point) qui.Point {
	cr := e.canvasRect()
	return qui.Point{
		X: cr.X + e.pan.X + p.X*e.zoom,
		Y: cr.Y + e.pan.Y + p.Y*e.zoom,
	}
}

// hitNodeAtWorld returns the topmost node whose silhouette contains
// `world`. Topmost = last drawn = end of e.d.nodes.
func (e *editor) hitNodeAtWorld(world qui.Point) *node {
	for i := len(e.d.nodes) - 1; i >= 0; i-- {
		n := e.d.nodes[i]
		if n.bounds().Contains(world) {
			return n
		}
	}
	return nil
}

// nodeInApproachRange returns the topmost node whose bounds, inflated
// by portApproachPad, contain `world`. Used to keep ports visible
// while the user is *approaching* a node — the small-target frustration
// the user hit before this fix.
func (e *editor) nodeInApproachRange(world qui.Point) *node {
	// Inflate the search by the approach pad scaled to world units —
	// the constant is in world coords already (it's added to the
	// bounds), so no zoom division.
	for i := len(e.d.nodes) - 1; i >= 0; i-- {
		n := e.d.nodes[i]
		b := n.bounds()
		inflated := qui.Rect{
			X: b.X - portApproachPad,
			Y: b.Y - portApproachPad,
			W: b.W + 2*portApproachPad,
			H: b.H + 2*portApproachPad,
		}
		if inflated.Contains(world) {
			return n
		}
	}
	return nil
}

// hitPortAtScreen returns the (node, port, ok) whose port marker is
// under the cursor. Port hit zone is generous (16 logical px) so the
// user doesn't have to be pixel-perfect.
func (e *editor) hitPortAtScreen(screen qui.Point) (*node, portDir, bool) {
	if e.hoverNodeID < 0 {
		return nil, 0, false
	}
	n := e.d.nodeByID(e.hoverNodeID)
	if n == nil {
		return nil, 0, false
	}
	const portHitRadius float32 = 16
	for _, p := range []portDir{portN, portE, portS, portW} {
		pp := e.screenFromWorld(n.portPos(p))
		if absF(screen.X-pp.X) <= portHitRadius && absF(screen.Y-pp.Y) <= portHitRadius {
			return n, p, true
		}
	}
	return nil, 0, false
}

// hitEdgeAtWorld returns the edge whose curve passes within
// `tolerance` world-units of `world`. Each routing knows how to
// flatten itself to a sample polyline; we walk those samples.
func (e *editor) hitEdgeAtWorld(world qui.Point, tolerance float32) *edge {
	tol2 := tolerance * tolerance
	for _, ed := range e.d.edges {
		samples := routeEdgeSampled(ed, e.d, 48)
		for i := 0; i < len(samples)-1; i++ {
			if pointSegmentDist2(world, samples[i], samples[i+1]) <= tol2 {
				return ed
			}
		}
	}
	return nil
}

func (e *editor) Handle(event qui.Event) bool {
	switch ev := event.(type) {
	case qui.MouseEvent:
		return e.handleMouse(ev)
	case qui.KeyEvent:
		return e.handleKey(ev)
	case qui.CharEvent:
		return e.handleChar(ev)
	}
	return false
}

// commitEdit writes editBuffer back to the editing edge's label and
// clears edit state. Called on Enter, Escape (no-revert variant), or
// focus-leave.
func (e *editor) commitEdit() {
	if e.editingEdgeID < 0 {
		return
	}
	if ed := e.d.edgeByID(e.editingEdgeID); ed != nil {
		ed.label = e.editBuffer
	}
	e.editingEdgeID = -1
	e.editBuffer = ""
	e.Invalidate()
}

// cancelEdit drops edit state without writing back.
func (e *editor) cancelEdit() {
	if e.editingEdgeID < 0 {
		return
	}
	e.editingEdgeID = -1
	e.editBuffer = ""
	e.Invalidate()
}

// startEdit puts the editor into "type into edge label" mode. Loads
// the existing label as the initial buffer so the user can append
// or backspace.
func (e *editor) startEdit(edgeID int) {
	if ed := e.d.edgeByID(edgeID); ed != nil {
		e.editingEdgeID = edgeID
		e.editBuffer = ed.label
		e.selEdgeID = edgeID
		e.selNodeID = -1
		e.Invalidate()
	}
}

// hitEdgeLabelAtWorld returns the edge whose label rect contains
// `world`. Used to detect clicks on the floating label and route
// them to selection / inline edit. Lookup is linear because edge
// counts are small.
func (e *editor) hitEdgeLabelAtWorld(world qui.Point) *edge {
	for _, ed := range e.d.edges {
		if ed.label == "" && ed.id != e.editingEdgeID {
			continue
		}
		center := edgeLabelCenter(ed, e.d)
		rect := edgeLabelRect(ed.label, center)
		if rect.Contains(world) {
			return ed
		}
	}
	return nil
}

func (e *editor) handleChar(ev qui.CharEvent) bool {
	if e.editingEdgeID < 0 {
		return false
	}
	// Filter control characters; everything printable joins the
	// buffer.
	if ev.Rune < 0x20 || ev.Rune == 0x7f {
		return false
	}
	e.editBuffer += string(ev.Rune)
	e.Invalidate()
	return true
}

func (e *editor) handleMouse(me qui.MouseEvent) bool {
	screen := qui.Point{X: me.X, Y: me.Y}
	e.lastMouse = screen
	canvas := e.canvasRect()
	palette := e.paletteRect()
	inspector := e.inspectorRect()
	switch me.Type() {
	case qui.EventMouseMove:
		// Hover-node update: prefer "inside-bounds" for selection-style
		// hits, then fall back to "approach range" to keep ports
		// visible. Update only when idle / dragging palette / dragging
		// edge (we want to keep the target node highlighted while
		// dragging an edge over it).
		if canvas.Contains(screen) && e.mode != modePan && e.mode != modeDragNode {
			world := e.worldFromScreen(screen)
			var hover *node
			if n := e.hitNodeAtWorld(world); n != nil {
				hover = n
			} else if n := e.nodeInApproachRange(world); n != nil {
				hover = n
			}
			newID := -1
			if hover != nil {
				newID = hover.id
			}
			if newID != e.hoverNodeID {
				e.hoverNodeID = newID
				e.Invalidate()
			}
		}
		switch e.mode {
		case modeDragNode:
			world := e.worldFromScreen(screen)
			e.dragNode.pos = qui.Point{
				X: world.X - e.dragNodeOff.X,
				Y: world.Y - e.dragNodeOff.Y,
			}
			e.Invalidate()
		case modeDragEdge:
			world := e.worldFromScreen(screen)
			if hit := e.hitNodeAtWorld(world); hit != nil && hit.id != e.dragFromNode.id {
				e.dragEdge.toID = hit.id
				e.dragEdge.toDir = hit.nearestPort(e.dragFromNode.portPos(e.dragFromPort))
				e.dragEdge.toPos = world
			} else if hit := e.nodeInApproachRange(world); hit != nil && hit.id != e.dragFromNode.id {
				// Approach range — still snap to nearest port so the
				// preview shows where it WOULD attach.
				e.dragEdge.toID = hit.id
				e.dragEdge.toDir = hit.nearestPort(world)
				e.dragEdge.toPos = world
			} else {
				e.dragEdge.toID = -1
				e.dragEdge.toPos = world
			}
			e.Invalidate()
		case modeDragPalette:
			e.Invalidate()
		case modePan:
			dx := screen.X - e.panStart.X
			dy := screen.Y - e.panStart.Y
			e.pan = qui.Point{X: e.panStartPan.X + dx, Y: e.panStartPan.Y + dy}
			e.Invalidate()
		}
		return true

	case qui.EventMouseDown:
		// Palette: each shape row is a draggable template.
		if palette.Contains(screen) {
			if shape, ok := e.paletteHit(screen); ok {
				e.mode = modeDragPalette
				e.dragPaletteShape = shape
				e.hasPaletteDrag = true
				e.Invalidate()
				return true
			}
			return false
		}
		// Inspector: clicks on toolbar buttons.
		if inspector.Contains(screen) {
			if e.inspectorHandleClick(screen) {
				e.Invalidate()
				return true
			}
			return false
		}
		if !canvas.Contains(screen) {
			return false
		}
		// Pan (middle button OR Space-held).
		if me.Button == qui.MouseButtonMiddle || (me.Button == qui.MouseButtonLeft && e.spaceHeld) {
			e.mode = modePan
			e.panStart = screen
			e.panStartPan = e.pan
			return true
		}
		// Port drag (left button) when hover-node has a port under cursor.
		if me.Button == qui.MouseButtonLeft {
			if n, port, ok := e.hitPortAtScreen(screen); ok {
				world := e.worldFromScreen(screen)
				e.mode = modeDragEdge
				e.dragFromNode = n
				e.dragFromPort = port
				e.dragEdge = &edge{
					id:         e.d.nextEdge,
					fromID:     n.id,
					fromDir:    port,
					toID:       -1,
					toPos:      world,
					routing:    e.defaultRouting,
					endArrow:   e.defaultEndArrow,
					startArrow: e.defaultStartArrow,
					dashed:     e.defaultDashed,
				}
				return true
			}
		}
		if me.Button == qui.MouseButtonLeft {
			world := e.worldFromScreen(screen)
			// Double-click detection: two MouseDowns close in time
			// and space at the same hit target enter inline edit
			// mode for the edge under the cursor. We use time.Now()
			// rather than the event's timestamp — synthetic test
			// events leave When zero, and using wall-clock keeps the
			// detector working under both real GLFW dispatch and
			// test-only dispatch through editor.handleMouse.
			now := time.Now()
			isDouble := e.hasPrevDown &&
				now.Sub(e.lastDownAt) < doubleClickWindow &&
				absF(screen.X-e.lastDownPos.X) < doubleClickRadius &&
				absF(screen.Y-e.lastDownPos.Y) < doubleClickRadius
			e.hasPrevDown = true
			e.lastDownAt = now
			e.lastDownPos = screen

			// Hit edge label FIRST — a label sits on top of the
			// edge curve and we want clicks on it to select the
			// edge (and double-click to edit), not to hit a node
			// below.
			if ed := e.hitEdgeLabelAtWorld(world); ed != nil {
				if isDouble {
					e.startEdit(ed.id)
				} else {
					e.commitEdit() // any other edge click ends editing
					e.selEdgeID = ed.id
					e.selNodeID = -1
					e.defaultRouting = ed.routing
					e.defaultEndArrow = ed.endArrow
					e.defaultStartArrow = ed.startArrow
					e.defaultDashed = ed.dashed
					e.Invalidate()
				}
				return true
			}
			// Any other click commits an in-progress edit.
			e.commitEdit()

			if n := e.hitNodeAtWorld(world); n != nil {
				e.selNodeID = n.id
				e.selEdgeID = -1
				e.mode = modeDragNode
				e.dragNode = n
				e.dragNodeOff = qui.Point{X: world.X - n.pos.X, Y: world.Y - n.pos.Y}
				e.Invalidate()
				return true
			}
			tol := 6 / e.zoom
			if ed := e.hitEdgeAtWorld(world, tol); ed != nil {
				if isDouble {
					e.startEdit(ed.id)
					return true
				}
				e.selEdgeID = ed.id
				e.selNodeID = -1
				// Adopt the selected edge's style as the default for
				// next drag — natural "carry style forward" behavior.
				e.defaultRouting = ed.routing
				e.defaultEndArrow = ed.endArrow
				e.defaultStartArrow = ed.startArrow
				e.defaultDashed = ed.dashed
				e.Invalidate()
				return true
			}
			if e.selNodeID >= 0 || e.selEdgeID >= 0 {
				e.selNodeID = -1
				e.selEdgeID = -1
				e.Invalidate()
			}
		}
		return true

	case qui.EventMouseUp:
		switch e.mode {
		case modeDragNode:
			e.mode = modeIdle
			e.dragNode = nil
		case modeDragEdge:
			if e.dragEdge.toID >= 0 && e.dragEdge.toID != e.dragEdge.fromID {
				e.dragEdge.id = e.d.nextEdge
				e.d.nextEdge++
				e.d.edges = append(e.d.edges, e.dragEdge)
				e.d.recomputeParallelOffsets()
				e.selEdgeID = e.dragEdge.id
				e.selNodeID = -1
			}
			e.dragEdge = nil
			e.dragFromNode = nil
			e.mode = modeIdle
			e.Invalidate()
		case modeDragPalette:
			if canvas.Contains(screen) {
				world := e.worldFromScreen(screen)
				size := defaultNodeSize(e.dragPaletteShape)
				pos := qui.Point{X: world.X - size.W/2, Y: world.Y - size.H/2}
				id := e.d.addNode(e.dragPaletteShape, pos, shapeName(e.dragPaletteShape))
				e.selNodeID = id
				e.selEdgeID = -1
			}
			e.mode = modeIdle
			e.hasPaletteDrag = false
			e.Invalidate()
		case modePan:
			e.mode = modeIdle
		}
		return true

	case qui.EventScroll:
		if !canvas.Contains(screen) {
			return false
		}
		oldZoom := e.zoom
		factor := float32(pow10(me.DeltaY * 0.04))
		newZoom := clamp(oldZoom*factor, 0.2, 4.0)
		if newZoom == oldZoom {
			return true
		}
		world := e.worldFromScreen(screen)
		e.zoom = newZoom
		e.pan = qui.Point{
			X: screen.X - canvas.X - world.X*newZoom,
			Y: screen.Y - canvas.Y - world.Y*newZoom,
		}
		e.Invalidate()
		return true
	}
	return false
}

func (e *editor) handleKey(ke qui.KeyEvent) bool {
	// Editing mode owns Backspace / Enter / Escape / Delete and
	// short-circuits the global hotkeys so the user isn't fighting
	// "1-5 sets routing" while typing "1".
	if e.editingEdgeID >= 0 && ke.Type() == qui.EventKeyDown {
		switch ke.Key {
		case qui.KeyEnter:
			e.commitEdit()
			return true
		case qui.KeyEscape:
			e.cancelEdit()
			return true
		case qui.KeyBackspace:
			if len(e.editBuffer) > 0 {
				// Trim one rune off the tail.
				r := []rune(e.editBuffer)
				e.editBuffer = string(r[:len(r)-1])
				e.Invalidate()
			}
			return true
		}
		// All other keys (printable letters / digits / symbols)
		// arrive as CharEvent — we don't consume them here.
		return false
	}
	switch ke.Type() {
	case qui.EventKeyDown:
		switch ke.Key {
		case qui.KeySpace:
			e.spaceHeld = true
			return true
		case qui.KeyDelete, qui.KeyBackspace:
			if e.selNodeID >= 0 {
				e.d.removeNode(e.selNodeID)
				e.selNodeID = -1
				e.hoverNodeID = -1
				e.Invalidate()
				return true
			}
			if e.selEdgeID >= 0 {
				e.d.removeEdge(e.selEdgeID)
				e.selEdgeID = -1
				e.Invalidate()
				return true
			}
		case qui.KeyEscape:
			if e.selNodeID >= 0 || e.selEdgeID >= 0 {
				e.selNodeID = -1
				e.selEdgeID = -1
				e.Invalidate()
				return true
			}
		case qui.Key1, qui.Key2, qui.Key3, qui.Key4, qui.Key5:
			// Cycle the selected edge's routing — G6-style keyboard
			// shortcuts.
			if e.selEdgeID >= 0 {
				ed := e.d.edgeByID(e.selEdgeID)
				if ed != nil {
					routings := []edgeRouting{routingLine, routingQuadratic, routingCubic, routingOrth, routingPolyline}
					ed.routing = routings[int(ke.Key)-int(qui.Key1)]
					e.defaultRouting = ed.routing
					e.Invalidate()
					return true
				}
			}
		case qui.KeyD:
			if e.selEdgeID >= 0 {
				ed := e.d.edgeByID(e.selEdgeID)
				if ed != nil {
					ed.dashed = !ed.dashed
					e.defaultDashed = ed.dashed
					e.Invalidate()
					return true
				}
			}
		case qui.KeyA:
			if e.selEdgeID >= 0 {
				ed := e.d.edgeByID(e.selEdgeID)
				if ed != nil {
					ed.endArrow = cycleArrow(ed.endArrow)
					e.defaultEndArrow = ed.endArrow
					e.Invalidate()
					return true
				}
			}
		case qui.KeyS:
			if e.selEdgeID >= 0 {
				ed := e.d.edgeByID(e.selEdgeID)
				if ed != nil {
					ed.startArrow = cycleArrow(ed.startArrow)
					e.defaultStartArrow = ed.startArrow
					e.Invalidate()
					return true
				}
			}
		}
	case qui.EventKeyUp:
		if ke.Key == qui.KeySpace {
			e.spaceHeld = false
			return true
		}
	}
	return false
}

// paletteHit returns the shape under the cursor when in palette area.
func (e *editor) paletteHit(p qui.Point) (nodeShape, bool) {
	b := e.paletteRect()
	const itemH = 78
	rowsStart := b.Y + 56
	for i := 0; i < paletteItemCount(); i++ {
		rowY := rowsStart + float32(i)*itemH
		if p.Y >= rowY && p.Y < rowY+itemH-4 && p.X >= b.X+8 && p.X <= b.X+b.W-8 {
			return paletteItem(i), true
		}
	}
	return 0, false
}

func paletteItemCount() int { return 7 }
func paletteItem(i int) nodeShape {
	return []nodeShape{
		shapeRect, shapeRoundedRect, shapeEllipse,
		shapeDiamond, shapeParallelogram, shapeHexagon, shapeCylinder,
	}[i]
}

// ---- Drawing ----

var (
	bgFill       = qui.Color{R: 0.07, G: 0.08, B: 0.10, A: 1}
	gridDot      = qui.Color{R: 0.16, G: 0.18, B: 0.22, A: 1}
	paletteBG    = qui.Color{R: 0.10, G: 0.11, B: 0.13, A: 1}
	paletteCard  = qui.Color{R: 0.14, G: 0.16, B: 0.20, A: 1}
	paletteHover = qui.Color{R: 0.20, G: 0.24, B: 0.30, A: 1}
	textCol      = qui.Color{R: 0.94, G: 0.95, B: 0.97, A: 1}
	subtextCol   = qui.Color{R: 0.60, G: 0.65, B: 0.72, A: 1}
	nodeFill     = qui.Color{R: 0.22, G: 0.30, B: 0.42, A: 1}
	nodeAccent   = qui.Color{R: 0.38, G: 0.30, B: 0.20, A: 1}
	nodeOval     = qui.Color{R: 0.20, G: 0.42, B: 0.32, A: 1}
	nodeStroke   = qui.Color{R: 0.18, G: 0.22, B: 0.28, A: 1}
	selStroke    = qui.Color{R: 0.30, G: 0.65, B: 1.0, A: 1}
	edgeStroke   = qui.Color{R: 0.65, G: 0.70, B: 0.78, A: 1}
	portFill     = qui.Color{R: 0.30, G: 0.65, B: 1.0, A: 1}
	portRing     = qui.Color{R: 0.85, G: 0.92, B: 1.0, A: 1}
	approachRing = qui.Color{R: 0.30, G: 0.65, B: 1.0, A: 0.35}
)

func (e *editor) Draw(canvas qui.Canvas) {
	b := e.Bounds()
	canvas.FillRect(b, bgFill)

	// Palette (left strip).
	e.drawPalette(canvas)

	// Canvas region: clip + transform.
	cr := e.canvasRect()
	id := canvas.Save()
	canvas.ClipRect(cr)
	canvas.Translate(cr.X+e.pan.X, cr.Y+e.pan.Y)
	canvas.Scale(e.zoom, e.zoom)

	e.drawGrid(canvas, cr)

	// Edges first (under nodes). Edge being edited renders its
	// stroke+arrows with the saved label SUPPRESSED — we paint the
	// editing label in a separate pass after nodes so the caret
	// sits on top.
	for _, ed := range e.d.edges {
		selected := ed.id == e.selEdgeID
		suppress := ed.id == e.editingEdgeID
		if suppress {
			drawEdgeStrokeOnly(canvas, ed, e.d, selected)
		} else {
			drawEdge(canvas, ed, e.d, selected)
		}
	}
	// Live drag-edge preview.
	if e.mode == modeDragEdge && e.dragEdge != nil {
		drawEdge(canvas, e.dragEdge, e.d, true)
	}

	// Nodes on top of edge bodies.
	for _, n := range e.d.nodes {
		selected := n.id == e.selNodeID
		drawNode(canvas, n, selected)
	}

	// Arrows on top of nodes — second edge pass. Without this the
	// target node's fill covers the arrow head whenever the edge
	// tangent arrives at the silhouette and the head's body extends
	// inward (which a cubic routing routinely does after a node move).
	for _, ed := range e.d.edges {
		selected := ed.id == e.selEdgeID
		drawEdgeArrows(canvas, ed, e.d, selected)
	}
	if e.mode == modeDragEdge && e.dragEdge != nil {
		drawEdgeArrows(canvas, e.dragEdge, e.d, true)
	}

	// Edge labels — drawn AFTER arrows so the floating text sits on
	// top of any intersection. The edited edge shows its in-progress
	// buffer with a caret; all others show the saved label.
	for _, ed := range e.d.edges {
		if ed.id == e.editingEdgeID {
			drawEdgeLabel(canvas, e.editBuffer, edgeLabelCenter(ed, e.d), true)
			continue
		}
		if ed.label != "" {
			drawEdgeLabel(canvas, ed.label, edgeLabelCenter(ed, e.d), false)
		}
	}

	// Port markers on hovered node (above nodes).
	if e.hoverNodeID >= 0 && e.mode != modeDragPalette && e.mode != modePan {
		if hovered := e.d.nodeByID(e.hoverNodeID); hovered != nil {
			drawPorts(canvas, hovered, e.zoom)
		}
	}

	canvas.RestoreTo(id)

	// Palette-drag preview (in SCREEN coords).
	if e.mode == modeDragPalette {
		previewSize := defaultNodeSize(e.dragPaletteShape)
		previewRect := qui.Rect{
			X: e.lastMouse.X - previewSize.W*e.zoom/2,
			Y: e.lastMouse.Y - previewSize.H*e.zoom/2,
			W: previewSize.W * e.zoom,
			H: previewSize.H * e.zoom,
		}
		drawShapeSilhouette(canvas, e.dragPaletteShape, previewRect,
			withAlpha(fillForShape(e.dragPaletteShape), 0.6),
			withAlpha(selStroke, 0.8), 2)
	}

	e.drawInspector(canvas)
	e.drawHUD(canvas)
}

func (e *editor) drawPalette(canvas qui.Canvas) {
	pr := e.paletteRect()
	canvas.FillRect(pr, paletteBG)
	canvas.DrawLine(qui.Point{X: pr.X + pr.W - 0.5, Y: pr.Y},
		qui.Point{X: pr.X + pr.W - 0.5, Y: pr.Y + pr.H},
		qui.Color{R: 0.06, G: 0.07, B: 0.09, A: 1}, 1)
	titleRect := qui.Rect{X: pr.X + 14, Y: pr.Y + 18, W: pr.W - 28, H: 24}
	canvas.DrawText("Shapes", titleRect, textCol, qui.Font{Size: 15, Bold: true})

	const itemH float32 = 78
	rowsStart := pr.Y + 56
	for i := 0; i < paletteItemCount(); i++ {
		shape := paletteItem(i)
		rowY := rowsStart + float32(i)*itemH
		row := qui.Rect{X: pr.X + 8, Y: rowY, W: pr.W - 16, H: itemH - 6}
		fill := paletteCard
		if e.lastMouse.X >= row.X && e.lastMouse.X <= row.X+row.W &&
			e.lastMouse.Y >= row.Y && e.lastMouse.Y <= row.Y+row.H {
			fill = paletteHover
		}
		canvas.DrawShape(qui.ShapeRRect{Rect: row, Radius: 6},
			qui.Paint{Color: fill, AntiAlias: true})
		previewRect := qui.Rect{X: row.X + 8, Y: row.Y + 12, W: 64, H: row.H - 24}
		drawShapeSilhouette(canvas, shape, previewRect, fillForShape(shape), nodeStroke, 1.2)
		labelRect := qui.Rect{X: row.X + 84, Y: row.Y + 18, W: row.W - 92, H: 20}
		canvas.DrawText(shapeName(shape), labelRect, textCol, qui.Font{Size: 13, Bold: true})
		hintRect := qui.Rect{X: row.X + 84, Y: row.Y + 38, W: row.W - 92, H: 16}
		canvas.DrawText("drag to canvas", hintRect, subtextCol, qui.Font{Size: 11})
	}
}

func (e *editor) drawGrid(canvas qui.Canvas, screenRect qui.Rect) {
	topLeft := e.worldFromScreen(qui.Point{X: screenRect.X, Y: screenRect.Y})
	botRight := e.worldFromScreen(qui.Point{X: screenRect.X + screenRect.W, Y: screenRect.Y + screenRect.H})
	step := float32(24)
	x0 := float32(int(topLeft.X/step)) * step
	if topLeft.X < 0 {
		x0 -= step
	}
	y0 := float32(int(topLeft.Y/step)) * step
	if topLeft.Y < 0 {
		y0 -= step
	}
	for y := y0; y <= botRight.Y; y += step {
		for x := x0; x <= botRight.X; x += step {
			canvas.FillRect(qui.Rect{X: x, Y: y, W: 1 / e.zoom, H: 1 / e.zoom}, gridDot)
		}
	}
}

func (e *editor) drawHUD(canvas qui.Canvas) {
	b := e.Bounds()
	hint := "drag a shape onto canvas    space+drag pans    wheel zooms    hover a node for ports    select an edge then 1-5/D/A/S to restyle"
	rect := qui.Rect{X: b.X + paletteWidth + 12, Y: b.Y + 8, W: b.W - paletteWidth - inspectorWidth - 24, H: 18}
	canvas.DrawText(hint, rect, subtextCol, qui.Font{Size: 11})
}

func drawNode(canvas qui.Canvas, n *node, selected bool) {
	stroke := nodeStroke
	width := float32(1.5)
	if selected {
		stroke = selStroke
		width = 2
	}
	drawShapeSilhouette(canvas, n.shape, n.bounds(), fillForShape(n.shape), stroke, width)
	tw, th := qui.TextMetrics(n.label, qui.Font{Size: 14, Bold: true})
	rect := qui.Rect{
		X: n.pos.X + (n.size.W-tw)/2,
		Y: n.pos.Y + (n.size.H-th)/2,
		W: tw, H: th,
	}
	canvas.DrawText(n.label, rect, textCol, qui.Font{Size: 14, Bold: true})
}

func drawPorts(canvas qui.Canvas, n *node, zoom float32) {
	// Hit-radius indicator: a soft ring around each port so the user
	// sees the generous click zone before clicking.
	hitR := 16 / zoom
	for _, p := range []portDir{portN, portE, portS, portW} {
		pp := n.portPos(p)
		halo := qui.NewPath().AddCircle(pp.X, pp.Y, hitR)
		canvas.DrawShape(qui.ShapePath{Path: halo},
			qui.Paint{Color: approachRing, AntiAlias: true})
	}
	radius := 5 / zoom
	ringRadius := radius + 1.5/zoom
	for _, p := range []portDir{portN, portE, portS, portW} {
		pp := n.portPos(p)
		ring := qui.NewPath().AddCircle(pp.X, pp.Y, ringRadius)
		canvas.DrawShape(qui.ShapePath{Path: ring},
			qui.Paint{Color: portRing, AntiAlias: true})
		disk := qui.NewPath().AddCircle(pp.X, pp.Y, radius)
		canvas.DrawShape(qui.ShapePath{Path: disk},
			qui.Paint{Color: portFill, AntiAlias: true})
	}
}

// ---- Small math helpers ----

func absF(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func dist(a, b qui.Point) float32 {
	dx := a.X - b.X
	dy := a.Y - b.Y
	return float32sqrt(dx*dx + dy*dy)
}

// pointSegmentDist2 returns the squared distance from p to the
// segment (a, b).
func pointSegmentDist2(p, a, b qui.Point) float32 {
	dx := b.X - a.X
	dy := b.Y - a.Y
	lenSq := dx*dx + dy*dy
	if lenSq == 0 {
		px := p.X - a.X
		py := p.Y - a.Y
		return px*px + py*py
	}
	t := ((p.X-a.X)*dx + (p.Y-a.Y)*dy) / lenSq
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	ex := a.X + t*dx - p.X
	ey := a.Y + t*dy - p.Y
	return ex*ex + ey*ey
}

func clamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func withAlpha(c qui.Color, a float32) qui.Color {
	c.A *= a
	return c
}

// ---- Entry point ----

func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	w, err := app.NewWindow("Flowchart — drawio-style editor (G6-aligned edges)", 1240, 800)
	if err != nil {
		log.Fatal(err)
	}
	w.SetRenderer(qui.NewGLRenderer())
	ed := newEditor()
	// FLOWCHART_PRESELECT_EDGE=<id> selects an edge at startup — used by
	// screenshot scripts that want to verify inspector edge controls
	// render without going through a mouse-click path.
	if v := os.Getenv("FLOWCHART_PRESELECT_EDGE"); v != "" {
		if id, err := strconv.Atoi(v); err == nil {
			ed.selEdgeID = id
		}
	}
	// FLOWCHART_MOVE_END_X / Y shifts the End node by N world px so a
	// screenshot can probe the "drag-target" routing behavior without a
	// real mouse drag.
	if v := os.Getenv("FLOWCHART_MOVE_END_X"); v != "" {
		if dx, err := strconv.ParseFloat(v, 32); err == nil {
			if end := ed.d.nodeByID(6); end != nil {
				end.pos.X += float32(dx)
			}
		}
	}
	if v := os.Getenv("FLOWCHART_MOVE_END_Y"); v != "" {
		if dy, err := strconv.ParseFloat(v, 32); err == nil {
			if end := ed.d.nodeByID(6); end != nil {
				end.pos.Y += float32(dy)
			}
		}
	}
	// FLOWCHART_ROUTING=polyline|orth|cubic|line|quadratic switches all
	// edges to the requested routing before SetRoot — convenient for
	// visual comparison of the routing types.
	if v := os.Getenv("FLOWCHART_ROUTING"); v != "" {
		var r edgeRouting
		match := true
		switch v {
		case "line":
			r = routingLine
		case "quadratic":
			r = routingQuadratic
		case "cubic":
			r = routingCubic
		case "orth":
			r = routingOrth
		case "polyline":
			r = routingPolyline
		default:
			match = false
		}
		if match {
			for _, e := range ed.d.edges {
				e.routing = r
			}
		}
	}
	w.SetRoot(ed)
	if srv, err := agent.BindEnv(w); err != nil {
		log.Printf("flowchart: agent.BindEnv: %v", err)
	} else if srv != nil {
		defer srv.Stop()
	}
	app.Run()
}
