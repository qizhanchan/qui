//go:build !darwin || !cgo

package media

import "unsafe"

// PushCVPixelBuffer is macOS-only; elsewhere it returns ErrNotSupported.
// Use PushRGBA.
func (v *FrameView) PushCVPixelBuffer(pb unsafe.Pointer) error { return ErrNotSupported }
