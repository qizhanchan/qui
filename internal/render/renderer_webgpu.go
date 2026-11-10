package render

import (
	"encoding/binary"
	"errors"
	"image"
	"log"
	"math"
	"os"
	"unsafe"

	"github.com/qizhanchan/qui/internal/gfx"
	"github.com/qizhanchan/qui/internal/wgpu"
)

// WebGPURenderer rasterizes on the CPU like every qui renderer and
// presents through WebGPU (Dawn): Metal on macOS, D3D12 on Windows, Vulkan
// on Linux. It is the renderer of windows that show GPU content (3D,
// custom shaders — GPUCanvas) and of platforms without a native GPU path
// of their own. See docs/rfc/0003-webgpu.zh.md.
//
// Structure:
//
//   - The CPU frame is, where the GPU shares memory with the CPU (Apple
//     silicon), an IOSurface imported as a texture: the raster writes the
//     memory the GPU samples, nothing is uploaded. Otherwise damaged
//     rectangles are uploaded with queue.writeTexture.
//   - A frame: GPU draws record their own passes on the frame's encoder;
//     then one composite pass on the surface texture clears it, runs the
//     composite operations in queue order (pre hook, retained / fresh
//     draws), draws the 2D layer premultiplied source-over and finally the
//     post hook's operations.
//   - Window.ScrollPixels moves pixels of the frame texture on the GPU.
type WebGPURenderer struct {
	img         *image.RGBA
	backend     *CPUBackend
	logicalSize gfx.Size
	damage      image.Rectangle
	transparent bool

	target  SurfaceTarget
	dev     *wgpu.Device
	surface *wgpu.Surface
	pipes   *webgpuPipes

	// frame is the GPU copy of img (or img's own memory, shared).
	frame     *wgpu.Texture
	frameView *wgpu.TextureView
	frameBG   *wgpu.BindGroup
	scratch   *wgpu.Texture
	shared    *sharedFrame // IOSurface-backed frame (darwin), nil otherwise
	locked    bool

	painted bool
	stale   bool // frame texture does not hold img: upload all of it

	// Surface acquisition. dropped: the last frame got no surface texture
	// (the window asks for a redraw-only frame to show it); drops counts
	// consecutive drops, lost consecutive Lost surfaces (recreated each
	// time). broken: the surface cannot present at all; SetSurfaceTarget
	// then reports false and the window falls back to the CPU path.
	dropped bool
	drops   int
	lost    int
	broken  bool

	retainedComp RetainedSet[func(Compositor)]
	queuedComp   []func(Compositor)
	retainedGPU  RetainedSet[func(*GPUFrame)]
	queuedGPU    []func(*GPUFrame)
	pre, post    func(*GPUFrame)

	damageRects []image.Rectangle
	moves       []image.Rectangle // x,y,w,h in Min/Max, dx/dy below
	moveDeltas  []image.Point

	// Per frame.
	ops     []compositeOp
	inst    []float32
	instBuf *wgpu.Buffer
	later   []func() // releases after submit
	access  []*sharedTex

	pixels map[any]*pixelTex
	yuv    map[any]*yuvTex
	pbuf   pixelBufferCache

	// keepShown copies each presented frame into shown (tests);
	// capture are one-shot readers of the next presented frame.
	keepShown bool
	capture   []func(*image.RGBA)
	shown     *wgpu.Texture
}

// NewWebGPURenderer creates a WebGPU-presenting renderer. It presents
// once the window hands it a surface (SetMetalLayer / SetSurfaceTarget).
func NewWebGPURenderer() *WebGPURenderer { return &WebGPURenderer{} }

// WebGPUAvailable reports whether Dawn can be loaded and a device made.
func WebGPUAvailable() bool {
	_, err := wgpu.Shared()
	return err == nil
}

var webgpuLogged bool

var errNoSurface = errors.New("render: WebGPU surface could not be created")

// webgpuNoShared forces the upload path instead of IOSurface frames
// (QUI_WEBGPU_SHARED=0, tests).
var webgpuNoShared = os.Getenv("QUI_WEBGPU_SHARED") == "0"

