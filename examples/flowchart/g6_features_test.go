package main

import (
	"image"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
)

// TestApproachZoneShowsPorts verifies that hoverNodeID is set when the
// cursor enters the inflated approach band, not just when it's inside
// the node's strict bounds. This is the "ports keep showing as I
// approach the shape" UX fix.
func TestApproachZoneShowsPorts(t *testing.T) {
	e := newEditor()
	e.Layout(qui.Rect{W: 1240, H: 800})

	// Start is at world (320, 80) size (140, 56), so its right edge is
	// world x=460. A cursor at world (475, 108) is 15 px outside but
	// well inside the 24-px approach pad.
	approachScreen := e.screenFromWorld(qui.Point{X: 475, Y: 108})
	var mods qui.Modifiers
	e.handleMouse(qui.NewMouseEvent(qui.EventMouseMove, approachScreen.X, approachScreen.Y, qui.MouseButtonLeft, mods))
	if e.hoverNodeID != 1 {
		t.Fatalf("expected ports to reveal for Start when cursor is in approach band; hoverNodeID=%d", e.hoverNodeID)
	}

	// 30px outside is beyond the 24px pad — ports should hide.
	farScreen := e.screenFromWorld(qui.Point{X: 495, Y: 108})
	e.handleMouse(qui.NewMouseEvent(qui.EventMouseMove, farScreen.X, farScreen.Y, qui.MouseButtonLeft, mods))
	if e.hoverNodeID != -1 {
		t.Errorf("expected ports to hide when cursor is past the approach pad; hoverNodeID=%d", e.hoverNodeID)
	}
}

// TestPortHitZoneIsGenerous confirms the port hit radius is large
// enough that a click 12 logical px away still grabs the port.
func TestPortHitZoneIsGenerous(t *testing.T) {
	e := newEditor()
	e.Layout(qui.Rect{W: 1240, H: 800})

	var mods qui.Modifiers
	// Reveal ports first by hovering Start's east edge.
	e.handleMouse(qui.NewMouseEvent(qui.EventMouseMove,
		e.screenFromWorld(qui.Point{X: 460, Y: 108}).X,
		e.screenFromWorld(qui.Point{X: 460, Y: 108}).Y,
		qui.MouseButtonLeft, mods))
	// Start's E port world (460, 108). Click 12 px below — still inside
	// the 16-px hit radius.
	portScreen := e.screenFromWorld(qui.Point{X: 460, Y: 108})
	clickX := portScreen.X
	clickY := portScreen.Y + 12
	_, _, ok := e.hitPortAtScreen(qui.Point{X: clickX, Y: clickY})
	if !ok {
		t.Errorf("expected port hit 12 px below the port center to succeed")
	}
}

// TestEdgeRoutingTypes exercises each of the 5 G6-aligned routings and
// confirms the sampler produces a non-empty polyline for each.
func TestEdgeRoutingTypes(t *testing.T) {
	d := newDiagram()
	a := d.nodeByID(1)
	b := d.nodeByID(2)
	if a == nil || b == nil {
		t.Fatal("expected starter diagram nodes")
	}
	for _, r := range []edgeRouting{routingLine, routingQuadratic, routingCubic, routingOrth, routingPolyline} {
		e := &edge{
			fromID: a.id, fromDir: portS,
			toID: b.id, toDir: portN,
			routing: r,
		}
		samples := routeEdgeSampled(e, d, 32)
		if len(samples) < 2 {
			t.Errorf("routing %s produced %d samples; expected ≥ 2", routingName(r), len(samples))
			continue
		}
		// First sample should equal the source port, last the target.
		if samples[0] != a.portPos(portS) {
			t.Errorf("%s: first sample %v ≠ source port %v", routingName(r), samples[0], a.portPos(portS))
		}
		if samples[len(samples)-1] != b.portPos(portN) {
			t.Errorf("%s: last sample %v ≠ target port %v", routingName(r), samples[len(samples)-1], b.portPos(portN))
		}
	}
}

