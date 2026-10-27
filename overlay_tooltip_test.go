package qui

import (
	"runtime"
	"testing"
	"time"
)

// fakeClipboard is an in-memory Clipboard for tests that assert copy.
type fakeClipboard struct{ text string }

func (f *fakeClipboard) Get() string     { return f.text }
func (f *fakeClipboard) Set(text string) { f.text = text }

// commandMod returns the platform's "command" modifier (Cmd on macOS,
// Ctrl elsewhere) so Cmd/Ctrl+C tests match IsCommandMod.
func commandMod() Modifiers {
	if runtime.GOOS == "darwin" {
		return ModSuper
	}
	return ModControl
}

// Tooltip tests drive the Window-level tooltip manager via its public
// AttachTooltip API + the synthetic hover path update that normally
// runs inside dispatch.

func TestAttachTooltipStoresText(t *testing.T) {
	w := &Window{lastSize: Size{W: 500, H: 500}}
	target := newPhaseSpy("t", Rect{X: 0, Y: 0, W: 100, H: 100}, nil)
	w.AttachTooltip(target, "Hello")
	if got := w.tooltips[target]; got != "Hello" {
		t.Errorf("tooltip not stored; got %q", got)
	}
}

func TestAttachTooltipEmptyRemoves(t *testing.T) {
	w := &Window{lastSize: Size{W: 500, H: 500}}
	target := newPhaseSpy("t", Rect{}, nil)
	w.AttachTooltip(target, "Hi")
	w.AttachTooltip(target, "")
	if _, ok := w.tooltips[target]; ok {
		t.Error("empty text should remove tooltip registration")
	}
}

func TestUpdateTooltipFromHoverShowsPopup(t *testing.T) {
	w := &Window{lastSize: Size{W: 500, H: 500}}
	target := newPhaseSpy("t", Rect{X: 10, Y: 10, W: 50, H: 50}, nil)
	w.AttachTooltip(target, "Hello")
	w.hoverPath = []Widget{target}

	w.updateTooltipFromHover(40, 40)
	if w.tooltipTarget != target {
		t.Errorf("tooltipTarget = %v, want target", w.tooltipTarget)
	}
	if w.tooltipView == nil {
		t.Error("tooltipView should be shown")
	}
	if len(w.overlays) == 0 {
		t.Error("tooltip popup should be on overlay stack")
	}
}

func TestUpdateTooltipFromHoverHidesWhenLeaving(t *testing.T) {
	w := &Window{lastSize: Size{W: 500, H: 500}}
	target := newPhaseSpy("t", Rect{}, nil)
	w.AttachTooltip(target, "Hi")
	w.hoverPath = []Widget{target}
	w.updateTooltipFromHover(10, 10)

	// Now hover moves to empty space (away from both the widget and the
	// tooltip) — this arms the grace-period close but does NOT close
	// immediately, so the user can slide onto the tooltip to copy.
	w.hoverPath = nil
	w.updateTooltipFromHover(400, 400)
	if w.tooltipView == nil {
		t.Fatal("tooltip should linger during the grace period, not close on leave")
	}
	if w.tooltipCloseAt.IsZero() {
		t.Error("leaving should arm the grace-period close deadline")
	}

	// Once the deadline passes, tickTooltip closes it.
	w.tickTooltip(time.Now().Add(tooltipGracePeriod + time.Second))
	if w.tooltipTarget != nil {
		t.Errorf("tooltipTarget should be cleared after grace expiry; got %v", w.tooltipTarget)
	}
	if w.tooltipView != nil {
		t.Error("tooltipView should be closed after grace expiry")
	}
}