// SetMetalLayer implements MetalLayerRenderer (cocoa).
func (r *WebGPURenderer) SetMetalLayer(layer unsafe.Pointer) bool {
	return r.SetSurfaceTarget(SurfaceTarget{MetalLayer: layer})
}

// SetSurfaceTarget implements NativeWindowRenderer.
func (r *WebGPURenderer) SetSurfaceTarget(t SurfaceTarget) bool {
	if r == nil {
		return false
	}
	if wgpu.Lost() || r.broken {
		// The window falls back to CPU presentation (Window.rendererFailed).
		r.releaseSurface()
		r.target = SurfaceTarget{}
		return false
	}
	if t == r.target {
		return t.IsZero() || r.surface != nil
	}
	r.releaseSurface()
	r.target = t
	if t.IsZero() {
		return true
	}
	dev, err := wgpu.Shared()
	if err != nil {
		log.Printf("qui: WebGPU unavailable: %v", err)
		r.target = SurfaceTarget{}
		return false
	}
	var s *wgpu.Surface
	switch {
	case t.MetalLayer != nil:
		s, err = dev.CreateSurfaceMetalLayer(t.MetalLayer)
	case t.HWND != nil:
		s, err = dev.CreateSurfaceHWND(t.HInstance, t.HWND)
	default:
		s, err = dev.CreateSurfaceXlib(t.XDisplay, t.XWindow)
	}
	if err != nil {
		log.Printf("qui: WebGPU surface: %v", err)
		r.target = SurfaceTarget{}
		return false
	}
	pipes, err := webgpuPipesFor(dev)
	if err != nil {
		log.Printf("qui: WebGPU pipelines: %v", err)
		s.Release()
		r.target = SurfaceTarget{}
		return false
	}
	if !webgpuLogged && os.Getenv("QUI_DEBUG_RENDERER") != "" {
		webgpuLogged = true
		log.Printf("qui: WebGPU on %s (%s)", dev.AdapterName(), wgpu.LibraryPath())
	}
	r.dev, r.surface, r.pipes = dev, s, pipes
	r.stale = true
	return true
}

func (r *WebGPURenderer) releaseSurface() {
	if r.surface != nil {
		r.surface.Release()
		r.surface = nil
	}
}

// Close releases the GPU resources; the CPU frame survives (Snapshot).
func (r *WebGPURenderer) Close() {
	if r == nil {
		return
	}
	if r.dev != nil {
		r.dev.WaitIdle()
	}
	r.detachShared()
	r.releaseSurface()
	r.releaseFrameTextures()
	for k := range r.pixels {
		r.forgetTextures(k)
	}
	for k := range r.yuv {
		r.forgetTextures(k)
	}
	r.pbuf.releaseAll()
	if r.instBuf != nil {
		r.instBuf.Release()
		r.instBuf = nil
	}
	if r.shown != nil {
		r.shown.Destroy()
		r.shown = nil
	}
	r.target = SurfaceTarget{}
}

func (r *WebGPURenderer) releaseFrameTextures() {
	if r.frameBG != nil {
		r.frameBG.Release()
		r.frameBG = nil
	}
	if r.frameView != nil {
		r.frameView.Release()
		r.frameView = nil
	}
	if r.frame != nil {
		if r.shared == nil {
			r.frame.Destroy()
		} else {
			r.frame.Release()
		}
		r.frame = nil
	}
	if r.scratch != nil {
		r.scratch.Destroy()
		r.scratch = nil
	}
}

// detachShared leaves IOSurface mode, copying the frame into Go memory so
// img stays valid.
func (r *WebGPURenderer) detachShared() {
	if r.shared == nil {
		return
	}
	r.unlock()
	if r.img != nil {
		cp := image.NewRGBA(r.img.Rect)
		n := r.img.Rect.Dx() * 4
		for y := 0; y < r.img.Rect.Dy(); y++ {
			copy(cp.Pix[y*cp.Stride:y*cp.Stride+n], r.img.Pix[y*r.img.Stride:])
		}
		r.img = cp
		if r.backend != nil {
			r.backend.SetBase(cp)
		}
	}
	r.releaseFrameTextures()
	r.shared.release()
	r.shared = nil
	r.stale = true
}

