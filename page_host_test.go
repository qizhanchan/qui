package qui

import (
	"image/color"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// pageTestBox fills its bounds and flips colour on every click.
type pageTestBox struct {
	BaseWidget
	on     bool
	clicks atomic.Int32
	keys   atomic.Int32
}

func newPageTestBox() *pageTestBox {
	b := &pageTestBox{BaseWidget: NewBaseWidget()}
	b.SetSelf(b)
	return b
}

var (
	pageRed  = Color{R: 1, A: 1}
	pageBlue = Color{B: 1, A: 1}
)

func (b *pageTestBox) Measure(avail Size) Size { return avail }
func (b *pageTestBox) Focusable() bool         { return true }
func (b *pageTestBox) SetFocused(bool)         {}
func (b *pageTestBox) Draw(c Canvas) {
	col := pageRed
	if b.on {
		col = pageBlue
	}
	c.FillRect(b.Bounds(), col)
}
func (b *pageTestBox) Handle(ev Event) bool {
	switch e := ev.(type) {
	case MouseEvent:
		if e.Type() == EventMouseUp {
			b.on = !b.on
			b.clicks.Add(1)
			b.Invalidate()
			return true
		}
		return e.Type() == EventMouseDown
	case KeyEvent:
		if e.Type() == EventKeyDown {
			b.keys.Add(1)
		}
		return true
	}
	return false
}

func pixelAt(t *testing.T, px color.RGBA, wantR, wantB uint8, what string) {
	t.Helper()
	if px.R != wantR || px.B != wantB || px.A != 255 {
		t.Fatalf("%s: pixel = %v, want R=%d B=%d", what, px, wantR, wantB)
	}
}

func TestPageHostSyncRendersAndTakesInput(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	th := NewSyncPageThread("test")
	defer th.Close()
	box := newPageTestBox()
	h := th.NewPageHost(PageHostConfig{Width: 120, Height: 80, Scale: 2, Setup: func(w *Window) {
		w.SetRoot(box)
	}})
	th.RunUntilIdle(20)
	img := h.Snapshot()
	if img == nil {
		t.Fatal("no frame committed")
	}
	if got := img.Bounds().Size(); got.X != 240 || got.Y != 160 {
		t.Fatalf("frame size = %v, want 240x160 (scale 2)", got)
	}
	pixelAt(t, img.RGBAAt(100, 100), 255, 0, "first frame")

	u, ok := h.TakeFrame(nil)
	front := u.Image
	if !ok || u.Changed != front.Bounds() {
		t.Fatalf("first TakeFrame: ok=%v changed=%v", ok, u.Changed)
	}
	if _, ok := h.TakeFrame(front); ok {
		t.Fatal("TakeFrame with nothing new reported a frame")
	}

	h.SendMouseButton(MouseButtonLeft, true, 10, 10, 0)
	h.SendMouseButton(MouseButtonLeft, false, 10, 10, 0)
	th.RunUntilIdle(20)
	if box.clicks.Load() != 1 {
		t.Fatalf("clicks = %d, want 1", box.clicks.Load())
	}
	u, ok = h.TakeFrame(front)
	front = u.Image
	if !ok {
		t.Fatal("click did not commit a frame")
	}
	pixelAt(t, front.RGBAAt(100, 100), 0, 255, "after click")

	h.SetViewport(60, 40, 1)
	th.RunUntilIdle(20)
	if got := h.Snapshot().Bounds().Size(); got.X != 60 || got.Y != 40 {
		t.Fatalf("after SetViewport frame size = %v, want 60x40", got)
	}
}

// TestPageHostDoesNotBlockHost checks the contract the whole design rests
// on: while the page thread is stuck, every host-side call returns at once.
func TestPageHostDoesNotBlockHost(t *testing.T) {
	th := NewPageThread("test")
	defer th.Close()
	frames := make(chan struct{}, 16)
	box := newPageTestBox()
	h := th.NewPageHost(PageHostConfig{Width: 100, Height: 100, Setup: func(w *Window) { w.SetRoot(box) }})
	h.OnFrame(func() {
		select {
		case frames <- struct{}{}:
		default:
		}
	})
	select {
	case <-frames:
	case <-time.After(5 * time.Second):
		t.Fatal("no first frame")
	}

	release := make(chan struct{})
	started := make(chan struct{})
	h.Do(func(*Window) {
		close(started)
		<-release // a page stuck in a long script
	})
	<-started
	t0 := time.Now()
	h.SetViewport(80, 80, 1)
	h.SendMouseMove(5, 5, 0)
	h.SendMouseButton(MouseButtonLeft, true, 5, 5, 0)
	h.SendMouseButton(MouseButtonLeft, false, 5, 5, 0)
	h.SendKey(KeyA, 0, true, false, 0)
	_, _ = h.TakeFrame(nil)
	if busy, since := th.Busy(); !busy || time.Since(since) <= 0 {
		t.Fatalf("Busy() = %v, %v while the page is stuck", busy, since)
	}
	if d := time.Since(t0); d > 50*time.Millisecond {
		t.Fatalf("host calls took %v while the page thread was busy", d)
	}
	close(release)

	deadline := time.Now().Add(5 * time.Second)
	for box.clicks.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if box.clicks.Load() != 1 {
		t.Fatalf("queued click not delivered after the page recovered (clicks=%d)", box.clicks.Load())
	}
}

func TestPageHostPanicStopsOnlyThatPage(t *testing.T) {
	th := NewPageThread("test")
	defer th.Close()
	crashed := make(chan struct{})
	bad := th.NewPageHost(PageHostConfig{Width: 50, Height: 50, Setup: func(w *Window) {
		w.SetRoot(newPageTestBox())
	}})
	bad.OnCrash(func() { close(crashed) })
	good := newPageTestBox()
	ok := th.NewPageHost(PageHostConfig{Width: 50, Height: 50, Setup: func(w *Window) { w.SetRoot(good) }})
	bad.Do(func(w *Window) {
		w.BeforeLayout(func() { panic("page bug") })
	})
	select {
	case <-crashed:
	case <-time.After(5 * time.Second):
		t.Fatal("panic in a page frame was not reported")
	}
	if !bad.Crashed() {
		t.Fatal("Crashed() = false")
	}
	ok.SendMouseButton(MouseButtonLeft, true, 5, 5, 0)
	ok.SendMouseButton(MouseButtonLeft, false, 5, 5, 0)
	deadline := time.Now().Add(5 * time.Second)
	for good.clicks.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if good.clicks.Load() != 1 {
		t.Fatal("the other page on the thread stopped too")
	}
}

// TestPageViewShowsAndForwards runs a page on its own thread inside a
// headless host window: the view shows the committed frame at its offset
// and forwards clicks.
func TestPageViewShowsAndForwards(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	host, err := NewHeadlessWindowForTest(200, 150)
	if err != nil {
		t.Skip(err)
	}
	defer host.Destroy()
	th := NewPageThread("test")
	defer th.Close()
	box := newPageTestBox()
	h := th.NewPageHost(PageHostConfig{Width: 1, Height: 1, Setup: func(w *Window) { w.SetRoot(box) }})
	view := NewPageView(h)
	pad := &pagePad{Container: NewContainer(nil, view), child: view}
	pad.SetSelf(pad)
	host.SetRoot(pad)

	waitHost := func(cond func() bool, what string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			host.Step()
			time.Sleep(2 * time.Millisecond)
		}
		host.Step()
	}
	waitHost(func() bool {
		w, ht, _ := h.Viewport()
		return w == 160 && ht == 110 && view.FrameSeq() > 0 && view.front != nil && view.front.Bounds().Dx() == 160
	}, "page frame at the view's size")
	host.Invalidate()
	host.Step()
	img := host.Snapshot()
	if img == nil {
		t.Fatal("host has no snapshot")
	}
	s := img.Bounds().Dx() / 200
	pixelAt(t, img.RGBAAt(5*s, 5*s), 255, 255, "host background outside the view")
	pixelAt(t, img.RGBAAt(30*s, 30*s), 255, 0, "page pixel inside the view")

	seq := view.FrameSeq()
	host.DispatchTestEvent(NewMouseEvent(EventMouseDown, 50, 50, MouseButtonLeft, 0))
	host.DispatchTestEvent(NewMouseEvent(EventMouseUp, 50, 50, MouseButtonLeft, 0))
	waitHost(func() bool { return box.clicks.Load() == 1 && view.FrameSeq() > seq }, "forwarded click")
	img = host.Snapshot()
	pixelAt(t, img.RGBAAt(30*s, 30*s), 0, 255, "page pixel after click")
}

