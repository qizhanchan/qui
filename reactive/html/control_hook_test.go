package html_test

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	h "github.com/qizhanchan/qui/reactive/html"
	"github.com/qizhanchan/qui/widgets"
)

// fieldHost is an app-built wrapper around a native Input (like q-excel's
// formula bar) that opts its Input into CSS styling.
type fieldHost struct {
	*qui.Container
	in *widgets.Input
}

func (f *fieldHost) ControlStyleTarget() qui.Widget { return f.in }

func newFieldHost() *fieldHost {
	in := widgets.NewInput("")
	c := qui.NewContainer(qui.FlexLayout{}, in)
	f := &fieldHost{Container: c, in: in}
	f.SetSelf(f)
	return f
}

var _ htmlcss.ControlStyleTarget = (*fieldHost)(nil)

// A Go-built widgets.Input gets its chrome and state styles from CSS
// through ControlStyleTarget — no States set in Go.
func TestHostedInputStatesFromCSS(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 800, H: 600})
	var host *fieldHost
	css := `.fx { border: 1px solid transparent; background: #ffffff; color: #1f1f1f;
	              font-size: 12px; padding: 2px 6px; border-radius: 4px; }
	        .fx:hover { border-color: #747775; }
	        .fx:focus { border-color: #0b57d0; border-width: 2px; }`
	rt := h.Mount(win, css, func() h.Node {
		return h.Div(h.Leaf("fx", "fx", func() *fieldHost {
			host = newFieldHost()
			return host
		}, nil).Class("fx"))
	})
	_ = rt
	if host == nil {
		t.Fatal("leaf not created")
	}
	ss := host.in.States
	if ss.Base.Font.Size != 12 || ss.Base.Padding.Left != 6 || ss.Base.Radius != 4 {
		t.Errorf("base chrome not applied: font=%v pad=%v radius=%v", ss.Base.Font.Size, ss.Base.Padding, ss.Base.Radius)
	}
	if ss.Base.Foreground != (qui.Color{R: 31.0 / 255, G: 31.0 / 255, B: 31.0 / 255, A: 1}) {
		t.Errorf("base color = %v", ss.Base.Foreground)
	}
	hover := ss.Resolve(qui.StateHover)
	if hover.Border != (qui.Color{R: 116.0 / 255, G: 119.0 / 255, B: 117.0 / 255, A: 1}) {
		t.Errorf(":hover border = %v", hover.Border)
	}
	focus := ss.Resolve(qui.StateFocused)
	if focus.BorderSize != 2 || focus.Border != (qui.Color{R: 11.0 / 255, G: 87.0 / 255, B: 208.0 / 255, A: 1}) {
		t.Errorf(":focus border = %v/%v", focus.Border, focus.BorderSize)
	}
	// The hosting element paints no box of its own.
	var el *htmlcss.El
	for _, k := range rt.Root().(*htmlcss.El).ChildList() {
		el, _ = k.(*htmlcss.El)
	}
	if el == nil {
		t.Fatal("host element not found")
	}
	if st := el.Style(); st.BorderSize != 0 || st.Background.A != 0 || st.Padding != (qui.Insets{}) {
		t.Errorf("host element keeps a box: border=%v bg=%v pad=%v", st.BorderSize, st.Background, st.Padding)
	}
}

// :disabled color / border rules reach a disabled <input>'s state.
func TestDisabledInputUsesAuthorRules(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 800, H: 600})
	css := `input:disabled { color: #ff0000; border: 1px solid #00ff00; }`
	rt := h.Mount(win, css, func() h.Node {
		return h.Div(h.Input().Disabled(true), h.Input())
	})
	var ins []*widgets.Input
	for _, k := range rt.Root().(*htmlcss.El).ChildList() {
		for _, c := range k.(*htmlcss.El).ChildList() {
			if in, ok := c.(*widgets.Input); ok {
				ins = append(ins, in)
			}
		}
	}
	if len(ins) != 2 {
		t.Fatalf("found %d inputs", len(ins))
	}
	d := ins[0].States.Resolve(qui.StateDisabled)
	if d.Foreground != (qui.Color{R: 1, A: 1}) || d.Border != (qui.Color{G: 1, A: 1}) {
		t.Errorf("disabled state fg=%v border=%v, want the :disabled rule", d.Foreground, d.Border)
	}
	// The enabled field's disabled slot keeps the native dimmed text.
	if e := ins[1].States.Resolve(qui.StateDisabled); e.Foreground == (qui.Color{R: 1, A: 1}) {
		t.Errorf("enabled field picked up the :disabled rule")
	}
}