func (r *WebGPURenderer) unlock() {
	if r.locked && r.shared != nil {
		r.shared.unlock()
	}
	r.locked = false
}

// frameBuffer makes img a w×h frame — IOSurface memory when possible —
// and the frame texture to match.
func (r *WebGPURenderer) frameBuffer(w, h int) {
	if r.dev != nil && r.dev.CanImportIOSurface() && sharedFramesAvailable && !webgpuNoShared {
		if r.shared != nil && r.shared.w == w && r.shared.h == h {
			return
		}
		if s := newSharedFrame(r.dev, w, h); s != nil {
			old := r.img
			if r.shared != nil {
				r.unlock()
				r.releaseFrameTextures()
				r.shared.release()
				r.shared = nil
			} else {
				// An upload frame (an earlier IOSurface failed): its
				// texture, view and bind group must not outlive it, or
				// the composite keeps sampling the old upload texture.
				r.releaseFrameTextures()
			}
			r.shared = s
			r.img = s.image()
			r.shared.lock()
			r.locked = true
			if old != nil && old.Rect == r.img.Rect {
				for y := 0; y < h; y++ {
					copy(r.img.Pix[y*r.img.Stride:y*r.img.Stride+w*4], old.Pix[y*old.Stride:])
				}
			} else {
				r.damage = image.Rectangle{}
			}
			r.frame = s.tex
			r.frameView = s.tex.CreateView()
			r.stale = false
			return
		}
		r.detachShared()
	}
	if r.img == nil || r.img.Rect.Dx() != w || r.img.Rect.Dy() != h {
		r.img = image.NewRGBA(image.Rect(0, 0, w, h))
		r.damage = image.Rectangle{}
		r.stale = true
	}
}

// Begin starts a frame of the given physical size.
func (r *WebGPURenderer) Begin(windowSize gfx.Size) gfx.Canvas {
	w := int(maxFloat(windowSize.W, 1))
	h := int(maxFloat(windowSize.H, 1))
	r.frameBuffer(w, h)
	if r.shared != nil && !r.locked {
		// The GPU may still be sampling the last frame from this memory.
		r.dev.WaitIdle()
		r.shared.lock()
		r.locked = true
	}
	if r.logicalSize.W == 0 || r.logicalSize.H == 0 {
		r.logicalSize = windowSize
	}
	if r.backend == nil {
		r.backend = newCPUBackend(r.img)
		r.backend.SetDeferFills(deferFillsDefault)
	} else {
		r.backend.SetBase(r.img)
	}
	r.backend.Begin(windowSize)
	state := gfx.NewCanvasState(gfx.Rect{W: float32(w), H: float32(h)})
	return webgpuCanvas{
		imageCanvas: imageCanvas{CanvasState: state, Backend: r.backend, Layers: newFrontendLayerTracker()},
		r:           r,
	}
}

// End finishes the raster and presents the frame.
func (r *WebGPURenderer) End() {
	if r.backend != nil {
		r.backend.End()
	}
	r.unlock()
	r.painted = true
	damage := r.damage
	rects := r.damageRects
	r.damage, r.damageRects = image.Rectangle{}, nil
	if r.surface == nil || r.img == nil {
		r.queuedComp, r.queuedGPU = r.queuedComp[:0], r.queuedGPU[:0]
		r.moves, r.moveDeltas = r.moves[:0], r.moveDeltas[:0]
		r.retainedComp.EndFrame()
		r.retainedGPU.EndFrame()
		return
	}
	img := r.img
	var up []image.Rectangle
	switch {
	case len(rects) > 0:
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
			d = d.Inset(-1).Intersect(img.Rect)
		}
		up = []image.Rectangle{d}
	}
	r.present(up, damage, false)
}

