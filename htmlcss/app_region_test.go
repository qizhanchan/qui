package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// app-region is what lets a stylesheet declare which parts of a
// self-drawn title bar move the OS window. The interesting behavior is not
// the parse but the resolution: it inherits, so a strip's descendants drag
// with it, and a `no-drag` island (a tab, a button) blocks the strip from
// dragging even though the strip is the element that receives the press by
// bubbling. Getting that backwards makes the whole window move whenever the
// user clicks a tab.
const appRegionCSS = `
  #strip { app-region: drag; display: flex; }
  #tab   { app-region: no-drag; }
  #wk    { -webkit-app-region: drag; }
`

func appRegionDoc(t *testing.T) RenderResult {
	t.Helper()
	res := RenderDoc(
		`<body><div id="strip"><span id="fill">···</span>`+
			`<div id="tab"><span id="tab-text">Tab</span></div></div>`+
			`<div id="wk">electron alias</div></body>`,
		appRegionCSS, Options{})
	return res
}

func elByID(t *testing.T, res RenderResult, id string) *El {
	t.Helper()
	w, ok := res.ByID[id]
	if !ok {
		t.Fatalf("no element with id %q", id)
	}
	el, ok := w.(*El)
	if !ok {
		t.Fatalf("element %q is %T, want *El", id, w)
	}
	return el
}

func TestAppRegionInheritsAndCarvesOut(t *testing.T) {
	res := appRegionDoc(t)
	for _, tc := range []struct {
		id   string
		want string
	}{
		{"strip", "drag"},
		{"fill", "drag"},        // inherited from the strip
		{"tab", "no-drag"},      // declared
		{"tab-text", "no-drag"}, // inherited from the tab, not the strip
		{"wk", "drag"},          // -webkit-app-region alias
	} {
		el := elByID(t, res, tc.id)
		if el.lastCS == nil {
			t.Fatalf("%s: no computed style", tc.id)
		}
		if got := el.lastCS.AppRegion; got != tc.want {
			t.Errorf("#%s app-region = %q, want %q", tc.id, got, tc.want)
		}
	}
}

func TestAppRegionForResolvesFromTheEventTarget(t *testing.T) {
	res := appRegionDoc(t)
	strip := elByID(t, res, "strip")

	// A press whose target is inside the no-drag tab must NOT drag the
	// window, even though the strip is the element handling it.
	if got := strip.appRegionFor(elByID(t, res, "tab-text")); got != "no-drag" {
		t.Errorf("press inside the tab resolved %q, want no-drag", got)
	}
	if got := strip.appRegionFor(elByID(t, res, "fill")); got != "drag" {
		t.Errorf("press on the strip filler resolved %q, want drag", got)
	}
	// No target (an event with nothing resolved) falls back to the strip's
	// own value rather than reporting "not a drag region".
	if got := strip.appRegionFor(nil); got != "drag" {
		t.Errorf("targetless press resolved %q, want drag", got)
	}
}

// A drag region must not swallow the presses its children need: htmlcss
// dispatches the window drag only after the capture phase, so a control in
// the strip keeps working. Test windows have no platform window, so
// BeginWindowDrag is a no-op — which is exactly the "unsupported platform"
// path, and it must leave normal click behavior intact.
func TestDragRegionStillDeliversClicksToChildren(t *testing.T) {
	res := appRegionDoc(t)
	root := res.Root
	win := qui.NewTestWindow(qui.Size{W: 400, H: 200})
	win.SetRoot(root)
	root.Measure(qui.Size{W: 400, H: 0})
	root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 200})

	clicked := 0
	tab := elByID(t, res, "tab")
	tab.SetOnClick(func() { clicked++ })

	b := tab.Bounds()
	x, y := b.X+b.W/2, b.Y+b.H/2
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, x, y, qui.MouseButtonLeft, 0))
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, x, y, qui.MouseButtonLeft, 0))
	if clicked != 1 {
		t.Errorf("tab click fired %d times, want 1", clicked)
	}
}