// Sliding the pointer onto the tooltip during the grace period cancels
// the close so the text stays reachable for selection / copy.
func TestTooltipGraceKeptAliveWhenPointerEntersTooltip(t *testing.T) {
	w := &Window{lastSize: Size{W: 500, H: 500}}
	target := newPhaseSpy("t", Rect{X: 0, Y: 0, W: 50, H: 20}, nil)
	target.SetTooltip("Copy me")
	w.hoverPath = []Widget{target}
	w.updateTooltipFromHover(10, 10)
	tv := w.tooltipView
	if tv == nil {
		t.Fatal("tooltip should be shown")
	}

	// Leave the widget → grace armed.
	w.hoverPath = nil
	w.updateTooltipFromHover(400, 400)
	if w.tooltipCloseAt.IsZero() {
		t.Fatal("grace should be armed after leaving the widget")
	}

	// Move onto the tooltip → grace canceled, tooltip stays.
	b := tv.Bounds()
	w.updateTooltipFromHover(b.X+b.W/2, b.Y+b.H/2)
	if !w.tooltipCloseAt.IsZero() {
		t.Error("entering the tooltip should cancel the grace-period close")
	}
	// Even past the old deadline, it must not close while hovered.
	w.tickTooltip(time.Now().Add(tooltipGracePeriod + time.Second))
	if w.tooltipView == nil {
		t.Error("tooltip should stay open while the pointer is over it")
	}
}

// Dragging across the tooltip selects text; Cmd/Ctrl+C copies it.
func TestTooltipSelectAndCopy(t *testing.T) {
	fake := &fakeClipboard{}
	SetClipboardProvider(fake)
	defer SetClipboardProvider(nil)

	tv := newTooltipView("hello world", TooltipStyle{})
	tv.SetWindow(&Window{lastSize: Size{W: 500, H: 500}})
	tv.Layout(Rect{X: 0, Y: 0, W: 200, H: 40})
	tv.focused = true

	// Drag from the far left to the far right → selects the whole line.
	tv.Handle(NewMouseEvent(EventMouseDown, 0, 10, MouseButtonLeft, 0))
	tv.Handle(NewMouseEvent(EventMouseMove, 1000, 10, MouseButtonLeft, 0))
	tv.Handle(NewMouseEvent(EventMouseUp, 1000, 10, MouseButtonLeft, 0))
	if !tv.hasSelection() {
		t.Fatal("drag should have produced a selection")
	}

	// Cmd/Ctrl+C copies the selected text.
	tv.Handle(NewKeyEvent(EventKeyDown, KeyC, commandMod()))
	if fake.text != "hello world" {
		t.Errorf("clipboard = %q, want %q", fake.text, "hello world")
	}
}

func TestUpdateTooltipReplacesOnDifferentWidget(t *testing.T) {
	w := &Window{lastSize: Size{W: 500, H: 500}}
	a := newPhaseSpy("a", Rect{}, nil)
	b := newPhaseSpy("b", Rect{}, nil)
	w.AttachTooltip(a, "A-text")
	w.AttachTooltip(b, "B-text")

	w.hoverPath = []Widget{a}
	w.updateTooltipFromHover(10, 10)
	firstPopup := w.tooltipView
	if firstPopup == nil {
		t.Fatal("first tooltip should be shown")
	}

	w.hoverPath = []Widget{b}
	w.updateTooltipFromHover(50, 50)
	if w.tooltipTarget != b {
		t.Errorf("target = %v, want b", w.tooltipTarget)
	}
	if w.tooltipView == firstPopup {
		t.Error("moving to different widget should replace popup")
	}
}

func TestUntooltippedWidgetDoesNotTrigger(t *testing.T) {
	w := &Window{lastSize: Size{W: 500, H: 500}}
	x := newPhaseSpy("x", Rect{}, nil) // no tooltip registered
	w.hoverPath = []Widget{x}
	w.updateTooltipFromHover(10, 10)
	if w.tooltipTarget != nil {
		t.Errorf("no tooltip registered; target should stay nil, got %v", w.tooltipTarget)
	}
}

// SetTooltip on the widget itself (via BaseWidget → TooltipProvider) must
// trigger the same hover popup without any AttachTooltip registration.
func TestSetTooltipTriggersViaProvider(t *testing.T) {
	w := &Window{lastSize: Size{W: 500, H: 500}}
	target := newPhaseSpy("t", Rect{X: 10, Y: 10, W: 50, H: 50}, nil)
	target.SetTooltip("Widget-owned")
	w.hoverPath = []Widget{target}

	w.updateTooltipFromHover(40, 40)
	if w.tooltipTarget != target {
		t.Errorf("tooltipTarget = %v, want target", w.tooltipTarget)
	}
	if w.tooltipView == nil || w.tooltipView.text != "Widget-owned" {
		t.Errorf("tooltip should show provider text; got %+v", w.tooltipView)
	}
}

