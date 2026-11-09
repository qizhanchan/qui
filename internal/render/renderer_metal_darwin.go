//go:build darwin && cgo && !qui_nometal

package render

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Metal -framework QuartzCore -framework CoreVideo -framework Foundation

#import <Metal/Metal.h>
#import <QuartzCore/QuartzCore.h>
#import <CoreVideo/CoreVideo.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

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
// scratch is the bounce texture pixel moves copy through.
@property (nonatomic, strong) id<MTLTexture> scratch;
@property (nonatomic, strong) id<MTLCommandBuffer> last;
@property (nonatomic, strong) CAMetalLayer* layer;
@property (nonatomic, assign) CVMetalTextureCacheRef cvCache;
// pixels: textures for DrawPixels, by the caller's key id.
@property (nonatomic, strong) NSMutableDictionary<NSNumber*, id<MTLTexture>>* pixels;
// yuv: the three plane textures DrawYUV420 keeps, by key id.
@property (nonatomic, strong) NSMutableDictionary<NSNumber*, NSArray<id<MTLTexture>>*>* yuv;
@property (nonatomic, strong) id<MTLRenderPipelineState> yuvPipeline;
// holds keeps CVMetalTextures (and so their pixel buffers' IOSurfaces)
// alive until the command buffer that sampled them has completed.
@property (nonatomic, strong) NSMutableArray* holds;
// The frame being encoded (between quiMetalBeginFrame and EndFrame).
@property (nonatomic, strong) id<MTLCommandBuffer> cmd;
@property (nonatomic, strong) id<MTLRenderCommandEncoder> enc;
@property (nonatomic, strong) id<CAMetalDrawable> drawable;
// lastShown is the texture of the last presented drawable, kept only when
// the layer allows reading it (tests: framebufferOnly = NO).
@property (nonatomic, strong) id<MTLTexture> lastShown;
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
     "}\n"
     "struct YUVConv { float4 c0; float4 c1; float4 c2; float4 off; float4 scale; };\n"
     "fragment float4 qui_yuv(VOut in [[stage_in]], texture2d<float> ty [[texture(0)]],\n"
     "                        texture2d<float> tu [[texture(1)]], texture2d<float> tv [[texture(2)]],\n"
     "                        sampler smp [[sampler(0)]], constant YUVConv& k [[buffer(0)]]) {\n"
     "    float3 yuv = float3(ty.sample(smp, in.uv).r, tu.sample(smp, in.uv).r, tv.sample(smp, in.uv).r);\n"
     "    yuv = (yuv - k.off.xyz) * k.scale.xyz;\n"
     "    float3x3 m = float3x3(k.c0.xyz, k.c1.xyz, k.c2.xyz);\n"
     "    return float4(clamp(m * yuv, 0.0, 1.0), 1.0);\n"
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
        id<MTLRenderPipelineState> yuvPipeline = quiMetalPipeline(device, lib, @"qui_yuv");
        if (layerPipeline == nil || texPipeline == nil || yuvPipeline == nil) return NULL;
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
        m.yuvPipeline = yuvPipeline;
        m.yuv = [NSMutableDictionary dictionary];
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
    [m.yuv removeObjectForKey:@(key)];
}

static id<MTLTexture> quiMetalSharedTexture(QuiMetal* m, MTLPixelFormat fmt, int w, int h) {
    MTLTextureDescriptor* td = [MTLTextureDescriptor texture2DDescriptorWithPixelFormat:fmt
                                                                                  width:w
                                                                                 height:h
                                                                              mipmapped:NO];
    td.usage = MTLTextureUsageShaderRead;
    td.storageMode = m.device.hasUnifiedMemory ? MTLStorageModeShared : MTLStorageModeManaged;
    return [m.device newTextureWithDescriptor:td];
}