// RedrawRetained produces a frame that replays the retained draws over the
// last painted frame without rasterizing anything.
func (r *WebGPURenderer) RedrawRetained() bool {
	if r == nil || r.surface == nil || r.img == nil || !r.painted ||
		r.retainedComp.Len()+r.retainedGPU.Len() == 0 && r.pre == nil && r.post == nil && !r.dropped {
		return false
	}
	r.present(nil, image.Rectangle{}, true)
	return true
}

func (r *WebGPURenderer) present(up []image.Rectangle, damage image.Rectangle, damageNone bool) {
	dev := r.dev
	img := r.img
	w, h := img.Rect.Dx(), img.Rect.Dy()
	replayComp := r.retainedComp.CarryOver(damage, damageNone)
	replayGPU := r.retainedGPU.CarryOver(damage, damageNone)
	freshComp, freshGPU := r.queuedComp, r.queuedGPU
	r.queuedComp, r.queuedGPU = r.queuedComp[:0], r.queuedGPU[:0]
	defer r.retainedComp.EndFrame()
	defer r.retainedGPU.EndFrame()

	r.updateFrameTexture(up)
	enc := dev.CreateCommandEncoder()

	target := r.acquire(w, h)
	if target == nil {
		dev.Submit(r.finish(enc))
		r.afterSubmit()
		return
	}
	targetView := target.CreateView()

	r.ops = r.ops[:0]
	r.inst = r.inst[:0]
	frame := &GPUFrame{
		Device: dev, Encoder: enc, Format: wgpu.FormatBGRA8Unorm,
		Size: gfx.Size{W: float32(w), H: float32(h)}, LogicalSize: r.logicalSize, r: r,
	}
	if r.pre != nil {
		r.pre(frame)
	}
	comp := webgpuCompositor{r: r}
	for _, fn := range replayComp {
		fn(comp)
	}
	for _, fn := range replayGPU {
		fn(frame)
	}
	for _, fn := range freshComp {
		fn(comp)
	}
	for _, fn := range freshGPU {
		fn(frame)
	}
	r.addQuad(r.pipes.layer, r.frameBindGroup(), gfx.Rect{W: float32(w), H: float32(h)}, gfx.Rect{}, gfx.Rect{}, 1, 1, 1, false)
	if r.post != nil {
		r.post(frame)
	}
	r.encodeComposite(enc, targetView, w, h)
	capture := r.capture
	r.capture = nil
	if r.keepShown || len(capture) > 0 {
		r.copyShown(enc, target)
	}
	dev.Submit(r.finish(enc))
	r.afterSubmit()
	if presentsWithTransaction(r.target) {
		// Live resize: Dawn presents through the command buffer, which
		// Core Animation may show after this transaction; finishing the
		// frame's GPU work first lets the drawable land with the new
		// window frame.
		dev.WaitIdle()
	}
	r.surface.Present()
	if len(capture) > 0 {
		if img := r.readShown(); img != nil {
			for _, fn := range capture {
				fn(img)
			}
		}
	}
	targetView.Release()
	target.Release()
}

// maxRetriedDrops bounds how many dropped frames in a row ask for a
// redraw: a surface that keeps timing out (an occluded window) waits for
// the next real frame instead of spinning the loop.
const maxRetriedDrops = 8

// maxLostSurfaces is how many times in a row a Lost surface is recreated
// before the window gives up on WebGPU presentation.
const maxLostSurfaces = 3

// acquire configures the surface for a w×h frame and returns its texture,
// or nil when this frame cannot be presented (see dropped / broken).
func (r *WebGPURenderer) acquire(w, h int) *wgpu.Texture {
	if err := r.surface.Configure(w, h, wgpu.FormatBGRA8Unorm, r.transparent); err != nil {
		log.Printf("qui: WebGPU surface: %v", err)
		r.broken = true
		return nil
	}
	t, st := r.surface.Acquire()
	switch st {
	case wgpu.AcquireOK:
		r.dropped, r.drops, r.lost = false, 0, 0
		return t
	case wgpu.AcquireLost:
		r.lost++
		if r.lost > maxLostSurfaces {
			log.Printf("qui: WebGPU surface lost %d times in a row", r.lost)
			r.broken = true
			return nil
		}
		// Recreated by the next SetSurfaceTarget (the target differs).
		r.releaseSurface()
		r.target = SurfaceTarget{}
	case wgpu.AcquireFailed:
		log.Printf("qui: WebGPU surface cannot present")
		r.broken = true
		return nil
	}
	r.drops++
	r.dropped = true
	return nil
}

