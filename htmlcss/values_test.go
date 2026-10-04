package htmlcss

import (
	"math"
	"testing"
)

func approxEq(a, b float32) bool { return math.Abs(float64(a-b)) < 0.01 }

func TestParseColorHSL(t *testing.T) {
	cases := []struct {
		in         string
		r, g, b, a float32
	}{
		{"hsl(0, 100%, 50%)", 1, 0, 0, 1},         // red
		{"hsl(120, 100%, 50%)", 0, 1, 0, 1},       // green
		{"hsl(240, 100%, 50%)", 0, 0, 1, 1},       // blue
		{"hsl(0, 0%, 100%)", 1, 1, 1, 1},          // white
		{"hsl(0, 0%, 0%)", 0, 0, 0, 1},            // black
		{"hsl(0 0% 50%)", 0.5, 0.5, 0.5, 1},       // space form, gray
		{"hsla(0, 100%, 50%, 0.5)", 1, 0, 0, 0.5}, // alpha
		{"hsl(240 100% 50% / 50%)", 0, 0, 1, 0.5}, // space + pct alpha
		{"hsl(-120, 100%, 50%)", 0, 0, 1, 1},      // negative hue wraps to 240 (blue)
	}
	for _, c := range cases {
		got, ok := parseColor(c.in)
		if !ok {
			t.Errorf("parseColor(%q) failed", c.in)
			continue
		}
		if !approxEq(got.R, c.r) || !approxEq(got.G, c.g) || !approxEq(got.B, c.b) || !approxEq(got.A, c.a) {
			t.Errorf("parseColor(%q) = %+v, want rgba(%v,%v,%v,%v)", c.in, got, c.r, c.g, c.b, c.a)
		}
	}
}

func TestNamedColors(t *testing.T) {
	cases := []struct {
		in         string
		r, g, b, a float32
	}{
		{"rebeccapurple", 0x66 / 255.0, 0x33 / 255.0, 0x99 / 255.0, 1},
		{"dodgerblue", 0x1E / 255.0, 0x90 / 255.0, 0xFF / 255.0, 1},
		{"DarkSlateGrey", 0x2F / 255.0, 0x4F / 255.0, 0x4F / 255.0, 1}, // case-insensitive + grey alias
		{"transparent", 0, 0, 0, 0},
		{"tomato", 0xFF / 255.0, 0x63 / 255.0, 0x47 / 255.0, 1},
	}
	for _, c := range cases {
		got, ok := parseColor(c.in)
		if !ok {
			t.Errorf("parseColor(%q) failed", c.in)
			continue
		}
		if !approxEq(got.R, c.r) || !approxEq(got.G, c.g) || !approxEq(got.B, c.b) || !approxEq(got.A, c.a) {
			t.Errorf("parseColor(%q) = %+v, want rgba(%v,%v,%v,%v)", c.in, got, c.r, c.g, c.b, c.a)
		}
	}
	if _, ok := parseColor("notacolor"); ok {
		t.Error("parseColor accepted an unknown name")
	}
}

func TestParseCalc(t *testing.T) {
	cases := []struct {
		in     string
		font   float32
		pctRef float32
		want   float32
	}{
		{"calc(100% - 40px)", 16, 200, 160},
		{"calc(100%-40px)", 16, 200, 160}, // no spaces
		{"calc(50% + 10px)", 16, 200, 110},
		{"calc(20px * 2)", 16, 0, 40},
		{"calc(100px / 4)", 16, 0, 25},
		{"calc(2em + 8px)", 10, 0, 28},           // 2*10 + 8
		{"calc((100% - 20px) / 2)", 16, 200, 90}, // parens + precedence
		{"calc(10px + 20px * 2)", 16, 0, 50},     // precedence
		{"calc(1rem + 4px)", 16, 0, 20},          // rem=16
	}
	for _, c := range cases {
		got, ok := parseLength(c.in, c.font, c.pctRef)
		if !ok {
			t.Errorf("parseLength(%q) failed", c.in)
			continue
		}
		if !approxEq(got, c.want) {
			t.Errorf("parseLength(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseMinMaxClamp(t *testing.T) {
	cases := []struct {
		in     string
		pctRef float32
		want   float32
	}{
		{"min(100px, 50px, 80px)", 0, 50},
		{"max(100px, 50px, 80px)", 0, 100},
		{"clamp(20px, 50px, 40px)", 0, 40},  // above max → max
		{"clamp(60px, 50px, 100px)", 0, 60}, // below min → min
		{"clamp(20px, 50px, 100px)", 0, 50}, // within → val
		{"min(50%, 120px)", 200, 100},       // 50% of 200 = 100 < 120
		{"max(calc(10px + 10px), 15px)", 0, 20},
	}
	for _, c := range cases {
		got, ok := parseLength(c.in, 16, c.pctRef)
		if !ok {
			t.Errorf("parseLength(%q) failed", c.in)
			continue
		}
		if !approxEq(got, c.want) {
			t.Errorf("parseLength(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseCalcInvalid(t *testing.T) {
	bad := []string{"calc()", "calc(10px +)", "calc(10px / 0)", "calc(10px 20px)"}
	for _, s := range bad {
		if _, ok := parseLength(s, 16, 100); ok {
			t.Errorf("parseLength(%q) unexpectedly succeeded", s)
		}
	}
}
