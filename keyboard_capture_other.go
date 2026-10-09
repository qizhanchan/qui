//go:build !darwin || !cgo

package qui

// keyCapture is nil: no system key capture off macOS.
var keyCapture keyCaptureBackend