// TestInspectorRoutingButtons clicks the inspector's "Orth" radio
// button and confirms the selected edge changes routing.
func TestInspectorRoutingButtons(t *testing.T) {
	e := newEditor()
	e.Layout(qui.Rect{W: 1240, H: 800})

	// Pick edge #1 (Start → Load input) for selection.
	e.selEdgeID = 1
	ed := e.d.edgeByID(1)
	if ed == nil {
		t.Fatal("edge id 1 missing")
	}
	if ed.routing != routingQuadratic {
		t.Fatalf("expected default quadratic, got %v", ed.routing)
	}

	// Build the inspector layout and find the "Orth" button.
	btns := e.inspectorLayout()
	var orthBtn *inspectorButton
	for i := range btns {
		if btns[i].label == "Orth" {
			orthBtn = &btns[i]
			break
		}
	}
	if orthBtn == nil {
		t.Fatal("Orth button not found in inspector layout")
	}
	// Click its center via the editor's mouse path.
	cx := orthBtn.rect.X + orthBtn.rect.W/2
	cy := orthBtn.rect.Y + orthBtn.rect.H/2
	var mods qui.Modifiers
	e.handleMouse(qui.NewMouseEvent(qui.EventMouseDown, cx, cy, qui.MouseButtonLeft, mods))
	e.handleMouse(qui.NewMouseEvent(qui.EventMouseUp, cx, cy, qui.MouseButtonLeft, mods))
	if ed.routing != routingOrth {
		t.Errorf("expected routing Orth after clicking the button; got %v", ed.routing)
	}
}

// TestKeyboardShortcutsRestyleEdge confirms the 1-5/D/A/S shortcuts.
func TestKeyboardShortcutsRestyleEdge(t *testing.T) {
	e := newEditor()
	e.Layout(qui.Rect{W: 1240, H: 800})
	e.selEdgeID = 1
	ed := e.d.edgeByID(1)
	if ed == nil {
		t.Fatal("edge id 1 missing")
	}
	var mods qui.Modifiers
	// Key1 → routingLine.
	e.handleKey(qui.NewKeyEvent(qui.EventKeyDown, qui.Key1, mods))
	if ed.routing != routingLine {
		t.Errorf("Key1 should set routingLine; got %v", ed.routing)
	}
	// Key4 → routingOrth.
	e.handleKey(qui.NewKeyEvent(qui.EventKeyDown, qui.Key4, mods))
	if ed.routing != routingOrth {
		t.Errorf("Key4 should set routingOrth; got %v", ed.routing)
	}
	// D → toggle dashed.
	wasDashed := ed.dashed
	e.handleKey(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyD, mods))
	if ed.dashed == wasDashed {
		t.Errorf("D should toggle dashed")
	}
	// A → cycle end arrow.
	prevEnd := ed.endArrow
	e.handleKey(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyA, mods))
	if ed.endArrow == prevEnd {
		t.Errorf("A should cycle end arrow")
	}
	// S → cycle start arrow.
	prevStart := ed.startArrow
	e.handleKey(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyS, mods))
	if ed.startArrow == prevStart {
		t.Errorf("S should cycle start arrow")
	}
}

// TestParallelEdgesFanOut verifies adding a second edge between the
// same two nodes assigns non-zero parallelOffset to both.
func TestParallelEdgesFanOut(t *testing.T) {
	d := newDiagram()
	a := d.nodeByID(1)
	b := d.nodeByID(2)
	// There's already one a→b edge in newDiagram. Add a second.
	id2 := d.addEdge(a.id, portE, b.id, portE, "", routingQuadratic)
	// Collect both edges between a and b.
	var ab []*edge
	for _, ed := range d.edges {
		if (ed.fromID == a.id && ed.toID == b.id) || (ed.fromID == b.id && ed.toID == a.id) {
			ab = append(ab, ed)
		}
	}
	if len(ab) != 2 {
		t.Fatalf("expected 2 parallel edges between a and b; have %d", len(ab))
	}
	if ab[0].parallelOffset == 0 && ab[1].parallelOffset == 0 {
		t.Errorf("expected non-zero parallelOffsets on parallel edges; got %v / %v",
			ab[0].parallelOffset, ab[1].parallelOffset)
	}
	if ab[0].parallelOffset == ab[1].parallelOffset {
		t.Errorf("parallel edges should fan in OPPOSITE directions; got %v / %v",
			ab[0].parallelOffset, ab[1].parallelOffset)
	}
	_ = id2
}

