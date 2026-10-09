package html_test

import (
	"testing"

	"github.com/qizhanchan/qui"
	h "github.com/qizhanchan/qui/reactive/html"
)

// Picking a menu row that lies over a clickable element must not click that
// element too. Menu rows act on MouseDown and close the popup; the release
// used to be hit-tested onto the element beneath, which fired its click on
// the release alone (q-ppt: Insert ▸ Text box also opened Insert image).
func TestMenuRowReleaseDoesNotClickElementBeneath(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 600, H: 400})
	under := 0
	h.Mount(win, ".under{width:600px;height:400px}", func() h.Node {
		return h.Div(h.Div(h.Text("under")).Class("under").OnClick(func() { under++ }))
	})
	win.LayoutForTest()
	if err := win.ClickAt(300, 300); err != nil || under != 1 {
		t.Fatalf("a plain click fired %d times (err %v), want 1", under, err)
	}

	under, picked := 0, 0
	h.ContextMenu(win, 100, 100, []h.MenuItem{{Label: "Pick", OnClick: func() { picked++ }}})
	win.LayoutForTest()
	ovs := win.Overlays()
	if len(ovs) == 0 {
		t.Fatal("context menu did not open")
	}
	b := ovs[len(ovs)-1].Bounds()
	if err := win.ClickAt(b.X+b.W/2, b.Y+20); err != nil {
		t.Fatal(err)
	}
	if picked != 1 {
		t.Fatalf("the menu row fired %d times, want 1", picked)
	}
	if under != 0 {
		t.Fatalf("the release under the closed menu clicked the element beneath (%d times)", under)
	}
	if err := win.ClickAt(300, 300); err != nil || under != 1 {
		t.Fatalf("the click after the menu pick fired %d times (err %v), want 1", under, err)
	}
}
