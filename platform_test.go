package qui

import (
	"errors"
	"image"
	"runtime"
	"testing"
	"unsafe"
)

// fakePlatformWindow is a second implementation of the platform seam,
// existing purely so the interface is exercised by something that is not
// GLFW and not macOS.
//
// This is the cheap version of the argument for a platform seam: an
// interface with one implementation grows that implementation's
// assumptions silently. Everything below
// records calls instead of touching an OS, which means a method that could
// only be satisfied by Cocoa would be obviously unimplementable here.
type fakePlatformWindow struct {
	handler platformHandler
	capsVal platformCaps

	// visible tracks setVisible so tests can assert show/hide without a
	// real OS window. lastActivate records whether the show asked for
	// focus — the distinction an overlay panel depends on.
	visible      bool
	lastActivate bool

	surf         *fakeSurface
	wakes        int
	w, h         int
	fbW, fbH     int
	scaleX       float32
	scaleY       float32
	posX, posY   int
	posOK        bool
	title        string
	clipboard    string
	cursor       CursorShape
	mods         Modifiers
	curX, curY   float64
	closing      bool
	swaps        int
	currents     int
	destroyed    bool
	iconified    bool
	maximized    bool
	restored     bool
	focused      bool
	setPosCalls  int
	sizeLimits   [4]int
	fullscreenTo platformMonitor
}

func newFakePlatformWindow() *fakePlatformWindow {
	return &fakePlatformWindow{
		capsVal: platformCaps{AbsolutePosition: true, ServerDecorations: true},
		w:       800, h: 600,
		fbW: 1600, fbH: 1200, // a 2× display
		scaleX: 2, scaleY: 2,
		posOK: true,
	}
}

func (f *fakePlatformWindow) setHandler(h platformHandler)     { f.handler = h }
func (f *fakePlatformWindow) caps() platformCaps               { return f.capsVal }
func (f *fakePlatformWindow) size() (int, int)                 { return f.w, f.h }
func (f *fakePlatformWindow) framebufferSize() (int, int)      { return f.fbW, f.fbH }
func (f *fakePlatformWindow) contentScale() (float32, float32) { return f.scaleX, f.scaleY }

func (f *fakePlatformWindow) setSize(w, h int) { f.w, f.h = w, h }

func (f *fakePlatformWindow) setVisible(visible, activate bool) {
	f.visible, f.lastActivate = visible, activate
}
func (f *fakePlatformWindow) isVisible() bool { return f.visible }

func (f *fakePlatformWindow) pos() (int, int, bool) { return f.posX, f.posY, f.posOK }

func (f *fakePlatformWindow) setPos(x, y int) bool {
	f.setPosCalls++
	if !f.posOK {
		return false
	}
	f.posX, f.posY = x, y
	return true
}

func (f *fakePlatformWindow) setTitle(t string) { f.title = t }
func (f *fakePlatformWindow) setSizeLimits(a, b, c, d int) {
	f.sizeLimits = [4]int{a, b, c, d}
}
func (f *fakePlatformWindow) iconify()          { f.iconified = true }
func (f *fakePlatformWindow) isIconified() bool { return f.iconified }
func (f *fakePlatformWindow) maximize()         { f.maximized = true }
func (f *fakePlatformWindow) restore()          { f.restored = true }
func (f *fakePlatformWindow) focus()            { f.focused = true }

func (f *fakePlatformWindow) setFullscreen(m platformMonitor, _, _, _, _, _ int) {
	f.fullscreenTo = m
}

func (f *fakePlatformWindow) shouldClose() bool     { return f.closing }
func (f *fakePlatformWindow) setShouldClose(v bool) { f.closing = v }
func (f *fakePlatformWindow) destroy()              { f.destroyed = true }

func (f *fakePlatformWindow) cursorPos() (float64, float64) { return f.curX, f.curY }
func (f *fakePlatformWindow) setCursor(s CursorShape)       { f.cursor = s }
func (f *fakePlatformWindow) currentMods() Modifiers        { return f.mods }

