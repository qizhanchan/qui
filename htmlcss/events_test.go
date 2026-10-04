package htmlcss

import (
	"testing"
	"time"

	"github.com/qizhanchan/qui"
)

// Pointer transitions fire once per crossing, on the element the pointer is
// actually over — and the enclosing element does NOT get a leave when the
// pointer moves onto its own child.
func TestMouseEnterLeave(t *testing.T) {
	res := RenderDoc(
		`<body><div id="card">hover me</div><div id="other">elsewhere</div></body>`,
		`#card { width: 100px; height: 40px; } #other { width: 100px; height: 40px; }`,
		Options{},
	)
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	win.SetRoot(res.Root)
	res.Root.Layout(qui.Rect{W: 400, H: 300})

	card := res.ByID["card"].(*El)
	var enters, leaves int
	card.SetOnMouseEnter(func() { enters++ })
	card.SetOnMouseLeave(func() { leaves++ })

	b := card.Bounds()
	move := func(x, y float32) {
		win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseMove, x, y, qui.MouseButtonLeft, 0))
	}
	move(b.X+5, b.Y+5)
	if enters != 1 || leaves != 0 {
		t.Fatalf("after entering: enters=%d leaves=%d, want 1/0", enters, leaves)
	}
	move(b.X+9, b.Y+9) // still inside → no repeat
	if enters != 1 {
		t.Errorf("enter fired again while still inside: %d", enters)
	}
	ob := res.ByID["other"].(*El).Bounds()
	move(ob.X+5, ob.Y+5)
	if leaves != 1 {
		t.Errorf("leave did not fire when the pointer left: %d", leaves)
	}
}

// A second release close in time and space is a double click; the single
// click handler still runs for both (DOM behavior).
func TestDoubleClick(t *testing.T) {
	res := RenderDoc(
		`<body><div id="d">dbl</div></body>`,
		`#d { width: 100px; height: 40px; }`,
		Options{},
	)
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	win.SetRoot(res.Root)
	res.Root.Layout(qui.Rect{W: 400, H: 300})

	d := res.ByID["d"].(*El)
	var clicks, dbl int
	var order []string
	d.SetOnClick(func() { clicks++; order = append(order, "click") })
	d.SetOnDoubleClick(func() { dbl++; order = append(order, "dblclick") })

	b := d.Bounds()
	x, y := b.X+5, b.Y+5
	release := func(at time.Time, x, y float32) {
		ev := qui.NewMouseEvent(qui.EventMouseUp, x, y, qui.MouseButtonLeft, 0)
		ev.When = at
		win.DispatchTestEvent(ev)
	}
	t0 := time.Unix(1000, 0)
	release(t0, x, y)
	release(t0.Add(150*time.Millisecond), x, y)
	if dbl != 1 {
		t.Errorf("double click did not fire: %d", dbl)
	}
	if clicks != 2 {
		t.Errorf("single clicks = %d, want 2 (both releases)", clicks)
	}
	// DOM order: click, click, dblclick — the double-click handler runs last.
	want := []string{"click", "click", "dblclick"}
	if len(order) != 3 || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] {
		t.Errorf("dispatch order = %v, want %v", order, want)
	}
	// A third click starts a new gesture rather than reading as another
	// double click.
	release(t0.Add(300*time.Millisecond), x, y)
	if dbl != 1 {
		t.Errorf("third click produced another double click: %d", dbl)
	}
	// Too slow → not a double click.
	release(t0.Add(2*time.Second), x, y)
	release(t0.Add(4*time.Second), x, y)
	if dbl != 1 {
		t.Errorf("releases far apart counted as a double click: %d", dbl)
	}
	// Too far apart in space → not a double click.
	release(t0.Add(10*time.Second), x, y)
	release(t0.Add(10*time.Second+50*time.Millisecond), x+40, y)
	if dbl != 1 {
		t.Errorf("releases far apart in space counted as a double click: %d", dbl)
	}
}

// Wheel handlers see the deltas, and consume the event only when they say so.
func TestWheelHandler(t *testing.T) {
	res := RenderDoc(
		`<body><div id="d">scroll</div></body>`,
		`#d { width: 100px; height: 40px; }`,
		Options{},
	)
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	win.SetRoot(res.Root)
	res.Root.Layout(qui.Rect{W: 400, H: 300})

	d := res.ByID["d"].(*El)
	var gotDX, gotDY float32
	consume := false
	d.SetOnWheel(func(dx, dy float32) bool { gotDX, gotDY = dx, dy; return consume })

	b := d.Bounds()
	ev := qui.NewScrollEvent(b.X+5, b.Y+5, 3, -7, 0)
	if handled := d.Handle(ev); handled {
		t.Error("wheel handler returning false must not consume the event")
	}
	if gotDX != 3 || gotDY != -7 {
		t.Errorf("deltas = (%v,%v), want (3,-7)", gotDX, gotDY)
	}
	consume = true
	if handled := d.Handle(qui.NewScrollEvent(b.X+5, b.Y+5, 1, 1, 0)); !handled {
		t.Error("wheel handler returning true must consume the event")
	}
}

// A key handler makes the element focusable and receives keys; returning
// true consumes them.
func TestKeyHandlerAndFocusable(t *testing.T) {
	res := RenderDoc(`<body><div id="d">keys</div></body>`, `#d { width: 80px; height: 30px; }`, Options{})
	d := res.ByID["d"].(*El)

	if d.Focusable() {
		t.Error("a plain div should not be focusable")
	}
	var seen []qui.Key
	d.SetOnKeyDown(func(ke qui.KeyEvent) bool {
		seen = append(seen, ke.Key)
		return ke.Key == qui.KeyEscape
	})
	if !d.Focusable() {
		t.Error("an element with a key handler must be focusable")
	}
	if handled := d.Handle(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyEnter, 0)); handled {
		t.Error("handler returning false must not consume the key")
	}
	if handled := d.Handle(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyEscape, 0)); !handled {
		t.Error("handler returning true must consume the key")
	}
	if len(seen) != 2 || seen[0] != qui.KeyEnter || seen[1] != qui.KeyEscape {
		t.Errorf("keys seen = %v", seen)
	}
}

// Focus / blur callbacks fire on the transition, once each.
func TestFocusBlurCallbacks(t *testing.T) {
	res := RenderDoc(`<body><div id="d">f</div></body>`, `#d:focus { border: 1px solid #000; }`, Options{})
	d := res.ByID["d"].(*El)
	var focus, blur int
	d.SetOnFocus(func() { focus++ })
	d.SetOnBlur(func() { blur++ })

	d.SetFocused(true)
	d.SetFocused(true) // no transition → no second call
	if focus != 1 {
		t.Errorf("focus fired %d times, want 1", focus)
	}
	d.SetFocused(false)
	if blur != 1 {
		t.Errorf("blur fired %d times, want 1", blur)
	}
}

// A disabled element swallows keys without invoking the handler, mirroring
// how it already swallows clicks.
func TestDisabledElementSwallowsKeys(t *testing.T) {
	res := RenderDoc(`<body><div id="d">x</div></body>`, ``, Options{})
	d := res.ByID["d"].(*El)
	fired := false
	d.SetOnKeyDown(func(qui.KeyEvent) bool { fired = true; return true })
	d.SetDisabled(true)
	d.Handle(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyEnter, 0))
	if fired {
		t.Error("key handler ran on a disabled element")
	}
}
