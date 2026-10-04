package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// A <button> with no author-supplied :hover must still show the native
// hover feedback it had while it was backed by widgets.Button — a plain
// styled Box dropped it in the el.go rewrite. Regression guard: a
// transparent toolbar-style button darkens under the cursor, and a
// real mouse-move through window dispatch triggers the repaint.
func TestButtonDefaultHover(t *testing.T) {
	body := Render(
		`<body><div class="bar"><button class="btn">Wrap</button></div></body>`,
		`.bar{display:flex;padding:4px;background:#edf2fa;}`+
			`.btn{background:transparent;padding:4px 7px;}`,
		Options{},
	)

	var btn *El
	var walk func(w qui.Widget)
	walk = func(w qui.Widget) {
		if e, ok := w.(*El); ok && e.tag == "button" {
			btn = e
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, ch := range c.ChildList() {
				walk(ch)
			}
		}
	}
	walk(body)
	if btn == nil {
		t.Fatal("no <button> El produced")
	}

	// Default hover synthesized even without an author :hover rule.
	if btn.Hover == nil {
		t.Fatal("button got no default hover style")
	}
	if btn.Hover.Background.A == 0 {
		t.Errorf("default hover background is fully transparent: %+v", btn.Hover.Background)
	}
	// Must not leak into the resting style.
	if btn.Style().Background.A > 0 {
		t.Errorf("resting background = %+v, want transparent", btn.Style().Background)
	}

	// Drive a real hover through the window dispatch and confirm it both
	// marks the button hovered and requests a repaint.
	win := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	win.SetRoot(body)
	body.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 100})
	win.ClearDirtyRegion()

	b := btn.Bounds()
	if b.W <= 0 || b.H <= 0 {
		t.Fatalf("button not laid out: %+v", b)
	}

	// Resting paint: count background fills (rect + rounded-rect; the UA
	// button default carries border-radius, so the fill is rounded).
	var rest qui.RecordingCanvas
	btn.Draw(&rest)
	restFills := len(rest.Fills) + len(rest.Rounds)

	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseMove, b.X+b.W/2, b.Y+b.H/2, 0, 0))
	if !btn.Hovering() {
		t.Error("button not hovered after mouse move over it")
	}
	if win.DirtyRegion().IsEmpty() {
		t.Error("no repaint requested on hover enter")
	}

	var hov qui.RecordingCanvas
	btn.Draw(&hov)
	hovFills := len(hov.Fills) + len(hov.Rounds)
	if hovFills <= restFills {
		t.Errorf("hover added no highlight fill (rest=%d hover=%d)", restFills, hovFills)
	}
}
