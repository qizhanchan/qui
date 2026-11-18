package svg

import (
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
)

func TestCurrentColorIndependentOfInheritedPaint(t *testing.T) {
	d, err := ParseBytes([]byte(`<svg viewBox="0 0 30 10" color="red" fill="none"><rect width="10" height="10" fill="currentColor"/><g fill="currentColor" color="blue"><rect x="10" width="10" height="10"/><rect x="20" width="10" height="10" color="green"/></g></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	img := d.Rasterize(30, 10, qui.Color{})
	for _, tc := range []struct {
		x       int
		r, g, b uint32
	}{{5, 65535, 0, 0}, {15, 0, 0, 65535}, {25, 0, 32896, 0}} {
		r, g, b, a := img.At(tc.x, 5).RGBA()
		if r != tc.r || g != tc.g || b != tc.b || a != 65535 {
			t.Errorf("pixel %d: %v", tc.x, img.At(tc.x, 5))
		}
	}
	b, err := d.MarshalSVG()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "currentColor") || !strings.Contains(string(b), "color:#ff0000") {
		t.Fatalf("lost color in serialization: %s", b)
	}
}

func TestEvenOddFillAndTranslucentColor(t *testing.T) {
	d, err := ParseBytes([]byte(`<svg viewBox="0 0 20 20"><path fill-rule="evenodd" fill="rgba(255,255,255,0.5)" d="M0 0H20V20H0Z M5 5H15V15H5Z"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	img := d.Rasterize(20, 20, qui.Color{})
	r, g, b, a := img.At(2, 2).RGBA()
	if a < 32000 || a > 33500 || r != a || g != a || b != a {
		t.Fatalf("translucent white darkened: %v", img.At(2, 2))
	}
	_, _, _, a = img.At(10, 10).RGBA()
	if a != 0 {
		t.Fatalf("evenodd hole filled: %v", img.At(10, 10))
	}
}

func TestTranslucentFillAndStrokePreserveColor(t *testing.T) {
	d, err := ParseBytes([]byte(`<svg viewBox="0 0 20 10"><rect width="10" height="10" fill="rgba(255,255,255,0.5)"/><path d="M15 0V10" stroke="rgba(255,255,255,0.5)" stroke-width="4" opacity="0.5"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	img := d.Rasterize(20, 10, qui.Color{})
	for _, tc := range []struct {
		x     int
		alpha uint32
	}{{5, 32896}, {15, 16448}} {
		r, g, b, a := img.At(tc.x, 5).RGBA()
		if a+300 < tc.alpha || a > tc.alpha+300 || r != a || g != a || b != a {
			t.Errorf("pixel %d: %v", tc.x, img.At(tc.x, 5))
		}
	}
}
