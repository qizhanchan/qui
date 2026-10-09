//go:build darwin && cgo

package qui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework ApplicationServices

#include <stdint.h>

int  quiTapPrepare(void);
void quiTapSetActive(uintptr_t handle, int active, int escCode, int escMods);
void quiTapAck(uint64_t seq);
*/
import "C"

// keyCapture is the CGEventTap backend (keyboard_capture_darwin.m).
var keyCapture keyCaptureBackend = darwinKeyCapture{}

type darwinKeyCapture struct{}

func (darwinKeyCapture) prepare() error {
	if C.quiTapPrepare() == 0 {
		return ErrAccessibilityPermission
	}
	return nil
}

func (darwinKeyCapture) setActive(w *Window, active bool) {
	code, mods := -1, 0
	if k, m := w.raw.chord(); k != KeyUnknown {
		code, mods = cocoaKeyCodeFor(k), nsModsMask(m)
	}
	C.quiTapSetActive(C.uintptr_t(w.nativeID), cBool(active), C.int(code), C.int(mods))
}

// cocoaKeyCodeFor inverts cocoaKeyCodes (-1 when the key has no code).
func cocoaKeyCodeFor(k Key) int {
	if k == KeyEnter {
		return 0x24 // Return, not keypad Enter
	}
	for code, key := range cocoaKeyCodes {
		if key == k {
			return code
		}
	}
	return -1
}

// nsModsMask is mapNSMods' inverse: the 4-bit shift/control/option/command
// mask the C side compares.
func nsModsMask(m Modifiers) int {
	out := 0
	if m&ModShift != 0 {
		out |= 1
	}
	if m&ModControl != 0 {
		out |= 2
	}
	if m&ModAlt != 0 {
		out |= 4
	}
	if m&ModSuper != 0 {
		out |= 8
	}
	return out
}

// quiTapKeyEvent runs on the tap thread: hand the key to the UI thread,
// in order, and return immediately.
//
//export quiTapKeyEvent
func quiTapKeyEvent(id C.uintptr_t, kind, keyCode, repeat C.int, flags C.uint64_t, mods C.int, chars *C.char, seq C.uint64_t) {
	w := lookupIMEWindow(uintptr(id))
	if w == nil {
		C.quiTapAck(seq)
		return
	}
	text := C.GoString(chars)
	k, code, rep, fl, md, s := int(kind), int(keyCode), repeat != 0, uint64(flags), mapNSMods(mods), seq
	w.PostPriorityJob(func() {
		w.injectCapturedKey(k, code, rep, fl, md, text)
		C.quiTapAck(s)
	})
}

// injectCapturedKey delivers a key the tap swallowed, exactly as the
// backends deliver keys: modifier changes as KeyDown/KeyUp of the
// modifier key, a keystroke as KeyDown (+ raw CharEvents) or KeyUp.
func (w *Window) injectCapturedKey(kind, code int, repeat bool, flags uint64, mods Modifiers, text string) {
	// A key queued just before capture ended is still delivered: the
	// system never saw it either. Dispatch routes it like any key.
	key := mapCocoaKey(code)
	switch kind {
	case 2: // flagsChanged
		down, ok := cocoaModifierKeyDown(code, flags)
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
