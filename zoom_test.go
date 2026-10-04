package qui

import "testing"

func TestZoomDefaultsTo100Percent(t *testing.T) {
	w := NewTestWindow(Size{W: 800, H: 600})
	if got := w.Zoom(); got != 1 {
		t.Errorf("Zoom() = %v, want 1", got)
	}
	// The zero value must behave as unzoomed too — plenty of code
	// constructs a bare Window.
	if got := (&Window{}).Zoom(); got != 1 {
		t.Errorf("zero-value Window Zoom() = %v, want 1", got)
	}
}

// The defining property of page zoom versus a magnifier: the viewport
// SHRINKS, so layout reflows into less space.
func TestZoomShrinksTheViewportAndLeavesTheWindowAlone(t *testing.T) {
	w := NewTestWindow(Size{W: 1000, H: 800})
	w.SetZoom(2)

	if got := w.ViewportSize(); got.W != 500 || got.H != 400 {
		t.Errorf("ViewportSize() = %v, want {500 400}", got)
	}
	if got := w.Size(); got.W != 500 {
		t.Errorf("Size() must be the viewport, got %v", got)
	}
	if got := w.WindowSize(); got.W != 1000 || got.H != 800 {
		t.Errorf("WindowSize() = %v, want {1000 800} — zoom must not move the OS window", got)
	}
	if got := w.Bounds(); got.W != 500 || got.H != 400 {
		t.Errorf("Bounds() = %v, want the viewport rect", got)
	}

	w.SetZoom(0.5)
	if got := w.ViewportSize(); got.W != 2000 || got.H != 1600 {
		t.Errorf("zooming out should grow the viewport, got %v", got)
	}
}

func TestZoomIsClampedAndQuantized(t *testing.T) {
	w := NewTestWindow(Size{W: 800, H: 600})

	w.SetZoom(100)
	if got := w.Zoom(); got != zoomMax {
		t.Errorf("Zoom() = %v after SetZoom(100), want the ceiling %v", got, zoomMax)
	}
	w.SetZoom(0.001)
	if got := w.Zoom(); got != zoomMin {
		t.Errorf("Zoom() = %v after SetZoom(0.001), want the floor %v", got, zoomMin)
	}

	// Quantization is what keeps a continuous pinch from thrashing the
	// text-measurement cache, so it has to actually snap.
	w.SetZoom(1.3337)
	got := w.Zoom()
	steps := got / zoomQuantum
	if d := steps - float32(int(steps+0.5)); d > 1e-4 || d < -1e-4 {
		t.Errorf("Zoom() = %v is not a multiple of %v", got, zoomQuantum)
	}
	if got < 1.32 || got > 1.35 {
		t.Errorf("Zoom() = %v, want ~1.325 (nearest 2.5%% step to 1.3337)", got)
	}

	// A negative or zero request means "no zoom", not a crash.
	w.SetZoom(0)
	if got := w.Zoom(); got != 1 {
		t.Errorf("SetZoom(0) gave %v, want 1", got)
	}
}

// A pinch drives SetZoom every frame; without quantization each frame
// would be a distinct font size and every text layout would miss the
// cache. Assert the number of distinct values a full-range sweep can
// produce stays small.
func TestQuantizationBoundsTheNumberOfDistinctZoomLevels(t *testing.T) {
	seen := map[float32]bool{}
	for i := 0; i <= 10000; i++ {
		z := zoomMin + (zoomMax-zoomMin)*float32(i)/10000
		seen[quantizeZoom(z)] = true
	}
	// (5 - 0.25) / 0.025 + 1 == 191 reachable stops.
	if len(seen) > 200 {
		t.Errorf("a full zoom sweep produced %d distinct levels; quantization is not working", len(seen))
	}
	if len(seen) < 50 {
		t.Errorf("only %d distinct levels — quantization is too coarse to feel smooth", len(seen))
	}
}