// quiMetalDrawYUV420 uploads three 8-bit planes (Y full size, Cb / Cr
// half) into the textures kept under key and composites the src rect
// (luma pixels) at dst, converting YCbCr to RGB in the shader. conv is
// the column-major 3x3 matrix, then offset and scale (15 floats).
static int quiMetalDrawYUV420(void* ctx, uintptr_t key,
                              const uint8_t* p0, const uint8_t* p1, const uint8_t* p2,
                              const int* stride, const int* pw, const int* ph, const float* conv,
                              float sx, float sy, float sw, float sh, float x, float y, float w, float h) {
    const uint8_t* pix[3] = {p0, p1, p2};
    QuiMetal* m = (__bridge QuiMetal*)ctx;
    if (m.enc == nil) return 0;
    for (int i = 0; i < 3; i++) if (pix[i] == NULL || pw[i] <= 0 || ph[i] <= 0) return 0;
    NSArray<id<MTLTexture>>* tex = m.yuv[@(key)];
    BOOL ok = tex != nil;
    for (int i = 0; ok && i < 3; i++) ok = (int)tex[i].width == pw[i] && (int)tex[i].height == ph[i];
    if (!ok) {
        id<MTLTexture> t0 = quiMetalSharedTexture(m, MTLPixelFormatR8Unorm, pw[0], ph[0]);
        id<MTLTexture> t1 = quiMetalSharedTexture(m, MTLPixelFormatR8Unorm, pw[1], ph[1]);
        id<MTLTexture> t2 = quiMetalSharedTexture(m, MTLPixelFormatR8Unorm, pw[2], ph[2]);
        if (t0 == nil || t1 == nil || t2 == nil) return 0;
        tex = @[t0, t1, t2];
        m.yuv[@(key)] = tex;
    }
    // The previous frame may still be sampling these textures.
    [m.last waitUntilCompleted];
    for (int i = 0; i < 3; i++) {
        [tex[i] replaceRegion:MTLRegionMake2D(0, 0, pw[i], ph[i]) mipmapLevel:0
                    withBytes:pix[i] bytesPerRow:stride[i]];
    }
    float fw = (float)m.drawable.texture.width, fh = (float)m.drawable.texture.height;
    float lw = (float)pw[0], lh = (float)ph[0];
    if (fw <= 0 || fh <= 0) return 0;
    float r[8] = {
        x / fw * 2 - 1, 1 - y / fh * 2, (x + w) / fw * 2 - 1, 1 - (y + h) / fh * 2,
        sx / lw, sy / lh, (sx + sw) / lw, (sy + sh) / lh,
    };
    float k[20] = {
        conv[0], conv[1], conv[2], 0,  conv[3], conv[4], conv[5], 0,  conv[6], conv[7], conv[8], 0,
        conv[9], conv[10], conv[11], 0,  conv[12], conv[13], conv[14], 0,
    };
    [m.enc setRenderPipelineState:m.yuvPipeline];
    [m.enc setVertexBytes:r length:sizeof r atIndex:0];
    for (int i = 0; i < 3; i++) [m.enc setFragmentTexture:tex[i] atIndex:i];
    [m.enc setFragmentSamplerState:m.sampler atIndex:0];
    [m.enc setFragmentBytes:k length:sizeof k atIndex:0];
    [m.enc drawPrimitives:MTLPrimitiveTypeTriangleStrip vertexStart:0 vertexCount:4];
    return 1;
}

static id<MTLTexture> quiMetalPrivateTexture(QuiMetal* m, int width, int height) {
    MTLTextureDescriptor* td =
        [MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatRGBA8Unorm
                                                           width:width
                                                          height:height
                                                       mipmapped:NO];
    td.usage = MTLTextureUsageShaderRead;
    // GPU-only: every write goes through a blit, ordered with the frames
    // that sample it, so the CPU never waits for the GPU to let go.
    td.storageMode = MTLStorageModePrivate;
    return [m.device newTextureWithDescriptor:td];
}

