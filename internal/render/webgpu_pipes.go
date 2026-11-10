//go:build cgo && !js

package render

import (
	"encoding/binary"
	"math"
	"unsafe"

	"github.com/qizhanchan/qui/internal/gfx"
	"github.com/qizhanchan/qui/internal/wgpu"
)

// The composite pass's pipelines share one vertex stage: an instanced
// quad whose per-instance data is the destination rect (NDC), the source
// rect (UV) and parameters (opacity, straight alpha). Three fragment
// stages:
//
//	fs_layer  the CPU raster, read texel-for-pixel (the frame texture has
//	          the target's size) — exact, no filtering;
//	fs_tex    a texture sampled (bilinear or nearest) onto any rect;
//	fs_yuv    planar YCbCr converted to RGB.
//
// All of them blend premultiplied source-over into a BGRA8 target.

const quadVertexWGSL = `
struct VOut {
    @builtin(position) pos: vec4f,
    @location(0) uv: vec2f,
    @location(1) @interpolate(flat) params: vec4f,
};
@vertex fn vs_main(@builtin(vertex_index) vi: u32, @location(0) dst: vec4f,
                   @location(1) src: vec4f, @location(2) params: vec4f) -> VOut {
    let c = vec2f(f32(vi & 1u), f32(vi >> 1u));
    var o: VOut;
    o.pos = vec4f(mix(dst.x, dst.z, c.x), mix(dst.y, dst.w, c.y), 0.0, 1.0);
    o.uv = vec2f(mix(src.x, src.z, c.x), mix(src.y, src.w, c.y));
    o.params = params;
    return o;
}
`

const compositeWGSL = quadVertexWGSL + `
@group(0) @binding(0) var tex: texture_2d<f32>;
@group(0) @binding(1) var smp: sampler;

@fragment fn fs_layer(in: VOut) -> @location(0) vec4f {
    let p = vec2u(in.pos.xy);
    let d = textureDimensions(tex);
    if (p.x >= d.x || p.y >= d.y) { return vec4f(0.0); }
    return textureLoad(tex, p, 0);
}

@fragment fn fs_tex(in: VOut) -> @location(0) vec4f {
    var c = textureSample(tex, smp, in.uv);
    if (in.params.y > 0.5) { c = vec4f(c.rgb * c.a, c.a); }
    return c * in.params.x;
}
`

const yuvWGSL = quadVertexWGSL + `
struct Conv { c0: vec4f, c1: vec4f, c2: vec4f, off: vec4f, scale: vec4f };
@group(0) @binding(0) var ty: texture_2d<f32>;
@group(0) @binding(1) var tu: texture_2d<f32>;
@group(0) @binding(2) var tv: texture_2d<f32>;
@group(0) @binding(3) var smp: sampler;
@group(0) @binding(4) var<uniform> k: Conv;

@fragment fn fs_yuv(in: VOut) -> @location(0) vec4f {
    var yuv = vec3f(textureSample(ty, smp, in.uv).r, textureSample(tu, smp, in.uv).r,
                    textureSample(tv, smp, in.uv).r);
    yuv = (yuv - k.off.xyz) * k.scale.xyz;
    let m = mat3x3f(k.c0.xyz, k.c1.xyz, k.c2.xyz);
    return vec4f(clamp(m * yuv, vec3f(0.0), vec3f(1.0)), 1.0) * in.params.x;
}
`

type webgpuPipes struct {
	dev                    *wgpu.Device
	layer, tex, yuv        *wgpu.RenderPipeline
	layerLayout, texLayout *wgpu.BindGroupLayout
	yuvLayout              *wgpu.BindGroupLayout
	linear, nearest        *wgpu.Sampler
}

var sharedPipes *webgpuPipes

