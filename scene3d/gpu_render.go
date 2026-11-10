//go:build cgo && !js

package scene3d

import (
	"encoding/binary"
	"log"
	"math"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/gpu"
)

// Target formats of the viewport's render pass — what a Drawable's
// pipelines must be built for.
const (
	ColorFormat = gpu.FormatRGBA8Unorm
	DepthFormat = gpu.FormatDepth24Plus
)

// Drawable is the escape hatch of a Viewport: full control of its render
// pass instead of the scene graph.
type Drawable interface {
	DrawGPU(ctx *DrawContext)
}

// DrawContext is what a Drawable draws with: the viewport's render pass
// (color ColorFormat with MSAASamples samples, depth DepthFormat, both
// cleared) and the frame it belongs to.
type DrawContext struct {
	Frame *qui.GPUFrame
	Pass  *gpu.RenderPass
	// Size is the render target's size in pixels.
	Size qui.Size
}

// Draw renders the scene (when it changed, or every frame when
// Continuous) and composites it under the 2D layer, through a hole the
// viewport punches in it. Without a GPU canvas (tests, CPU presentation)
// it draws a placeholder.
func (v *Viewport) Draw(canvas qui.Canvas) {
	g, ok := canvas.(qui.GPUCanvas)
	if !ok {
		canvas.FillRect(v.Bounds(), qui.Color{R: 0.10, G: 0.10, B: 0.12, A: 1})
		return
	}
	bounds := v.Bounds()
	canvas.ClearRect(bounds)
	v.physBox = canvas.CurrentMatrix().TransformRect(bounds)
	g.QueueRetainedGPUDraw(v, v.physBox, v.drawGPU)
}

// Destroy releases the viewport's GPU resources (UI thread). Meshes are
// owned by the scene and released with Mesh.Destroy.
func (v *Viewport) Destroy() {
	if v != nil {
		v.gpu.release()
	}
}

// ---- GPU state ----

// meshGPU is a mesh's vertex (and index) buffer.
type meshGPU struct {
	vb, ib *gpu.Buffer
	count  int
}

func (g *meshGPU) release() {
	g.vb.Release()
	g.ib.Release()
	*g = meshGPU{}
}

