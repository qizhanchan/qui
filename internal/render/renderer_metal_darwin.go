//go:build darwin && cgo && !qui_nometal

package render

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Metal -framework QuartzCore -framework CoreVideo -framework Foundation

#import <Metal/Metal.h>
#import <QuartzCore/QuartzCore.h>
#import <CoreVideo/CoreVideo.h>
#include <stdint.h>

// QuiMetal is everything one window's Metal presentation needs: device,
// queue and pipelines, the frame texture the CPU raster is uploaded into,
// the textures retained draws composite, and the frame being encoded.
@interface QuiMetal : NSObject
@property (nonatomic, strong) id<MTLDevice> device;
@property (nonatomic, strong) id<MTLCommandQueue> queue;
@property (nonatomic, strong) id<MTLRenderPipelineState> layerPipeline;
@property (nonatomic, strong) id<MTLRenderPipelineState> texPipeline;
@property (nonatomic, strong) id<MTLSamplerState> sampler;
@property (nonatomic, strong) id<MTLTexture> frame;
@property (nonatomic, strong) id<MTLCommandBuffer> last;
@property (nonatomic, strong) CAMetalLayer* layer;
@property (nonatomic, assign) CVMetalTextureCacheRef cvCache;
// pixels: textures for DrawPixels, by the caller's key id.
@property (nonatomic, strong) NSMutableDictionary<NSNumber*, id<MTLTexture>>* pixels;
// holds keeps CVMetalTextures (and so their pixel buffers' IOSurfaces)
// alive until the command buffer that sampled them has completed.
@property (nonatomic, strong) NSMutableArray* holds;
// The frame being encoded (between quiMetalBeginFrame and EndFrame).
@property (nonatomic, strong) id<MTLCommandBuffer> cmd;
@property (nonatomic, strong) id<MTLRenderCommandEncoder> enc;
@property (nonatomic, strong) id<CAMetalDrawable> drawable;
@end

@implementation QuiMetal
- (void)dealloc {
    if (_cvCache) CFRelease(_cvCache);
}
@end

// Two pipelines over one full-quad vertex stage:
//   qui_layer   the CPU raster, read texel-for-pixel (the frame texture has
//               the drawable's size), composited premultiplied src-over so
//               the holes ClearRect punched show what retained draws put
//               underneath;
//   qui_tex     a src rect of any texture, bilinear, onto a dst rect.
// Rects are NDC (x0,y0,x1,y1) for dst and UV for src, pushed as bytes.
static NSString* const quiMetalShaders =
    @"#include <metal_stdlib>\n"
     "using namespace metal;\n"
     "struct VOut { float4 pos [[position]]; float2 uv; };\n"
     "vertex VOut qui_quad(uint vid [[vertex_id]], constant float4* r [[buffer(0)]]) {\n"
     "    float2 c = float2(vid & 1, vid >> 1);\n"
     "    float4 d = r[0]; float4 s = r[1];\n"
     "    VOut o;\n"
     "    o.pos = float4(mix(d.x, d.z, c.x), mix(d.y, d.w, c.y), 0.0, 1.0);\n"
     "    o.uv = float2(mix(s.x, s.z, c.x), mix(s.y, s.w, c.y));\n"
     "    return o;\n"
     "}\n"
     "fragment float4 qui_layer(VOut in [[stage_in]], texture2d<float> tex [[texture(0)]]) {\n"
     "    uint2 p = uint2(in.pos.xy);\n"
     "    if (p.x >= tex.get_width() || p.y >= tex.get_height()) return float4(0.0);\n"
     "    return tex.read(p);\n"
     "}\n"
     "fragment float4 qui_tex(VOut in [[stage_in]], texture2d<float> tex [[texture(0)]],\n"
     "                        sampler smp [[sampler(0)]]) {\n"
     "    return tex.sample(smp, in.uv);\n"
     "}\n";

