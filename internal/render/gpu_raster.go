package render

import (
	"container/list"
	"encoding/binary"
	"image"
	"math"
	"os"
	"sync"
	"unsafe"

	"github.com/qizhanchan/qui/internal/gfx"
	"github.com/qizhanchan/qui/internal/wgpu"
)

// GPU rasterization of Pictures (RFC 0004 R5).
//
// A Picture is played onto gpuRasterBackend, which turns the backend calls
// into instanced quads drawn into a texture with WebGPU, the way Chromium's
// GPU raster turns a display list into GPU commands:
//
//   - solid fills and rounded rectangles: an analytic coverage (signed
//     distance) shader;
//   - images, including rotated and scaled ones: textured quads, sampled
//     bilinearly from a texture cache (minified images go through the CPU
//     scale cache first, as on the CPU path);
//   - text: the glyph coverage the canvas frontend computed, uploaded as a
//     mask and tinted;
//   - layers (SaveLayer, ClipPath): offscreen textures composited back with
//     their opacity and clip mask.
//
// Everything else — paths, strokes, shadows, vectors, legacy text — is
// rasterized on the CPU into a small transparent image exactly as the CPU
// backend would, and composited as a texture. What the GPU cannot do
// faithfully — backdrop filters, layers with image or color filters or a
// blend mode other than source-over, Clear — fails the raster, and the
// caller rasterizes the picture on the CPU instead.
//
// The result is not bit-identical to the CPU raster (the GPU blends and
// samples in its own precision), but equal to within a couple of levels.

var gpuRasterOff = os.Getenv("QUI_GPU_RASTER") == "0"

// GPURasterizer is a renderer that can rasterize Pictures into textures it
// keeps (WebGPURenderer). Compositor draws show them (TextureCompositor).
type GPURasterizer interface {
	// RasterizePicture plays pic's rect (device pixels of the recording)
	// into the texture cached under key, tagged version; false when the
	// GPU cannot rasterize it (the caller rasterizes on the CPU).
	RasterizePicture(key any, version uint64, pic *Picture, rect image.Rectangle) bool
}

// TextureCompositor is a Compositor that can show a texture the renderer
// rasterized (GPURasterizer.RasterizePicture).
type TextureCompositor interface {
	// DrawTexture shows the src part of the texture cached under key at
	// dst; false when there is none with that version.
	DrawTexture(key any, version uint64, src, dst gfx.Rect) bool
}

// grInstFloats is one quad instance:
//
//	m      [0:6]   unit square → NDC (affine, row-major a b tx / c d ty)
//	uv     [6:10]  texture coordinates over the square
//	color  [10:14] premultiplied color (solid / tint) or opacity (textures)
//	clip   [14:18] target pixels the quad may touch
//	params [18:22] mode, corner radius, shape inset in the quad, edge antialiasing
//	size   [22:24] the shape's size in pixels (rounded rectangles)
//	mask   [24:28] mask texture coordinates over the square
const grInstFloats = 28

const (
	grSolid    = 0 // signed-distance rounded rectangle, color
	grTex      = 1 // premultiplied texture × opacity × mask
	grTexMask  = 2 // R8 coverage texture × color
	grReplace  = 3 // write color, no blending (ClearRect)
	grSolidRaw = 4 // color, no coverage computation (whole pixels)
)

