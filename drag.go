package qui

import (
	"slices"
	"time"
)

// Widget drag-and-drop.
//
// A drag starts from the nearest Draggable ancestor of the press once the
// pointer moves past a 4px dead zone. The source receives DragStart /
// DragMove / DragEnd; the Droppable under the pointer receives DragEnter,
// DragOver (every move), DragLeave and finally Drop. Every event carries the
// same *DragData, which the source fills in through DragDataProvider.
//
// OS file drops use the same path: the Droppable under the drop point gets
// an EventDrop with Data.Files set and Source nil. Only if no widget takes
// it does the window-level SetOnFileDrop callback run.

// DropEffect is what a drop will do, for the cursor and for the source.
type DropEffect int

const (
	DropEffectMove DropEffect = iota
	DropEffectCopy
	DropEffectLink
	DropEffectNone
)

// DragData is a drag's payload, keyed by type — conventionally a MIME type
// ("text/plain", "text/uri-list") or an app-private one
// ("application/x-myapp-row"). A target checks Has before accepting.
type DragData struct {
	items map[string]any
	order []string
	// Files holds absolute paths for an OS file drop.
	Files []string
	// Effect is the operation the source allows / the target chose. A
	// target may lower it during DragOver (move → copy with Alt held).
	Effect DropEffect
	// Image, when set by the source, is painted under the pointer for the
	// duration of the drag (a row thumbnail, a count badge). It is laid out
	// at its measured size; ImageOffset places its top-left relative to the
	// pointer. It never takes hits.
	Image       Widget
	ImageOffset Point
}

// Set stores v under typ, replacing any earlier value.
func (d *DragData) Set(typ string, v any) {
	if d.items == nil {
		d.items = map[string]any{}
	}
	if _, ok := d.items[typ]; !ok {
		d.order = append(d.order, typ)
	}
	d.items[typ] = v
}

// Get returns the value stored under typ.
func (d *DragData) Get(typ string) (any, bool) {
	if d == nil {
		return nil, false
	}
	v, ok := d.items[typ]
	return v, ok
}

// Has reports whether the payload carries typ. "Files" is reported for a
// file drop.
func (d *DragData) Has(typ string) bool {
	if d == nil {
		return false
	}
	if typ == "Files" {
		return len(d.Files) > 0
	}
	_, ok := d.items[typ]
	return ok
}

// Text returns the "text/plain" item as a string.
func (d *DragData) Text() string {
	v, _ := d.Get("text/plain")
	s, _ := v.(string)
	return s
}

// Types lists the item types in the order they were first Set.
func (d *DragData) Types() []string {
	if d == nil {
		return nil
	}
	out := slices.Clone(d.order)
	if len(d.Files) > 0 {
		out = append(out, "Files")
	}
	return out
}

// DragDataProvider is implemented by a Draggable that has a payload. It is
// called once, as the drag starts; returning nil is the same as an empty
// payload.
type DragDataProvider interface {
	DragData() *DragData
}

// DropAcceptor lets a Droppable refuse a particular drag (wrong type, a
// folder onto itself). A refusing widget is skipped and the search moves to
// its Droppable ancestors, so an inner list can decline what the outer pane
// accepts. A Droppable without it accepts everything.
type DropAcceptor interface {
	AcceptsDrop(*DragData) bool
}

// dragSession is the window's view of the drag in flight.
type dragSession struct {
	data    *DragData
	over    Widget // droppable that received DragEnter
	pos     Point  // last pointer position, window space
	image   Widget
	imgRect Rect
}

// DragInProgress reports whether a widget drag is under way, and its
// payload.
func (w *Window) DragInProgress() (*DragData, bool) {
	if w == nil || !w.dragging {
		return nil, false
	}
	return w.drag.data, true
}

// droppableFor returns the nearest self-or-ancestor of hit that is
// Droppable and accepts data, or nil.
func droppableFor(hit Widget, data *DragData) Widget {
	for cur := hit; cur != nil; cur = cur.Parent() {
		d, ok := cur.(Droppable)
		if !ok || !d.Droppable() {
			continue
		}
		if a, ok := cur.(DropAcceptor); ok && !a.AcceptsDrop(data) {
			continue
		}
		return cur
	}
	return nil
}

