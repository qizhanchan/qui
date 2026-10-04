package main

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// TestDragNode exercises the full MouseDown → MouseMove → MouseUp flow
// through editor.handleMouse and asserts the node position updates by
// the drag delta. Mirrors what real GLFW dispatch would deliver, so a
// regression in editor.HitTest / state-machine routing fails this test
// before a human notices "I can't drag".
func TestDragNode(t *testing.T) {
	e := newEditor()
	e.Layout(qui.Rect{W: 1180, H: 760})

	// "Start" was added first → id 1. Center (390, 108) in world; with
	// pan=(0,0) zoom=1 and palette=200 the screen-space center is
	// (590, 108).
	start := e.d.nodeByID(1)
	if start == nil {
		t.Fatal("Start node missing")
	}
	origPos := start.pos

	var mods qui.Modifiers
	e.handleMouse(qui.NewMouseEvent(qui.EventMouseDown, 590, 108, qui.MouseButtonLeft, mods))
	if e.selNodeID != 1 {
		t.Fatalf("expected Start selected after MouseDown, got selNodeID=%d", e.selNodeID)
	}
	if e.mode != modeDragNode {
		t.Fatalf("expected modeDragNode, got %d", e.mode)
	}
	e.handleMouse(qui.NewMouseEvent(qui.EventMouseMove, 800, 300, qui.MouseButtonLeft, mods))
	delta := qui.Point{X: start.pos.X - origPos.X, Y: start.pos.Y - origPos.Y}
	if delta.X != 210 || delta.Y != 192 {
		t.Errorf("expected node to move by (210, 192) world units; got %v", delta)
	}
	e.handleMouse(qui.NewMouseEvent(qui.EventMouseUp, 800, 300, qui.MouseButtonLeft, mods))
	if e.mode != modeIdle {
		t.Errorf("expected modeIdle after MouseUp, got %d", e.mode)
	}
}

// TestPaletteDropCreatesNode confirms the palette → canvas drag-and-drop
// path: clicking on a palette row enters modeDragPalette, releasing
// over the canvas inserts a new node centered at the drop point.
func TestPaletteDropCreatesNode(t *testing.T) {
	e := newEditor()
	e.Layout(qui.Rect{W: 1180, H: 760})
	beforeN := len(e.d.nodes)

	// Click the first palette row — y is 56 (title gap) + halfway into
	// the 78-px tall first item = ~90 within the palette strip.
	var mods qui.Modifiers
	e.handleMouse(qui.NewMouseEvent(qui.EventMouseDown, 100, 90, qui.MouseButtonLeft, mods))
	if e.mode != modeDragPalette {
		t.Fatalf("expected modeDragPalette after palette click, got %d", e.mode)
	}
	// Move across into the canvas and release at (700, 400).
	e.handleMouse(qui.NewMouseEvent(qui.EventMouseMove, 700, 400, qui.MouseButtonLeft, mods))
	e.handleMouse(qui.NewMouseEvent(qui.EventMouseUp, 700, 400, qui.MouseButtonLeft, mods))

	if got := len(e.d.nodes); got != beforeN+1 {
		t.Fatalf("expected a new node after palette drop; have %d (was %d)", got, beforeN)
	}
	created := e.d.nodes[len(e.d.nodes)-1]
	// Drop in screen (700, 400) with palette=200, pan=(0,0), zoom=1 →
	// world (500, 400). Created node centered there.
	expectedCenter := qui.Point{X: 500, Y: 400}
	gotCenter := created.center()
	if absF(gotCenter.X-expectedCenter.X) > 0.5 || absF(gotCenter.Y-expectedCenter.Y) > 0.5 {
		t.Errorf("expected center near %v, got %v", expectedCenter, gotCenter)
	}
}

// TestPortDragCreatesEdge exercises the hover-port → drag-from-port →
// release-on-another-node connector creation path.
func TestPortDragCreatesEdge(t *testing.T) {
	e := newEditor()
	e.Layout(qui.Rect{W: 1180, H: 760})
	beforeE := len(e.d.edges)

	var mods qui.Modifiers
	// First move over Start to set hoverNodeID.
	e.handleMouse(qui.NewMouseEvent(qui.EventMouseMove, 590, 108, qui.MouseButtonLeft, mods))
	if e.hoverNodeID != 1 {
		t.Fatalf("expected hoverNodeID=1 after hovering Start, got %d", e.hoverNodeID)
	}
	// Start's east port is at world (460, 108) → screen (660, 108). Click on it.
	e.handleMouse(qui.NewMouseEvent(qui.EventMouseDown, 660, 108, qui.MouseButtonLeft, mods))
	if e.mode != modeDragEdge {
		t.Fatalf("expected modeDragEdge, got %d", e.mode)
	}
	// Drag toward Load input (world center 400, 230 → screen 600, 230).
	e.handleMouse(qui.NewMouseEvent(qui.EventMouseMove, 600, 230, qui.MouseButtonLeft, mods))
	if e.dragEdge.toID != 2 {
		t.Errorf("expected drag edge to target node 2 (Load input), got toID=%d", e.dragEdge.toID)
	}
	e.handleMouse(qui.NewMouseEvent(qui.EventMouseUp, 600, 230, qui.MouseButtonLeft, mods))
	if got := len(e.d.edges); got != beforeE+1 {
		t.Errorf("expected a new edge committed; have %d (was %d)", got, beforeE)
	}
}
