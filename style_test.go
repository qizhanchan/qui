package qui

import (
	"reflect"
	"testing"
)

// FlexLayout must honor Grow declared in Style, not just in
// FlexItem. Same behavior as SetFlex(1) on the child.
func TestFlexLayoutReadsStyleGrow(t *testing.T) {
	a := &BaseWidget{}
	a.SetSelf(a)
	b := &BaseWidget{}
	b.SetSelf(b)
	b.Style().Grow = 1 // declarative "fill remaining"

	container := NewContainer(FlexLayout{Direction: Horizontal})
	container.AddChild(a)
	container.AddChild(b)

	container.Layout(Rect{X: 0, Y: 0, W: 200, H: 20})

	if b.Bounds().W <= a.Bounds().W {
		t.Fatalf("Style.Grow=1 should stretch child b: a.W=%v b.W=%v",
			a.Bounds().W, b.Bounds().W)
	}
}

// Style.MinWidth must floor a child's laid-out width, mirroring
// SetMinSize.
func TestStyleMinWidthFloorsLayout(t *testing.T) {
	child := &BaseWidget{}
	child.SetSelf(child)
	child.Style().MinWidth = 80

	container := NewContainer(FlexLayout{Direction: Horizontal})
	container.AddChild(child)
	container.Layout(Rect{X: 0, Y: 0, W: 200, H: 20})

	if child.Bounds().W < 80 {
		t.Fatalf("Style.MinWidth=80 should floor W, got %v", child.Bounds().W)
	}
}

// Interface (SetMinSize) must win over Style when both are set on
// the same axis — Style is a fallback, not an override.
func TestInterfaceMinSizeWinsOverStyle(t *testing.T) {
	child := &BaseWidget{}
	child.SetSelf(child)
	child.SetMinSize(120, 0)    // interface path
	child.Style().MinWidth = 40 // should be ignored on this axis

	container := NewContainer(FlexLayout{Direction: Horizontal})
	container.AddChild(child)
	container.Layout(Rect{X: 0, Y: 0, W: 200, H: 20})

	if child.Bounds().W < 120 {
		t.Fatalf("interface MinSize=120 should win, got %v", child.Bounds().W)
	}
}

// Style.Width acts as a preferred-size override — same as
// SetPreferredSize on that axis.
func TestStyleWidthActsAsPreferred(t *testing.T) {
	child := &BaseWidget{}
	child.SetSelf(child)
	child.Style().Width = 60

	container := NewContainer(FlexLayout{Direction: Horizontal})
	container.AddChild(child)
	container.Layout(Rect{X: 0, Y: 0, W: 200, H: 20})

	if child.Bounds().W != 60 {
		t.Fatalf("Style.Width=60 should size child to 60, got %v", child.Bounds().W)
	}
}

// widgetFlexItem merges FlexItem-first, Style-fallback field-by-field.
func TestWidgetFlexItemMergesFields(t *testing.T) {
	w := &BaseWidget{}
	w.SetSelf(w)
	w.SetFlexItem(FlexItem{Grow: 2}) // interface set
	w.Style().Grow = 5               // should be ignored (interface wins)
	w.Style().Basis = 40             // no interface value → Style fills in
	w.Style().AlignSelf = AlignEnd
	fx := widgetFlexItem(w)
	if fx.Grow != 2 {
		t.Errorf("Grow: interface should win, got %v want 2", fx.Grow)
	}
	if fx.Basis != 40 {
		t.Errorf("Basis: Style should fill in when interface is 0, got %v want 40", fx.Basis)
	}
	if fx.Align != AlignEnd {
		t.Errorf("AlignSelf from Style should reach FlexItem.Align, got %v want AlignEnd", fx.Align)
	}
}

func TestUpdateStyleInvalidatesPaintWithoutDirtyingLayout(t *testing.T) {
	root := NewContainer(nil)
	root.Layout(Rect{W: 100, H: 80})
	w := NewTestWindow(Size{W: 100, H: 80})
	w.SetRoot(root)
	root.ClearLayoutDirty()
	w.ClearDirtyRegion()

	UpdateStyle(root, func(style *Style) {
		style.Background = Color{R: 1, A: 1}
	})

	if root.IsLayoutDirty() {
		t.Fatal("paint-only style update dirtied layout")
	}
	if w.DirtyRegion().IsEmpty() {
		t.Fatal("paint-only style update did not invalidate paint")
	}
}

