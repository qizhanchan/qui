package html_test

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
)

// `:class` replaces a template's static class, and a static id is a real
// element id (Window.Find, AX).
func TestTemplateStaticClassAndID(t *testing.T) {
	tpl := h.MustParse(`<div id="box" class="row" :class="cls">x</div>`)
	var root *htmlcss.El
	win, _ := mount(t, func() h.Node {
		return tpl.Bind(h.Scope{"cls": "row active"}).(*h.Builder).Ref(func(e *htmlcss.El) { root = e })
	})
	if got, _ := root.Attr("class"); got != "row active" {
		t.Errorf("class = %q, want the :class value", got)
	}
	if found, _ := win.Find("#box"); found == nil {
		t.Error(`Window.Find("#box") found nothing`)
	}
}

// A builder property set on one render and dropped on the next is cleared.
func TestDroppedBuilderIDIsCleared(t *testing.T) {
	var setWithID func(bool)
	var root *htmlcss.El
	win := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	rt := h.Mount(win, ``, func() h.Node {
		withID, set := reactive.UseState(true)
		setWithID = set
		b := h.Div("x").Ref(func(e *htmlcss.El) { root = e })
		if withID {
			b.ID("box")
		}
		return b
	})
	win.SetRoot(rt.Root())
	if _, ok := root.Attr("id"); !ok {
		t.Fatal("id not set on first render")
	}
	setWithID(false)
	rt.Flush()
	win.DrainJobsForTest()
	if v, ok := root.Attr("id"); ok {
		t.Errorf("id = %q after the builder dropped it", v)
	}
}

// Toggling a template :if must not remount the sibling after it.
func TestTemplateIfKeepsSiblingPosition(t *testing.T) {
	tpl := h.MustParse(`<div><span :if="err">bad</span><input class="field"></div>`)
	var setErr func(bool)
	var inputs []*htmlcss.El
	win := qui.NewTestWindow(qui.Size{W: 300, H: 100})
	rt := h.Mount(win, ``, func() h.Node {
		err, set := reactive.UseState(false)
		setErr = set
		return tpl.Bind(h.Scope{"err": err})
	})
	win.SetRoot(rt.Root())
	find := func() *htmlcss.El {
		root := rt.Root().(*htmlcss.El)
		return findEl(root, "input")
	}
	inputs = append(inputs, find())
	setErr(true)
	rt.Flush()
	inputs = append(inputs, find())
	if inputs[0] == nil || inputs[0] != inputs[1] {
		t.Fatalf("input remounted when the :if sibling toggled (%p -> %p)", inputs[0], inputs[1])
	}
}