// TakeDroppedFrame implements DroppedFrameRenderer: whether the last
// frame was not presented and should be shown again by a redraw-only
// frame. It reports each drop once, and stops after maxRetriedDrops in a
// row.
func (r *WebGPURenderer) TakeDroppedFrame() bool {
	if r == nil || !r.dropped || r.broken {
		return false
	}
	return r.drops <= maxRetriedDrops
}

// finish ends GPU access to shared textures around the submit.
func (r *WebGPURenderer) finish(enc *wgpu.CommandEncoder) *wgpu.CommandBuffer {
	cb := enc.Finish()
	if r.shared != nil {
		r.shared.mem.BeginAccess(r.frame, true)
	}
	for _, s := range r.access {
		s.mem.BeginAccess(s.tex, true)
	}
	return cb
}

func (r *WebGPURenderer) afterSubmit() {
	if r.shared != nil {
		r.shared.mem.EndAccess(r.frame)
	}
	for _, s := range r.access {
		s.mem.EndAccess(s.tex)
	}
	r.access = r.access[:0]
	for _, f := range r.later {
		f()
	}
	r.later = r.later[:0]
	r.pbuf.endFrame()
}

// updateFrameTexture brings the frame texture up to date with img: pixel
// moves first (through scratch: a texture cannot be copied onto itself),
// then the damaged rectangles. Nothing to do for a shared frame.
func (r *WebGPURenderer) updateFrameTexture(up []image.Rectangle) {
	img := r.img
	w, h := img.Rect.Dx(), img.Rect.Dy()
	if r.shared != nil {
		r.moves, r.moveDeltas = r.moves[:0], r.moveDeltas[:0]
		return
	}
	if r.frame == nil || r.frame.Width() != w || r.frame.Height() != h {
		r.releaseFrameTextures()
		r.frame = r.dev.CreateTexture(wgpu.TextureDescriptor{
			Label: "qui frame", Width: w, Height: h, Format: wgpu.FormatRGBA8Unorm,
			Usage: wgpu.UsageTextureBinding | wgpu.UsageCopyDst | wgpu.UsageCopySrc,
		})
		r.frameView = r.frame.CreateView()
		r.stale = true
	}
	if r.stale {
		up = []image.Rectangle{img.Rect}
		r.moves, r.moveDeltas = r.moves[:0], r.moveDeltas[:0]
		r.stale = false
	}
	if len(r.moves) > 0 {
		// writeTexture runs at queue time, before any command encoded in
		// enc: submit the moves on their own first, so uploads land on the
		// moved content and not under it.
		menc := r.dev.CreateCommandEncoder()
		for i, m := range r.moves {
			r.encodeMove(menc, m, r.moveDeltas[i])
		}
		r.dev.Submit(menc.Finish())
	}
	r.moves, r.moveDeltas = r.moves[:0], r.moveDeltas[:0]
	for _, u := range up {
		u = u.Intersect(img.Rect)
		if u.Empty() {
			continue
		}
		off := (u.Min.Y-img.Rect.Min.Y)*img.Stride + (u.Min.X-img.Rect.Min.X)*4
		r.dev.WriteTexture(r.frame, u.Min.X-img.Rect.Min.X, u.Min.Y-img.Rect.Min.Y, u.Dx(), u.Dy(),
			unsafe.Pointer(&img.Pix[off]), img.Stride)
	}
}