// The invariant that keeps the ladder from dead-ending: if a stop did not
// survive quantization, SetZoom would snap back to the value we were
// stepping away from and ZoomIn/ZoomOut would silently stop working.
func TestZoomLadderSurvivesQuantization(t *testing.T) {
	for _, stop := range zoomLadder {
		if got := quantizeZoom(stop); got != stop {
			t.Errorf("ladder stop %v quantizes to %v; every stop must be a valid step "+
				"or stepping onto it becomes a no-op", stop, got)
		}
	}
}

func TestQuantizeZoomIsIdempotent(t *testing.T) {
	for i := 0; i <= 400; i++ {
		z := zoomMin + (zoomMax-zoomMin)*float32(i)/400
		once := quantizeZoom(z)
		if twice := quantizeZoom(once); twice != once {
			t.Fatalf("quantizeZoom(%v) = %v but re-quantizes to %v", z, once, twice)
		}
	}
}

func TestZoomInOutWalksTheLadderAndRoundTrips(t *testing.T) {
	w := NewTestWindow(Size{W: 800, H: 600})

	w.ZoomIn()
	if got := w.Zoom(); got != 1.1 {
		t.Errorf("ZoomIn from 100%% gave %v, want 1.1", got)
	}
	w.ZoomIn()
	if got := w.Zoom(); got != 1.25 {
		t.Errorf("second ZoomIn gave %v, want 1.25", got)
	}
	// In then out must land exactly back — a multiplier-based
	// implementation would drift.
	w.ZoomOut()
	w.ZoomOut()
	if got := w.Zoom(); got != 1 {
		t.Errorf("ZoomIn×2 then ZoomOut×2 gave %v, want exactly 1", got)
	}

	// Stepping from an off-ladder value (where a pinch left it) must move,
	// not snap back to the nearest stop and stall.
	w.SetZoom(1.325)
	w.ZoomIn()
	if got := w.Zoom(); got <= 1.325 {
		t.Errorf("ZoomIn from an off-ladder 1.325 gave %v; it must move up", got)
	}
	w.SetZoom(1.325)
	w.ZoomOut()
	if got := w.Zoom(); got >= 1.325 {
		t.Errorf("ZoomOut from an off-ladder 1.325 gave %v; it must move down", got)
	}
}

func TestZoomLadderStopsAtTheEnds(t *testing.T) {
	w := NewTestWindow(Size{W: 800, H: 600})
	for i := 0; i < 30; i++ {
		w.ZoomIn()
	}
	if got := w.Zoom(); got != zoomMax {
		t.Errorf("repeated ZoomIn ended at %v, want %v", got, zoomMax)
	}
	for i := 0; i < 40; i++ {
		w.ZoomOut()
	}
	if got := w.Zoom(); got != zoomMin {
		t.Errorf("repeated ZoomOut ended at %v, want %v", got, zoomMin)
	}
}

func TestSmartZoomTogglesBackToTheLastLevel(t *testing.T) {
	w := NewTestWindow(Size{W: 800, H: 600})
	w.SetZoom(1.5)

	w.SmartZoom() // zoomed → back to 100%
	if got := w.Zoom(); got != 1 {
		t.Errorf("SmartZoom from 150%% gave %v, want 1", got)
	}
	w.SmartZoom() // 100% → restore 150%
	if got := w.Zoom(); got != 1.5 {
		t.Errorf("SmartZoom should restore the previous 1.5, got %v", got)
	}
}

func TestSmartZoomAtDefaultDoesSomethingVisible(t *testing.T) {
	// With no history, a double-tap that did nothing would read as a dead
	// gesture. It should step in instead.
	w := NewTestWindow(Size{W: 800, H: 600})
	w.SmartZoom()
	if got := w.Zoom(); got <= 1 {
		t.Errorf("SmartZoom with no history gave %v; it must zoom in", got)
	}
}

