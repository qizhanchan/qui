//go:build cgo && !js

package wgpu

import (
	"encoding/binary"
	"testing"
)

func TestDeviceRenderLimits(t *testing.T) {
	d := sharedOrSkip(t)
	limits, err := d.Limits()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("adapter %s, Dawn %s, enabled limits %+v", d.AdapterName(), DawnRevision, limits)
	if limits.MaxTextureDimension2D < 4096 || limits.MaxBindGroups < 3 ||
		limits.MaxSampledTexturesPerShaderStage < 5 || limits.MaxStorageBuffersPerShaderStage < 1 ||
		limits.MaxUniformBufferBindingSize < 16384 || limits.MaxStorageBufferBindingSize < 65536 ||
		limits.MaxBufferSize < limits.MaxStorageBufferBindingSize ||
		limits.MinUniformBufferOffsetAlignment == 0 || limits.MinStorageBufferOffsetAlignment == 0 {
		t.Fatalf("device cannot satisfy renderer baseline: %+v", limits)
	}
}

// A color above 1 must survive an HDR render attachment. A separate
// depth-only pass verifies that a shadow pass needs no color target.
func TestHDRAndDepthOnlyAttachments(t *testing.T) {
	d := sharedOrSkip(t)
	hdr := d.CreateTexture(TextureDescriptor{
		Label: "HDR probe", Width: 4, Height: 4, Format: FormatRGBA16Float,
		Usage: UsageRenderAttachment | UsageCopySrc | UsageTextureBinding,
	})
	defer hdr.Destroy()
	depth := d.CreateTexture(TextureDescriptor{
		Label: "shadow probe", Width: 4, Height: 4, Format: FormatDepth32Float,
		Usage: UsageRenderAttachment | UsageTextureBinding,
	})
	defer depth.Destroy()

	hv, dv := hdr.CreateView(), depth.CreateView()
	defer hv.Release()
	defer dv.Release()
	mod, err := d.CreateShaderModule("shadow probe", triangleWGSL)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Release()
	shadow, err := d.CreateRenderPipeline(RenderPipelineDescriptor{
		Label: "depth-only shadow probe", Module: mod,
		DepthStencil: &DepthStencil{Format: FormatDepth32Float, DepthWrite: true, Compare: CompareLess},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer shadow.Release()
	enc := d.CreateCommandEncoder()
	p := enc.BeginRenderPass(RenderPassDescriptor{Depth: &DepthAttachment{View: dv, Clear: 1}})
	p.SetPipeline(shadow)
	p.Draw(3, 1, 0, 0)
	p.End()
	p = enc.BeginRenderPass(RenderPassDescriptor{Color: []ColorAttachment{{
		View: hv, Load: LoadOpClear, Store: StoreOpStore, Clear: Color{R: 2, G: 0.5, B: 0, A: 1},
	}}})
	p.End()
	d.Submit(enc.Finish())

	pixels, err := d.ReadTexture(hdr, 0, 0, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(pixels) != 4*4*8 {
		t.Fatalf("HDR readback length %d, want 128", len(pixels))
	}
	for px := 0; px < len(pixels); px += 8 {
		for channel, want := range []uint16{0x4000, 0x3800, 0x0000, 0x3c00} {
			got := binary.LittleEndian.Uint16(pixels[px+channel*2:])
			if got != want {
				t.Fatalf("HDR pixel %d channel %d: 0x%04x, want 0x%04x", px/8, channel, got, want)
			}
		}
	}
}
