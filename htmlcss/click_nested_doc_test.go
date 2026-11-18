package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// A document hosted inside another htmlcss tree (a browser page inside its
// chrome) hears a release once per tree; its link still follows once.
func TestNestedDocumentLinkFollowsOnce(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	inner := RenderDoc(`<html><body style="margin:0"><a href="next.html" style="display:block;height:40px">next</a></body></html>`, "",
		Options{Window: win, ViewportWidth: 400, DocumentRoot: true})
	follows := 0
	inner.Engine.SetLinkHandler(func(string, *El) bool { follows++; return true })
	outer := RenderDoc(`<div id="host" style="width:400px;height:300px"></div>`, "", Options{Window: win, ViewportWidth: 400})
	host, _ := outer.ByID["host"].(*El)
	if host == nil {
		t.Fatal("no host element")
	}
	host.SetOnClick(func() {}) // an outer element that hears clicks
	host.SetHostedWidget(inner.Root)
	win.SetRoot(outer.Root)
	win.LayoutForTest()
	if err := win.ClickAt(20, 20); err != nil {
		t.Fatal(err)
	}
	if follows != 1 {
		t.Errorf("one click followed the link %d times, want 1", follows)
	}
}
