//go:build darwin && cgo

package qui

import "github.com/qizhanchan/qui/internal/platform/macos"

// SetFullscreen enters native macOS fullscreen on the given monitor, or
// exits fullscreen when m is the zero Monitor. macOS animates into a
// dedicated fullscreen Space; the menu bar and traffic-light controls
// auto-hide and reveal on hover at the top edge.
//
// The window enters fullscreen on whichever display it currently occupies;
// when a specific monitor is requested we first nudge the window onto it so
// the Space lands on the right screen.
func (w *Window) SetFullscreen(m Monitor) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetFullscreen")
	if w.plat == nil {
		return
	}
	ns := w.plat.nativeWindow()
	if ns == nil {
		return
	}
	on := m.plat != nil
	if on && !w.IsFullscreen() && w.CurrentMonitor().Position != m.Position {
		// The window sits on another display (monitors never overlap, so a
		// different top-left means a different monitor): nudge it onto the
		// requested one first, so the fullscreen Space lands there. This must
		// also run when the TARGET is the primary display at (0,0) — a window
		// that opened on a secondary monitor would otherwise go fullscreen
		// right where it is.
		w.plat.setPos(int(m.Position.X)+40, int(m.Position.Y)+40)
	}
	macos.WindowSetFullscreen(ns, on)
}

// IsFullscreen reports whether the window is in a native fullscreen Space.
// It reads the live NSWindow style mask, so it stays correct even when the
// user toggled fullscreen via the green traffic-light button or Ctrl+Cmd+F
// rather than SetFullscreen.
func (w *Window) IsFullscreen() bool {
	if w == nil {
		return false
	}
	w.assertUIThread("Window.IsFullscreen")
	if w.plat == nil {
		return false
	}
	ns := w.plat.nativeWindow()
	if ns == nil {
		return false
	}
	return macos.WindowIsFullscreen(ns)
}
