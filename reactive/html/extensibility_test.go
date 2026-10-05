package html_test

import (
	"context"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
	qw "github.com/qizhanchan/qui/widgets"
)

func center(el *htmlcss.El) (float32, float32) {
	b := qui.InteractionBoundsOf(el)
	return b.X + b.W/2, b.Y + b.H/2
}

func TestPointerEventsAndPreventDefault(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	var downs, moves, ups int
	var lastLocal float32
	submitted := 0
	rt := h.Mount(win, `.pad { width: 200px; height: 100px; }`, func() h.Node {
		return h.Div(
			h.Div().Class("pad").
				OnPointerDown(func(e htmlcss.PointerEvent) bool { downs++; lastLocal = e.LocalX; return true }).
				OnPointerMove(func(e htmlcss.PointerEvent) bool { moves++; return true }).
				OnPointerUp(func(e htmlcss.PointerEvent) bool { ups++; return true }),
			h.Form(
				h.Button("Save").Class("save").OnClickEvent(func(e htmlcss.PointerEvent) { e.PreventDefault() }),
			).OnFormSubmit(func(map[string]string) { submitted++ }),
		)
	})
	layoutAll(win, rt.Root())
	pad := findClass(rt.Root(), "pad")
	x, y := center(pad)
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, x, y, qui.MouseButtonLeft, 0))
	// Captured: the move far outside the pad still reaches it.
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseMove, 390, 290, 0, 0))
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, 390, 290, qui.MouseButtonLeft, 0))
	if downs != 1 || moves < 1 || ups != 1 {
		t.Fatalf("pointer events: down=%d move=%d up=%d", downs, moves, ups)
	}
	if lastLocal < 99 || lastLocal > 101 {
		t.Fatalf("LocalX = %v, want ≈100", lastLocal)
	}
	save := findClass(rt.Root(), "save")
	x, y = center(save)
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, x, y, qui.MouseButtonLeft, 0))
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, x, y, qui.MouseButtonLeft, 0))
	if submitted != 0 {
		t.Fatal("PreventDefault should stop the submit button submitting")
	}
}

func TestRefFollowsLatestRenderAndUnmount(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	var seen []int
	var setN func(int)
	var setShow func(bool)
	var stored *htmlcss.El
	ref := &stored
	rt := h.Mount(win, ``, func() h.Node {
		n, sn := reactive.UseState(1)
		show, ss := reactive.UseState(true)
		setN, setShow = sn, ss
		if !show {
			return h.Div()
		}
		return h.Div(h.Span("x").RefTo(ref).Ref(func(el *htmlcss.El) {
			if el == nil {
				seen = append(seen, -n)
				return
			}
			seen = append(seen, n)
		}))
	})
	setN(2)
	rt.Flush()
	if stored == nil {
		t.Fatal("RefTo not set")
	}
	setShow(false)
	rt.Flush()
	if len(seen) < 3 || seen[len(seen)-2] != 2 || seen[len(seen)-1] != -2 {
		t.Fatalf("ref calls %v: want the latest closure on render and nil on unmount", seen)
	}
	if stored != nil {
		t.Fatal("RefTo should be cleared on unmount")
	}
}

func TestLayoutEffectSeesFinalBounds(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	var passive, layout float32 = -1, -1
	h.Mount(win, `.box { width: 120px; height: 40px; }`, func() h.Node {
		ref := reactive.UseRef[*htmlcss.El](nil)
		reactive.UseEffect(func() func() {
			passive = (*ref).Bounds().W
			return nil
		})
		reactive.UseLayoutEffect(func() func() {
			layout = (*ref).Bounds().W
			return nil
		})
		return h.Div(h.Div().Class("box").RefTo(ref))
	})
	if layout != -1 {
		t.Fatal("layout effect ran before layout")
	}
	win.LayoutForTest()
	if layout != 120 {
		t.Fatalf("layout effect saw width %v, want 120 (passive saw %v)", layout, passive)
	}
}