func (f *fakePlatformWindow) clipboardText() string        { return f.clipboard }
func (f *fakePlatformWindow) setClipboardText(t string)    { f.clipboard = t }
func (f *fakePlatformWindow) makeCurrent()                 { f.currents++ }
func (f *fakePlatformWindow) wake()                        { f.wakes++ }
func (f *fakePlatformWindow) nativeWindow() unsafe.Pointer { return nil }

func (f *fakePlatformWindow) surface() platformSurface {
	if f.surf == nil {
		f.surf = &fakeSurface{owner: f}
	}
	return f.surf
}

// fakeSurface models both presentation paths so the engine's choice
// between them is testable without a compositor.
type fakeSurface struct {
	owner *fakePlatformWindow
	// cpuOK makes presentCPU succeed, standing in for a compositor that
	// can take an image.RGBA directly (CALayer, wl_shm).
	cpuOK bool
	// readyCh, when non-nil, models a display-driven frame signal
	// (CADisplayLink, wl_surface.frame).
	readyCh   chan struct{}
	cpuFrames int
	presented int
	lastImage *image.RGBA
}

func (s *fakeSurface) ready() <-chan struct{} {
	if s.readyCh == nil {
		return nil
	}
	return s.readyCh
}

func (s *fakeSurface) presentCPU(img *image.RGBA) bool {
	if !s.cpuOK {
		return false
	}
	s.cpuFrames++
	s.lastImage = img
	return true
}

func (s *fakeSurface) present() { s.presented++ }

// cpuFrameRenderer is a Renderer that hands out a CPU image, standing in
// for the reference rasterizer without needing a GL context.
type cpuFrameRenderer struct {
	img *image.RGBA
}

func (r *cpuFrameRenderer) Begin(Size) Canvas       { return NoopRenderer{}.Begin(Size{}) }
func (r *cpuFrameRenderer) End()                    {}
func (r *cpuFrameRenderer) FrameImage() *image.RGBA { return r.img }

// newWindowOnFake builds an engine Window on a fake backend. It bypasses
// NewWindow (which would create a real OS window) but wires the same
// handler relationship, so anything reached through w.plat is under test.
func newWindowOnFake(f *fakePlatformWindow) *Window {
	w := &Window{plat: f, platformBacked: true, renderer: NoopRenderer{}, lastSize: Size{W: 800, H: 600}}
	f.setHandler(w)
	return w
}

func TestWindowGeometryRoutesThroughTheSeam(t *testing.T) {
	f := newFakePlatformWindow()
	w := newWindowOnFake(f)

	if got := w.DevicePixelRatio(); got != 2 {
		t.Errorf("DevicePixelRatio = %v, want 2 (framebuffer 1600 / logical 800)", got)
	}
	if got := w.ContentScale(); got.X != 2 || got.Y != 2 {
		t.Errorf("ContentScale = %+v, want {2,2}", got)
	}
	w.SetTitle("hello")
	if f.title != "hello" {
		t.Errorf("SetTitle didn't reach the backend, got %q", f.title)
	}
	w.SetPosition(30, 40)
	if p := w.Position(); p.X != 30 || p.Y != 40 {
		t.Errorf("Position = %+v, want {30,40}", p)
	}
	w.Maximize()
	w.Iconify()
	w.Restore()
	w.Raise()
	if !f.maximized || !f.iconified || !f.restored || !f.focused {
		t.Errorf("window ops didn't all reach the backend: %+v", []bool{
			f.maximized, f.iconified, f.restored, f.focused})
	}
}

// TestFractionalContentScaleSurvives guards a constraint that only bites
// off macOS: Windows allows arbitrary scale percentages and Wayland has
// fractional scaling, so the scale must not be rounded to 1/2/3 anywhere
// on the way up.
func TestFractionalContentScaleSurvives(t *testing.T) {
	f := newFakePlatformWindow()
	f.scaleX, f.scaleY = 1.5, 1.5
	f.w, f.h = 800, 600
	f.fbW, f.fbH = 1200, 900
	w := newWindowOnFake(f)

	if got := w.ContentScale(); got.X != 1.5 {
		t.Errorf("ContentScale.X = %v, want 1.5 — fractional scaling must not be rounded", got.X)
	}
	if got := w.DevicePixelRatio(); got != 1.5 {
		t.Errorf("DevicePixelRatio = %v, want 1.5", got)
	}
}

