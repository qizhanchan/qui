package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

func TestFontShorthand(t *testing.T) {
	cs := computeFor(
		`<body><p class="x">hi</p></body>`,
		`.x { font: italic bold 20px/1.4 Georgia }`,
		func(r *Node) *Node { return nthOfTag(r, "p", 0) },
	)
	if cs.FontSize != 20 {
		t.Errorf("font-size = %v, want 20", cs.FontSize)
	}
	if cs.FontWeight != qui.FontWeightBold {
		t.Errorf("font-weight = %v, want bold", cs.FontWeight)
	}
	if !cs.Italic {
		t.Error("font-style italic not applied")
	}
	if cs.FontFamily != "Georgia" {
		t.Errorf("font-family = %q, want Georgia", cs.FontFamily)
	}
	if cs.LineHeight != 1.4 {
		t.Errorf("line-height = %v, want 1.4", cs.LineHeight)
	}
}

func TestFontShorthandLonghandWins(t *testing.T) {
	// An explicit longhand in the same block must not be overwritten.
	cs := computeFor(
		`<body><p class="x">hi</p></body>`,
		`.x { font: 12px Arial; font-size: 30px }`,
		func(r *Node) *Node { return nthOfTag(r, "p", 0) },
	)
	if cs.FontSize != 30 {
		t.Errorf("font-size = %v, want 30 (explicit longhand wins)", cs.FontSize)
	}
}

func TestFlexShorthand(t *testing.T) {
	cs := computeFor(
		`<body><div class="row"><div class="x">a</div></div></body>`,
		`.row { display: flex } .x { flex: 2 }`,
		func(r *Node) *Node { return nthOfTag(r, "div", 1) },
	)
	if cs.FlexGrow != 2 {
		t.Errorf("flex-grow = %v, want 2", cs.FlexGrow)
	}
}

func TestInsetShorthand(t *testing.T) {
	cs := computeFor(
		`<body><div class="x">a</div></body>`,
		`.x { position: relative; inset: 4px 8px 12px 16px }`,
		func(r *Node) *Node { return nthOfTag(r, "div", 0) },
	)
	if !cs.HasTop || cs.Top != 4 || cs.Right != 8 || cs.Bottom != 12 || cs.Left != 16 {
		t.Errorf("inset → t%v r%v b%v l%v, want 4/8/12/16", cs.Top, cs.Right, cs.Bottom, cs.Left)
	}
}

func TestBorderRadiusMultiValue(t *testing.T) {
	cs := computeFor(
		`<body><div class="x">a</div></body>`,
		`.x { border-radius: 8px 8px 0 0 }`,
		func(r *Node) *Node { return nthOfTag(r, "div", 0) },
	)
	// Multi-value border-radius now populates per-corner Corners (top corners
	// rounded, bottom square) instead of collapsing to the first value.
	want := qui.CornerRadii{TL: 8, TR: 8, BR: 0, BL: 0}
	if cs.Corners != want {
		t.Errorf("border-radius corners = %+v, want %+v", cs.Corners, want)
	}
}

func TestWhiteSpaceNoWrapAndEllipsis(t *testing.T) {
	res := RenderDoc(
		`<body><p id="p">a very long single line of text</p></body>`,
		`#p { white-space: nowrap; text-overflow: ellipsis; width: 60px }`,
		Options{},
	)
	// A width makes #p a visual box wrapping the label.
	var lbl *widgets.Label
	walkWidgets(res.Root, func(w qui.Widget) {
		if l, ok := w.(*widgets.Label); ok && lbl == nil {
			lbl = l
		}
	})
	if lbl == nil {
		t.Fatal("no label built for #p")
	}
	if lbl.Paragraph.Wrap {
		t.Error("white-space:nowrap should disable wrap")
	}
	if !lbl.Paragraph.Ellipsis || lbl.Paragraph.MaxLines != 1 {
		t.Errorf("text-overflow:ellipsis should set Ellipsis + MaxLines=1, got ellipsis=%v maxlines=%d",
			lbl.Paragraph.Ellipsis, lbl.Paragraph.MaxLines)
	}
}

func TestTextTransform(t *testing.T) {
	res := RenderDoc(
		`<body><p id="p">hello world</p></body>`,
		`#p { text-transform: uppercase }`,
		Options{},
	)
	lbl := res.ByID["p"].(*El).ChildList()[0].(*widgets.Label)
	if lbl.Text() != "HELLO WORLD" {
		t.Errorf("text = %q, want HELLO WORLD", lbl.Text())
	}
}

func TestTextTransformCapitalize(t *testing.T) {
	if got := transformText("hello world", "capitalize"); got != "Hello World" {
		t.Errorf("capitalize = %q, want Hello World", got)
	}
}