const gpuRasterWGSL = `
struct VOut {
    @builtin(position) pos: vec4f,
    @location(0) local: vec2f,
    @location(1) uv: vec2f,
    @location(2) muv: vec2f,
    @location(3) @interpolate(flat) color: vec4f,
    @location(4) @interpolate(flat) clip: vec4f,
    @location(5) @interpolate(flat) params: vec4f,
    @location(6) @interpolate(flat) size: vec2f,
};
@vertex fn vs_main(@builtin(vertex_index) vi: u32,
                   @location(0) m0: vec3f, @location(1) m1: vec3f,
                   @location(2) uv: vec4f, @location(3) color: vec4f,
                   @location(4) clip: vec4f, @location(5) params: vec4f,
                   @location(6) size: vec2f, @location(7) muv: vec4f) -> VOut {
    let c = vec2f(f32(vi & 1u), f32(vi >> 1u));
    var o: VOut;
    o.pos = vec4f(m0.x * c.x + m0.y * c.y + m0.z, m1.x * c.x + m1.y * c.y + m1.z, 0.0, 1.0);
    o.local = c;
    o.uv = mix(uv.xy, uv.zw, c);
    o.muv = mix(muv.xy, muv.zw, c);
    o.color = color;
    o.clip = clip;
    o.params = params;
    o.size = size;
    return o;
}

@group(0) @binding(0) var tex: texture_2d<f32>;
@group(0) @binding(1) var mask: texture_2d<f32>;
@group(0) @binding(2) var smp: sampler;

fn rrect(p: vec2f, size: vec2f, r: f32) -> f32 {
    let half = size * 0.5;
    let q = abs(p - half) - (half - vec2f(r));
    return length(max(q, vec2f(0.0))) + min(max(q.x, q.y), 0.0) - r;
}

@fragment fn fs_main(in: VOut) -> @location(0) vec4f {
    let t = textureSampleLevel(tex, smp, in.uv, 0.0);
    let mk = textureSampleLevel(mask, smp, in.muv, 0.0).r;
    let fw = max(fwidth(in.local), vec2f(1e-6));
    let p = in.pos.xy;
    if (p.x < in.clip.x || p.y < in.clip.y || p.x >= in.clip.z || p.y >= in.clip.w) {
        discard;
    }
    let mode = i32(in.params.x + 0.5);
    if (mode == 0) {
        let inset = in.params.z;
        let d = rrect(in.local * in.size - vec2f(inset), in.size - vec2f(2.0 * inset), in.params.y);
        return in.color * clamp(0.5 - d, 0.0, 1.0);
    }
    if (mode == 3 || mode == 4) {
        return in.color;
    }
    var cov = 1.0;
    if (in.params.w > 0.5) {
        let e = min(in.local, vec2f(1.0) - in.local) / fw;
        cov = clamp(min(e.x, e.y) + 0.5, 0.0, 1.0);
    }
    if (mode == 2) {
        return in.color * (t.r * mk * cov);
    }
    return t * (in.color.a * mk * cov);
}
`

type gpuRasterPipes struct {
	blend, replace *wgpu.RenderPipeline
	layout         *wgpu.BindGroupLayout
	linear         *wgpu.Sampler
	white          *gpuTex // 1×1 opaque white: "no texture" / "no mask"
}

var (
	grPipes    *gpuRasterPipes
	grPipesErr error
	grPipesMu  sync.Mutex
)

func gpuRasterPipesFor(dev *wgpu.Device) (*gpuRasterPipes, error) {
	grPipesMu.Lock()
	defer grPipesMu.Unlock()
	if grPipes != nil || grPipesErr != nil {
		return grPipes, grPipesErr
	}
	mod, err := dev.CreateShaderModule("qui gpu raster", gpuRasterWGSL)
	if err != nil {
		grPipesErr = err
		return nil, err
	}
	defer mod.Release()
	attr := func(loc uint32, off uint64, f wgpu.VertexFormat) wgpu.VertexAttribute {
		return wgpu.VertexAttribute{Format: f, Offset: off * 4, Location: loc}
	}
	bufs := []wgpu.VertexBufferLayout{{
		Stride: grInstFloats * 4, StepMode: wgpu.StepInstance,
		Attributes: []wgpu.VertexAttribute{
			attr(0, 0, wgpu.VertexFloat32x3), attr(1, 3, wgpu.VertexFloat32x3),
			attr(2, 6, wgpu.VertexFloat32x4), attr(3, 10, wgpu.VertexFloat32x4),
			attr(4, 14, wgpu.VertexFloat32x4), attr(5, 18, wgpu.VertexFloat32x4),
			attr(6, 22, wgpu.VertexFloat32x2), attr(7, 24, wgpu.VertexFloat32x4),
		},
	}}
	mk := func(label string, blend *wgpu.BlendState) (*wgpu.RenderPipeline, error) {
		return dev.CreateRenderPipeline(wgpu.RenderPipelineDescriptor{
			Label: label, Module: mod, VertexEntry: "vs_main", FragmentEntry: "fs_main", Buffers: bufs,
			Topology: wgpu.TopologyTriangleStrip,
			Targets:  []wgpu.ColorTarget{{Format: wgpu.FormatRGBA8Unorm, Blend: blend}},
		})
	}
	p := &gpuRasterPipes{}
	if p.blend, err = mk("qui gpu raster", wgpu.BlendPremultiplied()); err != nil {
		grPipesErr = err
		return nil, err
	}
	if p.replace, err = mk("qui gpu raster replace", nil); err != nil {
		grPipesErr = err
		return nil, err
	}
	p.layout = p.blend.BindGroupLayout(0)
	p.linear = dev.CreateSampler(wgpu.FilterLinear, wgpu.AddressClampToEdge)
	p.white = newGPUTex(dev, 1, 1, wgpu.FormatRGBA8Unorm)
	if p.white == nil {
		grPipesErr = errGPURaster
		return nil, grPipesErr
	}
	px := [4]uint8{255, 255, 255, 255}
	dev.WriteTexture(p.white.tex, 0, 0, 1, 1, unsafe.Pointer(&px[0]), 4)
	grPipes = p
	return p, nil
}

