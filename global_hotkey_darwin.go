//go:build darwin && cgo

package qui

/*
#cgo LDFLAGS: -framework Carbon

#include <stdint.h>

void* quiHotKeyRegister(int id, int code, int mods);
void  quiHotKeyUnregister(void* ref);
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func registerPlatformHotKey(id int, key Key, mods Modifiers) (func(), error) {
	code := cocoaKeyCodeFor(key)
	if code < 0 {
		return nil, fmt.Errorf("qui: global hotkey: key %v has no macOS key code", key)
	}
	ref := C.quiHotKeyRegister(C.int(id), C.int(code), C.int(nsModsMask(mods)))
	if ref == nil {
		return nil, fmt.Errorf("qui: global hotkey: the chord is taken or invalid")
	}
	return func() { C.quiHotKeyUnregister(unsafe.Pointer(ref)) }, nil
}

//export quiHotKeyFired
func quiHotKeyFired(id C.int) { fireHotKey(int(id)) }
