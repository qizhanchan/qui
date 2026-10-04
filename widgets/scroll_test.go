package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

func newScrollEvent(deltaY, x, y float32) MouseEvent {
	return NewScrollEvent(x, y, 0, deltaY, 0)
}

func TestScrollViewNoScrollbarWhenContentFits(t *testing.T) {
	sv := NewScrollView()
	content := newPhaseSpy("c", Rect{}, nil)
	sv.SetContent(content, Size{W: 100, H: 100})
	sv.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})

	if sv.hasVerticalBar() {
		t.Error("no scrollbar expected when content fits viewport")
	}
	if sv.maxScroll() != 0 {
		t.Errorf("maxScroll should be 0 when content fits; got %v", sv.maxScroll())
	}
}

func TestScrollViewHasScrollbarWhenContentOverflows(t *testing.T) {
	sv := NewScrollView()
	content := newPhaseSpy("c", Rect{}, nil)
	sv.SetContent(content, Size{W: 100, H: 1000})
	sv.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})

	if !sv.hasVerticalBar() {
		t.Error("scrollbar should render when content overflows")
	}
	if sv.maxScroll() != 800 {
		t.Errorf("maxScroll = %v, want 800 (1000 - 200)", sv.maxScroll())
	}
}

func TestScrollViewClampsScroll(t *testing.T) {
	sv := NewScrollView()
	content := newPhaseSpy("c", Rect{}, nil)
	sv.SetContent(content, Size{W: 100, H: 500})
	sv.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})
	// maxScroll = 300

	sv.ScrollTo(-50)
	if sv.ScrollY() != 0 {
		t.Errorf("negative scroll should clamp to 0; got %v", sv.ScrollY())
	}
	sv.ScrollTo(1000)
	if sv.ScrollY() != 300 {
		t.Errorf("over-scroll should clamp to maxScroll=300; got %v", sv.ScrollY())
	}
}

func TestScrollViewMouseWheelScrolls(t *testing.T) {
	sv := NewScrollView()
	content := newPhaseSpy("c", Rect{}, nil)
	sv.SetContent(content, Size{W: 100, H: 1000})
	sv.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})

	// Wheel down (negative DeltaY) should scroll content up, i.e.,
	// increase scrollY. Note: we invert DeltaY convention so wheel-down
	// moves content up; see wheelStep.
	sv.Handle(newScrollEvent(-1, 100, 100))
	if sv.ScrollY() == 0 {
		t.Errorf("scroll wheel had no effect; scrollY=%v", sv.ScrollY())
	}
}

func TestScrollViewWheelIgnoredOutsideBounds(t *testing.T) {
	sv := NewScrollView()
	content := newPhaseSpy("c", Rect{}, nil)
	sv.SetContent(content, Size{W: 100, H: 1000})
	sv.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})

	// Cursor outside ScrollView — scroll should be ignored.
	consumed := sv.Handle(newScrollEvent(-1, 500, 500))
	if consumed {
		t.Error("scroll outside bounds should not be consumed")
	}
	if sv.ScrollY() != 0 {
		t.Errorf("scrollY changed from out-of-bounds wheel; got %v", sv.ScrollY())
	}
}

// Scrolling does NOT move the content's retained bounds — the offset is a
// transform. On-screen geometry comes from the interaction transform.
func TestScrollViewContentTranslatedNotRelaidOut(t *testing.T) {
	sv := NewScrollView()
	content := newPhaseSpy("c", Rect{}, nil)
	sv.SetContent(content, Size{W: 100, H: 1000})
	sv.Layout(Rect{X: 0, Y: 10, W: 200, H: 200})

	if content.Bounds().Y != 10 {
		t.Errorf("content Y at scrollY=0: got %v, want 10", content.Bounds().Y)
	}
	sv.ScrollTo(50)
	if got := content.Bounds().Y; got != 10 {
		t.Errorf("content Y must stay at the layout origin, got %v", got)
	}
	if got := InteractionBoundsOf(content).Y; got != -40 {
		t.Errorf("on-screen content Y at scrollY=50: got %v, want -40", got)
	}
	// A point 50 px down the viewport maps to content Y = 100.
	if got := WindowPointToLocal(content, Point{X: 5, Y: 60}); got.Y != 110 {
		t.Errorf("window→content mapping: got %v, want Y=110", got)
	}
}