// webgpuPipesFor returns the composite pipelines (one set per process: the
// device is shared).
func webgpuPipesFor(dev *wgpu.Device) (*webgpuPipes, error) {
	if sharedPipes != nil {
		return sharedPipes, nil
	}
	mod, err := dev.CreateShaderModule("qui composite", compositeWGSL)
	if err != nil {
		return nil, err
	}
	defer mod.Release()
	ymod, err := dev.CreateShaderModule("qui yuv", yuvWGSL)
	if err != nil {
		return nil, err
	}
	defer ymod.Release()
	quads := []wgpu.VertexBufferLayout{{
		Stride: quadFloats * 4, StepMode: wgpu.StepInstance,
		Attributes: []wgpu.VertexAttribute{
			{Format: wgpu.VertexFloat32x4, Offset: 0, Location: 0},
			{Format: wgpu.VertexFloat32x4, Offset: 16, Location: 1},
			{Format: wgpu.VertexFloat32x4, Offset: 32, Location: 2},
		},
	}}
	mk := func(label string, m *wgpu.ShaderModule, fs string) (*wgpu.RenderPipeline, error) {
		return dev.CreateRenderPipeline(wgpu.RenderPipelineDescriptor{
			Label: label, Module: m, FragmentEntry: fs, Buffers: quads,
			Topology: wgpu.TopologyTriangleStrip,
			Targets:  []wgpu.ColorTarget{{Format: wgpu.FormatBGRA8Unorm, Blend: wgpu.BlendPremultiplied}},
		})
	}
	p := &webgpuPipes{dev: dev}
	if p.layer, err = mk("qui layer", mod, "fs_layer"); err != nil {
		return nil, err
	}
	if p.tex, err = mk("qui tex", mod, "fs_tex"); err != nil {
		return nil, err
	}
	if p.yuv, err = mk("qui yuv", ymod, "fs_yuv"); err != nil {
		return nil, err
	}
	p.layerLayout = p.layer.BindGroupLayout(0)
	p.texLayout = p.tex.BindGroupLayout(0)
	p.yuvLayout = p.yuv.BindGroupLayout(0)
	p.linear = dev.CreateSampler(wgpu.FilterLinear, wgpu.AddressClampToEdge)
	p.nearest = dev.CreateSampler(wgpu.FilterNearest, wgpu.AddressClampToEdge)
	sharedPipes = p
	return p, nil
}

func (p *webgpuPipes) textureBindGroup(view *wgpu.TextureView, nearest bool) *wgpu.BindGroup {
	s := p.linear
	if nearest {
		s = p.nearest
	}
	return p.dev.CreateBindGroup(p.texLayout,
		wgpu.BindGroupEntry{Binding: 0, TextureView: view},
		wgpu.BindGroupEntry{Binding: 1, Sampler: s})
}

// ---- compositor ----

type pixelTex struct {
	tex  *wgpu.Texture
	view *wgpu.TextureView
	bg   *wgpu.BindGroup
}

func (t *pixelTex) release() {
	t.bg.Release()
	t.view.Release()
	t.tex.Destroy()
}

type yuvTex struct {
	planes [3]*wgpu.Texture
	views  [3]*wgpu.TextureView
	conv   *wgpu.Buffer
	bg     *wgpu.BindGroup
}

func (t *yuvTex) release() {
	t.bg.Release()
	t.conv.Release()
	for i := range t.planes {
		t.views[i].Release()
		t.planes[i].Destroy()
	}
}

func (r *WebGPURenderer) forgetTextures(key any) {
	if t, ok := r.pixels[key]; ok {
		t.release()
		delete(r.pixels, key)
	}
	if t, ok := r.yuv[key]; ok {
		t.release()
		delete(r.yuv, key)
	}
}

type webgpuCompositor struct{ r *WebGPURenderer }

func (c webgpuCompositor) FramebufferSize() gfx.Size {
	return gfx.Size{W: float32(c.r.img.Rect.Dx()), H: float32(c.r.img.Rect.Dy())}
}