static id<MTLRenderPipelineState> quiMetalPipeline(id<MTLDevice> device, id<MTLLibrary> lib,
                                                   NSString* fragment) {
    MTLRenderPipelineDescriptor* pd = [[MTLRenderPipelineDescriptor alloc] init];
    pd.vertexFunction = [lib newFunctionWithName:@"qui_quad"];
    pd.fragmentFunction = [lib newFunctionWithName:fragment];
    MTLRenderPipelineColorAttachmentDescriptor* ca = pd.colorAttachments[0];
    ca.pixelFormat = MTLPixelFormatBGRA8Unorm;
    // Premultiplied source over: what the CPU raster produces and what a
    // CAMetalLayer composites.
    ca.blendingEnabled = YES;
    ca.rgbBlendOperation = MTLBlendOperationAdd;
    ca.alphaBlendOperation = MTLBlendOperationAdd;
    ca.sourceRGBBlendFactor = MTLBlendFactorOne;
    ca.sourceAlphaBlendFactor = MTLBlendFactorOne;
    ca.destinationRGBBlendFactor = MTLBlendFactorOneMinusSourceAlpha;
    ca.destinationAlphaBlendFactor = MTLBlendFactorOneMinusSourceAlpha;
    NSError* err = nil;
    id<MTLRenderPipelineState> p = [device newRenderPipelineStateWithDescriptor:pd error:&err];
    if (p == nil) NSLog(@"qui: Metal pipeline %@ failed: %@", fragment, err);
    return p;
}

// quiMetalCreate binds a presentation context to layer, or returns NULL
// when the machine has no Metal device or the shaders don't build.
static void* quiMetalCreate(void* layerPtr) {
    @autoreleasepool {
        CAMetalLayer* layer = (__bridge CAMetalLayer*)layerPtr;
        if (layer == nil) return NULL;
        id<MTLDevice> device = layer.device;
        if (device == nil) device = MTLCreateSystemDefaultDevice();
        if (device == nil) return NULL;
        NSError* err = nil;
        id<MTLLibrary> lib = [device newLibraryWithSource:quiMetalShaders options:nil error:&err];
        if (lib == nil) {
            NSLog(@"qui: Metal shader build failed: %@", err);
            return NULL;
        }
        id<MTLRenderPipelineState> layerPipeline = quiMetalPipeline(device, lib, @"qui_layer");
        id<MTLRenderPipelineState> texPipeline = quiMetalPipeline(device, lib, @"qui_tex");
        if (layerPipeline == nil || texPipeline == nil) return NULL;
        MTLSamplerDescriptor* sd = [[MTLSamplerDescriptor alloc] init];
        sd.minFilter = MTLSamplerMinMagFilterLinear;
        sd.magFilter = MTLSamplerMinMagFilterLinear;
        sd.sAddressMode = MTLSamplerAddressModeClampToEdge;
        sd.tAddressMode = MTLSamplerAddressModeClampToEdge;
        layer.device = device;
        layer.pixelFormat = MTLPixelFormatBGRA8Unorm;
        QuiMetal* m = [[QuiMetal alloc] init];
        m.device = device;
        m.queue = [device newCommandQueue];
        m.layerPipeline = layerPipeline;
        m.texPipeline = texPipeline;
        m.sampler = [device newSamplerStateWithDescriptor:sd];
        m.layer = layer;
        m.pixels = [NSMutableDictionary dictionary];
        m.holds = [NSMutableArray array];
        CVMetalTextureCacheRef cache = NULL;
        if (CVMetalTextureCacheCreate(kCFAllocatorDefault, NULL, device, NULL, &cache) == kCVReturnSuccess) {
            m.cvCache = cache;
        }
        return (__bridge_retained void*)m;
    }
}

static void quiMetalDestroy(void* ctx) {
    if (ctx == NULL) return;
    QuiMetal* m = (__bridge_transfer QuiMetal*)ctx;
    [m.last waitUntilCompleted];
    m = nil;
}

static void quiMetalForgetPixels(void* ctx, uintptr_t key) {
    QuiMetal* m = (__bridge QuiMetal*)ctx;
    [m.pixels removeObjectForKey:@(key)];
}

