package htmlcss

import (
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// Selecting a paragraph whose inline flow mixes text with atomic inline
// boxes — inline-block badges and an inline <img> — must include those
// atoms in the copy: the badge text (NEW / BETA) in both plain + HTML, and
// the image as an <img src> in the HTML flavor.
func TestInlineAtomsCopyWithText(t *testing.T) {
	body := Render(
		`<body><p class="d">Ship it `+
			`<img class="ico" src="icons/add.svg"> with an `+
			`<span class="tag">NEW</span> and a <span class="tag">BETA</span> badge.</p></body>`,
		`.d{font-size:16px;} .ico{width:18px;height:18px;} `+
			`.tag{display:inline-block;padding:1px 8px;background:#eee;color:#137333;}`,
		Options{},
	)
	win := qui.NewTestWindow(qui.Size{W: 500, H: 200})
	win.SetRoot(body)
	body.Layout(qui.Rect{X: 0, Y: 0, W: 500, H: body.Measure(qui.Size{W: 500}).H})

	var box *widgets.InlineBox
	var walk func(w qui.Widget)
	walk = func(w qui.Widget) {
		if ib, ok := w.(*widgets.InlineBox); ok && strings.Contains(ib.Text(), "Ship") {
			box = ib
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, ch := range c.ChildList() {
				walk(ch)
			}
		}
	}
	walk(body)
	if box == nil {
		t.Fatal("inline-demo InlineBox not found")
	}

	// The atoms extend the box's own selectable length beyond its text runs.
	if box.SelectableLength() <= len([]rune(box.Text())) {
		t.Errorf("SelectableLength %d should exceed text-only length %d (atoms not counted)",
			box.SelectableLength(), len([]rune(box.Text())))
	}

	box.SetSelectionRange(0, box.SelectableLength())

	sel := box.SelectedText()
	t.Logf("plain: %q", sel)
	if !strings.Contains(sel, "NEW") || !strings.Contains(sel, "BETA") {
		t.Errorf("plain copy missing badge text: %q", sel)
	}

	fake := &fakeClipboard{}
	qui.SetClipboardProvider(fake)
	defer qui.SetClipboardProvider(nil)
	win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyC, qui.ModSuper))
	if fake.html == "" {
		win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyC, qui.ModControl))
	}
	t.Logf("html: %s", fake.html)
	if !strings.Contains(fake.html, "NEW") || !strings.Contains(fake.html, "BETA") {
		t.Errorf("HTML copy missing badge text: %s", fake.html)
	}
	if !strings.Contains(fake.html, `<img src=`) {
		t.Errorf("HTML copy missing the inline image: %s", fake.html)
	}
}
