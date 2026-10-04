package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// inspectFixture builds and mounts:
//
//	div#app.wrap
//	  h1        "Title"
//	  button.primary   "Save"
//	  input[type=text]
//	  ul
//	    li.item          "one"
//	    li.item.active   "two"
func inspectFixture(t *testing.T, css string) (*qui.Window, *StyleEngine, *El) {
	t.Helper()
	eng := NewStyleEngine(css)
	app := eng.NewEl("div")
	app.SetElementID("app")
	app.SetClass("wrap")

	h1 := eng.NewEl("h1")
	h1.SetTextContent("Title")
	btn := eng.NewEl("button")
	btn.SetClass("primary")
	btn.SetTextContent("Save")
	input := eng.NewEl("input")
	input.SetAttr("type", "text")

	ul := eng.NewEl("ul")
	li1 := eng.NewEl("li")
	li1.SetClass("item")
	li1.SetTextContent("one")
	li2 := eng.NewEl("li")
	li2.SetClass("item active")
	li2.SetTextContent("two")
	ul.SetElementChildren([]qui.Widget{li1, li2})

	app.SetElementChildren([]qui.Widget{h1, btn, input, ul})
	eng.SetRoot(app)

	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	win.SetRoot(app)
	eng.Restyle()
	return win, eng, app
}

func TestInspectProjectsDOMCollapsingPlumbing(t *testing.T) {
	win, _, _ := inspectFixture(t, `.item { color: #808080 }`)
	dom := InspectWindow(win)

	if dom.Root == nil || dom.Root.Tag != "#document" {
		t.Fatalf("root = %+v, want #document", dom.Root)
	}
	if len(dom.Root.Children) != 1 {
		t.Fatalf("document has %d top-level nodes, want 1", len(dom.Root.Children))
	}
	app := dom.Root.Children[0]
	if app.Tag != "div" || app.ID != "app" {
		t.Fatalf("top node = %s#%s, want div#app", app.Tag, app.ID)
	}
	if len(app.Classes) != 1 || app.Classes[0] != "wrap" {
		t.Fatalf("app classes = %v, want [wrap]", app.Classes)
	}
	// The <input> renders through a backing widget; it must appear as ONE
	// DOM node (the El), not leak its backing control as a child.
	if len(app.Children) != 4 {
		t.Fatalf("app has %d children, want 4 (h1,button,input,ul); got %v", len(app.Children), childTags(app))
	}
	var input *DOMNode
	for _, c := range app.Children {
		if c.Tag == "input" {
			input = c
		}
	}
	if input == nil {
		t.Fatal("input node missing")
	}
	if len(input.Children) != 0 {
		t.Fatalf("input has %d children, want 0 (backing widget must be collapsed)", len(input.Children))
	}
	if input.Attrs["type"] != "text" {
		t.Fatalf("input type attr = %q, want text", input.Attrs["type"])
	}
}

func childTags(n *DOMNode) []string {
	var out []string
	for _, c := range n.Children {
		out = append(out, c.Tag)
	}
	return out
}

func TestInspectQueryCSSSelectors(t *testing.T) {
	win, _, _ := inspectFixture(t, ``)
	dom := InspectWindow(win)

	cases := []struct {
		sel  string
		want int
	}{
		{".item", 2},
		{"li.active", 1},
		{"ul > li", 2},
		{"#app", 1},
		{"button", 1},
		{"button.primary", 1},
		{"div .item", 2},
		{".missing", 0},
		{"span", 0},
	}
	for _, c := range cases {
		got, err := dom.Query(c.sel)
		if err != nil {
			t.Fatalf("Query(%q) error: %v", c.sel, err)
		}
		if len(got) != c.want {
			t.Errorf("Query(%q) = %d nodes, want %d", c.sel, len(got), c.want)
		}
	}

	// li.active must be the SECOND li (text "two").
	got, _ := dom.Query("li.active")
	if len(got) == 1 && got[0].Text != "two" {
		t.Errorf("li.active text = %q, want \"two\"", got[0].Text)
	}
}

