package htmlcss

import (
	"strings"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// A paragraph of mixed inline content folds into a single InlineBox. Its
// text must be selectable by a press-drag inside that one box — the
// window's cross-widget controller defers intra-widget drags to the
// anchor's own handler, which the InlineBox now supplies. Regression
// guard for "the <p class=lead> text can't be selected".
func TestInlineBoxIntraWidgetSelection(t *testing.T) {
	body := Render(
		`<body><p class="lead">This entire page is real <strong>HTML</strong> `+
			`parsed and <a href="https://example.com">rendered</a> by the engine.</p></body>`,
		`.lead{font-size:16px;}`,
		Options{},
	)

	viewportW := float32(600)
	h := body.Measure(qui.Size{W: viewportW, H: 0})
	win := qui.NewTestWindow(qui.Size{W: viewportW, H: 400})
	win.SetRoot(body)
	body.Layout(qui.Rect{X: 0, Y: 0, W: viewportW, H: h.H})

	var lead *widgets.InlineBox
	var walk func(w qui.Widget)
	walk = func(w qui.Widget) {
		if ib, ok := w.(*widgets.InlineBox); ok && strings.Contains(ib.Text(), "entire") {
			lead = ib
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, ch := range c.ChildList() {
				walk(ch)
			}
		}
	}
	walk(body)
	if lead == nil {
		t.Fatal("mixed-inline <p> did not fold into an InlineBox")
	}
	if !lead.Focusable() {
		t.Error("selectable InlineBox should be Focusable")
	}

	b := lead.Bounds()
	if b.W <= 0 || b.H <= 0 {
		t.Fatalf("InlineBox not laid out: %+v", b)
	}

	// Press-drag across the first line through the real window dispatch.
	y := b.Y + 4
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, b.X+2, y, qui.MouseButtonLeft, 0))
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseMove, b.X+b.W-2, y, qui.MouseButtonLeft, 0))
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, b.X+b.W-2, y, qui.MouseButtonLeft, 0))

	got := lead.SelectedText()
	if got == "" {
		t.Fatal("drag over the paragraph produced no selection")
	}
	if !strings.HasPrefix(got, "This entire page") {
		t.Errorf("selection = %q, want it to start with the paragraph text", got)
	}

	// Cmd/Ctrl+C copies the selection to the clipboard. Select the whole
	// box so the copy covers the folded <a href> too.
	lead.SetSelectionRange(0, lead.SelectableLength())
	fake := &fakeClipboard{}
	qui.SetClipboardProvider(fake)
	defer qui.SetClipboardProvider(nil)
	mod := qui.ModSuper
	win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyC, mod))
	if fake.text == "" {
		// Non-darwin uses Ctrl; retry so the test is platform-agnostic.
		win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyC, qui.ModControl))
	}
	if !strings.Contains(fake.text, "This entire page") {
		t.Errorf("clipboard after Cmd+C = %q, want the selected text", fake.text)
	}
	// The folded <a href> must survive into the HTML clipboard flavor so a
	// paste into Word / a browser keeps the "rendered" link.
	if !strings.Contains(fake.html, `<a href="https://example.com">`) {
		t.Errorf("clipboard HTML dropped the hyperlink: %s", fake.html)
	}
	if !strings.Contains(fake.html, "rendered") {
		t.Errorf("clipboard HTML missing link text: %s", fake.html)
	}

	// A plain click (no drag) must NOT be consumed by the InlineBox, so the
	// parent El's link / onClick handling still fires on the same MouseUp.
	lead.ClearTextSelection()
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, b.X+4, y, qui.MouseButtonLeft, 0))
	consumed := lead.Handle(qui.NewMouseEvent(qui.EventMouseUp, b.X+4, y, qui.MouseButtonLeft, 0))
	if consumed {
		t.Error("a plain click (no drag) should not be consumed by the InlineBox")
	}
}

