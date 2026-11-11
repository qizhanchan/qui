//go:build cgo && !js

package wgpu

/*
#include <stdlib.h>
#include "helpers.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

// Surface presents to a window: a CAMetalLayer (macOS), an HWND
// (Windows) or an X11 window (Linux).
type Surface struct {
	c      C.WGPUSurface
	dev    *Device
	format TextureFormat
	usage  TextureUsage
	w, h   int
	alpha  bool

	// What the surface supports on this adapter (queried once).
	capsDone   bool
	capUsages  TextureUsage
	capFormats []TextureFormat
	capAlpha   []C.WGPUCompositeAlphaMode
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

// Configure (re)sizes the swap chain. premultiplied asks for alpha
// compositing with the desktop (transparent windows); vsync FIFO
// presentation is used. It checks the request against the surface's
// capabilities: an unsupported format is an error, an unsupported alpha
// mode falls back to one the surface offers (Vulkan on X11 often has only
// Opaque or Inherit), and CopySrc is requested only where supported. A
// zero size is not configured (a minimized window) and reports no error.
func (s *Surface) Configure(w, h int, format TextureFormat, premultiplied bool) error {
	if w <= 0 || h <= 0 {
		return nil
	}
	if w == s.w && h == s.h && format == s.format && premultiplied == s.alpha {
		return nil
	}
	s.queryCaps()
	if len(s.capFormats) > 0 && !containsFormat(s.capFormats, format) {
		return fmt.Errorf("wgpu: surface does not support format %d (supports %v)", format, s.capFormats)
	}
	usage := UsageRenderAttachment
	if s.capUsages == 0 || s.capUsages&UsageCopySrc != 0 {
		usage |= UsageCopySrc
	}
	cfg := C.WGPUSurfaceConfiguration{
		device:      s.dev.c,
		format:      C.WGPUTextureFormat(format),
		usage:       C.WGPUTextureUsage(usage),
		width:       C.uint32_t(w),
		height:      C.uint32_t(h),
		alphaMode:   s.pickAlpha(premultiplied),
		presentMode: C.WGPUPresentMode_Fifo,
	}
	C.wgpuSurfaceConfigure(s.c, &cfg)
	s.w, s.h, s.format, s.alpha = w, h, format, premultiplied
	s.usage = usage
	return nil
}

func (s *Surface) queryCaps() {
	if s.capsDone {
		return
	}
	s.capsDone = true
	var c C.WGPUSurfaceCapabilities
	if C.wgpuSurfaceGetCapabilities(s.c, s.dev.adapter, &c) != C.WGPUStatus_Success {
		return
	}
	s.capUsages = TextureUsage(c.usages)
	if c.formatCount > 0 {
		for _, f := range unsafe.Slice(c.formats, c.formatCount) {
			s.capFormats = append(s.capFormats, TextureFormat(f))
		}
	}
	if c.alphaModeCount > 0 {
		s.capAlpha = append(s.capAlpha, unsafe.Slice(c.alphaModes, c.alphaModeCount)...)
	}
	C.wgpuSurfaceCapabilitiesFreeMembers(c)
}

// pickAlpha is the composite alpha mode closest to the request that the
// surface supports.
func (s *Surface) pickAlpha(premultiplied bool) C.WGPUCompositeAlphaMode {
	var prefs []C.WGPUCompositeAlphaMode
	if premultiplied {
		prefs = []C.WGPUCompositeAlphaMode{C.WGPUCompositeAlphaMode_Premultiplied, C.WGPUCompositeAlphaMode_Inherit,
			C.WGPUCompositeAlphaMode_Opaque}
	} else {
		prefs = []C.WGPUCompositeAlphaMode{C.WGPUCompositeAlphaMode_Opaque, C.WGPUCompositeAlphaMode_Inherit}
	}
	if len(s.capAlpha) == 0 {
		return prefs[0]
	}
	for _, p := range prefs {
		for _, a := range s.capAlpha {
			if a == p {
				return p
			}
		}
	}
	return s.capAlpha[0]
}

func containsFormat(fs []TextureFormat, f TextureFormat) bool {
	for _, x := range fs {
		if x == f {
			return true
		}
	}
	return false
}

// Size is the configured size.
func (s *Surface) Size() (int, int) { return s.w, s.h }

// AcquireStatus is why Acquire returned no texture.
type AcquireStatus int

const (
	AcquireOK      AcquireStatus = iota
	AcquireSkipped               // no frame this time (timeout, outdated, not configured): try the next one
	AcquireLost                  // the surface is gone: recreate it
	AcquireFailed                // the surface cannot present
)

// Acquire returns the texture to draw the next frame into, or nil and why
// not. After a skip the surface reconfigures on the next Configure.
func (s *Surface) Acquire() (*Texture, AcquireStatus) {
	if s.w == 0 || s.h == 0 {
		return nil, AcquireSkipped
	}
	var st C.WGPUSurfaceTexture
	C.wgpuSurfaceGetCurrentTexture(s.c, &st)
	if st.texture != nil &&
		(st.status == C.WGPUSurfaceGetCurrentTextureStatus_SuccessOptimal ||
			st.status == C.WGPUSurfaceGetCurrentTextureStatus_SuccessSuboptimal) {
		return &Texture{c: st.texture, w: s.w, h: s.h, format: s.format, usage: s.usage}, AcquireOK
	}
	if st.texture != nil {
		C.wgpuTextureRelease(st.texture)
	}
	s.w, s.h = 0, 0 // force a reconfigure
	switch st.status {
	case C.WGPUSurfaceGetCurrentTextureStatus_Timeout, C.WGPUSurfaceGetCurrentTextureStatus_Outdated:
		return nil, AcquireSkipped
	case C.WGPUSurfaceGetCurrentTextureStatus_Lost:
		return nil, AcquireLost
	}
	return nil, AcquireFailed
}

// CurrentTexture returns the texture to draw the next frame into, or nil
// when there is none (see Acquire for why).
func (s *Surface) CurrentTexture() *Texture {
	t, _ := s.Acquire()
	return t
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
	return &Texture{c: t, w: w, h: h, format: format, usage: usage}
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
