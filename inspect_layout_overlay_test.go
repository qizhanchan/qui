package qui

import "testing"

func TestDebugOverlayDefaultDisabled(t *testing.T) {
	w := NewTestWindow(Size{W: 100, H: 80})
	if w.debugOverlayEnabled {
		t.Fatal("debug overlay should be disabled by default")
	}
}

func TestEnableDebugOverlayInvalidatesFullWindow(t *testing.T) {
	w := NewTestWindow(Size{W: 100, H: 80})
	w.ClearDirtyRegion()

	w.EnableDebugOverlay(DebugOverlayOptions{})

	if !w.debugOverlayEnabled {
		t.Fatal("debug overlay should be enabled")
	}
	if got := w.DirtyRegion(); got != (Rect{W: 100, H: 80}) {
		t.Fatalf("dirty region = %+v, want full window", got)
	}
	if !w.debugOverlayOptions.ShowBounds || !w.debugOverlayOptions.ShowOverflow || !w.debugOverlayOptions.ShowLabels {
		t.Fatalf("zero options should enable useful defaults: %+v", w.debugOverlayOptions)
	}
}

func TestDebugOverlayShortcutTogglesBeforeAccelerators(t *testing.T) {
	w := NewTestWindow(Size{W: 100, H: 80})
	root := NewContainer(nil)
	root.Layout(Rect{W: 100, H: 80})
	w.SetRoot(root)
	w.EnableDebugOverlay(DebugOverlayOptions{})

	key, mods, err := defaultDebugOverlayShortcut()
	if err != nil {
		t.Fatal(err)
	}
	fired := false
	accels := NewAcceleratorRegistry()
	shortcut := "Ctrl+Shift+D"
	if mods&ModSuper != 0 {
		shortcut = "Cmd+Shift+D"
	}
	if err := accels.Register(shortcut, func() { fired = true }); err != nil {
		t.Fatal(err)
	}
	w.SetAcceleratorRegistry(accels)

	w.DispatchTestEvent(NewKeyEvent(EventKeyDown, key, mods))

	if w.debugOverlayEnabled {
		t.Fatal("debug shortcut should toggle overlay off")
	}
	if fired {
		t.Fatal("debug shortcut should be consumed before accelerators")
	}
}

func TestDebugOverlayEmptyShortcutDisablesKeyboardToggle(t *testing.T) {
	empty := ""
	w := NewTestWindow(Size{W: 100, H: 80})
	w.SetRoot(NewContainer(nil))
	w.EnableDebugOverlay(DebugOverlayOptions{ToggleShortcut: &empty})

	key, mods, err := defaultDebugOverlayShortcut()
	if err != nil {
		t.Fatal(err)
	}
	w.DispatchTestEvent(NewKeyEvent(EventKeyDown, key, mods))

	if !w.debugOverlayEnabled {
		t.Fatal("empty ToggleShortcut should disable keyboard toggling")
	}
}

func TestDebugLayoutDiagnosticsWarnsOnFullSizeFlexGrowBasis(t *testing.T) {
	caption := newSized(100, 20)
	fill := NewContainer(AbsoluteLayout{}, newSized(10, 10))
	fill.SetFlex(1)
	root := NewContainer(FlexLayout{Direction: Vertical, Gap: 4}, caption, fill)
	root.Layout(Rect{W: 100, H: 100})
	w := NewTestWindow(Size{W: 100, H: 100})
	w.SetRoot(root)

	diags := w.DebugLayoutDiagnostics()
	if !hasDiagnostic(diags, "flex grow child has a full-size measured basis") {
		t.Fatalf("expected flex basis warning, got %+v", diags)
	}

	fill.UpdateFlexItem(func(item *FlexItem) { item.Shrink = 1 })
	root.Layout(Rect{W: 100, H: 100})
	diags = w.DebugLayoutDiagnostics()
	if hasDiagnostic(diags, "flex grow child has a full-size measured basis") {
		t.Fatalf("shrink=1 should suppress flex basis warning, got %+v", diags)
	}
}

func TestDebugLayoutDiagnosticsReportsOverflow(t *testing.T) {
	child := newSized(0, 0)
	child.Layout(Rect{X: 80, Y: 10, W: 40, H: 20})
	root := NewContainer(nil, child)
	root.Layout(Rect{W: 100, H: 100})
	w := NewTestWindow(Size{W: 100, H: 100})
	w.SetRoot(root)

	diags := w.DebugLayoutDiagnostics()
	if !hasDiagnostic(diags, "widget overflows parent bounds") {
		t.Fatalf("expected overflow diagnostic, got %+v", diags)
	}
}

// scrollingContainer stands in for widgets.ScrollView (root cannot import
// the widgets package): a parent whose content is deliberately taller than
// its own box, identified by the ScrollIntoViewable contract.
type scrollingContainer struct{ *Container }

func (scrollingContainer) ScrollChildIntoView(Widget) {}

// Content bigger than the viewport is what a scrolling container is FOR, so
// it is not an overflow. Reporting it made every scrolled panel and list
// show a layout error and buried the real ones.
func TestDebugLayoutDiagnosticsIgnoresScrollContent(t *testing.T) {
	content := newSized(0, 0)
	inner := NewContainer(nil, content)
	scroller := scrollingContainer{inner}
	scroller.SetSelf(scroller)
	inner.Layout(Rect{W: 100, H: 100})
	// Taller than the viewport, which is the whole point of scrolling.
	content.Layout(Rect{X: 0, Y: 0, W: 100, H: 400})
	w := NewTestWindow(Size{W: 100, H: 100})
	w.SetRoot(scroller)

	if diags := w.DebugLayoutDiagnostics(); hasDiagnostic(diags, "widget overflows parent bounds") {
		t.Fatalf("scroll content reported as an overflow: %+v", diags)
	}

	// The same content under a non-scrolling parent is still an overflow.
	plain := NewContainer(nil, newSized(0, 0))
	plain.Layout(Rect{W: 100, H: 100})
	plain.ChildList()[0].Layout(Rect{X: 0, Y: 0, W: 100, H: 400})
	w2 := NewTestWindow(Size{W: 100, H: 100})
	w2.SetRoot(plain)
	if diags := w2.DebugLayoutDiagnostics(); !hasDiagnostic(diags, "widget overflows parent bounds") {
		t.Fatalf("a real overflow went unreported: %+v", diags)
	}
}

func TestDrawDebugOverlayRecordsBoundsLabelsAndOverflow(t *testing.T) {
	child := newSized(0, 0)
	child.Layout(Rect{X: 80, Y: 10, W: 40, H: 20})
	root := NewContainer(nil, child)
	root.Layout(Rect{W: 100, H: 100})
	w := NewTestWindow(Size{W: 100, H: 100})
	w.SetRoot(root)
	w.EnableDebugOverlay(DebugOverlayOptions{ShowBounds: true, ShowLabels: true, ShowOverflow: true})

	canvas := &RecordingCanvas{}
	w.drawDebugOverlay(canvas)

	if len(canvas.Strokes) == 0 {
		t.Fatal("debug overlay should draw bounds")
	}
	if len(canvas.Texts) == 0 {
		t.Fatal("debug overlay should draw labels")
	}
	if len(canvas.Fills) == 0 {
		t.Fatal("debug overlay should draw label backgrounds or overflow fills")
	}
}

func hasDiagnostic(diags []LayoutDiagnostic, message string) bool {
	for _, d := range diags {
		if d.Message == message {
			return true
		}
	}
	return false
}