func TestScrollViewScrollbarDrag(t *testing.T) {
	sv := NewScrollView()
	content := newPhaseSpy("c", Rect{}, nil)
	sv.SetContent(content, Size{W: 100, H: 1000})
	sv.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})

	// Thumb sits at top (scrollY=0). Its track has H=200, thumbH = max(200*200/1000, 24) = 40.
	// Travel = 200 - 40 = 160. Click in middle of thumb.
	thumb := sv.barThumb()
	mouseY := thumb.Y + thumb.H/2
	mouseX := thumb.X + thumb.W/2

	sv.Handle(newMouseEventForHandle(EventMouseDown, mouseX, mouseY))
	if !sv.barDragging {
		t.Fatal("MouseDown on thumb should start drag")
	}
	// Move mouse down 40 pixels.
	sv.Handle(newMouseEventForHandle(EventMouseMove, mouseX, mouseY+40))
	// 40 px of travel / 160 total = 0.25 of range → scrollY = 0.25 * 800 = 200.
	expected := float32(200)
	if sv.ScrollY() < expected-1 || sv.ScrollY() > expected+1 {
		t.Errorf("scrollbar drag: scrollY=%v, want ~%v", sv.ScrollY(), expected)
	}
	sv.Handle(newMouseEventForHandle(EventMouseUp, mouseX, mouseY+40))
	if sv.barDragging {
		t.Error("MouseUp should end drag")
	}
}

func TestScrollViewHitTestScrollbarBeatsContent(t *testing.T) {
	sv := NewScrollView()
	child := newPhaseSpy("c", Rect{X: 0, Y: 0, W: 200, H: 1000}, nil)
	sv.SetContent(child, Size{W: 200, H: 1000})
	sv.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})

	// Scrollbar area is at x ∈ [190, 200]. Point (195, 100) should hit
	// the scrollview (for bar interaction), not the child widget.
	hit := sv.HitTest(Point{X: 195, Y: 100})
	if hit != sv {
		t.Errorf("hit on scrollbar should return ScrollView; got %v", hit)
	}
	// Point (100, 100) is inside child bounds and outside scrollbar.
	hit = sv.HitTest(Point{X: 100, Y: 100})
	if hit == sv {
		t.Errorf("hit in content area should return child, not ScrollView")
	}
}

func TestScrollViewKeyboardScroll(t *testing.T) {
	sv := NewScrollView()
	content := newPhaseSpy("c", Rect{}, nil)
	sv.SetContent(content, Size{W: 100, H: 1000})
	sv.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})
	sv.SetFocused(true)

	sv.Handle(NewKeyEvent(EventKeyDown, KeyDown, 0))
	if sv.ScrollY() != defaultKeyStep {
		t.Errorf("KeyDown: scrollY=%v, want %v", sv.ScrollY(), defaultKeyStep)
	}
	sv.Handle(NewKeyEvent(EventKeyDown, KeyUp, 0))
	if sv.ScrollY() != 0 {
		t.Errorf("KeyUp: scrollY=%v, want 0", sv.ScrollY())
	}
}

func TestScrollViewHorizontalBarAtBottom(t *testing.T) {
	sv := NewScrollView()
	sv.Orientation = ScrollHorizontal
	content := newPhaseSpy("c", Rect{}, nil)
	sv.SetContent(content, Size{W: 1000, H: 60})
	sv.Layout(Rect{X: 0, Y: 0, W: 200, H: 60})

	if !sv.hasHorizontalBar() {
		t.Error("horizontal scrollbar should render when content overflows on X")
	}
	if sv.hasVerticalBar() {
		t.Error("vertical bar must be off in horizontal orientation")
	}
	if sv.maxScroll() != 800 {
		t.Errorf("maxScroll = %v, want 800 (1000 - 200)", sv.maxScroll())
	}
	track := sv.barTrack()
	if track.Y < 50 || track.W != 200 {
		t.Errorf("horizontal track should run along the bottom edge; got %+v", track)
	}
}

func TestScrollViewHorizontalContentTranslatedOnScrollX(t *testing.T) {
	sv := NewScrollView()
	sv.Orientation = ScrollHorizontal
	content := newPhaseSpy("c", Rect{}, nil)
	sv.SetContent(content, Size{W: 1000, H: 60})
	sv.Layout(Rect{X: 10, Y: 0, W: 200, H: 60})

	if content.Bounds().X != 10 {
		t.Errorf("content X at scroll=0: got %v, want 10", content.Bounds().X)
	}
	sv.ScrollTo(75)
	if got := content.Bounds().X; got != 10 {
		t.Errorf("content X must stay at the layout origin, got %v", got)
	}
	if got := InteractionBoundsOf(content).X; got != -65 {
		t.Errorf("on-screen content X at scroll=75: got %v, want -65", got)
	}
}