type gpuRasterError string

func (e gpuRasterError) Error() string { return string(e) }

const errGPURaster = gpuRasterError("render: GPU raster unavailable")

// gpuTex is a texture with its view.
type gpuTex struct {
	tex  *wgpu.Texture
	view *wgpu.TextureView
	w, h int
}

func newGPUTex(dev *wgpu.Device, w, h int, f wgpu.TextureFormat) *gpuTex {
	t := dev.CreateTexture(wgpu.TextureDescriptor{Label: "qui gpu raster", Width: w, Height: h, Format: f,
		Usage: wgpu.UsageTextureBinding | wgpu.UsageCopyDst | wgpu.UsageRenderAttachment | wgpu.UsageCopySrc})
	if t == nil {
		return nil
	}
	return &gpuTex{tex: t, view: t.CreateView(), w: w, h: h}
}

func (t *gpuTex) release() {
	if t == nil {
		return
	}
	t.view.Release()
	t.tex.Destroy()
}

// uploadRGBA uploads premultiplied RGBA pixels.
func uploadRGBA(dev *wgpu.Device, img *image.RGBA) *gpuTex {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	if w <= 0 || h <= 0 {
		return nil
	}
	t := newGPUTex(dev, w, h, wgpu.FormatRGBA8Unorm)
	if t == nil {
		return nil
	}
	dev.WriteTexture(t.tex, 0, 0, w, h, unsafe.Pointer(&img.Pix[img.PixOffset(img.Rect.Min.X, img.Rect.Min.Y)]), img.Stride)
	return t
}

func uploadR8(dev *wgpu.Device, pix []uint8, w, h, stride int) *gpuTex {
	if w <= 0 || h <= 0 {
		return nil
	}
	t := newGPUTex(dev, w, h, wgpu.FormatR8Unorm)
	if t == nil {
		return nil
	}
	dev.WriteTexture(t.tex, 0, 0, w, h, unsafe.Pointer(&pix[0]), stride)
	return t
}

// ---- image textures ----

// gpuImageCache keeps uploaded images (decoded photos are drawn again and
// again), within a byte budget.
type gpuImageCache struct {
	lru   *list.List
	items map[image.Image]*list.Element
	bytes int
}

type gpuImageEntry struct {
	img image.Image
	t   *gpuTex
}

const gpuImageBudget = 192 << 20

var grImages = &gpuImageCache{lru: list.New(), items: map[image.Image]*list.Element{}}

// texture returns img uploaded (premultiplied RGBA). UI thread, under the
// renderer's lock.
func (c *gpuImageCache) texture(dev *wgpu.Device, rgba *image.RGBA) *gpuTex {
	var img image.Image = rgba
	if el, ok := c.items[img]; ok {
		c.lru.MoveToFront(el)
		return el.Value.(*gpuImageEntry).t
	}
	t := uploadRGBA(dev, rgba)
	if t == nil {
		return nil
	}
	c.items[img] = c.lru.PushFront(&gpuImageEntry{img: img, t: t})
	c.bytes += t.w * t.h * 4
	return t
}

// trim evicts past the budget. Called after a raster is submitted, never
// during one: its commands may use any texture it looked up.
func (c *gpuImageCache) trim() {
	for c.bytes > gpuImageBudget && c.lru.Len() > 1 {
		old := c.lru.Back()
		e := old.Value.(*gpuImageEntry)
		c.lru.Remove(old)
		delete(c.items, e.img)
		c.bytes -= e.t.w * e.t.h * 4
		e.t.release()
	}
}

// ---- the backend ----

type grKind uint8

const (
	grDraw grKind = iota
	grPush
	grPop
)

type grCmd struct {
	kind    grKind
	inst    [grInstFloats]float32
	tex     *gpuTex
	mask    *gpuTex
	replace bool
	layer   image.Rectangle // push: the layer's rectangle, in its parent's coordinates
	alpha   float32         // pop: composite opacity
	clip    image.Rectangle // pop: composite clip, parent coordinates
	maskR   image.Rectangle // pop: mask rectangle, parent coordinates
}

// grTarget is a render target on the backend's stack: the tile itself or
// a layer, with its origin in tile coordinates.
type grTarget struct {
	rect image.Rectangle // in tile coordinates
	dead bool            // empty layer: draws are dropped
}

type gpuRasterBackend struct {
	dev       *wgpu.Device
	w, h      int
	cmds      []grCmd
	stack     []grTarget
	failed    bool
	transient []*gpuTex
}

