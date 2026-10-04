package qui

import (
	"image"
	"testing"
	"time"
)

// showOrderWindow records the sequence of platform calls ShowAt makes,
// because the ORDER is the contract: showing before positioning makes the
// panel appear at its previous location for one frame, which in an input
// method reads as a flicker on every single keystroke.
type showOrderWindow struct {
	*fakePlatformWindow
	calls []string
}

func (w *showOrderWindow) setPos(x, y int) bool {
	w.calls = append(w.calls, "setPos")
	return w.fakePlatformWindow.setPos(x, y)
}

func (w *showOrderWindow) setVisible(visible, activate bool) {
	w.calls = append(w.calls, "setVisible")
	w.fakePlatformWindow.setVisible(visible, activate)
}

func TestShowAtPositionsBeforeShowing(t *testing.T) {
	f := &showOrderWindow{fakePlatformWindow: newFakePlatformWindow()}
	w := newWindowOnFake(f.fakePlatformWindow)
	w.plat = f
	w.kind = WindowOverlayPanel

	w.ShowAt(120, 340)

	if len(f.calls) != 2 || f.calls[0] != "setPos" || f.calls[1] != "setVisible" {
		t.Fatalf("ShowAt call order = %v, want [setPos setVisible]", f.calls)
	}
	if x, y, _ := f.pos(); x != 120 || y != 340 {
		t.Errorf("position = (%d,%d), want (120,340)", x, y)
	}
	if !f.visible {
		t.Error("panel not visible after ShowAt")
	}
}

// A panel that takes focus is not a panel: the app the user is typing into
// would deactivate the moment a candidate is clicked.
func TestOverlayPanelNeverActivatesOnShow(t *testing.T) {
	f := newFakePlatformWindow()
	w := newWindowOnFake(f)
	w.kind = WindowOverlayPanel

	w.ShowAt(0, 0)
	if f.lastActivate {
		t.Error("ShowAt asked the platform to activate an overlay panel")
	}

	f.lastActivate = false
	w.Show()
	if f.lastActivate {
		t.Error("Show asked the platform to activate an overlay panel")
	}
}

// A normal window is expected to come forward and take focus when shown,
// or Show would look like it did nothing.
func TestNormalWindowActivatesOnShow(t *testing.T) {
	f := newFakePlatformWindow()
	w := newWindowOnFake(f)
	w.kind = WindowNormal

	w.Show()
	if !f.lastActivate {
		t.Error("Show on a normal window did not request activation")
	}
}

func TestHideKeepsThePlatformWindowAlive(t *testing.T) {
	f := newFakePlatformWindow()
	w := newWindowOnFake(f)
	w.kind = WindowOverlayPanel

	w.ShowAt(10, 10)
	if !w.IsVisible() {
		t.Fatal("not visible after ShowAt")
	}

	w.Hide()
	if w.IsVisible() {
		t.Error("still visible after Hide")
	}
	// Hide rather than destroy is the whole point of the API: a candidate
	// panel is shown and hidden on nearly every keystroke, and rebuilding
	// an OS window plus a GPU context at that rate is far too slow.
	if f.destroyed {
		t.Error("Hide destroyed the platform window")
	}
}

// SetRenderer is where a panel's renderer learns it must preserve alpha.
// Forgetting produces an opaque black rectangle, which reads as a rendering
// bug rather than a missing call — so it is derived from the window's kind
// instead of being left to the caller.
func TestSetRendererPropagatesTransparency(t *testing.T) {
	panel := &Window{kind: WindowOverlayPanel, zoom: 1}
	normal := &Window{kind: WindowNormal, zoom: 1}

	pr, nr := &GLRenderer{}, &GLRenderer{}
	panel.SetRenderer(pr)
	normal.SetRenderer(nr)

	if !pr.transparent {
		t.Error("overlay panel's GLRenderer was not put in transparent mode")
	}
	if nr.transparent {
		t.Error("normal window's GLRenderer was put in transparent mode")
	}
}

// The transparent composite path depends on which alpha convention the CPU
// rasterizer leaves in the framebuffer, and getting it backwards is
// invisible on an opaque window — the clear makes every pixel alpha 1, where
// premultiplied and straight coincide. It only shows up once a panel keeps
// its alpha, by which point the wrong blend reads as "the card is a bit dark"
// rather than as a bug. Pin the convention down here.
func TestRasterizerLeavesPremultipliedColorWhereAlphaIsPartial(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	c := NewImageCanvas(img)
	c.Clear(ColorTransparent)
	// Pure red at 50%: premultiplied -> R=128, straight -> R=255.
	c.FillRoundedRect(Rect{X: 5, Y: 5, W: 30, H: 30}, 4, Color{R: 1, A: 0.5})

	got := img.RGBAAt(20, 20)
	if got.A != 128 {
		t.Fatalf("alpha = %d, want 128", got.A)
	}
	if got.R != 128 {
		t.Errorf("red = %d, want 128 (premultiplied). If this is now 255 the "+
			"rasterizer switched to straight alpha and GLRenderer's transparent "+
			"blend must change from (ONE, ONE_MINUS_SRC_ALPHA) to "+
			"(SRC_ALPHA, ONE_MINUS_SRC_ALPHA)", got.R)
	}

	// Outside the card must stay fully transparent, or the panel would show
	// a rectangle instead of a rounded one.
	if corner := img.RGBAAt(1, 1); corner.A != 0 {
		t.Errorf("outside the card alpha = %d, want 0", corner.A)
	}
}

// An overlay panel is typically created after the real window and lives for
// the whole process, so letting it win the process-wide clipboard provider
// would hand the app's copy/paste to a window that has no text input.
func TestOverlayPanelDoesNotClaimClipboard(t *testing.T) {
	t.Cleanup(func() { SetClipboardProvider(nil) })

	sentinel := &panelClipboardSentinel{}
	SetClipboardProvider(sentinel)

	app := fakePlatformApp{}
	if _, err := newWindowFromConfig(app, platformWindowConfig{
		Width: 100, Height: 40, Kind: WindowOverlayPanel,
	}); err != nil {
		t.Fatalf("newWindowFromConfig(overlay): %v", err)
	}
	if currentClipboard() != Clipboard(sentinel) {
		t.Error("creating an overlay panel replaced the process clipboard provider")
	}

	if _, err := newWindowFromConfig(app, platformWindowConfig{
		Width: 100, Height: 40, Kind: WindowNormal,
	}); err != nil {
		t.Fatalf("newWindowFromConfig(normal): %v", err)
	}
	if currentClipboard() == Clipboard(sentinel) {
		t.Error("creating a normal window did NOT install its own clipboard provider")
	}
}

func currentClipboard() Clipboard {
	clipboardMu.RLock()
	defer clipboardMu.RUnlock()
	return activeClipboard
}

// panelClipboardSentinel is a Clipboard we can check identity against.
type panelClipboardSentinel struct{ text string }

func (c *panelClipboardSentinel) Get() string     { return c.text }
func (c *panelClipboardSentinel) Set(text string) { c.text = text }

// fakePlatformApp hands out fake windows so window construction can be
// exercised without an OS window.
type fakePlatformApp struct{}

func (fakePlatformApp) newWindow(platformWindowConfig) (platformWindow, error) {
	return newFakePlatformWindow(), nil
}
func (fakePlatformApp) pumpEvents(time.Duration)        {}
func (fakePlatformApp) wake()                           {}
func (fakePlatformApp) monitors() []platformMonitor     { return nil }
func (fakePlatformApp) primaryMonitor() platformMonitor { return nil }