// Double-click on a word inside a folded paragraph selects that word;
// triple-click selects the whole paragraph. Granularity comes from the
// window's selection controller via qui.SelectionGranular — regression
// guard for "mixed-inline paragraphs can't be double-click selected".
func TestInlineBoxMultiClickGranularity(t *testing.T) {
	body := Render(
		`<body><p class="lead">alpha beta <strong>gamma</strong> delta</p></body>`,
		`.lead{font-size:16px;}`,
		Options{},
	)
	viewportW := float32(600)
	h := body.Measure(qui.Size{W: viewportW, H: 0})
	win := qui.NewTestWindow(qui.Size{W: viewportW, H: 400})
	win.SetRoot(body)
	body.Layout(qui.Rect{X: 0, Y: 0, W: viewportW, H: h.H})

	var lead *widgets.InlineBox
	var walk func(w qui.Widget)
	walk = func(w qui.Widget) {
		if ib, ok := w.(*widgets.InlineBox); ok && strings.Contains(ib.Text(), "alpha") {
			lead = ib
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, ch := range c.ChildList() {
				walk(ch)
			}
		}
	}
	walk(body)
	if lead == nil {
		t.Fatal("paragraph did not fold into an InlineBox")
	}
	b := lead.Bounds()
	// A point inside "beta": past "alpha " plus a bit.
	x := b.X + 2
	full := lead.Text()
	idx := strings.Index(full, "beta")
	for _, r := range full[:idx+2] {
		x += qui.RuneAdvance(r, lead.Style().Font)
	}
	y := b.Y + 4

	base := time.UnixMilli(1000)
	click := func(n int) {
		for i := 0; i < n; i++ {
			d := qui.NewMouseEvent(qui.EventMouseDown, x, y, qui.MouseButtonLeft, 0)
			d.When = base.Add(time.Duration(i*90) * time.Millisecond)
			win.DispatchTestEvent(d)
			u := qui.NewMouseEvent(qui.EventMouseUp, x, y, qui.MouseButtonLeft, 0)
			u.When = d.When.Add(10 * time.Millisecond)
			win.DispatchTestEvent(u)
		}
	}

	click(2)
	if got := lead.SelectedText(); got != "beta" {
		t.Fatalf("double-click selected %q, want %q", got, "beta")
	}

	base = base.Add(5 * time.Second) // fresh gesture
	click(3)
	if got := lead.SelectedText(); !strings.Contains(got, "alpha beta") ||
		!strings.Contains(got, "delta") {
		t.Fatalf("triple-click selected %q, want the whole paragraph", got)
	}
}

// Word granularity must hold on soft-wrapped lines too: the InlineBox's
// selection offset space threads '\n' between visual lines, and the word
// expansion must not leak across them (regression: double-click at the
// middle of a 3-line paragraph selected the entire box).
func TestInlineBoxDoubleClickWordOnWrappedLine(t *testing.T) {
	body := Render(
		`<body><p class="lead">This entire page is real <strong>HTML</strong> parsed by golang.org/x/net/html, styled with real CSS, and rendered into the retained qui widget tree — no browser, no WebView. Read more in the project docs.</p></body>`,
		`.lead{font-size:16px;}`,
		Options{},
	)
	viewportW := float32(870)
	h := body.Measure(qui.Size{W: viewportW, H: 0})
	win := qui.NewTestWindow(qui.Size{W: viewportW, H: 400})
	win.SetRoot(body)
	body.Layout(qui.Rect{X: 0, Y: 0, W: viewportW, H: h.H})

	var lead *widgets.InlineBox
	var walk func(w qui.Widget)
	walk = func(w qui.Widget) {
		if ib, ok := w.(*widgets.InlineBox); ok && strings.Contains(ib.Text(), "entire") {
			lead = ib
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, ch := range c.ChildList() {
				walk(ch)
			}
		}
	}
	walk(body)
	if lead == nil {
		t.Fatal("paragraph did not fold into an InlineBox")
	}
	b := lead.Bounds()
	// Center of the box lands on a middle visual line.
	cx, cy := b.X+b.W/2, b.Y+b.H/2
	base := time.UnixMilli(1000)
	for i := 0; i < 2; i++ {
		d := qui.NewMouseEvent(qui.EventMouseDown, cx, cy, qui.MouseButtonLeft, 0)
		d.When = base.Add(time.Duration(i*90) * time.Millisecond)
		win.DispatchTestEvent(d)
		u := qui.NewMouseEvent(qui.EventMouseUp, cx, cy, qui.MouseButtonLeft, 0)
		u.When = d.When.Add(10 * time.Millisecond)
		win.DispatchTestEvent(u)
	}
	got := lead.SelectedText()
	if got == "" || strings.ContainsAny(got, " \n") {
		t.Fatalf("double-click at box center selected %q — want a single word", got)
	}
}

// fakeClipboard captures the last plain-text + HTML write for assertions.
type fakeClipboard struct{ text, html string }

func (c *fakeClipboard) Get() string         { return c.text }
func (c *fakeClipboard) Set(text string)     { c.text = text; c.html = "" }
func (c *fakeClipboard) SetRich(p, h string) { c.text = p; c.html = h }