func (g *gpuRasterBackend) top() grTarget { return g.stack[len(g.stack)-1] }

func (g *gpuRasterBackend) fail() { g.failed = true }

// quad appends a draw whose unit square maps through p (tile pixels).
func (g *gpuRasterBackend) quad(p gfx.Matrix, uv [4]float32, color [4]float32, clip image.Rectangle,
	mode int, radius float32, aa bool, size [2]float32, tex, mask *gpuTex, muv [4]float32, replace bool) {
	t := g.top()
	if t.dead || g.failed {
		return
	}
	clip = clip.Intersect(t.rect)
	if clip.Empty() {
		return
	}
	// tile pixels → this target's pixels → NDC
	tw, th := float32(t.rect.Dx()), float32(t.rect.Dy())
	ox, oy := float32(t.rect.Min.X), float32(t.rect.Min.Y)
	toNDC := gfx.Matrix{A: 2 / tw, TX: -1 - 2*ox/tw, D: -2 / th, TY: 1 + 2*oy/th}
	m := toNDC.Concat(p)
	var c grCmd
	c.kind = grDraw
	c.tex, c.mask, c.replace = tex, mask, replace
	in := &c.inst
	in[0], in[1], in[2] = m.A, m.B, m.TX
	in[3], in[4], in[5] = m.C, m.D, m.TY
	copy(in[6:10], uv[:])
	copy(in[10:14], color[:])
	cl := clip.Sub(t.rect.Min)
	in[14], in[15], in[16], in[17] = float32(cl.Min.X), float32(cl.Min.Y), float32(cl.Max.X), float32(cl.Max.Y)
	in[18], in[19] = float32(mode), radius
	if aa {
		in[21] = 1
	}
	in[22], in[23] = size[0], size[1]
	copy(in[24:28], muv[:])
	g.cmds = append(g.cmds, c)
}

var fullUV = [4]float32{0, 0, 1, 1}

func rectMatrix(r gfx.Rect) gfx.Matrix { return gfx.Matrix{A: r.W, TX: r.X, D: r.H, TY: r.Y} }

func premul(c gfx.Color) [4]float32 {
	a := clamp01(c.A)
	return [4]float32{clamp01(c.R) * a, clamp01(c.G) * a, clamp01(c.B) * a, a}
}

func clamp01(v float32) float32 { return max(0, min(1, v)) }

func pixRect(r image.Rectangle) gfx.Rect { return pixelRectToRect(r) }

func (g *gpuRasterBackend) Begin(gfx.Size)        {}
func (g *gpuRasterBackend) End()                  {}
func (g *gpuRasterBackend) CPUImage() *image.RGBA { return nil }

func (g *gpuRasterBackend) Clear(gfx.Color) { g.fail() }

func (g *gpuRasterBackend) ClearRect(dst image.Rectangle) {
	g.quad(rectMatrix(pixRect(dst)), fullUV, [4]float32{}, dst, grReplace, 0, false, [2]float32{}, nil, nil, fullUV, true)
}

func (g *gpuRasterBackend) FillRect(dst image.Rectangle, color gfx.Color) {
	if color.A <= 0 {
		return
	}
	g.quad(rectMatrix(pixRect(dst)), fullUV, premul(color), dst, grSolidRaw, 0, false, [2]float32{}, nil, nil, fullUV, false)
}

func (g *gpuRasterBackend) FillRoundedRect(rect gfx.Rect, radius float32, clip gfx.Rect, color gfx.Color) {
	if color.A <= 0 || rect.W <= 0 || rect.H <= 0 {
		return
	}
	radius = max(0, min(radius, min(rect.W, rect.H)/2))
	// The quad carries a pixel of margin for the antialiased edge; the
	// shader measures the shape inset by it (params.z).
	q := gfx.Rect{X: rect.X - 1, Y: rect.Y - 1, W: rect.W + 2, H: rect.H + 2}
	g.quad(rectMatrix(q), fullUV, premul(color), rectToPixelRect(clip), grSolid, radius, false,
		[2]float32{q.W, q.H}, nil, nil, fullUV, false)
	if n := len(g.cmds); n > 0 && g.cmds[n-1].kind == grDraw && g.cmds[n-1].inst[18] == grSolid {
		g.cmds[n-1].inst[20] = 1
	}
}

