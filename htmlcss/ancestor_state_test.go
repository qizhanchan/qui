package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// drawFillCount draws e on a fresh recording canvas (running its per-frame
// state resolution) and returns how many fills it painted.
func drawFillCount(e *El) int {
	var rec qui.RecordingCanvas
	e.Draw(&rec)
	return len(rec.Fills)
}

// A sibling-:hover rule (`.a:hover ~ .b`, `.a:hover + .b`) must reveal /
// restyle a FOLLOWING SIBLING while the named sibling is hovered — the
// trigger is beside the dependent, not above it — and revert on leave.
func TestSiblingHoverReveal(t *testing.T) {
	for _, comb := range []string{"~", "+"} {
		root := Render(
			`<body><div class="wrap">`+
				`<div class="a"></div>`+
				`<div class="b"></div>`+
				`</div></body>`,
			`.wrap { width: 200px; height: 40px; }
			 .a { width: 20px; height: 20px; background: #0000ff; }
			 .b { width: 20px; height: 20px; visibility: hidden; background: #00ff00; }
			 .a:hover `+comb+` .b { visibility: visible; background: #ff0000; }`,
			Options{},
		)
		aEl := findElByClassInTree(root, "a")
		bEl := findElByClassInTree(root, "b")
		wrapEl := findElByClassInTree(root, "wrap")
		if aEl == nil || bEl == nil || wrapEl == nil {
			t.Fatalf("comb %q: missing els", comb)
		}

		// The trigger is the SIBLING .a — not the wrap or any other ancestor —
		// and it is registered with the engine so .a's hover boundary can
		// invalidate .b (which lies outside .a's own rect).
		if len(bEl.stateTriggers) != 1 || bEl.stateTriggers[0].el != aEl || !bEl.stateTriggers[0].hover {
			t.Fatalf("comb %q: b.stateTriggers = %+v, want exactly the .a sibling as hover trigger",
				comb, bEl.stateTriggers)
		}
		if !bEl.engine.stateDeps[aEl][bEl] {
			t.Errorf("comb %q: engine.stateDeps[a] missing b — sibling reveal won't repaint", comb)
		}

		root.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 40})
		if n := drawFillCount(bEl); n != 0 {
			t.Errorf("comb %q: resting b fills = %d, want 0 (hidden)", comb, n)
		}
		// Hover the sibling .a — the wrap alone must NOT reveal.
		wrapEl.Handle(qui.NewMouseEvent(qui.EventMouseEnter, 2, 30, 0, 0))
		if n := drawFillCount(bEl); n != 0 {
			t.Errorf("comb %q: b revealed by hovering the wrap (fills=%d); trigger must be .a", comb, n)
		}
		aEl.Handle(qui.NewMouseEvent(qui.EventMouseEnter, 2, 2, 0, 0))
		if n := drawFillCount(bEl); n == 0 {
			t.Errorf("comb %q: b painted nothing while sibling .a hovered — sibling-hover reveal dead", comb)
		}
		aEl.Handle(qui.NewMouseEvent(qui.EventMouseLeave, 2, 2, 0, 0))
		if n := drawFillCount(bEl); n != 0 {
			t.Errorf("comb %q: b fills after .a leave = %d, want 0 (should hide again)", comb, n)
		}
	}
}

// A sibling-of-ancestor rule (`.a:hover ~ .b .c`) triggers from the element
// the selector names — a sibling of the dependent's ancestor.
func TestSiblingOfAncestorHoverReveal(t *testing.T) {
	root := Render(
		`<body><div class="wrap">`+
			`<div class="a"></div>`+
			`<div class="b"><div class="c"></div></div>`+
			`</div></body>`,
		`.wrap { width: 200px; height: 60px; }
		 .a { width: 20px; height: 20px; }
		 .b { width: 40px; height: 30px; }
		 .c { width: 20px; height: 20px; visibility: hidden; background: #00ff00; }
		 .a:hover ~ .b .c { visibility: visible; }`,
		Options{},
	)
	aEl := findElByClassInTree(root, "a")
	cEl := findElByClassInTree(root, "c")
	if aEl == nil || cEl == nil {
		t.Fatal("missing els")
	}
	if len(cEl.stateTriggers) != 1 || cEl.stateTriggers[0].el != aEl {
		t.Fatalf("c.stateTriggers = %+v, want exactly the .a (sibling of ancestor .b)", cEl.stateTriggers)
	}
	root.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 60})
	if n := drawFillCount(cEl); n != 0 {
		t.Errorf("resting c fills = %d, want 0", n)
	}
	aEl.Handle(qui.NewMouseEvent(qui.EventMouseEnter, 2, 2, 0, 0))
	if n := drawFillCount(cEl); n == 0 {
		t.Error("c not revealed while .a (sibling of its ancestor) hovered")
	}
}

