package chart

import "testing"

func TestParseColorHexAndRGB(t *testing.T) {
	tests := []struct {
		in   string
		want [4]float32
	}{
		{"#5470c6", [4]float32{0x54 / 255.0, 0x70 / 255.0, 0xc6 / 255.0, 1}},
		{"#fff", [4]float32{1, 1, 1, 1}},
		{"#00000080", [4]float32{0, 0, 0, 128 / 255.0}},
		{"rgb(255, 0, 128)", [4]float32{1, 0, 128 / 255.0, 1}},
		{"rgba(0,255,0,0.5)", [4]float32{0, 1, 0, 128 / 255.0}},
		{"transparent", [4]float32{0, 0, 0, 0}},
	}
	for _, tt := range tests {
		got, ok := ParseColor(tt.in)
		if !ok {
			t.Fatalf("ParseColor(%q) failed", tt.in)
		}
		if got.R != tt.want[0] || got.G != tt.want[1] || got.B != tt.want[2] || got.A != tt.want[3] {
			t.Fatalf("ParseColor(%q) = %+v, want %v", tt.in, got, tt.want)
		}
	}
	if _, ok := ParseColor("not-a-color"); ok {
		t.Fatal("invalid color should fail")
	}
}
