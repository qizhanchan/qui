package widgets

import . "github.com/qizhanchan/qui"

// PopupCloseReason says why a popup is closing.
type PopupCloseReason int

const (
	// PopupCloseProgrammatic is Close() from app code (or a menu row
	// picked). Not vetoable.
	PopupCloseProgrammatic PopupCloseReason = iota
	// PopupCloseOutsideClick is a press outside the popup.
	PopupCloseOutsideClick
	// PopupCloseEscape is the Escape key.
	PopupCloseEscape
	// PopupCloseResize is a window resize on a popup without a resize
	// handler.
	PopupCloseResize
	// PopupCloseRemoved is the popup leaving its window behind its back —
	// Window.RemoveOverlay / PopOverlay, or the window closing. Not
	// vetoable; the popup is already gone.
	PopupCloseRemoved
)

// Popup is a lightweight overlay for dropdowns, context menus, and
// tooltips. It sits above the main tree (via Window overlay stack) but
// is NOT modal — events outside the content either dismiss the popup
// (default) or fall through to the main tree.
//
// Typical use:
//
//	popup := qui.NewPopup(menuContent)
//	popup.ShowAt(window, anchor.X, anchor.Y + anchor.H)
//	// later: popup.Close(), or let outside-click auto-dismiss
type Popup struct {
	BaseWidget
	Content               Widget
	DismissOnOutsideClick bool
	// ClickThrough lets the press that dismisses the popup also reach
	// whatever is under it — clicking another toolbar button while a
	// dropdown is open both closes the dropdown and presses the button.
	// Without it the dismissing press is swallowed (the menu convention).
	ClickThrough bool
	// AutoFocus moves keyboard focus into the content when the popup is
	// shown: to Content itself when it is focusable, otherwise to its first
	// focusable descendant. Focus returns where it was on close. Leave it
	// off for a popup that must not take focus from a field (autocomplete).
	AutoFocus bool
	// NoClamp keeps ShowAt's position verbatim; by default the popup is
	// shifted to stay inside the window.
	NoClamp bool
	// OnClose runs after the popup closed, for any reason.
	OnClose func()
	// OnCloseReason runs after the popup closed, with the reason.
	OnCloseReason func(PopupCloseReason)
	// CanClose may veto a user-driven close (outside click, Escape,
	// resize) by returning false — a popover holding an unsaved edit.
	// Programmatic Close is never vetoed.
	CanClose func(PopupCloseReason) bool

	// onResize, when set, runs on a window resize instead of the default
	// dismiss — anchored menus install a closure that re-places the popup
	// against its (moved) trigger. Set via SetResizeHandler.
	onResize func(newSize Size)

	// natural records that ShowAt sized the popup from its content's own
	// measure (rather than a caller-supplied size), so a relayout after a
	// content change re-measures instead of keeping the show-time size.
	natural bool

	window       *Window
	removeFilter func()
}

// SetResizeHandler installs a window-resize handler. When set, a window
// resize invokes fn instead of dismissing the popup — anchored menus use
// this to follow their trigger. Passing nil restores dismiss-on-resize.
func (p *Popup) SetResizeHandler(fn func(newSize Size)) { p.onResize = fn }

// NewPopup creates a popup wrapping the given content widget.
// DismissOnOutsideClick defaults to true — the common dropdown pattern.
func NewPopup(content Widget) *Popup {
	p := &Popup{
		BaseWidget:            NewBaseWidget(),
		Content:               content,
		DismissOnOutsideClick: true,
	}
	p.SetSelf(p)
	return p
}

// IsShown reports whether the popup is on a window's overlay stack.
func (p *Popup) IsShown() bool { return p.window != nil }

// ShowAt measures the content at the window's current size, lays it out
// at (x, y), then pushes the popup onto the window's overlay stack.
// Calling ShowAt on an already-shown popup is a no-op. Unless NoClamp is
// set, the position is shifted so the popup stays inside the window.
//
// The measure goes through MeasureConstrained, not Content.Measure: a
// popup positions its content itself instead of handing it to a layout
// engine, so an explicitly sized content widget (Style().Width/Height,
// SetPreferredSize, min/max) would otherwise be silently sized by its own
// natural Measure. Container / Button report content-hugging sizes there,
// which is how a datalist list ended up narrower than its field.
func (p *Popup) ShowAt(w *Window, x, y float32) {
	if w == nil || p.Content == nil || p.window != nil {
		return
	}
	size := MeasureConstrained(p.Content, w.Size())
	if !p.NoClamp {
		x, y = clampPopup(w.Size(), x, y, size)
	}
	p.showAtSize(w, x, y, size)
	p.natural = true
}

// clampPopup shifts (x, y) so a popup of size stays inside win, preferring
// to keep the top-left edge visible when it can't fit at all.
func clampPopup(win Size, x, y float32, size Size) (float32, float32) {
	if x+size.W > win.W {
		x = win.W - size.W
	}
	if y+size.H > win.H {
		y = win.H - size.H
	}
	return max(x, 0), max(y, 0)
}