// TestWindowWithoutAbsolutePositionDegrades is the Wayland shape: a
// platform that cannot report or set a global position. The engine must
// say so rather than reporting a plausible-looking {0,0}.
func TestWindowWithoutAbsolutePositionDegrades(t *testing.T) {
	f := newFakePlatformWindow()
	f.posOK = false
	f.capsVal.AbsolutePosition = false
	w := newWindowOnFake(f)

	if w.SupportsAbsolutePosition() {
		t.Error("SupportsAbsolutePosition should be false when the platform hides it")
	}
	if _, ok := w.PositionOK(); ok {
		t.Error("PositionOK should report ok=false, not a fabricated coordinate")
	}
	if p := w.Position(); p.X != 0 || p.Y != 0 {
		t.Errorf("Position should degrade to the zero Point, got %+v", p)
	}
	// Centering is not expressible without global coordinates; it must be
	// a harmless no-op rather than moving the window somewhere wrong.
	w.CenterOn(Monitor{plat: nil, WorkArea: Rect{W: 1000, H: 1000}})
	if f.posX != 0 || f.posY != 0 {
		t.Errorf("window moved despite the platform refusing: (%d,%d)", f.posX, f.posY)
	}
}

func TestHandlerTranslatesInputToEvents(t *testing.T) {
	f := newFakePlatformWindow()
	w := newWindowOnFake(f)
	spy := newGestureSpy("root", Rect{W: 800, H: 600})
	var keys []KeyEvent
	var chars []rune
	probe := newEventProbe(&keys, &chars)
	// Focused widget for key/char routing; the spy stays the hit-test
	// target so pointer and gesture events land on it.
	root := NewContainer(&AbsoluteLayout{}, spy, probe)
	root.SetSelf(root)
	root.rect = Rect{W: 800, H: 600}
	w.SetRoot(root)
	w.SetFocus(probe)
	// Gestures anchor at the cursor (trackpads have no on-screen
	// position), so put the cursor over the spy.
	f.curX, f.curY = 100, 100

	// Keyboard: a backend reports down/up, the engine turns that into
	// KeyDown/KeyUp with modifiers attached.
	w.onKey(KeyA, 7, true, false, ModShift)
	w.onKey(KeyA, 7, false, false, ModShift)
	if len(keys) != 2 {
		t.Fatalf("expected KeyDown+KeyUp, got %d events", len(keys))
	}
	if keys[0].eventType != EventKeyDown || keys[1].eventType != EventKeyUp {
		t.Errorf("key event types = %v/%v", keys[0].eventType, keys[1].eventType)
	}
	if keys[0].Mods&ModShift == 0 || keys[0].ScanCode != 7 {
		t.Errorf("key event lost mods/scancode: %+v", keys[0])
	}

	w.onChar('q')
	if len(chars) != 1 || chars[0] != 'q' {
		t.Errorf("onChar didn't deliver a CharEvent: %v", chars)
	}

	// Motion has no modifiers of its own on any backend, so the engine
	// remembers what it was last told.
	w.onMouseMove(10, 20, ModAlt)
	if w.lastMods&ModAlt == 0 {
		t.Error("onMouseMove should record the modifier state it was given")
	}

	// Scroll carries phase through untouched.
	w.onScroll(5, 5, 0, -2, ModShift, GesturePhaseMomentum)
	if len(spy.scrolls) != 1 {
		t.Fatalf("expected 1 scroll event, got %d", len(spy.scrolls))
	}
	if spy.scrolls[0].ScrollPhase != GesturePhaseMomentum {
		t.Errorf("ScrollPhase = %v, want Momentum", spy.scrolls[0].ScrollPhase)
	}

	// Gestures go through the accumulator, not straight to the tree.
	w.onGesture(EventGesturePinch, GesturePhaseBegan, 0, 0, 0)
	w.onGesture(EventGesturePinch, GesturePhaseChanged, 0.5, 0, 0)
	if len(spy.got) != 2 {
		t.Fatalf("expected 2 gesture events, got %d", len(spy.got))
	}
	if spy.got[1].Scale != 1.5 {
		t.Errorf("cumulative Scale = %v, want 1.5", spy.got[1].Scale)
	}
}