// The applied payload is scoped per trigger: with `.outer:hover .del{color}`
// and `.row:hover .del{visibility}`, hovering ONLY the outer container must
// apply the color rule but NOT the visibility reveal (previously the union
// of every ancestor-state rule fired from any trigger).
func TestAncestorStatePayloadScopedPerTrigger(t *testing.T) {
	root := Render(
		`<body><div class="outer"><div class="row">`+
			`<div class="del">x</div>`+
			`</div></div></body>`,
		`.outer { width: 300px; height: 100px; }
		 .row { width: 200px; height: 40px; }
		 .del { width: 40px; height: 20px; visibility: hidden; color: #000000; background: #00ff00; }
		 .outer:hover .del { color: #ff0000; }
		 .row:hover .del { visibility: visible; }`,
		Options{},
	)
	outerEl := findElByClassInTree(root, "outer")
	rowEl := findElByClassInTree(root, "row")
	delEl := findElByClassInTree(root, "del")
	if outerEl == nil || rowEl == nil || delEl == nil {
		t.Fatal("missing els")
	}
	if len(delEl.stateTriggers) != 2 {
		t.Fatalf("del.stateTriggers = %+v, want the .row and the .outer", delEl.stateTriggers)
	}
	root.Layout(qui.Rect{X: 0, Y: 0, W: 300, H: 100})

	// Hover only the OUTER: the .row:hover visibility rule must NOT fire.
	outerEl.Handle(qui.NewMouseEvent(qui.EventMouseEnter, 2, 90, 0, 0))
	if n := drawFillCount(delEl); n != 0 {
		t.Errorf("del revealed by hovering .outer (fills=%d); its reveal names .row:hover", n)
	}
	// Its color payload (named by .outer:hover) DOES apply — check via the
	// resolved variant since the hidden element paints nothing.
	if v := delEl.ancestorStateVariant(); v == nil {
		t.Error("no ancestor variant while .outer hovered, want the color payload")
	} else {
		if got, want := v.cs.Color, (qui.Color{R: 1, A: 1}); got != want {
			t.Errorf("variant color = %+v, want red (from .outer:hover .del)", got)
		}
		if v.cs.Hidden != true {
			t.Error("variant un-hides del from .outer hover alone; visibility names .row:hover")
		}
	}

	// Hovering the ROW (cursor inside both) reveals.
	rowEl.Handle(qui.NewMouseEvent(qui.EventMouseEnter, 2, 2, 0, 0))
	if n := drawFillCount(delEl); n == 0 {
		t.Error("del not revealed while .row hovered")
	}
}

// An ancestor-:focus rule (`.row:focus .del`) fires while focus is WITHIN
// the row — qui normalizes :focus-within to :focus — covering both the row
// itself being focused and a control inside it.
func TestAncestorFocusReveal(t *testing.T) {
	root := Render(
		`<body><div class="row">`+
			`<div class="del"></div>`+
			`</div></body>`,
		`.row { width: 200px; height: 40px; }
		 .del { width: 20px; height: 20px; visibility: hidden; background: #00ff00; }
		 .row:focus .del { visibility: visible; }`,
		Options{},
	)
	rowEl := findElByClassInTree(root, "row")
	delEl := findElByClassInTree(root, "del")
	if rowEl == nil || delEl == nil {
		t.Fatal("missing els")
	}
	if len(delEl.stateTriggers) != 1 || delEl.stateTriggers[0].el != rowEl || !delEl.stateTriggers[0].focus {
		t.Fatalf("del.stateTriggers = %+v, want the .row as a focus trigger", delEl.stateTriggers)
	}
	// Focus dependents are tracked for the window focus-change listener.
	if !delEl.engine.focusDeps[delEl] {
		t.Error("engine.focusDeps missing del — focus changes won't repaint the reveal")
	}

	root.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 40})
	if n := drawFillCount(delEl); n != 0 {
		t.Errorf("resting del fills = %d, want 0", n)
	}
	rowEl.SetFocused(true)
	if n := drawFillCount(delEl); n == 0 {
		t.Error("del not revealed while .row focused")
	}
	rowEl.SetFocused(false)
	if n := drawFillCount(delEl); n != 0 {
		t.Errorf("del fills after blur = %d, want 0", n)
	}
}

