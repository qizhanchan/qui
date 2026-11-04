package reactive

import (
	"math"

	"github.com/qizhanchan/qui"
)

// portal_window.go — a portal whose content lives in its own OS window
// (PortalOptions.OwnWindow): a qui.WindowPopup owned by the runtime's
// window, sized to the content and placed against the anchor in screen
// coordinates, so the popup is no longer clipped to (or squeezed by) the
// owner window. Where the backend has no popup windows (x11, web, test
// windows) the portal falls back to the in-window overlay unchanged.
//
// The popup behaves like the overlay it replaces: a press in the owner
// window dismisses it (and is swallowed — the menu convention), Escape /
// Enter reach the host from inside the popup, and a popup that took
// keyboard focus also dismisses when it loses it (the user clicked
// another window or switched apps).

// portalWindow drives one OwnWindow portal's popup window.
type portalWindow struct {
	rt    *Runtime
	owner *qui.Window
	win   *qui.Window
	host  *portalHost

	// size is the content's logical size the window was last sized to.
	size qui.Size
	// pos is the window's last global position.
	pos   qui.Point
	shown bool
	// placePending: a place is queued for the end of the render pass.
	placePending bool
	// onShown runs once, right after the first show.
	onShown func()

	dismissed  bool
	swallowUp  bool
	unfilter   func()
	unactivate func()
}

// popupPool parks hidden popup windows per owner for reuse: an OS window
// plus a GPU context per completion list is too slow.
var popupPool = map[*qui.Window][]*qui.Window{}

// popupPoolMax bounds the windows parked per owner.
const popupPoolMax = 4

func acquirePopupWindow(owner *qui.Window) (*qui.Window, error) {
	if list := popupPool[owner]; len(list) > 0 {
		w := list[len(list)-1]
		popupPool[owner] = list[:len(list)-1]
		return w, nil
	}
	w, err := qui.NewPopupWindow(owner, 1, 1)
	if err != nil {
		return nil, err
	}
	if _, hooked := popupPool[owner]; !hooked {
		popupPool[owner] = nil
		// Owned windows close with their owner; forget the parked ones.
		owner.OnClose(func() {
			delete(popupPool, owner)
			delete(popupCapable, owner)
		})
	}
	return w, nil
}

func releasePopupWindow(owner, w *qui.Window) {
	w.Hide()
	w.SetRoot(nil)
	list, ok := popupPool[owner]
	if !ok || len(list) >= popupPoolMax {
		w.Close()
		return
	}
	popupPool[owner] = append(list, w)
}

// newPortalWindow mounts host as the root of a popup window owned by
// owner, or returns nil when the platform has none (the caller then uses
// the overlay stack).
func newPortalWindow(rt *Runtime, owner *qui.Window, host *portalHost) *portalWindow {
	if host.opts.Align == PortalFill {
		return nil
	}
	win, err := acquirePopupWindow(owner)
	if err != nil {
		return nil
	}
	p := &portalWindow{rt: rt, owner: owner, win: win, host: host}
	host.pw = p
	host.Style().Background = qui.Color{}
	win.SetPopupFocusable(!host.opts.KeepFocus)
	if z := owner.Zoom(); z != win.Zoom() {
		win.SetZoom(z)
	}
	win.SetRoot(host)
	if host.opts.OnBackdropClick != nil || host.opts.Modal {
		p.unfilter = owner.AddEventFilter(p.ownerFilter)
	}
	if !host.opts.KeepFocus {
		p.unactivate = win.OnActivate(func(active bool) {
			if !active && p.shown {
				p.dismiss()
			}
		})
	}
	return p
}

// ownerFilter: a press in the owner window dismisses the popup and is
// swallowed with its release, like a press on an overlay's backdrop.
func (p *portalWindow) ownerFilter(ev qui.Event) bool {
	me, ok := ev.(qui.MouseEvent)
	if !ok {
		return false
	}
	switch me.Type() {
	case qui.EventMouseDown:
		p.swallowUp = true
		p.dismiss()
		return true
	case qui.EventMouseUp:
		if p.swallowUp {
			p.swallowUp = false
			return true
		}
	}
	return false
}

// dismiss runs OnBackdropClick once per open popup.
func (p *portalWindow) dismiss() {
	if p.dismissed {
		return
	}
	if fn := p.host.opts.OnBackdropClick; fn != nil {
		p.dismissed = true
		fn()
	}
}

// close hands the window back to the pool.
func (p *portalWindow) close() {
	if p.unfilter != nil {
		p.unfilter()
		p.unfilter = nil
	}
	if p.unactivate != nil {
		p.unactivate()
		p.unactivate = nil
	}
	p.shown = false
	p.host.pw = nil
	releasePopupWindow(p.owner, p.win)
}

// schedulePlace places the popup once the render pass in progress has
// committed — its content is styled only then, and measuring it earlier
// would size the window to unstyled content — or right away outside a
// pass. Several requests in one pass place once.
func (p *portalWindow) schedulePlace() {
	if p.placePending {
		return
	}
	if p.rt != nil && p.rt.inPass {
		p.placePending = true
		p.rt.afterCommit = append(p.rt.afterCommit, func() {
			p.placePending = false
			if p.host.pw == p {
				p.place()
			}
		})
		return
	}
	p.place()
}

