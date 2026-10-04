package qui

import "time"

// Client-side title bars.
//
// A browser puts its tab strip where the window title would be: the
// content view extends to the top of the window, the app paints that strip
// itself, and the platform's own window controls (traffic lights on macOS)
// float above it. Everything else about the window stays native — resize
// edges, double-click-to-zoom, snapping, fullscreen.
//
// Three primitives are enough to build that, and all three are things only
// the OS can answer:
//
//   - SetTitlebarStyle(TitlebarOverlay) — extend the content under a
//     transparent title bar.
//   - TitlebarInsets — how much room the system controls occupy, so the
//     app's own chrome doesn't render underneath them. Never hardcode 78px:
//     the value differs per platform, per OS version and per window state.
//   - BeginWindowDrag — hand an in-progress mouse press to the window
//     manager so dragging the strip moves the window. This must be a
//     native handoff rather than "track the mouse and call SetPosition":
//     only the platform's own drag gives snapping/tiling, and on Wayland a
//     client cannot move itself at all (it asks the compositor, with the
//     serial of the press that started it).
//
// Widget-level opt-in lives above this: htmlcss maps CSS `app-region:
// drag` / `no-drag` onto BeginWindowDrag, so a stylesheet decides which
// parts of the strip move the window. See examples/chrome-tabs.
//
// Platform support: macOS (both the glfw and cocoa backends, since this
// reaches the NSWindow through the platform seam's nativeWindow). Elsewhere
// SetTitlebarStyle reports false and the app keeps its native title bar —
// the tab strip then simply sits below it, which is a working UI rather
// than a broken one. Check the return value (or TitlebarInsets) instead of
// assuming.

// TitlebarStyle selects how the window's title area is drawn.
type TitlebarStyle int

const (
	// TitlebarNative keeps the system title bar above the content area.
	// The default for every window.
	TitlebarNative TitlebarStyle = iota
	// TitlebarOverlay makes the content area span the full window height
	// and the system title bar transparent, so the widget tree paints the
	// title area itself. System window controls keep working on top of it;
	// use TitlebarInsets to leave room for them.
	TitlebarOverlay
)

// SetTitlebarStyle applies s to this window, reporting whether the platform
// could honor it. False means the window still has its native title bar and
// the app should lay out below it.
//
// Safe to call at any time; call it before the first frame to avoid a
// visible reflow. The requested style is remembered even when it could not
// be applied, so TitlebarStyle reflects the request and layout code can be
// written against one value.
func (w *Window) SetTitlebarStyle(s TitlebarStyle) bool {
	if w == nil {
		return false
	}
	w.assertUIThread("Window.SetTitlebarStyle")
	w.titlebarStyle = s
	if w.plat == nil {
		return false
	}
	ns := w.plat.nativeWindow()
	if ns == nil {
		return false
	}
	ok := platformSetTitlebarStyle(ns, s)
	if ok {
		// The content view grows into the title area, which changes the
		// layout viewport; the platform resize notification does the rest.
		w.InvalidateLayout()
	}
	return ok
}

// TitlebarStyle returns the style last requested via SetTitlebarStyle.
func (w *Window) TitlebarStyle() TitlebarStyle {
	if w == nil {
		return TitlebarNative
	}
	w.assertUIThread("Window.TitlebarStyle")
	return w.titlebarStyle
}

// TitlebarInsets reports the space, in logical points, that system window
// controls occupy inside the content area — the region an overlay title bar
// must keep clear.
//
// Top is the height of the platform's title bar (the strip whose frame
// still handles resize and double-click-to-zoom), Left and Right the width
// reserved by window buttons on each side: on macOS the traffic lights make
// Left non-zero and Right zero, on Windows it would be the reverse.
//
// All zero for a window with a native title bar (nothing overlaps the
// content) and for test windows, so `padding-left: insets.Left` is always
// the right thing to write.
func (w *Window) TitlebarInsets() Insets {
	if w == nil {
		return Insets{}
	}
	w.assertUIThread("Window.TitlebarInsets")
	if w.plat == nil || w.titlebarStyle == TitlebarNative {
		return Insets{}
	}
	ns := w.plat.nativeWindow()
	if ns == nil {
		return Insets{}
	}
	return platformTitlebarInsets(ns)
}

// BeginWindowDrag hands the mouse press being handled right now to the
// window manager, which then moves the window until the button is released.
// Call it from a MouseDown handler on whatever the app treats as its title
// bar; it reports whether the platform took over.
//
// The platform's drag loop swallows the matching MouseUp, so the release is
// re-synthesized once it returns — otherwise the widget that started the
// drag would stay visually pressed.
func (w *Window) BeginWindowDrag() bool {
	if w == nil {
		return false
	}
	w.assertUIThread("Window.BeginWindowDrag")
	if w.plat == nil {
		return false
	}
	ns := w.plat.nativeWindow()
	if ns == nil {
		return false
	}
	if !platformBeginWindowDrag(ns) {
		return false
	}
	// The platform's drag loop ran to completion inside the call above and
	// ate the release, so nothing would ever un-press the widget that
	// started the drag. Synthesize the release on the next frame — flagged
	// AfterDrag, the same way a widget drag's release is, so moving the
	// window by its title bar doesn't also read as a click on it.
	mods := w.lastMods
	w.PostJob(func() {
		x, y := w.cursorPos()
		w.dispatch(MouseEvent{
			baseEvent: baseEvent{shared: &eventState{}},
			eventType: EventMouseUp,
			When:      time.Now(),
			X:         x,
			Y:         y,
			Button:    MouseButtonLeft,
			Mods:      mods,
			AfterDrag: true,
		})
	})
	return true
}

// ToggleMaximize zooms the window to fill its screen's work area, or
// restores it if already zoomed — the standard double-click-on-the-title-bar
// behavior, for apps that draw their own title bar and want to offer it.
// Reports whether the platform performed it.
//
// macOS's own window drag already implements double-click-to-zoom, so this
// is for explicit affordances (a maximize button in a custom strip).
func (w *Window) ToggleMaximize() bool {
	if w == nil {
		return false
	}
	w.assertUIThread("Window.ToggleMaximize")
	if w.plat == nil {
		return false
	}
	ns := w.plat.nativeWindow()
	if ns == nil {
		return false
	}
	return platformToggleMaximize(ns)
}