// An empty SetTooltip must not trigger (matches "no tooltip registered").
func TestEmptyProviderTooltipDoesNotTrigger(t *testing.T) {
	w := &Window{lastSize: Size{W: 500, H: 500}}
	target := newPhaseSpy("t", Rect{X: 0, Y: 0, W: 50, H: 50}, nil) // SetTooltip never called → ""
	w.hoverPath = []Widget{target}
	w.updateTooltipFromHover(10, 10)
	if w.tooltipTarget != nil {
		t.Errorf("empty provider text should not trigger; got %v", w.tooltipTarget)
	}
}

// AttachTooltip must win over a widget's own SetTooltip text.
func TestAttachTooltipOverridesProvider(t *testing.T) {
	w := &Window{lastSize: Size{W: 500, H: 500}}
	target := newPhaseSpy("t", Rect{X: 0, Y: 0, W: 50, H: 50}, nil)
	target.SetTooltip("provider")
	w.AttachTooltip(target, "attached")
	w.hoverPath = []Widget{target}
	w.updateTooltipFromHover(10, 10)
	if w.tooltipView == nil || w.tooltipView.text != "attached" {
		t.Errorf("AttachTooltip should win; got %+v", w.tooltipView)
	}
}

// Near the right/bottom edges the tooltip must flip so it stays fully
// inside the window bounds instead of overflowing off-screen.
func TestTooltipPositionAdaptsToEdges(t *testing.T) {
	w := &Window{lastSize: Size{W: 200, H: 200}}
	target := newPhaseSpy("t", Rect{X: 0, Y: 0, W: 200, H: 200}, nil)
	target.SetTooltip("edge")
	w.hoverPath = []Widget{target}

	// Cursor near the bottom-right corner.
	w.updateTooltipFromHover(195, 195)
	b := w.tooltipView.Bounds()
	if b.X+b.W > 200 {
		t.Errorf("tooltip overflows right edge: X+W=%v > 200", b.X+b.W)
	}
	if b.Y+b.H > 200 {
		t.Errorf("tooltip overflows bottom edge: Y+H=%v > 200", b.Y+b.H)
	}
	// It should have been placed above/left of the cursor, not below-right.
	if b.Y >= 195 {
		t.Errorf("tooltip should flip above cursor near bottom edge; Y=%v", b.Y)
	}
}

func TestDetachTooltipHidesActive(t *testing.T) {
	w := &Window{lastSize: Size{W: 500, H: 500}}
	target := newPhaseSpy("t", Rect{}, nil)
	w.AttachTooltip(target, "Hi")
	w.hoverPath = []Widget{target}
	w.updateTooltipFromHover(10, 10)

	w.DetachTooltip(target)
	if w.tooltipView != nil {
		t.Error("DetachTooltip should hide active tooltip")
	}
}

// itemSpy paints its own items: one tooltip per band, like a document
// canvas' table handles or a chart's data points.
type itemSpy struct {
	*phaseSpy
	items map[string]Rect
}

func (s *itemSpy) TooltipAt(p Point) (string, Rect, bool) {
	for text, r := range s.items {
		if r.Contains(p) {
			return text, r, true
		}
	}
	return "", Rect{}, false
}

// A widget that paints many interactive items answers per point, and the
// bubble anchors to the ITEM — not to the widget, which for a full-page
// canvas would put the text nowhere near what it describes.
func TestTooltipAtProviderAnchorsToItem(t *testing.T) {
	w := &Window{lastSize: Size{W: 500, H: 500}}
	target := &itemSpy{
		phaseSpy: newPhaseSpy("canvas", Rect{X: 0, Y: 0, W: 500, H: 500}, nil),
		items:    map[string]Rect{"Pin header up to this row": {X: 100, Y: 200, W: 20, H: 20}},
	}
	target.SetSelf(target)
	w.hoverPath = []Widget{target}

	w.updateTooltipFromHover(110, 210)
	if w.tooltipView == nil || w.tooltipView.text != "Pin header up to this row" {
		t.Fatalf("tooltipView = %+v", w.tooltipView)
	}
	// Anchored to the 20x20 item, so the bubble sits next to it rather
	// than next to the 500x500 widget.
	if b := w.tooltipView.Bounds(); b.Y < 180 || b.Y > 260 {
		t.Errorf("tooltip not anchored to the item: %+v", b)
	}
}