func (p *Popup) showAtSize(w *Window, x, y float32, size Size) {
	if w == nil || p.Content == nil || p.window != nil {
		return
	}
	p.natural = false
	contentRect := Rect{X: x, Y: y, W: size.W, H: size.H}
	p.Layout(contentRect)
	p.Content.SetParent(p)
	p.Content.Layout(contentRect)
	p.window = w
	w.PushOverlay(p)
	// Window.PushOverlay invalidates only widget.Bounds(). Overlay
	// content that paints an elevation halo past Bounds (menu surface
	// level-2 shadow, dialog level-3 shadow) needs the halo region to
	// be in the first frame's dirty union too, otherwise clipCanvas
	// clips the halo's outer pixels and the show frame renders without
	// the full shadow. Union the inflated rect here so both Show and
	// Close see the same dirty area.
	if r, ok := overlayHaloRect(p.Content); ok {
		w.InvalidateRect(r)
	}
	if p.ClickThrough && p.DismissOnOutsideClick {
		p.removeFilter = w.AddEventFilter(p.clickThroughFilter)
	}
	if p.AutoFocus {
		if target := firstFocusable(p.Content); target != nil {
			w.SetFocus(target)
		}
	}
}

// clickThroughFilter dismisses on a press that lands anywhere outside
// this popup — in the main tree, or in an overlay beneath it (the dialog a
// toolbar dropdown was opened from) — and lets the press continue there.
// Presses on this popup, or on overlays above it (a submenu), are left
// alone.
func (p *Popup) clickThroughFilter(e Event) bool {
	me, ok := e.(MouseEvent)
	if !ok || me.Type() != EventMouseDown || p.window == nil {
		return false
	}
	hit := p.window.WidgetAt(Point{X: me.X, Y: me.Y})
	top := hit
	for top != nil && top.Parent() != nil {
		top = top.Parent()
	}
	if top != nil && p.overlayAtOrAbove(top) {
		return false
	}
	p.closeFor(PopupCloseOutsideClick)
	return false
}

// overlayAtOrAbove reports whether w is this popup or an overlay stacked
// above it.
func (p *Popup) overlayAtOrAbove(w Widget) bool {
	seen := false
	for _, ov := range p.window.Overlays() {
		if ov == p {
			seen = true
		}
		if seen && ov == w {
			return true
		}
	}
	return false
}

// SetWindow notices the popup being detached by something other than its
// own close path (Window.RemoveOverlay / PopOverlay, a closing window) and
// unwinds: IsShown turns false, OnClose / OnCloseReason(PopupCloseRemoved)
// fire, and ShowAt works again.
func (p *Popup) SetWindow(w *Window) {
	p.BaseWidget.SetWindow(w)
	if w == nil && p.window != nil {
		p.finishClose(PopupCloseRemoved)
	}
}

func firstFocusable(w Widget) Widget {
	if w == nil {
		return nil
	}
	if f, ok := w.(interface{ Focusable() bool }); ok && f.Focusable() {
		return w
	}
	if cl, ok := w.(ChildLister); ok {
		for _, c := range cl.ChildList() {
			if t := firstFocusable(c); t != nil {
				return t
			}
		}
	}
	return nil
}

// Close removes the popup from its window's overlay stack and fires
// OnClose / OnCloseReason. Safe to call multiple times.
func (p *Popup) Close() { p.close(PopupCloseProgrammatic) }

// closeFor is a user-driven close, which CanClose may veto.
func (p *Popup) closeFor(reason PopupCloseReason) bool {
	if p.CanClose != nil && !p.CanClose(reason) {
		return false
	}
	p.close(reason)
	return true
}

func (p *Popup) close(reason PopupCloseReason) {
	w := p.window
	if w == nil {
		return
	}
	// Invalidate the inflated content halo BEFORE RemoveOverlay so the
	// area covered by the elevation shadow gets repainted with whatever
	// is underneath. Without this, Window.RemoveOverlay only marks
	// Bounds dirty and the shadow halo pixels (painted into adjacent
	// regions when the overlay was up) stay on the framebuffer until
	// some other event redirties that area. The bug is most visible
	// when a menu opens upward — the bottom shadow halo extends into
	// the trigger button area, and on close the trigger button keeps
	// the gray halo overlay until next hover.
	if r, ok := overlayHaloRect(p.Content); ok {
		w.InvalidateRect(r)
	}
	// Mark closed before RemoveOverlay: detaching calls SetWindow(nil),
	// which must not read as an external removal. The callbacks run after
	// it, so one that reopens the popup isn't undone.
	p.markClosed()
	w.RemoveOverlay(p)
	p.fireClose(reason)
}

