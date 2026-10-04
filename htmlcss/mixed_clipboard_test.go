package htmlcss

import (
	"runtime"
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
)

// capClip captures both clipboard flavors written by a copy.
type capClip struct{ plain, html string }

func (c *capClip) Get() string         { return c.plain }
func (c *capClip) Set(s string)        { c.plain, c.html = s, "" }
func (c *capClip) SetRich(p, h string) { c.plain, c.html = p, h }

// copyWholeDoc renders html+css, selects everything, and fires Cmd/Ctrl+C
// through the real window dispatch, returning the captured clipboard.
func copyWholeDoc(t *testing.T, html, css string) *capClip {
	t.Helper()
	cb := &capClip{}
	qui.SetClipboardProvider(cb)
	t.Cleanup(func() { qui.SetClipboardProvider(nil) })

	doc := RenderDoc(html, css, Options{})
	w := qui.NewTestWindow(qui.Size{W: 600, H: 400})
	w.SetRoot(doc.Root)
	doc.Root.Layout(qui.Rect{X: 0, Y: 0, W: 600, H: 400})

	var walk func(qui.Widget)
	walk = func(x qui.Widget) {
		if ts, ok := x.(qui.TextSelectable); ok {
			ts.SetSelectionRange(0, ts.SelectableLength())
		}
		if cl, ok := x.(interface{ ChildList() []qui.Widget }); ok {
			for _, c := range cl.ChildList() {
				walk(c)
			}
		}
	}
	walk(doc.Root)

	mod := qui.ModControl
	if runtime.GOOS == "darwin" {
		mod = qui.ModSuper
	}
	w.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyC, mod))
	return cb
}

// A selection spanning a rich paragraph, a list, a table, and a trailing
// paragraph rebuilds each as its own structure, in document order, with none
// swallowing its neighbours.
func TestClipboardMixedListRichTable(t *testing.T) {
	cb := copyWholeDoc(t, `<body>
		<p>Intro <strong>bold</strong> <a href="https://x.com">link</a>.</p>
		<ul><li>first</li><li>second</li></ul>
		<table><tr><th>A</th><th>B</th></tr><tr><td>1</td><td>2</td></tr></table>
		<p>Outro.</p>
	</body>`, ``)
	h := cb.html

	iIntro := strings.Index(h, "Intro")
	iUl := strings.Index(h, "<ul>")
	iTable := strings.Index(h, "<table>")
	iOutro := strings.Index(h, "Outro")
	if !(iIntro >= 0 && iIntro < iUl && iUl < iTable && iTable < iOutro) {
		t.Fatalf("structures out of order (intro=%d ul=%d table=%d outro=%d):\n%s", iIntro, iUl, iTable, iOutro, h)
	}
	// Rich paragraph keeps bold + link.
	if !strings.Contains(h, "font-weight:700") || !strings.Contains(h, `href="https://x.com"`) {
		t.Errorf("rich paragraph lost bold/link:\n%s", h)
	}
	// List rebuilt, table rebuilt with a header cell.
	if !strings.Contains(h, "<li>") || !strings.Contains(h, "<th>") {
		t.Errorf("list/table structure missing:\n%s", h)
	}
	// The table must not absorb the following paragraph.
	tbl := h[iTable : strings.Index(h, "</table>")+len("</table>")]
	if strings.Contains(tbl, "Outro") {
		t.Errorf("table absorbed the trailing paragraph:\n%s", tbl)
	}
	// Plain flavor: list markers + table TSV, in order.
	for _, want := range []string{"first", "second", "A\tB", "1\t2", "Outro."} {
		if !strings.Contains(cb.plain, want) {
			t.Errorf("plain flavor missing %q:\n%s", want, cb.plain)
		}
	}
}

// An empty cell keeps its column: the rebuilt table (both flavors) stays
// aligned rather than going ragged.
func TestClipboardTableEmptyCellAligned(t *testing.T) {
	cb := copyWholeDoc(t, `<body><table>
		<tr><td>a</td><td></td><td>c</td></tr>
		<tr><td>d</td><td>e</td><td>f</td></tr>
	</table></body>`, ``)

	if cb.plain != "a\t\tc\nd\te\tf" {
		t.Errorf("plain TSV = %q, want %q (empty middle cell preserved)", cb.plain, "a\t\tc\nd\te\tf")
	}
	// 6 cells total (3 per row), so columns line up.
	if n := strings.Count(cb.html, "<td"); n != 6 {
		t.Errorf("got %d <td> cells, want 6 (aligned 3+3):\n%s", n, cb.html)
	}
}
