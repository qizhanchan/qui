package qui

import (
	"log"
	"os"
)

// Optional widget interfaces.
//
// The engine discovers most widget capabilities structurally: a widget
// that has the method gets the behavior. The interfaces below name those
// contracts so custom widgets can opt in on purpose (and assert
// conformance with `var _ qui.ModalOverlay = (*MyDialog)(nil)`). Others
// that live next to their subsystem: Tickable, Draggable, Droppable,
// DragDataProvider, DropAcceptor, ClickFocusPolicy, TabStopper,
// TabConsumer, OverlayLayouter, OverlayResizer, OverlayExiter, IMEClient,
// InteractionCanceler, PaintBounder, LayoutMeasurer.

// ChildLister exposes a widget's children to tree walks: focus order,
// hit testing helpers, window attach/detach, ticking, accessibility and the
// agent selectors. A custom container that holds child widgets without
// embedding Container must implement it, or its children are invisible to
// all of those.
type ChildLister interface {
	ChildList() []Widget
}

// ModalOverlay is implemented by overlays that trap input. While one is on
// the overlay stack, clicks outside it go nowhere, keys go to it, Tab
// cycles inside it, and window accelerators wait (scoped ones inside it
// excepted). Dialog is modal; Popup and tooltips are not.
type ModalOverlay interface {
	Modal() bool
}

// FocusVisibleAware widgets are told whether the current focus came from
// the keyboard (true) or a click (false), right after SetFocused(true) —
// the CSS :focus-visible distinction, so a focus ring can show for Tab and
// stay hidden after a click.
type FocusVisibleAware interface {
	SetFocusVisible(bool)
}

// WindowAware widgets are told when their subtree attaches to a window
// (SetWindow(w)) and detaches (SetWindow(nil)). BaseWidget implements it;
// a widget that overrides it must call the embedded SetWindow. It is the
// place to subscribe to process-wide notifications (SubscribeTheme,
// SubscribeLocale) and to drop those subscriptions again.
type WindowAware interface {
	SetWindow(*Window)
}

// LayoutDirtyMarker is implemented by a parent that doesn't embed
// BaseWidget but still wants its children's InvalidateLayout to schedule a
// relayout without a repaint. BaseWidget implements it.
type LayoutDirtyMarker interface {
	MarkLayoutDirty()
}

// widgetDebugEnabled turns on QUI_DEBUG_WIDGETS=1 structural checks: after
// each layout pass the tree is walked and every child whose Parent() is not
// the widget listing it is logged once. That mismatch is almost always a
// custom Container subclass that forgot SetSelf(outer) — which silently
// breaks hit testing, focus order and invalidation for its children.
var widgetDebugEnabled = os.Getenv("QUI_DEBUG_WIDGETS") == "1"

var widgetDebugReported = map[Widget]bool{}

func debugCheckTree(w Widget) {
	cl, ok := w.(ChildLister)
	if !ok {
		return
	}
	for _, child := range cl.ChildList() {
		if child == nil {
			continue
		}
		if p := child.Parent(); p != w && !widgetDebugReported[w] {
			widgetDebugReported[w] = true
			log.Printf("qui/debug-widgets: %T lists child %T whose Parent() is %T — missing SetSelf(outer) after construction?", w, child, p)
		}
		debugCheckTree(child)
	}
}