// TestInspectorShowsArrowThumbnails renders the inspector with an edge
// selected and confirms the 6 arrow thumbnails (None/Triangle/Vee/
// Diamond/Circle/Rect) appear in the layout for both start and end.
func TestInspectorShowsArrowThumbnails(t *testing.T) {
	e := newEditor()
	e.Layout(qui.Rect{W: 1240, H: 800})
	e.selEdgeID = 1
	btns := e.inspectorLayout()
	// Routing (5) + Dashed (1) + End label (1) + End thumbs (6) +
	// Start label (1) + Start thumbs (6) = 20.
	if len(btns) != 20 {
		t.Errorf("expected 20 inspector buttons (5+1+1+6+1+6); got %d", len(btns))
	}
}

// TestDashedOrthEdgeDoesNotHang is the regression test for the "click
// Dash on a selected orth-routed edge → CPU pegs at 100%" bug. The
// original drawDashedPolyline walked each segment with a
// `consumed += step` loop; on certain pattern phases `step` shrunk to a
// sub-ULP value and the loop spun forever without making progress.
//
// We exercise the failure path directly: build a RecordingCanvas, set
// the "yes" edge to dashed+orth, and render. A wall-clock deadline
// guards against a regression that re-introduces the hang.
func TestDashedOrthEdgeDoesNotHang(t *testing.T) {
	e := newEditor()
	e.Layout(qui.Rect{W: 1240, H: 800})

	// "yes" edge is id 3 (Valid? → Process, W → N, routingOrth).
	ed := e.d.edgeByID(3)
	if ed == nil || ed.routing != routingOrth {
		t.Fatalf("expected edge 3 to be orth-routed")
	}
	ed.dashed = true

	done := make(chan struct{})
	go func() {
		canvas := &qui.RecordingCanvas{}
		drawEdge(canvas, ed, e.d, false)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("drawEdge hung — dashed walker regressed on orth corners")
	}
}

// TestDashedAllRoutings renders every (routing × dashed) pair against
// the recording canvas to catch float-precision hangs in any of the
// 5 routing paths.
func TestDashedAllRoutings(t *testing.T) {
	d := newDiagram()
	a := d.nodeByID(1)
	b := d.nodeByID(4) // Process — far enough that orth has interior bends.
	for _, r := range []edgeRouting{routingLine, routingQuadratic, routingCubic, routingOrth, routingPolyline} {
		r := r
		t.Run(routingName(r), func(t *testing.T) {
			ed := &edge{
				fromID: a.id, fromDir: portS,
				toID: b.id, toDir: portN,
				routing: r,
				dashed:  true,
			}
			done := make(chan struct{})
			go func() {
				canvas := &qui.RecordingCanvas{}
				drawEdge(canvas, ed, d, false)
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(500 * time.Millisecond):
				t.Fatalf("dashed %s edge hung", routingName(r))
			}
		})
	}
}

