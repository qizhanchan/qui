//go:build cgo && !js

package wgpu

/*
#include <stdlib.h>
#include "helpers.h"
*/
import "C"

import (
	"errors"
	"unsafe"
)

// Surface presents to a window: a CAMetalLayer (macOS), an HWND
// (Windows) or an X11 window (Linux).
type Surface struct {
	c      C.WGPUSurface
	dev    *Device
	format TextureFormat
	w, h   int
	alpha  bool
}

func (d *Device) createSurface(chain *C.WGPUChainedStruct) (*Surface, error) {
	sd := C.WGPUSurfaceDescriptor{nextInChain: chain}
	s := C.wgpuInstanceCreateSurface(d.inst, &sd)
	if s == nil {
		return nil, errors.New("wgpu: cannot create surface")
	}
	return &Surface{c: s, dev: d}, nil
}

// CreateSurfaceMetalLayer wraps a CAMetalLayer.
func (d *Device) CreateSurfaceMetalLayer(layer unsafe.Pointer) (*Surface, error) {
	src := (*C.WGPUSurfaceSourceMetalLayer)(C.calloc(1, C.sizeof_WGPUSurfaceSourceMetalLayer))
	defer C.free(unsafe.Pointer(src))
	src.chain.sType = C.WGPUSType_SurfaceSourceMetalLayer
	src.layer = layer
	return d.createSurface(&src.chain)
}

// CreateSurfaceHWND wraps a Win32 window.
func (d *Device) CreateSurfaceHWND(hinstance, hwnd unsafe.Pointer) (*Surface, error) {
	src := (*C.WGPUSurfaceSourceWindowsHWND)(C.calloc(1, C.sizeof_WGPUSurfaceSourceWindowsHWND))
	defer C.free(unsafe.Pointer(src))
	src.chain.sType = C.WGPUSType_SurfaceSourceWindowsHWND
	src.hinstance = hinstance
	src.hwnd = hwnd
	return d.createSurface(&src.chain)
}

// CreateSurfaceXlib wraps an X11 window.
func (d *Device) CreateSurfaceXlib(display unsafe.Pointer, window uint64) (*Surface, error) {
	src := (*C.WGPUSurfaceSourceXlibWindow)(C.calloc(1, C.sizeof_WGPUSurfaceSourceXlibWindow))
	defer C.free(unsafe.Pointer(src))
	src.chain.sType = C.WGPUSType_SurfaceSourceXlibWindow
	src.display = display
	src.window = C.uint64_t(window)
	return d.createSurface(&src.chain)
}

// Configure (re)sizes the swap chain. premultiplied selects alpha
// compositing with the desktop (transparent windows); vsync FIFO
// presentation is used.
func (s *Surface) Configure(w, h int, format TextureFormat, premultiplied bool) {
	if w == s.w && h == s.h && format == s.format && premultiplied == s.alpha {
		return
	}
	cfg := C.WGPUSurfaceConfiguration{
		device:      s.dev.c,
		format:      C.WGPUTextureFormat(format),
		usage:       C.WGPUTextureUsage_RenderAttachment | C.WGPUTextureUsage_CopySrc,
		width:       C.uint32_t(w),
		height:      C.uint32_t(h),
		alphaMode:   C.WGPUCompositeAlphaMode_Opaque,
		presentMode: C.WGPUPresentMode_Fifo,
	}
	if premultiplied {
		cfg.alphaMode = C.WGPUCompositeAlphaMode_Premultiplied
	}
	C.wgpuSurfaceConfigure(s.c, &cfg)
	s.w, s.h, s.format, s.alpha = w, h, format, premultiplied
}

// Size is the configured size.
func (s *Surface) Size() (int, int) { return s.w, s.h }