static int quiMetalSetFrame(QuiMetal* m, int width, int height) {
    if (m.frame != nil && (int)m.frame.width == width && (int)m.frame.height == height) return 0;
    MTLTextureDescriptor* td =
        [MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatRGBA8Unorm
                                                           width:width
                                                          height:height
                                                       mipmapped:NO];
    td.usage = MTLTextureUsageShaderRead;
    td.storageMode = m.device.hasUnifiedMemory ? MTLStorageModeShared : MTLStorageModeManaged;
    m.frame = [m.device newTextureWithDescriptor:td];
    return 1;
}

// quiMetalBeginFrame waits for the previous frame's GPU work (it sampled
// the textures about to be rewritten), uploads the damaged rectangle
// (dx,dy,dw,dh) of the CPU raster — all of it when the frame texture is
// (re)allocated; dw = 0 uploads nothing — and opens a render pass on the
// next drawable, cleared to transparent (panels) or opaque black. Returns 0
// when there is no drawable to draw into (the frame is skipped).
static int quiMetalBeginFrame(void* ctx, const uint8_t* pix, int stride, int width, int height,
                              int dx, int dy, int dw, int dh) {
    QuiMetal* m = (__bridge QuiMetal*)ctx;
    if (m == nil || width <= 0 || height <= 0) return 0;
    [m.last waitUntilCompleted];
    m.last = nil;
    [m.holds removeAllObjects];
    if (m.cvCache) CVMetalTextureCacheFlush(m.cvCache, 0);
    if (quiMetalSetFrame(m, width, height)) {
        if (m.frame == nil) return 0;
        dx = 0; dy = 0; dw = width; dh = height;
        if (pix == NULL) return 0; // nothing to show yet
    }
    if (dx < 0) { dw += dx; dx = 0; }
    if (dy < 0) { dh += dy; dy = 0; }
    if (dx + dw > width) dw = width - dx;
    if (dy + dh > height) dh = height - dy;
    if (pix != NULL && dw > 0 && dh > 0) {
        [m.frame replaceRegion:MTLRegionMake2D(dx, dy, dw, dh)
                   mipmapLevel:0
                     withBytes:pix + (size_t)dy * stride + (size_t)dx * 4
                   bytesPerRow:stride];
    }

    CAMetalLayer* layer = m.layer;
    CGSize want = CGSizeMake(width, height);
    if (!CGSizeEqualToSize(layer.drawableSize, want)) {
        [CATransaction begin];
        [CATransaction setDisableActions:YES];
        layer.drawableSize = want;
        [CATransaction commit];
    }
    id<CAMetalDrawable> drawable = [layer nextDrawable];
    if (drawable == nil) return 0;
    MTLRenderPassDescriptor* rp = [MTLRenderPassDescriptor renderPassDescriptor];
    rp.colorAttachments[0].texture = drawable.texture;
    rp.colorAttachments[0].loadAction = MTLLoadActionClear;
    rp.colorAttachments[0].clearColor = MTLClearColorMake(0, 0, 0, layer.opaque ? 1 : 0);
    rp.colorAttachments[0].storeAction = MTLStoreActionStore;
    m.cmd = [m.queue commandBuffer];
    m.enc = [m.cmd renderCommandEncoderWithDescriptor:rp];
    m.drawable = drawable;
    return 1;
}

// quiMetalQuad draws tex's (u0,v0)-(u1,v1) onto the physical rect
// (x,y,w,h) of the drawable.
static void quiMetalQuad(QuiMetal* m, id<MTLTexture> tex, float u0, float v0, float u1, float v1,
                         float x, float y, float w, float h) {
    float fw = (float)m.drawable.texture.width, fh = (float)m.drawable.texture.height;
    if (fw <= 0 || fh <= 0) return;
    float r[8] = {
        x / fw * 2 - 1, 1 - y / fh * 2, (x + w) / fw * 2 - 1, 1 - (y + h) / fh * 2,
        u0, v0, u1, v1,
    };
    [m.enc setRenderPipelineState:m.texPipeline];
    [m.enc setVertexBytes:r length:sizeof r atIndex:0];
    [m.enc setFragmentTexture:tex atIndex:0];
    [m.enc setFragmentSamplerState:m.sampler atIndex:0];
    [m.enc drawPrimitives:MTLPrimitiveTypeTriangleStrip vertexStart:0 vertexCount:4];
}

