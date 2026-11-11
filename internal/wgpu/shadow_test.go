//go:build cgo && !js

package wgpu

import "testing"

func TestComparisonSamplerAndReadOnlyDepth(t *testing.T) {
	d := sharedOrSkip(t)
	depth := d.CreateTexture(TextureDescriptor{
		Width: 2, Height: 1, Format: FormatDepth32Float,
		Usage: UsageRenderAttachment | UsageTextureBinding,
	})
	defer depth.Destroy()
	if depth == nil || depth.Usage() != UsageRenderAttachment|UsageTextureBinding {
		t.Fatal("depth texture did not retain its render and sampling usages")
	}
	dv := depth.CreateView()
	defer dv.Release()
	color := d.CreateTexture(TextureDescriptor{
		Width: 2, Height: 1, Format: FormatRGBA8Unorm,
		Usage: UsageRenderAttachment | UsageCopySrc,
	})
	defer color.Destroy()
	cv := color.CreateView()
	defer cv.Release()

	mod, err := d.CreateShaderModule("shadow/readonly depth", `
@group(0) @binding(0) var map: texture_depth_2d;
@group(0) @binding(1) var compare: sampler_comparison;
@vertex fn vs_main(@builtin(vertex_index) i: u32) -> @builtin(position) vec4f {
    let points = array<vec2f, 3>(vec2f(-1,-1), vec2f(3,-1), vec2f(-1,3));
    return vec4f(points[i], 0.25, 1);
}
@fragment fn mask(@builtin(position) p: vec4f) {
    if p.x > 1.0 { discard; }
}
@fragment fn fs_main(@builtin(position) p: vec4f) -> @location(0) vec4f {
    let lit = textureSampleCompareLevel(map, compare, p.xy / vec2f(2,1), 0.5);
    return vec4f(lit, lit, lit, 1);
}
`)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Release()
	shadow, err := d.CreateRenderPipeline(RenderPipelineDescriptor{
		Module: mod, FragmentEntry: "mask",
		DepthStencil: &DepthStencil{Format: FormatDepth32Float, DepthWrite: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer shadow.Release()
	groupLayout := d.CreateBindGroupLayout("depth sample",
		BindGroupLayoutEntry{Binding: 0, Visibility: StageFragment, Type: BindDepthTexture},
		BindGroupLayoutEntry{Binding: 1, Visibility: StageFragment, Type: BindComparisonSampler},
	)
	defer groupLayout.Release()
	layout := d.CreatePipelineLayout("depth sample", groupLayout)
	defer layout.Release()
	pipeline, err := d.CreateRenderPipeline(RenderPipelineDescriptor{
		Module: mod, Layout: layout, Targets: []ColorTarget{{Format: FormatRGBA8Unorm}},
		DepthStencil: &DepthStencil{Format: FormatDepth32Float, Compare: CompareAlways},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pipeline.Release()
	sampler, err := d.CreateSamplerWithDescriptor(SamplerDescriptor{Compare: CompareLessEqual})
	if err != nil {
		t.Fatal(err)
	}
	defer sampler.Release()
	group := d.CreateBindGroup(groupLayout, BindGroupEntry{Binding: 0, TextureView: dv}, BindGroupEntry{Binding: 1, Sampler: sampler})
	defer group.Release()

	e := d.CreateCommandEncoder()
	p := e.BeginRenderPass(RenderPassDescriptor{Depth: &DepthAttachment{View: dv, Clear: 1}})
	p.SetPipeline(shadow)
	p.Draw(3, 1, 0, 0)
	p.End()
	p = e.BeginRenderPass(RenderPassDescriptor{
		Color: []ColorAttachment{{View: cv}},
		Depth: &DepthAttachment{View: dv, ReadOnly: true},
	})
	p.SetPipeline(pipeline)
	p.SetBindGroup(0, group)
	p.Draw(3, 1, 0, 0)
	p.End()
	d.Submit(e.Finish())
	got, err := d.ReadTexture(color, 0, 0, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string([]byte{0, 0, 0, 255, 255, 255, 255, 255}) {
		t.Fatalf("comparison after depth-only discard = %v", got)
	}
}
