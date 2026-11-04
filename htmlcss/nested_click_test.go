package htmlcss

import (
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
)

// nestedRow lays out a tab-shaped row — a label and a ✕ button inside a
// clickable row — and returns a clicker for it.
func nestedRow(t *testing.T) (res RenderResult, win *qui.Window, click func(w qui.Widget, button qui.MouseButton)) {
	t.Helper()
	res = RenderDoc(
		`<body><div id="row"><span id="label">Tab</span>`+
			`<button id="close">x</button></div></body>`,
		`#row { display: flex; gap: 8px; padding: 6px; }
		 #close { width: 20px; height: 20px; }`,
		Options{})
	root := res.Root
	win = qui.NewTestWindow(qui.Size{W: 300, H: 100})
	win.SetRoot(root)
	root.Measure(qui.Size{W: 300, H: 0})
	root.Layout(qui.Rect{X: 0, Y: 0, W: 300, H: 100})
	click = func(w qui.Widget, button qui.MouseButton) {
		b := w.Bounds()
		x, y := b.X+b.W/2, b.Y+b.H/2
		win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, x, y, button, 0))
		win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, x, y, button, 0))
	}
	return res, win, click
}

// A click bubbles, as in Chrome: a ✕ inside a clickable tab row runs its
// own handler and then the row's.
func TestNestedClickBubbles(t *testing.T) {
	res, _, click := nestedRow(t)
	row, closeBtn := res.ByID["row"].(*El), res.ByID["close"].(*El)
	var log []string
	row.SetOnClick(func() { log = append(log, "row") })
	closeBtn.SetOnClick(func() { log = append(log, "close") })

	click(closeBtn, qui.MouseButtonLeft)
	if got := strings.Join(log, ","); got != "close,row" {
		t.Fatalf("click on ✕ ran %q, want close,row", got)
	}
	log = nil
	click(res.ByID["label"], qui.MouseButtonLeft)
	if got := strings.Join(log, ","); got != "row" {
		t.Fatalf("click on the label ran %q, want row", got)
	}
}

// SetStopClickPropagation (h: StopClickPropagation) keeps the click on the
// ✕ — the browser-tab shape: the ✕ closes without activating the row.
func TestStopClickPropagationBarrier(t *testing.T) {
	res, _, click := nestedRow(t)
	row, closeBtn := res.ByID["row"].(*El), res.ByID["close"].(*El)
	var rowClicks, closeClicks, rowMenus int
	row.SetOnClick(func() { rowClicks++ })
	row.SetOnContextMenu(func(x, y float32) { rowMenus++ })
	closeBtn.SetOnClick(func() { closeClicks++ })
	closeBtn.SetStopClickPropagation(true)

	click(closeBtn, qui.MouseButtonLeft)
	if closeClicks != 1 || rowClicks != 0 {
		t.Fatalf("✕ click: close %d row %d, want 1 0", closeClicks, rowClicks)
	}
	// The barrier covers the context menu too.
	click(closeBtn, qui.MouseButtonRight)
	if rowMenus != 0 {
		t.Fatalf("right-click on the ✕ opened the row's menu")
	}
	click(res.ByID["label"], qui.MouseButtonRight)
	if rowMenus != 1 {
		t.Fatalf("right-click on the label: %d row menus, want 1", rowMenus)
	}
	// Keyboard activation of the ✕ stops there as well.
	closeBtn.Click()
	if closeClicks != 2 || rowClicks != 0 {
		t.Fatalf("✕ Click(): close %d row %d, want 2 0", closeClicks, rowClicks)
	}
}

// PointerEvent.StopPropagation from an OnClickEvent handler stops the
// click, but not the release: the row's pointerup listener still hears it.
func TestPointerEventStopPropagation(t *testing.T) {
	res, _, click := nestedRow(t)
	row, closeBtn := res.ByID["row"].(*El), res.ByID["close"].(*El)
	var rowClicks, rowUps int
	row.SetOnClick(func() { rowClicks++ })
	row.SetOnPointerUp(func(PointerEvent) bool { rowUps++; return false })
	closeBtn.SetOnClickEvent(func(ev PointerEvent) { ev.StopPropagation() })

	click(closeBtn, qui.MouseButtonLeft)
	if rowClicks != 0 {
		t.Fatalf("a stopped click reached the row")
	}
	if rowUps != 1 {
		t.Fatalf("row pointerup heard %d releases, want 1", rowUps)
	}
}

// The synthetic click of a keyboard activation bubbles like a real one.
func TestSyntheticClickBubbles(t *testing.T) {
	res, _, _ := nestedRow(t)
	row, closeBtn := res.ByID["row"].(*El), res.ByID["close"].(*El)
	var rowClicks int
	row.SetOnClick(func() { rowClicks++ })
	closeBtn.Click()
	if rowClicks != 1 {
		t.Fatalf("Click() on the ✕ reached the row %d times, want 1", rowClicks)
	}
}

// Contextmenu bubbles: every handler up the path runs, innermost first.
func TestContextMenuBubbles(t *testing.T) {
	res, _, click := nestedRow(t)
	row, closeBtn := res.ByID["row"].(*El), res.ByID["close"].(*El)
	var log []string
	row.SetOnContextMenu(func(x, y float32) { log = append(log, "row") })
	closeBtn.SetOnContextMenu(func(x, y float32) { log = append(log, "close") })
	click(closeBtn, qui.MouseButtonRight)
	if got := strings.Join(log, ","); got != "close,row" {
		t.Fatalf("right-click on ✕ ran %q, want close,row", got)
	}
}

// AuditClickNesting finds a handler (or a form control) inside another
// with no barrier between, and nothing once the barrier is declared.
func TestAuditClickNesting(t *testing.T) {
	res, _, _ := nestedRow(t)
	row, closeBtn := res.ByID["row"].(*El), res.ByID["close"].(*El)
	row.SetOnClick(func() {})
	closeBtn.SetOnClick(func() {})
	closeBtn.SetClickSite("app/tabs.go:42")
	got := AuditClickNesting(res.Root.(*El).TopEl())
	if len(got) != 1 || got[0].Kind != "click" || !strings.Contains(got[0].Inner, "app/tabs.go:42") ||
		!strings.Contains(got[0].Outer, "#row") {
		t.Fatalf("audit = %v, want the ✕ inside #row", got)
	}
	closeBtn.SetStopClickPropagation(true)
	if got := AuditClickNesting(res.Root.(*El).TopEl()); len(got) != 0 {
		t.Fatalf("audit with a barrier = %v, want none", got)
	}
}

// A handler declared to hear nested clicks isn't an audit finding.
func TestAuditSkipsHearsNestedClicks(t *testing.T) {
	res, _, _ := nestedRow(t)
	row, closeBtn := res.ByID["row"].(*El), res.ByID["close"].(*El)
	row.SetOnClick(func() {})
	closeBtn.SetOnClick(func() {})
	row.SetHearsNestedClicks(true)
	if got := AuditClickNesting(res.Root.(*El).TopEl()); len(got) != 0 {
		t.Fatalf("audit = %v, want none under a HearsNestedClicks row", got)
	}
}
