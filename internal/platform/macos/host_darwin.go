//go:build darwin && cgo

package macos

// #cgo CFLAGS: -x objective-c -fobjc-arc
// #cgo LDFLAGS: -framework Cocoa
// #include <stdint.h>
// #include <stdlib.h>
import "C"

import (
	"unsafe"
)

// Host receives the callbacks AppKit raises. Window-scoped callbacks carry
// the handle the engine passed to InstallIME; modifier masks are AppKit's
// raw flags (see internal/platform/darwinkeys).
type Host interface {
	MenuAction(id int)
	HotKeyFired(id int)

	IMESetPreedit(handle uintptr, text string, cursor int)
	// IMECaretRect returns the caret in view (window-logical) coordinates.
	IMECaretRect(handle uintptr) (x, y, w, h float64)

	RawKeyboardMode(handle uintptr, keyCode, mods int) int
	RawKeyboardActive(handle uintptr) bool
	RawKeyboardChars(handle uintptr, text string, mods int)
	AcceleratorClaims(handle uintptr, keyCode, mods int) bool

	// TapKeyEvent runs on the event-tap thread. The host must call TapAck
	// with seq once the key has been delivered (or dropped).
	TapKeyEvent(handle uintptr, kind, keyCode int, repeat bool, flags uint64, mods int, chars string, seq uint64)
}

var host Host

// SetHost installs the engine's callback receiver. Call it once, before
// any window is created.
func SetHost(h Host) { host = h }

//export quiMenuActionCallback
func quiMenuActionCallback(id C.int) {
	if host != nil {
		host.MenuAction(int(id))
	}
}

//export quiHotKeyFired
func quiHotKeyFired(id C.int) {
	if host != nil {
		host.HotKeyFired(int(id))
	}
}

//export quiIMESetPreedit
func quiIMESetPreedit(id C.uintptr_t, text *C.char, cursor C.int) {
	if host != nil {
		host.IMESetPreedit(uintptr(id), C.GoString(text), int(cursor))
	}
}

//export quiIMECaretRect
func quiIMECaretRect(id C.uintptr_t, outX, outY, outW, outH *C.double) {
	*outX, *outY, *outW, *outH = 0, 0, 0, 0
	if host == nil {
		return
	}
	x, y, w, h := host.IMECaretRect(uintptr(id))
	*outX, *outY, *outW, *outH = C.double(x), C.double(y), C.double(w), C.double(h)
}

//export quiRawKeyboardMode
func quiRawKeyboardMode(id C.uintptr_t, keyCode C.int, mods C.int) C.int {
	if host == nil {
		return 0
	}
	return C.int(host.RawKeyboardMode(uintptr(id), int(keyCode), int(mods)))
}

//export quiRawKeyboardActive
func quiRawKeyboardActive(id C.uintptr_t) C.int {
	if host == nil || !host.RawKeyboardActive(uintptr(id)) {
		return 0
	}
	return 1
}

//export quiRawKeyboardChars
func quiRawKeyboardChars(id C.uintptr_t, text *C.char, mods C.int) {
	if host != nil {
		host.RawKeyboardChars(uintptr(id), C.GoString(text), int(mods))
	}
}

//export quiAcceleratorClaims
func quiAcceleratorClaims(id C.uintptr_t, keyCode C.int, mods C.int) C.int {
	if host == nil || !host.AcceleratorClaims(uintptr(id), int(keyCode), int(mods)) {
		return 0
	}
	return 1
}

//export quiTapKeyEvent
func quiTapKeyEvent(id C.uintptr_t, kind, keyCode, repeat C.int, flags C.uint64_t, mods C.int, chars *C.char, seq C.uint64_t) {
	if host == nil {
		TapAck(uint64(seq))
		return
	}
	host.TapKeyEvent(uintptr(id), int(kind), int(keyCode), repeat != 0, uint64(flags), int(mods), C.GoString(chars), uint64(seq))
}

func cBool(v bool) C.int {
	if v {
		return 1
	}
	return 0
}

// cStr returns a C string and the function that frees it.
func cStr(s string) (*C.char, func()) {
	c := C.CString(s)
	return c, func() { C.free(unsafe.Pointer(c)) }
}
