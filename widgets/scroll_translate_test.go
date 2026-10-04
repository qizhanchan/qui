package widgets

import (
	"fmt"
	"testing"

	. "github.com/qizhanchan/qui"
)

// scrollRows builds a ScrollView over n stacked 20px rows in a 200x200
// viewport, mounted in a test window so dispatch and invalidation run.
func scrollRows(n int) (*Window, *ScrollView, []*Label) {
	win := NewTestWindow(Size{W: 200, H: 200})
	rows := make([]*Label, n)
	children := make([]Widget, n)
	for i := range rows {
		rows[i] = NewLabel(fmt.Sprintf("row %d", i))
		rows[i].SetID(fmt.Sprintf("row-%d", i))
		rows[i].SetPreferredSize(0, 20)
		children[i] = rows[i]
	}
	content := NewContainer(FlexLayout{Direction: Vertical}, children...)
	sv := NewScrollView()
	sv.SetContent(content, Size{W: 200, H: float32(n) * 20})
	win.SetRoot(sv)
	sv.Layout(Rect{W: 200, H: 200})
	return win, sv, rows
}

// Hit testing goes through the scroll transform: after scrolling one row
// height, the row that was second is under the viewport's top edge.
func TestScrollViewHitTestAfterScroll(t *testing.T) {
	win, sv, rows := scrollRows(50)

	if got := win.HitTestForTest(Point{X: 10, Y: 5}); got != Widget(rows[0]) {
		t.Fatalf("at scroll 0, top of viewport hit %v, want row-0", widgetID(got))
	}
	sv.ScrollTo(20)
	if got := win.HitTestForTest(Point{X: 10, Y: 5}); got != Widget(rows[1]) {
		t.Errorf("at scroll 20, top of viewport hit %v, want row-1", widgetID(got))
	}
	sv.ScrollTo(100)
	if got := win.HitTestForTest(Point{X: 10, Y: 25}); got != Widget(rows[6]) {
		t.Errorf("at scroll 100, y=25 hit %v, want row-6", widgetID(got))
	}
}

// A click inside a scrolled view reaches the right row AND that row sees the
// event in its own coordinate space (the space its bounds live in).
func TestScrollViewEventCoordsLocalized(t *testing.T) {
	win, sv, rows := scrollRows(50)
	sv.ScrollTo(100)

	target := rows[6] // content Y 120..140, on screen 20..40
	win.DispatchTestEvent(NewMouseEvent(EventMouseDown, 10, 25, MouseButtonLeft, 0))

	local := WindowPointToLocal(target, Point{X: 10, Y: 25})
	if local.Y != 125 {
		t.Errorf("window→local Y = %v, want 125", local.Y)
	}
	if !target.Bounds().Contains(local) {
		t.Errorf("localized point %+v outside row bounds %+v", local, target.Bounds())
	}
}

// Scrolling must not re-enter Layout on the content: that O(content) cost per
// wheel event is exactly what the transform replaces.
func TestScrollViewScrollDoesNotRelayoutContent(t *testing.T) {
	win := NewTestWindow(Size{W: 200, H: 200})
	content := &layoutCounter{BaseWidget: NewBaseWidget(), natural: Size{W: 200, H: 5000}}
	content.SetSelf(content)
	sv := NewScrollView()
	sv.SetContent(content, Size{W: 200, H: 5000})
	win.SetRoot(sv)
	sv.Layout(Rect{W: 200, H: 200})

	before := content.layouts
	for i := 0; i < 20; i++ {
		sv.ScrollTo(float32(i) * 37)
	}
	if content.layouts != before {
		t.Errorf("content laid out %d extra times across 20 scrolls; want 0",
			content.layouts-before)
	}
	if sv.ScrollOffset() == 0 {
		t.Fatal("nothing actually scrolled")
	}
}

// Scrolling dirties the viewport, not the whole window: the content's own
// extent (5000px tall) must not leak into the dirty region.
func TestScrollViewScrollDirtiesOnlyViewport(t *testing.T) {
	win := NewTestWindow(Size{W: 400, H: 400})
	content := &layoutCounter{BaseWidget: NewBaseWidget(), natural: Size{W: 200, H: 5000}}
	content.SetSelf(content)
	sv := NewScrollView()
	sv.SetContent(content, Size{W: 200, H: 5000})
	win.SetRoot(sv)
	sv.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})

	win.ClearDirtyRegion()
	sv.ScrollTo(80)
	dirty := win.DirtyRegion()
	if dirty.IsEmpty() {
		t.Fatal("a scroll must dirty the viewport")
	}
	if dirty.H > 200 || dirty.W > 200 {
		t.Errorf("dirty region %+v exceeds the 200x200 viewport", dirty)
	}
}