func TestHandlerFileDropInvokesCallbackAndRepaints(t *testing.T) {
	f := newFakePlatformWindow()
	w := newWindowOnFake(f)
	w.SetRoot(newGestureSpy("root", Rect{W: 800, H: 600}))

	var gotPaths []string
	var gotX, gotY float32
	w.SetOnFileDrop(func(paths []string, x, y float32) {
		gotPaths, gotX, gotY = paths, x, y
	})
	w.dirtyRegion = Rect{}

	w.onFileDrop([]string{"/tmp/a.png", "/tmp/b.png"}, 12, 34)

	if len(gotPaths) != 2 || gotPaths[0] != "/tmp/a.png" {
		t.Errorf("paths = %v", gotPaths)
	}
	if gotX != 12 || gotY != 34 {
		t.Errorf("drop point = (%v,%v), want (12,34)", gotX, gotY)
	}
	// A drop usually changes a lot of state, so the engine forces a full
	// repaint rather than trusting the callback to invalidate.
	if w.dirtyRegion.IsEmpty() {
		t.Error("file drop should have dirtied the window")
	}
}

func TestClipboardRoutesThroughTheSeam(t *testing.T) {
	f := newFakePlatformWindow()
	c := platformClipboard{win: f}

	c.Set("copied")
	if f.clipboard != "copied" {
		t.Errorf("clipboard Set didn't reach the backend, got %q", f.clipboard)
	}
	if got := c.Get(); got != "copied" {
		t.Errorf("clipboard Get = %q, want %q", got, "copied")
	}

	// A nil backend must be inert, not panic: SetClipboardProvider is
	// swapped for a fake in tests and the provider can outlive a window.
	empty := platformClipboard{}
	empty.Set("x")
	if got := empty.Get(); got != "" {
		t.Errorf("clipboard with no window should read empty, got %q", got)
	}
}

// eventProbe records key and char events routed to the focused widget.
type eventProbe struct {
	BaseWidget
	keys  *[]KeyEvent
	chars *[]rune
}

func newEventProbe(keys *[]KeyEvent, chars *[]rune) *eventProbe {
	p := &eventProbe{BaseWidget: NewBaseWidget(), keys: keys, chars: chars}
	p.SetSelf(p)
	return p
}

func (p *eventProbe) Handle(e Event) bool {
	if e.Phase() != PhaseTarget {
		return false
	}
	switch ev := e.(type) {
	case KeyEvent:
		*p.keys = append(*p.keys, ev)
	case CharEvent:
		*p.chars = append(*p.chars, ev.Rune)
	}
	return false
}

func (p *eventProbe) Focusable() bool { return true }

func TestPresentPrefersTheCPUPathWhenBothSidesSupportIt(t *testing.T) {
	f := newFakePlatformWindow()
	w := newWindowOnFake(f)
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	w.renderer = &cpuFrameRenderer{img: img}
	surf := f.surface().(*fakeSurface)
	surf.cpuOK = true

	w.present()

	if surf.cpuFrames != 1 {
		t.Errorf("expected the frame to go through presentCPU, got %d CPU frames", surf.cpuFrames)
	}
	if surf.lastImage != img {
		t.Error("presentCPU received a different image than the renderer produced")
	}
	// The whole point is skipping the GPU round-trip; a buffer swap here
	// would mean we paid for both paths.
	if surf.presented != 0 {
		t.Errorf("GPU present should have been skipped, got %d", surf.presented)
	}
}

func TestPresentFallsBackToGPUWhenSurfaceDeclinesCPU(t *testing.T) {
	f := newFakePlatformWindow()
	w := newWindowOnFake(f)
	w.renderer = &cpuFrameRenderer{img: image.NewRGBA(image.Rect(0, 0, 4, 4))}
	surf := f.surface().(*fakeSurface)
	surf.cpuOK = false // e.g. GLFW: presenting CPU pixels would mean a texture upload

	w.present()

	if surf.cpuFrames != 0 {
		t.Errorf("surface declined the CPU path but got %d CPU frames", surf.cpuFrames)
	}
	if surf.presented != 1 {
		t.Errorf("expected one GPU present, got %d", surf.presented)
	}
}

