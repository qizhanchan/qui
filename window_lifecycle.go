package qui

import (
	"errors"
	"slices"
)

// ErrWindowOwnershipUnsupported is returned by SetOwner / ShowAsSheet on a
// platform without native child windows or sheets.
var ErrWindowOwnershipUnsupported = errors.New("qui: owned windows / sheets are not supported on this platform")

// windowLifecycle is the per-window state behind OnCloseRequest, OnActivate,
// OnMove, OnMinimize and window ownership.
type windowLifecycle struct {
	closeRequest []closeRequestEntry
	nextID       int64
	// forceClose skips the OnCloseRequest veto for the close in progress
	// (Close, a parent closing, SIGINT/SIGTERM).
	forceClose bool

	onActivate []func(active bool)
	onMove     []func(x, y int)
	onMinimize []func(minimized bool)
	lastPos    Point
	posKnown   bool
	minimized  bool

	owner    *Window
	owned    []*Window
	asSheet  bool
	sheetFor *Window
}

type closeRequestEntry struct {
	id int64
	fn func() bool
}

// OnCloseRequest registers fn to decide whether a close the user asked for
// (the title-bar button, Cmd+W wired to RequestClose, App.Quit) goes ahead.
// Returning false vetoes it and the window stays open — the "save changes?"
// pattern: veto, show a dialog, and call Close from its Discard / Save
// handler. Handlers run on the UI thread in registration order; the first
// veto wins. Returns a remover.
//
// Close (and a parent window or a SIGINT/SIGTERM shutdown closing this one)
// is not vetoable. OnClose callbacks only run once a close is final.
func (w *Window) OnCloseRequest(fn func() bool) (remove func()) {
	if w == nil || fn == nil {
		return func() {}
	}
	w.assertUIThread("Window.OnCloseRequest")
	w.life.nextID++
	id := w.life.nextID
	w.life.closeRequest = append(w.life.closeRequest, closeRequestEntry{id: id, fn: fn})
	return func() {
		for i, e := range w.life.closeRequest {
			if e.id == id {
				w.life.closeRequest = append(w.life.closeRequest[:i:i], w.life.closeRequest[i+1:]...)
				return
			}
		}
	}
}

// RequestClose asks to close the window exactly as the title-bar close
// button does: OnCloseRequest handlers may veto it. The close happens on
// the next main-loop iteration.
func (w *Window) RequestClose() {
	if w == nil {
		return
	}
	w.assertUIThread("Window.RequestClose")
	if w.plat != nil {
		w.plat.setShouldClose(true)
	}
}

// Close closes the window on the next main-loop iteration without
// consulting OnCloseRequest — the call a "Discard" button makes after a
// veto. OnClose callbacks still run.
func (w *Window) Close() {
	if w == nil {
		return
	}
	w.assertUIThread("Window.Close")
	w.life.forceClose = true
	if w.plat != nil {
		w.plat.setShouldClose(true)
	}
}

// closeVetoed runs the OnCloseRequest handlers for a pending close and
// reports whether one refused. A vetoed window has its close flag cleared.
func (w *Window) closeVetoed() bool {
	if w.life.forceClose || len(w.life.closeRequest) == 0 {
		return false
	}
	for _, e := range append([]closeRequestEntry(nil), w.life.closeRequest...) {
		if !e.fn() {
			if w.plat != nil {
				w.plat.setShouldClose(false)
			}
			return true
		}
	}
	return false
}

// OnActivate registers fn to run when the window becomes (true) or stops
// being (false) the key window — the OS-level focus, not widget focus. Use
// it to dim a custom title bar or pause work while in the background.
func (w *Window) OnActivate(fn func(active bool)) {
	if w == nil || fn == nil {
		return
	}
	w.assertUIThread("Window.OnActivate")
	w.life.onActivate = append(w.life.onActivate, fn)
}

// OnMove registers fn to run when the window's top-left moves, in global
// logical coordinates. Never fires where the platform has no absolute
// position (Wayland).
func (w *Window) OnMove(fn func(x, y int)) {
	if w == nil || fn == nil {
		return
	}
	w.assertUIThread("Window.OnMove")
	if len(w.life.onMove) == 0 && w.plat != nil {
		if x, y, ok := w.plat.pos(); ok {
			w.life.lastPos, w.life.posKnown = Point{X: float32(x), Y: float32(y)}, true
		}
	}
	w.life.onMove = append(w.life.onMove, fn)
}

// OnMinimize registers fn to run when the window is minimized (true) or
// restored from the Dock / taskbar (false).
func (w *Window) OnMinimize(fn func(minimized bool)) {
	if w == nil || fn == nil {
		return
	}
	w.assertUIThread("Window.OnMinimize")
	if len(w.life.onMinimize) == 0 {
		w.life.minimized = w.IsMinimized()
	}
	w.life.onMinimize = append(w.life.onMinimize, fn)
}

