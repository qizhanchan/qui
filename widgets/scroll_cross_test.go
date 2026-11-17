package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

// fixedBox measures to a fixed size and lays out wherever it is placed.
type fixedBox struct {
	BaseWidget
	sz Size
}

func newFixedBox(w, h float32) *fixedBox {
	b := &fixedBox{BaseWidget: NewBaseWidget(), sz: Size{W: w, H: h}}
	b.SetSelf(b)
	return b
}

func (b *fixedBox) Measure(Size) Size { return b.sz }

// crossView is a 200×100 CrossScroll view over a column of rows, one of them
// 500px wide (wider than the viewport, as a nowrap row overflows its box).
func crossView(t *testing.T, cross bool) (*ScrollView, *fixedBox) {
	t.Helper()
	wide := newFixedBox(500, 20)
	col := NewContainer(FlexLayout{Direction: Vertical, AlignItems: AlignStart},
		newFixedBox(50, 20), wide, newFixedBox(50, 300))
	sv := NewScrollView()
	sv.OverlayBar = true
	sv.CrossScroll = cross
	sv.SetContentAuto(col)
	sv.Layout(Rect{W: 200, H: 100})
	return sv, wide
}

func TestScrollViewCrossScrollRange(t *testing.T) {
	sv, wide := crossView(t, true)
	want := wide.Bounds().X + 500 - 200
	if got := sv.MaxScrollX(); got != want {
		t.Fatalf("MaxScrollX = %v, want %v (500px row in a 200px view)", got, want)
	}
	if got := sv.MaxScroll(); got <= 0 {
		t.Fatalf("vertical MaxScroll = %v; the column is taller than the view", got)
	}
	sv.ScrollToX(1000)
	if sv.ScrollX() != want {
		t.Fatalf("ScrollToX clamps to %v, want %v", sv.ScrollX(), want)
	}
	off, _ := crossView(t, false)
	if off.MaxScrollX() != 0 {
		t.Fatalf("without CrossScroll MaxScrollX = %v", off.MaxScrollX())
	}
}

// A two-finger swipe sideways scrolls X, a vertical one Y, and hit testing
// follows both offsets.
func TestScrollViewCrossScrollWheelAndHit(t *testing.T) {
	sv, wide := crossView(t, true)
	if !sv.Handle(NewScrollEvent(100, 50, -3, 0, 0)) {
		t.Fatal("sideways wheel not taken")
	}
	x := sv.ScrollX()
	if x <= 0 || sv.ScrollY() != 0 {
		t.Fatalf("after sideways wheel x=%v y=%v", x, sv.ScrollY())
	}
	sv.Handle(NewScrollEvent(100, 50, 0, -1, 0))
	if sv.ScrollY() <= 0 || sv.ScrollX() != x {
		t.Fatalf("after vertical wheel x=%v y=%v", sv.ScrollX(), sv.ScrollY())
	}
	sv.ScrollTo(0)
	sv.ScrollToX(250)
	// Viewport x 190 is content x 440: past the viewport, still on the
	// wide row.
	wb := wide.Bounds()
	if hit := sv.HitTest(Point{X: 190, Y: wb.Y + wb.H/2}); hit != Widget(wide) {
		t.Fatalf("hit at scrolled x = %T, want the wide row", hit)
	}
	// At the right edge, a sideways swipe further right chains out.
	sv.ScrollToX(sv.MaxScrollX())
	if sv.Handle(NewScrollEvent(100, 50, -3, 0, 0)) {
		t.Fatal("wheel at the right edge was swallowed instead of chaining")
	}
}
