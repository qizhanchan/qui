//go:build darwin && cgo

package render

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework IOSurface -framework CoreVideo -framework CoreFoundation -framework QuartzCore
#include <IOSurface/IOSurface.h>
#include <CoreVideo/CoreVideo.h>
#import <QuartzCore/CAMetalLayer.h>

static int quiLayerPresentsWithTransaction(void* layer) {
    if (layer == NULL) return 0;
    return ((__bridge CAMetalLayer*)layer).presentsWithTransaction ? 1 : 0;
}

static IOSurfaceRef quiNewFrameSurface(int w, int h) {
    size_t row = IOSurfaceAlignProperty(kIOSurfaceBytesPerRow, (size_t)w * 4);
    CFMutableDictionaryRef props = CFDictionaryCreateMutable(NULL, 0,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    int bpe = 4;
    uint32_t fmt = 'RGBA';
    long rowl = (long)row;
    CFNumberRef nw = CFNumberCreate(NULL, kCFNumberIntType, &w);
    CFNumberRef nh = CFNumberCreate(NULL, kCFNumberIntType, &h);
    CFNumberRef nb = CFNumberCreate(NULL, kCFNumberIntType, &bpe);
    CFNumberRef nr = CFNumberCreate(NULL, kCFNumberLongType, &rowl);
    CFNumberRef nf = CFNumberCreate(NULL, kCFNumberSInt32Type, &fmt);
    CFDictionarySetValue(props, kIOSurfaceWidth, nw);
    CFDictionarySetValue(props, kIOSurfaceHeight, nh);
    CFDictionarySetValue(props, kIOSurfaceBytesPerElement, nb);
    CFDictionarySetValue(props, kIOSurfaceBytesPerRow, nr);
    CFDictionarySetValue(props, kIOSurfacePixelFormat, nf);
    IOSurfaceRef s = IOSurfaceCreate(props);
    CFRelease(nw); CFRelease(nh); CFRelease(nb); CFRelease(nr); CFRelease(nf);
    CFRelease(props);
    return s;
}

static void* quiSurfPtr(IOSurfaceRef s) { return (void*)s; }

// quiNewTestLayer is an offscreen CAMetalLayer (in no window) whose
// drawables can be read back: what tests render into. Retained (+1).
static void* quiNewTestLayer(int width, int height) {
    @autoreleasepool {
        CAMetalLayer* layer = [CAMetalLayer layer];
        layer.framebufferOnly = NO;
        layer.opaque = YES;
        layer.drawableSize = CGSizeMake(width, height);
        return (__bridge_retained void*)layer;
    }
}

static void quiReleaseTestLayer(void* layer) {
    if (layer) CFRelease(layer);
}
static void quiRetain(void* p) { if (p) CFRetain(p); }
static void quiRelease(void* p) { if (p) CFRelease(p); }

static IOSurfaceRef quiPixelBufferSurface(void* pb) {
    if (pb == NULL) return NULL;
    CVPixelBufferRef b = (CVPixelBufferRef)pb;
    if (CVPixelBufferGetPixelFormatType(b) != kCVPixelFormatType_32BGRA) return NULL;
    return CVPixelBufferGetIOSurface(b);
}
*/
import "C"

import (
	"image"
	"unsafe"

	"github.com/qizhanchan/qui/internal/wgpu"
)

// presentsWithTransaction reports that the CAMetalLayer wants each
// drawable presented inside the current Core Animation transaction (cocoa
// sets it during a live resize, so the new-size frame lands together with
// the window frame).
func presentsWithTransaction(t SurfaceTarget) bool {
	return C.quiLayerPresentsWithTransaction(t.MetalLayer) != 0
}

// sharedFramesAvailable: frames can live in IOSurfaces the GPU samples
// directly (when the device can import them — Apple silicon).
const sharedFramesAvailable = true

// sharedFrame is the CPU frame in an IOSurface, imported into WebGPU.
type sharedFrame struct {
	sharedTex
	surf   C.IOSurfaceRef
	w, h   int
	base   unsafe.Pointer
	stride int
}

func newSharedFrame(dev *wgpu.Device, w, h int) *sharedFrame {
	surf := C.quiNewFrameSurface(C.int(w), C.int(h))
	if surf == 0 {
		return nil
	}
	mem, err := dev.ImportIOSurface(C.quiSurfPtr(surf))
	if err != nil {
		C.CFRelease(C.CFTypeRef(surf))
		return nil
	}
	tex := mem.CreateTexture(w, h, wgpu.FormatRGBA8Unorm,
		wgpu.UsageTextureBinding|wgpu.UsageCopySrc|wgpu.UsageCopyDst)
	if tex == nil {
		mem.Release()
		C.CFRelease(C.CFTypeRef(surf))
		return nil
	}
	return &sharedFrame{
		sharedTex: sharedTex{mem: mem, tex: tex},
		surf:      surf, w: w, h: h,
		base:   C.IOSurfaceGetBaseAddress(surf),
		stride: int(C.IOSurfaceGetBytesPerRow(surf)),
	}
}

// image is the frame as an *image.RGBA over the surface's memory.
func (s *sharedFrame) image() *image.RGBA {
	return &image.RGBA{
		Pix:    unsafe.Slice((*byte)(s.base), s.stride*s.h),
		Stride: s.stride,
		Rect:   image.Rect(0, 0, s.w, s.h),
	}
}

func (s *sharedFrame) lock()   { C.IOSurfaceLock(s.surf, 0, nil) }
func (s *sharedFrame) unlock() { C.IOSurfaceUnlock(s.surf, 0, nil) }

// release frees the memory (the caller released the texture).
func (s *sharedFrame) release() {
	s.mem.Release()
	C.CFRelease(C.CFTypeRef(s.surf))
	s.surf = 0
}

// pixelBufferCache keeps imported CVPixelBuffer IOSurfaces: decoders cycle
// through a small pool of them, so each is imported once. An entry unused
// for a few frames is dropped; the pixel buffer is retained while cached
// so its surface stays out of the decoder's pool while the GPU may read it.
type pixelBufferCache struct {
	entries map[C.IOSurfaceRef]*pbEntry
	frame   int
}

type pbEntry struct {
	sharedTex
	view *wgpu.TextureView
	bg   *wgpu.BindGroup
	pb   unsafe.Pointer // CVPixelBufferRef, retained
	used int
}

func (c *pixelBufferCache) get(dev *wgpu.Device, pipes *webgpuPipes, pb unsafe.Pointer) *pbEntry {
	surf := C.quiPixelBufferSurface(pb)
	if surf == 0 || !dev.CanImportIOSurface() {
		return nil
	}
	if e := c.entries[surf]; e != nil {
		if e.pb != pb {
			C.quiRetain(pb)
			C.quiRelease(e.pb)
			e.pb = pb
		}
		e.used = c.frame
		return e
	}
	mem, err := dev.ImportIOSurface(C.quiSurfPtr(surf))
	if err != nil {
		return nil
	}
	w, h := int(C.IOSurfaceGetWidth(surf)), int(C.IOSurfaceGetHeight(surf))
	tex := mem.CreateTexture(w, h, wgpu.FormatBGRA8Unorm, wgpu.UsageTextureBinding)
	if tex == nil {
		mem.Release()
		return nil
	}
	view := tex.CreateView()
	C.quiRetain(pb)
	e := &pbEntry{sharedTex: sharedTex{mem: mem, tex: tex}, view: view,
		bg: pipes.textureBindGroup(view, false), pb: pb, used: c.frame}
	if c.entries == nil {
		c.entries = map[C.IOSurfaceRef]*pbEntry{}
	}
	c.entries[surf] = e
	return e
}

// endFrame drops entries unused for the last few frames.
func (c *pixelBufferCache) endFrame() {
	c.frame++
	for k, e := range c.entries {
		if c.frame-e.used > 4 {
			e.release()
			delete(c.entries, k)
		}
	}
}

func (c *pixelBufferCache) releaseAll() {
	for k, e := range c.entries {
		e.release()
		delete(c.entries, k)
	}
}

func (e *pbEntry) release() {
	e.bg.Release()
	e.view.Release()
	e.tex.Release()
	e.mem.Release()
	C.quiRelease(e.pb)
}

// NewOffscreenWebGPURenderer binds a WebGPURenderer to an offscreen
// CAMetalLayer of w×h pixels whose presented frames can be read back
// (PresentedFrame) — for tests of GPU widgets. release frees the layer
// after Close.
func NewOffscreenWebGPURenderer(w, h int) (r *WebGPURenderer, release func(), err error) {
	if _, err := wgpu.Shared(); err != nil {
		return nil, nil, err
	}
	layer := newTestMetalLayer(w, h)
	r = NewWebGPURenderer()
	r.keepShown = true
	if !r.SetMetalLayer(layer) {
		releaseTestMetalLayer(layer)
		return nil, nil, errNoSurface
	}
	return r, func() { r.Close(); releaseTestMetalLayer(layer) }, nil
}

func newTestMetalLayer(w, h int) unsafe.Pointer { return C.quiNewTestLayer(C.int(w), C.int(h)) }

func releaseTestMetalLayer(l unsafe.Pointer) { C.quiReleaseTestLayer(l) }

// PresentedFrame reads back the last presented frame (offscreen
// renderers only), as RGBA.
func (r *WebGPURenderer) PresentedFrame() *image.RGBA { return r.readShown() }
