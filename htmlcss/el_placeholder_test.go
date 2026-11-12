package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui/widgets"
)

// A placeholder changed after the control exists reaches the widget, and
// removing it falls back to none.
func TestPlaceholderAttributeUpdatesBacking(t *testing.T) {
	res := RenderDoc(`<input id="f" placeholder="Name">`, "", Options{})
	el, ok := res.ByID["f"].(*El)
	if !ok {
		t.Fatalf("no element: %#v", res.ByID["f"])
	}
	el.ensureBacking()
	in, ok := el.backing.(*widgets.Input)
	if !ok || in.Placeholder != "Name" {
		t.Fatalf("initial placeholder: %#v", el.backing)
	}
	el.SetAttr("placeholder", "Multiple")
	if in.Placeholder != "Multiple" {
		t.Fatalf("placeholder = %q after SetAttr", in.Placeholder)
	}
	el.RemoveAttr("placeholder")
	if in.Placeholder != "" {
		t.Fatalf("placeholder = %q after RemoveAttr", in.Placeholder)
	}
}
