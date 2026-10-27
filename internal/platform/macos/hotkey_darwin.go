//go:build darwin && cgo

package macos

/*
#cgo LDFLAGS: -framework Carbon
#include <stdint.h>
void* quiHotKeyRegister(int id, int code, int mods);
void  quiHotKeyUnregister(void* ref);
*/
import "C"

import "unsafe"

// HotKeyRegister registers a Carbon global hotkey; Host.HotKeyFired(id)
// fires when it is pressed. nil means the chord is taken or invalid.
func HotKeyRegister(id, keyCode, mods int) unsafe.Pointer {
	return C.quiHotKeyRegister(C.int(id), C.int(keyCode), C.int(mods))
}

// HotKeyUnregister releases a hotkey from HotKeyRegister.
func HotKeyUnregister(ref unsafe.Pointer) { C.quiHotKeyUnregister(ref) }
