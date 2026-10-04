package qui

import "testing"

func TestControlledLayoutMetadataInvalidatesRoot(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*BaseWidget)
	}{
		{
			name:  "flex",
			apply: func(widget *BaseWidget) { widget.SetFlexItem(FlexItem{Grow: 1}) },
		},
		{
			name: "grid",
			apply: func(widget *BaseWidget) {
				widget.SetGridItem(GridItem{Col: 1, Row: 2, Explicit: true})
			},
		},
		{
			name: "absolute",
			apply: func(widget *BaseWidget) {
				widget.SetAbsolutePosition(AbsolutePosition{Anchor: AnchorLeft | AnchorTop, Left: 10})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			child := &BaseWidget{}
			child.SetSelf(child)
			root := NewContainer(nil, child)
			root.ClearLayoutDirty()

			tt.apply(child)
			if !root.IsLayoutDirty() {
				t.Fatal("metadata update did not dirty the tree root")
			}

			root.ClearLayoutDirty()
			tt.apply(child)
			if root.IsLayoutDirty() {
				t.Fatal("identical metadata update dirtied layout")
			}
		})
	}
}

func TestLayoutMetadataValueMethodsReturnSnapshots(t *testing.T) {
	widget := &BaseWidget{}
	widget.SetSelf(widget)
	widget.SetFlexItem(FlexItem{Grow: 1})
	widget.SetGridItem(GridItem{Col: 2, Row: 3, Explicit: true})
	widget.SetAbsolutePosition(AbsolutePosition{Anchor: AnchorRight, Right: 8})

	flex := widget.FlexItemValue()
	grid := widget.GridItemValue()
	absolute := widget.AbsolutePositionValue()
	flex.Grow = 9
	grid.Col = 9
	absolute.Right = 99

	if widget.FlexItemValue().Grow != 1 || widget.GridItemValue().Col != 2 || widget.AbsolutePositionValue().Right != 8 {
		t.Fatal("mutating metadata snapshots changed widget storage")
	}
}

func TestSetGridCellPreservesSpans(t *testing.T) {
	widget := &BaseWidget{}
	widget.SetSelf(widget)
	widget.SetGridItem(GridItem{ColSpan: 3, RowSpan: 2})
	widget.SetGridCell(4, 5)

	got := widget.GridItemValue()
	if got.Col != 4 || got.Row != 5 || !got.Explicit || got.ColSpan != 3 || got.RowSpan != 2 {
		t.Fatalf("SetGridCell result = %+v", got)
	}
}

func TestSetFlexUsesControlledSemantics(t *testing.T) {
	widget := &BaseWidget{}
	widget.SetSelf(widget)
	widget.SetFlexItem(FlexItem{Basis: 40, Align: AlignEnd})
	widget.ClearLayoutDirty()

	widget.SetFlex(-1)
	got := widget.FlexItemValue()
	if got.Grow != 0 || got.Basis != 0 || got.Align != AlignEnd {
		t.Fatalf("SetFlex result = %+v", got)
	}
	if !widget.IsLayoutDirty() {
		t.Fatal("SetFlex did not invalidate layout")
	}
}

func TestPositioningFlagsInvalidateLayout(t *testing.T) {
	child := &BaseWidget{}
	child.SetSelf(child)
	root := NewContainer(nil, child)
	root.ClearLayoutDirty()

	child.SetOutOfFlow(true)
	if !root.IsLayoutDirty() {
		t.Fatal("SetOutOfFlow did not dirty layout")
	}
	root.ClearLayoutDirty()
	child.SetEstablishesAbsContainingBlock(true)
	if !root.IsLayoutDirty() {
		t.Fatal("SetEstablishesAbsContainingBlock did not dirty layout")
	}
}

type splitMetadataWidget struct {
	BaseWidget
	legacyFlex       FlexItem
	controlledFlex   FlexItem
	legacyGrid       GridItem
	controlledGrid   GridItem
	legacyAbsolute   AbsolutePosition
	controlledAbsPos AbsolutePosition
}

func newSplitMetadataWidget() *splitMetadataWidget {
	widget := &splitMetadataWidget{BaseWidget: NewBaseWidget()}
	widget.SetSelf(widget)
	return widget
}

func (w *splitMetadataWidget) FlexItem() *FlexItem { return &w.legacyFlex }
func (w *splitMetadataWidget) FlexItemValue() FlexItem {
	return w.controlledFlex
}
func (w *splitMetadataWidget) GridItem() *GridItem { return &w.legacyGrid }
func (w *splitMetadataWidget) GridItemValue() GridItem {
	return w.controlledGrid
}
func (w *splitMetadataWidget) AbsolutePosition() *AbsolutePosition { return &w.legacyAbsolute }
func (w *splitMetadataWidget) AbsolutePositionValue() AbsolutePosition {
	return w.controlledAbsPos
}

func TestLayoutMetadataReadersPreferValueAPI(t *testing.T) {
	widget := newSplitMetadataWidget()
	widget.legacyFlex = FlexItem{Grow: 1}
	widget.controlledFlex = FlexItem{Grow: 2}
	widget.legacyGrid = GridItem{Col: 1}
	widget.controlledGrid = GridItem{Col: 2}
	widget.legacyAbsolute = AbsolutePosition{Left: 1}
	widget.controlledAbsPos = AbsolutePosition{Left: 2}

	if got := FlexItemOf(widget); got.Grow != 2 {
		t.Fatalf("FlexItemOf = %+v, want controlled value", got)
	}
	if got := GridItemOf(widget); got.Col != 2 {
		t.Fatalf("GridItemOf = %+v, want controlled value", got)
	}
	if got := AbsolutePositionOf(widget); got.Left != 2 {
		t.Fatalf("AbsolutePositionOf = %+v, want controlled value", got)
	}
}
