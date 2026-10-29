package widgets

import (
	"strconv"
	"testing"

	. "github.com/qizhanchan/qui"
)

// clipCanvas records the clips a Draw sets.
type clipCanvas struct {
	listNoopCanvas
	clips []Rect
}

func (c *clipCanvas) ClipRect(r Rect) { c.clips = append(c.clips, r) }

func popupRows(n int) (*Box, []*Button) {
	list := NewBox(FlowLayout{})
	var rows []*Button
	for i := range n {
		b := NewButton("row "+strconv.Itoa(i), nil)
		list.AddChild(b)
		rows = append(rows, b)
	}
	return list, rows
}

// Content taller than the window: ShowAt keeps the popup inside the window
// (popupEdge from its edges) and scrolls the content, which keeps its full
// height; ScrollChildIntoView reaches the last row.
func TestPopupCapsAndScrollsTallContent(t *testing.T) {
	w := NewTestWindow(Size{W: 400, H: 300})
	list, rows := popupRows(80)
	p := NewPopup(list)
	p.ShowAt(w, 20, 40)
	b := p.Bounds()
	if !p.Scrolls() {
		t.Fatal("80 rows in a 300px window should scroll")
	}
	if b.Y < 0 || b.Y+b.H > 300 || b.H > 300-2*popupEdge {
		t.Errorf("popup %+v not inside the window", b)
	}
	if list.Bounds().H <= b.H {
		t.Errorf("content %v should keep its full height (> %v)", list.Bounds().H, b.H)
	}
	p.ScrollChildIntoView(rows[79])
	if p.ScrollOffset() == 0 {
		t.Error("the last row did not scroll into view")
	}
	// clicks reach rows through the scroll translation
	last := InteractionBoundsOf(rows[79])
	if hit := p.HitTest(Point{X: last.X + 5, Y: last.Y + last.H/2}); hit != rows[79] {
		t.Errorf("hit %T, want the last row", hit)
	}
	// MaxSize caps below the window too
	p.Close()
	p.MaxSize = Size{H: 120}
	p.ShowAt(w, 20, 40)
	if h := p.Bounds().H; h > 120 {
		t.Errorf("height %v past MaxSize 120", h)
	}
	// a short content no longer scrolls, and sits under the popup again
	short, _ := popupRows(3)
	p.SetContent(short)
	if p.Scrolls() || short.Parent() != p {
		t.Errorf("3 rows: scrolls=%v parent=%T", p.Scrolls(), short.Parent())
	}
}

// Content drawing is clipped to the popup: nothing inside it can paint
// over the page or outlive it.
func TestPopupClipsContent(t *testing.T) {
	w := NewTestWindow(Size{W: 400, H: 300})
	list, _ := popupRows(3)
	p := NewPopup(list)
	p.ShowAt(w, 20, 40)
	c := &clipCanvas{}
	p.Draw(c)
	if len(c.clips) == 0 || c.clips[0] != p.PaintBounds() {
		t.Errorf("clips %v, want the popup's paint bounds %v first", c.clips, p.PaintBounds())
	}
}