func TestPresentFallsBackWhenRendererHasNoCPUFrame(t *testing.T) {
	f := newFakePlatformWindow()
	w := newWindowOnFake(f)
	// NoopRenderer doesn't implement CPUFrameSource at all — the same
	// situation as a GPU-native raster backend whose pixels live in an FBO.
	w.renderer = NoopRenderer{}
	surf := f.surface().(*fakeSurface)
	surf.cpuOK = true

	w.present()

	if surf.cpuFrames != 0 {
		t.Errorf("a renderer with no CPU frame must not use the CPU path (%d frames)", surf.cpuFrames)
	}
	if surf.presented != 1 {
		t.Errorf("expected one GPU present, got %d", surf.presented)
	}
}

func TestFrameDuePacesOnTheDisplaySignal(t *testing.T) {
	f := newFakePlatformWindow()
	w := newWindowOnFake(f)
	surf := f.surface().(*fakeSurface)
	surf.readyCh = make(chan struct{}, 1)

	// No signal yet: painting would be discarded, so skip it.
	if frameDue(w) {
		t.Error("frameDue should be false before the display asks for a frame")
	}
	surf.readyCh <- struct{}{}
	if !frameDue(w) {
		t.Error("frameDue should be true once the display signalled")
	}
	// The signal is consumed, so the next iteration waits again.
	if frameDue(w) {
		t.Error("the frame signal should be consumed, not sticky")
	}
}

func TestFrameDueStillRunsWhenJobsArePending(t *testing.T) {
	f := newFakePlatformWindow()
	w := newWindowOnFake(f)
	surf := f.surface().(*fakeSurface)
	surf.readyCh = make(chan struct{}, 1) // a display link that never fires

	if frameDue(w) {
		t.Fatal("precondition: no frame signal means no frame")
	}
	// An occluded window stops getting display callbacks. Queued work must
	// not be stranded behind a frame that never arrives — an agent action
	// blocking forever is the failure this guards.
	w.PostJob(func() {})
	if !frameDue(w) {
		t.Error("a pending job must force a step even without a frame signal")
	}
}

func TestFrameDueWithoutASignalUsesTimeoutPacing(t *testing.T) {
	f := newFakePlatformWindow()
	w := newWindowOnFake(f)
	// readyCh stays nil: the GLFW shape, where vsync lives inside the swap
	// and there is nothing to wait on.
	if !frameDue(w) {
		t.Error("a backend with no frame signal must always be due")
	}
	if !frameDue(NewTestWindow(Size{W: 1, H: 1})) {
		t.Error("a window with no platform backend must always be due")
	}
}

// TestPostJobWakesThroughTheWindowsOwnBackend covers the bug this seam made
// possible: waking used to go through a process-global platform, which for a
// window on a different backend both woke the wrong loop and — worse —
// lazily initialized a windowing library from whatever goroutine called
// PostJob. Windowing libraries must be initialized on the main thread, so
// that crashed instead of failing.
func TestPostJobWakesThroughTheWindowsOwnBackend(t *testing.T) {
	f := newFakePlatformWindow()
	w := newWindowOnFake(f)

	w.PostJob(func() {})

	if f.wakes != 1 {
		t.Errorf("PostJob should wake this window's own backend, got %d wakes", f.wakes)
	}
}

func TestPostJobConcurrentWithDestroy(t *testing.T) {
	f := newFakePlatformWindow()
	w := newWindowOnFake(f)
	started := make(chan struct{})
	done := make(chan struct{})

	go func() {
		close(started)
		for i := 0; i < 10000; i++ {
			w.PostJob(func() {})
		}
		close(done)
	}()

	<-started
	runtime.Gosched()
	w.Destroy()
	<-done

	if !f.destroyed {
		t.Fatal("platform window was not destroyed")
	}
}

func TestSynchronousActionDoesNotRunInlineAfterDestroy(t *testing.T) {
	f := newFakePlatformWindow()
	w := newWindowOnFake(f)
	w.Destroy()
	called := false

	err := w.synchronously(func() error {
		called = true
		return nil
	})

	if !errors.Is(err, ErrWindowClosed) {
		t.Fatalf("synchronously after Destroy error = %v, want ErrWindowClosed", err)
	}
	if called {
		t.Fatal("synchronous action ran inline after production window was destroyed")
	}
}
