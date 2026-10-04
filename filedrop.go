package qui

// OS → app file drop. The platform reports a list of absolute paths when
// the user drags files from Finder / Explorer / Nautilus onto a window.
// Drop coordinates come from the current cursor position because not
// every platform includes them in the drop notification — the backend
// resolves that (see platformHandler.onFileDrop).

// SetOnFileDrop registers a callback invoked when the user drops one or
// more files onto the window. `paths` are absolute OS paths in the order
// reported by the OS. `x`, `y` are window-space logical pixels at the drop
// location.
//
// Replaces any previously registered callback. Pass nil to clear.
func (w *Window) SetOnFileDrop(fn func(paths []string, x, y float32)) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetOnFileDrop")
	w.onDrop = fn
	if dropper, ok := w.plat.(platformDropTarget); ok {
		dropper.setDropEnabled(fn != nil)
	}
}

// platformDropTarget is an optional platform capability: a backend that
// can receive OS file drops implements it. Optional rather than part of
// platformWindow because a backend may legitimately not support drops
// (a headless one never will), and the engine's behavior without them is
// simply "no drops arrive" — nothing to degrade.
type platformDropTarget interface {
	setDropEnabled(enabled bool)
}
