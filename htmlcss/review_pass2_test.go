package htmlcss

import "testing"

// A later ::before shorthand beats an earlier longhand, as on the element.
func TestPseudoShorthandEvictsEarlierLonghand(t *testing.T) {
	sheet := ParseCSS(`.tag::before{content:"x";padding-left:8px} .tag.big::before{padding:2px}`)
	doc := ParseHTML(`<span id="s" class="tag big"></span>`)
	n := findByID(doc, "s")
	cs, content := pseudoStyle(n, sheet, "before", computeNode(n, sheet, nil))
	if content != "x" || cs == nil {
		t.Fatalf("no ::before box (content %q)", content)
	}
	if cs.Padding.Left != 2 {
		t.Fatalf("padding-left = %v, want 2", cs.Padding.Left)
	}
}

// The operator is the one at the first '=', not one inside the value.
func TestAttrSelectorOperatorInsideValue(t *testing.T) {
	sheet := ParseCSS(`[data-v="a*=b"]{color:red} [title^="x=y"]{background-color:blue}`)
	doc := ParseHTML(`<div id="d" data-v="a*=b" title="x=yz"></div>`)
	n := findByID(doc, "d")
	cs := computeNode(n, sheet, nil)
	red := computeNode(findByID(ParseHTML(`<i id="r"></i>`), "r"), ParseCSS(`i{color:red}`), nil).Color
	if cs.Color != red {
		t.Fatalf("[data-v=\"a*=b\"] did not match: color %v", cs.Color)
	}
	if !cs.HasBackground {
		t.Fatalf(`[title^="x=y"] did not match`)
	}
}

// Whitespace may follow the bang of !important.
func TestSpacedImportant(t *testing.T) {
	decls := parseDeclarations(`color: red ! important; margin: 1px !IMPORTANT`)
	if len(decls) != 2 {
		t.Fatalf("got %d declarations, want 2", len(decls))
	}
	for _, d := range decls {
		if !d.Important {
			t.Errorf("%s not important", d.Property)
		}
	}
	if decls[0].Value != "red" {
		t.Errorf("value %q, want red", decls[0].Value)
	}
}