func TestUseResourceAndUseId(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	release := make(chan struct{})
	var res reactive.Resource[string]
	var ids []string
	rt := h.Mount(win, ``, func() h.Node {
		res = reactive.UseResource(func(ctx context.Context) (string, error) {
			<-release
			return "loaded", nil
		})
		ids = append(ids, reactive.UseId())
		return h.Div()
	})
	if !res.Loading || res.Ready {
		t.Fatalf("initial: %+v", res)
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for !res.Ready && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		win.DrainJobsForTest()
		rt.Flush()
	}
	if !res.Ready || res.Value != "loaded" || res.Loading || res.Err != nil {
		t.Fatalf("after load: %+v", res)
	}
	if len(ids) < 2 || ids[0] != ids[len(ids)-1] || ids[0] == "" {
		t.Fatalf("UseId not stable: %v", ids)
	}
}

func TestPopoverFollowsAndFlips(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	dismissed := 0
	css := `.trigger { margin-top: 260px; width: 80px; height: 20px; } .pop { width: 100px; height: 60px; }`
	rt := h.Mount(win, css, func() h.Node {
		anchor := reactive.UseRef[*htmlcss.El](nil)
		return h.Div(
			h.Div().Class("trigger").RefTo(anchor),
			h.PopoverAt(func() qui.Rect {
				if *anchor == nil {
					return qui.Rect{}
				}
				return qui.InteractionBoundsOf(*anchor)
			}, h.PopoverOptions{OnDismiss: func() { dismissed++ }}, h.Div().Class("pop")),
		)
	})
	layoutAll(win, rt.Root())
	host := win.Overlays()[0]
	host.Layout(qui.Rect{W: 400, H: 300})
	pop := findClass(host, "pop")
	trig := findClass(rt.Root(), "trigger")
	if pb, tb := pop.Bounds(), trig.Bounds(); pb.Y+pb.H > tb.Y+0.5 {
		t.Fatalf("popover should flip above a trigger near the bottom: pop %v trigger %v", pb, tb)
	}
	win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyEscape, 0))
	if dismissed != 1 {
		t.Fatalf("Escape dismissed %d times", dismissed)
	}
}

func TestLeafIsAStyledElement(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	rt := h.Mount(win, `.wrap > :nth-child(2) { width: 150px; height: 30px; margin-left: 10px; }`, func() h.Node {
		return h.Div(
			h.Span("a"),
			h.Leaf("x-label", "lbl", func() *qw.Label { return qw.NewLabel("native") }, nil).Class("native"),
		).Class("wrap")
	})
	layoutAll(win, rt.Root())
	el := findClass(rt.Root(), "native")
	if el == nil || el.Tag() != "x-label" {
		t.Fatalf("Leaf should mount as an element: %v", el)
	}
	if b := el.Bounds(); b.W != 150 || b.H != 30 {
		t.Fatalf(":nth-child + size didn't reach the leaf host: %v", b)
	}
	if _, ok := el.HostedWidget().(*qw.Label); !ok {
		t.Fatal("hosted widget missing")
	}
	if el.HostedWidget().Bounds().W != 150 {
		t.Fatalf("hosted widget should fill its element: %v", el.HostedWidget().Bounds())
	}
}

func TestCanvasDrawSeesComputedStyle(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	var got qui.Color
	var v string
	rt := h.Mount(win, `.c { color: #ff0000; --series: #00ff00; width: 50px; height: 20px; }`, func() h.Node {
		return h.Div(h.Canvas(func(cv qui.Canvas, ctx htmlcss.CanvasContext) {
			got = ctx.Color
			v = ctx.Style.Var("series")
		}).Class("c"))
	})
	layoutAll(win, rt.Root())
	rt.Root().Draw(&qui.RecordingCanvas{})
	if got != (qui.Color{R: 1, A: 1}) || v != "#00ff00" {
		t.Fatalf("canvas ctx color %v var %q", got, v)
	}
}
