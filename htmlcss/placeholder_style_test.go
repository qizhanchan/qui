package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui/widgets"
)

// ::placeholder color / opacity reach the backing control, with a separate
// :focus variant (GitHub hides the placeholder until the field is focused).
func TestPlaceholderPseudoStyle(t *testing.T) {
	css := `.f::placeholder { opacity: 0 } .f:focus::placeholder { opacity: 1 }
		.c::placeholder { color: #ff0000 }`
	res := RenderDoc(`<body><input id="a" class="f" placeholder="you@x"><input id="b" class="c" placeholder="q"><input id="n" placeholder="n"><textarea id="t" class="f" placeholder="t"></textarea></body>`, css, Options{})
	in := func(id string) *widgets.Input {
		e := res.ByID[id].(*El)
		e.ensureBacking()
		return e.backing.(*widgets.Input)
	}
	a := in("a")
	if a.PlaceholderColor == nil || a.PlaceholderColor.A != 0 {
		t.Fatalf("resting placeholder = %+v, want transparent", a.PlaceholderColor)
	}
	if a.PlaceholderFocusColor == nil || a.PlaceholderFocusColor.A != 1 {
		t.Fatalf("focused placeholder = %+v, want opaque", a.PlaceholderFocusColor)
	}
	if b := in("b"); b.PlaceholderColor == nil || b.PlaceholderColor.R < 0.9 || b.PlaceholderColor.G > 0.1 {
		t.Fatalf("colored placeholder = %+v", b.PlaceholderColor)
	}
	if n := in("n"); n.PlaceholderColor != nil {
		t.Fatalf("unstyled placeholder overridden: %+v", n.PlaceholderColor)
	}
	te := res.ByID["t"].(*El)
	te.ensureBacking()
	if c := te.backing.(*widgets.TextArea).PlaceholderColor; c == nil || c.A != 0 {
		t.Fatalf("textarea placeholder = %+v", c)
	}
}