// quiMetalDrawPixelBuffer composites the src rect of a 32BGRA,
// IOSurface-backed CVPixelBuffer at dst without copying its pixels.
static int quiMetalDrawPixelBuffer(void* ctx, void* pb, float sx, float sy, float sw, float sh,
                                   float x, float y, float w, float h) {
    QuiMetal* m = (__bridge QuiMetal*)ctx;
    if (m.enc == nil || m.cvCache == NULL || pb == NULL) return 0;
    CVPixelBufferRef buf = (CVPixelBufferRef)pb;
    if (CVPixelBufferGetIOSurface(buf) == NULL) return 0;
    size_t bw = CVPixelBufferGetWidth(buf), bh = CVPixelBufferGetHeight(buf);
    CVMetalTextureRef cvTex = NULL;
    if (CVMetalTextureCacheCreateTextureFromImage(kCFAllocatorDefault, m.cvCache, buf, NULL,
            MTLPixelFormatBGRA8Unorm, bw, bh, 0, &cvTex) != kCVReturnSuccess || cvTex == NULL) {
        return 0;
    }
    id<MTLTexture> tex = CVMetalTextureGetTexture(cvTex);
    [m.holds addObject:(__bridge_transfer id)cvTex];
    if (tex == nil || bw == 0 || bh == 0) return 0;
    quiMetalQuad(m, tex, sx / bw, sy / bh, (sx + sw) / bw, (sy + sh) / bh, x, y, w, h);
    return 1;
}

// quiMetalDrawPixels uploads a w×h frame of 32-bit pixels into the
// texture kept under key and composites its src rect at dst.
static int quiMetalDrawPixels(void* ctx, uintptr_t key, const uint8_t* pix, int stride, int pw, int ph,
                              int bgra, float sx, float sy, float sw, float sh,
                              float x, float y, float w, float h) {
    QuiMetal* m = (__bridge QuiMetal*)ctx;
    if (m.enc == nil || pix == NULL || pw <= 0 || ph <= 0) return 0;
    MTLPixelFormat fmt = bgra ? MTLPixelFormatBGRA8Unorm : MTLPixelFormatRGBA8Unorm;
    id<MTLTexture> tex = m.pixels[@(key)];
    if (tex == nil || (int)tex.width != pw || (int)tex.height != ph || tex.pixelFormat != fmt) {
        MTLTextureDescriptor* td = [MTLTextureDescriptor texture2DDescriptorWithPixelFormat:fmt
                                                                                      width:pw
                                                                                     height:ph
                                                                                  mipmapped:NO];
        td.usage = MTLTextureUsageShaderRead;
        td.storageMode = m.device.hasUnifiedMemory ? MTLStorageModeShared : MTLStorageModeManaged;
        tex = [m.device newTextureWithDescriptor:td];
        if (tex == nil) return 0;
        m.pixels[@(key)] = tex;
    }
    // Safe to overwrite: BeginFrame waited for the frame that last
    // sampled it.
    [tex replaceRegion:MTLRegionMake2D(0, 0, pw, ph) mipmapLevel:0 withBytes:pix bytesPerRow:stride];
    quiMetalQuad(m, tex, sx / pw, sy / ph, (sx + sw) / pw, (sy + sh) / ph, x, y, w, h);
    return 1;
}

