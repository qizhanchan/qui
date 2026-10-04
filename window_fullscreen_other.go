//go:build !darwin || !cgo

package qui

// SetFullscreen switches the window to monitor-fullscreen on the given
// monitor, or back to windowed mode when m is the zero Monitor. The
// windowed position and size are saved on entry and restored on exit.
//
// This is borderless-window fullscreen (a window covering the display).
// The darwin build overrides it with native NSWindow fullscreen so system
// controls stay reachable.
func (w *Window) SetFullscreen(m Monitor) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetFullscreen")
	if w.plat == nil {
		return
	}
	if m.plat == nil {
		// Return to windowed mode using the geometry saved on entry.
		if w.savedWindowed.W <= 0 || w.savedWindowed.H <= 0 {
			return // already windowed
		}
		r := w.savedWindowed
		w.savedWindowed = Rect{}
		w.plat.setFullscreen(nil, int(r.X), int(r.Y), int(r.W), int(r.H), 0)
		return
	}
	if w.savedWindowed.W > 0 { // already fullscreen — ignore re-entry
		return
	}
	px, py, ok := w.plat.pos()
	if !ok {
		// No absolute position to restore to (Wayland). Fullscreen would
		// be a one-way trip, so refuse rather than trap the window.
		return
	}
	sz := w.WindowSize() // window geometry to restore, not content extent
	w.savedWindowed = Rect{X: float32(px), Y: float32(py), W: sz.W, H: sz.H}
	mw, mh, hz := m.plat.videoMode()
	if mw <= 0 || mh <= 0 {
		w.savedWindowed = Rect{}
		return
	}
	w.plat.setFullscreen(m.plat, 0, 0, mw, mh, hz)
}

// IsFullscreen reports whether the window is currently in fullscreen mode.
func (w *Window) IsFullscreen() bool {
	if w == nil {
		return false
	}
	w.assertUIThread("Window.IsFullscreen")
	return w.savedWindowed.W > 0 && w.savedWindowed.H > 0
}
