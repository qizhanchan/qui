package qui_test

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// The wheel commands scroll the way they document: Agent.scroll and
// Input.dispatchMouseEvent like CDP's mouseWheel (positive dy = down),
// Input.synthesizeScrollGesture like CDP's gesture (positive yDistance =
// up). They used to pass the delta through, which scrolled the other way.
func TestProtocol_WheelDirections(t *testing.T) {
	setup := func() (*qui.Window, *widgets.ScrollView) {
		w := qui.NewTestWindow(qui.Size{W: 200, H: 200})
		sv := widgets.NewScrollView()
		sv.SetID("sv")
		sv.SetContent(widgets.NewBox(qui.FlowLayout{}), qui.Size{W: 200, H: 2000})
		w.SetRoot(sv)
		sv.Measure(qui.Size{W: 200, H: 200})
		sv.Layout(qui.Rect{W: 200, H: 200})
		sv.ScrollTo(1000)
		return w, sv
	}
	cases := []struct {
		name   string
		method string
		params map[string]any
		down   bool
	}{
		{"Agent.scroll +dy", "Agent.scroll", map[string]any{"target": "#sv", "dy": 2}, true},
		{"Agent.scroll -dy", "Agent.scroll", map[string]any{"target": "#sv", "dy": -2}, false},
		{"mouseWheel +deltaY", "Input.dispatchMouseEvent", map[string]any{"type": "mouseWheel", "x": 100, "y": 100, "deltaY": 2}, true},
		{"mouseWheel -deltaY", "Input.dispatchMouseEvent", map[string]any{"type": "mouseWheel", "x": 100, "y": 100, "deltaY": -2}, false},
		{"gesture +yDistance", "Input.synthesizeScrollGesture", map[string]any{"x": 100, "y": 100, "yDistance": 2}, false},
		{"gesture -yDistance", "Input.synthesizeScrollGesture", map[string]any{"x": 100, "y": 100, "yDistance": -2}, true},
	}
	for _, c := range cases {
		w, sv := setup()
		if _, err := call(t, w, c.method, c.params); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		got := sv.ScrollY()
		if c.down && got <= 1000 || !c.down && got >= 1000 {
			t.Errorf("%s: scrollY 1000 → %v, want %s", c.name, got, map[bool]string{true: "down (more)", false: "up (less)"}[c.down])
		}
	}
}
