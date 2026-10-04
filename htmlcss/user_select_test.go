package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// selectableOf reports the Selectable flag of the text widget an element
// renders its own text through (Label leaf or folded InlineBox run).
func selectableOf(t *testing.T, w qui.Widget) bool {
	t.Helper()
	el, ok := w.(*El)
	if !ok {
		t.Fatalf("widget %T is not an *El", w)
	}
	var found *bool
	var walk func(qui.Widget)
	walk = func(w qui.Widget) {
		switch tw := w.(type) {
		case *widgets.Label:
			if found == nil {
				v := tw.Selectable
				found = &v
			}
		case *widgets.InlineBox:
			if found == nil {
				v := tw.Selectable
				found = &v
			}
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, ch := range c.ChildList() {
				walk(ch)
			}
		}
	}
	walk(el)
	if found == nil {
		t.Fatalf("element rendered no Label / InlineBox to check")
	}
	return *found
}

// `user-select: none` takes the element's text out of drag-selection, and
// inherits to descendants — marking a toolbar covers every caption in it.
func TestUserSelectNoneInherits(t *testing.T) {
	res := RenderDoc(
		`<body><div id="bar"><span id="cap">Save</span></div>
		 <p id="prose">selectable text</p></body>`,
		`#bar { user-select: none; }`,
		Options{},
	)
	if selectableOf(t, res.ByID["bar"]) {
		t.Error("#bar text should not be selectable under user-select:none")
	}
	if cap, ok := res.ByID["cap"]; ok && selectableOf(t, cap) {
		t.Error("#cap should inherit user-select:none")
	}
	if !selectableOf(t, res.ByID["prose"]) {
		t.Error("#prose should stay selectable")
	}
}

// A descendant can opt back in with `user-select: text`.
func TestUserSelectTextOptsBackIn(t *testing.T) {
	res := RenderDoc(
		`<body><div id="bar"><span id="pick">copy me</span></div></body>`,
		`#bar { user-select: none; } #pick { user-select: text; }`,
		Options{},
	)
	if !selectableOf(t, res.ByID["pick"]) {
		t.Error("`user-select: text` should re-enable selection under a none ancestor")
	}
}

// The -webkit- spelling is accepted (still what many stylesheets ship).
func TestUserSelectWebkitAlias(t *testing.T) {
	res := RenderDoc(
		`<body><div id="bar">Save</div></body>`,
		`#bar { -webkit-user-select: none; }`,
		Options{},
	)
	if selectableOf(t, res.ByID["bar"]) {
		t.Error("-webkit-user-select: none should be honored")
	}
}

// Restyle has to hand selection BACK when the rule stops applying — Els are
// reused, and the old code only ever cleared the flag.
func TestUserSelectRestoredOnRestyle(t *testing.T) {
	res := RenderDoc(
		`<body><div id="bar" class="locked">Save</div></body>`,
		`.locked { user-select: none; } .free { color: #333; }`,
		Options{},
	)
	bar := res.ByID["bar"].(*El)
	if selectableOf(t, bar) {
		t.Fatal("initial state should be unselectable")
	}
	bar.SetClass("free")
	res.Engine.Restyle()
	if !selectableOf(t, bar) {
		t.Error("selection not restored after the user-select rule stopped matching")
	}
}