// Sliding from one item to another inside the SAME widget must swap the
// text. The widget never changes, so a target-identity check alone would
// leave the first item's text on screen — the anchor is what moves.
func TestTooltipAtProviderSwapsBetweenItems(t *testing.T) {
	w := &Window{lastSize: Size{W: 500, H: 500}}
	target := &itemSpy{
		phaseSpy: newPhaseSpy("canvas", Rect{X: 0, Y: 0, W: 500, H: 500}, nil),
		items: map[string]Rect{
			"Select row":       {X: 100, Y: 200, W: 20, H: 20},
			"Insert row below": {X: 140, Y: 200, W: 20, H: 20},
		},
	}
	target.SetSelf(target)
	w.hoverPath = []Widget{target}

	w.updateTooltipFromHover(110, 210)
	w.updateTooltipFromHover(150, 210)
	if w.tooltipView == nil || w.tooltipView.text != "Insert row below" {
		t.Fatalf("text did not follow the pointer to the next item: %+v", w.tooltipView)
	}
}

// Between items the widget's own whole-widget tooltip still applies: the
// per-point hook narrows the answer where it has one, it does not replace
// the fallback.
func TestTooltipAtProviderFallsBackToWidgetTooltip(t *testing.T) {
	w := &Window{lastSize: Size{W: 500, H: 500}}
	target := &itemSpy{
		phaseSpy: newPhaseSpy("canvas", Rect{X: 0, Y: 0, W: 500, H: 500}, nil),
		items:    map[string]Rect{"Select row": {X: 100, Y: 200, W: 20, H: 20}},
	}
	target.SetSelf(target)
	target.SetTooltip("document")
	w.hoverPath = []Widget{target}

	w.updateTooltipFromHover(400, 400) // between items
	if w.tooltipView == nil || w.tooltipView.text != "document" {
		t.Fatalf("expected the widget-level tooltip; got %+v", w.tooltipView)
	}
}

// Google-style placement: the chip is centred under its control, 5 px
// below it, 24 px tall, and clamped on screen near either edge.
func TestTooltipCentredUnderControl(t *testing.T) {
	w := &Window{lastSize: Size{W: 800, H: 600}}
	target := newPhaseSpy("t", Rect{X: 300, Y: 70, W: 30, H: 30}, nil)
	target.SetTooltip("Bold (⌘B)")
	w.hoverPath = []Widget{target}
	w.updateTooltipFromHover(310, 80)
	b := w.tooltipView.Bounds()
	if c := b.X + b.W/2; c < 314.5 || c > 315.5 {
		t.Errorf("tooltip centre x = %v, want 315 (control centre)", c)
	}
	if b.Y != 105 {
		t.Errorf("tooltip top = %v, want 105 (5 px under the control)", b.Y)
	}
	// 3 px padding above and below one text line (24 px with the 18 px
	// line of the default 12 px face).
	if want := w.tooltipView.lineHeight(w.tooltipView.font) + 6; b.H < want-0.5 || b.H > want+0.5 {
		t.Errorf("tooltip height = %v, want line + 6 = %v", b.H, want)
	}
	// At the left edge it shifts right to stay on screen.
	w2 := &Window{lastSize: Size{W: 800, H: 600}}
	edge := newPhaseSpy("e", Rect{X: 0, Y: 70, W: 20, H: 30}, nil)
	edge.SetTooltip("A long tooltip near the window edge")
	w2.hoverPath = []Widget{edge}
	w2.updateTooltipFromHover(5, 80)
	if b := w2.tooltipView.Bounds(); b.X < 4 {
		t.Errorf("tooltip ran off the left edge: x=%v", b.X)
	}
}

// tooltipCentreX is the horizontal centre of the shown chip.
func tooltipCentreX(w *Window) float32 {
	b := w.tooltipView.Bounds()
	return b.X + b.W/2
}

