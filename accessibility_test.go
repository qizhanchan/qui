package qui_test

import (
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// Phase 1 covers: BaseWidget ID, AccessibilityTree shape, selector
// grammar (id / role / name / value / layer / nth / visibility /
// focused), and modal-layer detection.
//
// Tests use NewTestWindow + manual Layout so they run without GLFW.

func newTree(t *testing.T) (*qui.Window, *widgets.Button, *widgets.Input, *widgets.CheckBox) {
	t.Helper()
	w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	submit := widgets.NewButton("Submit", nil)
	submit.SetID("submit")
	name := widgets.NewInput("Name…")
	name.Text = "Ada"
	agree := widgets.NewCheckBox("I agree", nil)
	agree.Checked = true

	c := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical})
	c.AddChild(submit)
	c.AddChild(name)
	c.AddChild(agree)
	c.SetSelf(c)
	w.SetRoot(c)

	// No Step runs in tests — manually drive layout so Bounds is set.
	c.Measure(qui.Size{W: 400, H: 300})
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})
	return w, submit, name, agree
}

func TestAccessibilityTree_Shape(t *testing.T) {
	w, submit, name, agree := newTree(t)
	tree := w.AccessibilityTree()
	if tree.Root == nil {
		t.Fatal("expected Root non-nil")
	}
	if got, want := len(tree.Root.Children), 3; got != want {
		t.Fatalf("root children = %d, want %d", got, want)
	}
	if tree.Root.Layer != "root" {
		t.Errorf("root layer = %q, want %q", tree.Root.Layer, "root")
	}
	if got, want := tree.Root.Children[0].Role, qui.RoleButton; got != want {
		t.Errorf("button role = %q, want %q", got, want)
	}
	if got, want := tree.Root.Children[0].Name, "Submit"; got != want {
		t.Errorf("button name = %q, want %q", got, want)
	}
	if got, want := tree.Root.Children[0].ID, "submit"; got != want {
		t.Errorf("button id = %q, want %q", got, want)
	}
	if got, want := tree.Root.Children[1].Role, qui.RoleTextbox; got != want {
		t.Errorf("textfield role = %q, want %q", got, want)
	}
	if got, want := tree.Root.Children[1].Value, "Ada"; got != want {
		t.Errorf("textfield value = %q, want %q", got, want)
	}
	if got, want := tree.Root.Children[2].Role, qui.RoleCheckbox; got != want {
		t.Errorf("checkbox role = %q, want %q", got, want)
	}
	if !strings.Contains(tree.Root.Children[2].State, "checked") {
		t.Errorf("checkbox state = %q, expected 'checked'", tree.Root.Children[2].State)
	}
	_ = submit
	_ = name
	_ = agree
}

func TestAccessibilityTree_Overlays(t *testing.T) {
	w, _, _, _ := newTree(t)
	d := widgets.NewDialog("Confirm", widgets.NewLabel("are you sure?"))
	w.PushOverlay(d)
	d.SetSelf(d)
	d.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	tree := w.AccessibilityTree()
	if len(tree.Overlays) != 1 {
		t.Fatalf("got %d overlays, want 1", len(tree.Overlays))
	}
	if got := tree.Overlays[0].Role; got != qui.RoleDialog {
		t.Errorf("overlay role = %q, want %q", got, qui.RoleDialog)
	}
	if !strings.HasPrefix(tree.Overlays[0].Layer, "modal[") {
		t.Errorf("dialog overlay layer = %q, expected modal[…]", tree.Overlays[0].Layer)
	}
	if got, want := tree.Overlays[0].Name, "Confirm"; got != want {
		t.Errorf("dialog name = %q, want %q", got, want)
	}
}

func TestSelector_ByID(t *testing.T) {
	w, _, _, _ := newTree(t)
	hit, err := w.Find("#submit")
	if err != nil {
		t.Fatal(err)
	}
	if hit == nil {
		t.Fatal("expected #submit to match a widget")
	}
	if hit.(*widgets.Button).Text != "Submit" {
		t.Errorf("got button %q, want Submit", hit.(*widgets.Button).Text)
	}
}

func TestSelector_ByRoleAndName(t *testing.T) {
	w, _, _, _ := newTree(t)
	matches, err := w.FindAll(`[role=button][name="Submit"]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(matches))
	}
}

func TestSelector_NameContains(t *testing.T) {
	w, _, _, _ := newTree(t)
	matches, err := w.FindAll(`[name*="agr"]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(matches))
	}
}

func TestSelector_Nth(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	c := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical})
	c.SetSelf(c)
	for i := 0; i < 4; i++ {
		c.AddChild(widgets.NewButton("b", nil))
	}
	w.SetRoot(c)
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	match, err := w.Find(`[role=button]:nth(2)`)
	if err != nil {
		t.Fatal(err)
	}
	if match == nil {
		t.Fatal("expected nth(2) to match a button")
	}
}

func TestSelector_LayerModal(t *testing.T) {
	w, _, _, _ := newTree(t)
	d := widgets.NewDialog("Confirm", widgets.NewLabel("body"))
	okBtn := d.AddButton("OK", nil)
	w.PushOverlay(d)
	d.SetSelf(d)
	d.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})
	// Ensure dialog children have non-empty bounds for visibility
	okBtn.Layout(qui.Rect{X: 10, Y: 10, W: 80, H: 24})

	matches, err := w.FindAll(`:layer(modal) [role=button]`)
	if err != nil {
		t.Fatal(err)
	}
	// The modal button "OK" should match; the root-tree "Submit"
	// should be excluded by the layer filter.
	foundOK := false
	for _, m := range matches {
		if b, ok := m.(*widgets.Button); ok && b.Text == "OK" {
			foundOK = true
		}
		if b, ok := m.(*widgets.Button); ok && b.Text == "Submit" {
			t.Errorf(":layer(modal) included root-tree button %q", b.Text)
		}
	}
	if !foundOK {
		t.Errorf(":layer(modal) did not include the dialog's OK button")
	}
}

