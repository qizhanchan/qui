package scene3d

import (
	"time"

	"github.com/qizhanchan/qui"
)

// Viewport is a 2D rectangle backed by an offscreen 3D render.
// Each frame (or on demand when Continuous=false) the viewport
// renders its Scene with WebGPU into private textures (4× MSAA color +
// depth, resolved), then composites the result under the 2D layer
// (GPUCanvas.QueueRetainedGPUDraw + GPUFrame.DrawTexture). On the CPU
// path (no Dawn) it draws a placeholder.
//
// Typical use — an orbit-camera 3D preview in a design tool panel:
//
//	scene := scene3d.NewScene()
//	cube := scene3d.NewNode("cube")
//	cube.Mesh = scene3d.NewCubeMesh()
//	cube.Material = scene3d.NewLitMaterial(qui.Color{R:0.7, G:0.4, B:0.9, A:1})
//	scene.Root.AddChild(cube)
//	scene.Lights = []scene3d.Light{scene3d.DirectionalLight{
//	    Direction: scene3d.Vec3{-0.4, -1, -0.2}, Color: qui.Color{1,1,1,1}, Intensity: 1,
//	}}
//	vp := scene3d.NewViewport(scene)
//	vp.Continuous = true
//	container.AddChild(vp)
//
// Lifetime: GPU resources are created lazily on the first frame and
// released with Destroy() on the UI thread.
type Viewport struct {
	qui.BaseWidget
	Scene      *Scene
	Continuous bool // if true, re-render every frame (animation / interactive camera)
	OnFrame    func(dt time.Duration)
	// Drawable is an optional raw-WebGPU hook used INSTEAD of Scene.
	// When set, rendering bypasses the scene graph — the callback gets
	// the viewport's render pass (cleared, depth-tested targets).
	Drawable Drawable
	// ClearColor is the color the 3D render starts from. Defaults to
	// opaque near-black if zero-valued.
	ClearColor qui.Color

	gpu viewportGPU // targets and per-node state (gpu_render.go)

	physBox    qui.Rect
	dirtyScene bool
	lastTick   time.Time
}

// MSAASamples is the multisample count of the viewport's render target.
// 4 is a good quality / bandwidth tradeoff on integrated GPUs.
const MSAASamples = 4

// NewViewport creates a viewport bound to the given scene. Pass
// nil to use Drawable-based custom rendering exclusively.
func NewViewport(scene *Scene) *Viewport {
	v := &Viewport{
		BaseWidget: qui.NewBaseWidget(),
		Scene:      scene,
		dirtyScene: true,
	}
	v.Style().Background = qui.Color{} // fully transparent — 3D shows through
	return v
}

// InvalidateScene marks the rendered content as stale; next Draw will
// re-render the scene. Continuous viewports ignore this (they
// repaint every frame regardless).
func (v *Viewport) InvalidateScene() {
	if v == nil {
		return
	}
	v.dirtyScene = true
}

// Tick implements Tickable for Continuous viewports. The returned
// Rect is the widget's current bounds so the window repaints us.
func (v *Viewport) Tick(now time.Time) qui.Rect {
	if v == nil || !v.Continuous {
		return qui.Rect{}
	}
	if !v.lastTick.IsZero() && v.OnFrame != nil {
		v.OnFrame(now.Sub(v.lastTick))
	}
	v.lastTick = now
	return qui.PaintBoundsInWindow(v)
}

// Measure: viewport takes whatever space the layout gives it.
func (v *Viewport) Measure(available qui.Size) qui.Size {
	// Default min size so a bare viewport has something to show.
	w := available.W
	if w <= 0 {
		w = 240
	}
	h := available.H
	if h <= 0 {
		h = 180
	}
	return qui.Size{W: w, H: h}
}

// HitTest returns self so mouse events route to the viewport
// (camera orbit, pick).
func (v *Viewport) HitTest(p qui.Point) qui.Widget {
	if v.Bounds().Contains(p) {
		return v
	}
	return nil
}