func (r *WebGPURenderer) encodeMove(enc *wgpu.CommandEncoder, m image.Rectangle, d image.Point) bool {
	x, y, w, h := m.Min.X, m.Min.Y, m.Dx(), m.Dy()
	sx, sy := x, y
	if d.X < 0 {
		sx -= d.X
	}
	if d.Y < 0 {
		sy -= d.Y
	}
	cw, ch := w-iabs(d.X), h-iabs(d.Y)
	fw, fh := r.frame.Width(), r.frame.Height()
	if cw <= 0 || ch <= 0 || x < 0 || y < 0 || x+w > fw || y+h > fh {
		return false
	}
	if r.scratch == nil || r.scratch.Width() != fw || r.scratch.Height() != fh {
		if r.scratch != nil {
			r.scratch.Destroy()
		}
		r.scratch = r.dev.CreateTexture(wgpu.TextureDescriptor{
			Label: "qui scroll", Width: fw, Height: fh, Format: wgpu.FormatRGBA8Unorm,
			Usage: wgpu.UsageCopyDst | wgpu.UsageCopySrc,
		})
	}
	enc.CopyTexture(r.frame, sx, sy, r.scratch, sx, sy, cw, ch)
	enc.CopyTexture(r.scratch, sx, sy, r.frame, sx+d.X, sy+d.Y, cw, ch)
	return true
}

func (r *WebGPURenderer) frameBindGroup() *wgpu.BindGroup {
	if r.frameBG == nil {
		r.frameBG = r.dev.CreateBindGroup(r.pipes.layerLayout, wgpu.BindGroupEntry{Binding: 0, TextureView: r.frameView})
	}
	return r.frameBG
}

// ---- composite operations ----

// compositeOp is one step of the composite pass: an instanced quad, or a
// caller's pass callback.
type compositeOp struct {
	pipe *wgpu.RenderPipeline
	bg   *wgpu.BindGroup
	inst int
	clip image.Rectangle // empty: whole frame
	pass func(*wgpu.RenderPass)
}

// quadFloats is the per-instance stride: dst NDC rect, src UV rect,
// params (opacity, straight, 0, 0).
const quadFloats = 12

// addQuad queues pipe drawing the src part (UV computed from a texW×texH
// source) of a texture at dst (physical pixels).
func (r *WebGPURenderer) addQuad(pipe *wgpu.RenderPipeline, bg *wgpu.BindGroup, dst, src, clip gfx.Rect,
	texW, texH int, opacity float32, straight bool) {
	fw, fh := float32(r.img.Rect.Dx()), float32(r.img.Rect.Dy())
	if fw <= 0 || fh <= 0 || bg == nil {
		return
	}
	u0, v0, u1, v1 := float32(0), float32(0), float32(1), float32(1)
	if src.W > 0 && src.H > 0 && texW > 0 && texH > 0 {
		u0, v0 = src.X/float32(texW), src.Y/float32(texH)
		u1, v1 = (src.X+src.W)/float32(texW), (src.Y+src.H)/float32(texH)
	}
	st := float32(0)
	if straight {
		st = 1
	}
	idx := len(r.inst) / quadFloats
	r.inst = append(r.inst,
		dst.X/fw*2-1, 1-dst.Y/fh*2, (dst.X+dst.W)/fw*2-1, 1-(dst.Y+dst.H)/fh*2,
		u0, v0, u1, v1,
		opacity, st, 0, 0)
	op := compositeOp{pipe: pipe, bg: bg, inst: idx}
	if clip.W > 0 && clip.H > 0 {
		op.clip = image.Rect(int(math.Floor(float64(clip.X))), int(math.Floor(float64(clip.Y))),
			int(math.Ceil(float64(clip.X+clip.W))), int(math.Ceil(float64(clip.Y+clip.H)))).Intersect(r.img.Rect)
		if op.clip.Empty() {
			return
		}
	}
	r.ops = append(r.ops, op)
}

