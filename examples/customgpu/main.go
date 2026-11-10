//go:build cgo && !js

package main

// Demonstrates Window.SetPreRender — a WebGPU hook that draws straight
// into the frame before the widget tree paints on top. Here it renders an
// animated full-screen shader background (Shadertoy-style) in WGSL, and
// 2D widgets render over it.
//
// The widget tree itself is regular — a Label and a Button sitting on
// top of the GPU-rendered background, which shows through wherever the
// 2D layer is transparent. The window presents with WebGPU (Dawn); run
// `go run github.com/qizhanchan/qui/cmd/qui-dawn` once to install it.

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/gpu"
	"github.com/qizhanchan/qui/widgets"
)

const shaderWGSL = `
struct Params { time: f32, width: f32, height: f32, pad: f32 };
@group(0) @binding(0) var<uniform> params: Params;

struct VOut { @builtin(position) pos: vec4f, @location(0) uv: vec2f };

@vertex fn vs_main(@builtin(vertex_index) i: u32) -> VOut {
    var p = array<vec2f, 3>(vec2f(-1.0, -1.0), vec2f(3.0, -1.0), vec2f(-1.0, 3.0));
    var o: VOut;
    o.pos = vec4f(p[i], 0.0, 1.0);
    o.uv = p[i];
    return o;
}

@fragment fn fs_main(in: VOut) -> @location(0) vec4f {
    var uv = in.uv;
    uv.x *= params.width / params.height;
    let t = params.time * 0.35;
    var d = 0.0;
    for (var i = 0; i < 4; i++) {
        let fi = f32(i);
        d += sin(uv.x * (3.0 + fi) + t + fi) * cos(uv.y * (3.0 + fi) + t);
    }
    var col = 0.5 + 0.5 * cos(vec3f(0.0, 2.0, 4.0) + d + t);
    col *= smoothstep(1.4, 0.2, length(uv));
    return vec4f(col, 1.0);
}
`

// background is the shader's pipeline, made on the first frame (the
// pipeline's color target must match the frame's format).
type background struct {
	pipeline *gpu.RenderPipeline
	params   *gpu.Buffer
	group    *gpu.BindGroup
	start    time.Time
}

func (b *background) draw(f *qui.GPUFrame) {
	if b.pipeline == nil {
		mod, err := f.Device.CreateShaderModule("background", shaderWGSL)
		if err != nil {
			log.Fatal(err)
		}
		defer mod.Release()
		b.pipeline, err = f.Device.CreateRenderPipeline(gpu.RenderPipelineDescriptor{
			Label: "background", Module: mod,
			Targets: []gpu.ColorTarget{{Format: f.Format}},
		})
		if err != nil {
			log.Fatal(err)
		}
		b.params = f.Device.CreateBuffer("params", gpu.BufferUniform|gpu.BufferCopyDst, 16)
		b.group = f.Device.CreateBindGroup(b.pipeline.BindGroupLayout(0),
			gpu.BindGroupEntry{Binding: 0, Buffer: b.params})
		b.start = time.Now()
	}
	var p [16]byte
	binary.LittleEndian.PutUint32(p[0:], math.Float32bits(float32(time.Since(b.start).Seconds())))
	binary.LittleEndian.PutUint32(p[4:], math.Float32bits(f.Size.W))
	binary.LittleEndian.PutUint32(p[8:], math.Float32bits(f.Size.H))
	f.Device.WriteBuffer(b.params, 0, p[:])
	f.RenderPass(func(pass *gpu.RenderPass) {
		pass.SetPipeline(b.pipeline)
		pass.SetBindGroup(0, b.group)
		pass.Draw(3, 1, 0, 0)
	})
}

func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	window, err := app.NewWindow("qui — custom GPU background", 800, 500)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewAutoRenderer())

	var clicks int
	label := widgets.NewLabel("WGSL shader background. 2D UI paints on top.")
	label.Style().Foreground = qui.ColorWhite
	label.Style().Font.Size = 20
	label.Style().Padding = qui.Insets{Top: 10, Right: 16, Bottom: 10, Left: 16}
	label.Style().Background = qui.Color{R: 0, G: 0, B: 0, A: 0.4}
	label.Style().Radius = 6

	countLabel := widgets.NewLabel("clicks: 0")
	countLabel.Style().Foreground = qui.ColorWhite
	countLabel.Style().Font.Size = 16

	btn := widgets.NewButton("Tick up", func() {
		clicks++
		countLabel.SetText(fmt.Sprintf("clicks: %d", clicks))
		window.Invalidate()
	})

	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical, Gap: 16, AlignItems: qui.AlignCenter, Justify: qui.JustifyCenter}, label, countLabel, btn)
	root.Style().Background = qui.Color{} // transparent: the shader shows through
	window.SetRoot(root)

	// The background animates: redraw the GPU content every frame
	// without repainting any widget.
	window.RegisterAnimator(&tickEveryFrame{window: window})

	bg := &background{}
	window.SetPreRender(bg.draw)
	captureIfAsked(window, app)

	app.Run()
}

// tickEveryFrame is an Animator that never finishes — it asks for a
// GPU-only redraw every tick so the shader's time advances.
type tickEveryFrame struct {
	window *qui.Window
}

func (t *tickEveryFrame) Tick(time.Time) (qui.Rect, bool) {
	t.window.RequestGPURedraw()
	return qui.Rect{}, false
}
func (t *tickEveryFrame) Stop() {}
