package svg

import (
	"strings"
	"testing"
)

func TestStyleSheetClasses(t *testing.T) {
	doc, err := Parse(strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">
<defs><style>.a{fill:#d81f26;} /* c */ path.b, #c {fill:none} rect{fill:#00ff00}</style></defs>
<rect class="a" width="1" height="1"/>
<path class="a b" d="M0 0L1 1"/>
<rect id="c" fill="red" width="1" height="1"/>
<rect class="a" style="fill:#0000ff" width="1" height="1"/>
<rect fill="red" width="1" height="1"/>
</svg>`))
	if err != nil {
		t.Fatal(err)
	}
	kids := doc.Root.Children
	if len(kids) != 5 {
		t.Fatalf("%d children", len(kids))
	}
	fill := func(i int) Paint { return kids[i].(interface{ base() *elementBase }).base().Style.Fill }
	if f := fill(0); f.Kind != PaintSolid || f.Color.R < 0.84 || f.Color.R > 0.85 {
		t.Errorf(".a rect fill = %+v, want #d81f26", f)
	}
	if f := fill(1); f.Kind != PaintNone {
		t.Errorf("path.b fill = %+v, want none (class+type beats class)", f)
	}
	if f := fill(2); f.Kind != PaintNone {
		t.Errorf("#c fill = %+v, want none (sheet beats presentation attribute)", f)
	}
	if f := fill(3); f.Kind != PaintSolid || f.Color.B != 1 {
		t.Errorf("style attr fill = %+v, want blue (inline beats sheet)", f)
	}
	if f := fill(4); f.Kind != PaintSolid || f.Color.G != 1 {
		t.Errorf("type rule fill = %+v, want green", f)
	}
}