// cpuOp rasterizes one backend call on the CPU into a transparent image
// over bounds (tile pixels) and composites it: exactly what the CPU
// backend would have drawn over the target there.
func (g *gpuRasterBackend) cpuOp(bounds image.Rectangle, rec func(r *PictureRecorder)) {
	t := g.top()
	if t.dead || g.failed {
		return
	}
	bounds = bounds.Intersect(t.rect)
	if bounds.Empty() {
		return
	}
	pr := &PictureRecorder{pic: &Picture{bounds: bounds}}
	rec(pr)
	pic := pr.Finish()
	if pic.Empty() {
		return
	}
	img := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	pic.RasterizeInto(img, bounds.Min)
	tx := uploadRGBA(g.dev, img)
	if tx == nil {
		g.fail()
		return
	}
	g.transient = append(g.transient, tx)
	g.quad(rectMatrix(pixRect(bounds)), fullUV, [4]float32{1, 1, 1, 1}, bounds, grTex, 0, false, [2]float32{}, tx, nil, fullUV, false)
}

func (g *gpuRasterBackend) StrokeRect(rect gfx.Rect, clip gfx.Rect, color gfx.Color, width float32) {
	g.cpuOp(outPix(clip), func(r *PictureRecorder) { r.StrokeRect(rect, clip, color, width) })
}

func (g *gpuRasterBackend) StrokeRoundedRect(rect gfx.Rect, radius float32, clip gfx.Rect, color gfx.Color, width float32) {
	g.cpuOp(outPix(clip), func(r *PictureRecorder) { r.StrokeRoundedRect(rect, radius, clip, color, width) })
}

func (g *gpuRasterBackend) FillPath(subs [][]gfx.Point, clip image.Rectangle, paint gfx.Paint, inv gfx.Matrix) {
	g.cpuOp(clip.Intersect(pointsBounds(subs, 2)), func(r *PictureRecorder) { r.FillPath(subs, clip, paint, inv) })
}

func (g *gpuRasterBackend) StrokePath(subs [][]gfx.Point, closed []bool, clip image.Rectangle, width float32, paint gfx.Paint) {
	pad := float32(math.Ceil(float64(width)))*2 + paint.MiterLimit*width + 2
	g.cpuOp(clip.Intersect(pointsBounds(subs, pad)), func(r *PictureRecorder) { r.StrokePath(subs, closed, clip, width, paint) })
}

func (g *gpuRasterBackend) DrawLine(p1, p2 gfx.Point, clip gfx.Rect, color gfx.Color, width float32, cap gfx.StrokeCap) {
	g.cpuOp(outPix(clip), func(r *PictureRecorder) { r.DrawLine(p1, p2, clip, color, width, cap) })
}

func (g *gpuRasterBackend) DrawPolyline(pts []gfx.Point, clip gfx.Rect, color gfx.Color, width float32) {
	g.cpuOp(outPix(clip), func(r *PictureRecorder) { r.DrawPolyline(pts, clip, color, width) })
}

func (g *gpuRasterBackend) DrawShadow(rect gfx.Rect, radius float32, spec gfx.ElevationSpec, c gfx.Color, clip gfx.Rect) {
	reach := float32(math.Ceil(float64(max(spec.Blur, 0)))) + 2 + max(spec.Spread, 0)
	area := gfx.Rect{X: rect.X + spec.X - reach - 1, Y: rect.Y + spec.Y - reach - 1, W: rect.W + 2*reach + 2, H: rect.H + 2*reach + 2}
	if spec.Inset {
		area = rect
	}
	g.cpuOp(outPix(clip).Intersect(outPix(area)), func(r *PictureRecorder) { r.DrawShadow(rect, radius, spec, c, clip) })
}

func (g *gpuRasterBackend) DrawText(text string, rect gfx.Rect, clip gfx.Rect, color gfx.Color, font gfx.Font) {
	g.cpuOp(outPix(clip), func(r *PictureRecorder) { r.DrawText(text, rect, clip, color, font) })
}

func (g *gpuRasterBackend) DrawVector(src gfx.VectorSource, dst gfx.Rect, clip gfx.Rect, tint gfx.Color) {
	g.cpuOp(outPix(clip).Intersect(outPix(dst)), func(r *PictureRecorder) { r.DrawVector(src, dst, clip, tint) })
}

// imageTexture is img uploaded at a size suited to drawing it through m
// (image pixels → tile pixels), and the matrix that draws that texture.
//
// The image work a texture needs — converting a decoded photo to
// premultiplied RGBA, scaling it down — is not done here, on the UI
// thread: a picture whose images are not ready fails, the caller
// rasterizes it on a CPU worker meanwhile, and the image is prepared in
// the background for the next raster (Chromium's image decode tasks run
// ahead of raster the same way).
func (g *gpuRasterBackend) imageTexture(img image.Image, m gfx.Matrix) (*gpuTex, gfx.Matrix) {
	src, mm, ready := scaledForDrawReady(img, m)
	if !ready {
		prepareImage(img, m)
		g.fail()
		return nil, m
	}
	rgba := src.(*image.RGBA)
	var t *gpuTex
	if gfx.IsVolatile(img) || gfx.IsVolatile(rgba) {
		// Not cached: its pixels change (or it is a per-recording copy).
		if t = uploadRGBA(g.dev, rgba); t != nil {
			g.transient = append(g.transient, t)
		}
	} else {
		t = grImages.texture(g.dev, rgba)
	}
	if t == nil {
		g.fail()
	}
	return t, mm
}

