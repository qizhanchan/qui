package widgets

import (
	"strings"
	"testing"
	"time"

	. "github.com/qizhanchan/qui"
)

// buildTwoLabelColumn lays out two single-line labels stacked vertically
// inside a container root, returning the window + both labels with their
// bounds already assigned (label0 at y 0..20, label1 at y 20..40).
func buildTwoLabelColumn(t *testing.T, a, b string) (*Window, *Label, *Label) {
	t.Helper()
	w := NewTestWindow(Size{W: 240, H: 60})
	l0 := NewLabel(a)
	l1 := NewLabel(b)
	col := NewContainer(FlexLayout{Direction: Vertical}, l0, l1)
	w.SetRoot(col)
	col.Layout(Rect{X: 0, Y: 0, W: 240, H: 40})
	l0.Layout(Rect{X: 0, Y: 0, W: 240, H: 20})
	l1.Layout(Rect{X: 0, Y: 20, W: 240, H: 20})
	col.ClearLayoutDirty()
	w.ClearDirtyRegion()
	return w, l0, l1
}

func TestCrossWidgetSelectionDragAndCopy(t *testing.T) {
	fc := withFakeClipboard(t)
	w, l0, l1 := buildTwoLabelColumn(t, "hello world", "second line")

	// Press at the far left of label0 (offset 0), drag past the end of
	// label1, release. The whole run across both labels should select.
	w.DispatchTestEvent(NewMouseEvent(EventMouseDown, 0, 10, MouseButtonLeft, 0))
	w.DispatchTestEvent(NewMouseEvent(EventMouseMove, 1000, 30, MouseButtonLeft, 0))
	w.DispatchTestEvent(NewMouseEvent(EventMouseUp, 1000, 30, MouseButtonLeft, 0))

	if got := l0.SelectedText(); got != "hello world" {
		t.Fatalf("label0 selected %q, want %q", got, "hello world")
	}
	if got := l1.SelectedText(); got != "second line" {
		t.Fatalf("label1 selected %q, want %q", got, "second line")
	}

	// Window-level Cmd/Ctrl+C aggregates both in document order.
	w.DispatchTestEvent(newCmdKeyDown(KeyC))
	if fc.buf != "hello world\nsecond line" {
		t.Fatalf("clipboard = %q, want %q", fc.buf, "hello world\nsecond line")
	}
}

func TestCrossWidgetSelectionClearedByFreshClick(t *testing.T) {
	_ = withFakeClipboard(t)
	w, l0, l1 := buildTwoLabelColumn(t, "hello world", "second line")

	w.DispatchTestEvent(NewMouseEvent(EventMouseDown, 0, 10, MouseButtonLeft, 0))
	w.DispatchTestEvent(NewMouseEvent(EventMouseMove, 1000, 30, MouseButtonLeft, 0))
	w.DispatchTestEvent(NewMouseEvent(EventMouseUp, 1000, 30, MouseButtonLeft, 0))
	if l1.SelectedText() == "" {
		t.Fatalf("precondition: label1 should be selected after cross drag")
	}

	// A fresh single click anywhere drops the prior selection. Stamp the
	// event well after the drag so it isn't coalesced into a double-click
	// (which would word-select under the cursor).
	down := NewMouseEvent(EventMouseDown, 40, 10, MouseButtonLeft, 0)
	down.When = time.UnixMilli(100000)
	up := NewMouseEvent(EventMouseUp, 40, 10, MouseButtonLeft, 0)
	up.When = time.UnixMilli(100010)
	w.DispatchTestEvent(down)
	w.DispatchTestEvent(up)
	if got := l1.SelectedText(); got != "" {
		t.Fatalf("label1 still selected %q after fresh click; want cleared", got)
	}
	if got := l0.SelectedText(); got != "" {
		t.Fatalf("label0 still selected %q after fresh click; want cleared", got)
	}
}

func TestCrossWidgetSelectAll(t *testing.T) {
	fc := withFakeClipboard(t)
	w, l0, l1 := buildTwoLabelColumn(t, "hello world", "second line")

	// Focus a label (as a click in the document would), then Cmd/Ctrl+A.
	w.SetFocus(l0)
	w.DispatchTestEvent(newCmdKeyDown(KeyA))

	if got := l0.SelectedText(); got != "hello world" {
		t.Fatalf("label0 selected %q after select-all, want full", got)
	}
	if got := l1.SelectedText(); got != "second line" {
		t.Fatalf("label1 selected %q after select-all, want full", got)
	}
	w.DispatchTestEvent(newCmdKeyDown(KeyC))
	if fc.buf != "hello world\nsecond line" {
		t.Fatalf("clipboard = %q after select-all+copy", fc.buf)
	}
}