// pagePad insets its one child by 20 points.
type pagePad struct {
	*Container
	child Widget
}

func (p *pagePad) Measure(avail Size) Size { return avail }
func (p *pagePad) Layout(r Rect) {
	p.BaseWidget.Layout(r)
	p.ClearLayoutDirty()
	p.child.Layout(Rect{X: r.X + 20, Y: r.Y + 20, W: r.W - 40, H: r.H - 40})
}
func (p *pagePad) Draw(c Canvas) {
	c.FillRect(p.Bounds(), ColorWhite)
	p.child.Draw(c)
}

// pageIMEBox is a focusable IME client recording what reaches it.
type pageIMEBox struct {
	pageTestBox
	preedit atomic.Value
	commit  atomic.Value
}

func newPageIMEBox() *pageIMEBox {
	b := &pageIMEBox{pageTestBox: pageTestBox{BaseWidget: NewBaseWidget()}}
	b.SetSelf(b)
	return b
}
func (b *pageIMEBox) SetPreedit(text string, _ int) { b.preedit.Store(text) }
func (b *pageIMEBox) CommitIME(text string)         { b.commit.Store(text) }
func (b *pageIMEBox) CaretRect() Rect               { return Rect{X: 30, Y: 40, W: 2, H: 16} }

// TestPageViewIMEAndFileDrop: the view stands in for the page's IME client
// (state and caret from the commit) and hands OS file drops to the page.
func TestPageViewIMEAndFileDrop(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	host, err := NewHeadlessWindowForTest(200, 150)
	if err != nil {
		t.Skip(err)
	}
	defer host.Destroy()
	th := NewPageThread("test")
	defer th.Close()
	box := newPageIMEBox()
	dropped := make(chan []string, 1)
	var dropAt atomic.Value
	h := th.NewPageHost(PageHostConfig{Width: 1, Height: 1, Setup: func(w *Window) {
		w.SetRoot(box)
		w.SetOnFileDrop(func(paths []string, x, y float32) {
			dropAt.Store(Point{X: x, Y: y})
			dropped <- paths
		})
	}})
	view := NewPageView(h)
	pad := &pagePad{Container: NewContainer(nil, view), child: view}
	pad.SetSelf(pad)
	host.SetRoot(pad)
	wait := func(cond func() bool, what string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			host.Step()
			time.Sleep(2 * time.Millisecond)
		}
	}
	wait(func() bool { return view.FrameSeq() > 0 }, "first frame")
	if view.IMEActive() {
		t.Fatal("IME active before anything is focused")
	}
	// A click focuses the view on the host and the box in the page.
	host.DispatchTestEvent(NewMouseEvent(EventMouseDown, 50, 50, MouseButtonLeft, 0))
	host.DispatchTestEvent(NewMouseEvent(EventMouseUp, 50, 50, MouseButtonLeft, 0))
	h.Do(func(w *Window) { w.SetFocus(box) })
	wait(func() bool { return view.IMEActive() }, "the page to report an active IME client")
	if got, want := host.CaretRect(), (Rect{X: 50, Y: 60, W: 2, H: 16}); got != want {
		t.Fatalf("host caret rect = %v, want %v (page caret + view offset)", got, want)
	}
	host.SetPreedit("ni", 2)
	host.CommitIME("你")
	wait(func() bool { s, _ := box.commit.Load().(string); return s == "你" }, "the IME commit to reach the page")
	if s, _ := box.preedit.Load().(string); s != "ni" {
		t.Fatalf("preedit = %q, want ni", s)
	}

	host.onFileDrop([]string{"/tmp/a.txt"}, 70, 80)
	select {
	case paths := <-dropped:
		if len(paths) != 1 || paths[0] != "/tmp/a.txt" {
			t.Fatalf("dropped %v", paths)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("file drop did not reach the page")
	}
	if p := dropAt.Load().(Point); p != (Point{X: 50, Y: 60}) {
		t.Fatalf("drop at %v, want page-local (50, 60)", p)
	}
}
