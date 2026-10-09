//go:build darwin && cgo

package qui

/*
#include <stdint.h>
*/
import "C"

// Go side of the raw keyboard hooks in ime_darwin.m. Windows are looked
// up through the IME registry: the hooks live on the same content view,
// keyed by the same handle.

//export quiRawKeyboardMode
func quiRawKeyboardMode(id C.uintptr_t, keyCode C.int, mods C.int) C.int {
	w := lookupIMEWindow(uintptr(id))
	if w == nil {
		return 0
	}
	return C.int(w.rawKeyMode(mapCocoaKey(int(keyCode)), mapNSMods(mods)))
}

//export quiRawKeyboardActive
func quiRawKeyboardActive(id C.uintptr_t) C.int {
	w := lookupIMEWindow(uintptr(id))
	if w == nil {
		return 0
	}
	// The escape chord's own keystroke stays out of the input method even
	// though handling its KeyDown may just have turned raw mode off.
	if w.rawKeyboardTarget() == nil && w.raw.escDown == KeyUnknown {
		return 0
	}
	return 1
}

//export quiRawKeyboardChars
func quiRawKeyboardChars(id C.uintptr_t, text *C.char, mods C.int) {
	w := lookupIMEWindow(uintptr(id))
	if w == nil {
		return
	}
	w.onRawChars(C.GoString(text), mapNSMods(mods))
}
