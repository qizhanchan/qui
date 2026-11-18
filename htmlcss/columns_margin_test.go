package htmlcss

import (
	"fmt"
	"math"
	"testing"
)

func TestColumnsContainCollapsedChildMargins(t *testing.T) {
	SetWebCompat(true)
	defer SetWebCompat(false)
	for _, columns := range []string{"column-count:1", "column-width:432px", "columns:1", "columns:432px", "column-count:2"} {
		t.Run(columns, func(t *testing.T) {
			// The multicol root contains the list's margins, while the list
			// itself collapses with its first and last child's margins.
			boxes := domLayoutBoxes(fmt.Sprintf(`<!doctype html><style>
body{margin:0} #host{width:300px;%s}
ol{margin:5px 0 7px;padding:0} li{height:20px;margin:10px 0 3px}
#after{height:10px}
</style><div id="host"><ol id="list"><li id="child"></li></ol></div><div id="after"></div>`, columns), true)
			wantHeight := float32(37) // max(5,10) + 20 + max(7,3)
			if columns == "column-count:2" {
				// Balanced columns can trim the trailing margin.
				wantHeight = 30
			}
			for id, want := range map[string][4]float32{
				"host":  {0, 0, 300, wantHeight},
				"list":  {0, 10, 300, 20},
				"child": {0, 10, 300, 20},
				"after": {0, wantHeight, 640, 10},
			} {
				if columns == "column-count:2" && (id == "list" || id == "child") {
					want[2] = 142 // (300 - normal 16px gap) / 2
				}
				got, ok := boxes[id]
				if !ok {
					t.Fatalf("missing #%s", id)
				}
				for i := range want {
					if math.Abs(float64(got.r[i]-want[i])) > .01 {
						t.Errorf("#%s: got %v, want %v", id, got.r, want)
						break
					}
				}
			}
		})
	}
}

func TestColumnsCollapseDescendantMarginsAtBreaks(t *testing.T) {
	SetWebCompat(true)
	defer SetWebCompat(false)
	boxes := domLayoutBoxes(`<!doctype html><style>
body{margin:0} #host{width:300px;column-count:2;column-gap:20px}
section>div{height:20px;margin:10px 0 5px}
</style><div id="host">
<section id="a"><div id="aa"></div></section>
<section id="b"><div id="bb"></div></section>
<section id="c"><div id="cc"></div></section>
<section id="d"><div id="dd"></div></section>
</div>`, true)
	for id, want := range map[string][4]float32{
		"host": {0, 0, 300, 60},
		"a":    {0, 10, 140, 20}, "aa": {0, 10, 140, 20},
		"b": {0, 40, 140, 20}, "bb": {0, 40, 140, 20},
		// The collapsed margins adjoining an unforced break are discarded.
		"c": {160, 0, 140, 20}, "cc": {160, 0, 140, 20},
		"d": {160, 30, 140, 20}, "dd": {160, 30, 140, 20},
	} {
		got, ok := boxes[id]
		if !ok {
			t.Fatalf("missing #%s", id)
		}
		for i := range want {
			if math.Abs(float64(got.r[i]-want[i])) > .01 {
				t.Errorf("#%s: got %v, want %v", id, got.r, want)
				break
			}
		}
	}
}
