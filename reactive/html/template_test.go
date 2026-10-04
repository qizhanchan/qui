package html_test

import (
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
)

const rowTpl = `
<div class="todo" :key="id">
  <span class="todo-text">{text}</span>
  <span class="tag" :if="urgent">urgent</span>
  <button class="del" @click="onDelete">✕</button>
</div>`

func TestTemplateBasics(t *testing.T) {
	tpl := h.MustParse(rowTpl)
	clicked := 0
	var root *htmlcss.El
	mount(t, func() h.Node {
		return tpl.Bind(h.Scope{
			"id": 7, "text": "write the docs", "urgent": true,
			"onDelete": func() { clicked++ },
		}).(*h.Builder).Ref(func(e *htmlcss.El) { root = e })
	})
	if root == nil {
		t.Fatal("template root never created")
	}
	if got, _ := root.Attr("class"); got != "todo" {
		t.Fatalf("root class = %q", got)
	}
	// The two spans fold into the row's inline run (they are pure-text
	// inline elements), so they show up in the accessible name rather than
	// as separate widgets — the same shape hand-written builders produce.
	if got := root.AccessibleName(); !strings.Contains(got, "write the docs") ||
		!strings.Contains(got, "urgent") {
		t.Fatalf("row text = %q, want the hole and the :if span in it", got)
	}
	// The @click handler is wired to the Scope's func.
	btn := findEl(root, "button")
	if btn == nil {
		t.Fatal("no <button> in the bound tree")
	}
	btn.Handle(qui.NewMouseEvent(qui.EventMouseUp, 1, 1, qui.MouseButtonLeft, 0))
	if clicked != 1 {
		t.Fatalf("click count = %d, want 1", clicked)
	}
}

// :if with a plain bool drops the element entirely.
func TestTemplateIfFalse(t *testing.T) {
	tpl := h.MustParse(rowTpl)
	win, _ := mount(t, func() h.Node {
		return tpl.Bind(h.Scope{
			"id": 1, "text": "x", "urgent": false, "onDelete": func() {},
		})
	})
	if strings.Contains(dumpText(t, win), "urgent") {
		t.Fatal("`:if=\"urgent\"` element rendered while the flag was false")
	}
}

// A signal in the Scope binds rather than interpolates: flipping it
// mounts the subtree with no render pass.
func TestTemplateSignalIf(t *testing.T) {
	tpl := h.MustParse(`<div><span class="tag" :if="on">urgent</span></div>`)
	on := reactive.NewSignal(false)
	renders := 0
	win, _ := mount(t, func() h.Node {
		renders++
		return tpl.Bind(h.Scope{"on": on})
	})
	if strings.Contains(dumpText(t, win), "urgent") {
		t.Fatal("subtree mounted while the signal was false")
	}
	before := renders
	on.Set(true)
	win.DrainJobsForTest()
	if !strings.Contains(dumpText(t, win), "urgent") {
		t.Fatal("signal flip did not mount the subtree")
	}
	if renders != before {
		t.Fatalf("signal flip cost %d render(s), want 0", renders-before)
	}
}

// A lone signal text hole becomes BindText — the update skips reconcile.
func TestTemplateSignalText(t *testing.T) {
	tpl := h.MustParse(`<span class="v">{label}</span>`)
	label := reactive.NewSignal("first")
	renders := 0
	win, root := mount(t, func() h.Node {
		renders++
		return tpl.Bind(h.Scope{"label": label})
	})
	if got := root.AccessibleName(); got != "first" {
		t.Fatalf("initial text = %q", got)
	}
	before := renders
	label.Set("second")
	win.DrainJobsForTest()
	if got := root.AccessibleName(); got != "second" {
		t.Fatalf("bound text = %q, want %q", got, "second")
	}
	if renders != before {
		t.Fatalf("signal set cost %d render(s), want 0", renders-before)
	}
}

