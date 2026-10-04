package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

func TestParseHTMLBuildsTree(t *testing.T) {
	dom := ParseHTML(`<body><h1>Hi</h1><p>a <strong>b</strong> c</p></body>`)
	body := findTag(dom, "body")
	if body == nil {
		t.Fatal("no body found")
	}
	h1 := findTag(body, "h1")
	if h1 == nil || collapseText(textContent(h1)) != "Hi" {
		t.Fatalf("h1 not parsed: %v", h1)
	}
}

func TestCSSCascadeSpecificity(t *testing.T) {
	sheet := ParseCSS(`p { color: red } p.x { color: green } #id { color: blue }`)
	dom := ParseHTML(`<p class="x" id="id">hi</p>`)
	styles := resolveStyles(dom, sheet)
	p := findTag(dom, "p")
	cs := styles[p]
	// #id (specificity 1,0,0) beats p.x (0,1,1) beats p (0,0,1).
	if cs.Color != (qui.Color{R: 0, G: 0, B: 1, A: 1}) {
		t.Fatalf("expected id color blue, got %+v", cs.Color)
	}
}

func TestRenderProducesWidgetTree(t *testing.T) {
	html := `
	<body>
	  <h1>Title</h1>
	  <div class="row">
	    <button>Click</button>
	    <a href="https://x.com">link</a>
	  </div>
	  <hr>
	  <p>Some <strong>bold</strong> text.</p>
	</body>`
	css := `
	.row { display: flex; gap: 8px; }
	h1 { color: #222; }
	button { background: #eee; }`

	root := Render(html, css, Options{})
	el, ok := root.(*El)
	if !ok {
		t.Fatalf("root = %T, want *htmlcss.El", root)
	}
	// Walk and count element kinds. The converged path renders every
	// element as an El — button/link identity shows through Role().
	var buttons, anchors, rules, texts int
	var walk func(w qui.Widget)
	walk = func(w qui.Widget) {
		switch v := w.(type) {
		case *El:
			switch v.Role() {
			case qui.RoleButton:
				buttons++
			case qui.RoleLink:
				anchors++
			}
		case *widgets.Rule:
			rules++
		case *widgets.Label:
			texts++
		case *widgets.InlineBox:
			texts++
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, ch := range c.ChildList() {
				walk(ch)
			}
		}
	}
	walk(el)

	if buttons != 1 {
		t.Errorf("buttons = %d, want 1", buttons)
	}
	if anchors != 1 {
		t.Errorf("anchors = %d, want 1", anchors)
	}
	if rules != 1 {
		t.Errorf("rules = %d, want 1", rules)
	}
	if texts < 2 { // at least the h1 and the mixed paragraph
		t.Errorf("text hosts = %d, want >= 2", texts)
	}
}

func TestFlexDisplayUsesFlexLayout(t *testing.T) {
	root := Render(`<body><div class="r"><span>a</span></div></body>`,
		`.r { display: flex; }`, Options{})
	box := &root.(*El).Box
	// The body's single child is the .r flex box.
	kids := box.ChildList()
	if len(kids) != 1 {
		t.Fatalf("body children = %d, want 1", len(kids))
	}
}
