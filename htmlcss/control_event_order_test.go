package htmlcss

import (
	"fmt"
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// Form elements are backed by native controls that sit BELOW the element in
// the widget tree, so their events target the control and would reach the
// element's author handlers only while bubbling — after the control's
// default action, and not at all when the control consumes the event. In
// the DOM the element IS the target: its listeners run first, and a
// listener that consumes the event suppresses the default.
//
// This matrix pins that for every control × event: the handler fires
// exactly once, sees the control's state from before its default action,
// and consuming the event leaves that state untouched (while not consuming
// it lets the default happen — which keeps the probes themselves honest).
//
// Cells whose "default" is nil have no default action to order against in
// qui; they still assert the handler fires exactly once. Deliberate
// differences from the DOM:
//
//   - Focus moves on press before the event is dispatched (window-level
//     click focus), so consuming pointerdown keeps the control's own press
//     action (caret placement, toggle, open) but not the focus change.
//   - checkbox / radio / select / range activate on PRESS, as native
//     toolkits do, not on click. Their activation is pointerdown's default
//     here, so PreventDefault on click cannot undo it.
//   - The contextmenu handler has no return value: an element that has one
//     always owns the right press.
//   - keyup and pointerup have no default action in any qui control.

type controlFixture struct {
	el  *El
	win *qui.Window
	// pt is where pointer events land, in window coordinates.
	pt qui.Point
	// key is the keydown whose default action is observable.
	key qui.Key
	// state snapshots what each event's default action changes; a column
	// missing from the map has no default for this control.
	state map[string]func() string
}

func newControlFixture(t *testing.T, row string) *controlFixture {
	t.Helper()
	eng := NewStyleEngine(`input, textarea, select { width: 200px; } textarea { height: 40px; }`)
	root := eng.NewEl("div")
	var el *El
	switch row {
	case "input":
		el = eng.NewEl("input")
		el.SetAttr("value", "main.go")
	case "textarea":
		el = eng.NewEl("textarea")
		el.SetAttr("value", strings.Repeat("a line of text\n", 20))
	case "select":
		el = eng.NewEl("select")
		el.SetSelectOptions([]string{"one", "two", "three"})
	case "checkbox", "radio", "range":
		el = eng.NewEl("input")
		el.SetAttr("type", row)
		if row == "radio" {
			el.SetAttr("name", "g")
		}
		if row == "range" {
			el.SetAttr("value", "10")
		}
	}
	root.SetElementChildren([]qui.Widget{el})
	win := qui.NewTestWindow(qui.Size{W: 400, H: 200})
	eng.opts.Window = win
	eng.SetRoot(root)
	win.SetRoot(root)
	eng.Restyle()
	root.Layout(qui.Rect{W: 400, H: 200})
	el.ensureBacking()
	if el.backing == nil {
		t.Fatalf("%s: no backing control", row)
	}

	b := el.Bounds()
	f := &controlFixture{el: el, win: win, pt: qui.Point{X: b.X + b.W/2, Y: b.Y + b.H/2}, state: map[string]func() string{}}
	switch w := el.backing.(type) {
	case *widgets.Input:
		// Caret at the end, press near the left edge: the press moves it.
		f.pt.X = b.X + 6
		f.key = qui.KeyHome
		caret := func() string { return fmt.Sprint(w.CursorPos()) }
		f.state["keydown"] = caret
		f.state["pointerdown"] = caret
	case *widgets.TextArea:
		f.key = qui.KeyDown
		caret := func() string { return fmt.Sprint(w.CursorPos()) }
		f.state["keydown"] = caret
		f.state["pointerdown"] = caret
		// The caret rect moves with the scroll offset.
		f.state["wheel"] = func() string { return fmt.Sprint(w.CaretRect().Y) }
	case *widgets.Select:
		f.key = qui.KeyDown
		f.state["keydown"] = func() string { return fmt.Sprint(w.SelectedIdx) }
		f.state["pointerdown"] = func() string { return fmt.Sprint(w.IsOpen()) }
	case *widgets.CheckBox:
		f.key = qui.KeySpace
		on := func() string { return fmt.Sprint(w.Checked) }
		f.state["keydown"] = on
		f.state["pointerdown"] = on
	case *widgets.RadioButton:
		f.key = qui.KeySpace
		on := func() string { return fmt.Sprint(w.Checked) }
		f.state["keydown"] = on
		f.state["pointerdown"] = on
	case *widgets.Slider:
		f.key = qui.KeyRight
		v := func() string { return fmt.Sprint(w.Value) }
		f.state["keydown"] = v
		f.state["pointerdown"] = v
	default:
		t.Fatalf("%s: unexpected backing %T", row, el.backing)
	}
	win.SetFocus(el.backing)
	return f
}

func (f *controlFixture) mouse(kind qui.EventType, button qui.MouseButton) {
	f.win.DispatchTestEvent(qui.NewMouseEvent(kind, f.pt.X, f.pt.Y, button, 0))
}

func (f *controlFixture) press(button qui.MouseButton) {
	f.mouse(qui.EventMouseDown, button)
	f.mouse(qui.EventMouseUp, button)
}

// controlEventColumns: install(f, consume, fired) wires the author handler
// (calling fired from inside it); send dispatches the event.
var controlEventColumns = []struct {
	name    string
	install func(f *controlFixture, consume bool, fired func())
	send    func(f *controlFixture)
}{
	{"keydown",
		func(f *controlFixture, consume bool, fired func()) {
			f.el.SetOnKeyDown(func(qui.KeyEvent) bool { fired(); return consume })
		},
		func(f *controlFixture) {
			f.win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, f.key, 0))
			f.win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyUp, f.key, 0))
		}},
	{"keyup",
		func(f *controlFixture, consume bool, fired func()) {
			f.el.SetOnKeyUp(func(qui.KeyEvent) bool { fired(); return consume })
		},
		func(f *controlFixture) {
			f.win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyUp, f.key, 0))
		}},
	{"pointerdown",
		func(f *controlFixture, consume bool, fired func()) {
			f.el.SetOnPointerDown(func(PointerEvent) bool { fired(); return consume })
		},
		func(f *controlFixture) { f.press(qui.MouseButtonLeft) }},
	{"pointerup",
		func(f *controlFixture, consume bool, fired func()) {
			f.el.SetOnPointerUp(func(PointerEvent) bool { fired(); return consume })
		},
		func(f *controlFixture) { f.press(qui.MouseButtonLeft) }},
	{"click",
		func(f *controlFixture, consume bool, fired func()) {
			f.el.SetOnClickEvent(func(pe PointerEvent) {
				fired()
				if consume {
					pe.PreventDefault()
				}
			})
		},
		func(f *controlFixture) { f.press(qui.MouseButtonLeft) }},
	{"contextmenu",
		func(f *controlFixture, _ bool, fired func()) {
			f.el.SetOnContextMenu(func(x, y float32) { fired() })
		},
		func(f *controlFixture) { f.press(qui.MouseButtonRight) }},
	{"wheel",
		func(f *controlFixture, consume bool, fired func()) {
			f.el.SetOnWheel(func(dx, dy float32) bool { fired(); return consume })
		},
		func(f *controlFixture) {
			f.win.DispatchTestEvent(qui.NewScrollEvent(f.pt.X, f.pt.Y, 0, -40, 0))
		}},
}

func TestControlEventOrderMatrix(t *testing.T) {
	rows := []string{"input", "textarea", "select", "checkbox", "radio", "range"}
	for _, row := range rows {
		for _, col := range controlEventColumns {
			for _, consume := range []bool{false, true} {
				if col.name == "contextmenu" && !consume {
					continue // always consumes; one variant covers it
				}
				name := fmt.Sprintf("%s/%s/consume=%v", row, col.name, consume)
				t.Run(name, func(t *testing.T) {
					f := newControlFixture(t, row)
					state := f.state[col.name]
					var before string
					if state != nil {
						before = state()
					}
					calls := 0
					col.install(f, consume, func() {
						calls++
						if state != nil {
							if got := state(); got != before {
								t.Errorf("handler ran after the default action: state %s, was %s", got, before)
							}
						}
					})
					col.send(f)
					if calls != 1 {
						t.Fatalf("handler fired %d times, want 1", calls)
					}
					if state == nil {
						return
					}
					after := state()
					if consume && after != before {
						t.Errorf("consumed event still ran the default: state %s -> %s", before, after)
					}
					if !consume && after == before {
						t.Errorf("unconsumed event skipped the default: state stayed %s", before)
					}
				})
			}
		}
	}
}
