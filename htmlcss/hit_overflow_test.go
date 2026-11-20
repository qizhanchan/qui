package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// Content that overflows its box (overflow: visible) is hit where it is
// painted: a marquee row of cards wider than its 200px box, translated so
// that a card far past the box's own width shows on screen.
func TestHitOverflowingChildOfTransformedRow(t *testing.T) {
	for _, tc := range []struct{ name, rowCSS string }{
		{"translated", "transform: translateX(-500px);"},
		{"plain", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := RenderDoc(
				`<body><div id="clip"><div id="row">
				   <div id="c0" class="c"></div><div id="c1" class="c"></div><div id="c2" class="c"></div>
				   <div id="c3" class="c"></div><div id="c4" class="c"></div><div id="c5" class="c"></div>
				 </div></div></body>`,
				`body { margin: 0 } #clip { overflow-x: clip; width: 300px; }
				 #row { display: flex; width: 200px; `+tc.rowCSS+` }
				 .c { flex: none; width: 150px; height: 80px; }`,
				Options{},
			)
			win := qui.NewTestWindow(qui.Size{W: 300, H: 200})
			win.SetRoot(res.Root)
			res.Root.Layout(qui.Rect{W: 300, H: 200})
			want := "c0"
			if tc.name == "translated" {
				want = "c4" // 4×150 − 500 = 100: x=120 is in c4
			}
			hit := win.HitTestForTest(qui.Point{X: 120, Y: 40})
			if hit != res.ByID[want] {
				t.Fatalf("hit %T %p, want #%s %p", hit, hit, want, res.ByID[want])
			}
			if tc.name == "plain" {
				// x=250 is c1 (150..300), past the row's own 200px.
				if hit := win.HitTestForTest(qui.Point{X: 250, Y: 40}); hit != res.ByID["c1"] {
					t.Fatalf("overflowing part of c1 not hit: %T", hit)
				}
			}
		})
	}
}
