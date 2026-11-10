//go:build !darwin || !cgo

package render

import (
	"image"
	"unsafe"

	"github.com/qizhanchan/qui/internal/wgpu"
)

// Frames shared with the GPU need IOSurface (macOS); elsewhere the frame
// is uploaded.
const sharedFramesAvailable = false

func presentsWithTransaction(SurfaceTarget) bool { return false }

type sharedFrame struct {
	sharedTex
	w, h int
}

func newSharedFrame(*wgpu.Device, int, int) *sharedFrame { return nil }
func (s *sharedFrame) image() *image.RGBA                { return nil }
func (s *sharedFrame) lock()                             {}
func (s *sharedFrame) unlock()                           {}
func (s *sharedFrame) release()                          {}

type pixelBufferCache struct{}

type pbEntry struct {
	sharedTex
	bg *wgpu.BindGroup
}

func (c *pixelBufferCache) get(*wgpu.Device, *webgpuPipes, unsafe.Pointer) *pbEntry { return nil }
func (c *pixelBufferCache) endFrame()                                               {}
func (c *pixelBufferCache) releaseAll()                                             {}
