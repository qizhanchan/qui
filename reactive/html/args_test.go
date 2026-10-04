package html_test

import (
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	h "github.com/qizhanchan/qui/reactive/html"
)

// mount renders a tree into a test window and returns the window plus the
// root element. Structural assertions go through htmlcss.InspectWindow —
// the projected DOM — rather than ChildList, because inline children fold
// into a shared InlineBox and the projection is what the markup (and an
// agent reading the tree) actually describes.
func mount(t *testing.T, build func() h.Node) (*qui.Window, *htmlcss.El) {
	t.Helper()
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	rt := h.Mount(win, ``, build)
	root, ok := rt.Root().(*htmlcss.El)
	if !ok {
		t.Fatalf("root is %T, want *htmlcss.El", rt.Root())
	}
	win.SetRoot(root)
	return win, root
}

// childTags lists the tags of the root element's projected children.
func childTags(t *testing.T, win *qui.Window) []string {
	t.Helper()
	doc := htmlcss.InspectWindow(win).Root
	// The projection roots at a synthetic document whose only child is the
	// mounted element.
	if len(doc.Children) != 1 {
		t.Fatalf("document has %d children, want 1", len(doc.Children))
	}
	out := []string{}
	for _, k := range doc.Children[0].Children {
		out = append(out, k.Tag)
	}
	return out
}

// A tag whose content is only text keeps that text as the element's OWN
// text content — one widget, no anonymous segments.
func TestTextOnlyContentCollapses(t *testing.T) {
	win, root := mount(t, func() h.Node { return h.Span("hello ", "world") })
	if got := root.AccessibleName(); got != "hello world" {
		t.Fatalf("span text = %q, want %q", got, "hello world")
	}
	if tags := childTags(t, win); len(tags) != 0 {
		t.Fatalf("text-only span has children %v, want none", tags)
	}
}

// Text mixed with element children lowers to anonymous #text segments, so
// `<p>Hello <b>world</b>!</p>` renders as one inline run.
func TestMixedTextAndElements(t *testing.T) {
	var b *htmlcss.El
	_, root := mount(t, func() h.Node {
		return h.P("Hello ", h.B("world").Ref(func(e *htmlcss.El) { b = e }), "!")
	})
	// Folded into ONE inline run: the two text segments and the <b> share a
	// line, so the paragraph reads as a single string...
	if got := root.AccessibleName(); got != "Hello world!" {
		t.Fatalf("paragraph text = %q, want %q", got, "Hello world!")
	}
	// ...while the <b> is still a real element that CSS can target.
	if b == nil || b.Tag() != "b" {
		t.Fatalf("inline child element = %v, want a live <b>", b)
	}
}

// Numbers are formatted as text — h.Span("count: ", n) is the natural way
// to write it and must not need a Sprintf.
func TestNumbersBecomeText(t *testing.T) {
	_, root := mount(t, func() h.Node { return h.Span("count: ", 42) })
	if got := root.AccessibleName(); got != "count: 42" {
		t.Fatalf("span text = %q", got)
	}
}

// A []Node splices in place, so a caller can build children in a loop and
// pass the slice with no spread and no wrapper.
func TestSliceSplices(t *testing.T) {
	win, _ := mount(t, func() h.Node {
		var rows []h.Node
		for _, s := range []string{"a", "b", "c"} {
			rows = append(rows, h.Li(s))
		}
		return h.Ul(rows)
	})
	if got := strings.Join(childTags(t, win), ","); got != "li,li,li" {
		t.Fatalf("children = %v, want three li", got)
	}
}

// The content-less elements read a bare string as their own payload
// rather than as text.
func TestStringPayloadPerTag(t *testing.T) {
	var img, in, sel *htmlcss.El
	mount(t, func() h.Node {
		return h.Div(
			h.Img("logo.png").Ref(func(e *htmlcss.El) { img = e }),
			h.Input("typed").Ref(func(e *htmlcss.El) { in = e }),
			h.Select("Low", "High").Ref(func(e *htmlcss.El) { sel = e }),
		)
	})
	if got, _ := img.Attr("src"); got != "logo.png" {
		t.Fatalf("img src = %q, want the string argument", got)
	}
	if got := in.InputValue(); got != "typed" {
		t.Fatalf("input value = %q, want the string argument", got)
	}
	if got := sel.SelectOptions(); len(got) != 2 || got[0] != "Low" || got[1] != "High" {
		t.Fatalf("select options = %v, want [Low High]", got)
	}
}

// reactive.Element satisfies Node, so a component sits among builder
// children with no h.El wrapper and no trailing .Build().
func TestComponentNeedsNoWrapper(t *testing.T) {
	badge := func(text string) h.Node {
		return h.Component("Badge", text, text, func(s string) h.Node {
			return h.Span(s).Class("badge")
		})
	}
	_, root := mount(t, func() h.Node { return h.Div(badge("new")) })
	if got := root.AccessibleName(); !strings.Contains(got, "new") {
		t.Fatalf("tree text = %q, want it to contain the component's text", got)
	}
}

// Disabled(false) REMOVES the attribute, so a conditionally-enabled
// control works when written the obvious way.
func TestDisabledFalseRemovesAttr(t *testing.T) {
	var on, off *htmlcss.El
	mount(t, func() h.Node {
		return h.Div(
			h.Button("Save").Disabled(false).Ref(func(e *htmlcss.El) { off = e }),
			h.Button("Del").Disabled(true).Ref(func(e *htmlcss.El) { on = e }),
		)
	})
	if _, ok := off.Attr("disabled"); ok {
		t.Fatal("Disabled(false) left the attribute set")
	}
	if _, ok := on.Attr("disabled"); !ok {
		t.Fatal("Disabled(true) did not set the attribute")
	}
}

// An argument that is neither text nor a node fails loudly, naming the
// tag, the position and the type — a silently dropped argument would ship
// an almost-right UI with nothing to chase.
func TestUnsupportedArgPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic")
		}
		msg, _ := r.(string)
		for _, want := range []string{"<div>", "argument 1", "struct {}"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("panic %q does not mention %q", msg, want)
			}
		}
	}()
	h.Div("ok", struct{}{})
}