func TestUpdateStyleInvalidatesLayoutForGeometryChange(t *testing.T) {
	root := NewContainer(nil)
	root.Layout(Rect{W: 100, H: 80})
	w := NewTestWindow(Size{W: 100, H: 80})
	w.SetRoot(root)
	root.ClearLayoutDirty()
	w.ClearDirtyRegion()

	UpdateStyle(root, func(style *Style) {
		style.Padding.Left = 20
	})

	if !root.IsLayoutDirty() {
		t.Fatal("geometry style update did not dirty layout")
	}
	if w.DirtyRegion().IsEmpty() {
		t.Fatal("geometry style update did not invalidate paint")
	}
}

func TestSetStyleOwnsCompositeFields(t *testing.T) {
	widget := &BaseWidget{}
	widget.SetSelf(widget)
	input := Style{
		Font: Font{
			Features:   []string{"liga"},
			Variations: map[string]float32{"wght": 640},
		},
		ExtraShadows: []ShadowStyle{{Blur: 4, Color: Color{A: 0.5}}},
	}
	widget.SetStyle(input)

	input.Font.Features[0] = "kern"
	input.Font.Variations["wght"] = 300
	input.ExtraShadows[0].Blur = 20
	snapshot := StyleValue(widget)
	if snapshot.Font.Features[0] != "liga" || snapshot.Font.Variations["wght"] != 640 || snapshot.ExtraShadows[0].Blur != 4 {
		t.Fatalf("SetStyle retained caller-owned aliases: %+v", snapshot)
	}

	snapshot.Font.Features[0] = "ss01"
	snapshot.Font.Variations["wght"] = 900
	snapshot.ExtraShadows[0].Blur = 30
	again := StyleValue(widget)
	if again.Font.Features[0] != "liga" || again.Font.Variations["wght"] != 640 || again.ExtraShadows[0].Blur != 4 {
		t.Fatalf("StyleValue exposed widget-owned aliases: %+v", again)
	}
}

func TestStylePatchAppliesExplicitZeroValues(t *testing.T) {
	base := Style{
		Background:     Color{R: 1, A: 1},
		Padding:        Insets{Top: 8, Right: 8, Bottom: 8, Left: 8},
		MarginLeftAuto: true,
		Opacity:        0.75,
		ExtraShadows:   []ShadowStyle{{Blur: 4, Color: Color{A: 0.5}}},
	}
	got := base.Patched(StylePatch{
		Values: Style{
			Background:     ColorTransparent,
			Padding:        Insets{},
			MarginLeftAuto: false,
			Opacity:        0,
			ExtraShadows:   nil,
		},
		Fields: StyleFieldBackground |
			StyleFieldPadding |
			StyleFieldMarginLeftAuto |
			StyleFieldOpacity |
			StyleFieldExtraShadows,
	})

	if got.Background != ColorTransparent || got.Padding != (Insets{}) || got.MarginLeftAuto || got.Opacity != 0 || got.ExtraShadows != nil {
		t.Fatalf("explicit zero patch was not applied: %+v", got)
	}
}

func TestApplyStylePatchUsesWidgetInvalidation(t *testing.T) {
	root := NewContainer(nil)
	root.Layout(Rect{W: 100, H: 80})
	w := NewTestWindow(Size{W: 100, H: 80})
	w.SetRoot(root)
	root.ClearLayoutDirty()
	w.ClearDirtyRegion()

	ApplyStylePatch(root, StylePatch{
		Values: Style{Padding: Insets{}},
		Fields: StyleFieldPadding,
	})

	if !root.IsLayoutDirty() {
		t.Fatal("explicit padding clear did not dirty layout")
	}
}

// Merge with a zero-value override must be a pure identity — this is
// the load-bearing rule that lets partial styles (recipe outputs, user
// overrides) be layered onto a base without unset fields clobbering
// specified ones.
func TestStyleMergeZeroIsIdentity(t *testing.T) {
	base := Style{
		Background: Color{1, 0, 0, 1},
		Foreground: Color{0, 1, 0, 1},
		Border:     Color{0, 0, 1, 1},
		BorderSize: 2,
		Radius:     8,
		Padding:    Insets{Top: 4, Right: 6, Bottom: 4, Left: 6},
		Margin:     Insets{Top: 1, Left: 2},
		Font:       Font{Family: "Roboto", Size: 14, Weight: FontWeightMedium},
		LineHeight: 1.4,
		Width:      100,
		MinHeight:  20,
		Grow:       1,
		AlignSelf:  AlignCenter,
		Opacity:    0.9,
		Shadow:     ShadowStyle{Y: 2, Blur: 4, Color: Color{0, 0, 0, 0.2}},
	}
	got := base.MergedWith(Style{})
	if !reflect.DeepEqual(got, base) {
		t.Fatalf("zero override changed base:\nbase=%+v\ngot =%+v", base, got)
	}
}