// quiMetalEndFrame lays the 2D layer over whatever retained draws put
// down, presents and commits.
static void quiMetalEndFrame(void* ctx) {
    @autoreleasepool {
        QuiMetal* m = (__bridge QuiMetal*)ctx;
        if (m.enc == nil) return;
        float r[8] = {-1, 1, 1, -1, 0, 0, 1, 1};
        [m.enc setRenderPipelineState:m.layerPipeline];
        [m.enc setVertexBytes:r length:sizeof r atIndex:0];
        [m.enc setFragmentTexture:m.frame atIndex:0];
        [m.enc drawPrimitives:MTLPrimitiveTypeTriangleStrip vertexStart:0 vertexCount:4];
        [m.enc endEncoding];
        id<MTLCommandBuffer> cmd = m.cmd;
        id<CAMetalDrawable> drawable = m.drawable;
        if (m.layer.presentsWithTransaction) {
            // Live resize: show the frame in the same Core Animation
            // transaction as the window's new size.
            [cmd commit];
            [cmd waitUntilScheduled];
            [drawable present];
        } else {
            [cmd presentDrawable:drawable];
            [cmd commit];
        }
        m.last = cmd;
        m.cmd = nil;
        m.enc = nil;
        m.drawable = nil;
    }
}
*/
import "C"

import (
	"image"
	"log"
	"unsafe"

	"github.com/qizhanchan/qui/internal/gfx"
)

// MetalRendererAvailable reports whether this build contains the Metal
// renderer (macOS with cgo, without the qui_nometal tag).
const MetalRendererAvailable = true

// MetalRenderer rasterizes on the CPU, like CPURenderer, and presents
// through Metal: each frame's damaged pixels are uploaded into a
// persistent texture, and a drawable of the window's CAMetalLayer is
// redrawn from that whole texture and presented. Because every drawable is
// redrawn in full from a texture that always holds the latest frame,
// nothing the compositor shows can be an older frame — the class of bug a
// double-buffered GL surface without a preserved back buffer has.
//
// The window hands it its layer (SetMetalLayer) and lets it present
// (PresentsItself). Retained Metal draws (RetainedMetalCanvas) composite
// video-like content under the 2D layer. Raw-GL requests from widgets
// (GPUCanvas, RetainedGLCanvas) cannot be honored; the first one is
// recorded (WantsGL) and the window switches itself to a GLRenderer.
type MetalRenderer struct {
	img         *image.RGBA
	backend     *CPUBackend
	logicalSize gfx.Size
	// damage is the physical-pixel area the frame being rendered changed
	// (SetDamage); empty means unknown, i.e. the whole frame.
	damage      image.Rectangle
	transparent bool

	layer   unsafe.Pointer // CAMetalLayer, owned by the window
	ctx     unsafe.Pointer // retained QuiMetal for layer
	wantsGL bool
	painted bool // a frame has been rasterized into img

	retained RetainedSet[func(MetalCompositor)]
	// queued are this frame's fresh retained draws, run in End.
	queued []func(MetalCompositor)
	// keys maps DrawPixels keys to the integer ids the C side files
	// their textures under.
	keys   map[any]uintptr
	nextID uintptr
}

// NewMetalRenderer creates a renderer that presents through Metal. It
// draws nothing until the window gives it a layer.
func NewMetalRenderer() *MetalRenderer { return &MetalRenderer{} }

// SetMetalLayer implements MetalLayerRenderer.
func (r *MetalRenderer) SetMetalLayer(layer unsafe.Pointer) bool {
	if r == nil {
		return false
	}
	if layer == r.layer {
		return layer == nil || r.ctx != nil
	}
	r.release()
	r.layer = layer
	if layer == nil {
		return true
	}
	r.ctx = C.quiMetalCreate(layer)
	if r.ctx == nil {
		log.Printf("qui: no usable Metal device for this window")
		r.layer = nil
		return false
	}
	return true
}

func (r *MetalRenderer) release() {
	if r.ctx != nil {
		C.quiMetalDestroy(r.ctx)
		r.ctx = nil
	}
	r.keys = nil
}

// Close releases the Metal objects. The renderer can be bound again.
func (r *MetalRenderer) Close() {
	if r == nil {
		return
	}
	r.release()
	r.layer = nil
}

