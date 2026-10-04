//go:build !darwin || !cgo

package media

// Non-darwin / no-cgo fallback. No backend is registered, so every
// public entry point returns ErrNotSupported. The package still
// compiles and the rest of qui's test suite still passes on Linux
// CI without a media stack.
//
// A real Linux backend would replace this file with backend_linux.go
// + backend_linux_nvdec.c / backend_linux_nvdec.go that registers
// itself via setBackend in init(), and adjust the build tag here to
// exclude that case.

func init() {
	// Intentionally leave currentBackend == nil. activeBackend()
	// will return nil and AudioPlayer / VideoView will surface
	// ErrNotSupported from Open.
}
