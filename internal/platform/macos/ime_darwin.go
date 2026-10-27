//go:build darwin && cgo

package macos

/*
#include <stdint.h>
void quiInstallIME(void* nsWindowPtr, uintptr_t handle);
int quiRawKeyCaptured(void* event);
*/
import "C"

import "unsafe"

// InstallIME hooks the window's content view into NSTextInputClient and the
// raw-keyboard hooks; handle comes back on every Host callback for it.
func InstallIME(nsWindow unsafe.Pointer, handle uintptr) {
	C.quiInstallIME(nsWindow, C.uintptr_t(handle))
}

// RawKeyCapturedHook is the C predicate the cocoa event pump consults before
// re-sending a swallowed Command-chord keyUp (contract.Host.RawKeyCaptured).
func RawKeyCapturedHook() unsafe.Pointer { return unsafe.Pointer(C.quiRawKeyCaptured) }