// prepareImage has img's scaled RGBA copy for drawing through m made on a
// background goroutine (at most a few at a time, each image once).
func prepareImage(img image.Image, m gfx.Matrix) {
	if _, busy := grPreparing.LoadOrStore(img, true); busy {
		return
	}
	go func() {
		grPrepSem <- struct{}{}
		defer func() { <-grPrepSem }()
		scaledForDraw(img, m)
		grPreparing.Delete(img)
	}()
}

var (
	grPreparing sync.Map
	grPrepSem   = make(chan struct{}, 2)
)

func (g *gpuRasterBackend) DrawImage(img image.Image, dst gfx.Rect, clip gfx.Rect) {
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 || dst.W <= 0 || dst.H <= 0 {
		return
	}
	m := gfx.Matrix{A: dst.W / float32(b.Dx()), TX: dst.X, D: dst.H / float32(b.Dy()), TY: dst.Y}
	t, mm := g.imageTexture(img, m)
	if t == nil {
		return
	}
	p := mm.Concat(gfx.ScaleMatrix(float32(t.w), float32(t.h)))
	g.quad(p, fullUV, [4]float32{1, 1, 1, 1}, rectToPixelRectOut(clip).Intersect(rectToPixelRectOut(dst)), grTex, 0, false,
		[2]float32{}, t, nil, fullUV, false)
}

func (g *gpuRasterBackend) DrawImageTransformed(img image.Image, m gfx.Matrix, clip gfx.Rect) {
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return
	}
	t, mm := g.imageTexture(img, m)
	if t == nil {
		return
	}
	p := mm.Concat(gfx.ScaleMatrix(float32(t.w), float32(t.h)))
	g.quad(p, fullUV, [4]float32{1, 1, 1, 1}, rectToPixelRectOut(clip), grTex, 0, !m.IsAxisAligned(),
		[2]float32{}, t, nil, fullUV, false)
}

func (g *gpuRasterBackend) blendCoverage(cov []float32, bounds, clip image.Rectangle, col gfx.Color) {
	vis := bounds.Intersect(clip).Intersect(g.top().rect)
	if vis.Empty() || col.A <= 0 {
		return
	}
	w := bounds.Dx()
	pix := make([]uint8, vis.Dx()*vis.Dy())
	for y := vis.Min.Y; y < vis.Max.Y; y++ {
		row := cov[(y-bounds.Min.Y)*w:]
		out := pix[(y-vis.Min.Y)*vis.Dx():]
		for x := vis.Min.X; x < vis.Max.X; x++ {
			a := row[x-bounds.Min.X] / 4
			out[x-vis.Min.X] = uint8(clamp01(a)*255 + 0.5)
		}
	}
	t := uploadR8(g.dev, pix, vis.Dx(), vis.Dy(), vis.Dx())
	if t == nil {
		g.fail()
		return
	}
	g.transient = append(g.transient, t)
	rgba := toRGBA(col)
	a := float32(rgba.A) / 255
	tint := [4]float32{float32(rgba.R) / 255 * a, float32(rgba.G) / 255 * a, float32(rgba.B) / 255 * a, a}
	g.quad(rectMatrix(pixRect(vis)), fullUV, tint, vis, grTexMask, 0, false, [2]float32{}, t, nil, fullUV, false)
}

func (g *gpuRasterBackend) backdropFilter(image.Rectangle, gfx.ImageFilter, *image.Alpha) { g.fail() }

func (g *gpuRasterBackend) PushLayer(physBounds, clip image.Rectangle, paint gfx.Paint) {
	if paint.ImageFilter != nil || paint.ColorFilter != nil || paint.BlendMode != gfx.BlendSrcOver {
		g.fail()
	}
	t := g.top()
	r := physBounds.Intersect(t.rect)
	if t.dead || r.Empty() {
		g.stack = append(g.stack, grTarget{rect: r, dead: true})
		g.cmds = append(g.cmds, grCmd{kind: grPush})
		return
	}
	g.stack = append(g.stack, grTarget{rect: r})
	g.cmds = append(g.cmds, grCmd{kind: grPush, layer: r, alpha: paint.EffectiveAlpha(), clip: clip.Intersect(t.rect)})
}

