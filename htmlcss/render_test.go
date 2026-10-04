package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

func walkWidgets(w qui.Widget, fn func(qui.Widget)) {
	if w == nil {
		return
	}
	fn(w)
	if cl, ok := w.(interface{ ChildList() []qui.Widget }); ok {
		for _, c := range cl.ChildList() {
			walkWidgets(c, fn)
		}
	}
}

func labelTexts(root qui.Widget) []string {
	var out []string
	walkWidgets(root, func(w qui.Widget) {
		if l, ok := w.(*widgets.Label); ok {
			if t := l.Text(); t != "" {
				out = append(out, t)
			}
		}
	})
	return out
}

func hasText(root qui.Widget, want string) bool {
	for _, t := range labelTexts(root) {
		if t == want {
			return true
		}
	}
	return false
}

func TestUnorderedListMarkers(t *testing.T) {
	root := Render(`<ul><li>a</li><li>b</li></ul>`, ``, Options{})
	// Bullets are DRAWN, not typeset — U+2022 is full-width in a CJK primary
	// font, so a Label would indent disc items past every other marker type
	// (see listmarker.go). Assert on the marker widgets, not on label text.
	var discs int
	walkWidgets(root, func(w qui.Widget) {
		if m, ok := w.(*listMarkerWidget); ok && m.shape == markerDisc {
			discs++
		}
	})
	if discs != 2 {
		t.Errorf("got %d disc markers, want 2 (labels present: %v)", discs, labelTexts(root))
	}
}

func TestOrderedListMarkers(t *testing.T) {
	root := Render(`<ol><li>a</li><li>b</li><li>c</li></ol>`, ``, Options{})
	texts := labelTexts(root)
	for _, want := range []string{"1.", "2.", "3."} {
		if !hasText(root, want) {
			t.Errorf("expected marker %q, got %v", want, texts)
		}
	}
}

func TestListStyleNoneSuppressesMarker(t *testing.T) {
	root := Render(`<ul style="list-style-type:none"><li>a</li></ul>`, ``, Options{})
	if hasText(root, "•") {
		t.Errorf("list-style-type:none should suppress the bullet, got %v", labelTexts(root))
	}
}

// anchorLabel digs the text label out of a standalone <a> element.
func anchorLabel(t *testing.T, w qui.Widget) *widgets.Label {
	t.Helper()
	el, ok := w.(*El)
	if !ok {
		t.Fatalf("anchor is %T, want *htmlcss.El", w)
	}
	kids := el.ChildList()
	if len(kids) != 1 {
		t.Fatalf("anchor has %d children, want 1 label", len(kids))
	}
	lbl, ok := kids[0].(*widgets.Label)
	if !ok {
		t.Fatalf("anchor child is %T, want *widgets.Label", kids[0])
	}
	return lbl
}

func TestAnchorUnderlineDecoration(t *testing.T) {
	res := RenderDoc(`<body><a id="a" href="#">link</a><hr></body>`, ``, Options{})
	lbl := anchorLabel(t, res.ByID["a"])
	if lbl.Paragraph.Decoration&qui.DecorationUnderline == 0 {
		t.Error("anchor should carry underline decoration (UA default)")
	}
}

func TestTextDecorationNoneOverridesUA(t *testing.T) {
	res := RenderDoc(`<body><a id="a" href="#">link</a><hr></body>`, `a { text-decoration: none }`, Options{})
	lbl := anchorLabel(t, res.ByID["a"])
	if lbl.Paragraph.Decoration != 0 {
		t.Errorf("text-decoration:none should clear underline, got %v", lbl.Paragraph.Decoration)
	}
}

func TestLineThroughDecoration(t *testing.T) {
	res := RenderDoc(`<body><p id="p" style="text-decoration:line-through">x</p></body>`, ``, Options{})
	p, ok := res.ByID["p"].(*El).ChildList()[0].(*widgets.Label)
	if !ok {
		t.Fatalf("#p child is %T, want *widgets.Label", res.ByID["p"].(*El).ChildList()[0])
	}
	if p.Paragraph.Decoration&qui.DecorationLineThrough == 0 {
		t.Error("expected line-through decoration")
	}
}

func TestPerSideBorder(t *testing.T) {
	res := RenderDoc(
		`<body><div id="q">quote</div></body>`,
		`#q { border-left: 4px solid #ddd }`,
		Options{},
	)
	el, ok := res.ByID["q"].(*El)
	if !ok {
		t.Fatalf("#q is %T, want *htmlcss.El", res.ByID["q"])
	}
	box := &el.Box
	st := box.Style()
	if st.BorderWidths.Left != 4 {
		t.Errorf("border-left width = %v, want 4", st.BorderWidths.Left)
	}
	if st.BorderWidths.Top != 0 || st.BorderWidths.Right != 0 || st.BorderWidths.Bottom != 0 {
		t.Errorf("only left border expected, got %+v", st.BorderWidths)
	}
	if st.BorderColors.Left.A == 0 {
		t.Error("border-left color should be set")
	}
}

func TestUniformBorderStyleDashed(t *testing.T) {
	res := RenderDoc(
		`<body><div id="b">x</div></body>`,
		`#b { border: 2px dashed #333; width: 40px }`,
		Options{},
	)
	box := &res.ByID["b"].(*El).Box
	st := box.Style()
	if st.BorderStyle != qui.BorderDashed {
		t.Errorf("BorderStyle = %v, want dashed", st.BorderStyle)
	}
	if st.BorderSize != 2 {
		t.Errorf("BorderSize = %v, want 2", st.BorderSize)
	}
}

