//go:build cgo && !js

package wgpu

import (
	"testing"
	"unsafe"
)

// sharedOrSkip returns the device, skipping when Dawn is not installed.
func sharedOrSkip(t *testing.T) *Device {
	t.Helper()
	d, err := Shared()
	if err != nil {
		t.Skipf("WebGPU unavailable: %v", err)
	}
	SetErrorHandler(func(kind int, msg string) { t.Errorf("WebGPU error %d: %s", kind, msg) })
	t.Cleanup(func() { SetErrorHandler(nil) })
	return d
}

const triangleWGSL = `
struct VOut { @builtin(position) pos: vec4f, @location(0) color: vec4f };
@vertex fn vs_main(@builtin(vertex_index) i: u32) -> VOut {
    var p = array<vec2f, 3>(vec2f(-1.0, -1.0), vec2f(3.0, -1.0), vec2f(-1.0, 3.0));
    var o: VOut;
    o.pos = vec4f(p[i], 0.0, 1.0);
    o.color = vec4f(0.0, 0.5, 1.0, 1.0);
    return o;
}
@fragment fn fs_main(in: VOut) -> @location(0) vec4f { return in.color; }
`

func TestOffscreenTriangle(t *testing.T) {
	d := sharedOrSkip(t)
	t.Logf("adapter: %s (lib %s)", d.AdapterName(), LibraryPath())
	mod, err := d.CreateShaderModule("tri", triangleWGSL)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Release()
	pl, err := d.CreateRenderPipeline(RenderPipelineDescriptor{
		Label: "tri", Module: mod, Targets: []ColorTarget{{Format: FormatRGBA8Unorm}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pl.Release()
	tex := d.CreateTexture(TextureDescriptor{Width: 16, Height: 8, Format: FormatRGBA8Unorm,
		Usage: UsageRenderAttachment | UsageCopySrc})
	defer tex.Destroy()
	view := tex.CreateView()
	defer view.Release()
	enc := d.CreateCommandEncoder()
	pass := enc.BeginRenderPass(RenderPassDescriptor{Color: []ColorAttachment{{View: view}}})
	pass.SetPipeline(pl)
	pass.Draw(3, 1, 0, 0)
	pass.End()
	d.Submit(enc.Finish())
	pix, err := d.ReadTexture(tex, 0, 0, 16, 8)
	if err != nil {
		t.Fatal(err)
	}
	if got := pix[4*(3*16+5):][:4]; got[0] != 0 || got[1] < 126 || got[1] > 129 || got[2] != 255 || got[3] != 255 {
		t.Fatalf("pixel = %v, want ~[0 128 255 255]", got)
	}
}

func TestShaderError(t *testing.T) {
	d := sharedOrSkip(t)
	SetErrorHandler(nil)
	if _, err := d.CreateShaderModule("bad", "fn main( {"); err == nil {
		t.Fatal("bad WGSL compiled")
	} else {
		t.Logf("error (expected): %.120s", err)
	}
}

func TestWriteTextureRoundTrip(t *testing.T) {
	d := sharedOrSkip(t)
	tex := d.CreateTexture(TextureDescriptor{Width: 4, Height: 3, Format: FormatRGBA8Unorm,
		Usage: UsageCopyDst | UsageCopySrc | UsageTextureBinding})
	defer tex.Destroy()
	src := make([]byte, 4*4*3)
	for i := range src {
		src[i] = byte(i)
	}
	d.WriteTexture(tex, 0, 0, 4, 3, unsafe.Pointer(&src[0]), 16)
	got, err := d.ReadTexture(tex, 0, 0, 4, 3)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(src) {
		t.Fatalf("round trip differs")
	}
}