// Begin starts a frame of the given physical size.
func (r *MetalRenderer) Begin(windowSize gfx.Size) gfx.Canvas {
	w := int(maxFloat(windowSize.W, 1))
	h := int(maxFloat(windowSize.H, 1))
	if r.img == nil || r.img.Rect.Dx() != w || r.img.Rect.Dy() != h {
		r.img = image.NewRGBA(image.Rect(0, 0, w, h))
		r.damage = image.Rectangle{}
	}
	if r.logicalSize.W == 0 || r.logicalSize.H == 0 {
		r.logicalSize = windowSize
	}
	if r.backend == nil {
		r.backend = newCPUBackend(r.img)
	} else {
		r.backend.SetBase(r.img)
	}
	r.backend.Begin(windowSize)
	state := gfx.NewCanvasState(gfx.Rect{W: float32(w), H: float32(h)})
	return metalCanvas{
		imageCanvas: imageCanvas{CanvasState: state, Backend: r.backend, Layers: newFrontendLayerTracker()},
		r:           r,
	}
}

// End finishes the frame and presents it.
func (r *MetalRenderer) End() {
	if r.backend != nil {
		r.backend.End()
	}
	r.painted = true
	damage := r.damage
	r.damage = image.Rectangle{}
	if r.ctx == nil || r.img == nil {
		r.queued = r.queued[:0]
		r.retained.EndFrame()
		return
	}
	img := r.img
	up := img.Rect
	if d := damage.Intersect(img.Rect); !d.Empty() {
		// One-pixel guard band, as GLRenderer's upload: the damage is
		// snapped outward already, this only absorbs rounding.
		up = d.Inset(-1).Intersect(img.Rect)
	}
	r.present(up.Sub(img.Rect.Min), damage, false)
}

// RedrawRetained implements RetainedGLRenderer for Window.RequestGLRedraw:
// a frame that replays the retained draws under the 2D layer the last
// frame left, without rasterizing or uploading it.
func (r *MetalRenderer) RedrawRetained() bool {
	if r == nil || r.ctx == nil || r.img == nil || !r.painted || r.retained.Len() == 0 {
		return false
	}
	r.present(image.Rectangle{}, image.Rectangle{}, true)
	return true
}

// present uploads the up rectangle of the frame (none when empty and the
// texture is current), replays the retained draws the frame's damage left
// alive plus this frame's fresh ones, lays the 2D layer on top and shows
// it.
func (r *MetalRenderer) present(up, damage image.Rectangle, damageNone bool) {
	img := r.img
	w, h := img.Rect.Dx(), img.Rect.Dy()
	replay := r.retained.CarryOver(damage, damageNone)
	fresh := r.queued
	r.queued = r.queued[:0]
	defer r.retained.EndFrame()
	if C.quiMetalBeginFrame(r.ctx, (*C.uint8_t)(unsafe.Pointer(&img.Pix[0])), C.int(img.Stride),
		C.int(w), C.int(h), C.int(up.Min.X), C.int(up.Min.Y), C.int(up.Dx()), C.int(up.Dy())) == 0 {
		return
	}
	comp := metalCompositor{r: r}
	for _, fn := range replay {
		fn(comp)
	}
	for _, fn := range fresh {
		fn(comp)
	}
	C.quiMetalEndFrame(r.ctx)
}

// PresentsItself implements SelfPresentingRenderer: with a layer bound,
// End has presented (or deliberately skipped) the frame.
func (r *MetalRenderer) PresentsItself() bool { return r != nil && r.ctx != nil }

// WantsGL implements GLFallbackRenderer.
func (r *MetalRenderer) WantsGL() bool { return r != nil && r.wantsGL }

// ForgetRetainedGLDraw implements RetainedGLRenderer: drops key's retained
// Metal draw and the texture DrawPixels kept for it.
func (r *MetalRenderer) ForgetRetainedGLDraw(key any) {
	if r == nil {
		return
	}
	r.retained.Forget(key)
	if id, ok := r.keys[key]; ok {
		delete(r.keys, key)
		if r.ctx != nil {
			C.quiMetalForgetPixels(r.ctx, C.uintptr_t(id))
		}
	}
}

// SetLogicalSize records the window's logical (point) size.
func (r *MetalRenderer) SetLogicalSize(s gfx.Size) {
	if r != nil {
		r.logicalSize = s
	}
}