func (c webgpuCompositor) DrawPixels(key any, pix unsafe.Pointer, stride, w, h int, bgra bool, src, dst gfx.Rect) bool {
	r := c.r
	if key == nil || pix == nil || w <= 0 || h <= 0 {
		return false
	}
	format := wgpu.FormatRGBA8Unorm
	if bgra {
		format = wgpu.FormatBGRA8Unorm
	}
	t := r.pixels[key]
	if t == nil || t.tex.Width() != w || t.tex.Height() != h || t.tex.Format() != format {
		if t != nil {
			t.release()
		}
		tex := r.dev.CreateTexture(wgpu.TextureDescriptor{Label: "qui pixels", Width: w, Height: h,
			Format: format, Usage: wgpu.UsageTextureBinding | wgpu.UsageCopyDst})
		if tex == nil {
			return false
		}
		view := tex.CreateView()
		t = &pixelTex{tex: tex, view: view, bg: r.pipes.textureBindGroup(view, false)}
		if r.pixels == nil {
			r.pixels = map[any]*pixelTex{}
		}
		r.pixels[key] = t
	}
	r.dev.WriteTexture(t.tex, 0, 0, w, h, pix, stride)
	r.addQuad(r.pipes.tex, t.bg, dst, src, gfx.Rect{}, w, h, 1, false)
	return true
}

func (c webgpuCompositor) DrawYUV420(key any, planes [3]YUVPlane, m [9]float32, off, scale [3]float32, src, dst gfx.Rect) bool {
	r := c.r
	if key == nil {
		return false
	}
	for _, p := range planes {
		if p.Pix == nil || p.W <= 0 || p.H <= 0 {
			return false
		}
	}
	t := r.yuv[key]
	ok := t != nil
	for i := 0; ok && i < 3; i++ {
		ok = t.planes[i].Width() == planes[i].W && t.planes[i].Height() == planes[i].H
	}
	if !ok {
		if t != nil {
			t.release()
		}
		t = &yuvTex{}
		for i, p := range planes {
			t.planes[i] = r.dev.CreateTexture(wgpu.TextureDescriptor{Label: "qui yuv", Width: p.W, Height: p.H,
				Format: wgpu.FormatR8Unorm, Usage: wgpu.UsageTextureBinding | wgpu.UsageCopyDst})
			t.views[i] = t.planes[i].CreateView()
		}
		t.conv = r.dev.CreateBuffer("qui yuv conv", wgpu.BufferUniform|wgpu.BufferCopyDst, 80)
		t.bg = r.dev.CreateBindGroup(r.pipes.yuvLayout,
			wgpu.BindGroupEntry{Binding: 0, TextureView: t.views[0]},
			wgpu.BindGroupEntry{Binding: 1, TextureView: t.views[1]},
			wgpu.BindGroupEntry{Binding: 2, TextureView: t.views[2]},
			wgpu.BindGroupEntry{Binding: 3, Sampler: r.pipes.linear},
			wgpu.BindGroupEntry{Binding: 4, Buffer: t.conv})
		if r.yuv == nil {
			r.yuv = map[any]*yuvTex{}
		}
		r.yuv[key] = t
	}
	for i, p := range planes {
		r.dev.WriteTexture(t.planes[i], 0, 0, p.W, p.H, p.Pix, p.Stride)
	}
	k := [20]float32{
		m[0], m[1], m[2], 0, m[3], m[4], m[5], 0, m[6], m[7], m[8], 0,
		off[0], off[1], off[2], 0, scale[0], scale[1], scale[2], 0,
	}
	buf := make([]byte, 80)
	for i, v := range k {
		binary.LittleEndian.PutUint32(buf[4*i:], math.Float32bits(v))
	}
	r.dev.WriteBuffer(t.conv, 0, buf)
	r.addQuad(r.pipes.yuv, t.bg, dst, src, gfx.Rect{}, planes[0].W, planes[0].H, 1, false)
	return true
}

func (c webgpuCompositor) DrawPixelBuffer(pb unsafe.Pointer, src, dst gfx.Rect) bool {
	r := c.r
	e := r.pbuf.get(r.dev, r.pipes, pb)
	if e == nil {
		return false
	}
	seen := false
	for _, a := range r.access {
		seen = seen || a == &e.sharedTex
	}
	if !seen {
		r.access = append(r.access, &e.sharedTex)
	}
	r.addQuad(r.pipes.tex, e.bg, dst, src, gfx.Rect{}, e.tex.Width(), e.tex.Height(), 1, false)
	return true
}

// sharedTex is a texture over imported memory, accessed by the GPU only
// between BeginAccess and EndAccess around a submit.
type sharedTex struct {
	mem *wgpu.SharedMemory
	tex *wgpu.Texture
}
