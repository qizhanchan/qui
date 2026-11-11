//go:build cgo && !js

package wgpu

import (
	"bytes"
	"testing"
)

func TestMipUploadViewAndSampler(t *testing.T) {
	d := sharedOrSkip(t)
	tex := d.CreateTexture(TextureDescriptor{
		Width: 4, Height: 4, Format: FormatRGBA8Unorm,
		Usage: UsageCopyDst | UsageTextureBinding, MipLevels: 3,
	})
	if tex == nil || tex.MipLevels() != 3 {
		t.Fatal("mip texture allocation failed")
	}
	defer tex.Destroy()
	base := bytes.Repeat([]byte{255, 0, 0, 255}, 16)
	copy(base, []byte{255, 255, 0, 255})
	copy(base[3*4*4:], []byte{255, 0, 255, 255})
	levels := [][]byte{
		base, bytes.Repeat([]byte{0, 255, 0, 255}, 4), {0, 0, 255, 255},
	}
	for i, pixels := range levels {
		n := 4 >> i
		if err := d.WriteTextureLevel(tex, i, 0, 0, n, n, pixels, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.WriteTextureLevel(tex, 1, 1, 1, 2, 2, levels[1], 0); err == nil {
		t.Fatal("out-of-bounds mip upload accepted")
	}
	if err := d.WriteTextureLevel(tex, 1, 0, 0, 2, 2, levels[1][:8], 8); err == nil {
		t.Fatal("truncated mip upload accepted")
	}
	if _, err := tex.CreateMipView(2, 2); err == nil {
		t.Fatal("invalid mip view accepted")
	}
	view := tex.CreateView()
	defer view.Release()
	last, err := tex.CreateMipView(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer last.Release()
	sampler, err := d.CreateSamplerWithDescriptor(SamplerDescriptor{
		AddressU: AddressRepeat, AddressV: AddressClampToEdge, MinFilter: FilterNearest,
		MagFilter: FilterNearest, MipFilter: FilterLinear,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sampler.Release()
	mod, err := d.CreateShaderModule("mip probe", `
@group(0) @binding(0) var mips: texture_2d<f32>;
@group(0) @binding(1) var tail: texture_2d<f32>;
@group(0) @binding(2) var sample_mode: sampler;
@vertex fn vs_main(@builtin(vertex_index) i: u32) -> @builtin(position) vec4f {
    let p = array<vec2f, 3>(vec2f(-1,-1), vec2f(3,-1), vec2f(-1,3));
    return vec4f(p[i], 0, 1);
}
@fragment fn fs_main(@builtin(position) p: vec4f) -> @location(0) vec4f {
    switch u32(p.x) {
        case 0: { return textureSampleLevel(mips, sample_mode, vec2f(0.375), 0.0); }
        case 1: { return textureSampleLevel(mips, sample_mode, vec2f(0.375), 0.5); }
        case 2: { return textureSampleLevel(tail, sample_mode, vec2f(0.375), 0.0); }
        default: { return textureSampleLevel(mips, sample_mode, vec2f(1.125, 1.125), 0.0); }
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Release()
	pl, err := d.CreateRenderPipeline(RenderPipelineDescriptor{
		Module: mod, Targets: []ColorTarget{{Format: FormatRGBA8Unorm}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pl.Release()
	layout := pl.BindGroupLayout(0)
	defer layout.Release()
	group := d.CreateBindGroup(layout,
		BindGroupEntry{Binding: 0, TextureView: view},
		BindGroupEntry{Binding: 1, TextureView: last},
		BindGroupEntry{Binding: 2, Sampler: sampler},
	)
	defer group.Release()
	target := d.CreateTexture(TextureDescriptor{
		Width: 4, Height: 1, Format: FormatRGBA8Unorm,
		Usage: UsageRenderAttachment | UsageCopySrc,
	})
	defer target.Destroy()
	out := target.CreateView()
	defer out.Release()
	encoder := d.CreateCommandEncoder()
	pass := encoder.BeginRenderPass(RenderPassDescriptor{Color: []ColorAttachment{{View: out}}})
	pass.SetPipeline(pl)
	pass.SetBindGroup(0, group)
	pass.Draw(3, 1, 0, 0)
	pass.End()
	d.Submit(encoder.Finish())
	pixels, err := d.ReadTexture(target, 0, 0, 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{255, 0, 0, 255, 128, 128, 0, 255, 0, 0, 255, 255, 255, 0, 255, 255}
	if len(pixels) != len(want) {
		t.Fatalf("unexpected readback: %v", pixels)
	}
	for i := range want {
		if delta := int(pixels[i]) - int(want[i]); delta < -1 || delta > 1 {
			t.Fatalf("pixels = %v, want %v", pixels, want)
		}
	}
}
