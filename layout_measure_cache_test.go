package qui

import "testing"

// countingLeaf reports a fixed size and counts how often it is measured.
type countingLeaf struct {
	BaseWidget
	size  Size
	calls int
}

func newCountingLeaf(size Size) *countingLeaf {
	l := &countingLeaf{BaseWidget: NewBaseWidget(), size: size}
	l.SetSelf(l)
	return l
}

func (l *countingLeaf) Measure(Size) Size { l.calls++; return l.size }

func (l *countingLeaf) grow(size Size) {
	l.size = size
	l.InvalidateLayout()
}

// nestedFlex builds depth levels of single-child vertical flex containers
// around leaf — the shape where per-pass re-measurement multiplied.
func nestedFlex(leaf Widget, depth int) *Container {
	var w Widget = leaf
	var c *Container
	for i := 0; i < depth; i++ {
		c = NewContainer(FlexLayout{Direction: Vertical, AlignItems: AlignStretch}, w)
		w = c
	}
	return c
}

// skipUnderCacheVerify skips call-counting tests when
// QUI_DEBUG_LAYOUT_CACHE=1 re-measures every cache hit on purpose.
func skipUnderCacheVerify(t *testing.T) {
	if layoutCacheVerify.Load() {
		t.Skip("QUI_DEBUG_LAYOUT_CACHE re-measures every cache hit")
	}
}

func layoutOnce(root Widget, size Size) {
	MeasureChild(root, size)
	root.Layout(Rect{W: size.W, H: size.H})
}

func TestMeasureChildMeasuresEachConstraintOnce(t *testing.T) {
	skipUnderCacheVerify(t)
	leaf := newCountingLeaf(Size{W: 40, H: 20})
	root := nestedFlex(leaf, 6)
	layoutOnce(root, Size{W: 300, H: 200})
	first := leaf.calls
	// Six nested flex levels used to re-measure the leaf hundreds of
	// times; with the cache it is a handful of distinct constraints.
	if first == 0 || first > measureCacheSize*2 {
		t.Fatalf("leaf measured %d times in one pass, want a handful", first)
	}

	// Nothing changed: a second pass answers entirely from the caches.
	leaf.calls = 0
	layoutOnce(root, Size{W: 300, H: 200})
	if leaf.calls != 0 {
		t.Fatalf("clean pass re-measured the leaf %d times, want 0", leaf.calls)
	}
}

func TestInvalidateLayoutDropsTheAncestorChain(t *testing.T) {
	skipUnderCacheVerify(t)
	changed := newCountingLeaf(Size{W: 40, H: 20})
	sibling := newCountingLeaf(Size{W: 40, H: 20})
	root := NewContainer(FlexLayout{Direction: Vertical},
		nestedFlex(changed, 3),
		nestedFlex(sibling, 3),
	)
	layoutOnce(root, Size{W: 300, H: 400})

	changed.calls, sibling.calls = 0, 0
	changed.grow(Size{W: 40, H: 90})
	layoutOnce(root, Size{W: 300, H: 400})
	if changed.calls == 0 {
		t.Fatal("invalidated leaf was not re-measured")
	}
	if sibling.calls != 0 {
		t.Fatalf("clean sibling subtree re-measured %d times, want 0", sibling.calls)
	}
	if got := changed.Bounds().H; got != 90 {
		t.Fatalf("grown leaf laid out at height %v, want 90", got)
	}
}

func TestGlobalChangesDropEveryCache(t *testing.T) {
	leaf := newCountingLeaf(Size{W: 40, H: 20})
	root := nestedFlex(leaf, 2)
	w := NewTestWindow(Size{W: 300, H: 200})
	w.SetRoot(root)
	layoutOnce(root, Size{W: 300, H: 200})

	for name, change := range map[string]func(){
		"Window.InvalidateLayout": func() { w.InvalidateLayout() },
		"theme":                   func() { SetTheme(*CurrentTheme()) },
	} {
		leaf.calls = 0
		change()
		layoutOnce(root, Size{W: 300, H: 200})
		if leaf.calls == 0 {
			t.Errorf("%s: leaf answered from a cache that should be gone", name)
		}
	}
}

// selfInvalidating changes size during its own Measure — the result of
// that Measure must not be cached as if nothing happened.
type selfInvalidating struct {
	BaseWidget
	h float32
}

func (s *selfInvalidating) Measure(Size) Size {
	if s.h < 50 {
		s.h = 50
		s.InvalidateLayout()
		return Size{W: 10, H: 10}
	}
	return Size{W: 10, H: s.h}
}

func TestMeasureInvalidatedMidCallIsNotCached(t *testing.T) {
	s := &selfInvalidating{BaseWidget: NewBaseWidget()}
	s.SetSelf(s)
	if got := MeasureChild(s, Size{W: 100, H: 100}); got.H != 10 {
		t.Fatalf("first measure = %v", got)
	}
	if got := MeasureChild(s, Size{W: 100, H: 100}); got.H != 50 {
		t.Fatalf("second measure = %v, want the post-invalidation size (stale entry cached)", got)
	}
}

// widthOnlyLeaf is a countingLeaf whose size depends on the width only.
type widthOnlyLeaf struct{ countingLeaf }

func (*widthOnlyLeaf) MeasuresByWidthOnly() bool { return true }

// A widget measured by width only (inline text) is memoized per width:
// layout engines probe it at many heights in one pass, and keyed on both
// those entries crowded the memo so every frame re-measured it.
func TestMeasureChildWidthOnlyIgnoresHeight(t *testing.T) {
	skipUnderCacheVerify(t)
	l := &widthOnlyLeaf{countingLeaf{BaseWidget: NewBaseWidget(), size: Size{W: 20, H: 10}}}
	l.SetSelf(l)
	for frame := 0; frame < 3; frame++ {
		for _, w := range []float32{100, 200, 300, 400} {
			for _, h := range []float32{0, 17, 35, 71, 500} {
				MeasureChild(l, Size{W: w, H: h})
			}
		}
	}
	if l.calls != 4 {
		t.Fatalf("measured %d times, want 4 (once per width)", l.calls)
	}
	// Other widgets still key on both.
	c := newCountingLeaf(Size{W: 20, H: 10})
	MeasureChild(c, Size{W: 100, H: 0})
	MeasureChild(c, Size{W: 100, H: 50})
	if c.calls != 2 {
		t.Fatalf("plain widget measured %d times, want 2", c.calls)
	}
}

// Nested flex probes a child under more constraints per pass than the old
// memo of 6 kept; a second pass over the same constraints must hit.
func TestMeasureChildKeepsAPassOfConstraints(t *testing.T) {
	skipUnderCacheVerify(t)
	l := newCountingLeaf(Size{W: 20, H: 10})
	pass := func() {
		for i := 0; i < 12; i++ {
			MeasureChild(l, Size{W: float32(100 + i), H: float32(i % 3)})
		}
	}
	pass()
	first := l.calls
	pass()
	if l.calls != first {
		t.Fatalf("second pass measured %d more times, want 0", l.calls-first)
	}
}
