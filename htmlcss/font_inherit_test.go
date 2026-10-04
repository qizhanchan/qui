package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// An inherited font-size (e.g. set on a form container) must reach the
// form controls — Button / Input / CheckBox render text via Style().Font,
// so the engine has to apply the computed font onto them (not just onto
// Labels). Regression for "font-size on .form didn't affect the input".
func TestInheritedFontReachesFormControls(t *testing.T) {
	root := Render(
		`<body><div class="form">
			<input type="text" placeholder="name">
			<input type="checkbox" value="ok">
			<button>Go</button>
		</div></body>`,
		`.form { display: flex; font-size: 30px; }`,
		Options{},
	)

	var input *widgets.Input
	var check *widgets.CheckBox
	var button *El
	var walk func(w qui.Widget)
	walk = func(w qui.Widget) {
		switch x := w.(type) {
		case *widgets.Input:
			input = x
		case *widgets.CheckBox:
			check = x
		case *El:
			if x.tag == "button" {
				button = x
			}
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, ch := range c.ChildList() {
				walk(ch)
			}
		}
	}
	walk(root)

	if input == nil || check == nil || button == nil {
		t.Fatalf("missing controls: input=%v check=%v button=%v", input != nil, check != nil, button != nil)
	}
	if got := input.Style().Font.Size; got != 30 {
		t.Errorf("input font size = %v, want 30 (inherited)", got)
	}
	// The button renders its text through an internal label.
	if lbl, ok := button.ChildList()[0].(*widgets.Label); !ok {
		t.Errorf("button child is %T, want *widgets.Label", button.ChildList()[0])
	} else if got := lbl.Style().Font.Size; got != 30 {
		t.Errorf("button font size = %v, want 30 (inherited)", got)
	}
	if got := check.Style().Font.Size; got != 30 {
		t.Errorf("checkbox font size = %v, want 30 (inherited)", got)
	}
	// The input's measured height must grow to fit the large font.
	if h := input.Measure(qui.Size{W: 200, H: 0}).H; h < 40 {
		t.Errorf("input height = %v, want it to grow with the 30px font", h)
	}
}
