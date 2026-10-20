//go:build !darwin || !cgo

package qui

import "unsafe"

// keyCapture is nil: no system key capture off macOS.
var keyCapture keyCaptureBackend

// rawKeyCapturedHook: no AppKit event pump to hand a predicate to.
func rawKeyCapturedHook() unsafe.Pointer { return nil }
