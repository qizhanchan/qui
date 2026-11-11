//go:build cgo && !js

package wgpu

import "testing"

func TestRenderPassTimestamps(t *testing.T) {
	d := sharedOrSkip(t)
	if !d.SupportsTimestampQueries() {
		t.Skipf("timestamp queries unavailable on %s", d.AdapterName())
	}
	if q, err := d.NewTimestampQuery(maxTimestampQueries + 1); err == nil || q != nil {
		t.Fatal("oversized query set accepted")
	}
	q, err := d.NewTimestampQuery(8)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Release()
	tex := d.CreateTexture(TextureDescriptor{
		Width: 512, Height: 512, Format: FormatRGBA8Unorm, Usage: UsageRenderAttachment,
	})
	defer tex.Release()
	view := tex.CreateView()
	defer view.Release()
	mod, err := d.CreateShaderModule("timestamp triangle", triangleWGSL)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Release()
	pl, err := d.CreateRenderPipeline(RenderPipelineDescriptor{
		Module: mod, Targets: []ColorTarget{{Format: FormatRGBA8Unorm, Blend: BlendPremultiplied()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pl.Release()

	for iteration := range 2 {
		enc := d.CreateCommandEncoder()
		defer enc.Release()
		desc := RenderPassDescriptor{Color: []ColorAttachment{{View: view}}}
		desc.TimestampWrites = &PassTimestampWrites{Query: q, Begin: 8, End: 1}
		if enc.BeginRenderPass(desc) != nil {
			t.Fatal("out-of-bounds timestamp write accepted")
		}
		for begin := 0; begin < 4; begin += 2 {
			desc.TimestampWrites = &PassTimestampWrites{Query: q, Begin: begin, End: begin + 1}
			pass := enc.BeginRenderPass(desc)
			if pass == nil {
				t.Fatal("valid timestamp write rejected")
			}
			pass.SetPipeline(pl)
			for range 16 {
				pass.Draw(3, 1, 0, 0)
			}
			pass.End()
		}
		if err := q.Resolve(enc, 4); err != nil {
			t.Fatal(err)
		}
		commands := enc.Finish()
		if _, err := q.Read(4); err == nil {
			t.Fatal("reading timestamps before submission succeeded")
		}
		d.Submit(commands)
		if _, err := q.Read(3); err == nil {
			t.Fatal("reading a different count from the resolve succeeded")
		}
		got, err := q.Read(4)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 4 || got[0] == 0 || got[1] < got[0] || got[2] == 0 || got[3] < got[2] {
			t.Fatalf("invalid timestamp pairs from %s: %v", d.AdapterName(), got)
		}
		t.Logf("%s sample %d render pass durations in ns: %d and %d", d.AdapterName(), iteration+1, got[1]-got[0], got[3]-got[2])
	}
}
