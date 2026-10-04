package htmlcss

import (
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// A local <img> must be inlined as a base64 data URI when copied (so the
// picture survives a paste into another app), while a remote http(s) src is
// kept verbatim.
func TestImageClipboardSrc(t *testing.T) {
	body := Render(
		`<body><p class="d">a `+
			`<img class="ico" src="icons/add.svg"> b `+
			`<img class="ico" src="https://example.com/x.png"> c</p></body>`,
		`.d{font-size:16px;} .ico{width:18px;height:18px;}`,
		Options{BaseDir: "../examples/html-css"},
	)
	win := qui.NewTestWindow(qui.Size{W: 500, H: 200})
	win.SetRoot(body)
	body.Layout(qui.Rect{X: 0, Y: 0, W: 500, H: body.Measure(qui.Size{W: 500}).H})

	var box *widgets.InlineBox
	var walk func(w qui.Widget)
	walk = func(w qui.Widget) {
		if ib, ok := w.(*widgets.InlineBox); ok && strings.Contains(ib.Text(), "a ") {
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
		t.Fatal("InlineBox not found")
	}
	box.SetSelectionRange(0, box.SelectableLength())

	fake := &fakeClipboard{}
	qui.SetClipboardProvider(fake)
	defer qui.SetClipboardProvider(nil)
	win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyC, qui.ModSuper))
	if fake.html == "" {
		win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyC, qui.ModControl))
	}

	html := fake.html
	if !strings.Contains(html, `<img src="data:image/png;base64,`) {
		t.Errorf("local svg was not inlined as a data URI: %s", html)
	}
	if strings.Contains(html, `src="icons/add.svg"`) {
		t.Errorf("local svg leaked its on-disk path: %s", html)
	}
	if !strings.Contains(html, `<img src="https://example.com/x.png">`) {
		t.Errorf("remote src should be kept verbatim: %s", html)
	}
}