func (w *Window) handleDragMove(me MouseEvent) {
	if w.dragCandidate == nil {
		return
	}
	at := Point{X: me.X, Y: me.Y}
	dx := me.X - w.dragStart.X
	dy := me.Y - w.dragStart.Y
	if !w.dragging {
		if dx*dx+dy*dy < 16 {
			return
		}
		w.dragging = true
		// A widget drag now owns the gesture. Drop any text selection that
		// formed during the sub-dead-zone moves and disarm the selection
		// drag so the highlight doesn't fight the drag (dispatch also stops
		// routing moves to the captured widget while w.dragging).
		w.clearAllTextSelection()
		w.endTextSelectionDrag()
		var data *DragData
		if p, ok := w.dragCandidate.(DragDataProvider); ok {
			data = p.DragData()
		}
		if data == nil {
			data = &DragData{}
		}
		w.drag = dragSession{data: data, pos: at}
		w.dragCandidate.Handle(w.dragEventFor(w.dragCandidate, EventDragStart, at))
		w.startDragImage(data.Image)
	}
	w.drag.pos = at
	w.moveDragImage()
	w.dragCandidate.Handle(w.dragEventFor(w.dragCandidate, EventDragMove, at))
	over := droppableFor(w.hitTestAll(at), w.drag.data)
	if over != w.drag.over {
		if prev := w.drag.over; prev != nil {
			prev.Handle(w.dragEventFor(prev, EventDragLeave, at))
		}
		w.drag.over = over
		if over != nil {
			over.Handle(w.dragEventFor(over, EventDragEnter, at))
		}
	}
	// Notify the droppable under the cursor (standard DnD dragover) so a
	// target can show a drop indicator. Fired every move; the target
	// dedupes if it wants.
	if over != nil {
		over.Handle(w.dragEventFor(over, EventDragOver, at))
	}
}

func (w *Window) handleDragEnd(me MouseEvent) {
	if w.dragCandidate == nil {
		return
	}
	if w.dragging {
		at := Point{X: me.X, Y: me.Y}
		target := droppableFor(w.hitTestAll(at), w.drag.data)
		if prev := w.drag.over; prev != nil && prev != target {
			prev.Handle(w.dragEventFor(prev, EventDragLeave, at))
		}
		accepted := false
		if target != nil {
			accepted = target.Handle(w.dragEventFor(target, EventDrop, at))
			target.Handle(w.dragEventFor(target, EventDragLeave, at))
		}
		end := w.dragEventFor(w.dragCandidate, EventDragEnd, at)
		end.Accepted = accepted
		w.dragCandidate.Handle(end)
	}
	w.finishDrag()
}

// cancelDrag ends a drag whose source left the tree: the hovered target
// gets DragLeave and the source a DragEnd with Accepted false.
func (w *Window) cancelDrag() {
	if w.dragging {
		if over := w.drag.over; over != nil {
			over.Handle(w.dragEventFor(over, EventDragLeave, w.drag.pos))
		}
		w.dragCandidate.Handle(w.dragEventFor(w.dragCandidate, EventDragEnd, w.drag.pos))
	}
	w.finishDrag()
}

func (w *Window) finishDrag() {
	w.stopDragImage()
	w.dragCandidate = nil
	w.dragging = false
	w.drag = dragSession{}
}

// dragEventFor builds a synthesized drag event addressed to receiver, with
// the cursor position mapped into the RECEIVER's own coordinate space — a
// drop target inside a scroll container computes its insertion index from
// its children's retained (content-space) bounds, so a raw window
// coordinate would land in the wrong row.
func (w *Window) dragEventFor(receiver Widget, kind EventType, at Point) DragEvent {
	p := WindowPointToLocal(receiver, at)
	data := w.drag.data
	if data == nil {
		data = &DragData{}
	}
	return DragEvent{
		baseEvent: baseEvent{shared: &eventState{target: receiver, currentTarget: receiver, phase: PhaseTarget}},
		eventType: kind,
		When:      time.Now(),
		X:         p.X,
		Y:         p.Y,
		Source:    w.dragCandidate,
		Data:      data,
	}
}

func (w *Window) startDragImage(img Widget) {
	if img == nil {
		return
	}
	if !AdoptWidgetTree(img, nil, w) {
		return
	}
	w.drag.image = img
	w.moveDragImage()
}

func (w *Window) moveDragImage() {
	img := w.drag.image
	if img == nil {
		return
	}
	size := img.Measure(w.lastSize)
	off := w.drag.data.ImageOffset
	r := Rect{X: w.drag.pos.X + off.X, Y: w.drag.pos.Y + off.Y, W: size.W, H: size.H}
	if r == w.drag.imgRect {
		return
	}
	w.InvalidateRect(PaintBoundsInWindow(img))
	img.Layout(r)
	w.drag.imgRect = r
	w.InvalidateRect(PaintBoundsInWindow(img))
}

func (w *Window) stopDragImage() {
	img := w.drag.image
	if img == nil {
		return
	}
	w.InvalidateRect(PaintBoundsInWindow(img))
	w.drag.image = nil
	DetachWidgetTree(img)
}

// routeFileDrop offers an OS file drop to the Droppable under (x, y) and
// reports whether a widget took it.
func (w *Window) routeFileDrop(paths []string, x, y float32) bool {
	data := &DragData{Files: slices.Clone(paths), Effect: DropEffectCopy}
	target := droppableFor(w.hitTestAll(Point{X: x, Y: y}), data)
	if target == nil {
		return false
	}
	saved := w.drag
	w.drag = dragSession{data: data}
	ev := w.dragEventFor(target, EventDrop, Point{X: x, Y: y})
	ev.Source = nil
	took := target.Handle(ev)
	w.drag = saved
	return took
}
