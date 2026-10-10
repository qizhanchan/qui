package widgets

import (
	"image"
	"testing"

	. "github.com/qizhanchan/qui"
)

// badgePixels counts pixels in img (within r) that match c.
func badgePixels(img *image.RGBA, r image.Rectangle, c Color) int {
	want := ColorToRGBA(c)
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if img.RGBAAt(x, y) == want {
				n++
			}
		}
	}
	return n
}

// A row with Badge reserves room for the pill and paints it, right-aligned
// in the theme's badge colors; rows without one paint none.
func TestMenuItemBadge(t *testing.T) {
	st := themedMenuStyle()
	// A long label keeps the row off the 112 px floor, so the growth shows.
	const long = "A rather long menu label here"
	plain := newMenuItemView(&MenuItem{Label: long}, st, nil)
	badged := newMenuItemView(&MenuItem{Label: long, Badge: "New"}, st, nil)
	if pw, bw := plain.Measure(Size{}).W, badged.Measure(Size{}).W; bw <= pw {
		t.Fatalf("a badge adds no width: %v vs %v", bw, pw)
	}

	items := []*MenuItem{
		{Label: "Pageless", Badge: "New"},
		{Label: "Page setup"},
		{Label: "Tabs", Badge: "New", Shortcut: "Ctrl+T"},
		{Label: "Insert", Badge: "New", Submenu: []*MenuItem{{Label: "x"}}},
	}
	w := NewTestWindow(Size{W: 600, H: 500})
	w.SetRoot(NewContainer(FlexLayout{}))
	popup := ShowContextMenu(w, 0, 0, items)
	list := popup.Content.(*menuListView)
	canvas, img := NewOffscreenCanvas(600, 500)
	list.Draw(canvas)

	surface := CurrentTheme().ResolvedPopup().MenuBadgeSurface
	for i, wantBadge := range []bool{true, false, true, true} {
		b := list.content.ChildAt(i).Bounds()
		right := image.Rect(int(b.X+b.W/2), int(b.Y), int(b.X+b.W), int(b.Y+b.H))
		got := badgePixels(img, right, surface)
		if wantBadge && got < 150 { // a 16 px pill ~ 30 px wide
			t.Errorf("row %d (%q): badge pill not painted (%d px)", i, items[i].Label, got)
		}
		if !wantBadge && got != 0 {
			t.Errorf("row %d (%q): unexpected badge pixels (%d)", i, items[i].Label, got)
		}
		// Never over the label column.
		left := image.Rect(int(b.X), int(b.Y), int(b.X+menuBadgeGap), int(b.Y+b.H))
		if n := badgePixels(img, left, surface); n != 0 {
			t.Errorf("row %d: badge pixels in the leading column (%d)", i, n)
		}
	}
}

func TestMenuBadgeThemeTokens(t *testing.T) {
	p := CurrentTheme().ResolvedPopup()
	if p.MenuBadgeText.A == 0 || p.MenuBadgeSurface.A == 0 {
		t.Fatalf("badge tokens unresolved: %+v / %+v", p.MenuBadgeText, p.MenuBadgeSurface)
	}
	if p.MenuBadgeText == p.MenuBadgeSurface {
		t.Error("badge text and surface are the same color")
	}
	// A hand-built theme (no Popup tokens) still gets a tinted pill.
	var th Theme
	th.Accent = Color{R: 0, G: 0, B: 1, A: 1}
	th.Text = Color{A: 1}
	th.SurfaceOverlay = Color{R: 1, G: 1, B: 1, A: 1}
	if q := th.ResolvedPopup(); q.MenuBadgeText != th.Accent || q.MenuBadgeSurface.A == 0 {
		t.Errorf("fallback badge tokens = %+v / %+v", q.MenuBadgeText, q.MenuBadgeSurface)
	}
}