// SetDamage records the physical-pixel area the coming frame repaints.
func (r *MetalRenderer) SetDamage(rect image.Rectangle) {
	if r != nil {
		r.damage = rect
	}
}

// SetTransparent records whether untouched pixels stay transparent. The
// CPU raster keeps alpha and the composite writes it through; the layer's
// opacity comes from the window.
func (r *MetalRenderer) SetTransparent(on bool) {
	if r != nil {
		r.transparent = on
	}
}

// IsTransparent reports whether SetTransparent turned transparency on.
func (r *MetalRenderer) IsTransparent() bool { return r != nil && r.transparent }

// Snapshot returns a copy of the last rendered frame (the 2D layer), or
// nil before the first frame.
func (r *MetalRenderer) Snapshot() *image.RGBA {
	if r == nil || r.img == nil {
		return nil
	}
	dst := image.NewRGBA(r.img.Rect)
	copy(dst.Pix, r.img.Pix)
	return dst
}

// metalCompositor is the MetalCompositor handed to retained draws.
type metalCompositor struct{ r *MetalRenderer }

func (c metalCompositor) FramebufferSize() gfx.Size {
	return gfx.Size{W: float32(c.r.img.Rect.Dx()), H: float32(c.r.img.Rect.Dy())}
}

func (c metalCompositor) DrawPixelBuffer(pb unsafe.Pointer, src, dst gfx.Rect) bool {
	return C.quiMetalDrawPixelBuffer(c.r.ctx, pb,
		C.float(src.X), C.float(src.Y), C.float(src.W), C.float(src.H),
		C.float(dst.X), C.float(dst.Y), C.float(dst.W), C.float(dst.H)) != 0
}

func (c metalCompositor) DrawPixels(key any, pix unsafe.Pointer, stride, w, h int, bgra bool, src, dst gfx.Rect) bool {
	if key == nil || pix == nil {
		return false
	}
	r := c.r
	id, ok := r.keys[key]
	if !ok {
		if r.keys == nil {
			r.keys = map[any]uintptr{}
		}
		r.nextID++
		id = r.nextID
		r.keys[key] = id
	}
	b := C.int(0)
	if bgra {
		b = 1
	}
	return C.quiMetalDrawPixels(r.ctx, C.uintptr_t(id), (*C.uint8_t)(pix), C.int(stride), C.int(w), C.int(h), b,
		C.float(src.X), C.float(src.Y), C.float(src.W), C.float(src.H),
		C.float(dst.X), C.float(dst.Y), C.float(dst.W), C.float(dst.H)) != 0
}

// metalCanvas is the canvas a MetalRenderer frame draws into: retained
// Metal draws, plus the raw-GL extension points only to notice that a
// widget wants them — the call is dropped and the renderer asks the window
// for GL.
type metalCanvas struct {
	imageCanvas
	r *MetalRenderer
}

// QueueRetainedMetalDraw implements RetainedMetalCanvas.
func (c metalCanvas) QueueRetainedMetalDraw(key any, physBounds gfx.Rect, fn func(MetalCompositor)) {
	if key == nil || fn == nil {
		return
	}
	c.r.retained.Put(key, physBounds, fn)
	c.r.queued = append(c.r.queued, fn)
}

func (c metalCanvas) needGL() {
	if !c.r.wantsGL {
		c.r.wantsGL = true
		log.Printf("qui: a widget needs raw OpenGL; switching this window from Metal to GL")
	}
}

// DrawTexture implements GPUCanvas (see metalCanvas).
func (c metalCanvas) DrawTexture(uint32, gfx.Rect, gfx.Rect, TextureOpts) { c.needGL() }

// QueueGLDraw implements GPUCanvas (see metalCanvas).
func (c metalCanvas) QueueGLDraw(func(GLState)) { c.needGL() }

// QueueRetainedGLDraw implements RetainedGLCanvas (see metalCanvas).
func (c metalCanvas) QueueRetainedGLDraw(any, gfx.Rect, func(GLState)) { c.needGL() }
