package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// backingInput digs out the widgets.Input an <input> element renders through.
func backingInput(t *testing.T, w qui.Widget) *widgets.Input {
	t.Helper()
	var found *widgets.Input
	var walk func(qui.Widget)
	walk = func(w qui.Widget) {
		if in, ok := w.(*widgets.Input); ok && found == nil {
			found = in
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, ch := range c.ChildList() {
				walk(ch)
			}
		}
	}
	walk(w)
	if found == nil {
		t.Fatal("element renders no widgets.Input")
	}
	return found
}

// `readonly` blocks every mutation path but leaves the field otherwise
// intact (still focusable, still selectable/copyable — those don't go
// through BeforeInput).
func TestReadonlyBlocksEdits(t *testing.T) {
	res := RenderDoc(
		`<body><input id="ro" value="locked" readonly>
		 <input id="rw" value="open"></body>`,
		``,
		Options{},
	)
	ro := backingInput(t, res.ByID["ro"])
	rw := backingInput(t, res.ByID["rw"])

	ro.InsertText("XYZ")
	if ro.Text != "locked" {
		t.Errorf("readonly field was edited: %q", ro.Text)
	}
	rw.InsertText("!")
	if rw.Text == "open" {
		t.Error("non-readonly field rejected an edit")
	}
	if !ro.Enabled() {
		t.Error("readonly must not disable the field (that is what `disabled` is for)")
	}
}

// `maxlength` caps insertions but never blocks deletions, and a selection
// being replaced frees up its own length.
func TestMaxLengthCapsInsertions(t *testing.T) {
	res := RenderDoc(
		`<body><input id="m" value="abc" maxlength="5"></body>`,
		``,
		Options{},
	)
	in := backingInput(t, res.ByID["m"])

	in.InsertText("de") // 3 + 2 = 5 → fits
	if in.Text != "abcde" {
		t.Fatalf("after fitting insert: %q, want %q", in.Text, "abcde")
	}
	in.InsertText("f") // would be 6 → rejected
	if in.Text != "abcde" {
		t.Errorf("maxlength exceeded: %q", in.Text)
	}
	// Replacing a selection: select all 5, type 3 → net 3, must be allowed.
	in.SelectRange(0, 5)
	in.InsertText("xyz")
	if in.Text != "xyz" {
		t.Errorf("replacing a selection under the cap failed: %q", in.Text)
	}
}

// An invalid / absent maxlength imposes no cap.
func TestMaxLengthAbsentOrInvalid(t *testing.T) {
	res := RenderDoc(
		`<body><input id="a" value=""><input id="b" value="" maxlength="oops"></body>`,
		``,
		Options{},
	)
	for _, id := range []string{"a", "b"} {
		in := backingInput(t, res.ByID[id])
		in.InsertText("0123456789")
		if in.Text != "0123456789" {
			t.Errorf("#%s capped without a valid maxlength: %q", id, in.Text)
		}
	}
}

// The global `hidden` attribute hides the element like `display: none` —
// but as a UA rule, so author CSS can still override it.
func TestHiddenAttribute(t *testing.T) {
	res := RenderDoc(
		`<body><div id="gone" hidden>x</div><div id="here">y</div></body>`,
		``,
		Options{},
	)
	// Static Render prunes resolved-none subtrees at compile time (see
	// COVERAGE.md), so a [hidden] element produces no widget at all.
	if w := res.ByID["gone"]; w != nil {
		t.Errorf("[hidden] element still built a widget (%T)", w)
	}
	here := res.ByID["here"].(*El)
	if got := here.Measure(qui.Size{W: 200, H: 200}); got.H == 0 {
		t.Error("non-hidden sibling collapsed too")
	}

	// Author CSS wins over the UA rule, as in a browser.
	res2 := RenderDoc(
		`<body><div id="shown" hidden>x</div></body>`,
		`#shown { display: block; }`,
		Options{},
	)
	shown := res2.ByID["shown"].(*El)
	if got := shown.Measure(qui.Size{W: 200, H: 200}); got.H == 0 {
		t.Error("author `display: block` should override the [hidden] UA rule")
	}
}

// styleOf returns the style CSS lands on for an element: form controls are
// styled on their backing widget (see applyCommon), everything else on the
// El box itself.
func styleOf(t *testing.T, w qui.Widget) *qui.Style {
	t.Helper()
	el, ok := w.(*El)
	if !ok {
		t.Fatalf("widget %T is not an *El", w)
	}
	el.ensureBacking()
	if el.backing != nil {
		return el.backing.Style()
	}
	return el.Style()
}

// The form-state pseudo-classes select on the attributes that drive them.
func TestFormStatePseudoClasses(t *testing.T) {
	res := RenderDoc(
		`<body>
		   <input id="req" required>
		   <input id="opt">
		   <input id="ro" readonly>
		   <input id="dis" disabled>
		   <input id="cb" type="checkbox">
		   <p id="para">text</p>
		 </body>`,
		`:required { border: 2px solid #d93025; }
		 :optional { border: 2px solid #1e8e3e; }
		 :read-only { background: #f1f3f4; }
		 :read-write { background: #ffffff; }`,
		Options{},
	)
	readOnlyFill := qui.Color{R: 0xf1 / 255.0, G: 0xf3 / 255.0, B: 0xf4 / 255.0, A: 1}
	white := qui.Color{R: 1, G: 1, B: 1, A: 1}

	// A required field: :required + :read-write, not :optional.
	if bw := styleOf(t, res.ByID["req"]).BorderSize; bw != 2 {
		t.Errorf(":required did not match (border %v)", bw)
	}
	if bg := styleOf(t, res.ByID["req"]).Background; bg != white {
		t.Errorf(":read-write should match an editable field, background = %+v", bg)
	}
	// An optional field matches :optional.
	if bw := styleOf(t, res.ByID["opt"]).BorderSize; bw != 2 {
		t.Errorf(":optional did not match (border %v)", bw)
	}
	// readonly / disabled fields are :read-only, not :read-write. A checkbox
	// is altered by activation rather than typing, so it is :read-only too.
	for _, id := range []string{"ro", "dis", "cb"} {
		if bg := styleOf(t, res.ByID[id]).Background; bg != readOnlyFill {
			t.Errorf("#%s background = %+v, want the :read-only fill %+v", id, bg, readOnlyFill)
		}
	}
	// An ordinary element is :read-only per spec, and never :required /
	// :optional (those apply to form controls only).
	para := res.ByID["para"].(*El)
	if bg := para.Style().Background; bg != readOnlyFill {
		t.Error("<p> should match :read-only (spec: everything not user-alterable)")
	}
	if bw := para.Style().BorderSize; bw != 0 {
		t.Errorf(":required/:optional leaked onto a <p> (border %v)", bw)
	}
}

// :enabled must not match a disabled control.
func TestEnabledPseudoExcludesDisabled(t *testing.T) {
	res := RenderDoc(
		`<body><input id="on"><input id="off" disabled></body>`,
		`:enabled { border: 3px solid #1a73e8; }`,
		Options{},
	)
	if bw := styleOf(t, res.ByID["on"]).BorderSize; bw != 3 {
		t.Errorf(":enabled did not match an enabled field (border %v)", bw)
	}
	if bw := styleOf(t, res.ByID["off"]).BorderSize; bw == 3 {
		t.Error(":enabled matched a disabled field")
	}
}
