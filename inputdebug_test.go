package qui_test

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// Input debuggability (Tier 1): TextState introspection + input-lifecycle
// event records.

func mountInput(t *testing.T) (*qui.Window, *widgets.Input) {
	t.Helper()
	w := qui.NewTestWindow(qui.Size{W: 400, H: 200})
	in := widgets.NewInput("name")
	root := qui.NewContainer(qui.FlexLayout{})
	root.SetSelf(root)
	root.AddChild(in)
	w.SetRoot(root)
	root.Layout(qui.Rect{W: 400, H: 200})
	in.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 28})
	return w, in
}

func TestTextStateIntrospection(t *testing.T) {
	_, in := mountInput(t)

	ts, ok := qui.WidgetTextState(in)
	if !ok {
		t.Fatal("Input should implement TextEditable")
	}
	if ts.Value != "" || ts.Caret != 0 || ts.SelStart != -1 {
		t.Fatalf("fresh input state = %+v, want empty/caret0/no-selection", ts)
	}

	in.SetText("hello")
	ts, _ = qui.WidgetTextState(in)
	if ts.Value != "hello" {
		t.Errorf("value = %q, want hello", ts.Value)
	}
	if ts.Caret != 5 {
		t.Errorf("caret = %d, want 5 (end after SetText)", ts.Caret)
	}
	if !ts.CanUndo {
		t.Errorf("CanUndo = false after an edit, want true")
	}

	in.SelectRange(1, 3)
	ts, _ = qui.WidgetTextState(in)
	if ts.SelStart != 1 || ts.SelEnd != 3 {
		t.Errorf("selection = [%d,%d], want [1,3]", ts.SelStart, ts.SelEnd)
	}
}

func TestTextStateSurfacedInAXTree(t *testing.T) {
	w, in := mountInput(t)
	in.SetText("hi")

	var found *qui.TextState
	for _, n := range w.AccessibilityTree().Flatten() {
		if n.Widget() == in {
			found = n.TextState
		}
	}
	if found == nil {
		t.Fatal("input AX node has no TextState")
	}
	if found.Value != "hi" || found.Caret != 2 {
		t.Errorf("AX TextState = %+v, want value=hi caret=2", *found)
	}
}

func TestInputLifecycleRecords(t *testing.T) {
	w, in := mountInput(t)

	var records []qui.EventRecord
	w.AddEventListener(func(r qui.EventRecord) { records = append(records, r) })

	in.SetText("draft")

	var change *qui.EventRecord
	for i := range records {
		if records[i].Kind == "Input" {
			change = &records[i]
		}
	}
	if change == nil {
		t.Fatal("no Input lifecycle record emitted for SetText")
	}
	if change.Value != "draft" {
		t.Errorf("record Value = %q, want draft", change.Value)
	}
	if change.OldValue != "" {
		t.Errorf("record OldValue = %q, want empty (was blank)", change.OldValue)
	}
	if change.Target == "" {
		t.Errorf("record Target path is empty")
	}
	if change.Source != "program" {
		t.Errorf("SetText record Source = %q, want program", change.Source)
	}
}

// lastInput returns the last "Input"-kind record captured, or nil.
func lastInput(records []qui.EventRecord) *qui.EventRecord {
	var out *qui.EventRecord
	for i := range records {
		if records[i].Kind == "Input" {
			out = &records[i]
		}
	}
	return out
}

func TestInputSourceUserVsProgram(t *testing.T) {
	w, in := mountInput(t)
	var records []qui.EventRecord
	w.AddEventListener(func(r qui.EventRecord) { records = append(records, r) })

	// Real keystrokes → source "user".
	if err := w.TypeWidget(in, "hi", qui.TypeOptions{Raw: true}); err != nil {
		t.Fatalf("raw type: %v", err)
	}
	if in.GetText() != "hi" {
		t.Fatalf("raw typing did not insert; text = %q", in.GetText())
	}
	if got := lastInput(records); got == nil || got.Source != "user" {
		t.Fatalf("keystroke record = %+v, want Source=user", got)
	}
}

func TestControlledValueOverwriteDiagnostic(t *testing.T) {
	w, in := mountInput(t)
	var records []qui.EventRecord
	w.AddEventListener(func(r qui.EventRecord) { records = append(records, r) })

	// User types, then a programmatic SetText replaces it with a DIFFERENT
	// value — the controlled-value fight (binding wrote a stale value back).
	if err := w.TypeWidget(in, "milk", qui.TypeOptions{Raw: true}); err != nil {
		t.Fatalf("raw type: %v", err)
	}
	records = nil
	in.SetText("") // binding overwrites the user's "milk" with stale ""

	got := lastInput(records)
	if got == nil {
		t.Fatal("no Input record for the overwriting SetText")
	}
	if got.Source != "program-overwrite" {
		t.Errorf("Source = %q, want program-overwrite", got.Source)
	}
	if got.OldValue != "milk" || got.Value != "" {
		t.Errorf("record = {old:%q new:%q}, want {old:milk new:''}", got.OldValue, got.Value)
	}

	// A subsequent programmatic SetText (no intervening user edit) is a
	// plain "program" change, not flagged.
	records = nil
	in.SetText("bread")
	if got := lastInput(records); got == nil || got.Source != "program" {
		t.Errorf("second SetText Source = %v, want program", got)
	}
}