func TestEffectiveScaleCombinesDPRAndZoomButDPRStaysClean(t *testing.T) {
	w := NewTestWindow(Size{W: 800, H: 600})
	// A test window has no platform backend, so DPR is 1 — enough to check
	// the composition and that zoom does not leak into DevicePixelRatio.
	w.SetZoom(2)
	if got := w.DevicePixelRatio(); got != 1 {
		t.Errorf("DevicePixelRatio() = %v; it must stay the display's true ratio", got)
	}
	if got := w.EffectiveScale(); got != 2 {
		t.Errorf("EffectiveScale() = %v, want dpr×zoom = 2", got)
	}
}

func TestZoomInvalidatesLayoutAndPaint(t *testing.T) {
	w := NewTestWindow(Size{W: 800, H: 600})
	root := &zoomProbe{}
	root.SetSelf(root)
	w.SetRoot(root)
	root.ClearLayoutDirty()
	w.dirtyRegion = Rect{}

	w.SetZoom(1.5)
	if w.dirtyRegion.IsEmpty() {
		t.Error("SetZoom must dirty the paint region — every glyph re-rasterizes")
	}
	if !root.IsLayoutDirty() {
		t.Error("SetZoom must invalidate layout — page zoom reflows")
	}
}

func TestRedundantSetZoomIsANoOp(t *testing.T) {
	w := NewTestWindow(Size{W: 800, H: 600})
	root := &zoomProbe{}
	root.SetSelf(root)
	w.SetRoot(root)
	w.SetZoom(1.5)
	root.ClearLayoutDirty()
	w.dirtyRegion = Rect{}

	w.SetZoom(1.5)
	if !w.dirtyRegion.IsEmpty() || root.IsLayoutDirty() {
		t.Error("setting the same zoom again must not schedule work")
	}
}

// Input must arrive in viewport coordinates: a click at window point
// (400,300) under 2× zoom is content point (200,150). Getting this wrong
// makes every click land in the wrong place as soon as a user zooms.
func TestInputCoordinatesAreConvertedToViewportSpace(t *testing.T) {
	w := NewTestWindow(Size{W: 1000, H: 800})
	probe := &zoomProbe{}
	probe.SetSelf(probe)
	probe.rect = Rect{W: 1000, H: 800}
	w.SetRoot(probe)

	w.SetZoom(2)
	w.onMouseMove(400, 300, 0)
	if probe.lastX != 200 || probe.lastY != 150 {
		t.Errorf("mouse at window (400,300) under 2× arrived at (%v,%v), want (200,150)",
			probe.lastX, probe.lastY)
	}

	w.SetZoom(1)
	w.onMouseMove(400, 300, 0)
	if probe.lastX != 400 || probe.lastY != 300 {
		t.Errorf("at 100%% the conversion must be the identity, got (%v,%v)", probe.lastX, probe.lastY)
	}
}

func TestScrollDeltasScaleWithZoomSoVisualSpeedIsConstant(t *testing.T) {
	w := NewTestWindow(Size{W: 1000, H: 800})
	probe := &zoomProbe{}
	probe.SetSelf(probe)
	probe.rect = Rect{W: 1000, H: 800}
	w.SetRoot(probe)

	w.SetZoom(2)
	w.onScroll(400, 300, 0, 10, 0, GesturePhaseChanged)
	if probe.lastDeltaY != 5 {
		t.Errorf("scroll delta 10 under 2× arrived as %v, want 5 — "+
			"a notch should cover the same visible distance at any zoom", probe.lastDeltaY)
	}
}

