package qui

import "github.com/qizhanchan/qui/internal/platform/contract"

// WindowPopup is a borderless, transparent window owned by another window
// of the app: a popup freed from its owner's bounds — a chooser opened
// near the window edge, a list wider than the window — that otherwise
// behaves like an in-window popover. It stays above its owner and moves
// with it, never floats above other apps, and hides while the app is
// inactive. See contract.WindowPopup.
const WindowPopup = contract.WindowPopup

// ErrPopupWindowUnsupported is returned by NewPopupWindow where the
// windowing backend cannot make a popup window (or the owner has no OS
// window, as in a test window). Fall back to an in-window overlay.
var ErrPopupWindowUnsupported = contract.ErrPopupWindowUnsupported

// NewPopupWindow creates a WindowPopup owned by owner, sharing its GPU
// context, and registers it with the app so the Run loop steps it. It
// starts HIDDEN: place it with ShowAt once its content is laid out and
// sized (SetSize). Hide parks it for reuse — creating an OS window and a
// GPU context per popup is too slow for a completion list.
//
// The popup closes with its owner. It does not take keyboard focus unless
// SetPopupFocusable(true) — then ShowAt makes it the key window.
func NewPopupWindow(owner *Window, width, height int) (*Window, error) {
	if owner == nil || owner.plat == nil || owner.platApp == nil {
		return nil, ErrPopupWindowUnsupported
	}
	owner.assertUIThread("NewPopupWindow")
	w, err := newWindowFromConfig(owner.platApp, platformWindowConfig{
		Width:  width,
		Height: height,
		Share:  owner.plat,
		Kind:   WindowPopup,
	})
	if err != nil {
		return nil, err
	}
	if r := owner.renderer; r != nil {
		if _, gl := r.(*GLRenderer); gl {
			w.SetRenderer(NewGLRenderer())
		}
	}
	w.life.owner = owner
	owner.life.owned = append(owner.life.owned, w)
	if a := processApp; a != nil {
		a.windows = append(a.windows, w)
	}
	return w, nil
}

// SetPopupFocusable sets whether a WindowPopup takes keyboard focus when
// shown (a chooser with a filter field) or leaves it with its owner (a
// completion list, a hover card). No-op for other kinds.
func (w *Window) SetPopupFocusable(on bool) {
	if w == nil || w.kind != WindowPopup {
		return
	}
	w.assertUIThread("Window.SetPopupFocusable")
	w.popupFocusable = on
	if w.plat == nil {
		return
	}
	if f, ok := platformOptional[contract.PopupFocuser](w.plat); ok {
		f.SetPopupFocusable(on)
	}
}

// PopupFocusable reports SetPopupFocusable.
func (w *Window) PopupFocusable() bool { return w != nil && w.popupFocusable }

// InvalidateShadow recomputes the native shadow of a transparent window
// (an overlay panel, a popup) from what it drew last. Call it after the
// content changed shape without the window changing size.
func (w *Window) InvalidateShadow() {
	if w == nil || w.plat == nil {
		return
	}
	w.assertUIThread("Window.InvalidateShadow")
	if s, ok := platformOptional[contract.ShadowInvalidator](w.plat); ok {
		s.InvalidateShadow()
	}
}

// ScreenOrigin is the top-left of the window's content area in global
// logical coordinates — where a point at window coordinates (0,0) is on
// screen. ok is false where the platform hides absolute positions.
func (w *Window) ScreenOrigin() (Point, bool) { return w.PositionOK() }

// ToScreen maps a rect in window coordinates to global logical
// coordinates (ok false where positions are unknowable).
func (w *Window) ToScreen(r Rect) (Rect, bool) {
	p, ok := w.PositionOK()
	if !ok {
		return Rect{}, false
	}
	z := w.Zoom()
	if z <= 0 {
		z = 1
	}
	return Rect{X: p.X + r.X*z, Y: p.Y + r.Y*z, W: r.W * z, H: r.H * z}, true
}

// showPopup orders a WindowPopup on screen: attached to its owner as a
// child window (ordered with it), and key only if focusable. Attaching on
// every show rather than once: AppKit drops an ordered-out child from its
// parent's list.
func (w *Window) showPopup() {
	activate := w.popupFocusable
	w.plat.setVisible(true, activate)
	if owner := w.life.owner; owner != nil && owner.plat != nil {
		nativeAttachChild(owner.plat.nativeWindow(), w.plat.nativeWindow())
	}
	if activate {
		w.plat.focus()
	}
}

// hidePopup detaches a WindowPopup from its owner and orders it out.
func (w *Window) hidePopup() {
	if owner := w.life.owner; owner != nil && owner.plat != nil {
		nativeDetachChild(owner.plat.nativeWindow(), w.plat.nativeWindow())
	}
	w.plat.setVisible(false, false)
}

// WorkAreaAt is the usable area (menu bar and dock excluded) of the
// display holding pt, in global logical coordinates — the display of the
// window's center when pt is on none. ok is false where the window's
// backend reports no displays or positions. Popups are kept inside it.
func (w *Window) WorkAreaAt(pt Point) (Rect, bool) {
	if w == nil || w.plat == nil || w.platApp == nil {
		return Rect{}, false
	}
	w.assertUIThread("Window.WorkAreaAt")
	pos, ok := w.PositionOK()
	if !ok {
		return Rect{}, false
	}
	sz := w.WindowSize()
	center := Point{X: pos.X + sz.W/2, Y: pos.Y + sz.H/2}
	var fallback Rect
	for _, h := range w.platApp.monitors() {
		m := monitorFrom(h)
		full := Rect{X: m.Position.X, Y: m.Position.Y, W: m.Size.W, H: m.Size.H}
		if full.W <= 0 || full.H <= 0 {
			full = m.WorkArea
		}
		if full.Contains(pt) && m.WorkArea.W > 0 {
			return m.WorkArea, true
		}
		if fallback.W <= 0 && full.Contains(center) {
			fallback = m.WorkArea
		}
	}
	if fallback.W <= 0 {
		if h := w.platApp.primaryMonitor(); h != nil {
			fallback = monitorFrom(h).WorkArea
		}
	}
	return fallback, fallback.W > 0 && fallback.H > 0
}