func (g *gpuRasterBackend) PopLayer(mask *image.Alpha) {
	if len(g.stack) <= 1 {
		return
	}
	g.stack = g.stack[:len(g.stack)-1]
	c := grCmd{kind: grPop}
	if mask != nil && !mask.Rect.Empty() {
		t := uploadR8(g.dev, mask.Pix, mask.Rect.Dx(), mask.Rect.Dy(), mask.Stride)
		if t == nil {
			g.fail()
		} else {
			g.transient = append(g.transient, t)
			c.mask, c.maskR = t, mask.Rect
		}
	}
	g.cmds = append(g.cmds, c)
}

var (
	_ RasterBackend    = (*gpuRasterBackend)(nil)
	_ glyphMaskBlender = (*gpuRasterBackend)(nil)
	_ backdropFilterer = (*gpuRasterBackend)(nil)
)

// ---- encoding ----

// gpuRasterPicture rasterizes pic's rect into dst (a w×h RGBA8 texture
// with render-attachment use). False when the picture holds something the
// GPU path does not do; nothing is submitted then.
func gpuRasterPicture(dev *wgpu.Device, pic *Picture, rect image.Rectangle, dst *gpuTex) bool {
	p, err := gpuRasterPipesFor(dev)
	if err != nil || pic == nil {
		return false
	}
	g := &gpuRasterBackend{dev: dev, w: rect.Dx(), h: rect.Dy()}
	g.stack = []grTarget{{rect: image.Rect(0, 0, g.w, g.h)}}
	pic.Playback(g, rect.Min, image.Rect(0, 0, g.w, g.h))
	defer func() {
		for _, t := range g.transient {
			t.release()
		}
	}()
	if g.failed {
		grImages.trim()
		return false
	}
	g.encode(p, dst)
	grImages.trim()
	return true
}

type grBatch struct {
	pipe      *wgpu.RenderPipeline
	tex, mask *gpuTex
	first, n  int
}