// CurrentTexture returns the texture to draw the next frame into, or nil
// when there is none (window minimized, surface lost — reconfigure).
func (s *Surface) CurrentTexture() *Texture {
	var st C.WGPUSurfaceTexture
	C.wgpuSurfaceGetCurrentTexture(s.c, &st)
	if st.texture == nil ||
		(st.status != C.WGPUSurfaceGetCurrentTextureStatus_SuccessOptimal &&
			st.status != C.WGPUSurfaceGetCurrentTextureStatus_SuccessSuboptimal) {
		if st.texture != nil {
			C.wgpuTextureRelease(st.texture)
		}
		s.w, s.h = 0, 0 // force a reconfigure
		return nil
	}
	return &Texture{c: st.texture, w: s.w, h: s.h, format: s.format}
}

// Present shows the texture CurrentTexture returned.
func (s *Surface) Present() { C.wgpuSurfacePresent(s.c) }

func (s *Surface) Release() {
	if s != nil && s.c != nil {
		C.wgpuSurfaceUnconfigure(s.c)
		C.wgpuSurfaceRelease(s.c)
		s.c = nil
	}
}

// SharedMemory is an imported platform texture (an IOSurface).
type SharedMemory struct{ c C.WGPUSharedTextureMemory }

// ImportIOSurface wraps an IOSurfaceRef without copying.
func (d *Device) ImportIOSurface(ioSurface unsafe.Pointer) (*SharedMemory, error) {
	if !d.ioSurface {
		return nil, errors.New("wgpu: IOSurface import unsupported")
	}
	src := (*C.WGPUSharedTextureMemoryIOSurfaceDescriptor)(C.calloc(1, C.sizeof_WGPUSharedTextureMemoryIOSurfaceDescriptor))
	defer C.free(unsafe.Pointer(src))
	src.chain.sType = C.WGPUSType_SharedTextureMemoryIOSurfaceDescriptor
	src.ioSurface = ioSurface
	desc := C.WGPUSharedTextureMemoryDescriptor{nextInChain: &src.chain}
	m := C.wgpuDeviceImportSharedTextureMemory(d.c, &desc)
	if m == nil {
		return nil, errors.New("wgpu: IOSurface import failed")
	}
	return &SharedMemory{c: m}, nil
}

// CreateTexture makes the texture over the shared memory (its size and
// format are the memory's own; usage restricts what it is used for).
func (m *SharedMemory) CreateTexture(w, h int, format TextureFormat, usage TextureUsage) *Texture {
	td := C.WGPUTextureDescriptor{
		usage:         C.WGPUTextureUsage(usage),
		dimension:     C.WGPUTextureDimension_2D,
		size:          C.WGPUExtent3D{C.uint32_t(w), C.uint32_t(h), 1},
		format:        C.WGPUTextureFormat(format),
		mipLevelCount: 1,
		sampleCount:   1,
	}
	t := C.wgpuSharedTextureMemoryCreateTexture(m.c, &td)
	if t == nil {
		return nil
	}
	return &Texture{c: t, w: w, h: h, format: format}
}

// BeginAccess lets the GPU use t until EndAccess. initialized says the
// memory already holds content the GPU must keep (the CPU wrote it).
func (m *SharedMemory) BeginAccess(t *Texture, initialized bool) bool {
	d := C.WGPUSharedTextureMemoryBeginAccessDescriptor{initialized: cbool(initialized)}
	return C.wgpuSharedTextureMemoryBeginAccess(m.c, t.c, &d) == C.WGPUStatus_Success
}

// EndAccess ends GPU use; work submitted before it is ordered before any
// later BeginAccess.
func (m *SharedMemory) EndAccess(t *Texture) bool {
	var st C.WGPUSharedTextureMemoryEndAccessState
	ok := C.wgpuSharedTextureMemoryEndAccess(m.c, t.c, &st) == C.WGPUStatus_Success
	C.wgpuSharedTextureMemoryEndAccessStateFreeMembers(st)
	return ok
}

func (m *SharedMemory) Release() {
	if m != nil && m.c != nil {
		C.wgpuSharedTextureMemoryRelease(m.c)
		m.c = nil
	}
}