func TestScrollViewHorizontalWheelDeltaY(t *testing.T) {
	// Vertical wheel on a horizontal ScrollView still scrolls — most
	// mice only have a vertical wheel, so wheel-down on an overflowing
	// toolbar must drive the X offset.
	sv := NewScrollView()
	sv.Orientation = ScrollHorizontal
	content := newPhaseSpy("c", Rect{}, nil)
	sv.SetContent(content, Size{W: 1000, H: 60})
	sv.Layout(Rect{X: 0, Y: 0, W: 200, H: 60})

	sv.Handle(newScrollEvent(-1, 100, 30))
	if sv.ScrollY() == 0 {
		t.Errorf("DeltaY wheel on horizontal scroll had no effect; got %v", sv.ScrollY())
	}
}

func TestScrollViewHorizontalKeyboardLeftRight(t *testing.T) {
	sv := NewScrollView()
	sv.Orientation = ScrollHorizontal
	content := newPhaseSpy("c", Rect{}, nil)
	sv.SetContent(content, Size{W: 1000, H: 60})
	sv.Layout(Rect{X: 0, Y: 0, W: 200, H: 60})
	sv.SetFocused(true)

	sv.Handle(NewKeyEvent(EventKeyDown, KeyRight, 0))
	if sv.ScrollY() != defaultKeyStep {
		t.Errorf("KeyRight: scroll=%v, want %v", sv.ScrollY(), defaultKeyStep)
	}
	sv.Handle(NewKeyEvent(EventKeyDown, KeyLeft, 0))
	if sv.ScrollY() != 0 {
		t.Errorf("KeyLeft: scroll=%v, want 0", sv.ScrollY())
	}
	// Up/Down must NOT scroll a horizontal view (it's the wrong axis).
	sv.Handle(NewKeyEvent(EventKeyDown, KeyDown, 0))
	if sv.ScrollY() != 0 {
		t.Errorf("KeyDown on horizontal scroll should be ignored; got %v", sv.ScrollY())
	}
}

func TestScrollViewSetContentWiresParent(t *testing.T) {
	sv := NewScrollView()
	child := newPhaseSpy("c", Rect{}, nil)
	sv.SetContent(child, Size{W: 100, H: 100})
	if child.Parent() != sv {
		t.Errorf("SetContent should wire child.Parent = ScrollView; got %v", child.Parent())
	}
}

func TestScrollViewChildListIncludesContent(t *testing.T) {
	sv := NewScrollView()
	child := newPhaseSpy("c", Rect{}, nil)
	sv.SetContent(child, Size{W: 100, H: 100})

	list := sv.ChildList()
	if len(list) != 1 || list[0] != child {
		t.Errorf("ChildList = %+v, want [child]", list)
	}
}

func TestScrollViewContentFocusablesViaChildList(t *testing.T) {
	// Verify that a focusable widget inside ScrollView's content tree
	// is discovered by the focus collector — proof that ChildList plays
	// nicely with the framework's tree walk.
	sv := NewScrollView()
	focusable := newFocusableSpy("f")
	inner := NewContainer(nil, focusable)
	sv.SetContent(inner, Size{W: 100, H: 100})

	root := NewContainer(nil, sv)
	w := windowWithRoot(Size{W: 200, H: 200}, root)
	list := w.CollectFocusables()

	found := false
	for _, wd := range list {
		if wd == focusable {
			found = true
		}
	}
	if !found {
		t.Errorf("focusable inside ScrollView should appear in focus list; got %+v", list)
	}
}

// TestScrollViewTransparentViewportPaintsNothing is the regression guard for
// the dark-card bug: the default (transparent) viewport must not fall back to
// the theme Surface, so a nested scroll host reveals its parent's background.
// Only an explicitly opaque background is painted.
func TestScrollViewTransparentViewportPaintsNothing(t *testing.T) {
	sv := NewScrollView()
	if _, ok := sv.viewportFill(); ok {
		t.Error("default transparent viewport must not paint a fill")
	}
	sv.Style().Background = Color{R: 0.97, G: 0.97, B: 0.97, A: 1}
	if _, ok := sv.viewportFill(); !ok {
		t.Error("explicit opaque viewport should paint a fill")
	}
}