// quiMetalBeginFrame brings the frame texture up to date with the CPU
// raster and opens a render pass on the next drawable, cleared to
// transparent (panels) or opaque black. Returns 1 when drawing can go on,
// 0 when there is no drawable (the texture is updated, the frame is not
// shown) and -1 when the texture could not be updated at all.
//
// The update is GPU work in the frame's own command buffer, in order:
// first the nmoves pixel moves (x, y, w, h, dx, dy each — Window.ScrollPixels
// done on the texture, through a scratch texture since a blit may not
// overlap itself), then the nrects damaged rectangles (x, y, w, h each)
// copied from the CPU raster through a staging buffer. A (re)allocated
// texture gets the whole frame instead. Because the GPU orders all of it
// after the previous frame's sampling, nothing here waits for that frame.
static int quiMetalBeginFrame(void* ctx, const uint8_t* pix, int stride, int width, int height,
                              const int32_t* rects, int nrects, const int32_t* moves, int nmoves) {
    QuiMetal* m = (__bridge QuiMetal*)ctx;
    if (m == nil || pix == NULL || width <= 0 || height <= 0) return -1;
    m.holds = [NSMutableArray array];
    if (m.cvCache) CVMetalTextureCacheFlush(m.cvCache, 0);
    int32_t full[4] = {0, 0, width, height};
    if (m.frame == nil || (int)m.frame.width != width || (int)m.frame.height != height) {
        m.frame = quiMetalPrivateTexture(m, width, height);
        m.scratch = nil;
        if (m.frame == nil) return -1;
        rects = full; nrects = 1;
        nmoves = 0; // nothing to move: the texture holds no frame yet
    }
    m.cmd = [m.queue commandBuffer];

    if (nmoves > 0 || nrects > 0) {
        id<MTLBlitCommandEncoder> blit = [m.cmd blitCommandEncoder];
        for (int i = 0; i < nmoves; i++) {
            const int32_t* mv = moves + i * 6;
            int x = mv[0], y = mv[1], w = mv[2], h = mv[3], dx = mv[4], dy = mv[5];
            // Source part that stays inside the rect after the move.
            int sx = x + (dx < 0 ? -dx : 0), sy = y + (dy < 0 ? -dy : 0);
            int cw = w - abs(dx), ch = h - abs(dy);
            if (cw <= 0 || ch <= 0 || x < 0 || y < 0 || x + w > width || y + h > height) continue;
            if (m.scratch == nil) m.scratch = quiMetalPrivateTexture(m, width, height);
            if (m.scratch == nil) continue;
            [blit copyFromTexture:m.frame sourceSlice:0 sourceLevel:0
                     sourceOrigin:MTLOriginMake(sx, sy, 0) sourceSize:MTLSizeMake(cw, ch, 1)
                        toTexture:m.scratch destinationSlice:0 destinationLevel:0
                destinationOrigin:MTLOriginMake(sx, sy, 0)];
            [blit copyFromTexture:m.scratch sourceSlice:0 sourceLevel:0
                     sourceOrigin:MTLOriginMake(sx, sy, 0) sourceSize:MTLSizeMake(cw, ch, 1)
                        toTexture:m.frame destinationSlice:0 destinationLevel:0
                destinationOrigin:MTLOriginMake(sx + dx, sy + dy, 0)];
        }
        if (nrects > 0) {
            size_t total = 0;
            for (int i = 0; i < nrects; i++) {
                const int32_t* r = rects + i * 4;
                if (r[2] > 0 && r[3] > 0) total += (size_t)r[2] * r[3] * 4;
            }
            id<MTLBuffer> staging = total ? [m.device newBufferWithLength:total
                                                                  options:MTLResourceStorageModeShared |
                                                                          MTLResourceCPUCacheModeWriteCombined]
                                          : nil;
            uint8_t* dst = staging.contents;
            size_t off = 0;
            for (int i = 0; staging && i < nrects; i++) {
                const int32_t* r = rects + i * 4;
                int x = r[0], y = r[1], w = r[2], h = r[3];
                if (w <= 0 || h <= 0 || x < 0 || y < 0 || x + w > width || y + h > height) continue;
                size_t row = (size_t)w * 4;
                for (int j = 0; j < h; j++) {
                    memcpy(dst + off + (size_t)j * row, pix + (size_t)(y + j) * stride + (size_t)x * 4, row);
                }
                [blit copyFromBuffer:staging sourceOffset:off sourceBytesPerRow:row
                     sourceBytesPerImage:row * h sourceSize:MTLSizeMake(w, h, 1)
                               toTexture:m.frame destinationSlice:0 destinationLevel:0
                       destinationOrigin:MTLOriginMake(x, y, 0)];
                off += row * h;
            }
        }
        [blit endEncoding];
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
    if (drawable == nil) {
        // Keep the texture update: commit it without presenting.
        [m.cmd commit];
        m.last = m.cmd;
        m.cmd = nil;
        return 0;
    }
    MTLRenderPassDescriptor* rp = [MTLRenderPassDescriptor renderPassDescriptor];
    rp.colorAttachments[0].texture = drawable.texture;
    rp.colorAttachments[0].loadAction = MTLLoadActionClear;
    rp.colorAttachments[0].clearColor = MTLClearColorMake(0, 0, 0, layer.opaque ? 1 : 0);
    rp.colorAttachments[0].storeAction = MTLStoreActionStore;
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
    // The previous frame may still be sampling this texture. (Pushed RGBA
    // video is the only caller; zero-copy pixel buffers never get here.)
    [m.last waitUntilCompleted];
    [tex replaceRegion:MTLRegionMake2D(0, 0, pw, ph) mipmapLevel:0 withBytes:pix bytesPerRow:stride];
    quiMetalQuad(m, tex, sx / pw, sy / ph, (sx + sw) / pw, (sy + sh) / ph, x, y, w, h);
    return 1;
}

// quiMetalReadFrame copies the frame texture into out (width x height
// RGBA, stride bytes per row), waiting for the GPU: a debug check that the
// texture matches the CPU raster (QUI_DEBUG_METAL_VERIFY).
static int quiMetalReadFrame(void* ctx, uint8_t* out, int stride, int width, int height) {
    @autoreleasepool {
        QuiMetal* m = (__bridge QuiMetal*)ctx;
        if (m.frame == nil || (int)m.frame.width != width || (int)m.frame.height != height) return 0;
        size_t row = (size_t)width * 4;
        id<MTLBuffer> buf = [m.device newBufferWithLength:row * height options:MTLResourceStorageModeShared];
        id<MTLCommandBuffer> cmd = [m.queue commandBuffer];
        id<MTLBlitCommandEncoder> blit = [cmd blitCommandEncoder];
        [blit copyFromTexture:m.frame sourceSlice:0 sourceLevel:0 sourceOrigin:MTLOriginMake(0, 0, 0)
                   sourceSize:MTLSizeMake(width, height, 1) toBuffer:buf destinationOffset:0
          destinationBytesPerRow:row destinationBytesPerImage:row * height];
        [blit endEncoding];
        [cmd commit];
        [cmd waitUntilCompleted];
        const uint8_t* src = buf.contents;
        for (int y = 0; y < height; y++) memcpy(out + (size_t)y * stride, src + (size_t)y * row, row);
        return 1;
    }
}

// quiMetalNewTestLayer is an offscreen CAMetalLayer (in no window) whose
// drawables can be read back: what tests render into. Retained (+1).
static void* quiMetalNewTestLayer(int width, int height) {
    @autoreleasepool {
        CAMetalLayer* layer = [CAMetalLayer layer];
        layer.framebufferOnly = NO;
        layer.opaque = YES;
        layer.drawableSize = CGSizeMake(width, height);
        return (__bridge_retained void*)layer;
    }
}

static void quiMetalReleaseTestLayer(void* layer) {
    if (layer) CFRelease(layer);
}

// quiMetalReadShown copies the last presented drawable (BGRA) into out as
// RGBA, waiting for the GPU. 0 when there is none (or it is not readable).
static int quiMetalReadShown(void* ctx, uint8_t* out, int stride, int width, int height) {
    @autoreleasepool {
        QuiMetal* m = (__bridge QuiMetal*)ctx;
        id<MTLTexture> t = m.lastShown;
        if (t == nil || (int)t.width != width || (int)t.height != height) return 0;
        [m.last waitUntilCompleted];
        size_t row = (size_t)width * 4;
        id<MTLBuffer> buf = [m.device newBufferWithLength:row * height options:MTLResourceStorageModeShared];
        id<MTLCommandBuffer> cmd = [m.queue commandBuffer];
        id<MTLBlitCommandEncoder> blit = [cmd blitCommandEncoder];
        [blit copyFromTexture:t sourceSlice:0 sourceLevel:0 sourceOrigin:MTLOriginMake(0, 0, 0)
                   sourceSize:MTLSizeMake(width, height, 1) toBuffer:buf destinationOffset:0
          destinationBytesPerRow:row destinationBytesPerImage:row * height];
        [blit endEncoding];
        [cmd commit];
        [cmd waitUntilCompleted];
        const uint8_t* src = buf.contents;
        for (int y = 0; y < height; y++) {
            const uint8_t* s = src + (size_t)y * row;
            uint8_t* d = out + (size_t)y * stride;
            for (int x = 0; x < width; x++) {
                d[4*x] = s[4*x+2]; d[4*x+1] = s[4*x+1]; d[4*x+2] = s[4*x]; d[4*x+3] = s[4*x+3];
            }
        }
        return 1;
    }
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
        m.lastShown = m.layer.framebufferOnly ? nil : drawable.texture;
        // CVMetalTextures (and the IOSurfaces under them) must outlive the
        // GPU's reads: the handler keeps them until the frame completes.
        NSArray* holds = m.holds;
        [cmd addCompletedHandler:^(id<MTLCommandBuffer> b) { (void)holds; }];
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
	"os"
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

	// damageRects (SetDamageRects) are the frame's painted rectangles,
	// uploaded one by one; moves are pending ScrollPixels moves for the
	// texture, as x, y, w, h, dx, dy. stale forces the next upload to
	// cover the whole frame (an update was lost).
	damageRects []image.Rectangle
	moves       []int32
	stale       bool
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
	rects := r.damageRects
	r.damage, r.damageRects = image.Rectangle{}, nil
	if r.ctx == nil || r.img == nil {
		r.queued = r.queued[:0]
		r.moves = r.moves[:0]
		r.retained.EndFrame()
		return
	}
	img := r.img
	var up []image.Rectangle
	switch {
	case r.stale:
		up = []image.Rectangle{img.Rect}
	case len(rects) > 0:
		// Moves are done on the GPU, so only what was painted goes up.
		for _, d := range rects {
			if d = d.Inset(-1).Intersect(img.Rect); !d.Empty() {
				up = append(up, d)
			}
		}
	default:
		d := damage.Intersect(img.Rect)
		if d.Empty() {
			d = img.Rect
		} else {
			// One-pixel guard band, as GLRenderer's upload: the damage is
			// snapped outward already, this only absorbs rounding.
			d = d.Inset(-1).Intersect(img.Rect)
		}
		up = []image.Rectangle{d}
	}
	r.present(up, damage, false)
}

// RedrawRetained implements RetainedGLRenderer for Window.RequestGLRedraw:
// a frame that replays the retained draws under the 2D layer the last
// frame left, without rasterizing or uploading it.
func (r *MetalRenderer) RedrawRetained() bool {
	if r == nil || r.ctx == nil || r.img == nil || !r.painted || r.retained.Len() == 0 {
		return false
	}
	var up []image.Rectangle
	if r.stale {
		up = []image.Rectangle{r.img.Rect}
	}
	r.present(up, image.Rectangle{}, true)
	return true
}

// present brings the texture up to date (pending moves, then the up
// rectangles of the CPU frame), replays the retained draws the frame's
// damage left alive plus this frame's fresh ones, lays the 2D layer on
// top and shows it.
func (r *MetalRenderer) present(up []image.Rectangle, damage image.Rectangle, damageNone bool) {
	img := r.img
	w, h := img.Rect.Dx(), img.Rect.Dy()
	replay := r.retained.CarryOver(damage, damageNone)
	fresh := r.queued
	r.queued = r.queued[:0]
	defer r.retained.EndFrame()
	rects := make([]int32, 0, 4*len(up))
	for _, u := range up {
		u = u.Sub(img.Rect.Min)
		rects = append(rects, int32(u.Min.X), int32(u.Min.Y), int32(u.Dx()), int32(u.Dy()))
	}
	var rp, mp *C.int32_t
	if len(rects) > 0 {
		rp = (*C.int32_t)(unsafe.Pointer(&rects[0]))
	}
	if len(r.moves) > 0 {
		mp = (*C.int32_t)(unsafe.Pointer(&r.moves[0]))
	}
	res := C.quiMetalBeginFrame(r.ctx, (*C.uint8_t)(unsafe.Pointer(&img.Pix[0])), C.int(img.Stride),
		C.int(w), C.int(h), rp, C.int(len(up)), mp, C.int(len(r.moves)/6))
	r.moves = r.moves[:0]
	r.stale = res < 0
	if res <= 0 {
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
	if metalVerify {
		r.verifyTexture()
	}
}

// metalVerify (QUI_DEBUG_METAL_VERIFY=1) reads the frame texture back
// after every present and compares it with the CPU raster — slow, for
// checking the GPU-side moves and partial uploads.
var metalVerify = os.Getenv("QUI_DEBUG_METAL_VERIFY") != ""

func (r *MetalRenderer) verifyTexture() {
	img := r.img
	got := image.NewRGBA(img.Rect)
	if C.quiMetalReadFrame(r.ctx, (*C.uint8_t)(unsafe.Pointer(&got.Pix[0])), C.int(got.Stride),
		C.int(img.Rect.Dx()), C.int(img.Rect.Dy())) == 0 {
		return
	}
	diff, first := 0, image.Point{-1, -1}
	for y := 0; y < img.Rect.Dy(); y++ {
		a := img.Pix[y*img.Stride : y*img.Stride+img.Rect.Dx()*4]
		b := got.Pix[y*got.Stride : y*got.Stride+img.Rect.Dx()*4]
		if string(a) == string(b) {
			continue
		}
		for x := 0; x < len(a); x += 4 {
			if a[x] != b[x] || a[x+1] != b[x+1] || a[x+2] != b[x+2] || a[x+3] != b[x+3] {
				if diff == 0 {
					first = image.Point{x / 4, y}
				}
				diff++
			}
		}
	}
	if diff > 0 {
		log.Printf("qui: Metal texture differs from the CPU frame in %d pixels (first at %v)", diff, first)
	}
}

// SetDamageRects implements DamageRectsRenderer: the rectangles (physical)
// the coming frame paints.
func (r *MetalRenderer) SetDamageRects(rects []image.Rectangle) {
	if r != nil {
		r.damageRects = append(r.damageRects[:0], rects...)
	}
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

// keyID is the integer the C side files key's textures under.
func (r *MetalRenderer) keyID(key any) uintptr {
	id, ok := r.keys[key]
	if !ok {
		if r.keys == nil {
			r.keys = map[any]uintptr{}
		}
		r.nextID++
		id = r.nextID
		r.keys[key] = id
	}
	return id
}

func (c metalCompositor) DrawYUV420(key any, planes [3]YUVPlane, m [9]float32, off, scale [3]float32, src, dst gfx.Rect) bool {
	if key == nil {
		return false
	}
	var pix [3]*C.uint8_t
	var stride, w, h [3]C.int
	for i, p := range planes {
		if p.Pix == nil {
			return false
		}
		pix[i] = (*C.uint8_t)(p.Pix)
		stride[i], w[i], h[i] = C.int(p.Stride), C.int(p.W), C.int(p.H)
	}
	var conv [15]C.float
	for i, v := range m {
		conv[i] = C.float(v)
	}
	for i := 0; i < 3; i++ {
		conv[9+i], conv[12+i] = C.float(off[i]), C.float(scale[i])
	}
	// The planes go as separate arguments: cgo forbids passing Go memory
	// that itself holds Go pointers.
	return C.quiMetalDrawYUV420(c.r.ctx, C.uintptr_t(c.r.keyID(key)), pix[0], pix[1], pix[2],
		&stride[0], &w[0], &h[0], &conv[0],
		C.float(src.X), C.float(src.Y), C.float(src.W), C.float(src.H),
		C.float(dst.X), C.float(dst.Y), C.float(dst.W), C.float(dst.H)) != 0
}

func (c metalCompositor) DrawPixels(key any, pix unsafe.Pointer, stride, w, h int, bgra bool, src, dst gfx.Rect) bool {
	if key == nil || pix == nil {
		return false
	}
	r := c.r
	id := r.keyID(key)
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

// CanScrollPixels implements PixelScroller.
func (r *MetalRenderer) CanScrollPixels() bool { return r != nil && r.img != nil }

// ScrollPixels implements PixelScroller: the CPU frame moves now, the
// texture on the GPU at the next present.
func (r *MetalRenderer) ScrollPixels(rect image.Rectangle, dx, dy int) bool {
	if r == nil || !scrollImage(r.img, rect, dx, dy) {
		return false
	}
	// The texture makes the same move on the GPU before this frame's
	// upload, so the moved pixels never cross the bus again.
	rr := rect.Sub(r.img.Rect.Min)
	r.moves = append(r.moves, int32(rr.Min.X), int32(rr.Min.Y), int32(rr.Dx()), int32(rr.Dy()), int32(dx), int32(dy))
	return true
}

// newTestMetalLayer / releaseTestMetalLayer / readShown exist for the
// package's tests (test files cannot use cgo): an offscreen layer to render
// into and a readback of what the last frame presented.
func newTestMetalLayer(w, h int) unsafe.Pointer { return C.quiMetalNewTestLayer(C.int(w), C.int(h)) }

func releaseTestMetalLayer(l unsafe.Pointer) { C.quiMetalReleaseTestLayer(l) }

func (r *MetalRenderer) readShown() *image.RGBA {
	if r.ctx == nil || r.img == nil {
		return nil
	}
	out := image.NewRGBA(r.img.Rect)
	if C.quiMetalReadShown(r.ctx, (*C.uint8_t)(unsafe.Pointer(&out.Pix[0])), C.int(out.Stride),
		C.int(out.Rect.Dx()), C.int(out.Rect.Dy())) == 0 {
		return nil
	}
	return out
}
