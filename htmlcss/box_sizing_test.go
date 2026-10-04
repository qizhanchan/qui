package htmlcss

import (
	"testing"
)

// box-sizing default is content-box (CSS spec): the declared width/height is
// the CONTENT box, so padding + border widen the outer (border) box.
// box-sizing:border-box makes the declared size the outer box (qui native).
func TestBoxSizingContentBoxVsBorderBox(t *testing.T) {
	res := RenderDoc(
		`<body><div id="cb">x</div><div id="bb">x</div></body>`,
		`#cb { width:100px; height:40px; padding:10px; border:2px solid #000; }`+
			`#bb { width:100px; height:40px; padding:10px; border:2px solid #000; box-sizing:border-box; }`,
		Options{},
	)
	cb := res.ByID["cb"].(*El)
	bb := res.ByID["bb"].(*El)

	// content-box: outer W = 100 + 2*10 (padding) + 2*2 (border) = 124.
	if w := cb.Style().Width; w != 124 {
		t.Errorf("content-box width = %v, want 124", w)
	}
	if h := cb.Style().Height; h != 64 { // 40 + 20 + 4
		t.Errorf("content-box height = %v, want 64", h)
	}
	// border-box: declared size IS the outer box.
	if w := bb.Style().Width; w != 100 {
		t.Errorf("border-box width = %v, want 100", w)
	}
	if h := bb.Style().Height; h != 40 {
		t.Errorf("border-box height = %v, want 40", h)
	}
}

// content-box also widens min/max sizing; a width with no padding/border is
// unchanged (extra is zero).
func TestBoxSizingMinMaxAndNoExtra(t *testing.T) {
	res := RenderDoc(
		`<body><div id="a">x</div><div id="b">x</div></body>`,
		`#a { min-width:50px; max-width:200px; padding:0 8px; border:1px solid #000; }`+
			`#b { width:22px; height:22px; }`,
		Options{},
	)
	a := res.ByID["a"].(*El)
	// padding 0/8 → horizontal 16; border 1 → 2; extra = 18.
	if mw := a.Style().MinWidth; mw != 68 {
		t.Errorf("content-box min-width = %v, want 68", mw)
	}
	if mx := a.Style().MaxWidth; mx != 218 {
		t.Errorf("content-box max-width = %v, want 218", mx)
	}
	// No padding/border → no extra, width unchanged.
	b := res.ByID["b"].(*El)
	if w := b.Style().Width; w != 22 {
		t.Errorf("width with no padding/border = %v, want 22 (no extra)", w)
	}
}