func TestInteractiveStateVariants(t *testing.T) {
	res := RenderDoc(
		`<body><div id="btn">Click</div></body>`,
		`#btn { background: #eee }
		 #btn:hover { background: #ddd }
		 #btn:focus { background: #cce }
		 #btn:active { background: #ccc }`,
		Options{},
	)
	el, ok := res.ByID["btn"].(*El)
	if !ok {
		t.Fatalf("#btn is %T, want *htmlcss.El", res.ByID["btn"])
	}
	box := &el.Box
	if box.Hover == nil {
		t.Error("expected :hover variant")
	}
	if box.Focus == nil {
		t.Error("expected :focus variant")
	}
	if box.Active == nil {
		t.Error("expected :active variant")
	}
	// A focus-styled box opts into focus.
	if !box.Focusable() {
		t.Error("box with :focus style should be Focusable")
	}
}

// TestActivePressPaints proves the :active variant actually paints on
// MouseDown and reverts on MouseUp (the new press-tracking path).
func TestActivePressPaints(t *testing.T) {
	res := RenderDoc(
		`<body><div id="b">press</div></body>`,
		`#b { width: 80px; height: 30px } #b:active { background: #ccc }`,
		Options{},
	)
	box := &res.ByID["b"].(*El).Box
	box.Layout(qui.Rect{X: 0, Y: 0, W: 80, H: 30})

	var rest qui.RecordingCanvas
	box.Draw(&rest)
	restFills := len(rest.Fills)

	bd := box.Bounds()
	box.Handle(qui.NewMouseEvent(qui.EventMouseDown, bd.X+2, bd.Y+2, qui.MouseButtonLeft, 0))
	var pressed qui.RecordingCanvas
	box.Draw(&pressed)
	if len(pressed.Fills) <= restFills {
		t.Errorf(":active did not add a fill on press (rest=%d pressed=%d)", restFills, len(pressed.Fills))
	}

	box.Handle(qui.NewMouseEvent(qui.EventMouseUp, bd.X+2, bd.Y+2, qui.MouseButtonLeft, 0))
	var released qui.RecordingCanvas
	box.Draw(&released)
	if len(released.Fills) != restFills {
		t.Errorf(":active fill should clear on release (rest=%d released=%d)", restFills, len(released.Fills))
	}
}

// TestFocusPaints proves the :focus variant paints when the box is focused.
func TestFocusPaints(t *testing.T) {
	res := RenderDoc(
		`<body><div id="b">x</div></body>`,
		`#b { width: 80px; height: 30px } #b:focus { background: #cce }`,
		Options{},
	)
	box := &res.ByID["b"].(*El).Box
	if !box.Focusable() {
		t.Fatal("box with :focus style should be Focusable")
	}
	box.Layout(qui.Rect{X: 0, Y: 0, W: 80, H: 30})

	var rest qui.RecordingCanvas
	box.Draw(&rest)
	restFills := len(rest.Fills)

	box.SetFocused(true)
	var focused qui.RecordingCanvas
	box.Draw(&focused)
	if len(focused.Fills) <= restFills {
		t.Errorf(":focus did not add a fill (rest=%d focused=%d)", restFills, len(focused.Fills))
	}
}

// TestFocusBoxClickFocuses proves a click on a focusable box's text
// focuses the BOX (not the inner label), so its :focus style shows —
// the fix for "clicking .focusbox does nothing".
func TestFocusBoxClickFocuses(t *testing.T) {
	res := RenderDoc(
		`<body><div id="fb">focus me</div></body>`,
		`#fb { width: 120px; height: 40px } #fb:focus { background: #cce }`,
		Options{},
	)
	win := qui.NewTestWindow(qui.Size{W: 300, H: 200})
	win.SetRoot(res.Root)
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 300, H: 200})

	if err := win.Click("#fb", qui.ClickOptions{}); err != nil {
		t.Fatalf("Click(#fb): %v", err)
	}
	if win.Focused() != res.ByID["fb"] {
		t.Errorf("after click, focused = %v, want the #fb box", win.Focused())
	}
}

func TestNoStateVariantWhenUnchanged(t *testing.T) {
	// A :hover that changes NOTHING (same values as the resting style) must
	// not attach a variant nor mark text-state active — otherwise we'd
	// repaint on every hover for no visual change.
	res := RenderDoc(
		`<body><div id="d">x</div></body>`,
		`#d { background: #eee; border: 1px solid #ccc; color: red } #d:hover { color: red }`,
		Options{},
	)
	el := res.ByID["d"].(*El)
	if el.Box.Hover != nil {
		t.Error("a no-op :hover should not attach a box hover variant")
	}
	if el.textStateActive {
		t.Error("a no-op :hover should not mark text-state active")
	}
}

// A :hover that only changes text color carries no box decoration, so it
// attaches a repaint sentinel (empty box variant) + marks text-state active
// so El.Draw recolors the label. This is the P0.1 fix: previously such a
// rule silently no-op'd (the old code left box.Hover nil and never
// recolored the text).
func TestTextOnlyHoverAttachesSentinel(t *testing.T) {
	res := RenderDoc(
		`<body><div id="d">x</div></body>`,
		`#d { background: #eee; color: #111 } #d:hover { color: red }`,
		Options{},
	)
	el := res.ByID["d"].(*El)
	if !el.textStateActive {
		t.Error("text-color :hover should mark text-state active")
	}
	if el.Box.Hover == nil {
		t.Error("text-color :hover should attach a repaint sentinel so Box invalidates on hover")
	}
}
