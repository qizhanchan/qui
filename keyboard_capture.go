package qui

import "errors"

// System shortcut capture.
//
// Raw keyboard mode (keyboard_raw.go) gets every key the application is
// sent — but some never are: macOS handles Cmd+Tab, Cmd+Space, Ctrl+arrows
// (Spaces), Mission Control and friends before any app sees them. A remote
// desktop wants those for the remote machine. SetSystemKeyCapture installs
// a system event tap (macOS: a CGEventTap, which needs the Accessibility
// permission) that, while it is ACTIVE, takes every keystroke before the
// system does and delivers it to the raw target as ordinary KeyEvents /
// raw CharEvents.
//
// It is active only while all of these hold, and switches off by itself
// the moment one stops holding:
//
//   - capture is enabled on the window (off by default);
//   - the window is the key window (it has OS keyboard focus);
//   - raw keyboard mode is active (a RawKeyboardTarget wants it).
//
// The raw escape chord is never captured, so the user can always get out.
// As a safety valve the tap also stops swallowing keys if the UI thread
// has not consumed captured keys for a second (a hung app must not hold
// the keyboard hostage).

// ErrSystemKeyCaptureUnsupported is returned where no system key capture
// exists (everything but macOS today).
var ErrSystemKeyCaptureUnsupported = errors.New("qui: system key capture is not supported on this platform")

// ErrAccessibilityPermission is returned when the OS refuses the event tap.
// On macOS: grant the app (or the terminal running it) access under System
// Settings › Privacy & Security › Accessibility, then enable again.
var ErrAccessibilityPermission = errors.New("qui: system key capture needs the Accessibility permission")

// keyCaptureState is a window's system-capture bookkeeping.
type keyCaptureState struct {
	enabled bool
	active  bool
	// osFocused mirrors the platform's key-window state (onFocus).
	osFocused bool
	// down tracks keys pressed through the tap, so turning capture off
	// mid-press releases them in the target instead of leaving them stuck.
	down map[int]Key
}

// keyCaptureBackend is the platform half. Swapped in tests.
type keyCaptureBackend interface {
	// prepare checks permission and creates the tap; called on enable.
	prepare() error
	// setActive turns swallowing on or off for w.
	setActive(w *Window, active bool)
}

// SetSystemKeyCapture enables or disables system shortcut capture for the
// window (see the comment at the top of keyboard_capture.go). Enabling
// returns ErrAccessibilityPermission if the OS refuses and
// ErrSystemKeyCaptureUnsupported off macOS; capture then stays off.
// UI thread.
func (w *Window) SetSystemKeyCapture(on bool) error {
	if w == nil {
		return nil
	}
	w.assertUIThread("Window.SetSystemKeyCapture")
	if on {
		if keyCapture == nil {
			return ErrSystemKeyCaptureUnsupported
		}
		if err := keyCapture.prepare(); err != nil {
			return err
		}
	}
	w.keyCapture.enabled = on
	w.syncKeyCapture()
	return nil
}

// SystemKeyCaptureActive reports whether the system tap is currently
// taking keys for this window. UI thread.
func (w *Window) SystemKeyCaptureActive() bool { return w != nil && w.keyCapture.active }

// syncKeyCapture recomputes whether capture should be active and tells the
// platform on a change. Cheap; called on focus changes, key events and
// every Step.
func (w *Window) syncKeyCapture() {
	if w == nil || keyCapture == nil {
		return
	}
	want := w.keyCapture.enabled && w.keyCapture.osFocused && w.rawKeyboardTarget() != nil
	if want == w.keyCapture.active {
		return
	}
	w.keyCapture.active = want
	keyCapture.setActive(w, want)
	if !want {
		w.releaseCapturedKeys()
	}
}

// releaseCapturedKeys sends KeyUp for every key the tap delivered a
// KeyDown for and no KeyUp yet.
func (w *Window) releaseCapturedKeys() {
	held := w.keyCapture.down
	w.keyCapture.down = nil
	for code, k := range held {
		w.onKey(k, code, false, false, w.lastMods)
	}
}

// noteCapturedKey records a key delivered through the tap.
func (w *Window) noteCapturedKey(code int, k Key, down bool) {
	if down {
		if w.keyCapture.down == nil {
			w.keyCapture.down = map[int]Key{}
		}
		w.keyCapture.down[code] = k
		return
	}
	delete(w.keyCapture.down, code)
}
