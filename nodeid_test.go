package qui_test

import (
	"errors"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

func TestNodeID_StableAcrossSiblingInsertion(t *testing.T) {
	w, submit, name, _ := newTree(t)
	before := w.AccessibilityTree()
	nameID := before.Root.Children[1].NodeID
	if nameID == 0 || nameID != name.NodeID() {
		t.Fatalf("AX nodeId = %d, widget NodeID = %d", nameID, name.NodeID())
	}
	pathBefore := before.Root.Children[1].Path

	// Insert a sibling in front: the positional path shifts, the ID must not.
	c := w.Root().(*qui.Container)
	c.InsertChild(0, widgets.NewLabel("banner"))
	c.Measure(qui.Size{W: 400, H: 300})
	c.Layout(qui.Rect{W: 400, H: 300})

	n, err := w.NodeByID(nameID)
	if err != nil {
		t.Fatalf("NodeByID after insert: %v", err)
	}
	if n.Widget() != name {
		t.Fatalf("NodeByID resolved to %T, want the input", n.Widget())
	}
	if n.Path == pathBefore {
		t.Fatalf("path %q did not shift — test is not exercising the insertion", n.Path)
	}
	if submit.NodeID() == name.NodeID() {
		t.Fatal("distinct widgets share a NodeID")
	}
}

func TestNodeID_RefSelectorAndStale(t *testing.T) {
	w, submit, _, agree := newTree(t)
	ref := submit.NodeID().String()
	got, err := w.Find(ref)
	if err != nil || got != submit {
		t.Fatalf("Find(%s) = %v, %v; want submit", ref, got, err)
	}
	// A ref combines with other criteria like any other predicate.
	if got, _ := w.Find(ref + "[role=checkbox]"); got != nil {
		t.Fatalf("@ref with a contradicting role matched %T", got)
	}

	// Remove the checkbox: its reference is stale, not a silent miss.
	agreeRef := agree.NodeID().String()
	c := w.Root().(*qui.Container)
	c.RemoveChild(agree)
	err = w.Click(agreeRef, qui.ClickOptions{})
	if !errors.Is(err, qui.ErrStaleNode) {
		t.Fatalf("click on removed node: err = %v, want ErrStaleNode", err)
	}
	if !errors.Is(err, qui.ErrNoMatch) {
		t.Fatalf("ErrStaleNode must still satisfy errors.Is(ErrNoMatch): %v", err)
	}
	if _, err := w.NodeByID(agree.NodeID()); !errors.Is(err, qui.ErrStaleNode) {
		t.Fatalf("NodeByID(removed) err = %v, want ErrStaleNode", err)
	}
}

func TestNodeID_BadRefIsSelectorError(t *testing.T) {
	w, _, _, _ := newTree(t)
	for _, sel := range []string{"@", "@0", "@-3", "@x"} {
		if _, err := w.FindNodes(sel); !errors.Is(err, qui.ErrInvalidSelector) {
			t.Errorf("FindNodes(%q) err = %v, want ErrInvalidSelector", sel, err)
		}
	}
	if id, ok := qui.ParseNodeRef("@42"); !ok || id != 42 {
		t.Errorf("ParseNodeRef(@42) = %d, %v", id, ok)
	}
}
