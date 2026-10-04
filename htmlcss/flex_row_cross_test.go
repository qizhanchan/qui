package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// A flex ROW is as tall as its tallest item, not as tall as its text: a row
// holding a 12px label and a 30px control measures 30. This is the invariant
// a property-panel row leans on — a muted caption beside a taller field.
func TestFlexRowTakesTallestChildHeight(t *testing.T) {
	res := RenderDoc(`<body><div id="row" style="display:flex;flex-direction:row;align-items:center;gap:6px">
		<span id="label" style="font-size:12px">Column width</span>
		<div id="field" style="width:60px;height:30px"></div>
	</div></body>`, ``, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	row := res.ByID["row"].(*El)
	field := res.ByID["field"].(*El).Bounds()
	if got := row.Bounds().H; got < 30 {
		t.Errorf("row height = %v, want >= 30 (its tallest child)", got)
	}
	if got := field.H; got != 30 {
		t.Errorf("field height = %v, want its explicit 30", got)
	}
	// And the child sits inside the row it was measured into.
	if field.Y < row.Bounds().Y-0.5 || field.Y+field.H > row.Bounds().Y+row.Bounds().H+0.5 {
		t.Errorf("field %v escapes row %v", field, row.Bounds())
	}
}

// The same rows inside a COLUMN that is too short to hold them: flex-shrink
// defaults to 1, so CSS squeezes the rows and their fixed-height controls
// spill out. That — not a bad row measurement — is what makes a panel
// report a dozen "widget overflows parent bounds" diagnostics before it
// becomes a scroll host. flex-shrink:0 is the fix, and this pins both
// halves so the panel cannot silently regress to the squeezed layout.
func TestFlexColumnShrinkSquashesRowsUnlessOptedOut(t *testing.T) {
	markup := func(shrink string) string {
		return `<body><div id="col" style="display:flex;flex-direction:column;gap:0;height:60px">
			<div class="r" style="display:flex;flex-direction:row;align-items:center;` + shrink + `">
				<span style="font-size:12px">a</span>
				<div class="f" style="width:20px;height:30px"></div>
			</div>
			<div class="r" style="display:flex;flex-direction:row;align-items:center;` + shrink + `">
				<span style="font-size:12px">b</span>
				<div class="f" style="width:20px;height:30px"></div>
			</div>
			<div class="r" style="display:flex;flex-direction:row;align-items:center;` + shrink + `">
				<span style="font-size:12px">c</span>
				<div class="f" style="width:20px;height:30px"></div>
			</div>
		</div></body>`
	}
	rowHeights := func(res RenderResult) []float32 {
		var out []float32
		for _, w := range res.ByClass["r"] {
			out = append(out, w.(*El).Bounds().H)
		}
		return out
	}

	// Default shrink: three 30px rows in 60px of column — every row gives
	// way, and the 30px controls inside them now overflow.
	res := RenderDoc(markup(""), ``, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})
	squeezed := rowHeights(res)
	if len(squeezed) != 3 {
		t.Fatalf("got %d rows, want 3", len(squeezed))
	}
	for i, h := range squeezed {
		if h >= 30 {
			t.Errorf("row %d height = %v, want it squeezed below 30 by flex-shrink", i, h)
		}
	}

	// Opted out: the rows keep their measured height and overflow the column
	// instead (which is what a scroll host then scrolls).
	res = RenderDoc(markup("flex-shrink:0"), ``, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})
	for i, h := range rowHeights(res) {
		if h < 30 {
			t.Errorf("row %d height = %v with flex-shrink:0, want >= 30", i, h)
		}
	}
}