// encode turns the commands into render passes: one per target stretch,
// layers as offscreen textures composited back when they pop.
func (g *gpuRasterBackend) encode(p *gpuRasterPipes, dst *gpuTex) {
	dev := g.dev
	var insts []float32
	enc := dev.CreateCommandEncoder()
	var bgs []*wgpu.BindGroup
	var layers []*gpuTex
	defer func() {
		for _, bg := range bgs {
			bg.Release()
		}
		for _, l := range layers {
			l.release()
		}
	}()

	// Collect all instances (with the pop composites resolved) into one
	// buffer, then replay the command list against it.
	type step struct {
		cmd   grCmd
		index int // instance index of a draw / pop composite
	}
	var steps []step
	type frame struct {
		rect image.Rectangle
		push grCmd
	}
	fstack := []frame{{rect: image.Rect(0, 0, g.w, g.h)}}
	for _, c := range g.cmds {
		switch c.kind {
		case grDraw:
			steps = append(steps, step{cmd: c, index: len(insts) / grInstFloats})
			insts = append(insts, c.inst[:]...)
		case grPush:
			fstack = append(fstack, frame{rect: c.layer, push: c})
			steps = append(steps, step{cmd: c})
		case grPop:
			if len(fstack) <= 1 {
				continue
			}
			f := fstack[len(fstack)-1]
			fstack = fstack[:len(fstack)-1]
			parent := fstack[len(fstack)-1].rect
			c.layer, c.alpha, c.clip = f.push.layer, f.push.alpha, f.push.clip
			idx := -1
			if !f.rect.Empty() && c.alpha > 0 {
				clip := c.clip.Intersect(f.rect)
				if c.mask != nil {
					clip = clip.Intersect(c.maskR)
				}
				var in [grInstFloats]float32
				pw, ph := float32(parent.Dx()), float32(parent.Dy())
				toNDC := gfx.Matrix{A: 2 / pw, TX: -1 - 2*float32(parent.Min.X)/pw, D: -2 / ph, TY: 1 + 2*float32(parent.Min.Y)/ph}
				m := toNDC.Concat(rectMatrix(pixRect(f.rect)))
				in[0], in[1], in[2], in[3], in[4], in[5] = m.A, m.B, m.TX, m.C, m.D, m.TY
				copy(in[6:10], fullUV[:])
				in[10], in[11], in[12], in[13] = c.alpha, c.alpha, c.alpha, c.alpha
				cl := clip.Sub(parent.Min)
				in[14], in[15], in[16], in[17] = float32(cl.Min.X), float32(cl.Min.Y), float32(cl.Max.X), float32(cl.Max.Y)
				in[18] = grTex
				muv := fullUV
				if c.mask != nil {
					mr := c.maskR
					muv = [4]float32{
						float32(f.rect.Min.X-mr.Min.X) / float32(mr.Dx()), float32(f.rect.Min.Y-mr.Min.Y) / float32(mr.Dy()),
						float32(f.rect.Max.X-mr.Min.X) / float32(mr.Dx()), float32(f.rect.Max.Y-mr.Min.Y) / float32(mr.Dy()),
					}
				}
				copy(in[24:28], muv[:])
				if !cl.Empty() {
					idx = len(insts) / grInstFloats
					insts = append(insts, in[:]...)
				}
			}
			steps = append(steps, step{cmd: c, index: idx})
		}
	}
	var buf *wgpu.Buffer
	if len(insts) > 0 {
		data := make([]byte, len(insts)*4)
		for i, f := range insts {
			binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(f))
		}
		buf = dev.CreateBuffer("qui gpu raster instances", wgpu.BufferVertex|wgpu.BufferCopyDst, uint64(len(data)))
		dev.WriteBuffer(buf, 0, data)
		defer buf.Release()
	}

	bind := func(tex, mask *gpuTex) *wgpu.BindGroup {
		if tex == nil {
			tex = p.white
		}
		if mask == nil {
			mask = p.white
		}
		bg := dev.CreateBindGroup(p.layout,
			wgpu.BindGroupEntry{Binding: 0, TextureView: tex.view},
			wgpu.BindGroupEntry{Binding: 1, TextureView: mask.view},
			wgpu.BindGroupEntry{Binding: 2, Sampler: p.linear})
		bgs = append(bgs, bg)
		return bg
	}
	type target struct {
		t    *gpuTex
		pass *wgpu.RenderPass
	}
	begin := func(t *gpuTex, clear bool) *wgpu.RenderPass {
		load := wgpu.LoadOpLoad
		if clear {
			load = wgpu.LoadOpClear
		}
		return enc.BeginRenderPass(wgpu.RenderPassDescriptor{Label: "qui gpu raster",
			Color: []wgpu.ColorAttachment{{View: t.view, Load: load, Store: wgpu.StoreOpStore}}})
	}
	tstack := []target{{t: dst, pass: begin(dst, true)}}
	var batch *grBatch
	flush := func() {
		if batch == nil || batch.n == 0 || buf == nil {
			batch = nil
			return
		}
		pass := tstack[len(tstack)-1].pass
		pass.SetPipeline(batch.pipe)
		pass.SetBindGroup(0, bind(batch.tex, batch.mask))
		pass.SetVertexBuffer(0, buf, 0, buf.Size())
		pass.Draw(4, batch.n, 0, batch.first)
		batch = nil
	}
	draw := func(pipe *wgpu.RenderPipeline, tex, mask *gpuTex, index int) {
		if batch != nil && batch.pipe == pipe && batch.tex == tex && batch.mask == mask && batch.first+batch.n == index {
			batch.n++
			return
		}
		flush()
		batch = &grBatch{pipe: pipe, tex: tex, mask: mask, first: index, n: 1}
	}
	for _, s := range steps {
		switch s.cmd.kind {
		case grDraw:
			pipe := p.blend
			if s.cmd.replace {
				pipe = p.replace
			}
			draw(pipe, s.cmd.tex, s.cmd.mask, s.index)
		case grPush:
			flush()
			tstack[len(tstack)-1].pass.End()
			var lt *gpuTex
			if !s.cmd.layer.Empty() {
				lt = newGPUTex(dev, s.cmd.layer.Dx(), s.cmd.layer.Dy(), wgpu.FormatRGBA8Unorm)
			}
			if lt == nil {
				// An empty layer still needs a pass to absorb its draws
				// (there are none: quad drops them while the layer is dead).
				lt = newGPUTex(dev, 1, 1, wgpu.FormatRGBA8Unorm)
			}
			layers = append(layers, lt)
			tstack = append(tstack, target{t: lt, pass: begin(lt, true)})
		case grPop:
			flush()
			if len(tstack) <= 1 {
				continue
			}
			top := tstack[len(tstack)-1]
			top.pass.End()
			tstack = tstack[:len(tstack)-1]
			parent := &tstack[len(tstack)-1]
			parent.pass = begin(parent.t, false)
			if s.index >= 0 {
				draw(p.blend, top.t, s.cmd.mask, s.index)
				flush()
			}
		}
	}
	flush()
	for len(tstack) > 0 {
		tstack[len(tstack)-1].pass.End()
		tstack = tstack[:len(tstack)-1]
	}
	dev.Submit(enc.Finish())
}