func TestInspectStylesExplainCascade(t *testing.T) {
	css := `.item { color: #808080; padding: 3px }
	        .item.active { color: #00ff00; font-weight: 700 }`
	win, _, _ := inspectFixture(t, css)
	dom := InspectWindow(win)

	styles, err := dom.Styles("li.active")
	if err != nil {
		t.Fatalf("Styles error: %v", err)
	}
	if len(styles) != 1 {
		t.Fatalf("Styles matched %d elements, want 1", len(styles))
	}
	es := styles[0]

	// Winning cascade value: .item.active beats .item on color.
	if es.Computed["color"] != "#00ff00" {
		t.Errorf("computed color = %q, want #00ff00", es.Computed["color"])
	}
	if es.Computed["padding"] != "3px" {
		t.Errorf("computed padding = %q, want 3px (inherited from .item)", es.Computed["padding"])
	}

	// Both author rules must appear, most-specific first, with the losing
	// color declaration marked inactive.
	var sawActiveWinner, sawItemLoser bool
	for _, r := range es.MatchedRules {
		if r.Origin != "author" {
			continue
		}
		for _, d := range r.Declarations {
			if d.Property != "color" {
				continue
			}
			if r.Selector == ".item.active" && d.Value == "#00ff00" && d.Active {
				sawActiveWinner = true
			}
			if r.Selector == ".item" && d.Value == "#808080" && !d.Active {
				sawItemLoser = true
			}
		}
	}
	if !sawActiveWinner {
		t.Errorf("expected .item.active color declaration marked active; rules=%+v", es.MatchedRules)
	}
	if !sawItemLoser {
		t.Errorf("expected .item color declaration marked overridden; rules=%+v", es.MatchedRules)
	}
	if es.Selector != "li.item.active" {
		t.Errorf("element descriptor = %q, want li.item.active", es.Selector)
	}
}

func TestInspectQueryHasText(t *testing.T) {
	win, _, _ := inspectFixture(t, ``)
	dom := InspectWindow(win)

	cases := []struct {
		sel  string
		want int
		text string // expected text of the single match, "" to skip
	}{
		{`button:has-text("Save")`, 1, "Save"},
		{`button:has-text("save")`, 1, "Save"}, // case-insensitive
		{`li:has-text("two")`, 1, "two"},
		{`:has-text("Title")`, 1, "Title"}, // bare text locator → innermost (h1, not div#app)
		{`li:has-text("nope")`, 0, ""},
		{`button:has-text("Title")`, 0, ""}, // right text, wrong element
	}
	for _, c := range cases {
		got, err := dom.Query(c.sel)
		if err != nil {
			t.Fatalf("Query(%q) error: %v", c.sel, err)
		}
		if len(got) != c.want {
			t.Errorf("Query(%q) = %d, want %d", c.sel, len(got), c.want)
			continue
		}
		if c.want == 1 && c.text != "" && got[0].Text != c.text {
			t.Errorf("Query(%q) matched text %q, want %q", c.sel, got[0].Text, c.text)
		}
	}
}

func TestInspectInputTextState(t *testing.T) {
	win, _, _ := inspectFixture(t, ``)
	dom := InspectWindow(win)

	got, err := dom.Query("input")
	if err != nil || len(got) != 1 {
		t.Fatalf("Query(input) = %d nodes (err %v), want 1", len(got), err)
	}
	// The <input> renders through a backing edit widget; its TextState must
	// surface on the element's own DOM node.
	if got[0].TextState == nil {
		t.Fatal("input DOM node has no TextState")
	}
	if got[0].TextState.SelStart != -1 {
		t.Errorf("fresh input SelStart = %d, want -1 (no selection)", got[0].TextState.SelStart)
	}

	// A non-control element has no TextState.
	h1, _ := dom.Query("h1")
	if len(h1) == 1 && h1[0].TextState != nil {
		t.Errorf("h1 should have no TextState, got %+v", *h1[0].TextState)
	}
}

func TestInspectResolveWidget(t *testing.T) {
	win, _, _ := inspectFixture(t, ``)
	dom := InspectWindow(win)

	widget, node, err := dom.ResolveWidget("li.active")
	if err != nil {
		t.Fatalf("ResolveWidget error: %v", err)
	}
	if widget == nil || node == nil {
		t.Fatal("ResolveWidget returned nil")
	}
	if node.Text != "two" {
		t.Errorf("resolved node text = %q, want \"two\"", node.Text)
	}
	if _, ok := widget.(*El); !ok {
		t.Errorf("resolved widget is %T, want *El", widget)
	}

	if _, _, err := dom.ResolveWidget(".nope"); err != qui.ErrNoMatch {
		t.Errorf("ResolveWidget(.nope) err = %v, want ErrNoMatch", err)
	}
}
