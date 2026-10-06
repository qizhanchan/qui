package html_test

// Regression tests for the app-dev surface (h DSL + htmlcss) driven the
// way an app drives it: h.Mount, real dispatched input, public API only.

import (
	"image"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
	qw "github.com/qizhanchan/qui/widgets"
)

func press(win *qui.Window, x, y float32) {
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, x, y, qui.MouseButtonLeft, 0))
}

func release(win *qui.Window, x, y float32) {
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, x, y, qui.MouseButtonLeft, 0))
}

// A press dragged off an element and released elsewhere is a cancelled
// click (pointerup still fires); released back on it, it clicks; and an
// ancestor containing both ends of the gesture gets the click (DOM: click
// on the common ancestor).
func TestDragOffElementCancelsClick(t *testing.T) {
	for _, kind := range []string{"div", "button"} {
		t.Run(kind, func(t *testing.T) {
			win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
			clicks, rowClicks, ups := 0, 0, 0
			rt := h.Mount(win, `.row { width: 300px; height: 50px; display: flex; } .pad { width: 100px; height: 50px; }`, func() h.Node {
				b := h.Div()
				if kind == "button" {
					b = h.Button("Delete")
				}
				return h.Div(h.Div(
					b.Class("pad").OnClick(func() { clicks++ }).
						OnPointerUp(func(htmlcss.PointerEvent) bool { ups++; return false }),
				).Class("row").OnClick(func() { rowClicks++ }))
			})
			win.LayoutForTest()
			x, y := center(findClass(rt.Root(), "pad"))

			press(win, x, y)
			win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseMove, 350, 250, 0, 0))
			release(win, 350, 250) // outside the row too
			if clicks != 0 || rowClicks != 0 || ups != 1 {
				t.Fatalf("released outside: click=%d row=%d pointerup=%d, want 0/0/1", clicks, rowClicks, ups)
			}
			press(win, x, y)
			release(win, x+200, y) // off the pad, still on the row
			if clicks != 0 || rowClicks != 1 {
				t.Fatalf("released on the row: click=%d row=%d, want 0/1", clicks, rowClicks)
			}
			press(win, x, y)
			release(win, x, y)
			if clicks != 1 {
				t.Fatalf("released on the element: click=%d, want 1", clicks)
			}
		})
	}
}

// columnHas paints the tree and reports whether the vertical line
// through el's center has any pixel of each wanted color.
func columnHas(rt *reactive.Runtime, el *htmlcss.El, want ...[3]uint8) []bool {
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	rt.Root().Draw(qui.NewImageCanvas(img))
	b := qui.InteractionBoundsOf(el)
	got := make([]bool, len(want))
	for y := int(b.Y); y < int(b.Y+b.H); y++ {
		c := img.RGBAAt(int(b.X+b.W/2), y)
		for i, w := range want {
			if c.R == w[0] && c.G == w[1] && c.B == w[2] {
				got[i] = true
			}
		}
	}
	return got
}

// `input:focus { border-color; background }` paints on the focused field —
// focus lives on the backing control, not the element — while a field the
// rule doesn't match keeps the native focus ring. Same for <textarea>.
func TestFocusedControlTakesAuthorFocusStyle(t *testing.T) {
	red, green, blue := [3]uint8{255, 0, 0}, [3]uint8{0, 255, 0}, [3]uint8{0, 0, 255}
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	css := `input, textarea { width: 200px; height: 40px; border: 2px solid #0000ff; }
		.hot:focus { border-color: #ff0000; background: #00ff00; }`
	rt := h.Mount(win, css, func() h.Node {
		return h.Div(h.Input().Class("hot a"), h.Input().Class("b"), h.Textarea().Class("hot c"))
	})
	win.LayoutForTest()
	for _, cls := range []string{"a", "c"} {
		el := findClass(rt.Root(), cls)
		if got := columnHas(rt, el, red, green, blue); got[0] || got[1] || !got[2] {
			t.Fatalf(".%s unfocused: red=%v green=%v blue=%v, want the resting blue border only", cls, got[0], got[1], got[2])
		}
		x, y := center(el)
		press(win, x, y)
		release(win, x, y)
		if got := columnHas(rt, el, red, green); !got[0] || !got[1] {
			t.Fatalf(".%s focused: red border=%v green background=%v, want both", cls, got[0], got[1])
		}
	}
	b := findClass(rt.Root(), "b")
	x, y := center(b)
	press(win, x, y)
	release(win, x, y)
	if got := columnHas(rt, b, red, green); got[0] || got[1] {
		t.Fatalf("an input the :focus rule doesn't match took it: red=%v green=%v", got[0], got[1])
	}
}

type gauge struct{ *qw.Label }

func init() {
	htmlcss.RegisterElement("x-regress-gauge", htmlcss.ElementDef{
		Create: func(*htmlcss.El) qui.Widget {
			g := &gauge{Label: qw.NewLabel("g")}
			g.SetSelf(g)
			return g
		},
	})
}

var gaugeTpl = h.MustParse(`<x-regress-gauge></x-regress-gauge>`)

// The first pass of h.Mount is styled before its effects run: an effect
// on initial mount sees the computed style and a custom element's hosted
// widget, exactly like one mounted later.
func TestMountEffectsSeeStyledTree(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	var styled, hosted bool
	h.Mount(win, `.x { --k: 1; }`, func() h.Node {
		ref := reactive.UseRef[*htmlcss.El](nil)
		reactive.UseEffect(func() func() {
			styled = (*ref).ComputedStyle() != nil && (*ref).ComputedStyle().Var("k") == "1"
			g := findEl(*ref, "x-regress-gauge")
			hosted = g != nil && g.HostedWidget() != nil
			return nil
		}, 0)
		return h.Div(gaugeTpl.Bind(h.Scope{})).Class("x").RefTo(ref)
	})
	if !styled || !hosted {
		t.Fatalf("initial-mount effect: styled=%v hosted widget=%v, want both", styled, hosted)
	}
}

