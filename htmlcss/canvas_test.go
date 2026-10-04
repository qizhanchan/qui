package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// findByRole walks a widget tree for the first widget carrying the role.
func findByRole(root qui.Widget, role string) qui.Widget {
	var found qui.Widget
	var walk func(w qui.Widget)
	walk = func(w qui.Widget) {
		if found != nil || w == nil {
			return
		}
		if qui.WidgetRole(w) == role {
			found = w
			return
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, ch := range c.ChildList() {
				walk(ch)
			}
		}
	}
	walk(root)
	return found
}

// A <canvas> with a wired paint callback renders a canvasLeaf sized from
// CSS (defaulting to 300×150), and its Draw invokes the callback.
func TestCanvasDrawCallback(t *testing.T) {
	res := RenderDoc(
		`<body><canvas id="c" class="art"></canvas></body>`,
		`.art { width: 240px; height: 120px; }`,
		Options{},
	)

	el, ok := res.ByID["c"].(*El)
	if !ok {
		t.Fatal("no El for #c")
	}

	painted := false
	el.SetCanvasDraw(func(cv qui.Canvas, b qui.Rect) { painted = true })
	res.Engine.Restyle()

	leaf, ok := el.canvasWidget.(*canvasLeaf)
	if !ok {
		t.Fatalf("canvas backing is %T, want *canvasLeaf", el.canvasWidget)
	}
	if leaf.placeholder {
		t.Error("wired canvas still shows the placeholder leaf")
	}
	if got := leaf.Measure(qui.Size{W: 1000, H: 1000}); got.W != 240 || got.H != 120 {
		t.Errorf("canvas measured %vx%v, want 240x120 (from CSS)", got.W, got.H)
	}

	leaf.Layout(qui.Rect{W: 240, H: 120})
	leaf.Draw(qui.NoopRenderer{}.Begin(qui.Size{W: 240, H: 120}))
	if !painted {
		t.Error("canvas paint callback was not invoked on Draw")
	}
}

// A <canvas> with no declared size falls back to HTML's intrinsic 300×150.
func TestCanvasDefaultSize(t *testing.T) {
	res := RenderDoc(`<body><canvas id="c"></canvas></body>`, ``, Options{})
	el := res.ByID["c"].(*El)
	el.SetCanvasDraw(func(cv qui.Canvas, b qui.Rect) {})
	res.Engine.Restyle()

	leaf := el.canvasWidget.(*canvasLeaf)
	if got := leaf.Measure(qui.Size{}); got.W != 300 || got.H != 150 {
		t.Errorf("default canvas measured %vx%v, want 300x150", got.W, got.H)
	}
}

// An un-wired <canvas> still produces a visible placeholder leaf so the
// region is a distinct box rather than collapsing to nothing.
func TestCanvasPlaceholder(t *testing.T) {
	res := RenderDoc(`<body><canvas id="c"></canvas></body>`, ``, Options{})
	el := res.ByID["c"].(*El)

	leaf, ok := el.canvasWidget.(*canvasLeaf)
	if !ok {
		t.Fatalf("un-wired canvas backing is %T, want *canvasLeaf placeholder", el.canvasWidget)
	}
	if !leaf.placeholder {
		t.Error("un-wired canvas did not use the placeholder leaf")
	}
	if findByRole(res.Root, qui.RoleImage) == nil {
		t.Error("canvas produced no image-role widget for the AX tree")
	}
}

// SetCanvas plugs an arbitrary widget in and sizes it from CSS.
func TestCanvasCustomWidget(t *testing.T) {
	res := RenderDoc(
		`<body><canvas id="c" class="v"></canvas></body>`,
		`.v { width: 200px; height: 100px; }`,
		Options{},
	)
	el := res.ByID["c"].(*El)

	custom := newCanvasLeaf(func(cv qui.Canvas, b qui.Rect) {}, 10, 10)
	el.SetCanvas(custom)
	res.Engine.Restyle()

	if el.canvasWidget != qui.Widget(custom) {
		t.Fatalf("canvas backing is %T, want the custom widget", el.canvasWidget)
	}
	if custom.Style().Width != 200 || custom.Style().Height != 100 {
		t.Errorf("custom widget sized %vx%v, want 200x100 from CSS",
			custom.Style().Width, custom.Style().Height)
	}
}