func TestSelectAllIgnoredWithoutSelectableFocus(t *testing.T) {
	_ = withFakeClipboard(t)
	w, l0, l1 := buildTwoLabelColumn(t, "hello world", "second line")

	// No selectable focused → window must NOT take over Cmd+A.
	w.SetFocus(nil)
	w.DispatchTestEvent(newCmdKeyDown(KeyA))
	if l0.SelectedText() != "" || l1.SelectedText() != "" {
		t.Fatalf("select-all should be inert without a TextSelectable focus")
	}
}

// richClip records both the plain and HTML flavors so rich-copy can be
// asserted without a real NSPasteboard.
type richClip struct {
	plain string
	html  string
}

func (c *richClip) Get() string                { return c.plain }
func (c *richClip) Set(text string)            { c.plain = text }
func (c *richClip) SetRich(plain, html string) { c.plain, c.html = plain, html }

func TestCrossWidgetCopyEmitsRichHTML(t *testing.T) {
	rc := &richClip{}
	SetClipboardProvider(rc)
	t.Cleanup(func() { SetClipboardProvider(nil) })

	w := NewTestWindow(Size{W: 240, H: 60})
	// l0 is a bold 27px "heading"; l1 is a paragraph with a bold word.
	l0 := NewLabel("")
	headFont := Font{Size: 27, Weight: FontWeightBold}
	l0.SetSpans([]TextSpan{{Text: "Heading", Font: &headFont}})
	l1 := NewLabel("")
	boldFont := Font{Size: 15, Weight: FontWeightBold}
	linkColor := Color{R: 0.42, G: 0.62, B: 0.96, A: 1}
	l1.SetSpans([]TextSpan{
		{Text: "a "},
		{Text: "bold", Font: &boldFont},
		{Text: " "},
		{Text: "link", Color: &linkColor, Href: "https://example.com/q"},
	})
	col := NewContainer(FlexLayout{Direction: Vertical}, l0, l1)
	w.SetRoot(col)
	col.Layout(Rect{X: 0, Y: 0, W: 240, H: 40})
	l0.Layout(Rect{X: 0, Y: 0, W: 240, H: 20})
	l1.Layout(Rect{X: 0, Y: 20, W: 240, H: 20})

	w.DispatchTestEvent(NewMouseEvent(EventMouseDown, 0, 10, MouseButtonLeft, 0))
	w.DispatchTestEvent(NewMouseEvent(EventMouseMove, 1000, 30, MouseButtonLeft, 0))
	w.DispatchTestEvent(NewMouseEvent(EventMouseUp, 1000, 30, MouseButtonLeft, 0))
	w.DispatchTestEvent(newCmdKeyDown(KeyC))

	if rc.plain != "Heading\na bold link" {
		t.Fatalf("plain = %q", rc.plain)
	}
	for _, want := range []string{
		"font-weight:700", "font-size:27", "Heading", ">bold<",
		`<a href="https://example.com/q">`,
	} {
		if !strings.Contains(rc.html, want) {
			t.Fatalf("html %q missing %q", rc.html, want)
		}
	}
}

func TestSingleWidgetSelectionLeftToWidgetCopy(t *testing.T) {
	fc := withFakeClipboard(t)
	w, l0, _ := buildTwoLabelColumn(t, "hello world", "second line")
	l0.SetFocused(true)

	// A drag fully inside label0 stays single-widget; the window-level
	// aggregator must NOT take over (fewer than two contributors), so the
	// label's own Cmd+C still copies just its selection.
	w.DispatchTestEvent(NewMouseEvent(EventMouseDown, 0, 10, MouseButtonLeft, 0))
	w.DispatchTestEvent(NewMouseEvent(EventMouseMove, 1000, 10, MouseButtonLeft, 0))
	w.DispatchTestEvent(NewMouseEvent(EventMouseUp, 1000, 10, MouseButtonLeft, 0))

	if got := l0.SelectedText(); got != "hello world" {
		t.Fatalf("label0 selected %q, want full line", got)
	}
	w.DispatchTestEvent(newCmdKeyDown(KeyC))
	if fc.buf != "hello world" {
		t.Fatalf("clipboard = %q, want %q", fc.buf, "hello world")
	}
}
