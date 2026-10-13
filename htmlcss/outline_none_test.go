package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui/widgets"
)

// `outline: none` is how a stylesheet turns off a text field's focus ring
// (the field sits in a row that draws its own focus frame). The native
// Input's focused state adds a 2px accent border; with outline:none on the
// field or its :focus rule, focus must keep the resting border instead.
func TestOutlineNoneDropsNativeFocusRing(t *testing.T) {
	css := `
		input { border: none; }
		#plain:focus { color: #ff0000; }
		#base { outline: none; }
		#onfocus:focus { outline: none; }
		#ring:focus { outline: none; border: 1px solid #00ff00; }
	`
	html := `<body>
		<input id="plain"><input id="base"><input id="onfocus"><input id="ring">
	</body>`
	res := RenderDoc(html, css, Options{})
	input := func(id string) *widgets.Input {
		el := res.ByID[id].(*El)
		el.ensureBacking()
		return el.backing.(*widgets.Input)
	}

	// Without outline:none the native ring stays (a color-only :focus rule
	// doesn't remove it, as in a browser).
	if f := input("plain").States.Focused; f == nil || f.BorderSize != 2 {
		t.Errorf("plain: focused border = %+v, want the native 2px ring", f)
	}
	for _, id := range []string{"base", "onfocus"} {
		in := input(id)
		f := in.States.Focused
		if f == nil {
			t.Fatalf("%s: no focused state", id)
		}
		if f.BorderSize != in.States.Base.BorderSize || f.Border != in.States.Base.Border {
			t.Errorf("%s: focused border = %v/%v, want the resting %v/%v",
				id, f.BorderSize, f.Border, in.States.Base.BorderSize, in.States.Base.Border)
		}
	}
	// An author :focus border still applies on top.
	if f := input("ring").States.Focused; f == nil || f.BorderSize != 1 || f.Border.G != 1 {
		t.Errorf("ring: focused border = %+v, want the author's 1px green", f)
	}
}