func floatBytes(fs []float32) []byte {
	b := make([]byte, 4*len(fs))
	for i, f := range fs {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return b
}

func (m *Mesh) buffers(dev *gpu.Device) *meshGPU {
	if m.gpu.vb != nil || len(m.Positions) == 0 {
		return &m.gpu
	}
	n := len(m.Positions)
	data := make([]float32, 0, n*8)
	for i, p := range m.Positions {
		var nrm Vec3
		if i < len(m.Normals) {
			nrm = m.Normals[i]
		}
		var uv Vec2
		if i < len(m.UVs) {
			uv = m.UVs[i]
		}
		data = append(data, p.X, p.Y, p.Z, nrm.X, nrm.Y, nrm.Z, uv.X, uv.Y)
	}
	m.gpu.vb = dev.CreateBufferInit("scene3d mesh", gpu.BufferVertex, floatBytes(data))
	m.gpu.count = n
	if len(m.Indices) > 0 {
		ib := make([]byte, 4*len(m.Indices))
		for i, x := range m.Indices {
			binary.LittleEndian.PutUint32(ib[4*i:], x)
		}
		m.gpu.ib = dev.CreateBufferInit("scene3d indices", gpu.BufferIndex, ib)
		m.gpu.count = len(m.Indices)
	}
	return &m.gpu
}

func (m *LineMesh) buffers(dev *gpu.Device) *meshGPU {
	if m.gpu.vb != nil || len(m.Positions) == 0 {
		return &m.gpu
	}
	data := make([]float32, 0, len(m.Positions)*3)
	for _, p := range m.Positions {
		data = append(data, p.X, p.Y, p.Z)
	}
	m.gpu.vb = dev.CreateBufferInit("scene3d lines", gpu.BufferVertex, floatBytes(data))
	m.gpu.count = len(m.Positions)
	return &m.gpu
}

// ---- pipelines (one per shader source and primitive kind, process-wide) ----

type pipeKey struct {
	src   string
	lines bool
}

type pipeEntry struct {
	p   *gpu.RenderPipeline
	err error
}

var (
	pipes        = map[pipeKey]*pipeEntry{}
	sceneGroup   *gpu.BindGroupLayout
	sceneLayout  *gpu.PipelineLayout
	meshVertices = []gpu.VertexBufferLayout{{Stride: 32, Attributes: []gpu.VertexAttribute{
		{Format: gpu.VertexFloat32x3, Offset: 0, Location: 0},
		{Format: gpu.VertexFloat32x3, Offset: 12, Location: 1},
		{Format: gpu.VertexFloat32x2, Offset: 24, Location: 2},
	}}}
	lineVertices = []gpu.VertexBufferLayout{{Stride: 12, Attributes: []gpu.VertexAttribute{
		{Format: gpu.VertexFloat32x3, Offset: 0, Location: 0},
	}}}
)

func layout(dev *gpu.Device) *gpu.PipelineLayout {
	if sceneLayout == nil {
		vis := gpu.StageVertex | gpu.StageFragment
		sceneGroup = dev.CreateBindGroupLayout("scene3d",
			gpu.BindGroupLayoutEntry{Binding: 0, Visibility: vis, Type: gpu.BindUniformBuffer},
			gpu.BindGroupLayoutEntry{Binding: 1, Visibility: vis, Type: gpu.BindUniformBuffer})
		sceneLayout = dev.CreatePipelineLayout("scene3d", sceneGroup)
	}
	return sceneLayout
}

// pipelineFor compiles (once) the pipeline drawing mat's shader.
func pipelineFor(dev *gpu.Device, mat Material, lines bool) *gpu.RenderPipeline {
	key := pipeKey{src: mat.WGSL(), lines: lines}
	if e := pipes[key]; e != nil {
		return e.p
	}
	e := &pipeEntry{}
	pipes[key] = e
	mod, err := dev.CreateShaderModule("scene3d material", Preamble+key.src)
	if err != nil {
		e.err = err
		log.Printf("scene3d: %v", err)
		return nil
	}
	defer mod.Release()
	desc := gpu.RenderPipelineDescriptor{
		Label: "scene3d", Layout: layout(dev), Module: mod,
		SampleCount: MSAASamples,
		Targets:     []gpu.ColorTarget{{Format: ColorFormat, Blend: gpu.BlendPremultiplied}},
	}
	if lines {
		desc.Buffers = lineVertices
		desc.Topology = gpu.TopologyLineList
		desc.DepthStencil = &gpu.DepthStencil{Format: DepthFormat, DepthWrite: true, Compare: gpu.CompareLessEqual}
	} else {
		desc.Buffers = meshVertices
		desc.Cull = gpu.CullBack
		// Push filled triangles slightly back so coincident edge lines
		// (Node.Lines) win the depth test instead of z-fighting the faces.
		desc.DepthStencil = &gpu.DepthStencil{Format: DepthFormat, DepthWrite: true, Compare: gpu.CompareLess,
			DepthBias: 1, DepthBiasSlope: 1}
	}
	e.p, e.err = dev.CreateRenderPipeline(desc)
	if e.err != nil {
		log.Printf("scene3d: %v", e.err)
	}
	return e.p
}

// ---- viewport ----

type itemKey struct {
	node  *Node
	lines bool
}

// item is one draw's object block.
type item struct {
	buf  *gpu.Buffer
	bg   *gpu.BindGroup
	used uint64
}

type viewportGPU struct {
	w, h        int
	msaa, depth *gpu.Texture
	resolve     *gpu.Texture
	msaaView    *gpu.TextureView
	depthView   *gpu.TextureView
	resolveView *gpu.TextureView
	frameBuf    *gpu.Buffer
	items       map[itemKey]*item
	frame       uint64
	rendered    bool
}

func (g *viewportGPU) releaseTargets() {
	for _, v := range []*gpu.TextureView{g.msaaView, g.depthView, g.resolveView} {
		v.Release()
	}
	for _, t := range []*gpu.Texture{g.msaa, g.depth, g.resolve} {
		t.Destroy()
	}
	g.msaa, g.depth, g.resolve = nil, nil, nil
	g.msaaView, g.depthView, g.resolveView = nil, nil, nil
	g.w, g.h, g.rendered = 0, 0, false
}

func (g *viewportGPU) release() {
	g.releaseTargets()
	for k, it := range g.items {
		it.bg.Release()
		it.buf.Release()
		delete(g.items, k)
	}
	g.frameBuf.Release()
	g.frameBuf = nil
}

func (g *viewportGPU) ensureTargets(dev *gpu.Device, w, h int) bool {
	if g.resolve != nil && g.w == w && g.h == h {
		return false
	}
	g.releaseTargets()
	g.msaa = dev.CreateTexture(gpu.TextureDescriptor{Label: "scene3d msaa", Width: w, Height: h,
		Format: ColorFormat, Usage: gpu.UsageRenderAttachment, SampleCount: MSAASamples})
	g.depth = dev.CreateTexture(gpu.TextureDescriptor{Label: "scene3d depth", Width: w, Height: h,
		Format: DepthFormat, Usage: gpu.UsageRenderAttachment, SampleCount: MSAASamples})
	g.resolve = dev.CreateTexture(gpu.TextureDescriptor{Label: "scene3d color", Width: w, Height: h,
		Format: ColorFormat, Usage: gpu.UsageRenderAttachment | gpu.UsageTextureBinding})
	g.msaaView, g.depthView, g.resolveView = g.msaa.CreateView(), g.depth.CreateView(), g.resolve.CreateView()
	g.w, g.h = w, h
	return true
}

func (v *Viewport) drawGPU(f *qui.GPUFrame) {
	box := v.physBox
	w, h := int(math.Round(float64(box.W))), int(math.Round(float64(box.H)))
	if w <= 0 || h <= 0 {
		return
	}
	g := &v.gpu
	if g.ensureTargets(f.Device, w, h) {
		v.dirtyScene = true
	}
	if v.dirtyScene || v.Continuous || !g.rendered {
		v.render(f, w, h)
		v.dirtyScene = false
		g.rendered = true
	}
	f.DrawTexture(g.resolveView, qui.Rect{W: float32(w), H: float32(h)}, box, qui.TextureOpts{})
}

func (v *Viewport) render(f *qui.GPUFrame, w, h int) {
	g := &v.gpu
	dev := f.Device
	c := v.ClearColor
	if c == (qui.Color{}) {
		c = qui.Color{R: 0.08, G: 0.08, B: 0.10, A: 1}
	}
	pass := f.Encoder.BeginRenderPass(gpu.RenderPassDescriptor{
		Label: "scene3d",
		Color: []gpu.ColorAttachment{{View: g.msaaView, Resolve: g.resolveView, Load: gpu.LoadOpClear,
			Store: gpu.StoreOpDiscard,
			Clear: gpu.Color{R: float64(c.R * c.A), G: float64(c.G * c.A), B: float64(c.B * c.A), A: float64(c.A)}}},
		Depth: &gpu.DepthAttachment{View: g.depthView, Store: gpu.StoreOpDiscard},
	})
	defer pass.End()
	if v.Drawable != nil {
		v.Drawable.DrawGPU(&DrawContext{Frame: f, Pass: pass, Size: qui.Size{W: float32(w), H: float32(h)}})
		return
	}
	if v.Scene == nil {
		return
	}
	g.frame++
	if g.frameBuf == nil {
		g.frameBuf = dev.CreateBuffer("scene3d frame", gpu.BufferUniform|gpu.BufferCopyDst, 192)
	}
	dev.WriteBuffer(g.frameBuf, 0, floatBytes(frameBlock(v.Scene, float32(w)/float32(h))))
	v.drawNode(dev, pass, v.Scene.Root, Mat4Identity())
	for k, it := range g.items {
		if it.used != g.frame {
			it.bg.Release()
			it.buf.Release()
			delete(g.items, k)
		}
	}
}

// frameBlock is the Frame uniform (see Preamble).
func frameBlock(s *Scene, aspect float32) []float32 {
	view := s.Camera.ViewMatrix()
	proj := s.Camera.ProjectionMatrix(aspect)
	d := s.firstDirectional()
	l := d.Direction.Mul(-1).Normalize()
	out := make([]float32, 0, 48)
	out = append(out, view[:]...)
	out = append(out, proj[:]...)
	p := s.Camera.Position
	out = append(out, p.X, p.Y, p.Z, 1, l.X, l.Y, l.Z, 0)
	out = append(out, d.Color.R*d.Intensity, d.Color.G*d.Intensity, d.Color.B*d.Intensity, 1)
	out = append(out, s.Ambient.R, s.Ambient.G, s.Ambient.B, s.Ambient.A)
	return out
}

func (v *Viewport) drawNode(dev *gpu.Device, pass *gpu.RenderPass, n *Node, parent Mat4) {
	if n == nil || !n.Visible {
		return
	}
	world := Mat4Mul(parent, n.Transform.LocalMatrix())
	if n.Mesh != nil && n.Material != nil {
		if b := n.Mesh.buffers(dev); b.vb != nil {
			v.draw(dev, pass, itemKey{node: n}, n.Material, world, b, false)
		}
	}
	if n.Lines != nil {
		lm := n.LineMaterial
		if lm == nil {
			lm = defaultLineMaterial()
		}
		if b := n.Lines.buffers(dev); b.vb != nil {
			v.draw(dev, pass, itemKey{node: n, lines: true}, lm, world, b, true)
		}
	}
	for _, c := range n.Children {
		v.drawNode(dev, pass, c, world)
	}
}

func (v *Viewport) draw(dev *gpu.Device, pass *gpu.RenderPass, key itemKey, mat Material, world Mat4, b *meshGPU, lines bool) {
	p := pipelineFor(dev, mat, lines)
	if p == nil {
		return
	}
	g := &v.gpu
	if g.items == nil {
		g.items = map[itemKey]*item{}
	}
	it := g.items[key]
	if it == nil {
		buf := dev.CreateBuffer("scene3d object", gpu.BufferUniform|gpu.BufferCopyDst, 160)
		it = &item{buf: buf, bg: dev.CreateBindGroup(sceneGroup,
			gpu.BindGroupEntry{Binding: 0, Buffer: g.frameBuf},
			gpu.BindGroupEntry{Binding: 1, Buffer: buf})}
		g.items[key] = it
	}
	it.used = g.frame
	mp := mat.Params(v.Scene)
	obj := make([]float32, 0, 40)
	obj = append(obj, world[:]...)
	obj = append(obj, mp.Color.R, mp.Color.G, mp.Color.B, mp.Color.A)
	obj = append(obj, mp.Params[:]...)
	obj = append(obj, mp.User[:]...)
	dev.WriteBuffer(it.buf, 0, floatBytes(obj))
	pass.SetPipeline(p)
	pass.SetBindGroup(0, it.bg)
	pass.SetVertexBuffer(0, b.vb, 0, 0)
	if b.ib != nil {
		pass.SetIndexBuffer(b.ib, gpu.IndexUint32, 0, 0)
		pass.DrawIndexed(b.count, 1, 0, 0, 0)
	} else {
		pass.Draw(b.count, 1, 0, 0)
	}
}