// place measures the content, sizes the window to it and puts it next to
// the anchor (or centered over the owner), kept on the anchor's display.
// The first call shows the window.
func (p *portalWindow) place() {
	h := p.host
	kids := h.ChildList()
	if len(kids) == 0 {
		return
	}
	child := kids[0]
	z := p.owner.Zoom()
	if z <= 0 {
		z = 1
	}
	if p.win.Zoom() != z {
		p.win.SetZoom(z)
	}
	ownerPos, ok := p.owner.PositionOK()
	if !ok {
		return
	}
	ownerSize := p.owner.WindowSize()
	ownerScreen := qui.Rect{X: ownerPos.X, Y: ownerPos.Y, W: ownerSize.W, H: ownerSize.H}

	var anchor qui.Rect
	if h.opts.Anchor != nil {
		anchor = h.opts.Anchor()
	}
	h.lastAnchor = anchor
	anchorScreen, _ := p.owner.ToScreen(anchor)

	ref := qui.Point{X: ownerScreen.X + ownerScreen.W/2, Y: ownerScreen.Y + ownerScreen.H/2}
	switch h.opts.Align {
	case PortalAnchored:
		ref = qui.Point{X: anchorScreen.X, Y: anchorScreen.Y}
	case PortalAtPosition:
		ref = qui.Point{X: ownerPos.X + h.opts.X*z, Y: ownerPos.Y + h.opts.Y*z}
	}
	work, ok := p.owner.WorkAreaAt(ref)
	if !ok {
		work = ownerScreen
	}

	// Sizing room: a dialog keeps sizing against the owner (its `90%`
	// means of the window, as in the overlay); a popover may use the
	// whole display.
	room := qui.Size{W: work.W / z, H: work.H / z}
	if h.opts.Align == PortalCenter {
		room = qui.Size{W: ownerSize.W / z, H: ownerSize.H / z}
	}
	sz := qui.MeasureConstrained(child, room)
	scr := qui.Size{W: sz.W * z, H: sz.H * z}

	var r qui.Rect
	switch h.opts.Align {
	case PortalAnchored:
		o := h.opts
		o.Offset *= z
		r = placeAnchored(work, anchorScreen, scr, o)
	case PortalAtPosition:
		r = clampInto(qui.Rect{X: ref.X, Y: ref.Y, W: scr.W, H: scr.H}, work)
	default:
		if p.shown {
			// Shown already: stay where it is (the user may have moved
			// it), only kept on the display as it grows.
			cur := p.win.Position()
			r = clampInto(qui.Rect{X: cur.X, Y: cur.Y, W: scr.W, H: scr.H}, work)
			break
		}
		r = qui.Rect{
			X: ownerScreen.X + (ownerScreen.W-scr.W)/2,
			Y: ownerScreen.Y + (ownerScreen.H-scr.H)/2,
			W: scr.W, H: scr.H,
		}
		r = clampInto(r, work)
	}
	w, hgt := int(math.Ceil(float64(r.W))), int(math.Ceil(float64(r.H)))
	p.size = qui.Size{W: float32(w) / z, H: float32(hgt) / z}
	cur := p.win.WindowSize()
	if int(cur.W) != w || int(cur.H) != hgt {
		p.win.SetSize(w, hgt)
	}
	h.Layout(qui.Rect{W: p.size.W, H: p.size.H})
	p.win.Invalidate()
	pos := qui.Point{X: float32(math.Round(float64(r.X))), Y: float32(math.Round(float64(r.Y)))}
	if !p.shown {
		p.shown = true
		p.dismissed = false
		p.pos = pos
		p.win.ShowAt(int(pos.X), int(pos.Y))
		if fn := p.onShown; fn != nil {
			p.onShown = nil
			fn()
		}
		return
	}
	if pos != p.win.Position() {
		p.pos = pos
		p.win.SetPosition(int(pos.X), int(pos.Y))
	}
}

// clampInto shifts r to lie inside area (its top-left wins when r is the
// larger).
func clampInto(r, area qui.Rect) qui.Rect {
	if r.X+r.W > area.X+area.W {
		r.X = area.X + area.W - r.W
	}
	if r.Y+r.H > area.Y+area.H {
		r.Y = area.Y + area.H - r.H
	}
	if r.X < area.X {
		r.X = area.X
	}
	if r.Y < area.Y {
		r.Y = area.Y
	}
	return r
}

// popupCapable memoizes, per owner, whether OwnWindow portals get a
// window there.
var popupCapable = map[*qui.Window]bool{}

// OwnWindowAvailable reports whether an OwnWindow portal of the runtime
// rendering now (or of window, when given) gets its own popup window, as
// opposed to falling back to the overlay — for content that lays itself
// out differently in the two (a dialog's top margin inside a full-window
// overlay has no place in a window sized to the dialog).
func OwnWindowAvailable(window *qui.Window) bool {
	if window == nil {
		if r := CurrentRuntime(); r != nil {
			window = r.window
		}
	}
	if window == nil {
		return false
	}
	if ok, known := popupCapable[window]; known {
		return ok
	}
	w, err := acquirePopupWindow(window)
	ok := err == nil
	if ok {
		releasePopupWindow(window, w)
	}
	popupCapable[window] = ok
	return ok
}
