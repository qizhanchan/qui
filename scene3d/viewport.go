package scene3d

import (
	"time"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/qizhanchan/qui"
)

// Viewport is a 2D rectangle backed by an offscreen 3D render.
// Each frame (or on demand when Continuous=false) the viewport
// renders its Scene into a private FBO, then composites the FBO's
// color attachment onto the 2D canvas via GPUCanvas.DrawTexture.
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
// Lifetime: FBO resources are created lazily on first Draw (in the
// window's GL context) and must be released on the main goroutine
// via Destroy() before the window closes.
type Viewport struct {
	qui.BaseWidget
	Scene      *Scene
	Continuous bool // if true, re-render every frame (animation / interactive camera)
	OnFrame    func(dt time.Duration)
	// Drawable is an optional raw-GL hook used INSTEAD of Scene.
	// When set, rendering bypasses the scene graph — the callback
	// gets a fresh FBO bound with viewport + scissor already set.
	Drawable GLDrawable
	// ClearColor is the GL clearColor used before each 3D render.
	// Defaults to opaque near-black if zero-valued.
	ClearColor qui.Color

	// Resolve target: a single-sample texture DrawTexture samples from.
	fbo uint32
	tex uint32
	// Multisampled render target (4× MSAA) that the scene renders into,
	// then blit-resolves into fbo/tex. Antialiases both filled edges and
	// GL_LINES overlays.
	msaaFBO   uint32
	msaaColor uint32
	msaaDepth uint32

	fboSize    qui.Size
	dirtyScene bool
	lastTick   time.Time
}

// msaaSamples is the multisample count for the viewport's render
// target. 4 is a good quality/bandwidth tradeoff on integrated GPUs.
const msaaSamples = 4

// GLDrawable is an escape hatch: widgets that want full control of
// the render pass implement this instead of using Scene.
type GLDrawable interface {
	DrawGL(state qui.GLState, viewportPx qui.Rect)
}

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

// InvalidateScene marks the FBO content as stale; next Draw will
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

// Draw either renders the scene into its FBO (if dirty or
// continuous) and composites the result, or skips the render and
// re-composites the cached FBO. When the canvas isn't GPU-backed
// we no-op (tests with NoopRenderer).
func (v *Viewport) Draw(canvas qui.Canvas) {
	gpu, ok := canvas.(qui.GPUCanvas)
	if !ok {
		// CPU fallback: draw a placeholder background so empty
		// FlexLayout cells aren't confusing.
		canvas.FillRect(v.Bounds(), qui.Color{R: 0.10, G: 0.10, B: 0.12, A: 1})
		return
	}

	// Punch a hole in the 2D CPU buffer at our bounds so the 3D
	// texture underneath shows through. Widgets drawn later (e.g.,
	// overlay tooltips) will re-paint opaque pixels on top.
	canvas.FillRect(v.Bounds(), qui.Color{R: 0, G: 0, B: 0, A: 0})

	bounds := v.Bounds()
	// Queue the 3D render + composite as a deferred GL op so it
	// runs between the framebuffer clear and the 2D blit.
	gpu.QueueGLDraw(func(state qui.GLState) {
		v.renderScene(state, bounds)
	})
}

func (v *Viewport) renderScene(state qui.GLState, bounds qui.Rect) {
	if bounds.IsEmpty() {
		return
	}
	// Viewport rect in physical pixels.
	scaleX := float32(1)
	scaleY := float32(1)
	if state.LogicalSize.W > 0 {
		scaleX = state.FramebufferSize.W / state.LogicalSize.W
	}
	if state.LogicalSize.H > 0 {
		scaleY = state.FramebufferSize.H / state.LogicalSize.H
	}
	physRect := qui.Rect{
		X: bounds.X * scaleX,
		Y: bounds.Y * scaleY,
		W: bounds.W * scaleX,
		H: bounds.H * scaleY,
	}
	if physRect.W <= 0 || physRect.H <= 0 {
		return
	}
	targetSize := qui.Size{W: physRect.W, H: physRect.H}

	// Lazy FBO allocation / resize on first use or size change.
	if v.fbo == 0 || v.fboSize != targetSize {
		v.allocFBO(int32(targetSize.W), int32(targetSize.H))
		v.fboSize = targetSize
		v.dirtyScene = true
	}

	if v.dirtyScene || v.Continuous {
		v.renderIntoFBO(int32(targetSize.W), int32(targetSize.H))
		v.dirtyScene = false
	}

	// Composite the FBO color texture onto the default framebuffer.
	// GLRenderer.DrawTexture expects coordinates in framebuffer
	// pixels (which is what physRect already is). FlipY=false —
	// FBO color attachment is native OpenGL bottom-up.
	if glr := qui.ActiveGLRenderer(); glr != nil {
		glr.DrawTexture(
			v.tex,
			qui.Rect{W: targetSize.W, H: targetSize.H}, // full texture
			physRect,
			int32(targetSize.W),
			int32(targetSize.H),
			qui.TextureOpts{FlipY: false, Tint: qui.Color{R: 1, G: 1, B: 1, A: 1}, Opacity: 1},
		)
	}
}

