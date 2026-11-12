//go:build cgo && !js

package wgpu

import "testing"

// A clip distance cuts a full-target triangle at x = 0: the left half is
// drawn, the right half is not.
func TestClipDistances(t *testing.T) {
	d := sharedOrSkip(t)
	if !d.SupportsClipDistances() {
		t.Skipf("clip distances unavailable on %s", d.AdapterName())
	}
	mod, err := d.CreateShaderModule("clipped", `
enable clip_distances;
struct VOut { @builtin(position) pos: vec4f, @builtin(clip_distances) clip: array<f32, 1> };
@vertex fn vs_main(@builtin(vertex_index) i: u32) -> VOut {
    var p = array<vec2f, 3>(vec2f(-1.0, -1.0), vec2f(3.0, -1.0), vec2f(-1.0, 3.0));
    var o: VOut;
    o.pos = vec4f(p[i], 0.0, 1.0);
    o.clip[0] = -p[i].x; // keep x <= 0
    return o;
}
@fragment fn fs_main() -> @location(0) vec4f { return vec4f(1.0); }
`)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Release()
	pl, err := d.CreateRenderPipeline(RenderPipelineDescriptor{Module: mod, Targets: []ColorTarget{{Format: FormatRGBA8Unorm}}})
	if err != nil {
		t.Fatal(err)
	}
	defer pl.Release()
	tex := d.CreateTexture(TextureDescriptor{Width: 16, Height: 4, Format: FormatRGBA8Unorm, Usage: UsageRenderAttachment | UsageCopySrc})
	defer tex.Destroy()
	view := tex.CreateView()
	defer view.Release()
	enc := d.CreateCommandEncoder()
	pass := enc.BeginRenderPass(RenderPassDescriptor{Color: []ColorAttachment{{View: view}}})
	pass.SetPipeline(pl)
	pass.Draw(3, 1, 0, 0)
	pass.End()
	d.Submit(enc.Finish())
	pix, err := d.ReadTexture(tex, 0, 0, 16, 4)
	if err != nil {
		t.Fatal(err)
	}
	for x := range 16 {
		got, want := pix[4*(16+x)], byte(255)
		if x >= 8 {
			want = 0
		}
		if got != want {
			t.Fatalf("pixel %d = %d, want %d", x, got, want)
		}
	}
}
