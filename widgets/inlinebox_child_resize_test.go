package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

type resizableBox struct {
	BaseWidget
	size Size
}

func (r *resizableBox) Measure(Size) Size { return r.size }

// Growing an atomic child re-flows the paragraph instead of laying the
// child out into the fragment sized for its old measurement.
func TestInlineBoxReflowsWhenAtomicChildResizes(t *testing.T) {
	box := NewInlineBox()
	img := &resizableBox{BaseWidget: NewBaseWidget()}
	img.SetSelf(img)
	box.AddText("before ", Font{Size: 14}, Color{A: 1}, TextDecoration(0), "")
	box.AddBox(img, InlineBaseline)
	win := NewTestWindow(Size{W: 400, H: 200})
	win.SetRoot(box)
	win.LayoutForTest()

	img.size = Size{W: 40, H: 30}
	img.InvalidateLayout()
	win.LayoutForTest()
	if got := img.Bounds(); got.W != 40 || got.H != 30 {
		t.Fatalf("atomic child laid out at %v, want 40x30", got)
	}
}