// Override should win on every scalar/color/insets/font/shadow field.
func TestStyleMergeOverrideWins(t *testing.T) {
	base := Style{
		Background: Color{1, 0, 0, 1},
		BorderSize: 1,
		Radius:     4,
		Padding:    Insets{Top: 1},
		Font:       Font{Family: "Roboto", Size: 12},
		Width:      50,
		Grow:       1,
		AlignSelf:  AlignStart,
		Opacity:    0.5,
	}
	over := Style{
		Background: Color{0, 0, 1, 1},
		BorderSize: 3,
		Radius:     8,
		Padding:    Insets{Right: 5},
		Font:       Font{Size: 16},
		Width:      120,
		Grow:       2,
		AlignSelf:  AlignEnd,
		Opacity:    0.8,
	}
	got := base.MergedWith(over)
	if got.Background != over.Background {
		t.Errorf("Background: got %+v want %+v", got.Background, over.Background)
	}
	if got.BorderSize != 3 {
		t.Errorf("BorderSize: got %v want 3", got.BorderSize)
	}
	if got.Radius != 8 {
		t.Errorf("Radius: got %v want 8", got.Radius)
	}
	if got.Padding != (Insets{Right: 5}) {
		t.Errorf("Padding: got %+v want {Right:5}", got.Padding)
	}
	// Font merges field-wise: family from base, size from override.
	if got.Font.Family != "Roboto" {
		t.Errorf("Font.Family should inherit from base, got %q", got.Font.Family)
	}
	if got.Font.Size != 16 {
		t.Errorf("Font.Size: got %v want 16", got.Font.Size)
	}
	if got.Width != 120 {
		t.Errorf("Width: got %v want 120", got.Width)
	}
	if got.Grow != 2 {
		t.Errorf("Grow: got %v want 2", got.Grow)
	}
	if got.AlignSelf != AlignEnd {
		t.Errorf("AlignSelf: got %v want AlignEnd", got.AlignSelf)
	}
	if got.Opacity != 0.8 {
		t.Errorf("Opacity: got %v want 0.8", got.Opacity)
	}
}

// The free-function Merge and the method MergedWith must be equivalent.
func TestMergeFunctionMatchesMethod(t *testing.T) {
	base := Style{Radius: 4, Width: 100}
	over := Style{Radius: 8, Height: 50}
	if !reflect.DeepEqual(Merge(base, over), base.MergedWith(over)) {
		t.Fatal("Merge and MergedWith diverged")
	}
}

// Transparent (A==0) colors on override must NOT clobber a specified
// base color. This is how partial styles opt out of tinting.
func TestStyleMergeTransparentColorPreservesBase(t *testing.T) {
	base := Style{Background: Color{1, 0, 0, 1}}
	over := Style{Background: Color{0, 0, 1, 0}} // A=0 → unspecified
	got := base.MergedWith(over)
	if got.Background != base.Background {
		t.Fatalf("transparent override clobbered base: got %+v want %+v",
			got.Background, base.Background)
	}
}

// Fonts merge field-wise; a totally-empty Font in override must leave
// the base font intact.
func TestFontMergeFieldWise(t *testing.T) {
	base := Font{Family: "Roboto", Size: 14, Weight: FontWeightNormal, Italic: false}
	// Empty override.
	if got := mergeFont(base, Font{}); !reflect.DeepEqual(got, base) {
		t.Errorf("empty font override changed base: %+v", got)
	}
	// Weight-only override keeps family/size.
	if got := mergeFont(base, Font{Weight: FontWeightBold}); got.Family != "Roboto" || got.Size != 14 || got.Weight != FontWeightBold {
		t.Errorf("weight-only override wrong: %+v", got)
	}
	// Italic true wins; italic false in override can't distinguish
	// "keep base" from "explicit not-italic".
	if got := mergeFont(base, Font{Italic: true}); !got.Italic {
		t.Errorf("italic override should have propagated")
	}
	baseItalic := base
	baseItalic.Italic = true
	if got := mergeFont(baseItalic, Font{Italic: false}); !got.Italic {
		t.Errorf("italic=false override should not clobber base's italic=true")
	}
}

func TestShadowStyleIsZero(t *testing.T) {
	if !(ShadowStyle{}).IsZero() {
		t.Fatal("zero ShadowStyle must report IsZero")
	}
	if (ShadowStyle{Blur: 1}).IsZero() {
		t.Fatal("non-zero ShadowStyle must not report IsZero")
	}
	// A shadow with only a color set is still non-zero.
	if (ShadowStyle{Color: Color{A: 0.5}}).IsZero() {
		t.Fatal("shadow with color must not report IsZero")
	}
}