// A <slot> takes the subtree the caller built in Go — this is how loops
// and anything else the template deliberately cannot express get in.
func TestTemplateSlot(t *testing.T) {
	tpl := h.MustParse(`
<ul class="list">
  <slot name="rows"></slot>
</ul>`)
	var rows []h.Node
	for _, s := range []string{"a", "b", "c"} {
		rows = append(rows, h.Li(s))
	}
	win, _ := mount(t, func() h.Node { return tpl.Bind(h.Scope{"rows": rows}) })
	dom := htmlcss.InspectWindow(win).Root.Children[0]
	if len(dom.Children) != 3 {
		t.Fatalf("slot produced %d rows, want 3", len(dom.Children))
	}
}

// Inline text and elements on one line stay one inline run, exactly as
// the markup reads.
func TestTemplateMixedInline(t *testing.T) {
	tpl := h.MustParse(`<p>Hello <b>world</b>!</p>`)
	_, root := mount(t, func() h.Node { return tpl.Bind(nil) })
	if got := root.AccessibleName(); got != "Hello world!" {
		t.Fatalf("paragraph = %q, want %q", got, "Hello world!")
	}
}

// Indentation between block children is not content and must not leak
// empty labels into the tree.
func TestTemplateDropsLayoutWhitespace(t *testing.T) {
	tpl := h.MustParse("<div>\n  <div>a</div>\n  <div>b</div>\n</div>")
	win, _ := mount(t, func() h.Node { return tpl.Bind(nil) })
	dom := htmlcss.InspectWindow(win).Root.Children[0]
	if len(dom.Children) != 2 {
		t.Fatalf("got %d children, want 2 (indentation leaked)", len(dom.Children))
	}
}

// <select> reads its options from <option> children, as HTML does, and
// :ref hands the live element back for the assertions.
func TestTemplateSelectOptions(t *testing.T) {
	tpl := h.MustParse(`
<select :ref="ref" :selected="idx" @select="onPick">
  <option>Low</option>
  <option value="hi" disabled>High</option>
</select>`)
	var sel *htmlcss.El
	mount(t, func() h.Node {
		return tpl.Bind(h.Scope{
			"idx": 1, "onPick": func(int, string) {},
			"ref": func(e *htmlcss.El) { sel = e },
		})
	})
	if sel == nil {
		t.Fatal(":ref never fired")
	}
	if got := sel.SelectOptions(); len(got) != 2 || got[0] != "Low" || got[1] != "High" {
		t.Fatalf("options = %v, want [Low High]", got)
	}
}

// Names lists the Scope keys a template needs, so a test can assert a
// caller covers them.
func TestTemplateNames(t *testing.T) {
	tpl := h.MustParse(rowTpl)
	if got := strings.Join(tpl.Names(), " "); got != "id onDelete text urgent" {
		t.Fatalf("Names() = %q", got)
	}
}

// A typo in a directive fails at Parse with the template line, not at
// render with a mystery.
func TestTemplateUnknownDirectiveReportsLine(t *testing.T) {
	_, err := h.Parse("<div>\n  <span :iff=\"urgent\">x</span>\n</div>")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"line 2", ":iff", ":if"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}

// A missing Scope key names the line, the key, and what the Scope does
// hold — the three things needed to fix it.
func TestTemplateMissingScopeKeyReportsLine(t *testing.T) {
	tpl := h.MustParse("<div>\n  <button @click=\"onSave\">Save</button>\n</div>")
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic")
		}
		msg, _ := r.(string)
		for _, want := range []string{"line 2", "onSave", "onCancel"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("panic %q does not mention %q", msg, want)
			}
		}
	}()
	mount(t, func() h.Node { return tpl.Bind(h.Scope{"onCancel": func() {}}) })
}

// A self-closing <slot> would swallow the rest of the template, so it is
// rejected with the fix spelled out.
func TestTemplateSelfClosingSlotRejected(t *testing.T) {
	_, err := h.Parse(`<div><slot name="rows"/><span>after</span></div>`)
	if err == nil || !strings.Contains(err.Error(), "explicitly closed") {
		t.Fatalf("error = %v, want the self-closing-slot explanation", err)
	}
}