func (r *WebGPURenderer) addTextureOp(view *wgpu.TextureView, src, dst gfx.Rect, opts TextureOpts) {
	t := view.Texture()
	tw, th := 0, 0
	if t != nil {
		tw, th = t.Width(), t.Height()
	}
	bg := r.pipes.textureBindGroup(view, opts.Nearest)
	r.later = append(r.later, bg.Release)
	op := opts.Opacity
	if op == 0 {
		op = 1
	}
	r.addQuad(r.pipes.tex, bg, dst, src, opts.Clip, tw, th, op, opts.Straight)
}

func (r *WebGPURenderer) encodeComposite(enc *wgpu.CommandEncoder, view *wgpu.TextureView, w, h int) {
	clear := wgpu.Color{A: 1}
	if r.transparent {
		clear.A = 0
	}
	if n := len(r.inst) * 4; n > 0 {
		if r.instBuf == nil || r.instBuf.Size() < uint64(n) {
			if r.instBuf != nil {
				r.instBuf.Release()
			}
			size := uint64(4096)
			for size < uint64(n) {
				size *= 2
			}
			r.instBuf = r.dev.CreateBuffer("qui quads", wgpu.BufferVertex|wgpu.BufferCopyDst, size)
		}
		buf := make([]byte, n)
		for i, f := range r.inst {
			binary.LittleEndian.PutUint32(buf[4*i:], math.Float32bits(f))
		}
		r.dev.WriteBuffer(r.instBuf, 0, buf)
	}
	pass := enc.BeginRenderPass(wgpu.RenderPassDescriptor{
		Label: "qui composite",
		Color: []wgpu.ColorAttachment{{View: view, Load: wgpu.LoadOpClear, Clear: clear}},
	})
	var cur *wgpu.RenderPipeline
	clipped := false
	for _, op := range r.ops {
		if op.pass != nil {
			op.pass(pass)
			pass.SetViewport(0, 0, float32(w), float32(h), 0, 1)
			pass.SetScissorRect(0, 0, w, h)
			cur, clipped = nil, false
			continue
		}
		if op.pipe != cur {
			pass.SetPipeline(op.pipe)
			pass.SetVertexBuffer(0, r.instBuf, 0, 0)
			cur = op.pipe
		}
		switch {
		case !op.clip.Empty():
			pass.SetScissorRect(op.clip.Min.X, op.clip.Min.Y, op.clip.Dx(), op.clip.Dy())
			clipped = true
		case clipped:
			pass.SetScissorRect(0, 0, w, h)
			clipped = false
		}
		pass.SetBindGroup(0, op.bg)
		pass.Draw(4, 1, 0, op.inst)
	}
	pass.End()
}

func (r *WebGPURenderer) copyShown(enc *wgpu.CommandEncoder, target *wgpu.Texture) {
	w, h := target.Width(), target.Height()
	if r.shown == nil || r.shown.Width() != w || r.shown.Height() != h {
		if r.shown != nil {
			r.shown.Destroy()
		}
		r.shown = r.dev.CreateTexture(wgpu.TextureDescriptor{Width: w, Height: h,
			Format: wgpu.FormatBGRA8Unorm, Usage: wgpu.UsageCopyDst | wgpu.UsageCopySrc})
	}
	enc.CopyTexture(target, 0, 0, r.shown, 0, 0, w, h)
}

// readShown returns the last presented frame as RGBA (tests).
func (r *WebGPURenderer) readShown() *image.RGBA {
	if r.shown == nil {
		return nil
	}
	pix, err := r.dev.ReadTexture(r.shown, 0, 0, r.shown.Width(), r.shown.Height())
	if err != nil {
		return nil
	}
	out := image.NewRGBA(image.Rect(0, 0, r.shown.Width(), r.shown.Height()))
	for i := 0; i+3 < len(pix); i += 4 {
		out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = pix[i+2], pix[i+1], pix[i], pix[i+3]
	}
	return out
}

// ---- window-facing capabilities ----

func (r *WebGPURenderer) SetDamageRects(rects []image.Rectangle) {
	if r != nil {
		r.damageRects = append(r.damageRects[:0], rects...)
	}
}

