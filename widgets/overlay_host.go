package widgets

import . "github.com/qizhanchan/qui"

// overlayHost is the overlay-stack lifecycle Popup and Dialog share (and,
// through Popup, Select's dropdown and MenuBar's menus): which window the
// overlay is on, pushing it there, taking it off again, and noticing when
// something else took it off — Window.RemoveOverlay / PopOverlay or the
// window closing. Embedded by value; each overlay keeps its own close
// reasons and callbacks and calls in here for the bookkeeping.
//
// The ordering it encodes is the subtle part: the shown state is cleared
// BEFORE RemoveOverlay, because detaching calls SetWindow(nil) on the
// overlay, and that must not read as an external removal (which would fire
// the close callbacks twice). Callbacks run after the overlay is off the
// stack, so one that reopens it isn't undone.
type overlayHost struct {
	window *Window
	// leaveHooks undo show-time side effects (an event filter) when the
	// overlay leaves its window, however that happens.
	leaveHooks []func()
}

// IsShown reports whether the overlay is on a window's overlay stack.
func (h *overlayHost) IsShown() bool { return h.window != nil }

// pushOverlay records w and pushes self onto its overlay stack. halo is the
// content whose elevation shadow (overlayHalo) paints past self's bounds;
// Window.PushOverlay only dirties Bounds, so without this the first frame
// clips the shadow's outer pixels. Nil when there is none.
func (h *overlayHost) pushOverlay(w *Window, self, halo Widget) {
	h.window = w
	w.PushOverlay(self)
	invalidateOverlayHalo(w, halo)
}

// onLeave registers fn to run when the overlay leaves its window.
func (h *overlayHost) onLeave(fn func()) {
	if fn != nil {
		h.leaveHooks = append(h.leaveHooks, fn)
	}
}

// popOverlay takes self off its window and reports whether it was shown.
// The halo is dirtied first so the shadow's pixels repaint with whatever
// is underneath — otherwise a menu that opened upward leaves its gray halo
// on the trigger until something else redirties that area.
func (h *overlayHost) popOverlay(self, halo Widget) bool {
	w := h.window
	if w == nil {
		return false
	}
	invalidateOverlayHalo(w, halo)
	h.leave()
	w.RemoveOverlay(self)
	return true
}

// removedExternally is the SetWindow hook: it reports (once) that the
// overlay was detached by something other than popOverlay, after clearing
// the shown state so a later show works again.
func (h *overlayHost) removedExternally(w *Window) bool {
	if w != nil || h.window == nil {
		return false
	}
	h.leave()
	return true
}

func (h *overlayHost) leave() {
	h.window = nil
	hooks := h.leaveHooks
	h.leaveHooks = nil
	for _, fn := range hooks {
		fn()
	}
}

// overlayHalo is the optional interface an overlay's content can
// implement to declare visible pixels beyond its layout bounds — i.e.
// elevation shadows. The host checks for it at show + close time so the
// extra region participates in the dirty union and the shadow halo
// repaints cleanly on both edges of the overlay's lifetime.
//
// Returned rect is in window coordinates (same as Bounds).
type overlayHalo interface {
	OverlayHaloRect() Rect
}

func overlayHaloRect(content Widget) (Rect, bool) {
	if content == nil {
		return Rect{}, false
	}
	if h, ok := content.(overlayHalo); ok {
		return h.OverlayHaloRect(), true
	}
	return Rect{}, false
}

func invalidateOverlayHalo(w *Window, content Widget) {
	if r, ok := overlayHaloRect(content); ok {
		w.InvalidateRect(r)
	}
}

// overlayOwner is the trigger side of the same lifecycle: a control that
// keeps at most one Popup open at a time and anchors it to itself (Select's
// dropdown, MenuBar's menus). It remembers the window popups open in —
// the constructor's until the control is mounted, then the attached one —
// and drops the open popup when the control leaves its window: a dropdown
// anchored to an unmounted control has nothing left to belong to (and must
// not keep firing OnChange from it).
type overlayOwner struct {
	window *Window
	open   *Popup
}

// holdPopup makes p the open popup. closed runs after p's own OnClose when
// p closes while it is still the one held, for any reason.
func (o *overlayOwner) holdPopup(p *Popup, closed func()) {
	o.open = p
	prev := p.OnClose
	p.OnClose = func() {
		if prev != nil {
			prev()
		}
		if o.open == p {
			o.open = nil
			if closed != nil {
				closed()
			}
		}
	}
}

// closePopup closes the held popup, if any; its closed hook runs.
func (o *overlayOwner) closePopup() {
	if o.open != nil {
		o.open.Close()
	}
}

// ownerSetWindow is the owner's SetWindow hook (after BaseWidget's):
// it tracks w and closes the open popup when the owner leaves its window.
func (o *overlayOwner) ownerSetWindow(w *Window) {
	o.window = w
	if w == nil {
		o.closePopup()
	}
}
