package qui

// Window-local values: state a package keeps per window rather than per
// process. Page windows (RFC 0005) run on page threads of their own, so a
// package-level registry that every window writes to is a data race; one
// kept here belongs to the thread that owns the window.

// LocalValue returns the value stored under key on w (nil when none).
// UI thread of w.
func (w *Window) LocalValue(key any) any {
	if w == nil {
		return nil
	}
	return w.locals[key]
}

// SetLocalValue stores v under key on w; nil deletes it. Use an unexported
// key type, as with context.Context. UI thread of w.
func (w *Window) SetLocalValue(key, v any) {
	if w == nil {
		return
	}
	if v == nil {
		delete(w.locals, key)
		return
	}
	if w.locals == nil {
		w.locals = map[any]any{}
	}
	w.locals[key] = v
}
