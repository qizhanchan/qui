//go:build darwin && cgo

package qui

import (
	"image/color"
	"testing"
)

func TestNativeNSCursorLifecycle(t *testing.T) {
	shape, err := NewImageCursorScaled(solidCursorImage(64, 64, color.NRGBA{R: 255, A: 128}), 2, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	p := nativeNSCursor(shape)
	if p == nil {
		t.Fatal("NSCursor not created for a 2x image cursor")
	}
	if again := nativeNSCursor(shape); again != p {
		t.Fatal("NSCursor must be cached per shape")
	}
	if nativeNSCursor(CursorHidden) == nil {
		t.Fatal("no transparent cursor for CursorHidden")
	}
	if nativeNSCursor(CursorHand) != nil {
		t.Fatal("built-in shapes have no custom NSCursor")
	}
	ReleaseCursor(shape)
	if _, ok := nsCursors[shape]; ok {
		t.Fatal("ReleaseCursor left the NSCursor cached")
	}
}

func TestPremultiplied(t *testing.T) {
	px := premultiplied(solidCursorImage(1, 1, color.NRGBA{R: 200, G: 100, B: 50, A: 128}))
	if px[0] != 100 || px[1] != 50 || px[2] != 25 || px[3] != 128 {
		t.Fatalf("premultiplied = %v", px)
	}
}
