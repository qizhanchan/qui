package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// scrollHost builds an overflow:auto element with 20 fixed-height rows and
// returns the host, its ScrollView, and the row elements.
func scrollHost(t *testing.T) (*El, *El) {
	t.Helper()
	eng := NewStyleEngine(`
		.host { width:200px; height:100px; overflow-y:auto }
		.row  { height:20px }`)
	host := eng.NewEl("div")
	host.SetClass("host")
	rows := make([]qui.Widget, 20)
	var third *El
	for i := range rows {
		r := eng.NewEl("div")
		r.SetClass("row")
		if i == 6 {
			r.SetID("row-6")
			third = r
		}
		rows[i] = r
	}
	host.SetElementChildren(rows)
	eng.SetRoot(host)

	win := qui.NewTestWindow(qui.Size{W: 400, H: 400})
	win.SetRoot(host)
	eng.Restyle()
	host.Layout(qui.Rect{W: 200, H: 100})
	return host, third
}

// Scrolling an overflow:auto element moves what the pointer hits, while the
// element children keep their scroll-independent layout bounds.
func TestOverflowScrollHitTestFollowsScroll(t *testing.T) {
	host, row6 := scrollHost(t)
	sv := host.scrollView
	if sv == nil {
		t.Fatal("overflow:auto did not host a ScrollView")
	}
	if sv.MaxScroll() <= 0 {
		t.Fatalf("content should overflow; maxScroll = %v", sv.MaxScroll())
	}

	before := row6.Bounds()
	// row 6 sits at content Y 120..140 — below the 100px viewport.
	if hit := host.HitTest(qui.Point{X: 10, Y: 25}); hit == qui.Widget(row6) {
		t.Fatal("row-6 must not be hittable before scrolling to it")
	}
	sv.ScrollTo(120)
	if got := row6.Bounds(); got != before {
		t.Errorf("child bounds moved on scroll: %+v → %+v", before, got)
	}
	hit := host.HitTest(qui.Point{X: 10, Y: 5})
	if hit != qui.Widget(row6) {
		t.Errorf("after scrolling to 120, viewport top hit %T, want row-6", hit)
	}
	// On-screen geometry (what the AX tree, agent, and overlay anchors use)
	// reflects the scroll; raw bounds do not.
	if got := qui.InteractionBoundsOf(row6).Y; got != 0 {
		t.Errorf("row-6 on-screen Y = %v, want 0", got)
	}
}

// A suggestion popup anchors to the field's ON-SCREEN box, so a datalist
// inside a scrolled container still opens next to its field.
func TestDatalistAnchorUsesScreenBounds(t *testing.T) {
	eng := NewStyleEngine(`.host { width:300px; height:60px; overflow-y:auto }`)
	host := eng.NewEl("div")
	host.SetClass("host")
	spacer := eng.NewEl("div")
	spacer.SetAttr("style", "height:200px")
	field := eng.NewEl("input")
	field.SetSuggestions([]string{"Paris", "Portland"})
	host.SetElementChildren([]qui.Widget{spacer, field})
	eng.SetRoot(host)

	win := qui.NewTestWindow(qui.Size{W: 400, H: 400})
	win.SetRoot(host)
	eng.Restyle()
	host.Layout(qui.Rect{W: 300, H: 60})

	sv := host.scrollView
	if sv == nil || sv.MaxScroll() <= 0 {
		t.Fatal("expected an overflowing scroll host")
	}
	sv.ScrollTo(sv.MaxScroll()) // bring the field into view

	field.refreshSuggestions("p")
	if !field.suggestOpen() {
		t.Fatal("suggestions did not open")
	}
	popup := field.suggestPopup.Content.Bounds()
	onScreen := qui.InteractionBoundsOf(field)
	retained := field.Bounds()
	if onScreen.Y == retained.Y {
		t.Fatalf("test is not exercising a scrolled field: on-screen %+v == retained %+v",
			onScreen, retained)
	}
	// The list hangs just under the field's ON-SCREEN box. Anchoring to the
	// retained (content-space) bounds would put it ~1 scroll offset lower.
	wantTop := onScreen.Y + onScreen.H
	if gap := popup.Y - wantTop; gap < 0 || gap > 8 {
		t.Errorf("popup top = %v, want just below the field's on-screen bottom %v "+
			"(retained bounds would give ~%v)", popup.Y, wantTop, retained.Y+retained.H)
	}
}