// The chip centres on its control whatever the control's width — a wide
// control puts it mid-control, not at the control's left edge.
func TestTooltipCentredOnWideControl(t *testing.T) {
	w := &Window{lastSize: Size{W: 800, H: 600}}
	target := newPhaseSpy("wide", Rect{X: 200, Y: 40, W: 240, H: 32}, nil)
	target.SetTooltip("Zoom")
	w.hoverPath = []Widget{target}
	// The pointer enters at the control's far left; the chip still centres
	// on the control, not on the pointer or the left edge.
	w.updateTooltipFromHover(203, 50)
	if c := tooltipCentreX(w); c < 319.5 || c > 320.5 {
		t.Errorf("tooltip centre x = %v, want 320 (control centre)", c)
	}
}

// Near the bottom edge the chip flips ABOVE its control, still centred on
// it and with the same 5 px gap.
func TestTooltipFlipsAboveNearBottomCentred(t *testing.T) {
	w := &Window{lastSize: Size{W: 800, H: 600}}
	target := newPhaseSpy("status", Rect{X: 400, Y: 570, W: 60, H: 26}, nil)
	target.SetTooltip("Zoom level")
	w.hoverPath = []Widget{target}
	w.updateTooltipFromHover(420, 580)
	b := w.tooltipView.Bounds()
	if want := float32(570) - 5 - b.H; b.Y != want {
		t.Errorf("tooltip top = %v, want %v (5 px above the control)", b.Y, want)
	}
	if c := tooltipCentreX(w); c < 429.5 || c > 430.5 {
		t.Errorf("tooltip centre x = %v, want 430 (control centre)", c)
	}
}

// A control against the right edge keeps its chip inside the window.
func TestTooltipClampedAtRightEdge(t *testing.T) {
	w := &Window{lastSize: Size{W: 800, H: 600}}
	target := newPhaseSpy("account", Rect{X: 770, Y: 8, W: 30, H: 30}, nil)
	target.SetTooltip("Google Account: someone@example.com")
	w.hoverPath = []Widget{target}
	w.updateTooltipFromHover(785, 20)
	if b := w.tooltipView.Bounds(); b.X+b.W > 800-4+0.01 || b.X < 4 {
		t.Errorf("tooltip not kept inside the window: %+v", b)
	}
}

// End to end: a real pointer move through the window's dispatch shows the
// chip centred under the control, and moving to the next control swaps it
// with the same centring.
func TestTooltipCentredFromPointerMove(t *testing.T) {
	w := NewTestWindow(Size{W: 800, H: 600})
	log := []phaseLogEntry{}
	a := newPhaseSpy("a", Rect{X: 100, Y: 60, W: 40, H: 30}, &log)
	b := newPhaseSpy("b", Rect{X: 300, Y: 60, W: 80, H: 30}, &log)
	a.SetTooltip("Undo")
	b.SetTooltip("Print")
	root := newPhaseSpyContainer("root", Rect{X: 0, Y: 0, W: 800, H: 600}, &log, a, b)
	w.SetRoot(root)

	w.DispatchTestEvent(NewMouseEvent(EventMouseMove, 105, 70, MouseButtonLeft, 0))
	if w.tooltipView == nil || w.tooltipView.text != "Undo" {
		t.Fatalf("tooltip after hovering a = %+v", w.tooltipView)
	}
	if c := tooltipCentreX(w); c < 119.5 || c > 120.5 {
		t.Errorf("tooltip centre x = %v, want 120", c)
	}
	w.DispatchTestEvent(NewMouseEvent(EventMouseMove, 310, 70, MouseButtonLeft, 0))
	if w.tooltipView == nil || w.tooltipView.text != "Print" {
		t.Fatalf("tooltip after hovering b = %+v", w.tooltipView)
	}
	if c := tooltipCentreX(w); c < 339.5 || c > 340.5 {
		t.Errorf("tooltip centre x = %v, want 340", c)
	}
	if y := w.tooltipView.Bounds().Y; y != 95 {
		t.Errorf("tooltip top = %v, want 95", y)
	}
}