// allocFBO (re)allocates the multisampled render target + the
// single-sample resolve target for the viewport.
func (v *Viewport) allocFBO(w, h int32) {
	v.freeFBO()

	// Multisampled render target (color + depth as MS renderbuffers).
	gl.GenFramebuffers(1, &v.msaaFBO)
	gl.BindFramebuffer(gl.FRAMEBUFFER, v.msaaFBO)
	gl.GenRenderbuffers(1, &v.msaaColor)
	gl.BindRenderbuffer(gl.RENDERBUFFER, v.msaaColor)
	gl.RenderbufferStorageMultisample(gl.RENDERBUFFER, msaaSamples, gl.RGBA8, w, h)
	gl.FramebufferRenderbuffer(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.RENDERBUFFER, v.msaaColor)
	gl.GenRenderbuffers(1, &v.msaaDepth)
	gl.BindRenderbuffer(gl.RENDERBUFFER, v.msaaDepth)
	gl.RenderbufferStorageMultisample(gl.RENDERBUFFER, msaaSamples, gl.DEPTH24_STENCIL8, w, h)
	gl.FramebufferRenderbuffer(gl.FRAMEBUFFER, gl.DEPTH_STENCIL_ATTACHMENT, gl.RENDERBUFFER, v.msaaDepth)

	// Single-sample resolve target: a texture DrawTexture can sample.
	gl.GenFramebuffers(1, &v.fbo)
	gl.BindFramebuffer(gl.FRAMEBUFFER, v.fbo)
	gl.GenTextures(1, &v.tex)
	gl.BindTexture(gl.TEXTURE_2D, v.tex)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, w, h, 0, gl.RGBA, gl.UNSIGNED_BYTE, nil)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.FramebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, v.tex, 0)

	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
}

// freeFBO releases all FBO-related GL objects (both targets).
func (v *Viewport) freeFBO() {
	if v.tex != 0 {
		gl.DeleteTextures(1, &v.tex)
	}
	if v.fbo != 0 {
		gl.DeleteFramebuffers(1, &v.fbo)
	}
	if v.msaaColor != 0 {
		gl.DeleteRenderbuffers(1, &v.msaaColor)
	}
	if v.msaaDepth != 0 {
		gl.DeleteRenderbuffers(1, &v.msaaDepth)
	}
	if v.msaaFBO != 0 {
		gl.DeleteFramebuffers(1, &v.msaaFBO)
	}
	v.tex, v.fbo, v.msaaColor, v.msaaDepth, v.msaaFBO = 0, 0, 0, 0, 0
}

// renderIntoFBO renders the scene into the multisampled target, then
// resolves it into the single-sample texture DrawTexture composites.
func (v *Viewport) renderIntoFBO(w, h int32) {
	gl.BindFramebuffer(gl.FRAMEBUFFER, v.msaaFBO)
	gl.Viewport(0, 0, w, h)
	gl.Enable(gl.MULTISAMPLE)
	gl.Enable(gl.DEPTH_TEST)
	gl.DepthFunc(gl.LESS)
	gl.Enable(gl.CULL_FACE)
	gl.CullFace(gl.BACK)
	gl.Disable(gl.BLEND)
	gl.Disable(gl.SCISSOR_TEST)
	// Push filled triangles slightly back in depth so coincident edge
	// lines (Node.Lines) win the depth test and don't z-fight the faces.
	gl.Enable(gl.POLYGON_OFFSET_FILL)
	gl.PolygonOffset(1, 1)

	c := v.ClearColor
	if c == (qui.Color{}) {
		c = qui.Color{R: 0.08, G: 0.08, B: 0.10, A: 1}
	}
	gl.ClearColor(c.R, c.G, c.B, c.A)
	gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)

	if v.Drawable != nil {
		v.Drawable.DrawGL(qui.GLState{
			FramebufferSize: qui.Size{W: float32(w), H: float32(h)},
			LogicalSize:     qui.Size{W: float32(w), H: float32(h)},
		}, qui.Rect{W: float32(w), H: float32(h)})
	} else if v.Scene != nil {
		aspect := float32(w) / float32(h)
		v.drawNode(v.Scene.Root, Mat4Identity(), aspect)
	}

	// Resolve MSAA → single-sample texture.
	gl.BindFramebuffer(gl.READ_FRAMEBUFFER, v.msaaFBO)
	gl.BindFramebuffer(gl.DRAW_FRAMEBUFFER, v.fbo)
	gl.BlitFramebuffer(0, 0, w, h, 0, 0, w, h, gl.COLOR_BUFFER_BIT, gl.NEAREST)
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
}

func (v *Viewport) drawNode(n *Node, parentWorld Mat4, aspect float32) {
	if n == nil || !n.Visible {
		return
	}
	world := Mat4Mul(parentWorld, n.Transform.LocalMatrix())
	if n.Mesh != nil && n.Material != nil {
		n.Material.Bind(v.Scene, aspect, world)
		n.Mesh.Draw()
	}
	if n.Lines != nil {
		lm := n.LineMaterial
		if lm == nil {
			lm = defaultLineMaterial()
		}
		lm.Bind(v.Scene, aspect, world)
		n.Lines.Draw()
	}
	for _, c := range n.Children {
		v.drawNode(c, world, aspect)
	}
}

// Destroy releases FBO resources. Must be called on the main goroutine
// with the owning GL context current.
func (v *Viewport) Destroy() {
	if v == nil {
		return
	}
	v.freeFBO()
}