// TestArrowVisibleAfterNodeMove reproduces the "I dragged End to the
// right, now the inbound arrows disappear" report. The bug is paint
// order: drawEdge paints the arrow head as part of the edge pass, and
// the edge pass runs BEFORE the node pass — so the target node's fill
// covers the arrow whenever the cubic's tangent crosses the node's
// silhouette near the port (which happens routinely once you move
// the target).
//
// We render the full editor into an *image.RGBA, locate the End node,
// and assert the arrow head pixels OUTSIDE the ellipse silhouette are
// visible (non-background). Saving the PNG out via QUI_GOLDEN=1 lets a
// human inspect the result.
func TestArrowVisibleAfterNodeMove(t *testing.T) {
	e := newEditor()
	e.Layout(qui.Rect{W: 1240, H: 800})
	end := e.d.nodeByID(6)
	if end == nil || end.label != "End" {
		t.Fatal("expected node 6 to be End")
	}
	end.pos.X += 250

	img := image.NewRGBA(image.Rect(0, 0, 1240, 800))
	canvas := qui.NewImageCanvas(img)
	e.Draw(canvas)

	if os.Getenv("QUI_GOLDEN") == "1" {
		f, _ := os.Create("/tmp/flowchart-arrow-test.png")
		defer f.Close()
		_ = png.Encode(f, img)
		t.Logf("wrote /tmp/flowchart-arrow-test.png")
	}

	// Sample several points along the arrow body for Process→End.
	// The cubic tangent at the target is +X (right) → arrow body sits
	// to the LEFT of End.portW. Body length ≈ 12 px → sample 4 px left
	// of the silhouette edge to land mid-body.
	bgR, bgG, bgB := bgFill.R*255, bgFill.G*255, bgFill.B*255
	for _, dx := range []float32{-4, -8} {
		sampleWorld := qui.Point{X: end.pos.X + dx, Y: end.pos.Y + end.size.H/2}
		sampleScreen := e.screenFromWorld(sampleWorld)
		c := img.RGBAAt(int(sampleScreen.X), int(sampleScreen.Y))
		// Background or close to it = invisible arrow.
		if absI(int(c.R)-int(bgR)) < 16 && absI(int(c.G)-int(bgG)) < 16 && absI(int(c.B)-int(bgB)) < 16 {
			t.Errorf("arrow body at world %v / screen %v matched background %v — arrow hidden by node fill",
				sampleWorld, sampleScreen, c)
		}
	}
}

