package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/fonts/jetbrainsmono"
)

// TestFontFamilyListPicksFirstInstalled: a fallback list resolves to the
// first family that is registered, not blindly to the first entry.
func TestFontFamilyListPicksFirstInstalled(t *testing.T) {
	const fam = "QUI Test Fallback Family"
	if err := qui.LoadFontVariantFromBytes(fam, qui.FontWeightNormal, false, jetbrainsmono.Regular()); err != nil {
		t.Fatalf("register: %v", err)
	}
	cases := []struct{ decl, want string }{
		{`"Not Installed Sans", '` + fam + `', Arial, sans-serif`, fam},
		{`'` + fam + `', Arial`, fam},
		// Nothing installed: keep the author's first choice (renders with
		// the default face).
		{`"Not Installed Sans", Nope, sans-serif`, "Not Installed Sans"},
		// A generic keyword ends the search: families after it are not
		// consulted (it already means "the default face").
		{`Nope, sans-serif, '` + fam + `'`, "Nope"},
	}
	for _, c := range cases {
		cs := computeFor(`<body><p class="x">hi</p></body>`, `.x { font-family: `+c.decl+` }`,
			func(r *Node) *Node { return nthOfTag(r, "p", 0) })
		if cs.FontFamily != c.want {
			t.Errorf("font-family: %s -> %q, want %q", c.decl, cs.FontFamily, c.want)
		}
	}
	// The font shorthand goes through the same resolution.
	cs := computeFor(`<body><p class="x">hi</p></body>`, `.x { font: 14px "Nope Sans", '`+fam+`', sans-serif }`,
		func(r *Node) *Node { return nthOfTag(r, "p", 0) })
	if cs.FontFamily != fam {
		t.Errorf("font shorthand family = %q, want %q", cs.FontFamily, fam)
	}
}