// Window zoom is opt-in: enabling it for every existing app would hand
// apps that already interpret pinch a second, competing zoom.
func TestWindowZoomIsOptIn(t *testing.T) {
	w := NewTestWindow(Size{W: 800, H: 600})
	if w.ViewportZoomEnabled() {
		t.Fatal("viewport zoom must be off by default")
	}
	w.ingestGesture(EventGesturePinch, GesturePhaseChanged, 0.5, 0, 0)
	if got := w.Zoom(); got != 1 {
		t.Errorf("an unclaimed pinch zoomed to %v with the feature disabled", got)
	}

	w.SetViewportZoomEnabled(true)
	w.ingestGesture(EventGesturePinch, GesturePhaseChanged, 0.5, 0, 0)
	if got := w.Zoom(); got <= 1 {
		t.Errorf("an unclaimed pinch left zoom at %v; it should have zoomed in", got)
	}
}

// A widget that handles pinch owns it — the window must not also zoom, or
// the two fight and the content scales twice.
func TestWidgetConsumingPinchPreventsWindowZoom(t *testing.T) {
	w := NewTestWindow(Size{W: 800, H: 600})
	w.SetViewportZoomEnabled(true)
	probe := &zoomProbe{consumeGestures: true}
	probe.SetSelf(probe)
	probe.rect = Rect{W: 800, H: 600}
	w.SetRoot(probe)

	w.ingestGesture(EventGesturePinch, GesturePhaseChanged, 0.5, 0, 0)
	if probe.gestures == 0 {
		t.Fatal("the widget never saw the pinch")
	}
	if got := w.Zoom(); got != 1 {
		t.Errorf("window zoomed to %v even though a widget consumed the pinch", got)
	}
}

func TestZoomShortcutsRequireTheFeatureAndTheCommandModifier(t *testing.T) {
	w := NewTestWindow(Size{W: 800, H: 600})
	w.SetViewportZoomEnabled(true)

	cmd := commandModifier()
	if !w.handleZoomShortcut(KeyEvent{Key: KeyEqual, Mods: cmd}) {
		t.Fatal("Cmd/Ctrl+= should zoom in")
	}
	if got := w.Zoom(); got != 1.1 {
		t.Errorf("after zoom-in shortcut Zoom() = %v, want 1.1", got)
	}
	if !w.handleZoomShortcut(KeyEvent{Key: KeyMinus, Mods: cmd}) {
		t.Fatal("Cmd/Ctrl+- should zoom out")
	}
	w.SetZoom(2)
	if !w.handleZoomShortcut(KeyEvent{Key: Key0, Mods: cmd}) {
		t.Fatal("Cmd/Ctrl+0 should reset")
	}
	if got := w.Zoom(); got != 1 {
		t.Errorf("reset shortcut left Zoom() = %v", got)
	}

	// Bare keys must pass through — '=' and '-' are ordinary typing.
	if w.handleZoomShortcut(KeyEvent{Key: KeyEqual}) {
		t.Error("unmodified '=' must not zoom")
	}
	// And nothing fires when the feature is off.
	w.SetViewportZoomEnabled(false)
	if w.handleZoomShortcut(KeyEvent{Key: KeyEqual, Mods: cmd}) {
		t.Error("shortcut fired with viewport zoom disabled")
	}
}

// commandModifier returns whatever IsCommandMod accepts on this platform,
// so the shortcut test is not darwin-specific.
func commandModifier() Modifiers {
	if IsCommandMod(ModSuper) {
		return ModSuper
	}
	return ModControl
}

// zoomProbe records the coordinates events arrive with.
type zoomProbe struct {
	BaseWidget
	rect            Rect
	lastX, lastY    float32
	lastDeltaY      float32
	gestures        int
	consumeGestures bool
}

func (p *zoomProbe) Bounds() Rect { return p.rect }

func (p *zoomProbe) HitTest(pt Point) Widget {
	if p.rect.Contains(pt) {
		return p
	}
	return nil
}

func (p *zoomProbe) Handle(e Event) bool {
	switch ev := e.(type) {
	case MouseEvent:
		p.lastX, p.lastY = ev.X, ev.Y
		p.lastDeltaY = ev.DeltaY
	case GestureEvent:
		p.gestures++
		p.lastX, p.lastY = ev.X, ev.Y
		return p.consumeGestures
	}
	return false
}