// findEl locates the first element with the given tag, descending through
// the plumbing widgets (InlineBox, ScrollView) the engine inserts.
func findEl(w qui.Widget, tag string) *htmlcss.El {
	if e, ok := w.(*htmlcss.El); ok && e.Tag() == tag {
		return e
	}
	kids, ok := w.(interface{ ChildList() []qui.Widget })
	if !ok {
		return nil
	}
	for _, c := range kids.ChildList() {
		if f := findEl(c, tag); f != nil {
			return f
		}
	}
	return nil
}

// A template file starts with a comment and indentation around its root
// element; neither is content, and treating them as content used to
// produce an unnamed element the reconciler rejects.
func TestTemplateIgnoresLeadingCommentAndIndent(t *testing.T) {
	tpl := h.MustParse("<!-- what this is -->\n\n<div class=\"card\">{title}</div>\n")
	_, root := mount(t, func() h.Node { return tpl.Bind(h.Scope{"title": "Cards"}) })
	if root.Tag() != "div" {
		t.Fatalf("root tag = %q, want div", root.Tag())
	}
	if got := root.AccessibleName(); got != "Cards" {
		t.Fatalf("root text = %q", got)
	}
}

func dumpText(t *testing.T, win *qui.Window) string {
	t.Helper()
	var sb strings.Builder
	var walk func(*htmlcss.DOMNode)
	walk = func(n *htmlcss.DOMNode) {
		sb.WriteString(n.Tag + " " + n.Text + "\n")
		for _, k := range n.Children {
			walk(k)
		}
	}
	walk(htmlcss.InspectWindow(win).Root)
	return sb.String()
}

const setSrc = `
<template id="row">
  <div class="r"><span>{label}</span></div>
</template>

<template id="empty">
  <span class="e">nothing here</span>
</template>`

// One file, several named fragments — the shape a dialog wants.
func TestParseSet(t *testing.T) {
	set := h.MustParseSet(setSrc)
	if got := strings.Join(set.Names(), " "); got != "empty row" {
		t.Fatalf("Names() = %q, want \"empty row\"", got)
	}
	_, root := mount(t, func() h.Node { return set.Bind("row", h.Scope{"label": "hi"}) })
	if got := root.AccessibleName(); got != "hi" {
		t.Fatalf("row text = %q", got)
	}
}

// An unknown id names the ids the set does hold — the whole diagnosis for
// a typo, which otherwise only shows up when the dialog is opened.
func TestSetUnknownIDNamesTheOthers(t *testing.T) {
	set := h.MustParseSet(setSrc)
	defer func() {
		msg, _ := recover().(string)
		for _, want := range []string{"\"rows\"", "empty, row"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("panic %q does not mention %q", msg, want)
			}
		}
	}()
	set.Bind("rows", nil)
}

// Line numbers stay relative to the whole file, so they point at the
// fragment the author is looking at.
func TestSetErrorsCarryFileLines(t *testing.T) {
	_, err := h.ParseSet("<template id=\"a\">\n  <div>x</div>\n</template>\n<template id=\"b\">\n  <span :nope=\"v\">y</span>\n</template>")
	if err == nil || !strings.Contains(err.Error(), "line 5") {
		t.Fatalf("error = %v, want it to point at line 5", err)
	}
}

// Passing a fragment file to Parse (or a plain fragment to ParseSet) says
// which one to use instead of failing obscurely.
func TestParseAndParseSetPointAtEachOther(t *testing.T) {
	if _, err := h.Parse(setSrc); err == nil || !strings.Contains(err.Error(), "MustParseSet") {
		t.Fatalf("Parse of a fragment file: %v", err)
	}
	if _, err := h.ParseSet(`<div>x</div>`); err == nil || !strings.Contains(err.Error(), "MustParse") {
		t.Fatalf("ParseSet of a plain fragment: %v", err)
	}
}
