package htmlcss

import (
	"strings"

	"github.com/qizhanchan/qui"
)

// PointerEvent is what an element's pointer and click handlers receive.
type PointerEvent struct {
	// X, Y are window coordinates; LocalX, LocalY are relative to the
	// element's top-left corner.
	X, Y           float32
	LocalX, LocalY float32
	Button         qui.MouseButton
	Mods           qui.Modifiers
	// Clicks is the press count (2 on the second press of a double-click).
	Clicks int
	// Pressed reports whether a mouse button is held (a drag, for a move).
	Pressed bool

	prevented *bool
}

// PreventDefault suppresses the element's built-in behavior for this
// event: a submit button doesn't submit its form, a link doesn't navigate,
// a file / color input doesn't open its picker.
func (p PointerEvent) PreventDefault() {
	if p.prevented != nil {
		*p.prevented = true
	}
}

// DefaultPrevented reports whether a handler called PreventDefault.
func (p PointerEvent) DefaultPrevented() bool { return p.prevented != nil && *p.prevented }

func (e *El) pointerEvent(me qui.MouseEvent, prevented *bool) PointerEvent {
	win := qui.LocalPointToWindow(e, qui.Point{X: me.X, Y: me.Y})
	b := e.Bounds()
	return PointerEvent{
		X: win.X, Y: win.Y,
		LocalX: me.X - b.X, LocalY: me.Y - b.Y,
		Button: me.Button, Mods: me.Mods, Clicks: me.Clicks,
		Pressed:   e.Pressed(),
		prevented: prevented,
	}
}

// SetOnPointerDown / SetOnPointerMove / SetOnPointerUp install raw pointer
// handlers — the building blocks for splitters, custom sliders, marquee
// selection and drawing surfaces. Returning true consumes the event. While
// a button is held the moves keep arriving even when the pointer leaves the
// element (the press captures the pointer), and so does the release.
func (e *El) SetOnPointerDown(fn func(PointerEvent) bool) { e.onPointerDown = fn }

// SetOnPointerMove: see SetOnPointerDown.
func (e *El) SetOnPointerMove(fn func(PointerEvent) bool) { e.onPointerMove = fn }

// SetOnPointerUp: see SetOnPointerDown.
func (e *El) SetOnPointerUp(fn func(PointerEvent) bool) { e.onPointerUp = fn }

// SetOnClickEvent installs a click handler that receives the position,
// button, modifiers and click count, and can PreventDefault the element's
// built-in click behavior. It fires alongside SetOnClick.
func (e *El) SetOnClickEvent(fn func(PointerEvent)) { e.onClickEvent = fn }

// SetOnFileDrop makes the element a drop zone for files dragged in from
// the OS: fn receives their absolute paths. The window-level
// SetOnFileDrop callback no longer sees drops that land here.
func (e *El) SetOnFileDrop(fn func(paths []string)) {
	e.onFileDrop = fn
	e.syncFileDropRetain()
}

// SetOnPaste intercepts Cmd/Ctrl+V while focus is inside the element: fn
// receives the clipboard text and returns true to take over the paste
// (the focused field then doesn't paste it itself).
func (e *El) SetOnPaste(fn func(text string) bool) { e.onPaste = fn }

// syncFileDropRetain keeps the window delivering OS file drops while this
// element is a mounted drop zone.
func (e *El) syncFileDropRetain() {
	want := e.onFileDrop != nil && e.Window() != nil
	switch {
	case want && e.fileDropRelease == nil:
		e.fileDropRelease = e.Window().RetainFileDrops()
	case !want && e.fileDropRelease != nil:
		e.fileDropRelease()
		e.fileDropRelease = nil
	}
}

// AcceptsDrop implements qui.DropAcceptor: OS file drops go to a file
// drop zone, widget drags to an OnDrop / OnDragOver target.
func (e *El) AcceptsDrop(d *qui.DragData) bool {
	if d != nil && len(d.Files) > 0 {
		return e.onFileDrop != nil
	}
	return e.onDrop != nil || e.onDragOver != nil
}

// handlePaste runs the paste hook for a Cmd/Ctrl+V aimed inside e.
func (e *El) handlePaste(ke qui.KeyEvent) bool {
	if e.onPaste == nil || ke.Type() != qui.EventKeyDown || ke.Key != qui.KeyV || !qui.IsCommandMod(ke.Mods) {
		return false
	}
	return e.onPaste(qui.GetClipboardText())
}

// LinkHandler decides what following a link means. Return true when the
// app handled href (an in-app route such as "#/settings"); false lets the
// engine's default run. from is the <a> element — also when its text was
// folded into a paragraph's inline run — so a router can read its id and
// data-* attributes.
type LinkHandler func(href string, from *El) bool

// SetLinkHandler installs the engine's link handler, consulted for every
// <a href> click and every link folded into inline text. Without one, only
// http(s) and mailto links open (in the system handler); fragment, relative
// and other-scheme links do nothing — never hand untrusted content's
// `file:` or custom-scheme URLs to the OS by default.
func (s *StyleEngine) SetLinkHandler(fn LinkHandler) { s.linkHandler = fn }

// followLink resolves a link click on e; from is the element the link
// handler is told about (e itself, or the <a> folded into e's text).
func (e *El) followLink(href string, from *El) {
	if href == "" {
		return
	}
	if e.engine != nil && e.engine.linkHandler != nil && e.engine.linkHandler(href, from) {
		return
	}
	if isExternalLink(href) {
		_ = qui.OpenURL(href)
	}
}

func isExternalLink(href string) bool {
	h := strings.ToLower(strings.TrimSpace(href))
	return strings.HasPrefix(h, "http://") || strings.HasPrefix(h, "https://") || strings.HasPrefix(h, "mailto:")
}
