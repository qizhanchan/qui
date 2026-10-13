package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// Rows are 20px in a 100px viewport; row 6 sits at content Y 120..140.
func TestScrollIntoViewNearest(t *testing.T) {
	host, row6 := scrollHost(t)
	sv := host.scrollView

	row6.ScrollIntoView(ScrollBlockNearest)
	if got := sv.ScrollOffset(); got != 40 {
		t.Fatalf("below the viewport: offset = %v, want 40 (bottom-aligned)", got)
	}
	if got := qui.InteractionBoundsOf(row6); got.Y != 80 || got.Y+got.H != 100 {
		t.Errorf("row-6 on screen = %+v, want 80..100", got)
	}
	// Already visible: no move.
	row6.ScrollIntoView(ScrollBlockNearest)
	if got := sv.ScrollOffset(); got != 40 {
		t.Errorf("visible row moved the scroll: %v", got)
	}
	// Above the viewport: top-aligned.
	sv.ScrollTo(200)
	row6.ScrollIntoView(ScrollBlockNearest)
	if got := sv.ScrollOffset(); got != 120 {
		t.Errorf("above the viewport: offset = %v, want 120", got)
	}
}

func TestScrollIntoViewBlocks(t *testing.T) {
	host, row6 := scrollHost(t)
	sv := host.scrollView
	cases := []struct {
		block ScrollBlock
		want  float32
	}{
		{ScrollBlockStart, 120},
		{ScrollBlockEnd, 40},
		{ScrollBlockCenter, 80}, // 130 (row center) - 50
		{ScrollBlockCenterIfNeeded, 80},
	}
	for _, c := range cases {
		sv.ScrollTo(0)
		row6.ScrollIntoView(c.block)
		if got := sv.ScrollOffset(); got != c.want {
			t.Errorf("block %d: offset = %v, want %v", c.block, got, c.want)
		}
	}
}

// Start-aligning the last row would need offset 380; it clamps to the max.
func TestScrollIntoViewCenterIfNeededKeepsVisible(t *testing.T) {
	host, row6 := scrollHost(t)
	sv := host.scrollView
	sv.ScrollTo(50) // row 6 on screen at 70..90
	row6.ScrollIntoView(ScrollBlockCenterIfNeeded)
	if got := sv.ScrollOffset(); got != 50 {
		t.Errorf("visible row moved the scroll: %v", got)
	}
	sv.ScrollTo(130) // row 6 half hidden above the viewport
	row6.ScrollIntoView(ScrollBlockCenterIfNeeded)
	if got := sv.ScrollOffset(); got != 80 {
		t.Errorf("partly hidden row: offset = %v, want 80 (centered)", got)
	}
}

func TestScrollIntoViewClamps(t *testing.T) {
	eng := NewStyleEngine(`
		.host { width:200px; height:100px; overflow-y:auto }
		.row  { height:20px }`)
	host := eng.NewEl("div")
	host.SetClass("host")
	rows := make([]qui.Widget, 20)
	for i := range rows {
		r := eng.NewEl("div")
		r.SetClass("row")
		rows[i] = r
	}
	host.SetElementChildren(rows)
	eng.SetRoot(host)
	win := qui.NewTestWindow(qui.Size{W: 400, H: 400})
	win.SetRoot(host)
	eng.Restyle()
	host.Layout(qui.Rect{W: 200, H: 100})

	rows[19].(*El).ScrollIntoView(ScrollBlockStart)
	sv := host.scrollView
	if got, max := sv.ScrollOffset(), sv.MaxScroll(); got != max || max <= 0 {
		t.Errorf("offset = %v, want max %v", got, max)
	}
}
