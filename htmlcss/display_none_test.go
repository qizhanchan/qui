package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// P0.3: toggling display:none at runtime (via class change) must hide the
// element — zero measured size, no children attached — and toggling back
// must restore it. (The static Render path prunes display:none at compile
// time; this covers the live/reactive restyle path.)
func TestRuntimeDisplayNoneTogglesVisibility(t *testing.T) {
	res := RenderDoc(
		`<body><div id="panel" class="p">Panel content</div></body>`,
		`.hidden { display: none; }`,
		Options{},
	)
	e := res.ByID["panel"].(*El)

	// Visible: measures to something.
	if sz := e.Measure(qui.Size{W: 300, H: 0}); sz.W <= 0 || sz.H <= 0 {
		t.Fatalf("visible panel measured %+v, want non-zero", sz)
	}
	if e.displayNone {
		t.Fatal("panel starts display:none unexpectedly")
	}

	// Hide it (test mode flushes the restyle synchronously).
	e.SetClass("p hidden")
	if !e.displayNone {
		t.Fatal("adding .hidden did not set displayNone")
	}
	if sz := e.Measure(qui.Size{W: 300, H: 0}); sz.W != 0 || sz.H != 0 {
		t.Errorf("hidden panel measured %+v, want zero", sz)
	}
	if n := len(e.ChildList()); n != 0 {
		t.Errorf("hidden panel keeps %d children attached, want 0", n)
	}

	// Show it again: restored.
	e.SetClass("p")
	if e.displayNone {
		t.Fatal("removing .hidden did not clear displayNone")
	}
	if sz := e.Measure(qui.Size{W: 300, H: 0}); sz.W <= 0 || sz.H <= 0 {
		t.Errorf("re-shown panel measured %+v, want non-zero", sz)
	}
}

// A display:none child must vanish from its parent's layout entirely — no
// leftover gap slot. With `gap:10px` and the middle of three 50px items
// hidden, the third item must sit exactly one gap after the first (60px),
// not two gaps plus a zero-width slot (70px).
func TestDisplayNoneLeavesNoGapResidue(t *testing.T) {
	res := RenderDoc(
		`<body><div class="row">`+
			`<div id="a" class="cell">A</div>`+
			`<div id="b" class="cell">B</div>`+
			`<div id="c" class="cell">C</div>`+
			`</div></body>`,
		`.row { display: flex; gap: 10px; }
		 .cell { width: 50px; height: 20px; }
		 .hidden { display: none; }`,
		Options{},
	)
	a := res.ByID["a"].(*El)
	b := res.ByID["b"].(*El)
	c := res.ByID["c"].(*El)

	layout := func() { res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 200}) }
	layout()
	if got := c.Bounds().X - a.Bounds().X; got != 120 {
		t.Fatalf("baseline: c.X-a.X = %v, want 120 (two cells + two gaps)", got)
	}

	b.SetClass("cell hidden")
	layout()
	if got := c.Bounds().X - a.Bounds().X; got != 60 {
		t.Errorf("with b hidden: c.X-a.X = %v, want 60 (one cell + ONE gap, no residue)", got)
	}

	b.SetClass("cell")
	layout()
	if got := c.Bounds().X - a.Bounds().X; got != 120 {
		t.Errorf("b re-shown: c.X-a.X = %v, want 120 restored", got)
	}
}

// Mirrors the examples/html-css wiring: a button's onClick toggles a
// panel's class, which restyles the panel to display:none and back. Proves
// the click → SetClass → restyle → hide path works end-to-end in a window.
func TestDisplayNoneToggleViaClick(t *testing.T) {
	res := RenderDoc(
		`<body><div class="row"><button id="btn">Toggle</button>`+
			`<div id="panel" class="panel">content</div></div></body>`,
		`.row { display: flex; gap: 8px; } .panel.hidden { display: none; }`,
		Options{},
	)
	btn := res.ByID["btn"].(*El)
	panel := res.ByID["panel"].(*El)
	hidden := false
	btn.SetOnClick(func() {
		hidden = !hidden
		if hidden {
			panel.SetClass("panel hidden")
		} else {
			panel.SetClass("panel")
		}
	})

	win := qui.NewTestWindow(qui.Size{W: 400, H: 200})
	win.SetRoot(res.Root)
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 200})

	if panel.displayNone {
		t.Fatal("panel starts hidden")
	}
	if err := win.Click("#btn", qui.ClickOptions{}); err != nil {
		t.Fatalf("Click(#btn): %v", err)
	}
	win.DrainJobsForTest()
	if !panel.displayNone {
		t.Error("after first click, panel should be display:none")
	}
	if err := win.Click("#btn", qui.ClickOptions{}); err != nil {
		t.Fatalf("Click(#btn) #2: %v", err)
	}
	win.DrainJobsForTest()
	if panel.displayNone {
		t.Error("after second click, panel should be visible again")
	}
}