func (r *WebGPURenderer) PresentsItself() bool { return r != nil && r.surface != nil }

func (r *WebGPURenderer) SetLogicalSize(s gfx.Size) {
	if r != nil {
		r.logicalSize = s
	}
}

func (r *WebGPURenderer) SetDamage(rect image.Rectangle) {
	if r != nil {
		r.damage = rect
	}
}

func (r *WebGPURenderer) SetTransparent(on bool) {
	if r != nil {
		r.transparent = on
	}
}

func (r *WebGPURenderer) IsTransparent() bool { return r != nil && r.transparent }

func (r *WebGPURenderer) SetPreRender(fn func(*GPUFrame))  { r.pre = fn }
func (r *WebGPURenderer) SetPostRender(fn func(*GPUFrame)) { r.post = fn }

// Snapshot copies the last rasterized frame.
func (r *WebGPURenderer) Snapshot() *image.RGBA {
	if r == nil || r.img == nil {
		return nil
	}
	dst := image.NewRGBA(r.img.Rect)
	n := r.img.Rect.Dx() * 4
	for y := 0; y < r.img.Rect.Dy(); y++ {
		copy(dst.Pix[y*dst.Stride:y*dst.Stride+n], r.img.Pix[y*r.img.Stride:])
	}
	return dst
}

// ForgetRetainedDraw drops key's retained draw and its cached textures.
func (r *WebGPURenderer) ForgetRetainedDraw(key any) {
	if r == nil {
		return
	}
	r.retainedComp.Forget(key)
	r.retainedGPU.Forget(key)
	r.forgetTextures(key)
}

func (r *WebGPURenderer) CanScrollPixels() bool { return r != nil && r.img != nil }

// ScrollPixels moves frame pixels: on a shared frame on the GPU right away
// (the CPU then paints the exposed part into the same memory), otherwise
// in img now and on the texture with the next frame's commands.
func (r *WebGPURenderer) ScrollPixels(rect image.Rectangle, dx, dy int) bool {
	if r == nil || r.img == nil {
		return false
	}
	rr := rect.Sub(r.img.Rect.Min)
	if r.shared != nil && r.frame != nil {
		// Same contract as scrollImage: the rect must lie in the frame
		// and the move must keep some of it, or nothing moved.
		w, h := rr.Dx(), rr.Dy()
		if !rr.In(image.Rect(0, 0, r.frame.Width(), r.frame.Height())) ||
			dx <= -w || dx >= w || dy <= -h || dy >= h {
			return false
		}
		r.unlock()
		enc := r.dev.CreateCommandEncoder()
		moved := r.encodeMove(enc, rr, image.Pt(dx, dy))
		r.dev.Submit(r.finish(enc))
		r.afterSubmit()
		r.dev.WaitIdle()
		return moved
	}
	if !scrollImage(r.img, rect, dx, dy) {
		return false
	}
	if r.frame != nil && !r.stale {
		r.moves = append(r.moves, rr)
		r.moveDeltas = append(r.moveDeltas, image.Pt(dx, dy))
	}
	return true
}

// ---- canvas ----

type webgpuCanvas struct {
	imageCanvas
	r *WebGPURenderer
}

func (c webgpuCanvas) QueueRetainedDraw(key any, physBounds gfx.Rect, fn func(Compositor)) {
	if key == nil || fn == nil {
		return
	}
	c.r.retainedComp.Put(key, physBounds, fn)
	c.r.queuedComp = append(c.r.queuedComp, fn)
}

func (c webgpuCanvas) QueueGPUDraw(fn func(*GPUFrame)) {
	if fn != nil {
		c.r.queuedGPU = append(c.r.queuedGPU, fn)
	}
}

func (c webgpuCanvas) QueueRetainedGPUDraw(key any, physBounds gfx.Rect, fn func(*GPUFrame)) {
	if key == nil || fn == nil {
		return
	}
	c.r.retainedGPU.Put(key, physBounds, fn)
	c.r.queuedGPU = append(c.r.queuedGPU, fn)
}
