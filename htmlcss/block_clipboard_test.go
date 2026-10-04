package htmlcss

import (
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// Copying a selection that spans a heading and a paragraph must keep the
// semantic block tags on the clipboard (qui.ClipboardBlock) — a pasted
// <h2> stays a heading in Word / Google Docs instead of flattening to a
// <div>. Non-semantic blocks (a bare <div>) keep the generic wrapper.
func TestBlockSelectionCopiesSemanticTags(t *testing.T) {
	body := Render(
		`<body>`+
			`<h2>Section title</h2>`+
			`<p>Body text with <strong>emphasis</strong> inline.</p>`+
			`<div>plain box</div>`+
			`</body>`,
		``,
		Options{},
	)
	win := qui.NewTestWindow(qui.Size{W: 500, H: 400})
	win.SetRoot(body)
	body.Layout(qui.Rect{X: 0, Y: 0, W: 500, H: 400})

	var walk func(w qui.Widget)
	walk = func(w qui.Widget) {
		switch t2 := w.(type) {
		case *widgets.Label:
			if t2.Selectable && t2.SelectableLength() > 0 {
				t2.SetSelectionRange(0, t2.SelectableLength())
			}
		case *widgets.InlineBox:
			if t2.SelectableLength() > 0 {
				t2.SetSelectionRange(0, t2.SelectableLength())
			}
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, ch := range c.ChildList() {
				walk(ch)
			}
		}
	}
	walk(body)

	fake := &fakeClipboard{}
	qui.SetClipboardProvider(fake)
	defer qui.SetClipboardProvider(nil)
	win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyC, qui.ModSuper))
	if fake.html == "" {
		win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyC, qui.ModControl))
	}

	for _, want := range []string{"<h2>", "Section title", "</h2>", "<p>", "</p>", "<div>", "plain box", "</div>"} {
		if !strings.Contains(fake.html, want) {
			t.Errorf("clipboard HTML missing %q:\n%s", want, fake.html)
		}
	}
	// The paragraph's inline styling still rides inside the <p>.
	if !strings.Contains(fake.html, "font-weight:700") {
		t.Errorf("clipboard HTML lost the <strong> styling:\n%s", fake.html)
	}
	// Plain flavor: one line per block.
	lines := strings.Split(fake.text, "\n")
	if len(lines) != 3 {
		t.Errorf("plain flavor = %q, want 3 lines (h2 / p / div)", fake.text)
	}
}