func absI(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// TestOrthRouteAvoidsTargetNode reproduces the user's polyline bug:
// after dragging End so that Show error's portS sits over End's
// x-range, the naive L-corner (from.X, to.Y) lands INSIDE End and the
// routing slices through the target's body. The fix routes around the
// inflated target bbox.
//
// We assert two invariants for the Show error → End polyline:
//   - No interior vertex lies inside End's inflated bbox.
//   - No segment crosses End's interior except the final stub-to-port
//     arrival.
func TestOrthRouteAvoidsTargetNode(t *testing.T) {
	d := newDiagram()
	end := d.nodeByID(6)
	if end == nil {
		t.Fatal("expected End node")
	}
	// Move End so it sits under Show error horizontally.
	showErr := d.nodeByID(5)
	if showErr == nil {
		t.Fatal("expected Show error node")
	}
	// Position End so its X-range covers Show error's portS X.
	end.pos = qui.Point{
		X: showErr.portPos(portS).X - end.size.W*0.7,
		Y: showErr.pos.Y + showErr.size.H + 120,
	}

	// Find the Show error → End edge.
	var seToEnd *edge
	for _, e := range d.edges {
		if e.fromID == showErr.id && e.toID == end.id {
			seToEnd = e
			break
		}
	}
	if seToEnd == nil {
		t.Fatal("Show error → End edge missing from starter diagram")
	}
	seToEnd.routing = routingPolyline

	verts := routeEdgeSampled(seToEnd, d, 32)
	if len(verts) < 3 {
		t.Fatalf("expected at least 3 vertices; got %d (%v)", len(verts), verts)
	}

	// 1) No interior vertex (i.e. neither first nor last, which are
	//    the port itself) may lie inside End's bbox.
	endBox := end.bounds()
	for i := 1; i < len(verts)-1; i++ {
		if pointInRect(verts[i], endBox) {
			t.Errorf("interior vertex %d (%v) lies inside End bbox %v — routing slices through the target",
				i, verts[i], endBox)
		}
	}

	// 2) Interior segments (not the last one, which arrives at the
	//    port from outside) must not cross End's interior.
	for i := 0; i < len(verts)-2; i++ {
		if segmentCrossesRectInterior(verts[i], verts[i+1], endBox) {
			t.Errorf("segment %d→%d (%v → %v) crosses End interior %v",
				i, i+1, verts[i], verts[i+1], endBox)
		}
	}
}

// segmentCrossesRectInterior reports whether the segment [a, b]
// passes through the rect's INTERIOR (not just touching an edge).
// Sample N points along the segment and test each.
func segmentCrossesRectInterior(a, b qui.Point, r qui.Rect) bool {
	const samples = 16
	const skin float32 = 1.0
	inside := qui.Rect{X: r.X + skin, Y: r.Y + skin, W: r.W - 2*skin, H: r.H - 2*skin}
	if inside.W <= 0 || inside.H <= 0 {
		return false
	}
	for i := 1; i < samples; i++ {
		t := float32(i) / float32(samples)
		p := qui.Point{X: a.X + t*(b.X-a.X), Y: a.Y + t*(b.Y-a.Y)}
		if p.X >= inside.X && p.X <= inside.X+inside.W && p.Y >= inside.Y && p.Y <= inside.Y+inside.H {
			return true
		}
	}
	return false
}

// TestDedupColinearKeepsBacktracks pins the dedupColinear semantic
// fix: three colinear points where the middle is a TURNING POINT
// (not between the other two) must stay in the output, so the routing
// bug that produced the backtrack stays visible.
func TestDedupColinearKeepsBacktracks(t *testing.T) {
	// Monotonic colinear — middle should be dropped.
	out := dedupColinear([]qui.Point{{X: 0}, {X: 5}, {X: 10}})
	if len(out) != 2 {
		t.Errorf("monotonic colinear: expected 2 points, got %d (%v)", len(out), out)
	}
	// Reversing colinear — middle is the turning point, must stay.
	out = dedupColinear([]qui.Point{{X: 0}, {X: 10}, {X: 5}})
	if len(out) != 3 {
		t.Errorf("backtrack colinear: expected 3 points, got %d (%v)", len(out), out)
	}
}

// TestDoubleClickEdgeLabelStartsEdit checks that two MouseDowns
// close in time and space on an edge's label rect put the editor
// into inline-edit mode for that edge.
func TestDoubleClickEdgeLabelStartsEdit(t *testing.T) {
	e := newEditor()
	e.Layout(qui.Rect{W: 1240, H: 800})

	// Edge 3 is the "yes" label (Valid? → Process). Find its label
	// center in screen coords.
	yesEdge := e.d.edgeByID(3)
	if yesEdge == nil || yesEdge.label != "yes" {
		t.Fatalf("expected edge 3 to be the 'yes' edge, got %v", yesEdge)
	}
	labelWorld := edgeLabelCenter(yesEdge, e.d)
	labelScreen := e.screenFromWorld(labelWorld)

	var mods qui.Modifiers
	first := qui.NewMouseEvent(qui.EventMouseDown, labelScreen.X, labelScreen.Y, qui.MouseButtonLeft, mods)
	second := qui.NewMouseEvent(qui.EventMouseDown, labelScreen.X+1, labelScreen.Y+1, qui.MouseButtonLeft, mods)

	e.handleMouse(first)
	e.handleMouse(qui.NewMouseEvent(qui.EventMouseUp, labelScreen.X, labelScreen.Y, qui.MouseButtonLeft, mods))
	if e.editingEdgeID == 3 {
		t.Fatal("single click should NOT start edit")
	}
	if e.selEdgeID != 3 {
		t.Fatalf("single click on label should select the edge; selEdgeID=%d", e.selEdgeID)
	}

	// Second MouseDown within the double-click window.
	e.handleMouse(second)
	if e.editingEdgeID != 3 {
		t.Fatalf("double click should start edit on edge 3; editingEdgeID=%d", e.editingEdgeID)
	}
	if e.editBuffer != "yes" {
		t.Errorf("edit buffer should initialize with the current label; got %q", e.editBuffer)
	}
}

// TestCharAndBackspaceEditLabel walks the editor through typing into
// a label and pressing Backspace, then committing with Enter.
func TestCharAndBackspaceEditLabel(t *testing.T) {
	e := newEditor()
	e.Layout(qui.Rect{W: 1240, H: 800})
	e.startEdit(3) // Valid? → Process (label "yes")
	if e.editBuffer != "yes" {
		t.Fatalf("expected initial buffer 'yes', got %q", e.editBuffer)
	}
	// Type "!!" — two CharEvents.
	e.handleChar(qui.CharEvent{Rune: '!'})
	e.handleChar(qui.CharEvent{Rune: '!'})
	if e.editBuffer != "yes!!" {
		t.Errorf("after typing '!!' buffer should be 'yes!!'; got %q", e.editBuffer)
	}
	// Backspace pops one char.
	var mods qui.Modifiers
	e.handleKey(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyBackspace, mods))
	if e.editBuffer != "yes!" {
		t.Errorf("after backspace buffer should be 'yes!'; got %q", e.editBuffer)
	}
	// Enter commits.
	e.handleKey(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyEnter, mods))
	if e.editingEdgeID != -1 {
		t.Errorf("Enter should exit edit mode; editingEdgeID=%d", e.editingEdgeID)
	}
	if ed := e.d.edgeByID(3); ed == nil || ed.label != "yes!" {
		t.Errorf("Enter should write buffer back to edge label; got %v", ed)
	}
}

