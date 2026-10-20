package qui

import (
	"testing"

	"github.com/qizhanchan/qui/internal/platform/contract"
	"github.com/qizhanchan/qui/internal/platform/headless"
)

// The headless backend is what offscreen checks (CI, containers,
// QUI_SNAPSHOT) stand on, so these tests run a real Window on it end to
// end: platform window → default renderer → paint → presentCPU → Snapshot.

// headlessProbe is the slice of the headless backend's window the tests
// read back.
type headlessProbe interface {
	contract.Window
	Frames() int
}

func newHeadlessTestWindow(t *testing.T, scale float32, w, h int) (*Window, headlessProbe) {
	t.Helper()
	win, err := newWindowFromConfig(adaptApp(headless.New(scale)), platformWindowConfig{Title: "t", Width: w, Height: h})
	if err != nil {
		t.Fatalf("newWindowFromConfig: %v", err)
	}
	return win, win.plat.(contractWindow).w.(headlessProbe)
}

// fillWidget paints its whole bounds one color.
type fillWidget struct {
	BaseWidget
	c Color
}

func (f *fillWidget) Draw(canvas Canvas) { canvas.FillRect(f.Bounds(), f.c) }

func TestHeadlessWindowRendersOffscreen(t *testing.T) {
	win, hw := newHeadlessTestWindow(t, 1, 40, 30)
	if _, ok := win.renderer.(*CPURenderer); !ok {
		t.Fatalf("headless window should default to CPURenderer, got %T", win.renderer)
	}
	red := Color{R: 1, A: 1}
	win.SetRoot(&fillWidget{c: red})
	win.Step()

	img := win.Snapshot()
	if img == nil {
		t.Fatal("no frame after Step")
	}
	if img.Rect.Dx() != 40 || img.Rect.Dy() != 30 {
		t.Fatalf("frame is %v, want 40x30", img.Rect)
	}
	if got := img.RGBAAt(20, 15); got.R != 255 || got.G != 0 || got.A != 255 {
		t.Fatalf("center pixel %v, want opaque red", got)
	}
	if hw.Frames() != 1 {
		t.Fatalf("presentCPU ran %d times, want 1", hw.Frames())
	}
}

func TestHeadlessGLRendererFallsBackToCPU(t *testing.T) {
	win, _ := newHeadlessTestWindow(t, 1, 10, 10)
	win.SetRenderer(NewGLRenderer())
	if _, ok := win.renderer.(gpuContextRenderer); ok {
		t.Fatalf("a GL renderer on a surface without a GPU context must be replaced, got %T", win.renderer)
	}
	win.SetRoot(&fillWidget{c: Color{G: 1, A: 1}})
	win.Step()
	if img := win.Snapshot(); img == nil || img.RGBAAt(5, 5).G != 255 {
		t.Fatal("fallback renderer produced no green frame")
	}
}

func TestHeadlessScaleRendersHiDPIFramebuffer(t *testing.T) {
	win, _ := newHeadlessTestWindow(t, 2, 40, 30)
	win.SetRoot(&fillWidget{c: Color{B: 1, A: 1}})
	win.Step()
	img := win.Snapshot()
	if img == nil || img.Rect.Dx() != 80 || img.Rect.Dy() != 60 {
		t.Fatalf("scale 2 should render an 80x60 framebuffer, got %v", img.Rect)
	}
}

func TestHeadlessResizeReachesTheWindow(t *testing.T) {
	win, hw := newHeadlessTestWindow(t, 1, 40, 30)
	win.Step()
	hw.SetSize(64, 48)
	win.Step()
	if s := win.Size(); s.W != 64 || s.H != 48 {
		t.Fatalf("window size %v after setSize(64,48)", s)
	}
	if img := win.Snapshot(); img.Rect.Dx() != 64 {
		t.Fatalf("frame width %d after resize, want 64", img.Rect.Dx())
	}
}
