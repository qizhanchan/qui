//go:build cgo && !js

package wgpu

import "testing"

// A six-layer texture renders into each face through single-layer views,
// then a cube view samples the right face for each axis direction, and a
// 2D-array view picks layers by index.
func TestCubeAndArrayViews(t *testing.T) {
	d := sharedOrSkip(t)
	if tex := d.CreateTexture(TextureDescriptor{Width: 4, Height: 4, Format: FormatRGBA8Unorm, Usage: UsageTextureBinding, Layers: -1}); tex != nil {
		t.Fatal("negative layer count accepted")
	}
	cube := d.CreateTexture(TextureDescriptor{Width: 4, Height: 4, Format: FormatRGBA8Unorm, Layers: 6, MipLevels: 2,
		Usage: UsageRenderAttachment | UsageTextureBinding})
	if cube == nil || cube.Layers() != 6 {
		t.Fatal("cube texture allocation failed")
	}
	defer cube.Destroy()
	if _, err := cube.CreateViewWith(TextureViewDescriptor{Dimension: TextureViewCube, LayerCount: 5}); err == nil {
		t.Fatal("a five-layer cube view was accepted")
	}
	if _, err := cube.CreateViewWith(TextureViewDescriptor{BaseLayer: 6}); err == nil {
		t.Fatal("a layer past the end was accepted")
	}
	colors := [6]Color{{R: 1, A: 1}, {G: 1, A: 1}, {B: 1, A: 1}, {R: 1, G: 1, A: 1}, {G: 1, B: 1, A: 1}, {R: 1, B: 1, A: 1}}
	encoder := d.CreateCommandEncoder()
	for face, c := range colors {
		v, err := cube.CreateViewWith(TextureViewDescriptor{BaseLayer: face, MipCount: 1})
		if err != nil {
			t.Fatal(err)
		}
		pass := encoder.BeginRenderPass(RenderPassDescriptor{Color: []ColorAttachment{{View: v, Clear: c}}})
		pass.End()
		defer v.Release()
	}
	d.Submit(encoder.Finish())
	cubeView, err := cube.CreateViewWith(TextureViewDescriptor{Dimension: TextureViewCube, MipCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer cubeView.Release()
	arrayView, err := cube.CreateViewWith(TextureViewDescriptor{Dimension: TextureView2DArray, MipCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer arrayView.Release()
	layout := d.CreateBindGroupLayout("cube probe",
		BindGroupLayoutEntry{Binding: 0, Visibility: StageFragment, Type: BindCubeTexture},
		BindGroupLayoutEntry{Binding: 1, Visibility: StageFragment, Type: BindTextureArray},
		BindGroupLayoutEntry{Binding: 2, Visibility: StageFragment, Type: BindSampler})
	defer layout.Release()
	sampler, err := d.CreateSamplerWithDescriptor(SamplerDescriptor{MinFilter: FilterNearest, MagFilter: FilterNearest})
	if err != nil {
		t.Fatal(err)
	}
	defer sampler.Release()
	mod, err := d.CreateShaderModule("cube probe", `
@group(0) @binding(0) var env: texture_cube<f32>;
@group(0) @binding(1) var layers: texture_2d_array<f32>;
@group(0) @binding(2) var s: sampler;
@vertex fn vs_main(@builtin(vertex_index) i: u32) -> @builtin(position) vec4f {
    let p = array<vec2f, 3>(vec2f(-1,-1), vec2f(3,-1), vec2f(-1,3));
    return vec4f(p[i], 0, 1);
}
@fragment fn fs_main(@builtin(position) p: vec4f) -> @location(0) vec4f {
    let dirs = array<vec3f, 6>(vec3f(1,0,0), vec3f(-1,0,0), vec3f(0,1,0), vec3f(0,-1,0), vec3f(0,0,1), vec3f(0,0,-1));
    let i = u32(p.x);
    if i < 6u { return textureSampleLevel(env, s, dirs[i], 0.0); }
    return textureSampleLevel(layers, s, vec2f(0.5), 4, 0.0);
}
`)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Release()
	pl := d.CreatePipelineLayout("cube probe", layout)
	defer pl.Release()
	pipeline, err := d.CreateRenderPipeline(RenderPipelineDescriptor{Layout: pl, Module: mod, Targets: []ColorTarget{{Format: FormatRGBA8Unorm}}})
	if err != nil {
		t.Fatal(err)
	}
	defer pipeline.Release()
	group := d.CreateBindGroup(layout, BindGroupEntry{Binding: 0, TextureView: cubeView},
		BindGroupEntry{Binding: 1, TextureView: arrayView}, BindGroupEntry{Binding: 2, Sampler: sampler})
	defer group.Release()
	target := d.CreateTexture(TextureDescriptor{Width: 7, Height: 1, Format: FormatRGBA8Unorm, Usage: UsageRenderAttachment | UsageCopySrc})
	defer target.Destroy()
	out := target.CreateView()
	defer out.Release()
	encoder = d.CreateCommandEncoder()
	pass := encoder.BeginRenderPass(RenderPassDescriptor{Color: []ColorAttachment{{View: out}}})
	pass.SetPipeline(pipeline)
	pass.SetBindGroup(0, group)
	pass.Draw(3, 1, 0, 0)
	pass.End()
	d.Submit(encoder.Finish())
	px, err := d.ReadTexture(target, 0, 0, 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 7 {
		c := colors[i%6]
		if i == 6 {
			c = colors[4] // the last pixel reads array layer 4
		}
		want := [4]byte{byte(c.R * 255), byte(c.G * 255), byte(c.B * 255), 255}
		if got := [4]byte(px[4*i : 4*i+4]); got != want {
			t.Fatalf("pixel %d = %v, want %v (all %v)", i, got, want, px)
		}
	}
}
