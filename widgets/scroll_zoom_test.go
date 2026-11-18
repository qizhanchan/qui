package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

// zoomProbe is content that fills the width it is given, 1000 units tall.
type zoomProbe struct {
	BaseWidget
	laidOut Rect
}

func (p *zoomProbe) Measure(avail Size) Size { return Size{W: avail.W, H: 1000} }
func (p *zoomProbe) Layout(r Rect)           { p.BaseWidget.Layout(r); p.laidOut = r }
func (p *zoomProbe) HitTest(pt Point) Widget {
	if p.Bounds().Contains(pt) {
		return p
	}
	return nil
}

func TestScrollViewContentZoom(t *testing.T) {
	win := NewTestWindow(Size{W: 400, H: 300})
	sv := NewScrollView()
	sv.OverlayBar = true // no bar slot: the content gets the whole width
	p := &zoomProbe{BaseWidget: NewBaseWidget()}
	p.SetSelf(p)
	sv.SetContentAuto(p)
	win.SetRoot(sv)
	win.LayoutForTest()
	if p.laidOut.W != 400 {
		t.Fatalf("unzoomed content width %v", p.laidOut.W)
	}

	sv.SetContentZoom(2)
	sv.ContentSize = Size{} // re-derive at the zoomed width
	win.LayoutForTest()
	if p.laidOut.W != 200 {
		t.Fatalf("zoomed content laid out at %v wide, want 200 (viewport / zoom)", p.laidOut.W)
	}
	// Content (10, 10) paints and hit-tests at (20, 20).
	if got := LocalPointToWindow(p, Point{X: 10, Y: 10}); got != (Point{X: 20, Y: 20}) {
		t.Errorf("content (10,10) maps to %v, want (20,20)", got)
	}
	if hit := win.WidgetAt(Point{X: 390, Y: 290}); hit != Widget(p) {
		t.Errorf("hit at the bottom-right = %T, want the content", hit)
	}
	// Scroll offsets are content units: 1000 tall in a 150-unit viewport.
	if got := sv.MaxScroll(); got != 850 {
		t.Errorf("MaxScroll = %v, want 850", got)
	}
	sv.ScrollTo(100)
	if got := LocalPointToWindow(p, Point{X: 0, Y: 100}); got != (Point{}) {
		t.Errorf("after ScrollTo(100), content y=100 maps to %v, want the top", got)
	}
	sv.SetContentZoom(1)
	sv.ContentSize = Size{}
	win.LayoutForTest()
	if p.laidOut.W != 400 || sv.ContentZoom() != 1 {
		t.Errorf("reset: width %v zoom %v", p.laidOut.W, sv.ContentZoom())
	}
}