func TestSelector_Visible(t *testing.T) {
	w, _, name, _ := newTree(t)
	// Force the textfield to zero bounds → hidden.
	name.Layout(qui.Rect{})
	visible, err := w.FindAll(`[role=textbox]:visible`)
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 0 {
		t.Errorf("hidden textbox still matched :visible")
	}
	hidden, err := w.FindAll(`[role=textbox]:hidden`)
	if err != nil {
		t.Fatal(err)
	}
	if len(hidden) != 1 {
		t.Errorf("hidden textbox should match :hidden, got %d", len(hidden))
	}
}

func TestAccessibilityTree_CollapsedAncestorHidesRetainedBounds(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	button := widgets.NewButton("Hidden action", nil)
	button.Layout(qui.Rect{X: 10, Y: 10, W: 100, H: 30})
	parent := qui.NewContainer(nil, button)
	parent.SetSelf(parent)
	parent.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 100})
	root := qui.NewContainer(nil, parent)
	root.SetSelf(root)
	root.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 100})
	w.SetRoot(root)

	parent.SetCollapsed(true)
	tree := w.AccessibilityTree()
	node := tree.Root.Children[0].Children[0]
	if node.Visible {
		t.Fatal("child of collapsed parent reported visible despite retained bounds")
	}
	if !strings.Contains(node.State, "hidden") {
		t.Fatalf("collapsed descendant state = %q, want hidden", node.State)
	}
}

func TestSelector_Errors(t *testing.T) {
	w, _, _, _ := newTree(t)
	bad := []string{
		"[",
		"[role",
		"[role=",
		"[role=button",
		":foo",
		":nth(",
		":layer(unknown)",
	}
	for _, s := range bad {
		if _, err := w.Find(s); err == nil {
			t.Errorf("selector %q should have failed", s)
		}
	}
}

func TestAccessibilityTree_Hash(t *testing.T) {
	w, _, _, _ := newTree(t)
	h1 := w.AccessibilityTree().Hash()
	if !strings.HasPrefix(h1, "ax-") {
		t.Fatalf("hash = %q, expected ax- prefix", h1)
	}
	// Same tree → same hash.
	h2 := w.AccessibilityTree().Hash()
	if h1 != h2 {
		t.Errorf("repeated hash differs: %q vs %q", h1, h2)
	}
}

func TestAgentOverlayShortcut_TogglesWithoutOptIn(t *testing.T) {
	// Regression: Cmd+Shift+A in a fresh app (no prior
	// EnableDebugOverlay call) should self-configure and toggle the
	// agent overlay on. Bug: the shortcut was wired through the
	// existing debugOverlayConfigured gate, so it stayed silent in
	// apps like q-excel.
	w := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	w.SetRoot(qui.NewContainer(qui.FlexLayout{}))
	w.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyA, qui.ModSuper|qui.ModShift))
	if !w.IsDebugOverlayEnabled() {
		t.Fatal("Cmd+Shift+A did not enable the overlay on a fresh window")
	}
	// Toggling again should turn it off.
	w.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyA, qui.ModSuper|qui.ModShift))
	if w.IsDebugOverlayEnabled() {
		t.Fatal("Cmd+Shift+A did not disable the overlay on second press")
	}
}

func TestAccessibilityTree_HashChangesOnState(t *testing.T) {
	w, _, _, agree := newTree(t)
	h1 := w.AccessibilityTree().Hash()
	agree.Checked = !agree.Checked
	h2 := w.AccessibilityTree().Hash()
	if h1 == h2 {
		t.Errorf("hash did not change after state mutation: %q", h1)
	}
}

// TestAccessibilityTree_SelectOptions verifies a Select's option set and
// current selection surface on its AXNode (the agent can read choices
// without opening the dropdown), and that a selection change invalidates
// the tree hash.
func TestAccessibilityTree_SelectOptions(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	sel := widgets.NewSelect(w, []string{"GET", "POST", "PUT"}, nil)
	sel.SelectedIdx = 0
	sel.SetID("method")
	c := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical})
	c.AddChild(sel)
	c.SetSelf(c)
	w.SetRoot(c)
	c.Measure(qui.Size{W: 400, H: 300})
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	tree := w.AccessibilityTree()
	var node *qui.AXNode
	for _, n := range tree.Flatten() {
		if n.ID == "method" {
			node = n
			break
		}
	}
	if node == nil {
		t.Fatal("select node not found")
	}
	if node.Role != qui.RoleCombobox {
		t.Errorf("role = %q, want combobox", node.Role)
	}
	if len(node.Options) != 3 {
		t.Fatalf("options len = %d, want 3", len(node.Options))
	}
	if node.Options[0].Label != "GET" || !node.Options[0].Selected {
		t.Errorf("options[0] = %+v, want {GET selected}", node.Options[0])
	}

	h1 := tree.Hash()
	sel.SelectedIdx = 1
	if h2 := w.AccessibilityTree().Hash(); h1 == h2 {
		t.Error("changing the selection should change the tree hash")
	}
}