// IsMinimized reports whether the window is currently minimized.
func (w *Window) IsMinimized() bool {
	if w == nil || w.plat == nil {
		return false
	}
	if p, ok := w.plat.(interface{ isIconified() bool }); ok {
		return p.isIconified()
	}
	return nativeWindowMinimized(w.plat.nativeWindow())
}

func (w *Window) noteActivation(active bool) {
	for _, fn := range slices.Clone(w.life.onActivate) {
		fn(active)
	}
}

// pollLifecycle fires OnMove / OnMinimize. It runs from App.RunStep for
// every live window — not from Step, which a minimized window whose display
// link has stopped may never reach.
func (w *Window) pollLifecycle() {
	if w == nil || w.plat == nil {
		return
	}
	if len(w.life.onMove) > 0 {
		if x, y, ok := w.plat.pos(); ok {
			p := Point{X: float32(x), Y: float32(y)}
			if !w.life.posKnown || p != w.life.lastPos {
				w.life.lastPos, w.life.posKnown = p, true
				for _, fn := range slices.Clone(w.life.onMove) {
					fn(x, y)
				}
			}
		}
	}
	if len(w.life.onMinimize) > 0 {
		if m := w.IsMinimized(); m != w.life.minimized {
			w.life.minimized = m
			for _, fn := range slices.Clone(w.life.onMinimize) {
				fn(m)
			}
		}
	}
}

// SetOwner makes w an owned (child) window of owner: on macOS it stays
// above owner, moves with it and minimizes with it. On every platform an
// owned window closes when its owner closes (not vetoable). Pass nil to
// release it. Returns ErrWindowOwnershipUnsupported where the platform
// can't attach native child windows; the close-with-owner contract still
// holds in that case.
func (w *Window) SetOwner(owner *Window) error {
	if w == nil || owner == w {
		return nil
	}
	w.assertUIThread("Window.SetOwner")
	if prev := w.life.owner; prev != nil {
		prev.life.owned = removeWindow(prev.life.owned, w)
		if w.plat != nil && prev.plat != nil {
			nativeDetachChild(prev.plat.nativeWindow(), w.plat.nativeWindow())
		}
	}
	w.life.owner = owner
	if owner == nil {
		return nil
	}
	owner.life.owned = append(owner.life.owned, w)
	if w.plat == nil || owner.plat == nil {
		return nil
	}
	if !nativeAttachChild(owner.plat.nativeWindow(), w.plat.nativeWindow()) {
		return ErrWindowOwnershipUnsupported
	}
	return nil
}

// Owner returns the window set by SetOwner or ShowAsSheet, or nil.
func (w *Window) Owner() *Window {
	if w == nil {
		return nil
	}
	return w.life.owner
}

// ShowAsSheet presents w as a document-modal sheet attached to parent's
// title bar (macOS): parent stops taking input until the sheet closes.
// Closing w (Close / RequestClose / App.CloseWindow) ends the sheet. Returns
// ErrWindowOwnershipUnsupported elsewhere — show an in-window Dialog there.
func (w *Window) ShowAsSheet(parent *Window) error {
	if w == nil || parent == nil || parent == w {
		return nil
	}
	w.assertUIThread("Window.ShowAsSheet")
	if w.plat == nil || parent.plat == nil {
		return ErrWindowOwnershipUnsupported
	}
	if !nativeBeginSheet(parent.plat.nativeWindow(), w.plat.nativeWindow()) {
		return ErrWindowOwnershipUnsupported
	}
	w.life.owner = parent
	w.life.asSheet = true
	parent.life.owned = append(parent.life.owned, w)
	return nil
}

// releaseOwnership runs as w is destroyed: owned windows are force-closed,
// a sheet is ended and w leaves its owner's list.
func (w *Window) releaseOwnership() {
	for _, child := range append([]*Window(nil), w.life.owned...) {
		child.life.owner = nil
		if child.plat != nil {
			child.life.forceClose = true
			child.plat.setShouldClose(true)
		}
	}
	w.life.owned = nil
	if owner := w.life.owner; owner != nil {
		owner.life.owned = removeWindow(owner.life.owned, w)
		if w.plat != nil && owner.plat != nil {
			if w.life.asSheet {
				nativeEndSheet(owner.plat.nativeWindow(), w.plat.nativeWindow())
			} else {
				nativeDetachChild(owner.plat.nativeWindow(), w.plat.nativeWindow())
			}
		}
		w.life.owner = nil
		w.life.asSheet = false
	}
}

func removeWindow(list []*Window, w *Window) []*Window {
	for i, x := range list {
		if x == w {
			return append(list[:i:i], list[i+1:]...)
		}
	}
	return list
}
