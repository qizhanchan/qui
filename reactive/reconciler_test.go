package reactive

import (
	"fmt"
	"testing"

	"github.com/qizhanchan/qui"
)

type testLeaf struct {
	qui.BaseWidget
	text string
}

func newTestLeaf() *testLeaf {
	return &testLeaf{BaseWidget: qui.NewBaseWidget()}
}

func (l *testLeaf) HitTest(p qui.Point) qui.Widget {
	if l.Bounds().Contains(p) {
		return l
	}
	return nil
}

func leafElem(key, text string) Element {
	return Leaf[*testLeaf](
		"leaf",
		key,
		func() *testLeaf { return newTestLeaf() },
		func(w *testLeaf) Flags {
			if w.text == text {
				return FlagNone
			}
			w.text = text
			return FlagLayout
		},
	)
}

func containerElem(children ...Element) Element {
	return Node[*qui.Container](
		"container",
		"",
		func() *qui.Container { return qui.NewContainer(nil) },
		nil,
		SetContainerChildren,
		children...,
	)
}

func TestReconcilerReuseAndLayoutFlagOnUpdate(t *testing.T) {
	var r Reconciler

	root1, flags1 := r.Render(containerElem(leafElem("title", "hello")))
	if root1 == nil {
		t.Fatal("first render should mount root")
	}
	if !flags1.has(FlagLayout) {
		t.Fatalf("first render should request layout, got flags=%v", flags1)
	}

	root2, flags2 := r.Render(containerElem(leafElem("title", "world")))
	if root2 != root1 {
		t.Fatal("container should be reused across renders")
	}
	if !flags2.has(FlagLayout) {
		t.Fatalf("text update should request layout, got flags=%v", flags2)
	}

	container := root2.(*qui.Container)
	if container.ChildCount() != 1 {
		t.Fatalf("expected one child, got %d", container.ChildCount())
	}
	leaf := container.ChildAt(0).(*testLeaf)
	if leaf.text != "world" {
		t.Fatalf("expected updated text 'world', got %q", leaf.text)
	}
}

func TestReconcilerKeyedReorderReusesWidgets(t *testing.T) {
	var r Reconciler

	initial := containerElem(
		leafElem("a", "a"),
		leafElem("b", "b"),
		leafElem("c", "c"),
	)
	root1, _ := r.Render(initial)
	container1 := root1.(*qui.Container)

	byText := map[string]*testLeaf{}
	for _, child := range container1.Children() {
		leaf := child.(*testLeaf)
		byText[leaf.text] = leaf
	}

	reordered := containerElem(
		leafElem("c", "c"),
		leafElem("a", "a"),
		leafElem("b", "b"),
	)
	root2, flags := r.Render(reordered)
	container2 := root2.(*qui.Container)

	if container2 != container1 {
		t.Fatal("container should be reused on keyed reorder")
	}
	if !flags.has(FlagLayout) {
		t.Fatalf("reorder should request layout, got flags=%v", flags)
	}

	gotOrder := make([]string, 0, container2.ChildCount())
	for _, child := range container2.Children() {
		leaf := child.(*testLeaf)
		gotOrder = append(gotOrder, leaf.text)
		if byText[leaf.text] != leaf {
			t.Fatalf("leaf %q should be reused, got a new instance", leaf.text)
		}
	}
	want := []string{"c", "a", "b"}
	for i := range want {
		if gotOrder[i] != want[i] {
			t.Fatalf("unexpected order at index %d: got=%q want=%q", i, gotOrder[i], want[i])
		}
	}
}

func TestSetContainerChildrenSetsParentLinks(t *testing.T) {
	container := qui.NewContainer(nil)
	child1 := newTestLeaf()
	child2 := newTestLeaf()

	SetContainerChildren(container, []qui.Widget{child1, child2})

	if child1.Parent() != container {
		t.Fatal("child1 parent was not assigned")
	}
	if child2.Parent() != container {
		t.Fatal("child2 parent was not assigned")
	}
}

func TestRuntimeBindSignalRerender(t *testing.T) {
	count := NewSignal(0)
	window := qui.NewTestWindow(qui.Size{W: 800, H: 600})

	runtime := NewRuntime(window, func() Element {
		return containerElem(leafElem("counter", fmt.Sprintf("count:%d", count.Get())))
	})
	unbind := runtime.Bind(count)
	defer unbind()

	runtime.Render()
	leafBefore := window.Root().(*qui.Container).ChildAt(0).(*testLeaf)
	if leafBefore.text != "count:0" {
		t.Fatalf("unexpected initial text %q", leafBefore.text)
	}

	count.Set(3)
	leafAfter := window.Root().(*qui.Container).ChildAt(0).(*testLeaf)
	if leafAfter.text != "count:3" {
		t.Fatalf("expected rerendered text count:3, got %q", leafAfter.text)
	}
	if leafAfter != leafBefore {
		t.Fatal("signal update should reuse keyed leaf instance")
	}
}

func TestReconcilerEmptyUnmountsRoot(t *testing.T) {
	var r Reconciler
	root, _ := r.Render(containerElem(leafElem("x", "x")))
	if root == nil {
		t.Fatal("expected mounted root")
	}

	unmounted, flags := r.Render(Empty())
	if unmounted != nil {
		t.Fatal("expected nil root after Empty render")
	}
	if !flags.has(FlagLayout) {
		t.Fatalf("unmount should request layout, got flags=%v", flags)
	}
}
