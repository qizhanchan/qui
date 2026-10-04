package html_test

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
)

// A conditional attribute must be REMOVED when a re-render stops supplying
// it. The DSL used to apply attrs with per-key SetAttr, which can only add:
// a toolbar button rendered `disabled` on its first frame stayed disabled
// (and greyed) for the process lifetime, no matter what later renders said.
//
// Covers `disabled` (also feeds the backing control + :disabled CSS) and a
// plain aria-* attribute (feeds the AX tree).
func TestConditionalAttrIsRemovedOnRerender(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	// The target button is the one under test; a separate always-enabled
	// trigger flips the state (clicking the target itself wouldn't work —
	// a disabled element correctly refuses to act).
	app := func() h.Node {
		on, set := reactive.UseState(true)
		target := h.Button("target").ID("target").Attr("aria-pressed", boolStr(on))
		if on {
			target = target.Attr("disabled", "")
		}
		return h.Div(
			target,
			h.Button("flip").ID("flip").OnClick(func() { set(false) }),
		)
	}
	rt := h.Mount(win, ``, app)

	kids := rt.Root().(*htmlcss.El).ChildList()
	target := kids[0].(*htmlcss.El)
	flip := kids[1].(*htmlcss.El)
	if st := target.AccessibleState().String(); st != "pressed|disabled" {
		t.Fatalf("initial state = %q, want pressed|disabled", st)
	}
	if target.Enabled() {
		t.Errorf("initial: disabled attribute should disable the element")
	}

	flip.Handle(qui.NewMouseEvent(qui.EventMouseUp, 1, 1, qui.MouseButtonLeft, 0))
	rt.Flush()

	target2 := rt.Root().(*htmlcss.El).ChildList()[0].(*htmlcss.El)
	if target2 != target {
		t.Fatalf("button instance was not reused across re-render")
	}
	if st := target2.AccessibleState().String(); st != "" {
		t.Errorf("after re-render state = %q, want empty (disabled gone, aria-pressed false)", st)
	}
	if !target2.Enabled() {
		t.Errorf("after re-render: element should be enabled again")
	}
}

// A disabled element must not run its click handler. Before the element's own
// Enabled flag tracked the attribute, `disabled` only greyed the button via
// CSS while the handler still fired.
func TestDisabledElementDoesNotClick(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	clicks := 0
	app := func() h.Node {
		return h.Div(
			h.Button("go").ID("btn").Attr("disabled", "").
				OnClick(func() { clicks++ }),
		)
	}
	rt := h.Mount(win, ``, app)
	btn := rt.Root().(*htmlcss.El).ChildList()[0].(*htmlcss.El)
	btn.Handle(qui.NewMouseEvent(qui.EventMouseUp, 1, 1, qui.MouseButtonLeft, 0))
	rt.Flush()
	if clicks != 0 {
		t.Errorf("disabled button fired its handler %d time(s)", clicks)
	}
}

// Attributes set outside the declarative path must survive a re-render that
// manages a different key — SetManagedAttrs only owns what the DSL supplied.
func TestSetAttrOutsideDSLSurvivesRerender(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	app := func() h.Node {
		n, set := reactive.UseState(0)
		return h.Div(
			h.Button("go").ID("btn").Attr("aria-label", label(n)).
				OnClick(func() { set(n + 1) }),
		)
	}
	rt := h.Mount(win, ``, app)

	btn := rt.Root().(*htmlcss.El).ChildList()[0].(*htmlcss.El)
	btn.SetAttr("data-external", "keep")

	btn.Handle(qui.NewMouseEvent(qui.EventMouseUp, 1, 1, qui.MouseButtonLeft, 0))
	rt.Flush()

	if got, ok := btn.Attr("data-external"); !ok || got != "keep" {
		t.Errorf("externally set attr = %q (present=%t), want keep", got, ok)
	}
	if got := btn.AccessibleName(); got != "one" {
		t.Errorf("managed aria-label = %q, want one", got)
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func label(n int) string {
	if n == 0 {
		return "zero"
	}
	return "one"
}