// finishClose clears the shown state and fires the close callbacks.
func (p *Popup) finishClose(reason PopupCloseReason) {
	p.markClosed()
	p.fireClose(reason)
}

func (p *Popup) markClosed() {
	p.window = nil
	if p.removeFilter != nil {
		p.removeFilter()
		p.removeFilter = nil
	}
}

func (p *Popup) fireClose(reason PopupCloseReason) {
	if p.OnClose != nil {
		p.OnClose()
	}
	if p.OnCloseReason != nil {
		p.OnCloseReason(reason)
	}
}

// overlayHalo is the optional interface an overlay's content can
// implement to declare visible pixels beyond its layout bounds — i.e.
// elevation shadows. Popup checks for it at show + close time so the
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

// OnWindowResize reacts to a window resize (satisfies OverlayResizer).
// A popup is positioned once at show time and the frame loop doesn't
// re-lay overlays out, so a resize would otherwise strand it detached
// from its trigger. Anchored menus install a resize handler that
// re-places the popup against its moved trigger; a point-anchored popup
// (right-click context menu) has nothing live to track, so it dismisses.
func (p *Popup) OnWindowResize(newSize Size) {
	if p.onResize != nil {
		p.onResize(newSize)
		return
	}
	if !p.closeFor(PopupCloseResize) {
		// Vetoed: keep it on screen.
		b := p.Bounds()
		x, y := clampPopup(newSize, b.X, b.Y, Size{W: b.W, H: b.H})
		p.RelayoutAt(x, y, Size{W: b.W, H: b.H})
	}
}

// RelayoutAt moves an already-shown popup to a new position/size,
// invalidating both the old and new extents (content halo included) so
// the move repaints cleanly. No-op if the popup isn't shown. Used by
// anchored menus to follow their trigger across a window resize.
func (p *Popup) RelayoutAt(x, y float32, size Size) {
	if p.window == nil || p.Content == nil {
		return
	}
	// Old extent (+ elevation halo) must repaint with what's underneath.
	if r, ok := overlayHaloRect(p.Content); ok {
		p.window.InvalidateRect(r)
	}
	p.window.InvalidateRect(p.Bounds())
	contentRect := Rect{X: x, Y: y, W: size.W, H: size.H}
	p.Layout(contentRect)
	p.Content.Layout(contentRect)
	// New extent (+ halo) into the dirty union.
	if r, ok := overlayHaloRect(p.Content); ok {
		p.window.InvalidateRect(r)
	}
	p.window.InvalidateRect(p.Bounds())
}

// RelayoutOverlay re-lays the content out after it invalidated layout
// while shown (satisfies OverlayLayouter). The position is kept; a popup
// shown at its content's natural size (ShowAt) re-measures, while one
// given an explicit size (anchored menus) keeps it.
func (p *Popup) RelayoutOverlay(winSize Size) {
	if p.window == nil || p.Content == nil {
		return
	}
	b := p.Bounds()
	size := Size{W: b.W, H: b.H}
	if p.natural {
		size = MeasureConstrained(p.Content, winSize)
	}
	p.RelayoutAt(b.X, b.Y, size)
}

// PaintBounds covers the content's elevation halo too, so the window's
// overlay paint pass doesn't skip the popup when only its shadow lies in
// the dirty region.
func (p *Popup) PaintBounds() Rect {
	b := p.Bounds()
	if r, ok := overlayHaloRect(p.Content); ok {
		b = b.Union(r)
	}
	return b
}

// ChildList exposes Content for focus traversal / tick / etc.
func (p *Popup) ChildList() []Widget {
	if p.Content == nil {
		return nil
	}
	return []Widget{p.Content}
}

func (p *Popup) Draw(canvas Canvas) {
	if p.Content != nil {
		p.Content.Draw(canvas)
	}
}

func (p *Popup) Handle(event Event) bool {
	switch e := event.(type) {
	case MouseEvent:
		if e.Type() == EventMouseDown {
			if !p.Bounds().Contains(Point{X: e.X, Y: e.Y}) {
				if p.DismissOnOutsideClick {
					p.closeFor(PopupCloseOutsideClick)
				}
				return true // consume so main tree doesn't also see it
			}
		}
	case KeyEvent:
		if e.Type() == EventKeyDown && e.Key == KeyEscape {
			p.closeFor(PopupCloseEscape)
			return true
		}
	}
	return false
}

// HitTest routes clicks inside the popup to its content. If
// DismissOnOutsideClick is set (and ClickThrough isn't), the popup claims
// outside clicks too so Handle can dismiss them. Otherwise outside clicks
// fall through to whatever's underneath.
func (p *Popup) HitTest(point Point) Widget {
	if p.Content != nil {
		if p.Bounds().Contains(point) {
			if hit := p.Content.HitTest(point); hit != nil {
				return hit
			}
			return p
		}
	}
	if p.DismissOnOutsideClick && !p.ClickThrough {
		return p
	}
	return nil
}
