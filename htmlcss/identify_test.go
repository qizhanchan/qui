package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func TestRenderDocByID(t *testing.T) {
	res := RenderDoc(
		`<body><div id="main"><button id="go">Go</button></div></body>`,
		`#main { background: #eee }`,
		Options{},
	)
	w, ok := res.ByID["main"]
	if !ok || w == nil {
		t.Fatal("ByID missing #main")
	}
	if qui.WidgetID(w) != "main" {
		t.Errorf("WidgetID = %q, want main", qui.WidgetID(w))
	}
	if _, ok := res.ByID["go"]; !ok {
		t.Error("ByID missing #go (button)")
	}
}

func TestRenderDocByClass(t *testing.T) {
	res := RenderDoc(
		`<body><div class="card"></div><div class="card wide"></div><div class="other"></div></body>`,
		`.card { background: #eee }`,
		Options{},
	)
	if got := len(res.ByClass["card"]); got != 2 {
		t.Errorf("ByClass[card] len = %d, want 2", got)
	}
	if got := len(res.ByClass["wide"]); got != 1 {
		t.Errorf("ByClass[wide] len = %d, want 1", got)
	}
}

func TestDuplicateIDFirstWins(t *testing.T) {
	res := RenderDoc(
		`<body><div id="dup" class="first"></div><div id="dup" class="second"></div></body>`,
		``,
		Options{},
	)
	w := res.ByID["dup"]
	if w == nil {
		t.Fatal("ByID missing #dup")
	}
	// The first element in document order wins.
	if len(res.ByClass["first"]) == 0 || res.ByClass["first"][0] != w {
		t.Error("ByID[dup] should be the first #dup element")
	}
}

// TestSetIDReachesFind confirms the HTML id propagates all the way to the
// framework's Window.Find selector layer.
func TestSetIDReachesFind(t *testing.T) {
	res := RenderDoc(
		`<body><div id="panel"><button id="submit">OK</button></div></body>`,
		``,
		Options{},
	)
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	win.SetRoot(res.Root)
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	found, err := win.Find("#submit")
	if err != nil {
		t.Fatalf("Find(#submit): %v", err)
	}
	if found != res.ByID["submit"] {
		t.Error("Window.Find(#submit) did not resolve to the same widget as ByID")
	}
}

func TestAnchorHrefPreserved(t *testing.T) {
	// The id makes the anchor non-foldable, so it materializes as its
	// own element with link semantics (role + href-driven click-through).
	res := RenderDoc(`<body><a id="lnk" href="https://example.com">link</a><hr></body>`, ``, Options{})
	a, ok := res.ByID["lnk"].(*El)
	if !ok {
		t.Fatalf("#lnk is %T, want *htmlcss.El", res.ByID["lnk"])
	}
	if a.Role() != qui.RoleLink {
		t.Errorf("anchor role = %q, want link", a.Role())
	}
	if a.attrs["href"] != "https://example.com" {
		t.Errorf("anchor href = %q, want https://example.com", a.attrs["href"])
	}
}
