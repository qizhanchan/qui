package widgets_test

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// The agent's Type action sets an Input's text once; its OnChange must run
// once too (Input.SetText already fires it).
func TestAgentTypeIntoInputFiresOnChangeOnce(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 300, H: 100})
	in := widgets.NewInput("")
	in.SetID("name")
	calls := 0
	in.OnChange = func(string) { calls++ }
	win.SetRoot(qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical}, in))
	win.LayoutForTest()
	if err := win.Type("#name", "hello", qui.TypeOptions{}); err != nil {
		t.Fatal(err)
	}
	if in.Text != "hello" {
		t.Fatalf("Text = %q", in.Text)
	}
	if calls != 1 {
		t.Fatalf("OnChange ran %d times, want 1", calls)
	}
}