// An ancestor-:active rule (`.row:active .del`) fires while the row is
// pressed (a press anywhere in its subtree counts, per Box's capture-phase
// press tracking) and reverts on release.
func TestAncestorActiveReveal(t *testing.T) {
	root := Render(
		`<body><div class="row">`+
			`<div class="del"></div>`+
			`</div></body>`,
		`.row { width: 200px; height: 40px; }
		 .del { width: 20px; height: 20px; visibility: hidden; background: #00ff00; }
		 .row:active .del { visibility: visible; }`,
		Options{},
	)
	rowEl := findElByClassInTree(root, "row")
	delEl := findElByClassInTree(root, "del")
	if rowEl == nil || delEl == nil {
		t.Fatal("missing els")
	}
	if len(delEl.stateTriggers) != 1 || delEl.stateTriggers[0].el != rowEl || !delEl.stateTriggers[0].active {
		t.Fatalf("del.stateTriggers = %+v, want the .row as an active trigger", delEl.stateTriggers)
	}

	root.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 40})
	if n := drawFillCount(delEl); n != 0 {
		t.Errorf("resting del fills = %d, want 0", n)
	}
	rowEl.Handle(qui.NewMouseEvent(qui.EventMouseDown, 2, 2, qui.MouseButtonLeft, 0))
	if !rowEl.Pressed() {
		t.Fatal("row not pressed after MouseDown")
	}
	if n := drawFillCount(delEl); n == 0 {
		t.Error("del not revealed while .row pressed")
	}
	rowEl.Handle(qui.NewMouseEvent(qui.EventMouseUp, 2, 2, qui.MouseButtonLeft, 0))
	if n := drawFillCount(delEl); n != 0 {
		t.Errorf("del fills after release = %d, want 0", n)
	}
}

// `.row:focus .del` must NOT be linked when the sheet only carries the
// element's own :focus (`.del:focus`) — and, conversely, focusing the
// DESCENDANT counts as focus-within the row (the subject's focus propagates
// to ancestor compounds), matching the :focus-within normalization.
func TestAncestorFocusSubjectPropagation(t *testing.T) {
	root := Render(
		`<body><div class="row">`+
			`<div class="del">x</div>`+
			`</div></body>`,
		`.row { width: 200px; height: 40px; }
		 .del { width: 40px; height: 20px; color: #000000; }
		 .row:focus .del { color: #ff0000; }`,
		Options{},
	)
	delEl := findElByClassInTree(root, "del")
	if delEl == nil {
		t.Fatal("missing del")
	}
	// The subject's own Focus variant carries the rule (focus on .del ⇒
	// focus-within .row), so focusing .del recolors it too.
	if delEl.lastCS == nil || delEl.lastCS.Focus == nil {
		t.Fatal("del has no Focus variant")
	}
	if got, want := delEl.lastCS.Focus.Color, (qui.Color{R: 1, A: 1}); got != want {
		t.Errorf("del Focus variant color = %+v, want red (subject focus counts as focus-within .row)", got)
	}
}

// `.a:hover .b:hover` (both hovered at once) applies via .b's own Hover
// variant: hovering .b implies its ancestor .a is hovered, so the subject's
// hover propagates to the ancestor compound.
func TestNestedBothHover(t *testing.T) {
	root := Render(
		`<body><div class="a"><div class="b">x</div></div></body>`,
		`.a { width: 200px; height: 40px; }
		 .b { width: 40px; height: 20px; color: #000000; }
		 .a:hover .b:hover { color: #ff0000; }`,
		Options{},
	)
	bEl := findElByClassInTree(root, "b")
	if bEl == nil || bEl.lastCS == nil || bEl.lastCS.Hover == nil {
		t.Fatal("missing b / Hover variant")
	}
	if got, want := bEl.lastCS.Hover.Color, (qui.Color{R: 1, A: 1}); got != want {
		t.Errorf("b Hover variant color = %+v, want red (`.a:hover .b:hover` via subject-hover propagation)", got)
	}
	// And a sibling `:hover` must NOT ride the subject's hover: `.s:hover ~ .b`
	// styles don't fire from hovering .b itself.
	root2 := Render(
		`<body><div class="wrap"><div class="s"></div><div class="b">x</div></div></body>`,
		`.s { width: 10px; height: 10px; }
		 .b { width: 40px; height: 20px; color: #000000; }
		 .s:hover ~ .b { color: #ff0000; }`,
		Options{},
	)
	b2 := findElByClassInTree(root2, "b")
	if b2 == nil || b2.lastCS == nil {
		t.Fatal("missing b2")
	}
	if b2.lastCS.Hover != nil && b2.lastCS.Hover.Color == (qui.Color{R: 1, A: 1}) {
		t.Error("`.s:hover ~ .b` leaked into .b's own Hover variant (sibling state must not ride subject hover)")
	}
}
