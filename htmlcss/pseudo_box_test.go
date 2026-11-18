package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// ::before / ::after on an element with children are boxes beside the
// children — in a block flow and as flex items — never a replacement.
func TestPseudoElementBoxesKeepChildren(t *testing.T) {
	for _, mode := range []bool{false, true} {
		SetWebCompat(mode)
		src := `<html><head><style>
#c::before{display:block;height:20px;content:"banner"}
#f{display:flex}
#f::before{content:"*";width:10px}
#f::after{content:"!";width:10px}
.k{width:50px;height:10px}
</style></head><body style="margin:0"><div id="c"><div id="a" class="k"></div><p id="p" style="margin:0" class="k"></p></div><div id="f"><div id="x" class="k"></div><div id="y" class="k"></div></div></body></html>`
		win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
		res := RenderDoc(src, "", Options{Window: win, ViewportWidth: 400, DocumentRoot: true})
		win.SetRoot(res.Root)
		sz := res.Root.Measure(qui.Size{W: 400})
		res.Root.Layout(qui.Rect{W: 400, H: sz.H})
		pos := func(id string) qui.Point {
			w := res.ByID[id]
			if w == nil {
				t.Fatalf("webCompat=%v: no #%s", mode, id)
			}
			b := w.Bounds()
			return qui.LocalPointToWindow(w, qui.Point{X: b.X, Y: b.Y})
		}
		if a := pos("a"); a.Y != 20 {
			t.Errorf("webCompat=%v: #a should sit below the 20px ::before, y=%v", mode, a.Y)
		}
		if p := pos("p"); p.Y != 30 {
			t.Errorf("webCompat=%v: #p y=%v, want 30", mode, p.Y)
		}
		if x, y := pos("x"), pos("y"); x.X != 10 || y.X != 60 {
			t.Errorf("webCompat=%v: flex items should follow the 10px ::before item: x=%v y=%v", mode, x.X, y.X)
		}
	}
	SetWebCompat(false)
}

func TestUnescapeCSS(t *testing.T) {
	for in, want := range map[string]string{
		`\a0`:               " ",
		`\f002`:             "",
		`Font Awesome\ 5`:   "Font Awesome 5",
		`\26 B`:             "&B",
		`a\"b`:              `a"b`,
		`no escapes`:        "no escapes",
		`\0`:                "�",
	} {
		if got := UnescapeCSS(in); got != want {
			t.Errorf("UnescapeCSS(%q) = %q, want %q", in, got, want)
		}
	}
	if got := unquoteContent(`"\201C" attr(x) "\"q\""`, &Node{Attrs: map[string]string{"x": "X"}}); got != "“X\"q\"" {
		t.Errorf("content with escapes = %q", got)
	}
}

// Escaped selector names (Tailwind): \: \[ \] \/ and hex escapes are part
// of the class name; a ']' inside a quoted attribute value is too.
func TestEscapedSelectors(t *testing.T) {
	sheet := ParseCSSViewport(`.flex{display:flex} @media (width>=64rem){.lg\:hidden{display:none}}
.w-\[23px\]{width:23px} .\32xl\:grid{display:grid} .bg-x\/95{height:7px} [data-x="a]b"]{min-height:5px}`, 1280)
	style := func(cls string) *ComputedStyle {
		return computeNode(&Node{Type: ElementNode, Tag: "div", Attrs: map[string]string{"class": cls, "data-x": "a]b"}}, sheet, nil)
	}
	if d := style("lg:hidden flex").Display; d != "none" {
		t.Errorf(".lg\\:hidden: display %q", d)
	}
	if w := style("w-[23px]").Width; w != 23 {
		t.Errorf(".w-\\[23px\\]: width %v", w)
	}
	if d := style("2xl:grid").Display; d != "grid" {
		t.Errorf(".\\32xl\\:grid: display %q", d)
	}
	cs := style("bg-x/95")
	if cs.Height != 7 || cs.MinHeight != 5 {
		t.Errorf(".bg-x\\/95 height %v, [data-x=\"a]b\"] min-height %v", cs.Height, cs.MinHeight)
	}
}
