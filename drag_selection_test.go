package qui

import "testing"

// dragRowContainer is a Draggable row holding two selectable children laid
// out side by side. It reuses stubSel from text_selection_test.go.
type dragRowContainer struct {
	BaseWidget
	kids           []*stubSel
	dragStarted    bool
	dragOverSource Widget
}

func newDragRowContainer(a, b *stubSel) *dragRowContainer {
	r := &dragRowContainer{kids: []*stubSel{a, b}}
	r.SetSelf(r)
	a.SetParent(r)
	b.SetParent(r)
	return r
}

func (r *dragRowContainer) Draggable() bool { return true }

func (r *dragRowContainer) ChildList() []Widget {
	return []Widget{r.kids[0], r.kids[1]}
}

func (r *dragRowContainer) HitTest(p Point) Widget {
	if !r.Bounds().Contains(p) {
		return nil
	}
	for _, k := range r.kids {
		if hit := k.HitTest(p); hit != nil {
			return hit
		}
	}
	return r
}

// dragOverSource records the last widget seen dragging over this row.
func (r *dragRowContainer) Droppable() bool { return true }

func (r *dragRowContainer) Handle(event Event) bool {
	if de, ok := event.(DragEvent); ok {
		switch de.Type() {
		case EventDragStart:
			r.dragStarted = true
		case EventDragOver:
			r.dragOverSource = de.Source
		}
	}
	return false
}

// A widget drag over selectable children must NOT leave a text selection:
// once the drag owns the gesture the highlight is cleared and no longer
// extended. (Without the fix, dragging across the two children would
// paint a cross-widget selection over both.)
func TestDragSuppressesTextSelection(t *testing.T) {
	left := newStubSel("left row text")
	right := newStubSel("right row text")
	row := newDragRowContainer(left, right)

	w := NewTestWindow(Size{W: 400, H: 100})
	w.SetRoot(row)
	row.Layout(Rect{X: 0, Y: 0, W: 400, H: 40})
	left.Layout(Rect{X: 0, Y: 0, W: 200, H: 40})
	right.Layout(Rect{X: 200, Y: 0, W: 200, H: 40})

	// Press on the left child, then drag well past the dead-zone into the
	// right child.
	w.DispatchTestEvent(NewMouseEvent(EventMouseDown, 20, 20, MouseButtonLeft, 0))
	w.DispatchTestEvent(NewMouseEvent(EventMouseMove, 300, 20, MouseButtonLeft, 0))

	if !row.dragStarted {
		t.Fatalf("expected the row drag to start")
	}
	if got := left.SelectedText(); got != "" {
		t.Fatalf("left child selected %q during drag, want none", got)
	}
	if got := right.SelectedText(); got != "" {
		t.Fatalf("right child selected %q during drag, want none", got)
	}

	w.DispatchTestEvent(NewMouseEvent(EventMouseUp, 300, 20, MouseButtonLeft, 0))
}

// dragParent is a plain container holding two draggable rows so a drag can
// travel from one to the other (droppableAncestor walks up to each row).
type dragParent struct {
	BaseWidget
	rows []*dragRowContainer
}

func (p *dragParent) ChildList() []Widget {
	out := make([]Widget, len(p.rows))
	for i, r := range p.rows {
		out[i] = r
	}
	return out
}

func (p *dragParent) HitTest(pt Point) Widget {
	if !p.Bounds().Contains(pt) {
		return nil
	}
	for _, r := range p.rows {
		if hit := r.HitTest(pt); hit != nil {
			return hit
		}
	}
	return p
}

// A drag delivers EventDragOver to the droppable under the cursor (the row
// being hovered), carrying the dragged source — the primitive an app uses
// to render a drop indicator.
func TestDragOverNotifiesTarget(t *testing.T) {
	src := newDragRowContainer(newStubSel("a"), newStubSel("b"))
	dst := newDragRowContainer(newStubSel("c"), newStubSel("d"))
	parent := &dragParent{rows: []*dragRowContainer{src, dst}}
	parent.SetSelf(parent)
	src.SetParent(parent)
	dst.SetParent(parent)

	w := NewTestWindow(Size{W: 400, H: 200})
	w.SetRoot(parent)
	parent.Layout(Rect{X: 0, Y: 0, W: 400, H: 200})
	src.Layout(Rect{X: 0, Y: 0, W: 400, H: 40})
	src.kids[0].Layout(Rect{X: 0, Y: 0, W: 200, H: 40})
	src.kids[1].Layout(Rect{X: 200, Y: 0, W: 200, H: 40})
	dst.Layout(Rect{X: 0, Y: 100, W: 400, H: 40})
	dst.kids[0].Layout(Rect{X: 0, Y: 100, W: 200, H: 40})
	dst.kids[1].Layout(Rect{X: 200, Y: 100, W: 200, H: 40})

	// Press on the source row, drag down onto the destination row.
	w.DispatchTestEvent(NewMouseEvent(EventMouseDown, 20, 20, MouseButtonLeft, 0))
	w.DispatchTestEvent(NewMouseEvent(EventMouseMove, 20, 120, MouseButtonLeft, 0))

	if !src.dragStarted {
		t.Fatalf("expected the source row drag to start")
	}
	if dst.dragOverSource != Widget(src) {
		t.Fatalf("dst dragOverSource = %v, want the source row", dst.dragOverSource)
	}

	w.DispatchTestEvent(NewMouseEvent(EventMouseUp, 20, 120, MouseButtonLeft, 0))
}

// upRecorder records the MouseUp events it receives, so a test can check
// what the release that ends a drag carries.
type upRecorder struct {
	BaseWidget
	ups []MouseEvent
}

func (u *upRecorder) Draggable() bool { return true }

func (u *upRecorder) Handle(event Event) bool {
	if me, ok := event.(MouseEvent); ok && me.Type() == EventMouseUp {
		u.ups = append(u.ups, me)
	}
	return false
}

// The MouseUp that ends a drag is flagged AfterDrag so widgets can tell a
// drop from a click — dropping a row where you wanted it is not also a
// click on it. A release with no drag in flight is NOT flagged.
func TestMouseUpAfterDragIsFlagged(t *testing.T) {
	rec := &upRecorder{}
	rec.SetSelf(rec)

	w := NewTestWindow(Size{W: 200, H: 200})
	w.SetRoot(rec)
	rec.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})

	// Press / release without moving: a plain click, not a drag.
	w.DispatchTestEvent(NewMouseEvent(EventMouseDown, 20, 20, MouseButtonLeft, 0))
	w.DispatchTestEvent(NewMouseEvent(EventMouseUp, 20, 20, MouseButtonLeft, 0))
	if len(rec.ups) != 1 || rec.ups[0].AfterDrag {
		t.Fatalf("plain release: ups=%d AfterDrag=%v, want 1 false", len(rec.ups), rec.ups[0].AfterDrag)
	}

	// Press, drag past the dead-zone, release.
	w.DispatchTestEvent(NewMouseEvent(EventMouseDown, 20, 20, MouseButtonLeft, 0))
	w.DispatchTestEvent(NewMouseEvent(EventMouseMove, 20, 120, MouseButtonLeft, 0))
	w.DispatchTestEvent(NewMouseEvent(EventMouseUp, 20, 120, MouseButtonLeft, 0))
	if len(rec.ups) != 2 || !rec.ups[1].AfterDrag {
		t.Fatalf("drag release: ups=%d AfterDrag=%v, want 2 true", len(rec.ups), rec.ups[1].AfterDrag)
	}
}
