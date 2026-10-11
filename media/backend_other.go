//go:build !darwin || !cgo

package media

// Non-darwin / no-cgo fallback. No *native* backend is registered here, so
// the default ("auto") resolves to the pure-Go backend in
// backend_golang.go. On platforms that add a native backend, register it as
// "native" from that file's init().

func init() {}
