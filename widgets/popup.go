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
	overlayHost
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

	// passThrough, when set, resolves points OUTSIDE the popup before the
	// catch-all outside-click capture: a submenu routes the pointer to the
	// menus it was opened from, so hovering (or clicking) a parent row
	// reaches that row while the submenu is still up.
	passThrough func(Point) Widget
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

// SetContent replaces the popup content. A shown popup adopts the new tree
// and re-lays it immediately; assigning the Content field directly leaves
// the old tree attached and the new one unlaid.
func (p *Popup) SetContent(w Widget) {
	if p.Content == w {
		return
	}
	if p.window != nil {
		if w != nil && !AdoptWidgetTree(w, p, p.window) {
			return
		}
		if old := p.Content; old != nil {
			DetachWidgetTree(old)
		}
		p.Content = w
		if p.window != nil {
			p.RelayoutOverlay(p.window.Size())
		}
		return
	}
	p.Content = w
}

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
	if !AdoptWidgetTree(p.Content, p, w) {
		return
	}
	p.Content.Layout(contentRect)
	p.pushOverlay(w, p, p.Content)
	if p.ClickThrough && p.DismissOnOutsideClick {
		p.onLeave(w.AddEventFilter(p.clickThroughFilter))
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
	if p.removedExternally(w) {
		p.fireClose(PopupCloseRemoved)
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
	if p.popOverlay(p, p.Content) {
		p.fireClose(reason)
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
	x, y := b.X, b.Y
	if !p.NoClamp {
		x, y = clampPopup(winSize, x, y, size)
	}
	p.RelayoutAt(x, y, size)
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
	if p.Content != nil && !boxChildHidden(p.Content) {
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
		// Escape belongs to the focused content first. The popup is an
		// ancestor, so capture would otherwise close an editor/autocomplete
		// before it ever sees the key.
		if e.Phase() == PhaseCapture {
			return false
		}
		if e.Type() == EventKeyDown && e.Key == KeyEscape {
			p.closeFor(PopupCloseEscape)
			return true
		}
	}
	return false
}

// ConsumesTab never claims Tab; it closes the popup only when this Tab is
// about to move focus OUT of it. Without that, Tab moved focus into the page
// while the popup stayed open but keyboard-dead. Tab between controls inside
// the popup (a form in a popover) leaves it open. Content that wants Tab
// itself (autocomplete) implements TabConsumer and is consulted first.
func (p *Popup) ConsumesTab(shift bool) bool {
	w := p.window
	if w == nil {
		return false
	}
	if next := nextTabTarget(w, shift); next != nil && !p.contains(next) {
		p.close(PopupCloseProgrammatic)
	}
	return false
}

// contains reports whether w is the popup or one of its descendants.
func (p *Popup) contains(w Widget) bool {
	var self Widget = p
	if s := p.Self(); s != nil {
		self = s
	}
	for ; w != nil; w = w.Parent() {
		if w == self {
			return true
		}
	}
	return false
}

// nextTabTarget is the widget Tab (or Shift+Tab) will focus next, using the
// same wrap-around order as Window.FocusNext / FocusPrev.
func nextTabTarget(w *Window, shift bool) Widget {
	list := w.CollectFocusables()
	if len(list) == 0 {
		return nil
	}
	cur := -1
	for i, f := range list {
		if f == w.Focused() {
			cur = i
			break
		}
	}
	step := 1
	if shift {
		step = -1
	}
	if cur < 0 {
		if shift {
			return list[len(list)-1]
		}
		return list[0]
	}
	return list[(cur+step+len(list))%len(list)]
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
	if p.passThrough != nil {
		if hit := p.passThrough(point); hit != nil {
			return hit
		}
	}
	if p.DismissOnOutsideClick && !p.ClickThrough {
		return p
	}
	return nil
}
