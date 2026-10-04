package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// An ancestor-:hover rule (`.row:hover .del`) must reveal / restyle a
// DESCENDANT while the ANCESTOR is hovered — not only while the descendant
// itself is — and must revert cleanly when the ancestor is un-hovered. It
// must also register the row as a state trigger with the engine so the
// row's hover boundary actually repaints the dependents, otherwise the
// swap only shows on the next unrelated repaint.
func TestAncestorHoverRevealsAndHidesDescendant(t *testing.T) {
	root := Render(
		`<body><div class="row">`+
			`<div class="del"></div>`+
			`<div class="txt">x</div>`+
			`</div></body>`,
		`.row { width: 200px; height: 40px; padding: 4px; }
		 .del { width: 20px; height: 20px; visibility: hidden; background: #00ff00; }
		 .txt { color: #000000; }
		 .row:hover .del { visibility: visible; background: #ff0000; }
		 .row:hover .txt { color: #ff0000; }`,
		Options{},
	)

	rowEl := findElByClassInTree(root, "row")
	delEl := findElByClassInTree(root, "del")
	txtEl := findElByClassInTree(root, "txt")
	if rowEl == nil || delEl == nil || txtEl == nil {
		t.Fatalf("missing els: row=%v del=%v txt=%v", rowEl, delEl, txtEl)
	}

	// The row — and ONLY the row (not the window-root body) — is recorded as
	// the hover trigger, and registered with the engine so the row's hover
	// boundary invalidates the dependents.
	for _, dep := range []*El{delEl, txtEl} {
		if len(dep.stateTriggers) != 1 || dep.stateTriggers[0].el != rowEl || !dep.stateTriggers[0].hover {
			t.Errorf("%s stateTriggers = %+v, want exactly the .row as a hover trigger",
				dep.attrs["class"], dep.stateTriggers)
		}
	}
	eng := delEl.engine
	if eng == nil || !eng.stateDeps[rowEl][delEl] || !eng.stateDeps[rowEl][txtEl] {
		t.Error("engine.stateDeps[row] missing del/txt — row's hover boundary won't repaint them")
	}

	root.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 40})

	// drawFills draws e (which runs its per-frame state swap) and returns the
	// number of fills recorded.
	drawFills := func(e *El) int {
		var rec qui.RecordingCanvas
		e.Draw(&rec)
		return len(rec.Fills)
	}
	txtColor := func() qui.Color {
		var rec qui.RecordingCanvas
		txtEl.Draw(&rec) // Draw runs applyStateText, which sets the label color
		return txtEl.textLabel.Style().Foreground
	}

	// Resting: del is visibility:hidden → paints nothing; txt is black.
	if n := drawFills(delEl); n != 0 {
		t.Errorf("resting del fills = %d, want 0 (hidden)", n)
	}
	if got, want := txtColor(), (qui.Color{A: 1}); got != want {
		t.Errorf("resting txt foreground = %+v, want black", got)
	}

	// Hover the ROW (ancestor) — cursor never touches del/txt themselves.
	rowEl.Handle(qui.NewMouseEvent(qui.EventMouseEnter, 2, 2, 0, 0))
	if delEl.Hovering() {
		t.Fatal("del reported hovered; the test must exercise ANCESTOR hover only")
	}
	if n := drawFills(delEl); n == 0 {
		t.Error("del painted nothing while row hovered; ancestor-hover reveal failed")
	}
	if got, want := txtColor(), (qui.Color{R: 1, A: 1}); got != want {
		t.Errorf("hovered txt foreground = %+v, want red", got)
	}

	// Un-hover the row — the descendant reverts (the "disappear" path).
	rowEl.Handle(qui.NewMouseEvent(qui.EventMouseLeave, 2, 2, 0, 0))
	if n := drawFills(delEl); n != 0 {
		t.Errorf("del fills after row leave = %d, want 0 (should hide again)", n)
	}
	if got, want := txtColor(), (qui.Color{A: 1}); got != want {
		t.Errorf("txt foreground after row leave = %+v, want black again", got)
	}
}

// A self-:hover rule (`.btn:hover`) must NOT fire from an ancestor's hover,
// and an element with no ancestor-hover dependency must not force its
// ancestors to repaint on every hover.
func TestSelfHoverNotTriggeredByAncestor(t *testing.T) {
	root := Render(
		`<body><div class="row"><div class="btn">x</div></div></body>`,
		`.row { width: 200px; height: 40px; }
		 .btn { width: 40px; height: 20px; color: #000000; }
		 .btn:hover { color: #ff0000; }`,
		Options{},
	)
	rowEl := findElByClassInTree(root, "row")
	btnEl := findElByClassInTree(root, "btn")
	if rowEl == nil || btnEl == nil {
		t.Fatalf("missing els: row=%v btn=%v", rowEl, btnEl)
	}
	// No ancestor-hover dependency → no trigger links recorded.
	if len(btnEl.stateTriggers) != 0 {
		t.Errorf("btn.stateTriggers = %+v for a self-:hover-only sheet (over-linking)", btnEl.stateTriggers)
	}

	root.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 40})
	var rec qui.RecordingCanvas
	btnEl.Draw(&rec)

	// Hovering the ancestor must NOT recolor the button (its :hover is its own).
	rowEl.Handle(qui.NewMouseEvent(qui.EventMouseEnter, 2, 2, 0, 0))
	btnEl.Draw(&rec)
	if got := btnEl.textLabel.Style().Foreground; got != (qui.Color{A: 1}) {
		t.Errorf("btn recolored on ancestor hover = %+v, want black (self :hover only)", got)
	}
}
