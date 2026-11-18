package svg

import (
	"github.com/qizhanchan/qui"
	"testing"
)

func TestLinearGradientPaintServerAfterReference(t *testing.T) {
	d, err := ParseBytes([]byte(`<svg viewBox="0 0 20 10" fill="none"><rect x="2" y="2" width="16" height="6" fill="url(#g)"/><defs><linearGradient id="g" x1="2" y1="0" x2="18" y2="0" gradientUnits="userSpaceOnUse"><stop stop-color="#ff0000"/><stop offset="1" stop-color="#0000ff"/></linearGradient></defs></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	img := d.Rasterize(40, 20, qui.Color{})
	r, _, b, a := img.At(6, 10).RGBA()
	if a < 60000 || r <= b {
		t.Fatalf("left gradient pixel: %v", img.At(6, 10))
	}
	r, _, b, a = img.At(34, 10).RGBA()
	if a < 60000 || b <= r {
		t.Fatalf("right gradient pixel: %v", img.At(34, 10))
	}
	if _, _, _, a = img.At(0, 0).RGBA(); a != 0 {
		t.Fatal("gradient escaped geometry")
	}
}
func TestObjectBoundingBoxGradientTransform(t *testing.T) {
	d, err := ParseBytes([]byte(`<svg viewBox="0 0 40 20"><defs><linearGradient id="g"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient></defs><g transform="translate(10 0)"><rect width="20" height="20" fill="url(#g)" fill-opacity="0.5"/></g></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	img := d.Rasterize(40, 20, qui.Color{})
	r, _, b, a := img.At(12, 10).RGBA()
	if r <= b || a < 32000 || a > 34000 {
		t.Fatalf("gradient coordinates/opacity: %v", img.At(12, 10))
	}
}

func TestRadialGradientPaintServerTransform(t *testing.T) {
	for _, units := range []string{`cx=".5" cy=".5" r=".5"`, `cx="0" cy="0" r="1" gradientUnits="userSpaceOnUse" gradientTransform="translate(20 10) scale(20 10)"`} {
		d, err := ParseBytes([]byte(`<svg viewBox="0 0 40 20"><rect width="40" height="20" fill="url(#g)"/><defs><radialGradient id="g" ` + units + `><stop stop-color="red"/><stop offset="1" stop-color="blue"/></radialGradient></defs></svg>`))
		if err != nil {
			t.Fatal(err)
		}
		img := d.Rasterize(40, 20, qui.Color{})
		r, _, b, a := img.At(20, 10).RGBA()
		if a < 65000 || r < b {
			t.Fatalf("radial center: %v", img.At(20, 10))
		}
		r, _, b, a = img.At(1, 10).RGBA()
		if a < 65000 || b < r {
			t.Fatalf("radial edge: %v", img.At(1, 10))
		}
	}
}
