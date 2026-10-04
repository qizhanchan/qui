package htmlcss

import (
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// Copying a selection that spans an <ol> / <ul> must rebuild real list
// markup on the clipboard (so a paste into Word / a browser stays a
// numbered / bulleted list), not a flat run of <div>s. The plain-text
// fallback keeps the markers.
func TestListSelectionCopiesAsListHTML(t *testing.T) {
	body := Render(
		`<body>`+
			`<ol><li>Parse the HTML</li><li>Resolve the cascade</li></ol>`+
			`<ul><li>Alpha</li><li>Beta</li></ul>`+
			`</body>`,
		``,
		Options{},
	)
	win := qui.NewTestWindow(qui.Size{W: 400, H: 400})
	win.SetRoot(body)
	body.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 400})

	// Select every content run (skip the non-selectable markers).
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

	html := fake.html
	t.Logf("clipboard html: %s", html)

	// Structure: one <ol> with two <li>, one <ul> with two <li>.
	if strings.Count(html, "<ol>") != 1 || strings.Count(html, "</ol>") != 1 {
		t.Errorf("want exactly one <ol>...</ol>, got: %s", html)
	}
	if strings.Count(html, "<ul>") != 1 || strings.Count(html, "</ul>") != 1 {
		t.Errorf("want exactly one <ul>...</ul>, got: %s", html)
	}
	if strings.Count(html, "<li>") != 4 {
		t.Errorf("want 4 <li>, got %d: %s", strings.Count(html, "<li>"), html)
	}
	if !strings.Contains(html, "Parse the HTML") || !strings.Contains(html, "Beta") {
		t.Errorf("list item text missing: %s", html)
	}
	// The <ol> must come before the <ul>, and markers must NOT leak into
	// the HTML as text (the list regenerates them).
	if strings.Index(html, "<ol>") > strings.Index(html, "<ul>") {
		t.Error("ol/ul order not preserved")
	}
	if strings.Contains(html, "1.") || strings.Contains(html, "•") {
		t.Errorf("list markers leaked into clipboard HTML: %s", html)
	}

	// Plain text keeps the markers so a plain paste still reads as a list.
	if !strings.Contains(fake.text, "1. Parse the HTML") {
		t.Errorf("plain text lost its ordered marker: %q", fake.text)
	}
	if !strings.Contains(fake.text, "• Alpha") {
		t.Errorf("plain text lost its bullet marker: %q", fake.text)
	}
}