// CSS-wide keywords work on registered properties as on built-in ones.
func TestRegisteredPropertyCSSWideKeywords(t *testing.T) {
	htmlcss.RegisterProperty("regress-gap", htmlcss.PropertyDef{Initial: "4"})
	htmlcss.RegisterProperty("regress-tone", htmlcss.PropertyDef{Inherited: true, Initial: "plain"})
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	css := `.p { regress-gap: 12; regress-tone: loud; }
		.inherit { regress-gap: inherit; }
		.initial { regress-gap: 9; regress-tone: initial; }
		.initial { regress-gap: initial; }
		.unset { regress-gap: 7; regress-tone: quiet; }
		.unset { regress-gap: unset; regress-tone: unset; }`
	rt := h.Mount(win, css, func() h.Node {
		return h.Div(h.Div(
			h.Div().Class("inherit"), h.Div().Class("initial"), h.Div().Class("unset"),
		).Class("p"))
	})
	for _, c := range []struct{ cls, prop, want string }{
		{"inherit", "regress-gap", "12"},
		{"initial", "regress-gap", "4"},
		{"initial", "regress-tone", "plain"},
		{"unset", "regress-gap", "4"},     // not inherited → initial
		{"unset", "regress-tone", "loud"}, // inherited → parent's
	} {
		if got := findClass(rt.Root(), c.cls).ComputedStyle().Property(c.prop); got != c.want {
			t.Errorf(".%s %s = %q, want %q", c.cls, c.prop, got, c.want)
		}
	}
}

// A link folded into a paragraph's text reaches the link handler as its
// own <a> element, so two links with the same href are told apart by
// their attributes.
func TestLinkHandlerGetsFoldedAnchor(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	var routes []string
	rt := h.Mount(win, `p { width: 380px; }`, func() h.Node {
		return h.Div(h.P(
			h.A("first").Href("#/x").Attr("data-route", "one"), " and ",
			h.A("second").Href("#/x").Attr("data-route", "two"),
		))
	})
	h.Engine(rt).SetLinkHandler(func(href string, from *htmlcss.El) bool {
		r, _ := from.Attr("data-route")
		routes = append(routes, from.Tag()+":"+r)
		return true
	})
	win.LayoutForTest()
	p := findEl(rt.Root(), "p")
	b := qui.InteractionBoundsOf(p)
	for x := b.X + 1; x < b.X+b.W; x += 3 {
		press(win, x, b.Y+b.H/2)
		release(win, x, b.Y+b.H/2)
	}
	seen := map[string]bool{}
	for _, r := range routes {
		seen[r] = true
	}
	if !seen["a:one"] || !seen["a:two"] || len(seen) != 2 {
		t.Fatalf("link handler saw %v, want a:one and a:two only", seen)
	}
}

// :focus-visible follows the input modality: autofocus on a fresh window
// (no pointer yet) shows it; focus moved by a click handler doesn't; after
// a key press, programmatic focus shows it again.
func TestFocusVisibleFollowsModality(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	var ref **htmlcss.El
	rt := h.Mount(win, `button { width: 80px; height: 30px; } button:focus-visible { background: #ff0000; }`, func() h.Node {
		r := reactive.UseRef[*htmlcss.El](nil)
		ref = r
		return h.Div(
			h.Button("auto").Class("auto").Autofocus(),
			h.Button("go").Class("go").OnClick(func() { (*r).RequestFocus() }),
			h.Button("t").Class("t").RefTo(r),
		)
	})
	win.LayoutForTest()
	win.DrainJobsForTest()
	auto := findClass(rt.Root(), "auto")
	if win.Focused() != qui.Widget(auto) || !auto.FocusVisibleNow() {
		t.Fatalf("autofocus with no pointer interaction: focused=%v visible=%v", win.Focused() == qui.Widget(auto), auto.FocusVisibleNow())
	}
	x, y := center(findClass(rt.Root(), "go"))
	press(win, x, y)
	release(win, x, y)
	tgt := *ref
	if win.Focused() != qui.Widget(tgt) || tgt.FocusVisibleNow() {
		t.Fatalf("focus moved from a click handler: focused=%v visible=%v, want focused and not visible",
			win.Focused() == qui.Widget(tgt), tgt.FocusVisibleNow())
	}
	win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyLeft, 0))
	auto.RequestFocus()
	if !auto.FocusVisibleNow() {
		t.Fatal("programmatic focus after keyboard use is not focus-visible")
	}
}

var langSelectTpl = h.MustParse(`<select class="lang"><option value="fr">French</option><option value="de" disabled>German</option></select>`)

// The DOM view of a <select> carries each option's submitted value.
func TestInspectSelectOptionValues(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	h.Mount(win, ``, func() h.Node { return h.Div(langSelectTpl.Bind(h.Scope{})) })
	win.LayoutForTest()
	var opts []qui.AXOption
	var walk func(n *htmlcss.DOMNode)
	walk = func(n *htmlcss.DOMNode) {
		if n.Tag == "select" {
			opts = n.Options
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(htmlcss.InspectWindow(win).Root)
	if len(opts) != 2 || opts[0].Value != "fr" || opts[1].Value != "de" || !opts[1].Disabled {
		t.Fatalf("select options in the DOM view = %+v", opts)
	}
}
