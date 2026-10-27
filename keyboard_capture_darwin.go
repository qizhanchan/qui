//go:build darwin && cgo

package qui

import (
	"github.com/qizhanchan/qui/internal/platform/darwinkeys"
	"github.com/qizhanchan/qui/internal/platform/macos"
)

// keyCapture is the CGEventTap backend (internal/platform/macos).
var keyCapture keyCaptureBackend = darwinKeyCapture{}

type darwinKeyCapture struct{}

func (darwinKeyCapture) prepare() error {
	if !macos.TapPrepare() {
		return ErrAccessibilityPermission
	}
	return nil
}

func (darwinKeyCapture) setActive(w *Window, active bool) {
	code, mods := -1, 0
	if k, m := w.raw.chord(); k != KeyUnknown {
		code, mods = darwinkeys.CodeFor(k), darwinkeys.ModsMask(m)
	}
	macos.TapSetActive(w.nativeID, active, code, mods)
}

// injectCapturedKey delivers a key the tap swallowed, exactly as the
// backends deliver keys: modifier changes as KeyDown/KeyUp of the
// modifier key, a keystroke as KeyDown (+ raw CharEvents) or KeyUp.
func (w *Window) injectCapturedKey(kind, code int, repeat bool, flags uint64, mods Modifiers, text string) {
	// A key queued just before capture ended is still delivered: the
	// system never saw it either. Dispatch routes it like any key.
	key := darwinkeys.Map(code)
	switch kind {
	case 2: // flagsChanged
		down, ok := darwinkeys.ModifierKeyDown(code, flags)
		if !ok {
			return
		}
		w.noteCapturedKey(code, key, down)
		w.onKey(key, code, down, false, mods)
	case 0:
		w.noteCapturedKey(code, key, true)
		w.onKey(key, code, true, repeat, mods)
		if text != "" {
			w.onRawChars(text, mods)
		}
	case 1:
		w.noteCapturedKey(code, key, false)
		w.onKey(key, code, false, false, mods)
	}
}
