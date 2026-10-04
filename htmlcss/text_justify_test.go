package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// text-align: justify reaches both text assemblies: the plain leaf Label
// (pure-text element) and the folded inline run (InlineBox, when the
// paragraph mixes inline elements).
func TestTextAlignJustifyWiring(t *testing.T) {
	res := RenderDoc(
		`<body>
			<p id="plain">alpha beta gamma delta epsilon zeta eta theta iota kappa</p>
			<p id="mixed">alpha <b>beta</b> gamma delta epsilon zeta eta theta iota kappa</p>
		</body>`,
		`p { text-align: justify; width: 160px; }`,
		Options{},
	)

	plain := res.ByID["plain"].(*El)
	if plain.textLabel == nil {
		t.Fatal("#plain should render as a text leaf")
	}
	if plain.textLabel.Paragraph.Align != qui.TextAlignJustify {
		t.Errorf("#plain Paragraph.Align = %v, want TextAlignJustify", plain.textLabel.Paragraph.Align)
	}

	mixed := res.ByID["mixed"].(*El)
	var ib *widgets.InlineBox
	for _, ch := range mixed.ChildList() {
		if b, ok := ch.(*widgets.InlineBox); ok {
			ib = b
			break
		}
	}
	if ib == nil {
		t.Fatal("#mixed should fold into an InlineBox run")
	}
	if ib.Align != qui.TextAlignJustify {
		t.Errorf("#mixed InlineBox.Align = %v, want TextAlignJustify", ib.Align)
	}
}

// End-to-end geometry: a justified wrapped paragraph's first laid-out line
// stretches to the content width while the last line stays natural.
func TestTextAlignJustifyStretch(t *testing.T) {
	res := RenderDoc(
		`<body><p id="j">alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu</p></body>`,
		`body{margin:0;padding:0} p{margin:0;text-align:justify;width:180px}`,
		Options{},
	)
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 400})

	p := res.ByID["j"].(*El)
	if p.textLabel == nil {
		t.Fatal("#j should render as a text leaf")
	}
	layout := qui.BuildTextLayout(p.textLabel.Text(), p.textLabel.Style().Font,
		p.textLabel.Paragraph.LayoutOptions(180))
	if len(layout.Lines) < 2 {
		t.Fatalf("fixture should wrap, got %d lines", len(layout.Lines))
	}
	if layout.Lines[0].JustifyExtra <= 0 || layout.Lines[0].Width != 180 {
		t.Errorf("first line extra=%v width=%v, want stretched to 180", layout.Lines[0].JustifyExtra, layout.Lines[0].Width)
	}
	if last := layout.Lines[len(layout.Lines)-1]; last.JustifyExtra != 0 {
		t.Errorf("last line JustifyExtra = %v, want 0", last.JustifyExtra)
	}
}
