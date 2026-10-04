package svg

import (
	"math"
	"testing"
)

func TestParsePathAbsoluteCommands(t *testing.T) {
	cases := []struct {
		name string
		d    string
		want []PathCommand
	}{
		{
			name: "M-L-Z",
			d:    "M 10 20 L 30 40 Z",
			want: []PathCommand{
				{Op: OpMoveTo, Args: [7]float32{10, 20}},
				{Op: OpLineTo, Args: [7]float32{30, 40}},
				{Op: OpClose},
			},
		},
		{
			name: "H + V",
			d:    "M 5 5 H 25 V 15",
			want: []PathCommand{
				{Op: OpMoveTo, Args: [7]float32{5, 5}},
				{Op: OpLineTo, Args: [7]float32{25, 5}},
				{Op: OpLineTo, Args: [7]float32{25, 15}},
			},
		},
		{
			name: "cubic",
			d:    "M 0 0 C 10 0 20 10 20 20",
			want: []PathCommand{
				{Op: OpMoveTo, Args: [7]float32{0, 0}},
				{Op: OpCurveTo, Args: [7]float32{10, 0, 20, 10, 20, 20}},
			},
		},
		{
			name: "quadratic",
			d:    "M 0 0 Q 10 0 20 20",
			want: []PathCommand{
				{Op: OpMoveTo, Args: [7]float32{0, 0}},
				{Op: OpQuadTo, Args: [7]float32{10, 0, 20, 20}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParsePath(tc.d)
			if err != nil {
				t.Fatalf("parse %q: %v", tc.d, err)
			}
			if !commandsEqual(p.Commands, tc.want) {
				t.Errorf("got %v, want %v", p.Commands, tc.want)
			}
		})
	}
}

func TestParsePathRelative(t *testing.T) {
	// "m 10 10 l 5 0 l 0 5" — relative line segments accumulating
	// from the previous point.
	p, err := ParsePath("m 10 10 l 5 0 l 0 5")
	if err != nil {
		t.Fatal(err)
	}
	want := []PathCommand{
		{Op: OpMoveTo, Args: [7]float32{10, 10}},
		{Op: OpLineTo, Args: [7]float32{15, 10}},
		{Op: OpLineTo, Args: [7]float32{15, 15}},
	}
	if !commandsEqual(p.Commands, want) {
		t.Errorf("got %v, want %v", p.Commands, want)
	}
}

func TestParsePathImplicitLineto(t *testing.T) {
	// "M 0 0 10 10 20 20" — extra coord pairs after M are implicit L
	// (per SVG spec — first surprise everyone hits).
	p, err := ParsePath("M 0 0 10 10 20 20")
	if err != nil {
		t.Fatal(err)
	}
	want := []PathCommand{
		{Op: OpMoveTo, Args: [7]float32{0, 0}},
		{Op: OpLineTo, Args: [7]float32{10, 10}},
		{Op: OpLineTo, Args: [7]float32{20, 20}},
	}
	if !commandsEqual(p.Commands, want) {
		t.Errorf("got %v, want %v", p.Commands, want)
	}
}

func TestParsePathSmoothCubicReflection(t *testing.T) {
	// "M 0 0 C 10 0 20 10 30 10 S 50 30 60 30"
	// Smooth S reflects the previous cubic's second control point
	// through the current point: (30,10) reflected through (30,10)
	// of last control (20,10) gives (40,10).
	p, err := ParsePath("M 0 0 C 10 0 20 10 30 10 S 50 30 60 30")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Commands) != 3 {
		t.Fatalf("want 3 commands, got %d", len(p.Commands))
	}
	c := p.Commands[2]
	if c.Op != OpCurveTo {
		t.Fatalf("expected CurveTo, got %v", c.Op)
	}
	// Reflected control = 2*current - last_ctrl = 2*(30,10) - (20,10) = (40,10)
	if !approx(c.Args[0], 40) || !approx(c.Args[1], 10) {
		t.Errorf("reflected control = (%v, %v), want (40, 10)", c.Args[0], c.Args[1])
	}
}

func TestParsePathArcFlags(t *testing.T) {
	// Arc command with no separator after flags — valid per spec.
	// "0150 0" decomposes as large=0, sweep=1, x=50, y=0.
	p, err := ParsePath("M 0 0 A 25 25 0 0150 0")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Commands) != 2 {
		t.Fatalf("want 2 commands, got %d", len(p.Commands))
	}
	a := p.Commands[1]
	if a.Op != OpArcTo {
		t.Fatalf("expected ArcTo, got %v", a.Op)
	}
	// rx=25 ry=25 rot=0 large=0 sweep=1 x=50 y=0
	want := [7]float32{25, 25, 0, 0, 1, 50, 0}
	if a.Args != want {
		t.Errorf("ArcTo args = %v, want %v", a.Args, want)
	}
}

func TestParsePathExponents(t *testing.T) {
	// Numbers in scientific notation should parse — uncommon but
	// valid SVG output from some tools.
	p, err := ParsePath("M 0 0 L 1e2 2e1")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Commands) != 2 {
		t.Fatalf("want 2 commands, got %d", len(p.Commands))
	}
	if !approx(p.Commands[1].Args[0], 100) || !approx(p.Commands[1].Args[1], 20) {
		t.Errorf("got %v, want (100, 20)", p.Commands[1].Args)
	}
}

func commandsEqual(a, b []PathCommand) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Op != b[i].Op {
			return false
		}
		for j := 0; j < 7; j++ {
			if !approx(a[i].Args[j], b[i].Args[j]) {
				return false
			}
		}
	}
	return true
}

func approx(a, b float32) bool {
	return math.Abs(float64(a-b)) < 1e-4
}