// TestEscapeCancelsEdit verifies Escape drops the buffer without
// touching the saved label.
func TestEscapeCancelsEdit(t *testing.T) {
	e := newEditor()
	e.Layout(qui.Rect{W: 1240, H: 800})
	e.startEdit(3)
	e.handleChar(qui.CharEvent{Rune: 'X'})
	if e.editBuffer != "yesX" {
		t.Fatalf("typing X should append; got %q", e.editBuffer)
	}
	var mods qui.Modifiers
	e.handleKey(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyEscape, mods))
	if e.editingEdgeID != -1 {
		t.Errorf("Escape should exit edit mode")
	}
	if ed := e.d.edgeByID(3); ed.label != "yes" {
		t.Errorf("Escape should NOT modify the saved label; got %q", ed.label)
	}
}

// TestEditNotTriggeredByHotkey makes sure the global "1-5/D/A/S"
// hotkeys are suppressed while editing — so typing digits into a
// label doesn't accidentally change the routing.
func TestEditNotTriggeredByHotkey(t *testing.T) {
	e := newEditor()
	e.Layout(qui.Rect{W: 1240, H: 800})
	e.startEdit(3)
	ed := e.d.edgeByID(3)
	startRouting := ed.routing
	var mods qui.Modifiers
	e.handleKey(qui.NewKeyEvent(qui.EventKeyDown, qui.Key1, mods))
	if ed.routing != startRouting {
		t.Errorf("digits during edit should NOT change routing; routing changed from %v to %v",
			startRouting, ed.routing)
	}
}

// TestSelfLoopRouting verifies a self-edge produces a non-degenerate
// arc above the node.
func TestSelfLoopRouting(t *testing.T) {
	d := newDiagram()
	n := d.nodeByID(1)
	e := &edge{
		fromID: n.id, fromDir: portN,
		toID: n.id, toDir: portN,
		routing: routingQuadratic,
	}
	samples := routeEdgeSampled(e, d, 32)
	if len(samples) < 4 {
		t.Fatalf("self-loop should produce a curve; got %d samples", len(samples))
	}
	// Loop should bow upward, so at least one sample has y < node.top.
	top := n.bounds().Y
	bowed := false
	for _, p := range samples {
		if p.Y < top-1 {
			bowed = true
			break
		}
	}
	if !bowed {
		t.Errorf("self-loop should bow above the node top y=%v", top)
	}
}
