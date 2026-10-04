package reactive_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/reactive"
)

// findNode returns the first node in the tree matching pred (pre-order).
func findNode(n *reactive.ReactiveNode, pred func(*reactive.ReactiveNode) bool) *reactive.ReactiveNode {
	if n == nil {
		return nil
	}
	if pred(n) {
		return n
	}
	for _, c := range n.Children {
		if got := findNode(c, pred); got != nil {
			return got
		}
	}
	return nil
}

func TestInspectSurfacesComponentHooksAndSignals(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	app := func() reactive.Element {
		return uiVBox().Children(
			uiElem(reactive.Component("Greeter", "g1", struct{ Name string }{"world"},
				func(p struct{ Name string }) reactive.Element {
					count, _ := reactive.UseState(7)
					flag := reactive.UseSignal(true)
					_ = count
					_ = flag
					return uiLabel(fmt.Sprintf("hi %s", p.Name)).Build()
				})),
		).Build()
	}
	rt := reactive.Mount(win, app)
	defer rt.Flush()

	snap := rt.Inspect()
	if snap == nil {
		t.Fatal("Inspect returned nil after Mount")
	}

	comp := findNode(snap, func(n *reactive.ReactiveNode) bool { return n.Name == "Greeter" })
	if comp == nil {
		t.Fatalf("Greeter component not found in tree:\n%s", snap.String())
	}
	if comp.Kind != "component" {
		t.Errorf("kind = %q, want component", comp.Kind)
	}
	if comp.Key != "g1" {
		t.Errorf("key = %q, want g1", comp.Key)
	}
	if !strings.Contains(comp.Props, "world") {
		t.Errorf("props = %q, want to contain world", comp.Props)
	}

	// Hooks: one state (7) and one signal (true).
	var sawState, sawSignal bool
	for _, h := range comp.Hooks {
		switch h.Kind {
		case "state":
			if h.Value == "7" {
				sawState = true
			}
		case "signal":
			if h.Value == "true" {
				sawSignal = true
			}
		}
	}
	if !sawState {
		t.Errorf("no state hook with value 7; hooks=%+v", comp.Hooks)
	}
	if !sawSignal {
		t.Errorf("no signal hook with value true; hooks=%+v", comp.Hooks)
	}

	// A host node with the label should be present under the component.
	host := findNode(snap, func(n *reactive.ReactiveNode) bool { return n.Kind == "host" && n.WidgetRole != "" })
	if host == nil {
		t.Errorf("no host node with a widget role found:\n%s", snap.String())
	}
}

func TestRuntimeForWindowAndDirty(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 200, H: 200})
	var setName func(string)
	app := func() reactive.Element {
		return uiElem(reactive.Component("Box", "", 0, func(int) reactive.Element {
			name, set := reactive.UseState("a")
			setName = set
			return uiLabel(name).Build()
		})).Build()
	}
	rt := reactive.Mount(win, app)

	if reactive.RuntimeForWindow(win) != rt {
		t.Fatal("RuntimeForWindow did not return the mounted runtime")
	}

	// Inspect reflects committed state: the setState (which flushes
	// synchronously in a test window) is visible, and the tree is clean
	// once settled. (The transient Dirty=true window between a batched
	// setState and the next frame only appears under a deferred scheduler.)
	setName("b")
	snap := rt.Inspect()
	box := findNode(snap, func(n *reactive.ReactiveNode) bool { return n.Name == "Box" })
	if box == nil {
		t.Fatal("Box component not found")
	}
	var stateVal string
	for _, h := range box.Hooks {
		if h.Kind == "state" {
			stateVal = h.Value
		}
	}
	if stateVal != "b" {
		t.Errorf("state after setState = %q, want b", stateVal)
	}
	if box.Dirty {
		t.Errorf("component should be clean once settled; box=%+v", box)
	}
}