// A row scrolled out of view invalidates nothing — its dirty rect is clipped
// away by the viewport instead of dirtying whatever occupies those pixels.
func TestScrollViewOffscreenChildInvalidationClipped(t *testing.T) {
	win, sv, rows := scrollRows(50)
	sv.ScrollTo(0)
	win.ClearDirtyRegion()

	rows[40].Invalidate() // content Y 800..820, far below the viewport
	if got := win.DirtyRegion(); !got.IsEmpty() {
		t.Errorf("off-screen row dirtied %+v, want empty", got)
	}

	rows[2].Invalidate() // content Y 40..60, on screen
	if got := win.DirtyRegion(); got.IsEmpty() {
		t.Error("visible row invalidation was dropped")
	}
}

// ScrollChildIntoView still lands the child inside the viewport now that
// child bounds are scroll-independent.
func TestScrollViewScrollChildIntoView(t *testing.T) {
	_, sv, rows := scrollRows(50)

	sv.ScrollChildIntoView(rows[30]) // content Y 600..620
	vp := sv.Bounds()
	got := InteractionBoundsOf(rows[30])
	if got.Y < vp.Y || got.Y+got.H > vp.Y+vp.H {
		t.Errorf("row 30 on-screen at %+v, outside viewport %+v (scroll=%v)",
			got, vp, sv.ScrollOffset())
	}
}

// Nested scroll containers compose: an inner view's transform stacks on the
// outer one for hit testing, screen geometry, and point mapping.
func TestNestedScrollViewsCompose(t *testing.T) {
	win := NewTestWindow(Size{W: 200, H: 200})

	leaf := NewLabel("leaf")
	leaf.SetPreferredSize(0, 20)
	spacerIn := NewLabel("pad")
	spacerIn.SetPreferredSize(0, 150)
	innerContent := NewContainer(FlexLayout{Direction: Vertical}, spacerIn, leaf)
	inner := NewScrollView()
	inner.SetFixedHeight(80) // a viewport smaller than its content
	inner.SetContent(innerContent, Size{W: 200, H: 400})

	spacerOut := NewLabel("pad")
	spacerOut.SetPreferredSize(0, 400)
	outerContent := NewContainer(FlexLayout{Direction: Vertical}, spacerOut, inner)
	outer := NewScrollView()
	outer.SetContent(outerContent, Size{W: 200, H: 900})
	win.SetRoot(outer)
	outer.Layout(Rect{W: 200, H: 200})

	// Scroll the inner content so the leaf reaches the inner viewport's top,
	// then scroll the outer view so that inner viewport reaches the window
	// top. Offsets are derived from the real layout rather than assumed, so
	// the test asserts composition, not flex sizing.
	innerOffset := leaf.Bounds().Y - innerContent.Bounds().Y
	outerOffset := inner.Bounds().Y - outerContent.Bounds().Y
	inner.ScrollTo(innerOffset)
	outer.ScrollTo(outerOffset)
	if inner.ScrollOffset() != innerOffset || outer.ScrollOffset() != outerOffset {
		t.Fatalf("offsets clamped (inner %v/%v, outer %v/%v); fixture too small",
			inner.ScrollOffset(), innerOffset, outer.ScrollOffset(), outerOffset)
	}

	// Both translations compose into the on-screen position.
	if got, want := InteractionBoundsOf(leaf).Y, leaf.Bounds().Y-innerOffset-outerOffset; got != want {
		t.Errorf("leaf on-screen Y = %v, want %v", got, want)
	}
	if got := InteractionBoundsOf(leaf).Y; got != 0 {
		t.Errorf("leaf should sit at the window top, got Y=%v", got)
	}
	if hit := win.HitTestForTest(Point{X: 20, Y: 5}); hit != Widget(leaf) {
		t.Errorf("hit at viewport top = %v, want the leaf label", widgetID(hit))
	}
	local := WindowPointToLocal(leaf, Point{X: 20, Y: 5})
	if !leaf.Bounds().Contains(local) {
		t.Errorf("localized %+v outside leaf bounds %+v", local, leaf.Bounds())
	}
}

// layoutCounter counts Layout calls so a test can assert scrolling never
// re-lays the content out.
type layoutCounter struct {
	BaseWidget
	natural Size
	layouts int
}

func (l *layoutCounter) Measure(Size) Size { return l.natural }

func (l *layoutCounter) Layout(rect Rect) {
	l.layouts++
	l.BaseWidget.Layout(rect)
}

func widgetID(w Widget) string {
	if w == nil {
		return "<nil>"
	}
	if ided, ok := w.(interface{ ID() string }); ok && ided.ID() != "" {
		return ided.ID()
	}
	return fmt.Sprintf("%T", w)
}

// BenchmarkScrollViewScrollEvent is the point of the whole change: the cost of
// one scroll event must not grow with the content size.
func BenchmarkScrollViewScrollEvent(b *testing.B) {
	for _, rows := range []int{500, 10000} {
		b.Run(fmt.Sprintf("rows=%d", rows), func(b *testing.B) {
			_, sv, _ := scrollRows(rows)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				sv.ScrollTo(float32(i%1000) * 3)
			}
		})
	}
}
