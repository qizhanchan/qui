package htmlcss

import (
	"testing"
)

// TestTextSpacingAndIndentWiring verifies letter-spacing / word-spacing reach
// the Label's Font and text-indent reaches its Paragraph.FirstIndent.
func TestTextSpacingAndIndentWiring(t *testing.T) {
	res := RenderDoc(
		`<body><p id="p">spaced and indented paragraph text</p></body>`,
		`#p { letter-spacing: 2px; word-spacing: 5px; text-indent: 30px }`,
		Options{},
	)
	el := res.ByID["p"].(*El)
	lbl := el.textLabel
	if lbl == nil {
		t.Fatal("#p did not fold to a text Label")
	}
	f := lbl.Style().Font
	if f.LetterSpacing != 2 {
		t.Errorf("Font.LetterSpacing = %v, want 2", f.LetterSpacing)
	}
	if f.WordSpacing != 5 {
		t.Errorf("Font.WordSpacing = %v, want 5", f.WordSpacing)
	}
	if lbl.Paragraph.FirstIndent != 30 {
		t.Errorf("Paragraph.FirstIndent = %v, want 30", lbl.Paragraph.FirstIndent)
	}
}

// TestTextSpacingInherits confirms letter-spacing / word-spacing / text-indent
// inherit to descendant text (CSS: all three are inherited).
func TestTextSpacingInherits(t *testing.T) {
	res := RenderDoc(
		`<body class="root"><div><p id="p">child text</p></div></body>`,
		`.root { letter-spacing: 3px; word-spacing: 4px; text-indent: 12px }`,
		Options{},
	)
	lbl := res.ByID["p"].(*El).textLabel
	if lbl == nil {
		t.Fatal("#p did not fold to a text Label")
	}
	f := lbl.Style().Font
	if f.LetterSpacing != 3 || f.WordSpacing != 4 {
		t.Errorf("inherited spacing = L%v W%v, want L3 W4", f.LetterSpacing, f.WordSpacing)
	}
	if lbl.Paragraph.FirstIndent != 12 {
		t.Errorf("inherited text-indent = %v, want 12", lbl.Paragraph.FirstIndent)
	}
}

// TestLetterSpacingNormalResets checks `letter-spacing: normal` clears an
// inherited tracking value.
func TestLetterSpacingNormalResets(t *testing.T) {
	res := RenderDoc(
		`<body class="root"><p id="p">reset me</p></body>`,
		`.root { letter-spacing: 5px } #p { letter-spacing: normal }`,
		Options{},
	)
	lbl := res.ByID["p"].(*El).textLabel
	if lbl == nil {
		t.Fatal("#p did not fold to a text Label")
	}
	if f := lbl.Style().Font; f.LetterSpacing != 0 {
		t.Errorf("letter-spacing:normal left %v, want 0", f.LetterSpacing)
	}
}
